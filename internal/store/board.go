// Package store defines the persistence ports: BoardStore (per-task board
// state) and IDRegistry (stable task-id anchoring). The default backend is the
// out-of-repo file store (store/file); in-repo/MCP backends may be added later.
package store

import "github.com/unsafe9/clutch/internal/model"

// BoardStore is the port to a task's durable Board state. The CLI is the sole
// gateway to it (architecture invariant 1).
type BoardStore interface {
	// Get returns the board for taskID.
	Get(taskID string) (*model.Board, error)
	// SetPrinciples sets the task's work principles.
	SetPrinciples(taskID, principles string) error
	// SetDesign sets the task's evolving design (engineering altitude, no code).
	SetDesign(taskID, design string) error
	// AppendDecision appends a decision to the task's design.
	AppendDecision(taskID string, d model.Decision) error
	// AddADR appends an architecture decision record.
	AddADR(taskID string, adr model.ADR) error
	// Query runs a cross-board query for project knowledge (related tasks /
	// prior decisions).
	Query(q Query) (*QueryResult, error)
}

// IDRegistry is the port that anchors stable clutch task ids to durable
// representation signatures. Same signature → same id across scans. It lives
// beside the board store. One id may have MANY signatures attached (one per
// representation that anchors it) — multi-representation anchoring is done here,
// via Attach.
//
// IDRegistry satisfies correlate.IDResolver (Resolve/Mint/Attach/Merge) and adds
// the registry-maintenance op Retire, which the pure correlation core does not
// need.
//
// Vanished-representation behavior: when a previously-anchored signature stops
// appearing in scans, the id is RETAINED (board history is the knowledge
// pillar) — the representation simply drops from the projection, and the task
// may become `stale`. Split (one id discovered to be two tasks) is deferred:
// there is no method for it yet.
type IDRegistry interface {
	// Resolve returns the existing id for sig, or ok=false if none is anchored.
	Resolve(sig model.Signature) (id string, ok bool, err error)
	// Mint anchors a new stable id to sig and returns it.
	Mint(sig model.Signature) (id string, err error)
	// Attach anchors an additional signature to an existing id (also covers
	// aliasing a second signature onto the same task).
	Attach(id string, sig model.Signature) error
	// Merge folds mergeID into keepID once two ids are found to be one task and
	// returns the surviving id.
	Merge(keepID, mergeID string) (id string, err error)
	// Retire marks an id as gone. The id is NOT deleted (board knowledge
	// persists) — it is only marked retired.
	Retire(id string) error
}

// Query is a cross-board query for project knowledge.
type Query struct {
	// Text is a free-text query over board contents.
	Text string `json:"text"`
	// TaskIDs optionally scopes the query to specific tasks.
	TaskIDs []string `json:"task_ids,omitempty"`
}

// QueryResult is the result of a cross-board Query.
type QueryResult struct {
	// Tasks are related task ids.
	Tasks []string `json:"tasks"`
	// Decisions are prior decisions matching the query.
	Decisions []model.Decision `json:"decisions"`
	// ADRs are prior ADRs matching the query.
	ADRs []model.ADR `json:"adrs"`
}
