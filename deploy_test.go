package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/flowercomputers/flop/api"
)

func TestBogSlug(t *testing.T) {
	for in, want := range map[string]string{"search-server": "search-server", "My_Bog": "my-bog", "-x-": "x"} {
		if got := bogSlug(in); got != want {
			t.Errorf("bogSlug(%q) = %q, want %q", in, got, want)
		}
	}
}

// a sandbox bog asleep at the generation waited for is waiting for a
// request: the wait says so, and how to wake it
func TestWaitingOnASleepingBogSaysHowToWakeIt(t *testing.T) {
	d := api.Deployment{ID: "d1", Bog: "try", Tier: api.TierSandbox, Generation: 2, DesiredState: api.DesiredRunning,
		Observed: &api.Observation{State: api.BogStateAsleep, Generation: 2, At: time.Now().Add(time.Hour)}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(api.ListDeploymentsResponse{Deployments: []api.Deployment{d}})
	}))
	defer srv.Close()
	client := NewClient(Config{URLs: URLConfig{APIBaseURL: srv.URL}})
	var said bytes.Buffer
	_, err := waitForDeployment(context.Background(), client, "d1", 2, time.Now(), time.Millisecond, 20*time.Millisecond, &said)
	e := classify(err)
	if e.Code != "timeout" || !strings.Contains(e.Message, "try is asleep at generation 2") || !strings.Contains(e.Hint, "`flop bog curl try /healthz`") ||
		!strings.Contains(said.String(), "asleep (it wakes on its next request)") {
		t.Fatalf("%+v\n%s", e, said.String())
	}
}
