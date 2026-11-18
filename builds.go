package main

import (
	"context"
	"fmt"
	"io"
	"text/tabwriter"
	"time"

	"github.com/flowercomputers/flop/api"
)

// buildList is the organization's builds, newest first
type buildList struct {
	Builds []api.Build `json:"builds"`
}

func ListBuilds(ctx context.Context, pkg string) (buildList, error) {
	client, err := loggedInClient()
	if err != nil {
		return buildList{}, err
	}
	builds, err := client.ListBuilds(ctx, pkg)
	if err != nil {
		return buildList{}, fmt.Errorf("list builds: %w", err)
	}
	if builds == nil {
		builds = []api.Build{}
	}
	return buildList{Builds: builds}, nil
}

func (l buildList) human(w io.Writer) {
	if len(l.Builds) == 0 {
		fmt.Fprintln(w, "No builds yet. Run `flop bog build` in a project to start one.")
		return
	}
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tPACKAGE\tSTATUS\tVERSION\tDURATION\tAGE")
	for _, b := range l.Builds {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n",
			shortID(b.ID), b.Package, b.Status, buildVersion(b), buildDuration(b), ago(b.CreatedAt))
	}
	tw.Flush()
}

func shortID(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}

func buildVersion(b api.Build) string {
	if b.GitSHA == "" {
		return "no-git"
	}
	if b.Dirty {
		return b.GitSHA + "-dirty"
	}
	return b.GitSHA
}

func buildDuration(b api.Build) string {
	if b.StartedAt == nil || b.FinishedAt == nil {
		return "-"
	}
	return b.FinishedAt.Sub(*b.StartedAt).Round(time.Second).String()
}
