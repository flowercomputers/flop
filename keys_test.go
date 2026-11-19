package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/flowercomputers/flop/api"
)

func TestKeysTable(t *testing.T) {
	now := time.Now()
	past, future := now.Add(-time.Hour), now.Add(time.Hour)
	var out strings.Builder
	keyList{Bog: "search", now: now, Keys: []api.BogKey{
		{ID: "k1", Name: "ci", CreatedAt: now, LastUsedAt: &past, Requests: 1234},
		{ID: "k2", Name: "demo", CreatedAt: now, ExpiresAt: &future},
		{ID: "k3", Name: "old", CreatedAt: now, ExpiresAt: &past},
	}}.human(&out)
	for _, want := range []string{"k1", "never", "k2", "k3", "(expired)", "LAST USED", "1234", "1h ago"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("table lacks %q:\n%s", want, out.String())
		}
	}
	if strings.Count(out.String(), "(expired)") != 1 {
		t.Fatalf("only k3 is expired:\n%s", out.String())
	}
}

// every curl makes a key: when spade holds key-making back, the hint is the
// way to call a bog many times
func TestCurlHeldBackSaysToMakeOneKey(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "too many requests: try again in 2 minutes", http.StatusTooManyRequests)
	}))
	defer srv.Close()
	client := NewClient(Config{URLs: URLConfig{APIBaseURL: srv.URL}})
	_, err := curlBog(context.Background(), client, http.DefaultClient, "search", "/", "", "")
	e := classify(err)
	if e.Code != "unavailable" || e.exit() != 8 || !strings.Contains(e.Message, "try again in 2 minutes") ||
		!strings.Contains(e.Hint, "flop bog keys create search") {
		t.Fatalf("%+v", e)
	}
}

// a key takes a moment to reach the routers: curl asks again while a router
// refuses its new key, and takes a bog's own 401 as the answer
func TestCurlWaitsForItsKeyToReachTheRouters(t *testing.T) {
	curlKeyPoll = time.Millisecond
	for _, c := range []struct {
		name            string
		routerRefusals  int // before the router has the key
		bogStatus, want int
		requests        int
	}{
		{"the router has the key on the third request", 2, 200, 200, 3},
		{"the bog itself answers 401", 0, 401, 401, 1},
	} {
		requests := 0
		mux := http.NewServeMux()
		srv := httptest.NewServer(mux)
		defer srv.Close()
		mux.HandleFunc("POST /api/v1/bogs/search/keys", func(w http.ResponseWriter, r *http.Request) {
			json.NewEncoder(w).Encode(api.CreateBogKeyResponse{Key: api.BogKey{ID: "k1"}, Secret: "bog_k1_s", URL: srv.URL + "/bog"})
		})
		mux.HandleFunc("DELETE /api/v1/bogs/search/keys/k1", func(w http.ResponseWriter, r *http.Request) {})
		mux.HandleFunc("/bog/", func(w http.ResponseWriter, r *http.Request) {
			if requests++; requests <= c.routerRefusals {
				w.Header().Set("WWW-Authenticate", `Bearer realm="bog"`)
				http.Error(w, "this API key is not valid for this bog (unknown, revoked or expired)", http.StatusUnauthorized)
				return
			}
			w.WriteHeader(c.bogStatus)
		})
		got, err := curlBog(context.Background(), NewClient(Config{URLs: URLConfig{APIBaseURL: srv.URL}}), http.DefaultClient, "search", "/x", "", "")
		if err != nil || got.Status != c.want || requests != c.requests {
			t.Fatalf("%s: status %d after %d requests (%v)", c.name, got.Status, requests, err)
		}
	}
}
