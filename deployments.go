package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/flowercomputers/flop/api"
)

// loggedInClient is the precondition every organization-scoped command shares.
func loggedInClient() (*Client, error) {
	cfg, err := LoadConfig()
	if err != nil {
		return nil, err
	}
	if cfg.LocalConfig.APIKey == "" {
		return nil, errNotLoggedIn
	}
	if cfg.LocalConfig.OrganizationID == "" {
		return nil, errNoOrganization
	}
	return NewClient(cfg), nil
}

// DeployTo places a bog image on a specific box.
func DeployTo(ctx context.Context, box, image, name string) (deploymentChange, error) {
	client, err := loggedInClient()
	if err != nil {
		return deploymentChange{}, err
	}
	if box == "" || image == "" || name == "" {
		return deploymentChange{}, usage("deploy-to needs a box, an image and a name", "flop bog deploy-to <instance-id> --image <ref> --name <bog>")
	}
	d, err := client.CreateDeployment(ctx, api.CreateDeploymentRequest{Bog: name, ImageRef: image, PeaterID: box})
	if err != nil {
		return deploymentChange{}, fmt.Errorf("deploy: %w", err)
	}
	return deploymentChange{Action: "deploy", Deployment: d}, nil
}

// deploymentList is GET /deployments, as `flop bog deployments` shows it
type deploymentList struct {
	Deployments []api.Deployment `json:"deployments"`
}

func ListDeployments(ctx context.Context) (deploymentList, error) {
	client, err := loggedInClient()
	if err != nil {
		return deploymentList{}, err
	}
	list, err := client.ListDeployments(ctx)
	if err != nil {
		return deploymentList{}, fmt.Errorf("list deployments: %w", err)
	}
	return deploymentList{Deployments: list}, nil
}

func (l deploymentList) human(w io.Writer) {
	if len(l.Deployments) == 0 {
		fmt.Fprintln(w, "No deployments. Deploy one from a project with `flop bog deploy`.")
		return
	}
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tBOG\tBOX\tIMAGE\tGEN\tSTATE\tHEALTH\tVERSION\tUPDATED\tURL")
	for _, d := range l.Deployments {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			shortID(d.ID), d.Bog, box(d), imageTag(d.ImageRef),
			deploymentGen(d), deploymentState(d), deploymentHealth(d), orDash(d.BoxVersion), ago(d.UpdatedAt), orDash(d.URL))
	}
	tw.Flush()
	// errors under the table: a stuck rollout ("1→2" that never settles)
	// must say why, not just look slow
	for _, d := range l.Deployments {
		for _, e := range deploymentErrors(d) {
			fmt.Fprintf(w, "\n%s: %s\n", shortID(d.ID), e)
		}
	}
}

// deploymentErrors is what stands in a deployment's way, as spade and its box
// report it
func deploymentErrors(d api.Deployment) []string {
	var out []string
	if d.ControlError != "" {
		out = append(out, d.ControlError)
	}
	if d.BoxError != "" {
		out = append(out, d.BoxError)
	}
	if d.Observed != nil && d.Observed.Error != "" {
		out = append(out, d.Observed.Error)
	}
	return out
}

// deploymentChange is what rollout, undeploy, rebox and deploy-to did: the
// deployment as spade has it now
type deploymentChange struct {
	Action     string         `json:"action"` // deploy, rollout, undeploy or rebox
	Deployment api.Deployment `json:"deployment"`
}

func (c deploymentChange) human(w io.Writer) {
	d := c.Deployment
	switch c.Action {
	case "deploy":
		fmt.Fprintf(w, "Deploying %s (%s) to %s at generation %d; follow with `flop bog wait %s`\n", d.Bog, shortID(d.ID), orDash(d.PeaterID), d.Generation, d.Bog)
	case "rollout":
		fmt.Fprintf(w, "Rolling %s (%s) out to generation %d (%s); follow with `flop bog wait %s`\n", d.Bog, shortID(d.ID), d.Generation, imageTag(d.ImageRef), d.Bog)
	case "undeploy":
		fmt.Fprintf(w, "Undeploying %s (%s) from %s: its box drains and stops it; its data is kept\n", d.Bog, shortID(d.ID), orDash(d.PeaterID))
	case "rebox":
		fmt.Fprintf(w, "Moving %s (%s) off %s: spade launches a new box in the same zone, then the bog drains, its data moves, and the old box is terminated.\n"+
			"The bog is down from the drain until it runs on the new box; follow with `flop bog wait %s`\n", d.Bog, shortID(d.ID), orDash(d.PeaterID), d.Bog)
	}
}

