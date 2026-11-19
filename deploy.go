package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/flowercomputers/flop/api"
)

type deployOptions struct {
	Package   string
	Fresh     bool
	Name      string // bog name; defaults to the package name
	Box       string // instance to place a NEW deployment on (manual); empty: spade launches one
	BuildOnly bool
	NoWait    bool
	Arch      string // the build's architecture: arm64 ("") or amd64
	Timeout   time.Duration
	Size      sizing
}

// sizing is the machine and data a deploy asks for; zero values: what runs
// (or spade's defaults, for a new bog)
type sizing struct {
	MemoryGB int
	CPU      string
	DataGB   int
}

// deployed is what `flop bog deploy` did
type deployed struct {
	Bog     string `json:"bog"`
	Created bool   `json:"created"` // a new deployment; false: a rollout of the bog's
	Build   built  `json:"build"`
	// Waited: flop followed the deployment until its box ran the image,
	// healthy. False with --no-wait: follow it with `flop bog wait`.
	Waited     bool           `json:"waited"`
	Deployment api.Deployment `json:"deployment"`
	URL        string         `json:"url,omitempty"`
	// a deployed bog asked for at another size: after the rollout, the move
	// to it, and the grow of its data
	Resized *resized `json:"resized,omitempty"`
	Grown   *grown   `json:"grown,omitempty"`
}

func (d deployed) human(w io.Writer) {
	if !d.Waited {
		fmt.Fprintf(w, "Deploying %s at generation %d; follow with `flop bog wait %s`\n", d.Bog, d.Deployment.Generation, d.Bog)
	} else {
		fmt.Fprintf(w, "%s is running and healthy at generation %d\n", d.Bog, d.Deployment.Generation)
	}
	if d.Resized != nil {
		d.Resized.human(w)
	}
	if d.Grown != nil {
		d.Grown.human(w)
	}
	if d.URL != "" {
		fmt.Fprintf(w, "%s\n", d.URL)
	}
}

// DeployBog builds the current project and runs the result: a rollout if a
// deployment of this bog already exists, otherwise a new deployment on Box,
// or on a box spade launches.
// The image ref goes straight from the build to the deployment, so nobody
// copies (or mistypes) it.
func DeployBog(ctx context.Context, o *ui, opts deployOptions) (result, error) {
	client, err := loggedInClient()
	if err != nil {
		return nil, err
	}
	if opts.Arch != "" && opts.Arch != api.ArchARM64 && opts.Arch != api.ArchAMD64 {
		return nil, usage("--arch is arm64 or amd64", "")
	}
	arch := opts.Arch
	if arch == "" && !opts.BuildOnly {
		if arch, err = archFor(ctx, client, opts); err != nil {
			return nil, err
		}
	}
	b, err := buildImage(ctx, o, client, opts.Package, opts.Fresh, arch)
	if err != nil {
		return nil, err
	}
	if opts.BuildOnly {
		return b, nil
	}

	name := opts.Name
	if name == "" {
		name = bogSlug(b.Package)
	}
	started := time.Now()
	d, created, err := deployImage(ctx, client, name, opts.Box, b.Image, opts.Size)
	if err != nil {
		return nil, err
	}
	switch {
	case created && d.Tier == api.TierSandbox:
		o.say("Deploying %s to the sandbox (%s): it sleeps when idle and wakes on its next request", d.Bog, shortID(d.ID))
	case created && d.PeaterID == "":
		o.say("Deploying %s to a new box (%s)", d.Bog, shortID(d.ID))
	case created:
		o.say("Deploying %s to %s (%s)", d.Bog, d.PeaterID, shortID(d.ID))
	default:
		o.say("Rolling %s out to generation %d on %s", d.Bog, d.Generation, box(d))
	}
	out := deployed{Bog: d.Bog, Created: created, Build: b, Deployment: d, URL: d.URL}
	if !opts.NoWait {
		if out.Deployment, err = waitForDeployment(ctx, client, d.ID, d.Generation, started, 2*time.Second, opts.Timeout, o.err); err != nil {
			return nil, err
		}
		out.Waited = true
	}
	if err := resizeAfterRollout(ctx, o, client, &out, opts); err != nil {
		return nil, err
	}
	return out, nil
}

