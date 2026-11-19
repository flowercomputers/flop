package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/flowercomputers/flop/api"
)

// sizingSpade acts out spade for one bog: a resize moves it (old box, then a
// new one at the new size), a grow grows its volume and then its filesystem.
// 402, 409 and 400 for the sizes spade would refuse.
type sizingSpade struct {
	mu   sync.Mutex
	d    api.Deployment
	data api.BogVolume
	// failMove: the move fails, the bog stays where it was (no capacity)
	failMove bool
	tooSoon  bool
}

func newSizingSpade(t *testing.T) (*sizingSpade, *Client) {
	s := &sizingSpade{
		d: api.Deployment{ID: "d1", Bog: "search", PeaterID: "i-old", MemoryGB: 8, CPU: "standard", Generation: 3,
			DesiredState: api.DesiredRunning, BoxManaged: true, BoxState: api.BoxReady,
			Observed: &api.Observation{State: api.BogStateRunning, Healthy: true, Generation: 3, At: time.Now(), DataSizeBytes: 20 << 30}},
		data: api.BogVolume{Bog: "search", VolumeID: "vol-1", SizeGB: 20, AttachedTo: "i-old"},
	}
	later := func(fn func()) {
		go func() {
			time.Sleep(60 * time.Millisecond)
			s.mu.Lock()
			defer s.mu.Unlock()
			fn()
		}()
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/deployments", func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		defer s.mu.Unlock()
		json.NewEncoder(w).Encode(api.ListDeploymentsResponse{Deployments: []api.Deployment{s.d}})
	})
	mux.HandleFunc("GET /api/v1/bogs/{name}", func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		defer s.mu.Unlock()
		d, v := s.d, s.data
		json.NewEncoder(w).Encode(api.Bog{Name: "search", Deployment: &d, Data: &v})
	})
	mux.HandleFunc("POST /api/v1/deployments/{id}/resize", func(w http.ResponseWriter, r *http.Request) {
		var req api.ResizeDeploymentRequest
		json.NewDecoder(r.Body).Decode(&req)
		switch req.MemoryGB {
		case 3:
			http.Error(w, "no size with 3 GB of memory and more CPU", http.StatusBadRequest)
			return
		case 32:
			http.Error(w, "a card on file is needed for any size but 2 GB standard", http.StatusPaymentRequired)
			return
		case 128:
			http.Error(w, "over the organization's limit: past its 64 GB limit", http.StatusConflict)
			return
		}
		s.mu.Lock()
		s.d.BoxState = api.BoxMoving
		resp := s.d
		s.mu.Unlock()
		later(func() {
			if s.failMove {
				s.d.BoxState, s.d.BoxError = api.BoxReady, "EC2 has no 16 GB more box (m7g.xlarge) in us-east-2a right now"
				s.d.Observed.At = time.Now()
				return
			}
			s.d.PeaterID, s.d.BoxState, s.d.MemoryGB, s.d.CPU = "i-new", api.BoxReady, req.MemoryGB, req.CPU
			s.d.Observed = &api.Observation{State: api.BogStateRunning, Healthy: true, Generation: 3, At: time.Now()}
		})
		w.WriteHeader(http.StatusAccepted)
		json.NewEncoder(w).Encode(resp)
	})
	mux.HandleFunc("PATCH /api/v1/bogs/{name}/data", func(w http.ResponseWriter, r *http.Request) {
		var req api.GrowBogDataRequest
		json.NewDecoder(r.Body).Decode(&req)
		if s.tooSoon {
			http.Error(w, "this bog's data changed size less than 6 hours ago; it may grow again after 2026-10-30T20:00:00Z", http.StatusConflict)
			return
		}
		s.mu.Lock()
		s.data.Pending = "grow"
		resp := s.data
		s.mu.Unlock()
		later(func() {
			s.data.SizeGB, s.data.Pending = req.SizeGB, ""
			later(func() { s.d.Observed.DataSizeBytes = int64(req.SizeGB) << 30 }) // then its filesystem
		})
		w.WriteHeader(http.StatusAccepted)
		json.NewEncoder(w).Encode(resp)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return s, NewClient(Config{URLs: URLConfig{APIBaseURL: srv.URL}, LocalConfig: LocalConfig{APIKey: "t", OrganizationID: "flower"}})
}

func TestResizeWaitsForTheMove(t *testing.T) {
	s, client := newSizingSpade(t)
	o, _, errs := testUI("", false, true)
	r, err := resize(context.Background(), o, client, s.d, 16, "more", false, 5*time.Second)
	if err != nil || !r.Waited || r.From != "8 GB standard" || r.To != "16 GB more" || r.Deployment.PeaterID != "i-new" {
		t.Fatalf("resize: %v %+v\n%s", err, r, errs)
	}
	if !strings.Contains(errs.String(), "down while its data moves") {
		t.Fatalf("does not say the bog is down: %s", errs)
	}
	// its own size: nothing asked
	if r, err := resize(context.Background(), o, client, r.Deployment, 16, "more", false, time.Second); err != nil || !r.Unchanged {
		t.Fatalf("to its own size: %v %+v", err, r)
	}
}

func TestResizeThatFailsSaysWhereTheBogIs(t *testing.T) {
	s, client := newSizingSpade(t)
	s.failMove = true
	o, _, _ := testUI("", false, true)
	_, err := resize(context.Background(), o, client, s.d, 16, "more", false, 5*time.Second)
	e := classify(err)
	if e.Code != "bog_failed" || !strings.Contains(e.Message, "still runs at 8 GB standard") || !strings.Contains(e.Message, "m7g.xlarge") {
		t.Fatalf("a failed move: %+v", e)
	}
}

func TestResizeRefusals(t *testing.T) {
	s, client := newSizingSpade(t)
	o, _, _ := testUI("", false, true)
	for memory, want := range map[int]struct {
		code string
		exit int
		hint string
	}{
		3:   {"invalid", 2, "flop sizes"},
		32:  {"payment_required", 9, ""},
		128: {"conflict", 5, ""},
	} {
		_, err := resize(context.Background(), o, client, s.d, memory, "more", false, time.Second)
		e := classify(err)
		if e.Code != want.code || exitCodes[e.Code] != want.exit || !strings.Contains(e.Hint, want.hint) {
			t.Errorf("%d GB: %+v", memory, e)
		}
	}
}

func TestGrowWaitsForTheFilesystem(t *testing.T) {
	s, client := newSizingSpade(t)
	o, _, _ := testUI("", false, true)
	b := api.Bog{Name: "search", Deployment: &s.d, Data: &s.data}
	g, err := grow(context.Background(), o, client, b, 30, false, 5*time.Second)
	if err != nil || !g.Waited || !g.Deployed || g.FromGB != 20 || g.ToGB != 30 {
		t.Fatalf("grow: %v %+v", err, g)
	}
	s.mu.Lock()
	fs := s.d.Observed.DataSizeBytes
	s.mu.Unlock()
	if fs != 30<<30 {
		t.Fatalf("returned before the filesystem grew: %d", fs)
	}
	if _, err := grow(context.Background(), o, client, api.Bog{Name: "search", Data: &api.BogVolume{SizeGB: 30}}, 30, false, time.Second); classify(err).Code != "usage" {
		t.Fatalf("to its own size: %v", err)
	}
	if _, err := grow(context.Background(), o, client, api.Bog{Name: "search"}, 30, false, time.Second); classify(err).Code != "not_found" {
		t.Fatalf("no data: %v", err)
	}
	s.tooSoon = true
	if _, err := grow(context.Background(), o, client, api.Bog{Name: "search", Data: &api.BogVolume{SizeGB: 30}}, 40, false, time.Second); classify(err).Code != "conflict" ||
		!strings.Contains(err.Error(), "may grow again after") {
		t.Fatalf("too soon: %v", err)
	}
}

func TestSizesSaysWhatMayBeChosen(t *testing.T) {
	l := sizeList{api.SizesResponse{
		Sizes: []api.Size{
			{MemoryGB: 2, CPU: "standard", VCPUs: 2, Burstable: true, PricePerHour: 227, Default: true, Allowed: true},
			{MemoryGB: 16, CPU: "more", VCPUs: 4, PricePerHour: 1691, NotAllowed: "a card on file is needed for any size but 2 GB standard"},
		},
		Data:   api.DataSizes{MinGB: 10, MaxGB: 1000, DefaultGB: 10, PricePerGBMonth: 800},
		Limits: api.Limits{MemoryGB: 64, MemoryInUseGB: 8, DataGB: 500, DataKeptGB: 20},
	}}
	var out bytes.Buffer
	l.human(&out)
	for _, line := range strings.Split(out.String(), "\n") {
		if strings.HasPrefix(line, "16 GB") && !strings.Contains(line, "more") {
			t.Errorf("16 GB row without its CPU: %q", line)
		}
	}
	for _, want := range []string{"2 burstable", "$0.0227", "$16.57", "the default", "not now: a card on file",
		"10 to 1000 GB", "$0.08 per GB", "8 of 64 GB of memory", "20 of 500 GB of data"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("missing %q:\n%s", want, out.String())
		}
	}
}

