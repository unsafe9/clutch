package model

import "time"

// BoardRef locates a task's board within a BoardStore backend.
type BoardRef struct {
	// Backend names the BoardStore backend (e.g. "file").
	Backend string `json:"backend"`
	// Locator is the backend-specific address of the board.
	Locator string `json:"locator"`
}

// Board is the durable per-task state behind the BoardStore port. It holds
// engineering-altitude knowledge for a task — NO code.
type Board struct {
	// Principles are the work principles for the task.
	Principles string `json:"principles"`
	// Design is the evolving design that converges to final; decisions
	// overwrite/accumulate. Engineering altitude, NO code.
	Design string `json:"design"`
	// Questions are open design unknowns the planner must close (or explicitly
	// defer) before the plan is declared complete.
	Questions []Question `json:"questions"`
	// ADRs are architecture decision records.
	ADRs []ADR `json:"adrs"`
	// Appraisals cache classify / inferred-relation results to avoid
	// recomputation.
	Appraisals []Appraisal `json:"appraisals"`
}

// ADR is an architecture decision record.
type ADR struct {
	Decision     string   `json:"decision"`
	Context      string   `json:"context"`
	Alternatives []string `json:"alternatives"`
	Consequence  string   `json:"consequence"`
}

// Decision is a single design decision appended to a board's design.
type Decision struct {
	Summary string `json:"summary"`
	Detail  string `json:"detail"`
}

// Question is an open design unknown the planner must close (or explicitly
// defer) before the plan is declared complete. Reopening is not modeled: a new
// concern is a new question.
type Question struct {
	ID         int            `json:"id"`
	Text       string         `json:"text"`
	Status     QuestionStatus `json:"status"`
	Resolution string         `json:"resolution,omitempty"`
	Created    time.Time      `json:"created"`
	Resolved   time.Time      `json:"resolved,omitempty"`
}

// Appraisal is a cached result of a classify / inferred-relation computation.
type Appraisal struct {
	// Kind is the appraisal kind.
	Kind AppraisalKind `json:"kind"`
	// Subject is the representation this appraisal concerns (within-task RepRef).
	Subject RepRef `json:"subject"`
	// Result is the appraisal outcome.
	Result string `json:"result"`
	// Confidence in [0,1].
	Confidence float64 `json:"confidence"`
	// InputFingerprint identifies the inputs this appraisal was computed over,
	// so a cached appraisal can be invalidated when its inputs change.
	InputFingerprint string `json:"input_fingerprint"`
	// ComputedAt is when this appraisal was computed.
	ComputedAt time.Time `json:"computed_at"`
}