// archFor is what a deploy builds for: a deployed bog, its tier's
// architecture (a size asked for moves it after the rollout); a new one,
// the sandbox's (amd64, on Fly) at a shared size, EC2's (arm64) at a
// standard or more one, and with no CPU named, as spade places it: the
// sandbox where that is its default and has the memory asked for, else EC2
func archFor(ctx context.Context, client *Client, opts deployOptions) (string, error) {
	name := opts.Name
	if name == "" {
		pkg, err := localPackage(opts.Package)
		if err != nil {
			return "", err
		}
		name = bogSlug(pkg)
	}
	list, err := client.ListDeployments(ctx)
	if err != nil {
		return "", fmt.Errorf("list deployments: %w", err)
	}
	for _, d := range list {
		if d.Bog == name && d.DesiredState == api.DesiredRunning {
			if d.Tier == api.TierSandbox {
				return api.ArchAMD64, nil
			}
			return api.ArchARM64, nil
		}
	}
	switch opts.Size.CPU {
	case "shared":
		return api.ArchAMD64, nil
	case "":
		if opts.Box != "" {
			return api.ArchARM64, nil // a box named is EC2
		}
		sizes, err := client.GetSizes(ctx)
		if err != nil {
			return "", fmt.Errorf("sizes: %w", err)
		}
		isDefault, hasMemory := false, opts.Size.MemoryGB == 0
		for _, s := range sizes.Sizes {
			if s.Tier == api.TierSandbox {
				isDefault = isDefault || s.Default
				hasMemory = hasMemory || s.MemoryGB == opts.Size.MemoryGB
			}
		}
		if isDefault && hasMemory {
			return api.ArchAMD64, nil
		}
	}
	return api.ArchARM64, nil
}

// resizeAfterRollout: a deployed bog asked for at another machine moves to
// it, and with more data than it has, its data grows: two steps after the
// rollout, said as such. A new bog had its size in
// its create; data that is larger already stays (it only grows).
func resizeAfterRollout(ctx context.Context, o *ui, client *Client, out *deployed, opts deployOptions) error {
	want := opts.Size
	d := out.Deployment
	if want.MemoryGB != 0 || want.CPU != "" {
		if r, err := resize(ctx, o, client, d, want.MemoryGB, want.CPU, opts.NoWait, opts.Timeout); err != nil {
			return err
		} else if !r.Unchanged {
			out.Resized, out.Deployment = &r, r.Deployment
		}
	}
	if want.DataGB == 0 {
		return nil
	}
	b, err := client.GetBog(ctx, d.Bog)
	if err != nil {
		return fmt.Errorf("bog %s: %w", d.Bog, err)
	}
	if b.Data == nil || b.Data.SizeGB >= want.DataGB {
		return nil
	}
	g, err := grow(ctx, o, client, b, want.DataGB, opts.NoWait, opts.Timeout)
	if err != nil {
		return err
	}
	out.Grown = &g
	return nil
}

