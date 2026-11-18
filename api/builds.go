package api

import "time"

type BuildStatus string

const (
	BuildStatusQueued    BuildStatus = "queued"
	BuildStatusRunning   BuildStatus = "running"
	BuildStatusSucceeded BuildStatus = "succeeded"
	BuildStatusFailed    BuildStatus = "failed"
	BuildStatusCanceled  BuildStatus = "canceled"
)

// Terminal reports whether a build in this status will never change again.
func (s BuildStatus) Terminal() bool {
	switch s {
	case BuildStatusSucceeded, BuildStatusFailed, BuildStatusCanceled:
		return true
	}
	return false
}

// Build is the build record as returned by the builds endpoints.
type Build struct {
	ID             string      `json:"id"`
	OrganizationID string      `json:"organization_id"`
	PrincipalID    string      `json:"principal_id"`
	Package        string      `json:"package"`
	GitSHA         string      `json:"git_sha,omitempty"`
	Dirty          bool        `json:"dirty"`
	Fresh          bool        `json:"fresh"`
	Status         BuildStatus `json:"status"`
	ImageRef       string      `json:"image_ref,omitempty"`
	ExitCode       *int        `json:"exit_code,omitempty"`
	TarballBytes   int64       `json:"tarball_bytes"`
	CreatedAt      time.Time   `json:"created_at"`
	StartedAt      *time.Time  `json:"started_at,omitempty"`
	FinishedAt     *time.Time  `json:"finished_at,omitempty"`
}

type ListBuildsResponse struct {
	Builds []Build `json:"builds"`
}

// BuildEvent is one line of the NDJSON stream returned by POST
// /api/v1/builds. Exactly one field is set per line: Out and Err carry raw
// build output chunks, HB is a keepalive during quiet stretches, and the
// stream always ends with either Done or Error.
type BuildEvent struct {
	Out   string     `json:"out,omitempty"`
	Err   string     `json:"err,omitempty"`
	HB    bool       `json:"hb,omitempty"`
	Done  *BuildDone `json:"done,omitempty"`
	Error string     `json:"error,omitempty"`
}

// BuildDone means the build ran to completion; ExitCode may still be nonzero,
// in which case Image is empty.
type BuildDone struct {
	ExitCode int    `json:"exit_code"`
	Image    string `json:"image,omitempty"`
}

// Request headers on POST /api/v1/builds. Dirty and Fresh are "true"/"false";
// Arch is ArchARM64 (the default: EC2's Graviton boxes) or ArchAMD64 (the
// sandbox tier on Fly).
const (
	HeaderPackage = "X-Bog-Package"
	HeaderGitSHA  = "X-Bog-Git-Sha"
	HeaderDirty   = "X-Bog-Dirty"
	HeaderFresh   = "X-Bog-Fresh"
	HeaderArch    = "X-Bog-Arch"
)

// The architectures a bog is built for, as OCI images name them.
const (
	ArchARM64 = "arm64"
	ArchAMD64 = "amd64"
)

// HeaderBuildID is set on the response before the stream begins, so a client
// can reference the build even if the stream drops.
const HeaderBuildID = "X-Bog-Build-Id"

// HeaderOrganizationID scopes CLI-token requests to an organization; JWTs carry the organization
// in their claims instead.
const HeaderOrganizationID = "X-Spade-Organization-ID"

// MaxTarballBytes caps the gzipped source upload.
const MaxTarballBytes = 50 << 20
