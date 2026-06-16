// Package git produces raw git observations via `git`/`gh` shell-out.
//
// There is deliberately NO common Discoverer interface: git, fs and session are
// distinct producers exposed as concrete functions. No git Go library is used.
package git

import "github.com/unsafe9/clutch/internal/model"

// Observe returns git observations for the repos found under roots, shelling
// out to `git` (branches, worktrees, commits, fork-point) and `gh` (PRs).
func Observe(roots []string) ([]model.GitObservation, error) {
	// TODO(wave1-a): per repo run `git` for branches/worktrees/commits/base and
	// `gh` for PRs; map results to model.GitObservation.
	panic("not implemented")
}
