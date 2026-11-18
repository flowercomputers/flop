package main

import "testing"

func TestSparklineScalesToThePeak(t *testing.T) {
	if got := sparkline([]float64{0, 1, 2, 4}, 40); got != "▁▃▅█" {
		t.Fatalf("%q", got)
	}
	if got := sparkline([]float64{0, 0, 0}, 40); got != "▁▁▁" {
		t.Fatalf("all zero: %q", got)
	}
	if got := []rune(sparkline(make([]float64, 100), 40)); len(got) != 40 {
		t.Fatalf("width %d", len(got))
	}
}

func TestUnitsReadLikeAPerson(t *testing.T) {
	for _, c := range []struct {
		v    float64
		unit string
		want string
	}{
		{0.4234, "per_second", "0.42/s"}, {0.012, "seconds", "12 ms"}, {1.5, "seconds", "1.50 s"},
		{2 << 20, "bytes", "2.0 MB"}, {512, "bytes", "512 B"}, {0.0213, "cores", "0.021 cores"},
	} {
		if got := format(c.v, c.unit); got != c.want {
			t.Errorf("%v %s: %q, want %q", c.v, c.unit, got, c.want)
		}
	}
}

func TestFriendlyDurations(t *testing.T) {
	for in, want := range map[string]string{"30m0s": "30m", "1h0m0s": "1h", "6m0s": "6m", "15s": "15s", "1h30m0s": "1h30m", "10m0s": "10m", "20h0m0s": "20h"} {
		if got := friendlyDuration(in); got != want {
			t.Errorf("%s: %q, want %q", in, got, want)
		}
	}
}
