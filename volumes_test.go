package main

import (
	"strings"
	"testing"
	"time"

	"github.com/flowercomputers/flop/api"
)

func TestVolumesTable(t *testing.T) {
	var out strings.Builder
	volumeList{Volumes: []api.BogVolume{
		{Bog: "search", VolumeID: "vol-1", SizeGB: 10, AvailabilityZone: "us-east-2a", AttachedTo: "i-a", CreatedAt: time.Now()},
		{Bog: "notes", VolumeID: "vol-2", SizeGB: 10, AvailabilityZone: "us-east-2a", CreatedAt: time.Now()},
		{Bog: "old", RestoredFrom: "snap-9", SizeGB: 20, CreatedAt: time.Now()},
	}}.human(&out)
	for _, want := range []string{"attached to i-a", "kept (detached)", "restore from snap-9 at next deploy", "3 volumes, 40 GB kept"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("table lacks %q:\n%s", want, out.String())
		}
	}
}
