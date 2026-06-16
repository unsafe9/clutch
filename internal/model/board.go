package model

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

// Appraisal is a cached result of a classify / inferred-relation computation.
type Appraisal struct {
	// Kind is the appraisal kind (e.g. classify, inferred-relation).
	Kind string `json:"kind"`
	// Subject is what was appraised (e.g. a task id or representation ref).
	Subject string `json:"subject"`
	// Result is the appraisal outcome.
	Result string `json:"result"`
	// Confidence in [0,1].
	Confidence float64 `json:"confidence"`
}
