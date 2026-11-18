package api

// Size is a machine a bog can run on (GET /api/v1/sizes): an amount of
// memory, and how much CPU comes with it
type Size struct {
	MemoryGB int    `json:"memory_gb"`
	CPU      string `json:"cpu"` // "standard", or "more": twice the vCPUs at the same memory; "shared": the sandbox
	// Tier: "standard" (a box of its own, on EC2) or "sandbox" (Fly; it
	// sleeps when idle and wakes on a request)
	Tier string `json:"tier"`
	// Sleeps, for the sandbox: "suspend" (memory kept, a fast wake) or
	// "stop" (a cold start; Fly suspends only up to 2 GB)
	Sleeps string `json:"sleeps,omitempty"`
	VCPUs  int    `json:"vcpus"`
	// Burstable: the vCPUs run at a fraction of full time on average,
	// throttled to it once their credits are spent
	Burstable    bool  `json:"burstable"`
	PricePerHour Money `json:"price_per_hour"`
	Default      bool  `json:"default,omitempty"` // what a deployment gets when it names none
	// OnRequest: offered to organizations Flower Computer allows (64, 128 GB)
	OnRequest bool `json:"on_request,omitempty"`
	// Allowed: this organization may choose it for a new bog now; if not,
	// NotAllowed says why (a card needed, its memory limit)
	Allowed    bool   `json:"allowed"`
	NotAllowed string `json:"not_allowed,omitempty"`
}

// SizesResponse is GET /api/v1/sizes: every size, smallest first, and the
// range a bog's data may be (in whole GB; it only grows)
type SizesResponse struct {
	Sizes []Size    `json:"sizes"`
	Data  DataSizes `json:"data"`
	// SandboxData is the range a sandbox bog's data may be
	SandboxData DataSizes `json:"sandbox_data"`
	// the organization's limits: its bogs' memory in all, and the data it
	// keeps (an operator raises them)
	Limits Limits `json:"limits"`
}

type Limits struct {
	MemoryGB      int `json:"memory_gb"`
	MemoryInUseGB int `json:"memory_in_use_gb"`
	DataGB        int `json:"data_gb"`
	DataKeptGB    int `json:"data_kept_gb"`
	// BigSizes: the sizes on request (64, 128 GB) are allowed it
	BigSizes bool `json:"big_sizes,omitempty"`
	// the sandbox bogs it may run at once, and runs
	SandboxBogs      int `json:"sandbox_bogs"`
	SandboxBogsInUse int `json:"sandbox_bogs_in_use"`
}

type DataSizes struct {
	MinGB           int   `json:"min_gb"`
	MaxGB           int   `json:"max_gb"`
	DefaultGB       int   `json:"default_gb"`
	PricePerGBMonth Money `json:"price_per_gb_month"`
}
