package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestNewerReleases(t *testing.T) {
	for _, c := range []struct {
		a, b  string
		newer bool
	}{
		{"0.2.0", "0.1.0", true},
		{"0.1.0", "0.2.0", false},
		{"0.1.0", "0.1.0", false},
		{"0.10.0", "0.9.0", true},
		{"1.0.0", "0.99.99", true},
		{"1.0.0", "1.0.0-rc.1", true},
		{"1.0.0-rc.1", "1.0.0", false},
		{"1.0.0-rc.2", "1.0.0-rc.1", true},
		{"", "0.1.0", false},
		{"0.2.0", "dev", false},
		{"1.2", "1.1.0", false},
	} {
		if got := newer(c.a, c.b); got != c.newer {
			t.Errorf("newer(%q, %q) = %v", c.a, c.b, got)
		}
	}
}

func setVersion(t *testing.T, v string) {
	old := appVersion
	appVersion = v
	t.Cleanup(func() { appVersion = old })
}

// testReleases serves flop's releases with latest as the newest. A release's
// flop is a script answering `version --json` as that flop would; badSums
// lists a checksum that doesn't match. It counts the times latest is asked.
func testReleases(t *testing.T, latest string, badSums bool) *atomic.Int32 {
	t.Helper()
	var asked atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("GET /latest", func(w http.ResponseWriter, r *http.Request) {
		asked.Add(1)
		if ua := r.Header.Get("User-Agent"); !strings.HasPrefix(ua, "flop/") || !strings.HasSuffix(ua, fmt.Sprintf(" (%s/%s)", runtime.GOOS, runtime.GOARCH)) {
			t.Errorf("asked as %q", ua)
		}
		fmt.Fprintln(w, latest)
	})
	mux.HandleFunc("GET /{v}/{file}", func(w http.ResponseWriter, r *http.Request) {
		v := strings.TrimPrefix(r.PathValue("v"), "v")
		name := fmt.Sprintf("flop_%s_%s_%s.tar.gz", v, runtime.GOOS, runtime.GOARCH)
		archive := releaseArchive(t, fmt.Sprintf("#!/bin/sh\necho '{\"version\":\"%s\"}'\n", v))
		sum := sha256.Sum256(archive)
		if badSums {
			sum = sha256.Sum256(nil)
		}
		switch r.PathValue("file") {
		case name:
			w.Write(archive)
		case "checksums.txt":
			fmt.Fprintf(w, "%x  %s\n", sum, name)
		default:
			http.NotFound(w, r)
		}
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	old := releasesURL
	releasesURL = srv.URL
	t.Cleanup(func() { releasesURL = old })
	return &asked
}

// releaseArchive is a release's tar.gz, flop in it, as the release workflow
// makes it
func releaseArchive(t *testing.T, flop string) []byte {
	var b bytes.Buffer
	gz := gzip.NewWriter(&b)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(&tar.Header{Name: "flop", Mode: 0o755, Size: int64(len(flop))}); err != nil {
		t.Fatal(err)
	}
	tw.Write([]byte(flop))
	tw.Close()
	gz.Close()
	return b.Bytes()
}

func notice(asJSON bool) string {
	var w bytes.Buffer
	startUpdateCheck().notify(&w, asJSON)
	return w.String()
}

// a newer release is told once a day; a new major version after every command
func TestTheUpdateNotice(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("FLOP_NO_UPDATE_CHECK", "")
	setVersion(t, "0.1.0")

	asked := testReleases(t, "0.2.0", false)
	if got := notice(false); !strings.Contains(got, "flop 0.2.0 is out (this is 0.1.0): run `flop update`") {
		t.Fatalf("a new release: %q", got)
	}
	if got := notice(false); got != "" || asked.Load() != 1 {
		t.Fatalf("the next command, the same day: %q, latest asked %d times", got, asked.Load())
	}

	// a day later, 1.0.0
	asked = testReleases(t, "1.0.0", false)
	saveReleaseCheck(releaseCheck{CheckedAt: time.Now().Add(-checkEvery - time.Minute), Latest: "0.2.0"})
	for range 2 {
		if got := notice(false); !strings.Contains(got, "flop 1.0.0 is out, a new major version: this flop (0.1.0) may no longer work") {
			t.Fatalf("a new major version: %q", got)
		}
	}
	var e updateAvailable
	if err := json.Unmarshal([]byte(notice(true)), &e); err != nil || e != (updateAvailable{"update", "0.1.0", "1.0.0", true}) {
		t.Fatalf("as JSON: %+v (%v)", e, err)
	}
	if asked.Load() != 1 {
		t.Fatalf("latest asked %d times in a day", asked.Load())
	}

	setVersion(t, "1.0.0")
	if got := notice(false); got != "" {
		t.Fatalf("the latest: %q", got)
	}

	// offline: nothing said, and not asked again that day
	releasesURL = "http://127.0.0.1:1"
	saveReleaseCheck(releaseCheck{})
	if got := notice(false); got != "" || time.Since(loadReleaseCheck().CheckedAt) > time.Minute {
		t.Fatalf("offline: %q, %+v", got, loadReleaseCheck())
	}

	setVersion(t, "dev")
	if startUpdateCheck() != nil {
		t.Fatal("a build from source asks")
	}
	setVersion(t, "0.1.0")
	t.Setenv("FLOP_NO_UPDATE_CHECK", "1")
	if startUpdateCheck() != nil {
		t.Fatal("FLOP_NO_UPDATE_CHECK=1 asks")
	}
}

func TestUpdateReplacesThisFlop(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	setVersion(t, "0.1.0")
	dir := t.TempDir()
	exe := filepath.Join(dir, "flop")
	if err := os.WriteFile(exe, []byte("the old flop"), 0o755); err != nil {
		t.Fatal(err)
	}
	o, _, _ := testUI("", false, false)
	ctx := context.Background()

	testReleases(t, "0.2.0", true)
	if _, err := updateAt(ctx, o, exe); err == nil || !strings.Contains(err.Error(), "doesn't match its checksum") {
		t.Fatalf("a bad checksum: %v", err)
	}
	if b, _ := os.ReadFile(exe); string(b) != "the old flop" {
		t.Fatalf("a bad checksum replaced flop: %q", b)
	}

	testReleases(t, "0.2.0", false)
	u, err := updateAt(ctx, o, exe)
	if err != nil || u != (updated{From: "0.1.0", To: "0.2.0", Path: exe, Updated: true}) {
		t.Fatalf("update: %+v (%v)", u, err)
	}
	if out, err := exec.Command(exe, "version", "--json").Output(); err != nil || !strings.Contains(string(out), `"0.2.0"`) {
		t.Fatalf("the new flop: %q (%v)", out, err)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 1 {
		t.Fatalf("left beside it: %v", entries)
	}
	if c := loadReleaseCheck(); c.Latest != "0.2.0" {
		t.Fatalf("remembered: %+v", c)
	}

	setVersion(t, "0.2.0")
	if u, err := updateAt(ctx, o, exe); err != nil || u.Updated {
		t.Fatalf("the latest already: %+v (%v)", u, err)
	}
}

// a build from source (as the tests build flop) refuses
func TestABuildFromSourceDoesNotUpdate(t *testing.T) {
	r := runFlop(t, "http://127.0.0.1:1", "", nil, "update", "--json")
	if e := jsonError(t, r); r.code != 2 || !strings.Contains(e.Message, "build from source") {
		t.Fatalf("update: %+v", r)
	}
}
