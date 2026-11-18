package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/flowercomputers/flop/api"
)

func (c *Client) BogMetrics(ctx context.Context, bog, window string) (api.BogMetrics, error) {
	var out api.BogMetrics
	q := url.Values{}
	if window != "" {
		q.Set("window", window)
	}
	err := c.doJSON(ctx, http.MethodGet, "/api/v1/bogs/"+url.PathEscape(bog)+"/metrics?"+q.Encode(), nil, &out)
	return out, err
}

func (c *Client) BogLogs(ctx context.Context, bog, since string, limit int) ([]api.LogEvent, error) {
	var out api.BogLogs
	err := c.doJSON(ctx, http.MethodGet, "/api/v1/bogs/"+url.PathEscape(bog)+"/logs?"+logQuery(since, limit, false), nil, &out)
	return out.Events, err
}

// FollowLogs streams a bog's recent lines, then new ones, until ctx ends.
func (c *Client) FollowLogs(ctx context.Context, bog, since string, limit int, each func(api.LogEvent)) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/api/v1/bogs/"+url.PathEscape(bog)+"/logs?"+logQuery(since, limit, true), nil)
	if err != nil {
		return err
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	if c.organizationID != "" {
		req.Header.Set(api.HeaderOrganizationID, c.organizationID)
	}
	req.Header.Set("User-Agent", userAgent())
	resp, err := c.stream.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		buf, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return &APIError{Status: resp.StatusCode, Body: string(buf)}
	}
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for sc.Scan() {
		var e api.LogEvent
		if json.Unmarshal(sc.Bytes(), &e) == nil {
			each(e)
		}
	}
	if ctx.Err() != nil {
		return nil
	}
	return sc.Err()
}

func logQuery(since string, limit int, follow bool) string {
	q := url.Values{}
	if since != "" {
		q.Set("since", since)
	}
	if limit > 0 {
		q.Set("limit", fmt.Sprint(limit))
	}
	if follow {
		q.Set("follow", "true")
	}
	return q.Encode()
}

// bogStats is a bog's metrics over a window
type bogStats struct {
	api.BogMetrics
}

func (m bogStats) human(w io.Writer) { writeStats(w, m.BogMetrics) }

// Stats returns a bog's metrics over a window (the last hour by default).
func Stats(ctx context.Context, bog, window string) (bogStats, error) {
	client, err := loggedInClient()
	if err != nil {
		return bogStats{}, err
	}
	if bog == "" {
		return bogStats{}, usage("stats needs a bog", "flop bog stats <bog> [--window 1h]")
	}
	m, err := client.BogMetrics(ctx, bog, window)
	if err != nil {
		return bogStats{}, fmt.Errorf("stats: %w", err)
	}
	return bogStats{m}, nil
}

func writeStats(out io.Writer, m api.BogMetrics) error {
	fmt.Fprintf(out, "%s · last %s, a point every %s\n\n", m.Bog, friendlyDuration(m.Window), friendlyDuration(m.Step))
	tw := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
	for _, s := range m.Series {
		if len(s.Points) == 0 {
			fmt.Fprintf(tw, "%s\t%s\tno data\n", s.Title, strings.Repeat("·", 12))
			continue
		}
		values := make([]float64, len(s.Points))
		peak := 0.0
		for i, p := range s.Points {
			values[i] = p.V
			peak = math.Max(peak, p.V)
		}
		fmt.Fprintf(tw, "%s\t%s\t%s now\tpeak %s\n", s.Title, sparkline(values, 40), format(values[len(values)-1], s.Unit), format(peak, s.Unit))
	}
	return tw.Flush()
}

var bars = []rune("▁▂▃▄▅▆▇█")

// sparkline draws values in at most width columns, scaled to their peak
func sparkline(values []float64, width int) string {
	if len(values) > width {
		// keep the most recent width points
		values = values[len(values)-width:]
	}
	peak := 0.0
	for _, v := range values {
		peak = math.Max(peak, v)
	}
	var b strings.Builder
	for _, v := range values {
		i := 0
		if peak > 0 {
			i = int(math.Round(v / peak * float64(len(bars)-1)))
		}
		b.WriteRune(bars[i])
	}
	return b.String()
}

func format(v float64, unit string) string {
	switch unit {
	case "per_second":
		return fmt.Sprintf("%.2f/s", v)
	case "seconds":
		if v < 1 {
			return fmt.Sprintf("%.0f ms", v*1000)
		}
		return fmt.Sprintf("%.2f s", v)
	case "cores":
		return fmt.Sprintf("%.3f cores", v)
	case "bytes":
		for _, u := range []struct {
			n    float64
			name string
		}{{1 << 30, "GB"}, {1 << 20, "MB"}, {1 << 10, "KB"}} {
			if v >= u.n {
				return fmt.Sprintf("%.1f %s", v/u.n, u.name)
			}
		}
		return fmt.Sprintf("%.0f B", v)
	}
	return fmt.Sprintf("%g", v)
}

// friendlyDuration turns "1h0m0s" into "1h", "1m0s" into "1m"
func friendlyDuration(d string) string {
	parsed, err := time.ParseDuration(d)
	if err != nil {
		return d
	}
	s := parsed.String()
	// only whole units: "30m0s" is "30m", "1h0m0s" is "1h"; never touch the
	// digits ("30m" must not lose its 0)
	if strings.HasSuffix(s, "m0s") {
		s = strings.TrimSuffix(s, "0s")
	}
	if strings.HasSuffix(s, "h0m") {
		s = strings.TrimSuffix(s, "0m")
	}
	return s
}

// LogsCLI prints a bog's recent lines; with follow, keeps printing new ones.
// logLines is a bog's recent log lines
type logLines struct {
	Bog    string         `json:"bog"`
	Events []api.LogEvent `json:"events"`
}

func (l logLines) human(w io.Writer) {
	if len(l.Events) == 0 {
		fmt.Fprintln(w, "No log lines in that window.")
	}
	for _, e := range l.Events {
		logEvent(e).human(w)
	}
}

// logEvent is one line, as `flop bog logs --follow` streams them (with
// --json, one JSON object per line)
type logEvent api.LogEvent

func (e logEvent) human(w io.Writer) { fmt.Fprintln(w, logLine(api.LogEvent(e))) }

// Logs returns a bog's recent lines; with follow, it streams new ones until
// interrupted, and returns no result.
func Logs(ctx context.Context, o *ui, bog, since string, limit int, follow bool) (result, error) {
	client, err := loggedInClient()
	if err != nil {
		return nil, err
	}
	if bog == "" {
		return nil, usage("logs needs a bog", "flop bog logs <bog> [--since 1h] [--follow]")
	}
	if follow {
		return nil, client.FollowLogs(ctx, bog, since, limit, func(e api.LogEvent) { o.event(logEvent(e)) })
	}
	events, err := client.BogLogs(ctx, bog, since, limit)
	if err != nil {
		return nil, fmt.Errorf("logs: %w", err)
	}
	if events == nil {
		events = []api.LogEvent{}
	}
	return logLines{Bog: bog, Events: events}, nil
}

func logLine(e api.LogEvent) string {
	return e.Timestamp.Local().Format("15:04:05") + "  " + e.Message
}
