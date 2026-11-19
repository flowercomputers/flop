package main

import (
	"strings"
	"testing"
	"time"

	"github.com/flowercomputers/flop/api"
)

func TestCopiesTable(t *testing.T) {
	var out strings.Builder
	copyList{Bog: "try", Copies: []api.BogCopy{
		{ID: "20261119T040000Z", State: "done", TakenAt: time.Now().Add(-3 * time.Hour), SizeBytes: 5 << 20},
		{ID: "20261118T040000Z", State: "failed", TakenAt: time.Now().Add(-27 * time.Hour), Error: "the helper exited 1"},
	}}.human(&out)
	for _, want := range []string{"20261119T040000Z", "3h ago", "done", "5.0 MB", "failed: the helper exited 1", "flop bog restore try --snapshot <copy>"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("table lacks %q:\n%s", want, out.String())
		}
	}
	out.Reset()
	copyList{Bog: "big", Copies: []api.BogCopy{}}.human(&out)
	if !strings.Contains(out.String(), "No copies of big's data") {
		t.Fatalf("no copies: %s", out.String())
	}
	// a sandbox bog's restore names the copy; a box's, its snapshot and zone
	out.Reset()
	restored{Bog: "try", from: "20261119T040000Z", RestoreBogDataResponse: api.RestoreBogDataResponse{Volume: api.BogVolume{State: "fly"}}}.human(&out)
	if got := out.String(); got != "Bog try will start from its copy 20261119T040000Z at its next deploy.\n" {
		t.Fatalf("a sandbox bog's restore: %q", got)
	}
}

func TestVolumesTable(t *testing.T) {
	var out strings.Builder
	volumeList{Volumes: []api.BogVolume{
		{Bog: "search", VolumeID: "vol-1", SizeGB: 10, AvailabilityZone: "us-east-2a", AttachedTo: "i-a", CreatedAt: time.Now()},
		{Bog: "notes", VolumeID: "vol-2", SizeGB: 10, AvailabilityZone: "us-east-2a", CreatedAt: time.Now()},
		{Bog: "old", RestoredFrom: "snap-9", SizeGB: 20, CreatedAt: time.Now()},
		{Bog: "try", VolumeID: "vol_1", SizeGB: 1, State: "fly", CreatedAt: time.Now()},
	}}.human(&out)
	for _, want := range []string{"attached to i-a", "kept (detached)", "restore from snap-9 at next deploy", "in the sandbox", "4 volumes, 41 GB kept"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("table lacks %q:\n%s", want, out.String())
		}
	}
}
