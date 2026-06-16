// Package github implements the adapter.IssueTracker port via `gh` shell-out.
// No GitHub Go SDK is used.
package github

import (
	"github.com/unsafe9/clutch/internal/adapter"
	"github.com/unsafe9/clutch/internal/model"
)

// Tracker is the github IssueTracker backend (shells out to `gh`).
type Tracker struct{}

// Compile-time proof that *Tracker implements the port.
var _ adapter.IssueTracker = (*Tracker)(nil)

// New returns a github issue tracker.
func New() *Tracker { return &Tracker{} }

// Name implements adapter.IssueTracker.
func (t *Tracker) Name() string {
	// TODO(wave2): return "github".
	panic("not implemented")
}

// Fetch implements adapter.IssueTracker.
func (t *Tracker) Fetch(key string) (model.Issue, error) {
	// TODO(wave2): shell out to `gh issue view <key> --json ...`.
	panic("not implemented")
}
