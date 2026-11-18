package api

import "time"

// A bog's variables and secrets: given to it as environment variables. A
// variable's value can be read back; a secret's never can, once set.

type EnvVar struct {
	Name   string `json:"name"`
	Secret bool   `json:"secret"`
	// Value: a variable's; never a secret's
	Value string    `json:"value,omitempty"`
	SetBy *Actor    `json:"set_by,omitempty"`
	SetAt time.Time `json:"set_at"`
}

// GET /api/v1/bogs/{name}/env, and what a change answers
type EnvResponse struct {
	Vars []EnvVar `json:"vars"`
	// Restarting: a change made the running bog roll out with it
	Restarting bool `json:"restarting,omitempty"`
}

type EnvValue struct {
	Value  string `json:"value"`
	Secret bool   `json:"secret"`
}

// PUT /api/v1/bogs/{name}/env: set some (the rest are kept); the running
// bog rolls out with them unless NoRestart (then they apply at its next
// deploy)
type SetEnvRequest struct {
	Vars      map[string]EnvValue `json:"vars"`
	NoRestart bool                `json:"no_restart,omitempty"`
}