// deployImage rolls out an existing deployment of name, or creates one on box
// at size (a rollout keeps its size: resizeAfterRollout changes it).
func deployImage(ctx context.Context, client *Client, name, box, image string, size sizing) (api.Deployment, bool, error) {
	list, err := client.ListDeployments(ctx)
	if err != nil {
		return api.Deployment{}, false, fmt.Errorf("list deployments: %w", err)
	}
	for _, d := range list {
		if d.Bog != name || d.DesiredState != api.DesiredRunning {
			continue
		}
		if box != "" && box != d.PeaterID {
			return api.Deployment{}, false, fail("conflict", fmt.Sprintf("%s already runs on %s; --box places only a new deployment", name, d.PeaterID), fmt.Sprintf("leave out --box, or move it with `flop bog rebox %s`", name))
		}
		updated, err := client.UpdateDeployment(ctx, d.ID, api.UpdateDeploymentRequest{ImageRef: image})
		if err != nil {
			return api.Deployment{}, false, fmt.Errorf("rollout: %w", err)
		}
		return updated, false, nil
	}
	req := api.CreateDeploymentRequest{Bog: name, ImageRef: image, PeaterID: box, MemoryGB: size.MemoryGB, CPU: size.CPU, DataGB: size.DataGB}
	if size.CPU == "shared" {
		req.Tier = api.TierSandbox // a shared size is the sandbox's
	}
	if size.DataGB != 0 {
		// data kept from an earlier deployment keeps its size here (spade
		// refuses one for it); resizeAfterRollout grows it if asked for more
		if b, err := client.GetBog(ctx, name); err == nil && b.Data != nil {
			req.DataGB = 0
		}
	}
	created, err := client.CreateDeployment(ctx, req)
	var apiErr *APIError
	if box == "" && errors.As(err, &apiErr) && strings.Contains(apiErr.Body, "BOXES_ENABLED") {
		return api.Deployment{}, false, usage("this spade does not launch boxes", fmt.Sprintf("pass --box <instance-id> to place %s", name))
	}
	if err != nil {
		return api.Deployment{}, false, sizeRefusal("create deployment", err)
	}
	return created, true, nil
}

// waitForDeployment follows a deployment until its box reports generation gen
// running and healthy. A failure counts only if the box reported it after
// this deploy started: an error left over from an earlier attempt is stale.
func waitForDeployment(ctx context.Context, client *Client, id string, gen int64, since time.Time, interval, timeout time.Duration, out io.Writer) (api.Deployment, error) {
	deadline := time.Now().Add(timeout)
	last := ""
	var unreachableSince time.Time
	for {
		list, err := client.ListDeployments(ctx)
		if err != nil {
			// spade restarting (a deploy of spade itself) answers 502/503 or
			// refuses connections for a few seconds: the deployment carries on
			// without us, so keep following it, for a while
			if !transient(err) || (!unreachableSince.IsZero() && time.Since(unreachableSince) > spadeOutageGrace) {
				return api.Deployment{}, fmt.Errorf("follow deployment: %w", err)
			}
			if unreachableSince.IsZero() {
				unreachableSince = time.Now()
				fmt.Fprintln(out, "  spade is not answering (restarting?); still following")
				last = ""
			}
			select {
			case <-ctx.Done():
				return api.Deployment{}, ctx.Err()
			case <-time.After(interval):
			}
			continue
		}
		unreachableSince = time.Time{}
		var d *api.Deployment
		for i := range list {
			if list[i].ID == id {
				d = &list[i]
			}
		}
		if d == nil {
			return api.Deployment{}, fail("not_found", "the deployment disappeared while waiting (undeployed?)", "see `flop bog deployments`")
		}
		obs := d.Observed
		fresh := obs != nil && obs.At.After(since)

		// a placement problem needs a person (wrong zone, box not running);
		// waiting it out would only delay the bad news
		if d.ControlError != "" {
			return *d, fail("bog_failed", "spade cannot place this deployment: "+d.ControlError, "")
		}
		// a ready bog's box error is a move that failed earlier: it runs where
		// it was, and the error stays until the next move
		if d.BoxError != "" && d.BoxState != api.BoxReady {
			return *d, fail("bog_failed", "spade cannot get this deployment a box: "+d.BoxError, "")
		}
		// while it moves, its old box still reports it: done only once the
		// new box does (the move clears the old box's observation)
		moving := d.BoxState == api.BoxMoving

		switch {
		case fresh && obs.Error != "":
			return *d, fail("bog_failed", fmt.Sprintf("the box could not run generation %d: %s", gen, obs.Error),
				fmt.Sprintf("the box keeps retrying; deploy a fixed build to replace it, and see `flop bog logs %s`", d.Bog))
		case !moving && fresh && obs.Generation == gen && obs.State == api.BogStateRunning && obs.Healthy:
			fmt.Fprintf(out, "Running and healthy at generation %d\n", gen)
			return *d, nil
		}

		status := "waiting for the box to report"
		unhealthy, asleep := false, false
		switch {
		case d.BoxState == api.BoxLaunching:
			status = "launching its box"
		case d.BoxState == api.BoxBooting:
			status = fmt.Sprintf("box %s is booting", d.PeaterID)
		case d.BoxState == api.BoxMoving:
			status = "moving to its new box"
		case d.VolumeState == api.VolumePending:
			status = "waiting for its data volume to attach"
		case obs == nil:
		case obs.State == api.BogStateUnknown:
			status = "the box has gone quiet; waiting"
		case obs.Generation < gen:
			status = fmt.Sprintf("box is still on generation %d", obs.Generation)
		case obs.State == api.BogStateAsleep:
			// a sandbox bog, idle: spade wakes one for a new generation, so
			// one that stays asleep is waiting for a request
			status = "asleep (it wakes on its next request)"
			asleep = true
		case fresh && obs.Generation == gen && obs.State == api.BogStateRunning && !obs.Healthy:
			// the peater calls a bog healthy only once GET /healthz on its
			// port answers 200; an app serving its check elsewhere never is
			status = "running, but failing its health check (GET /healthz must answer 200)"
			unhealthy = true
		default:
			status = string(obs.State)
		}
		if status != last {
			fmt.Fprintf(out, "  %s\n", status)
			last = status
		}

		if time.Now().After(deadline) {
			if unhealthy {
				return *d, fail("bog_failed", fmt.Sprintf("gave up after %s: generation %d is running but its health check (GET /healthz) never passed", timeout, gen),
					fmt.Sprintf("a bog must answer 200 at GET /healthz on its port; see `flop bog logs %s`", d.Bog))
			}
			if asleep {
				return *d, fail("timeout", fmt.Sprintf("gave up after %s: %s is asleep at generation %d", timeout, d.Bog, gen),
					fmt.Sprintf("a sleeping bog wakes on its next request: `flop bog curl %s /healthz`, then wait again", d.Bog))
			}
			return *d, fail("timeout", fmt.Sprintf("gave up after %s (last: %s)", timeout, status),
				fmt.Sprintf("it may still get there: `flop bog wait %s`, or see `flop bog status %s`", d.Bog, d.Bog))
		}
		select {
		case <-ctx.Done():
			return *d, ctx.Err()
		case <-time.After(interval):
		}
	}
}