func Rollout(ctx context.Context, target, image string) (deploymentChange, error) {
	client, err := loggedInClient()
	if err != nil {
		return deploymentChange{}, err
	}
	if target == "" || image == "" {
		return deploymentChange{}, usage("rollout needs a bog and an image", "flop bog rollout <bog> --image <ref>")
	}
	d, err := resolveDeployment(ctx, client, target)
	if err != nil {
		return deploymentChange{}, err
	}
	updated, err := client.UpdateDeployment(ctx, d.ID, api.UpdateDeploymentRequest{ImageRef: image})
	if err != nil {
		return deploymentChange{}, fmt.Errorf("rollout: %w", err)
	}
	return deploymentChange{Action: "rollout", Deployment: updated}, nil
}

func Undeploy(ctx context.Context, target string) (deploymentChange, error) {
	client, err := loggedInClient()
	if err != nil {
		return deploymentChange{}, err
	}
	if target == "" {
		return deploymentChange{}, usage("undeploy needs a bog", "flop bog undeploy <bog>")
	}
	d, err := resolveDeployment(ctx, client, target)
	if err != nil {
		return deploymentChange{}, err
	}
	updated, err := client.DeleteDeployment(ctx, d.ID)
	if err != nil {
		return deploymentChange{}, fmt.Errorf("undeploy: %w", err)
	}
	return deploymentChange{Action: "undeploy", Deployment: updated}, nil
}

// Rebox moves a deployment to a fresh box spade launches (for example to
// pick up a new AMI). The bog is down while it moves.
func Rebox(ctx context.Context, target string) (deploymentChange, error) {
	client, err := loggedInClient()
	if err != nil {
		return deploymentChange{}, err
	}
	if target == "" {
		return deploymentChange{}, usage("rebox needs a bog", "flop bog rebox <bog>")
	}
	d, err := resolveDeployment(ctx, client, target)
	if err != nil {
		return deploymentChange{}, err
	}
	updated, err := client.ReboxDeployment(ctx, d.ID)
	if err != nil {
		return deploymentChange{}, fmt.Errorf("rebox: %w", err)
	}
	return deploymentChange{Action: "rebox", Deployment: updated}, nil
}

// destroyed is what `flop bog destroy` did
type destroyed struct {
	Bog      string `json:"bog"`
	VolumeID string `json:"volume_id,omitempty"`
	// Done: the volume is deleted. False: spade deletes it once it detaches
	// (see `flop bog volumes`)
	Done bool `json:"done"`
}

func (d destroyed) human(w io.Writer) {
	if d.Done {
		fmt.Fprintf(w, "Deleted the data of bog %s (volume %s)\n", d.Bog, orDash(d.VolumeID))
		return
	}
	fmt.Fprintf(w, "Spade deletes the data of bog %s (volume %s) once it has detached; check with `flop bog volumes`\n", d.Bog, orDash(d.VolumeID))
}

