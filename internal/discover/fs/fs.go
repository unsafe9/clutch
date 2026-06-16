// Package fs scans configured roots for repos and their worktrees. Concrete
// functions only — no common Discoverer interface.
package fs

import "github.com/unsafe9/clutch/internal/model"

// Observe scans roots and returns repo/worktree filesystem observations.
func Observe(roots []string) ([]model.FSObservation, error) {
	// TODO(wave1-a): walk roots; detect .git dirs and linked worktrees; map to
	// model.FSObservation.
	panic("not implemented")
}
