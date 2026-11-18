package api

import "time"

// Deployments are the control plane's desired state: "this bog, at this
// image, should run on that box." The box is either named by the caller
// (manual placement) or launched by spade (BoxManaged).

type DesiredState string

const (
	DesiredRunning DesiredState = "running"
	// deleted deployments stay visible until the box reports the bog gone,
	// then are removed
	DesiredDeleted DesiredState = "deleted"
)

// Observation is the latest truth a peater reported about a deployment.
type Observation struct {
	State      BogState  `json:"state"`
	Healthy    bool      `json:"healthy"`
	Generation int64     `json:"generation"` // which epoch is actually running
	Address    string    `json:"address,omitempty"`
	Error      string    `json:"error,omitempty"`
	At         time.Time `json:"at"`
	// its data's filesystem, used and size (0 without a data volume)
	DataUsedBytes int64 `json:"data_used_bytes,omitempty"`
	DataSizeBytes int64 `json:"data_size_bytes,omitempty"`
	// the times the kernel killed the bog for memory on its box, the last at
	// LastOOMAt
	OOMKills  int64      `json:"oom_kills,omitempty"`
	LastOOMAt *time.Time `json:"last_oom_at,omitempty"`
	// Region is where a sandbox bog runs: a Fly region (empty on EC2)
	Region string `json:"region,omitempty"`
}

type Deployment struct {
	ID             string `json:"id"`
	OrganizationID string `json:"organization_id"`
	Bog            string `json:"bog"`
	ImageRef       string `json:"image_ref"`
	PeaterID       string `json:"peater_id"` // the EC2 instance it is placed on; empty while spade launches one
	Port           int    `json:"port"`
	// Tier: TierStandard (EC2) or TierSandbox (Fly)
	Tier string `json:"tier"`
	// its machine: memory, and CPU standard or more
	// (sandbox: shared)
	MemoryGB int    `json:"memory_gb"`
	CPU      string `json:"cpu"`
	// DataGB: the size the bog's data starts at, when this deployment makes
	// it (0: the default); a bog's data size after that is its volume's
	DataGB int `json:"data_gb,omitempty"`
	// Seed and Export: a move between tiers. Seed
	// is the copy this deployment's fresh data starts from; Export, a copy
	// of its data to take after stopping it. Copy ids, in the bog's copies.
	Seed         string       `json:"seed,omitempty"`
	Export       string       `json:"export,omitempty"`
	Generation   int64        `json:"generation"` // desired epoch; bumps on every change
	DesiredState DesiredState `json:"desired_state"`
	Observed     *Observation `json:"observed,omitempty"`
	// the bog's data volume once attached to this deployment's box
	VolumeID    string      `json:"volume_id,omitempty"`
	VolumeState VolumeState `json:"volume_state,omitempty"`
	// VolumeFresh: the volume has never been mounted, so it holds no data yet
	VolumeFresh bool `json:"volume_fresh,omitempty"`
	// ControlError is a control-plane problem placing this deployment (for
	// example its box is in the wrong availability zone for its data)
	ControlError string `json:"control_error,omitempty"`
	// BoxManaged: spade launched this deployment's box and terminates it on
	// undeploy. A manual box (named by the caller) is never launched,
	// started or terminated by spade.
	BoxManaged bool     `json:"box_managed,omitempty"`
	BoxState   BoxState `json:"box_state,omitempty"`
	// BoxError is a problem with a spade-managed box that needs a person (a
	// cap reached, a box that never registered). Separate from ControlError,
	// which the volume reconciler owns.
	BoxError string `json:"box_error,omitempty"`
	// BoxImage is the AMI the box was launched from (spade-managed boxes);
	// BoxVersion is the peater build the box last reported. Together they
	// say what a box actually runs.
	BoxImage string `json:"box_image,omitempty"`
	// URL is the bog's public address (https://<hostname>), the same across
	// redeploys and box moves; empty when this spade does not route bogs
	URL        string    `json:"url,omitempty"`
	BoxVersion string    `json:"box_version,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// POST /api/v1/deployments
type CreateDeploymentRequest struct {
	Bog      string `json:"bog"`
	ImageRef string `json:"image_ref"`
	PeaterID string `json:"peater_id,omitempty"` // empty: spade launches a box
	Port     int    `json:"port,omitempty"`      // default 7877
	// the machine (GET /api/v1/sizes): memory in GB (default 2), CPU
	// "standard" (default) or "more"
	MemoryGB int    `json:"memory_gb,omitempty"`
	CPU      string `json:"cpu,omitempty"`
	// Tier: TierStandard (EC2, the default for now) or TierSandbox (Fly)
	Tier string `json:"tier,omitempty"`
	// DataGB: the size of the bog's data, when it has none yet (default 10;
	// it only grows after)
	DataGB int `json:"data_gb,omitempty"`
}

// POST /api/v1/deployments/{id}/resize: a move to a box of this size (the
// bog is down while its data moves). Either may be left out: it stays.
type ResizeDeploymentRequest struct {
	MemoryGB int    `json:"memory_gb,omitempty"`
	CPU      string `json:"cpu,omitempty"`
}

// PATCH /api/v1/deployments/{id}: a new image is a new generation, which
// peater rolls out with its drain-then-swap handover
type UpdateDeploymentRequest struct {
	ImageRef string `json:"image_ref"`
}

// VolumeState tracks a deployment's data volume; empty when volumes are not
// managed (local development), in which case assignments are never held back.
type VolumeState string

const (
	// the volume is being created or attached; the box gets no assignment yet,
	// so a bog never starts against an empty directory on the root disk
	VolumePending  VolumeState = "pending"
	VolumeAttached VolumeState = "attached"
)

type ListDeploymentsResponse struct {
	Deployments []Deployment `json:"deployments"`
}

// The tiers a bog runs on.
const (
	TierStandard = "standard" // a box of its own on EC2
	TierSandbox  = "sandbox"  // a Fly Machine that sleeps when idle
)

// MoveResponse answers a resize across tiers (POST
// /api/v1/deployments/{id}/resize from a shared size to a standard or more
// one, or back): the bog moves with its data
type MoveResponse struct {
	ID       string `json:"id"`
	Bog      string `json:"bog"`
	To       string `json:"to"` // the tier: standard (EC2) or sandbox
	MemoryGB int    `json:"memory_gb"`
	CPU      string `json:"cpu"`
	State    string `json:"state"`
	Note     string `json:"note"`
}

type BogState string

const (
	BogStateProvisioning BogState = "provisioning" // pulling image, preparing
	BogStateRunning      BogState = "running"
	BogStateDraining     BogState = "draining" // flush requested, not yet confirmed
	BogStateStopped      BogState = "stopped"
	BogStateFailed       BogState = "failed"
	// a sandbox bog (Fly) suspended or stopped while idle: its next request
	// wakes it. Set by spade, never by peater
	BogStateAsleep BogState = "asleep"
	// set by the control plane, never by peater: the box has gone quiet, so
	// the last observation can no longer be trusted
	BogStateUnknown BogState = "unknown"
)

// BoxState is a spade-managed box's progress, as shown on its deployment.
type BoxState string

const (
	BoxLaunching BoxState = "launching" // no instance yet
	BoxBooting   BoxState = "booting"   // instance exists; its peater has not reported
	BoxReady     BoxState = "ready"     // its peater reports
	BoxMoving    BoxState = "moving"    // moving to a new box (rebox or replacement)
)
