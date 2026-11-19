package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/flowercomputers/flop/api"
)

// hoursPerMonth turns an hourly price into a month's, as AWS and spade's
// billing count it
const hoursPerMonth = 730

// sizeList is what a bog's machine and data may be, for this organization:
// every size with its price and whether it may be chosen,
// the data range, and the organization's limits
type sizeList struct {
	api.SizesResponse
}

func Sizes(ctx context.Context) (sizeList, error) {
	client, err := loggedInClient()
	if err != nil {
		return sizeList{}, err
	}
	s, err := client.GetSizes(ctx)
	if err != nil {
		return sizeList{}, fmt.Errorf("sizes: %w", err)
	}
	return sizeList{s}, nil
}

func (l sizeList) human(out io.Writer) {
	tw := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "MEMORY\tCPU\tVCPUS\tPER HOUR\tPER MONTH\t")
	for _, s := range l.Sizes {
		vcpus := fmt.Sprint(s.VCPUs)
		if s.Burstable {
			vcpus += " burstable"
		}
		note := ""
		switch {
		case !s.Allowed:
			note = "not now: " + s.NotAllowed
		case s.Default:
			note = "the default"
		case s.Tier == api.TierSandbox && s.Sleeps == "suspend":
			note = "sandbox: billed awake only; sleeps when idle, wakes in about 0.6 s"
		case s.Tier == api.TierSandbox:
			note = "sandbox: billed awake only; sleeps when idle, wakes from a cold start (about 1 s)"
		}
		fmt.Fprintf(tw, "%d GB\t%s\t%s\t%s\t%s\t%s\n", s.MemoryGB, s.CPU, vcpus,
			dollars(s.PricePerHour, 4), dollars(s.PricePerHour*hoursPerMonth, 2), note)
	}
	tw.Flush()
	if sd := l.SandboxData; sd.MaxGB > 0 {
		fmt.Fprintf(out, "\nsandbox data: %d to %d GB (default %d), %s per GB a month\n", sd.MinGB, sd.MaxGB, sd.DefaultGB, dollars(sd.PricePerGBMonth, 2))
	}
	d, lim := l.Data, l.Limits
	fmt.Fprintf(out, "\ndata: %d to %d GB (default %d; it only grows), %s per GB a month\n",
		d.MinGB, d.MaxGB, d.DefaultGB, dollars(d.PricePerGBMonth, 2))
	fmt.Fprintf(out, "this organization: %d of %d GB of memory in use, %d of %d GB of data kept, %d of %d sandbox bogs running (Flower Computer raises these)\n",
		lim.MemoryInUseGB, lim.MemoryGB, lim.DataKeptGB, lim.DataGB, lim.SandboxBogsInUse, lim.SandboxBogs)
	fmt.Fprintln(out, "choose with `flop bog deploy --memory 16 --cpu more --data 30`, `flop bog resize`, `flop bog grow`")
}

// dollars is m with places decimals: $0.0227 an hour, $16.57 a month
func dollars(m api.Money, places int) string {
	return fmt.Sprintf("$%.*f", places, float64(m)/float64(api.MoneyPerDollar))
}

// machine is a deployment's size in spade's words: "16 GB more"
func machine(memoryGB int, cpu string) string {
	if cpu == "" {
		cpu = "standard"
	}
	return fmt.Sprintf("%d GB %s", memoryGB, cpu)
}

// resized is what `flop bog resize` did: the bog's machine before and after
type resized struct {
	Bog  string `json:"bog"`
	From string `json:"from"` // "8 GB standard"
	To   string `json:"to"`
	// Unchanged: it already runs at that size; nothing was asked of spade
	Unchanged  bool           `json:"unchanged,omitempty"`
	Waited     bool           `json:"waited"`
	Deployment api.Deployment `json:"deployment"`
}

func (r resized) human(w io.Writer) {
	switch {
	case r.Unchanged:
		fmt.Fprintf(w, "%s already runs at %s\n", r.Bog, r.To)
	case r.Waited:
		fmt.Fprintf(w, "%s runs at %s on %s\n", r.Bog, r.To, box(r.Deployment))
	default:
		fmt.Fprintf(w, "Moving %s from %s to %s; follow with `flop bog wait %s`\n", r.Bog, r.From, r.To, r.Bog)
	}
}

