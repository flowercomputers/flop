package main

import (
	"context"
	"fmt"
	"io"
	"text/tabwriter"

	"github.com/flowercomputers/flop/api"
)

// volumeList is the organization's kept data volumes. Kept volumes cost money
// whether or not anything runs, so they must be easy to see.
type volumeList struct {
	Volumes []api.BogVolume `json:"volumes"`
}

func Volumes(ctx context.Context) (volumeList, error) {
	client, err := loggedInClient()
	if err != nil {
		return volumeList{}, err
	}
	list, err := client.ListBogData(ctx)
	if err != nil {
		return volumeList{}, fmt.Errorf("list volumes: %w", err)
	}
	if list == nil {
		list = []api.BogVolume{}
	}
	return volumeList{Volumes: list}, nil
}

func (l volumeList) human(out io.Writer) {
	if len(l.Volumes) == 0 {
		fmt.Fprintln(out, "No kept data volumes.")
		return
	}
	tw := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "BOG\tVOLUME\tSIZE\tZONE\tSTATE\tCREATED")
	total := 0
	for _, v := range l.Volumes {
		total += v.SizeGB
		fmt.Fprintf(tw, "%s\t%s\t%d GB\t%s\t%s\t%s\n",
			v.Bog, orDash(v.VolumeID), v.SizeGB, orDash(v.AvailabilityZone), volumeState(v), ago(v.CreatedAt))
	}
	tw.Flush()
	fmt.Fprintf(out, "\n%d volumes, %d GB kept\n", len(l.Volumes), total)
}

func volumeState(v api.BogVolume) string {
	switch {
	case v.VolumeID == "" && v.RestoredFrom != "":
		return "restore from " + v.RestoredFrom + " at next deploy"
	case v.VolumeID == "":
		return "creating"
	case v.AttachedTo != "":
		return "attached to " + v.AttachedTo
	default:
		return "kept (detached)"
	}
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

// restored is a restore asked for: the bog starts from the snapshot at its
// next deploy
type restored struct {
	Bog string `json:"bog"`
	api.RestoreBogDataResponse
}

func (r restored) human(w io.Writer) {
	fmt.Fprintf(w, "Bog %s will start from %s at its next deploy, in the zone of the box it is deployed to.\n", r.Bog, r.Volume.RestoredFrom)
	if r.ReplacedVolumeID != "" {
		fmt.Fprintf(w, "Its previous volume %s is kept, unattached. Delete it once you have checked the restore.\n", r.ReplacedVolumeID)
	}
}

// Restore points a bog at a new volume made from one of its snapshots.
func Restore(ctx context.Context, bog, snapshotID string) (restored, error) {
	client, err := loggedInClient()
	if err != nil {
		return restored{}, err
	}
	if bog == "" || snapshotID == "" {
		return restored{}, usage("restore needs a bog and a snapshot", "flop bog restore <bog> --snapshot <snap-id>")
	}
	out, err := client.RestoreBogData(ctx, bog, snapshotID)
	if err != nil {
		return restored{}, fmt.Errorf("restore: %w", err)
	}
	return restored{Bog: bog, RestoreBogDataResponse: out}, nil
}
