package model

import "time"

// SchemaVersion is the version of the machine (--json) projection contract.
// Bump it on any breaking change to the envelope or Task shape.
const SchemaVersion = "0.1"

// ProjectionEnvelope is the stable, schema-versioned machine contract emitted
// by clutch's --json output. It is the sole public data shape; further surfaces
// (MCP/file/dashboard) are thin projections of it.
type ProjectionEnvelope struct {
	SchemaVersion string      `json:"schema_version"`
	GeneratedAt   time.Time   `json:"generated_at"`
	Tasks         []Task      `json:"tasks"`
	Diagnostics   Diagnostics `json:"diagnostics"`
}

// Diagnostics carries scan-wide ambiguity flags and statistics.
type Diagnostics struct {
	Unresolved []Unresolved `json:"unresolved"`
	ScanStats  ScanStats    `json:"scan_stats"`
}

// ScanStats summarizes a scan run.
type ScanStats struct {
	ReposScanned   int   `json:"repos_scanned"`
	Worktrees      int   `json:"worktrees"`
	Sessions       int   `json:"sessions"`
	TasksProjected int   `json:"tasks_projected"`
	DurationMS     int64 `json:"duration_ms"`
}