// Resize moves a bog to a machine of another size: a new box in the same
// zone, its data moved (the bog is down meanwhile). memoryGB 0 or cpu ""
// keep what it has. Waits for the move unless noWait.
func Resize(ctx context.Context, o *ui, bog string, memoryGB int, cpu string, noWait bool, timeout time.Duration) (resized, error) {
	client, err := loggedInClient()
	if err != nil {
		return resized{}, err
	}
	if bog == "" {
		return resized{}, usage("resize needs a bog", "flop bog resize <bog> --memory 16 [--cpu more]")
	}
	if memoryGB == 0 && cpu == "" {
		return resized{}, usage("resize needs --memory, --cpu or both", "see the sizes with `flop sizes`")
	}
	d, err := resolveDeployment(ctx, client, bog)
	if err != nil {
		return resized{}, err
	}
	return resize(ctx, o, client, d, memoryGB, cpu, noWait, timeout)
}

func resize(ctx context.Context, o *ui, client *Client, d api.Deployment, memoryGB int, cpu string, noWait bool, timeout time.Duration) (resized, error) {
	if memoryGB == 0 {
		memoryGB = d.MemoryGB
	}
	if cpu == "" {
		cpu = d.CPU
	}
	out := resized{Bog: d.Bog, From: machine(d.MemoryGB, d.CPU), To: machine(memoryGB, cpu), Deployment: d}
	if out.From == out.To {
		out.Unchanged = true
		return out, nil
	}
	if (d.CPU == "shared") != (cpu == "shared") {
		return move(ctx, o, client, d, out, memoryGB, cpu, noWait, timeout)
	}
	o.say("Moving %s from %s to %s: a new box in the same zone; the bog is down while its data moves (a few minutes)", d.Bog, out.From, out.To)
	started := time.Now()
	updated, err := client.ResizeDeployment(ctx, d.ID, api.ResizeDeploymentRequest{MemoryGB: memoryGB, CPU: cpu})
	if err != nil {
		return resized{}, sizeRefusal("resize", err)
	}
	out.Deployment = updated
	if noWait {
		return out, nil
	}
	if out.Deployment, err = waitForDeployment(ctx, client, d.ID, d.Generation, started, 2*time.Second, timeout, o.err); err != nil {
		return resized{}, err
	}
	// a move that fails leaves the bog running where it was, at its size
	if now := machine(out.Deployment.MemoryGB, out.Deployment.CPU); now != out.To {
		return resized{}, fail("bog_failed", fmt.Sprintf("%s could not move to %s and still runs at %s: %s", d.Bog, out.To, now, orDash(out.Deployment.BoxError)),
			fmt.Sprintf("try again later (`flop bog resize %s`), or choose another size (`flop sizes`)", d.Bog))
	}
	out.Waited = true
	return out, nil
}

// move resizes across tiers: a shared size runs in the sandbox, the others
// on a box of their own, so the bog moves with its data. It is built for
// the other tier first (it keeps running), then is down from its final copy
// until it is healthy there; a move that doesn't get healthy moves back.
func move(ctx context.Context, o *ui, client *Client, d api.Deployment, out resized, memoryGB int, cpu string, noWait bool, timeout time.Duration) (resized, error) {
	where := "a box of its own"
	if cpu == "shared" {
		where = "the sandbox"
	}
	o.say("Moving %s from %s to %s, on %s: it's built for there first and keeps running meanwhile, then is down while its data moves (a few minutes)",
		d.Bog, out.From, out.To, where)
	var started api.MoveResponse
	if err := client.doJSON(ctx, http.MethodPost, "/api/v1/deployments/"+url.PathEscape(d.ID)+"/resize",
		api.ResizeDeploymentRequest{MemoryGB: memoryGB, CPU: cpu}, &started); err != nil {
		return resized{}, sizeRefusal("resize", err)
	}
	if noWait {
		return out, nil
	}
	deadline := time.Now().Add(timeout)
	for {
		if time.Now().After(deadline) {
			return resized{}, fail("timeout", fmt.Sprintf("%s is still moving after %s", d.Bog, timeout), fmt.Sprintf("follow with `flop bog status %s`", d.Bog))
		}
		select {
		case <-ctx.Done():
			return resized{}, ctx.Err()
		case <-time.After(3 * time.Second):
		}
		b, err := client.GetBog(ctx, d.Bog)
		if err != nil {
			if transient(err) {
				continue
			}
			return resized{}, err
		}
		if strings.HasPrefix(b.Status, "moving") || b.Deployment == nil {
			continue
		}
		now := machine(b.Deployment.MemoryGB, b.Deployment.CPU)
		switch {
		case now == out.To && b.Healthy:
			out.Deployment, out.Waited = *b.Deployment, true
			return out, nil
		case now == out.From:
			return resized{}, fail("bog_failed", fmt.Sprintf("%s could not move to %s and runs at %s, where it was", d.Bog, out.To, now),
				fmt.Sprintf("the organization's activity in the panel says why; try again later (`flop bog resize %s`)", d.Bog))
		}
	}
}

