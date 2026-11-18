package main

import (
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
