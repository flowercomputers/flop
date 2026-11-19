package api

import "time"

// BogVolume is a bog's data volume. It belongs to the bog — organization plus
// bog name — not to a deployment or a box, so undeploying keeps it and
// deploying the bog again reattaches it. Deleted only by an explicit destroy.
type BogVolume struct {
	OrganizationID string `json:"organization_id"`
	Bog            string `json:"bog"`
	VolumeID       string `json:"volume_id,omitempty"` // empty while being created
	// the bog is pinned here; empty for a restore not yet placed, which is
	// created in the zone of the box it is next deployed to
	AvailabilityZone string `json:"availability_zone"`
	// RestoredFrom is the snapshot the volume is (to be) created from
	RestoredFrom string `json:"restored_from,omitempty"`
	SizeGB       int    `json:"size_gb"`
	AttachedTo   string `json:"attached_to,omitempty"` // instance id, empty when detached
	State        string `json:"state"`                 // EC2's volume state, as last observed; "fly": a sandbox bog's data
	// Initialized: a box has mounted it at least once, so it holds a
	// filesystem. Until then it is fresh and may be formatted.
	Initialized bool      `json:"initialized"`
	CreatedAt   time.Time `json:"created_at"`
	// ModifiedAt: when it last changed size. EBS allows one change in 6
	// hours, so it may next grow 6 hours after.
	ModifiedAt *time.Time `json:"modified_at,omitempty"`
	// LastCopyAt is a sandbox bog's newest copy in S3; its copies are at
	// GET /api/v1/bogs/{name}/data/copies
	LastCopyAt *time.Time `json:"last_copy_at,omitempty"`
	// Pending is a "destroy", "restore" or "grow" asked for and not yet done:
	// the volume pass carries out a destroy or restore once the volume is
	// detached, a grow at once (online)
	Pending string `json:"pending,omitempty"`
}

type ListBogDataResponse struct {
	Volumes []BogVolume `json:"volumes"`
}

// BogCopy is a copy of a sandbox bog's data in S3, taken nightly while it
// runs. Its ID restores it: POST /api/v1/bogs/{name}/data/restore with
// snapshot_id set to it.
type BogCopy struct {
	ID        string     `json:"id"`
	State     string     `json:"state"` // snapshotting, copying, done or failed
	TakenAt   time.Time  `json:"taken_at"`
	DoneAt    *time.Time `json:"done_at,omitempty"`
	SizeBytes int64      `json:"size_bytes,omitempty"`
	Error     string     `json:"error,omitempty"`
}

// GET /api/v1/bogs/{name}/data/copies: a sandbox bog's copies, newest first
type BogCopiesResponse struct {
	Copies []BogCopy `json:"copies"`
}

// PATCH /api/v1/bogs/{name}/data: grow the bog's data to SizeGB, online
type GrowBogDataRequest struct {
	SizeGB int `json:"size_gb"`
}

type RestoreBogDataRequest struct {
	SnapshotID string `json:"snapshot_id"`
}

type RestoreBogDataResponse struct {
	Volume BogVolume `json:"volume"`
	// the volume the bog used before; kept, unattached, never deleted by a
	// restore
	ReplacedVolumeID string `json:"replaced_volume_id,omitempty"`
}
