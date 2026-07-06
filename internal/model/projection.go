package model

import "time"

// SchemaVersion is the version of the machine (--json) projection contract,
// formatted MAJOR.MINOR.
//
// Versioning policy:
//   - MAJOR bumps on a breaking change: a field removed, renamed, or retyped, or
//     a field's semantics changed.
//   - MINOR bumps on an additive change: a new field.
//   - Consumers MUST ignore unknown fields and MUST NOT assume any field beyond
//     their pinned MAJOR.
//   - Pre-1.0 (0.x) is unstable: while MAJOR is 0, a MINOR bump MAY break.
const SchemaVersion = "0.2"

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
