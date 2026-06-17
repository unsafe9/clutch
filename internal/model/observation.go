package model

// Signature is a single durable representation signature used to anchor a stable
// task id. The same signature resolves to the same id across scans (e.g. repo
// identity + branch, or an issue link). It lives in model so the pure correlate
// package can name it without importing the store.
//
// A Signature is ONE durable key, not a bundle: a task id may have MANY
// signatures attached (one per representation that anchors it). Multi-
// representation anchoring is done through the registry (Attach), NOT by adding
// more fields to a single Signature.
type Signature struct {
	// Repo is the durable repo identity (not a path).
	Repo string `json:"repo,omitempty"`
	// Branch anchors a branch-scoped task.
	Branch string `json:"branch,omitempty"`
	// IssueLink anchors an issue-scoped task (tracker key/url).
	IssueLink string `json:"issue_link,omitempty"`
}

// GitObservation is a raw git/gh discovery record for one repo checkout. It is
// an input to correlation, not a projected representation.
type GitObservation struct {
	Repo      RepoRef    `json:"repo"`
	Branches  []Branch   `json:"branches"`
	Worktrees []Worktree `json:"worktrees"`
	// Base now lives per-Branch (Branches carry it); there is no observation-
	// level base.
	Commits CommitSummary `json:"commits"`
	PRs     []PullRequest `json:"prs"`
	Issues  []Issue       `json:"issues"`
}

// FSObservation is a raw filesystem discovery record: a repo and the worktrees
// found beneath a configured root.
type FSObservation struct {
	Repo      RepoRef    `json:"repo"`
	Worktrees []Worktree `json:"worktrees"`
}

// SessionObservation is a raw agent-session discovery record.
//
// TODO(wave1-b): PROVISIONAL shape pending session-format reverse.
type SessionObservation struct {
	Session Session `json:"session"`
}

// Observations is the full set of raw discovery inputs handed to correlate.
type Observations struct {
	Git      []GitObservation     `json:"git"`
	FS       []FSObservation      `json:"fs"`
	Sessions []SessionObservation `json:"sessions"`
}
