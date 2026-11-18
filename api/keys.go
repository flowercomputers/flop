package api

import "time"

// Bog API keys: the credential for calling a bog through the router. A key
// belongs to one bog; its secret (bog_<id>_<secret>) is shown once, and only
// its SHA-256 hash is stored and published to routers.

// A key is only an identity: routers refuse an invalid one and pass a valid
// one with any method. What a caller may do is the bog's to decide, from
// X-Bog-Auth and X-Bog-Key-Id.

// AccessMode is who may call a bog without a key. Routers treat a mode they
// don't know as private, so spade can add one without breaking them.
type AccessMode string

const (
	AccessPrivate AccessMode = "private" // every request needs a key
	AccessPublic  AccessMode = "public"  // anyone, any method; a key, if sent, must be valid
)

type BogKey struct {
	ID        string     `json:"id"`
	Bog       string     `json:"bog"`
	Name      string     `json:"name"`
	CreatedAt time.Time  `json:"created_at"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
	// usage as routers report it (every ~30 s)
	LastUsedAt *time.Time `json:"last_used_at,omitempty"`
	Requests   int64      `json:"requests"`
	CreatedBy  *Actor     `json:"created_by,omitempty"` // absent: not known
}

// POST /api/v1/bogs/{name}/keys
type CreateBogKeyRequest struct {
	Name string `json:"name"`
	// ExpiresIn is a Go duration ("720h", "5m"); empty means no expiry
	ExpiresIn string `json:"expires_in,omitempty"`
}

type CreateBogKeyResponse struct {
	Key BogKey `json:"key"`
	// Secret is the whole key, shown only in this response
	Secret string `json:"secret"`
	// URL is the bog's address, for convenience
	URL string `json:"url,omitempty"`
}

type ListBogKeysResponse struct {
	Keys []BogKey `json:"keys"`
}

// GET/PUT /api/v1/bogs/{name}/access
type BogAccess struct {
	Mode AccessMode `json:"mode"`
}
