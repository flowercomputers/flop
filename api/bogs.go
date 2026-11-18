package api

import "time"

// A bog as the panel shows it: everything spade keeps about one bog name in
// an organization (its deployment, kept data, keys, access mode).
type Bog struct {
	Name string `json:"name"`
	// Status is the deployment's (Deployment.Status), or "not deployed"
	Status     string      `json:"status"`
	Healthy    bool        `json:"healthy"`
	URL        string      `json:"url,omitempty"`
	Deployment *Deployment `json:"deployment,omitempty"` // the current one
	// Build is the build that made the deployment's image, when spade built it
	Build        *Build     `json:"build,omitempty"`
	LastDeployAt *time.Time `json:"last_deploy_at,omitempty"`
	Data         *BogVolume `json:"data,omitempty"` // its kept data volume
	Keys         int        `json:"keys"`
	Access       AccessMode `json:"access"`
	// Advice is what to do about something that is not yet a failure, such
	// as data nearly full
	Advice []string `json:"advice,omitempty"`
}

// BogNotDeployed is a bog's status when nothing runs it (its data, keys or
// settings are kept).
const BogNotDeployed = "not deployed"

// Status is a deployment's state in a word or two, the first thing that
// stands between it and serving.
func (d Deployment) Status() string {
	switch {
	case d.DesiredState == DesiredDeleted:
		return "deleting"
	case d.ControlError != "", d.BoxError != "":
		return "blocked"
	case d.BoxState == BoxLaunching:
		return "launching box"
	case d.BoxState == BoxBooting:
		return "box booting"
	case d.BoxState == BoxMoving:
		return "moving box"
	case d.VolumeState == VolumePending:
		return "volume pending"
	case d.Observed == nil:
		return "pending"
	default:
		return string(d.Observed.State)
	}
}