// how long flop keeps following a deployment while spade does not answer: a
// restart of spade's box takes about a minute
const spadeOutageGrace = 3 * time.Minute

// transient reports whether err means spade is briefly away (its load
// balancer has no healthy spade, or the connection failed), not a refusal.
func transient(err error) bool {
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		return apiErr.Status == http.StatusBadGateway || apiErr.Status == http.StatusServiceUnavailable || apiErr.Status == http.StatusGatewayTimeout
	}
	var urlErr *url.Error
	return errors.As(err, &urlErr) && !errors.Is(err, context.Canceled)
}

// bogSlug derives a valid bog name from a cargo package name
func bogSlug(pkg string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(pkg) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			b.WriteRune(r)
		} else {
			b.WriteByte('-')
		}
	}
	return strings.Trim(b.String(), "-")
}

// bogStatus is one bog as the panel shows it (GET /bogs/{name})
type bogStatus struct {
	api.Bog
	// Machine is the catalog's entry for its size: vCPUs and price (absent
	// when not deployed, or the catalog could not be read)
	Machine *api.Size `json:"machine,omitempty"`
}

func (b bogStatus) human(w io.Writer) {
	fmt.Fprintf(w, "%s: %s", b.Name, b.Status)
	if b.Deployment != nil && b.Status != api.BogNotDeployed {
		fmt.Fprintf(w, ", %s", deploymentHealth(*b.Deployment))
	}
	fmt.Fprintln(w)
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	if b.URL != "" {
		fmt.Fprintf(tw, "url\t%s\n", b.URL)
	}
	if d := b.Deployment; d != nil {
		fmt.Fprintf(tw, "deployment\t%s, generation %s\n", d.ID, deploymentGen(*d))
		fmt.Fprintf(tw, "image\t%s\n", d.ImageRef)
		fmt.Fprintf(tw, "box\t%s\n", box(*d))
		m := machine(d.MemoryGB, d.CPU)
		if s := b.Machine; s != nil {
			m += fmt.Sprintf(" (%d vCPUs", s.VCPUs)
			if s.Burstable {
				m += ", burstable"
			}
			m += fmt.Sprintf("), %s an hour", dollars(s.PricePerHour, 4))
		}
		fmt.Fprintf(tw, "machine\t%s\n", m)
	}
	if b.Data != nil {
		used := ""
		if d := b.Deployment; d != nil && d.Observed != nil && d.Observed.DataSizeBytes > 0 {
			used = fmt.Sprintf(", %d%% used", d.Observed.DataUsedBytes*100/d.Observed.DataSizeBytes)
		}
		fmt.Fprintf(tw, "data\t%s, %d GB%s, %s\n", orDash(b.Data.VolumeID), b.Data.SizeGB, used, volumeState(*b.Data))
	}
	fmt.Fprintf(tw, "access\t%s, %d keys\n", b.Access, b.Keys)
	tw.Flush()
	if b.Deployment != nil {
		for _, e := range deploymentErrors(*b.Deployment) {
			fmt.Fprintf(w, "\nerror: %s\n", e)
		}
	}
	for _, a := range b.Advice {
		fmt.Fprintf(w, "\nadvice: %s\n", a)
	}
}

