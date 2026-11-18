package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"time"
)

// A release of flop asks, at most once a day and while a command runs, which
// release is the latest (flop/latest, as install.sh does), and says after the
// command when a newer one is out: a new major version (a breaking change)
// after every command until flop is updated, anything else once a day.
// `flop update` installs it. A build from source ("dev") or
// FLOP_NO_UPDATE_CHECK=1 asks nothing.

// flop's releases; tests point it at their own server
var releasesURL = "https://dl.flowercomputer.com/flop"

var releasesClient = &http.Client{Timeout: 2 * time.Minute}

const checkEvery = 24 * time.Hour

// releaseCheck is what flop last learned of its releases (update.json, beside
// the logins)
type releaseCheck struct {
	CheckedAt time.Time `json:"checked_at"`
	Latest    string    `json:"latest,omitempty"`
}

// updateCheck is this run's: started before the command, read after it
type updateCheck struct {
	done    chan struct{}
	state   releaseCheck
	fetched bool // learned on this run, not remembered
}

// skipUpdateNotice: flop update says it all itself
var skipUpdateNotice bool

func startUpdateCheck() *updateCheck {
	if appVersion == "dev" || os.Getenv("FLOP_NO_UPDATE_CHECK") != "" {
		return nil
	}
	c := &updateCheck{done: make(chan struct{}), state: loadReleaseCheck()}
	if age := time.Since(c.state.CheckedAt); age >= 0 && age < checkEvery {
		close(c.done)
		return c
	}
	go func() {
		defer close(c.done)
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if latest, err := latestRelease(ctx); err == nil {
			c.state.Latest, c.fetched = latest, true
		}
		// a failure too: flop offline asks once a day, not before every command
		c.state.CheckedAt = time.Now()
		saveReleaseCheck(c.state)
	}()
	return c
}

// updateAvailable is the notice with --json: a JSON line on stderr
type updateAvailable struct {
	Event    string `json:"event"`    // "update"
	Version  string `json:"version"`  // this flop's
	Latest   string `json:"latest"`   // the release `flop update` installs
	Breaking bool   `json:"breaking"` // a new major version: this flop may no longer work
}

// notify says, on w, when a newer release is out
func (c *updateCheck) notify(w io.Writer, asJSON bool) {
	if c == nil || skipUpdateNotice {
		return
	}
	<-c.done
	latest := c.state.Latest
	if !newer(latest, appVersion) {
		return
	}
	breaking := majorOf(latest) > majorOf(appVersion)
	if !breaking && !c.fetched {
		return
	}
	switch {
	case asJSON:
		json.NewEncoder(w).Encode(updateAvailable{Event: "update", Version: appVersion, Latest: latest, Breaking: breaking})
	case breaking:
		fmt.Fprintf(w, "\nflop %s is out, a new major version: this flop (%s) may no longer work; run `flop update`\n", latest, appVersion)
	default:
		fmt.Fprintf(w, "\nflop %s is out (this is %s): run `flop update`\n", latest, appVersion)
	}
}

// updated is what flop update did
type updated struct {
	From    string `json:"from"`
	To      string `json:"to"`
	Path    string `json:"path"`
	Updated bool   `json:"updated"` // false: this flop is the latest already
}

func (u updated) human(w io.Writer) {
	if !u.Updated {
		fmt.Fprintf(w, "flop %s is the latest\n", u.From)
		return
	}
	fmt.Fprintf(w, "updated flop %s → %s at %s\n", u.From, u.To, u.Path)
}

// Update installs the latest release over this flop. The latest is whatever
// flop/latest names, older or newer: a release taken back there takes back
// whoever updates.
func Update(ctx context.Context, o *ui) (updated, error) {
	skipUpdateNotice = true
	if appVersion == "dev" {
		return updated{}, usage("this flop is a build from source, not a release: it doesn't update itself",
			"install a release: `curl -fsSL https://flowercomputer.com/install.sh | sh`")
	}
	exe, err := os.Executable()
	if err == nil {
		exe, err = filepath.EvalSymlinks(exe)
	}
	if err != nil {
		return updated{}, fmt.Errorf("find this flop: %w", err)
	}
	return updateAt(ctx, o, exe)
}

