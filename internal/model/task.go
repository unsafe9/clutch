package model

import "time"

// Task is the central clutch object. Its fields fall into three classes by
// derivation & persistence, made visible below by grouped fields and
// sub-structs:
//
//	① Identity & policy   — PERSISTED (store/id-registry); stable across scans.
//	② Representations     — DERIVED; recomputed each scan; NEVER persisted.
//	③ Relations & corr.   — MIXED: derived + declared + appraisal.
//
// The basis of the classification is derivation & persistence, NOT provenance:
// Provenance (clutch-initiated / git-detected) is a single field within Class ①,
// not the axis these classes are cut along.
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
	// Commits is a summary (NOT a full commit list).
	Commits CommitSummary `json:"commits"`
	// PRs are the pull requests for this task.
	PRs []PullRequest `json:"prs"`
	// Issues are external tracker issues (jira/github).
	Issues []Issue `json:"issues"`
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

// RepRef is an opaque, stable, within-task key identifying ONE representation of
// a task, so a Link, Unresolved flag, or Appraisal can name which representation
// it concerns. It is stable within a single task projection; it is not a global
// identifier. The key scheme is:
//
//	repo:<identity>            — a RepoRef
//	branch:<repo-identity>/<name> — a Branch
//	worktree:<path>            — a Worktree
//	pr:<host>#<number>         — a PullRequest
//	issue:<tracker>/<key>      — an Issue
//	session:<host>/<cwd>       — a Session
type RepRef string

// RepoRef identifies a clone/checkout this task spans.
type RepoRef struct {
	// Ref is this representation's within-task key (repo:<identity>).
	Ref RepRef `json:"ref"`
	// Identity is the durable repo identity (not a path).
	Identity string `json:"identity"`
	// Path is the local checkout path.
	Path string `json:"path"`
	// Remote is the canonical remote URL.
	Remote string `json:"remote"`
}

// Branch is a branch representation within a repo. Base and Integration live
// here, per representation: a task may span multiple branches/repos with
// divergent fork-points and merge states, so these cannot be Task-level scalars.
type Branch struct {
	// Ref is this representation's within-task key (branch:<repo-identity>/<name>).
	Ref  RepRef `json:"ref"`
	Repo string `json:"repo"`
	Name string `json:"name"`
	Head string `json:"head"`
	// Base is this branch's fork-point ref.
	Base     string `json:"base"`
	Upstream string `json:"upstream"`
	Ahead    int    `json:"ahead"`
	Behind   int    `json:"behind"`
	// Integration is this branch's integration state vs its base.
	Integration Integration `json:"integration"`
}

// Worktree is a linked git worktree.
type Worktree struct {
	// Ref is this representation's within-task key (worktree:<path>).
	Ref    RepRef `json:"ref"`
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
	// Ref is this representation's within-task key (pr:<host>#<number>).
	Ref    RepRef `json:"ref"`
	Host   string `json:"host"`
	Number int    `json:"number"`
	URL    string `json:"url"`
	State  string `json:"state"`
	Draft  bool   `json:"draft"`
	// Checks is the CI checks rollup state.
	Checks string `json:"checks"`
	// ReviewDecision is the PR's review verdict: approved | changes_requested |
	// review_required | "" (none requested/recorded).
	ReviewDecision string `json:"review_decision"`
	// Mergeable is the PR's merge state: mergeable | conflicting | unknown | "".
	Mergeable string `json:"mergeable"`
}

// Issue is an external tracker issue (jira/github).
type Issue struct {
	// Ref is this representation's within-task key (issue:<tracker>/<key>).
	Ref     RepRef `json:"ref"`
	Tracker string `json:"tracker"`
	Key     string `json:"key"`
	URL     string `json:"url"`
	State   string `json:"state"`
}

// Session is an agent session touching the task. Fields are finalized against
// the reverse-engineered CC and Codex on-disk formats (docs/session-format.md):
// both hosts yield host, cwd, git branch, and a last-activity timestamp;
// Running is a deterministic recency derivation (no host lock/pid file exists).
type Session struct {
	// Ref is this representation's within-task key (session:<host>/<cwd>).
	Ref RepRef `json:"ref"`
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
	// Subject is the representation this link concerns (within-task RepRef).
	Subject RepRef `json:"subject"`
	// Method is how the link was established.
	Method LinkMethod `json:"method"`
	// Confidence in [0,1]; convention/declared are 1.0, appraisal is < 1.0.
	Confidence float64 `json:"confidence"`
}

// Unresolved is an ambiguity flag fed to the `classify` orchestrator later.
type Unresolved struct {
	// Kind categorizes the ambiguity.
	Kind UnresolvedKind `json:"kind"`
	// Detail describes what is ambiguous, for the LLM/engineer reader.
	Detail string `json:"detail"`
	// Refs are the representation(s) the ambiguity concerns (empty if none).
	Refs []RepRef `json:"refs,omitempty"`
	// TaskID is the task the flag pertains to (empty for scan-wide flags).
	TaskID string `json:"task_id,omitempty"`
}