// a deployed bog asked for at another size: after the rollout it moves, and
// its data grows; asked for its own size, nothing more happens
func TestARedeployAtAnotherSizeMovesAndGrows(t *testing.T) {
	s, client := newSizingSpade(t)
	o, _, errs := testUI("", false, true)
	out := deployed{Bog: "search", Deployment: s.d, Waited: true}
	if err := resizeAfterRollout(context.Background(), o, client, &out, deployOptions{Timeout: 5 * time.Second,
		Size: sizing{MemoryGB: 8, CPU: "standard", DataGB: 10}}); err != nil || out.Resized != nil || out.Grown != nil {
		t.Fatalf("its own size, less data: %v %+v", err, out)
	}
	if err := resizeAfterRollout(context.Background(), o, client, &out, deployOptions{Timeout: 5 * time.Second,
		Size: sizing{MemoryGB: 16, CPU: "more", DataGB: 30}}); err != nil {
		t.Fatal(err)
	}
	if out.Resized == nil || out.Resized.To != "16 GB more" || out.Grown == nil || out.Grown.ToGB != 30 || out.Deployment.MemoryGB != 16 {
		t.Fatalf("redeploy at 16 GB more with 30 GB: %+v", out)
	}
	if !strings.Contains(errs.String(), "Moving search from 8 GB standard to 16 GB more") || !strings.Contains(errs.String(), "Growing the data of search from 20 to 30 GB") {
		t.Fatalf("does not say both steps:\n%s", errs)
	}
}