// grown is what `flop bog grow` did
type grown struct {
	Bog    string `json:"bog"`
	FromGB int    `json:"from_gb"`
	ToGB   int    `json:"to_gb"`
	Waited bool   `json:"waited"`
	// Deployed: the bog runs, so its filesystem grew while it served;
	// otherwise it grows at its next deploy
	Deployed bool          `json:"deployed"`
	Data     api.BogVolume `json:"data"`
}

func (g grown) human(w io.Writer) {
	switch {
	case !g.Waited:
		fmt.Fprintf(w, "Growing the data of %s from %d to %d GB; follow with `flop bog status %s`\n", g.Bog, g.FromGB, g.ToGB, g.Bog)
	case g.Deployed:
		fmt.Fprintf(w, "The data of %s is %d GB, grown while it served\n", g.Bog, g.ToGB)
	default:
		fmt.Fprintf(w, "The data of %s is %d GB; its filesystem grows at its next deploy\n", g.Bog, g.ToGB)
	}
}

// Grow grows a bog's data to sizeGB, online: EBS grows the volume and the
// box grows the filesystem while the bog runs. Data only grows, at most once
// in 6 hours. Waits until the bog's filesystem shows it unless noWait.
func Grow(ctx context.Context, o *ui, bog string, sizeGB int, noWait bool, timeout time.Duration) (grown, error) {
	client, err := loggedInClient()
	if err != nil {
		return grown{}, err
	}
	if bog == "" || sizeGB == 0 {
		return grown{}, usage("grow needs a bog and its new size", "flop bog grow <bog> --data 30")
	}
	b, err := client.GetBog(ctx, bog)
	if err != nil {
		return grown{}, fmt.Errorf("bog %s: %w", bog, err)
	}
	return grow(ctx, o, client, b, sizeGB, noWait, timeout)
}

func grow(ctx context.Context, o *ui, client *Client, b api.Bog, sizeGB int, noWait bool, timeout time.Duration) (grown, error) {
	if b.Data == nil {
		return grown{}, fail("not_found", fmt.Sprintf("bog %s has no data to grow", b.Name), "a bog's data starts at its first deploy (`flop bog deploy --data 30`)")
	}
	out := grown{Bog: b.Name, FromGB: b.Data.SizeGB, ToGB: sizeGB, Data: *b.Data}
	if sizeGB <= b.Data.SizeGB {
		return grown{}, usage(fmt.Sprintf("the data of %s is %d GB already; data only grows", b.Name, b.Data.SizeGB), "")
	}
	var before int64 // its filesystem's size now, if it runs
	if b.Deployment != nil && b.Deployment.Observed != nil {
		before = b.Deployment.Observed.DataSizeBytes
	}
	v, err := client.GrowBogData(ctx, b.Name, sizeGB)
	if err != nil {
		return grown{}, sizeRefusal("grow", err)
	}
	out.Data = v
	if noWait {
		return out, nil
	}
	o.say("Growing the data of %s from %d to %d GB", b.Name, out.FromGB, sizeGB)
	deadline := time.Now().Add(timeout)
	for {
		now, err := client.GetBog(ctx, b.Name)
		if err != nil && !transient(err) {
			return grown{}, fmt.Errorf("follow the grow: %w", err)
		}
		if err == nil && now.Data != nil {
			out.Data = *now.Data
			deployed := now.Deployment != nil && now.Deployment.DesiredState == api.DesiredRunning
			var fs int64
			if deployed && now.Deployment.Observed != nil {
				fs = now.Deployment.Observed.DataSizeBytes
			}
			switch {
			case now.Data.Pending == "" && now.Data.SizeGB < sizeGB:
				// spade asked and EC2 refused (the request is dropped)
				return grown{}, fail("bog_failed", fmt.Sprintf("spade could not grow the data of %s (it is %d GB)", b.Name, now.Data.SizeGB),
					"try again later; if it keeps failing, Flower Computer can see why in spade's logs")
			case now.Data.SizeGB >= sizeGB && (!deployed || fs > before):
				out.Waited, out.Deployed = true, deployed
				return out, nil
			}
		}
		if time.Now().After(deadline) {
			return grown{}, fail("timeout", fmt.Sprintf("gave up after %s waiting for the data of %s to grow", timeout, b.Name),
				fmt.Sprintf("it may still get there: `flop bog status %s`", b.Name))
		}
		select {
		case <-ctx.Done():
			return grown{}, ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
}

// sizeRefusal explains spade refusing a size: an unknown one points at the
// catalog; a card and the limits keep spade's own words (402, 409)
func sizeRefusal(what string, err error) error {
	var apiErr *APIError
	if errors.As(err, &apiErr) && apiErr.Status == http.StatusBadRequest {
		return fail("invalid", strings.TrimSpace(apiErr.Body), "see what may be chosen: `flop sizes`")
	}
	return fmt.Errorf("%s: %w", what, err)
}