// updateAt installs the latest release at exe, as install.sh does: this
// machine's archive, checked against the release's checksums, written beside
// exe, run, then renamed over it (a flop running meanwhile keeps its file)
func updateAt(ctx context.Context, o *ui, exe string) (updated, error) {
	latest, err := latestRelease(ctx)
	if err != nil {
		return updated{}, unreachable(err)
	}
	saveReleaseCheck(releaseCheck{CheckedAt: time.Now(), Latest: latest})
	u := updated{From: appVersion, To: latest, Path: exe}
	if latest == appVersion {
		return u, nil
	}
	o.say("downloading flop %s (%s/%s)", latest, runtime.GOOS, runtime.GOARCH)
	bin, err := downloadRelease(ctx, latest)
	if err != nil {
		return u, err
	}
	tmp := filepath.Join(filepath.Dir(exe), fmt.Sprintf(".flop.%d", os.Getpid()))
	defer os.Remove(tmp) // gone already once it is renamed
	if err := os.WriteFile(tmp, bin, 0o755); err != nil {
		if errors.Is(err, fs.ErrPermission) {
			return u, fail("failed", fmt.Sprintf("flop is in %s, which you can't write", filepath.Dir(exe)),
				fmt.Sprintf("run `sudo %s update`", exe))
		}
		return u, err
	}
	cmd := exec.CommandContext(ctx, tmp, "version", "--json")
	cmd.Env = append(os.Environ(), "FLOP_NO_UPDATE_CHECK=1")
	out, err := cmd.Output()
	var v versionInfo
	if err != nil || json.Unmarshal(out, &v) != nil || v.Version != latest {
		return u, fail("failed", fmt.Sprintf("flop %s doesn't run on this machine: not installed", latest), "")
	}
	if err := os.Rename(tmp, exe); err != nil {
		return u, fmt.Errorf("replace %s: %w", exe, err)
	}
	u.Updated = true
	return u, nil
}

// latestRelease is the newest release's version
func latestRelease(ctx context.Context) (string, error) {
	b, err := fetch(ctx, releasesURL+"/latest")
	if err != nil {
		return "", err
	}
	v := strings.TrimPrefix(strings.TrimSpace(string(b)), "v")
	if _, _, ok := parseVersion(v); !ok {
		return "", fmt.Errorf("%s/latest: %q isn't a version", releasesURL, v)
	}
	return v, nil
}

// downloadRelease is a release's flop for this machine, checked against the
// release's checksums.txt
func downloadRelease(ctx context.Context, version string) ([]byte, error) {
	name := fmt.Sprintf("flop_%s_%s_%s.tar.gz", version, runtime.GOOS, runtime.GOARCH)
	base := fmt.Sprintf("%s/v%s/", releasesURL, version)
	sums, err := fetch(ctx, base+"checksums.txt")
	if err != nil {
		return nil, unreachable(err)
	}
	archive, err := fetch(ctx, base+name)
	if err != nil {
		return nil, unreachable(err)
	}
	got := sha256.Sum256(archive)
	if want, ok := checksum(sums, name); !ok || want != hex.EncodeToString(got[:]) {
		return nil, fail("failed", name+" doesn't match its checksum: not installed", "")
	}
	gz, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if err != nil {
			return nil, fmt.Errorf("%s: no flop in it (%w)", name, err)
		}
		if h.Name == "flop" {
			return io.ReadAll(tr)
		}
	}
}

// checksum is name's in a sha256sum listing
func checksum(sums []byte, name string) (string, bool) {
	for line := range strings.SplitSeq(string(sums), "\n") {
		if f := strings.Fields(line); len(f) == 2 && f[1] == name {
			return f[0], true
		}
	}
	return "", false
}

func fetch(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent())
	resp, err := releasesClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: %s", url, resp.Status)
	}
	return io.ReadAll(resp.Body)
}

func unreachable(err error) error {
	return fail("unavailable", err.Error(), "flop's releases ("+releasesURL+") could not be reached; check your connection")
}

func loadReleaseCheck() releaseCheck {
	var s releaseCheck
	if dir, err := configDir(); err == nil {
		if b, err := os.ReadFile(filepath.Join(dir, "update.json")); err == nil {
			json.Unmarshal(b, &s)
		}
	}
	return s
}

// saveReleaseCheck remembers it; a failure here fails no command
func saveReleaseCheck(s releaseCheck) {
	dir, err := configDir()
	if err != nil || os.MkdirAll(dir, 0o700) != nil {
		return
	}
	b, _ := json.Marshal(s)
	os.WriteFile(filepath.Join(dir, "update.json"), b, 0o600)
}

// parseVersion reads a release's version: 1.2.3, or 1.2.3-rc.1 (a
// pre-release, before 1.2.3)
func parseVersion(s string) (n [3]int, pre string, ok bool) {
	core, pre, _ := strings.Cut(s, "-")
	parts := strings.Split(core, ".")
	if len(parts) != 3 {
		return n, "", false
	}
	for i, p := range parts {
		x, err := strconv.Atoi(p)
		if err != nil || x < 0 {
			return n, "", false
		}
		n[i] = x
	}
	return n, pre, true
}

// newer: a is a later release than b (pre-releases of one version compared as
// text)
func newer(a, b string) bool {
	na, pa, okA := parseVersion(a)
	nb, pb, okB := parseVersion(b)
	if !okA || !okB {
		return false
	}
	if c := slices.Compare(na[:], nb[:]); c != 0 {
		return c > 0
	}
	switch {
	case pa == pb:
		return false
	case pa == "" || pb == "":
		return pa == "" // 1.2.3 is after 1.2.3-rc.1
	}
	return pa > pb
}

func majorOf(v string) int {
	n, _, _ := parseVersion(v)
	return n[0]
}