// box is where a deployment runs: its EC2 box, or for a sandbox bog, Fly
// and its region
func box(d api.Deployment) string {
	if d.Tier != api.TierSandbox {
		return orDash(d.PeaterID)
	}
	if d.Observed != nil && d.Observed.Region != "" {
		return "Fly, " + d.Observed.Region
	}
	return "Fly"
}

func BogStatus(ctx context.Context, bog string) (bogStatus, error) {
	client, err := loggedInClient()
	if err != nil {
		return bogStatus{}, err
	}
	if bog == "" {
		return bogStatus{}, usage("status needs a bog", "flop bog status <bog>")
	}
	b, err := client.GetBog(ctx, bog)
	if err != nil {
		return bogStatus{}, fmt.Errorf("bog %s: %w", bog, err)
	}
	out := bogStatus{Bog: b}
	if d := b.Deployment; d != nil && b.Status != api.BogNotDeployed {
		// its vCPUs and price; status stands without them
		if sizes, err := client.GetSizes(ctx); err == nil {
			for _, s := range sizes.Sizes {
				if s.MemoryGB == d.MemoryGB && s.CPU == d.CPU {
					out.Machine = &s
				}
			}
		}
	}
	return out, nil
}

// waited is what `flop bog wait` saw: the deployment running and healthy
type waited struct {
	Bog        string         `json:"bog"`
	Deployment api.Deployment `json:"deployment"`
}

func (w waited) human(out io.Writer) {
	fmt.Fprintf(out, "%s is running and healthy at generation %d\n", w.Bog, w.Deployment.Generation)
}

// WaitForBog waits until the bog's deployment runs its current generation,
// healthy: what `flop bog deploy` does after deploying, on its own.
func WaitForBog(ctx context.Context, o *ui, bog string, timeout time.Duration) (waited, error) {
	client, err := loggedInClient()
	if err != nil {
		return waited{}, err
	}
	if bog == "" {
		return waited{}, usage("wait needs a bog", "flop bog wait <bog>")
	}
	d, err := resolveDeployment(ctx, client, bog)
	if err != nil {
		return waited{}, err
	}
	if d.DesiredState != api.DesiredRunning {
		return waited{}, fail("conflict", fmt.Sprintf("%s is being undeployed, not deployed", bog), "")
	}
	// only what the box reported since the deployment last changed counts
	d, err = waitForDeployment(ctx, client, d.ID, d.Generation, d.UpdatedAt, 2*time.Second, timeout, o.err)
	if err != nil {
		return waited{}, err
	}
	return waited{Bog: d.Bog, Deployment: d}, nil
}