// destroyBogData permanently deletes a bog's kept data. The bog name must
// come back, as --confirm or typed at a prompt (only when flop may ask).
func destroyBogData(ctx context.Context, client *Client, o *ui, bog, confirm string) (destroyed, error) {
	if bog == "" {
		return destroyed{}, usage("destroy needs a bog", "flop bog destroy <bog> --confirm <bog>")
	}
	// spade refuses too; checking first saves typing the name for nothing
	list, err := client.ListDeployments(ctx)
	if err != nil {
		return destroyed{}, fmt.Errorf("list deployments: %w", err)
	}
	for _, d := range list {
		if d.Bog == bog {
			return destroyed{}, fail("conflict", fmt.Sprintf("bog %s is still deployed (%s)", bog, shortID(d.ID)),
				fmt.Sprintf("run `flop bog undeploy %s` first, and wait for it to go", bog))
		}
	}
	switch {
	case confirm == bog:
	case confirm != "":
		return destroyed{}, usage(fmt.Sprintf("--confirm %s does not match the bog %s; nothing was deleted", confirm, bog), "")
	case !o.canAsk:
		return destroyed{}, usage("destroying data needs the bog's name typed back; nothing was deleted",
			fmt.Sprintf("flop bog destroy %s --confirm %s", bog, bog))
	default:
		fmt.Fprintf(o.err, "This PERMANENTLY deletes the data of bog %s. There is no undo.\nType the bog name to confirm: ", bog)
		typed, _ := bufio.NewReader(o.in).ReadString('\n')
		if strings.TrimSpace(typed) != bog {
			return destroyed{}, usage("the name did not match; nothing was deleted", "")
		}
	}
	v, err := client.DestroyBogData(ctx, bog)
	if err != nil {
		return destroyed{}, fmt.Errorf("destroy: %w", err)
	}
	// spade deletes the volume once it is detached: wait for that
	for deadline := time.Now().Add(destroyWait); ; {
		list, err := client.ListBogData(ctx)
		if err != nil {
			return destroyed{}, fmt.Errorf("checking on the destroy: %w", err)
		}
		var left *api.BogVolume
		for i := range list {
			if list[i].Bog == bog {
				left = &list[i]
			}
		}
		switch {
		case left == nil:
			return destroyed{Bog: bog, VolumeID: v.VolumeID, Done: true}, nil
		case left.Pending != "destroy":
			return destroyed{}, fail("conflict", fmt.Sprintf("the destroy of bog %s was called off (it was deployed again, or a restore was asked for); its data is kept", bog), "")
		case time.Now().After(deadline):
			return destroyed{Bog: bog, VolumeID: v.VolumeID}, nil
		}
		select {
		case <-ctx.Done():
			return destroyed{}, ctx.Err()
		case <-time.After(destroyPoll):
		}
	}
}

// how long destroy waits for spade to delete the volume, checking every
// destroyPoll
var destroyWait, destroyPoll = 2 * time.Minute, 2 * time.Second

// resolveDeployment finds the deployment a command means: a bog's (its live
// one, or its only one), or any unambiguous id prefix, like git does for
// commit hashes, so the short ids `flop bog deployments` prints work as-is.
func resolveDeployment(ctx context.Context, client *Client, target string) (api.Deployment, error) {
	list, err := client.ListDeployments(ctx)
	if err != nil {
		return api.Deployment{}, fmt.Errorf("list deployments: %w", err)
	}
	var ofBog []api.Deployment
	for _, d := range list {
		if d.Bog == target {
			ofBog = append(ofBog, d)
		}
	}
	for _, d := range ofBog {
		if d.DesiredState == api.DesiredRunning {
			return d, nil
		}
	}
	if len(ofBog) == 1 {
		return ofBog[0], nil
	}
	var matches []api.Deployment
	for _, d := range list {
		if strings.HasPrefix(d.ID, target) {
			matches = append(matches, d)
		}
	}
	switch len(matches) {
	case 0:
		return api.Deployment{}, fail("not_found", fmt.Sprintf("no bog or deployment %q", target), "see `flop bog deployments`")
	case 1:
		return matches[0], nil
	default:
		ids := make([]string, len(matches))
		for i, d := range matches {
			ids[i] = d.ID
		}
		return api.Deployment{}, usage(fmt.Sprintf("%q matches %d deployments: %s", target, len(matches), strings.Join(ids, ", ")), "use the bog's name, or more of the id")
	}
}

// deploymentGen shows the desired generation, and the observed one when the
// box has not caught up yet: "1→2" means mid-rollout.
func deploymentGen(d api.Deployment) string {
	if d.Observed == nil || d.Observed.Generation == d.Generation {
		return fmt.Sprintf("%d", d.Generation)
	}
	return fmt.Sprintf("%d→%d", d.Observed.Generation, d.Generation)
}

func deploymentState(d api.Deployment) string { return d.Status() }

func deploymentHealth(d api.Deployment) string {
	switch {
	case d.Observed == nil, d.Observed.State == api.BogStateUnknown:
		return "-"
	case d.Observed.State == api.BogStateAsleep:
		return "wakes on its next request"
	case d.Observed.Healthy:
		return "healthy"
	default:
		return "unhealthy"
	}
}

// imageTag trims a registry ref to its last path element: the repo name and
// tag are what a person scans for.
func imageTag(ref string) string {
	if i := strings.LastIndex(ref, "/"); i >= 0 {
		return ref[i+1:]
	}
	return ref
}

func ago(t time.Time) string {
	switch d := time.Since(t); {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
}
