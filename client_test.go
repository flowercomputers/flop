package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"runtime"
	"testing"

	"github.com/flowercomputers/flop/api"
)

// every request to spade names this flop (spade logs it): its version, os and
// arch
func TestRequestsNameThisFlop(t *testing.T) {
	agents := make(chan string, 3)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		agents <- r.UserAgent()
	}))
	defer srv.Close()
	c := NewClient(Config{URLs: URLConfig{APIBaseURL: srv.URL}})
	ctx := context.Background()
	c.doJSON(ctx, http.MethodGet, "/api/v1/deployments", nil, nil)
	c.FollowLogs(ctx, "search", "", 0, func(api.LogEvent) {})
	if s, err := c.StartBuild(ctx, StartBuildRequest{Package: "search", Tarball: []byte("x")}); err == nil {
		s.body.Close()
	}
	want := "flop/dev (" + runtime.GOOS + "/" + runtime.GOARCH + ")"
	for range 3 {
		if got := <-agents; got != want {
			t.Errorf("asked as %q, not %q", got, want)
		}
	}
}
