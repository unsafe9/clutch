package model

import "time"

// Task is the central clutch object. Its fields fall into three provenance
// classes, made visible below by grouped fields and sub-structs:
//
//	① Identity & policy   — PERSISTED (store/id-registry); stable across scans.
//	② Representations     — DERIVED; recomputed each scan; NEVER persisted.
//	③ Relations & corr.   — MIXED: derived + declared + appraisal.
//
// Only class ③'s appraisal/declared parts and class ①'s planner-set parts are
// written by the agent layer; those persist to the board to avoid
// recomputation. Class ② carries zero LLM.
type Task struct {
	// ── Class ① Identity & policy ─────────────────────────────────────────
	// PERSISTED in the store / id-registry; stable across scans.

	// ID is the clutch-assigned identity, independent of any representation.
	ID string `json:"id"`
	// Title is a human label (from branch/PR/issue or planner).
	Title string `json:"title"`
	// Lifecycle is the task lifecycle state.
	Lifecycle Lifecycle `json:"lifecycle"`
	// Mode is the execution mode (project default → task override).
	Mode Mode `json:"mode"`
	// Provenance records how the task came to exist.
	Provenance Provenance `json:"provenance"`
	// Board locates this task's board backend.
	Board *BoardRef `json:"board"`
	// Created is when the task identity was minted.
	Created time.Time `json:"created"`
	// Updated is when the persisted identity/policy last changed.
	Updated time.Time `json:"updated"`

	// ── Class ② Representations ───────────────────────────────────────────
	// DERIVED; recomputed each scan; NEVER persisted; carries zero LLM.

	// Repos are the clones/checkouts this task spans.
	Repos []RepoRef `json:"repos"`
	// Branches are the branches that make up this task.
	Branches []Branch `json:"branches"`
	// Worktrees are the linked worktrees for this task.
	Worktrees []Worktree `json:"worktrees"`
	// Base is the fork-point ref.
	Base string `json:"base"`
	// Commits is a summary (NOT a full commit list).
	Commits CommitSummary `json:"commits"`
	// PRs are the pull requests for this task.
	PRs []PullRequest `json:"prs"`
	// Issues are external tracker issues (jira/github).
	Issues []Issue `json:"issues"`
	// Integration is the integration state vs base.
	Integration Integration `json:"integration"`
	// Sessions are the agent sessions touching this task.
	Sessions []Session `json:"sessions"`

	// ── Class ③ Relations & correlation ───────────────────────────────────
	// MIXED: derived + declared + appraisal. Appraisal/declared parts persist
	// to the board.

	// Lineage holds parent task ids.
	Lineage Lineage `json:"lineage"`
	// Relations holds the dependency DAG edges.
	Relations Relations `json:"relations"`
	// Links record how each representation was associated to this task.
	Links []Link `json:"links"`
	// Unresolved are ambiguity flags fed to the `classify` orchestrator later.
	Unresolved []Unresolved `json:"unresolved"`
}

// RepoRef identifies a clone/checkout this task spans.
type RepoRef struct {
	// Identity is the durable repo identity (not a path).
	Identity string `json:"identity"`
	// Path is the local checkout path.
	Path string `json:"path"`
	// Remote is the canonical remote URL.
	Remote string `json:"remote"`
}

// Branch is a branch representation within a repo.
type Branch struct {
	Repo     string `json:"repo"`
	Name     string `json:"name"`
	Head     string `json:"head"`
	Upstream string `json:"upstream"`
	Ahead    int    `json:"ahead"`
	Behind   int    `json:"behind"`
}

// Worktree is a linked git worktree.
type Worktree struct {
	Path   string `json:"path"`
	Branch string `json:"branch"`
	Repo   string `json:"repo"`
}

// CommitSummary is a head/count rollup, not a full commit list.
type CommitSummary struct {
	Head  string `json:"head"`
	Count int    `json:"count"`
}

// PullRequest is a PR representation.
type PullRequest struct {
	Host   string `json:"host"`
	Number int    `json:"number"`
	URL    string `json:"url"`
	State  string `json:"state"`
	Draft  bool   `json:"draft"`
	// Checks is the CI checks rollup state.
	Checks string `json:"checks"`
}

// Issue is an external tracker issue (jira/github).
type Issue struct {
	Tracker string `json:"tracker"`
	Key     string `json:"key"`
	URL     string `json:"url"`
	State   string `json:"state"`
}

// Session is an agent session touching the task.
//
// TODO(wave1-b): fields are PROVISIONAL and will be finalized after the CC and
// Codex on-disk session formats are reverse-engineered.
type Session struct {
	// Host is the agent host: claude-code | codex.
	Host         string    `json:"host"`
	Cwd          string    `json:"cwd"`
	Branch       string    `json:"branch,omitempty"`
	LastActivity time.Time `json:"last_activity"`
	Running      bool      `json:"running"`
}

// Lineage holds derived/declared parent task ids.
type Lineage struct {
	// Parents are parent task ids (derived from base where possible, else
	// declared).
	Parents []string `json:"parents"`
}

// Relations holds the dependency DAG edges (declared or appraised).
type Relations struct {
	Depends []string `json:"depends"`
	Blocks  []string `json:"blocks"`
}

// Link records how one representation was associated to the task.
type Link struct {
	// Method is how the link was established.
	Method LinkMethod `json:"method"`
	// Confidence in [0,1]; convention/declared are 1.0, appraisal is < 1.0.
	Confidence float64 `json:"confidence"`
}

// Unresolved is an ambiguity flag fed to the `classify` orchestrator later.
type Unresolved struct {
	// Kind categorizes the ambiguity (e.g. lineage, relation, link).
	Kind string `json:"kind"`
	// Detail describes what is ambiguous, for the LLM/engineer reader.
	Detail string `json:"detail"`
	// TaskID is the task the flag pertains to (empty for scan-wide flags).
	TaskID string `json:"task_id,omitempty"`
}
