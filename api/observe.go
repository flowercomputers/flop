package api

import "time"

// What owners read about a bog: GET /api/v1/bogs/{name}/metrics and
// /logs. Spade builds every query itself, for the caller's own bog; nothing
// the caller sends becomes part of one.

type MetricPoint struct {
	T int64   `json:"t"` // unix seconds
	V float64 `json:"v"`
}

type MetricSeries struct {
	Name   string        `json:"name"`  // e.g. "requests"
	Title  string        `json:"title"` // e.g. "requests per second"
	Unit   string        `json:"unit"`  // "per_second", "seconds", "bytes", "cores"
	Points []MetricPoint `json:"points"`
}

// GET /api/v1/bogs/{name}/metrics?window=1h
type BogMetrics struct {
	Bog    string         `json:"bog"`
	Window string         `json:"window"`
	Step   string         `json:"step"`
	Series []MetricSeries `json:"series"`
}

type LogEvent struct {
	Timestamp time.Time `json:"timestamp"`
	Message   string    `json:"message"`
	// Deployment is the log stream: one per deployment of the bog
	Deployment string `json:"deployment,omitempty"`
}

// GET /api/v1/bogs/{name}/logs?since=1h; with &follow=true the response is
// newline-delimited LogEvents for as long as the client listens
type BogLogs struct {
	Events []LogEvent `json:"events"`
}
