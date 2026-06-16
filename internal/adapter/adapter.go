// Package adapter defines the IssueTracker port for external issue trackers.
// It is multi-backend by design: github now, jira later.
package adapter

import "github.com/unsafe9/clutch/internal/model"

// IssueTracker is the port to an external issue tracker.
type IssueTracker interface {
	// Name identifies the backend (e.g. "github", "jira").
	Name() string
	// Fetch resolves a single issue by its tracker key.
	Fetch(key string) (model.Issue, error)
}