func TestStatusShowsTheMachineDataAndAdvice(t *testing.T) {
	b := bogStatus{Bog: api.Bog{Name: "search", Status: "running",
		Deployment: &api.Deployment{ID: "d1", PeaterID: "i-a", MemoryGB: 16, CPU: "more", Generation: 1,
			Observed: &api.Observation{State: api.BogStateRunning, Healthy: true, Generation: 1, DataUsedBytes: 12, DataSizeBytes: 100}},
		Data:   &api.BogVolume{VolumeID: "vol-1", SizeGB: 30, AttachedTo: "i-a"},
		Advice: []string{"ran out of memory 2 time(s) on its 16 GB box"}},
		Machine: &api.Size{MemoryGB: 16, CPU: "more", VCPUs: 4, PricePerHour: 1691}}
	var out bytes.Buffer
	b.human(&out)
	for _, want := range []string{"16 GB more (4 vCPUs), $0.1691 an hour", "30 GB, 12% used", "advice: ran out of memory 2 time(s)"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("missing %q:\n%s", want, out.String())
		}
	}
}

func TestStatusShowsASleepingSandboxBogOnFly(t *testing.T) {
	b := bogStatus{Bog: api.Bog{Name: "try", Status: "asleep",
		Deployment: &api.Deployment{ID: "d1", Tier: api.TierSandbox, MemoryGB: 2, CPU: "shared", Generation: 1,
			Observed: &api.Observation{State: api.BogStateAsleep, Generation: 1, Region: "ord"}}}}
	var out bytes.Buffer
	b.human(&out)
	for _, want := range []string{"try: asleep, wakes on its next request", "Fly, ord"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("missing %q:\n%s", want, out.String())
		}
	}
}

func TestADeployBuildsForTheBogsTier(t *testing.T) {
	deployed := []api.Deployment{{ID: "d1", Bog: "try", Tier: api.TierSandbox, DesiredState: api.DesiredRunning},
		{ID: "d2", Bog: "big", Tier: api.TierStandard, DesiredState: api.DesiredRunning}}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/deployments", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(api.ListDeploymentsResponse{Deployments: deployed})
	})
	sandboxDefault := true
	mux.HandleFunc("GET /api/v1/sizes", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(api.SizesResponse{Sizes: []api.Size{
			{MemoryGB: 2, CPU: "shared", Tier: api.TierSandbox, Default: sandboxDefault},
			{MemoryGB: 2, CPU: "standard", Tier: api.TierStandard, Default: !sandboxDefault}}})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	client := NewClient(Config{URLs: URLConfig{APIBaseURL: srv.URL}, LocalConfig: LocalConfig{APIKey: "t", OrganizationID: "flower"}})
	for _, c := range []struct {
		name, cpu string
		memory    int
		want      string
	}{
		{"try", "", 0, api.ArchAMD64},         // in the sandbox: its rollout runs there
		{"try", "standard", 0, api.ArchAMD64}, // still: the move after it builds for EC2
		{"big", "shared", 0, api.ArchARM64},   // on EC2, the same
		{"new", "shared", 0, api.ArchAMD64},   // a new one, at a shared size
		{"new", "standard", 0, api.ArchARM64},
		{"new", "", 0, api.ArchAMD64},  // no size: spade's default, the sandbox's
		{"new", "", 2, api.ArchAMD64},  // a memory the sandbox has
		{"new", "", 16, api.ArchARM64}, // more than it has: a box of its own
	} {
		got, err := archFor(context.Background(), client, deployOptions{Name: c.name, Size: sizing{CPU: c.cpu, MemoryGB: c.memory}})
		if err != nil || got != c.want {
			t.Errorf("%s at %d GB %q: %s %v, want %s", c.name, c.memory, c.cpu, got, err, c.want)
		}
	}
	sandboxDefault = false // a spade without the sandbox
	if got, _ := archFor(context.Background(), client, deployOptions{Name: "new"}); got != api.ArchARM64 {
		t.Errorf("no sandbox, no size: %s", got)
	}
}
