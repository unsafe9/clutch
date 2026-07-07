package cli

import "github.com/unsafe9/clutch/internal/model"

// The machine contract renders every documented array as [] never null. Slices
// arrive from the correlation core and the store as nil when empty, which JSON
// marshals as null; these helpers coerce empty slices to non-nil at projection
// assembly so the emitted shape stays consistent. Optional fields marked `?` in
// the contract (e.g. Unresolved.refs) keep their omitempty semantics and are
// left untouched.

// normalizeTask ensures every contract-documented array on a Task marshals as []
// rather than null.
func normalizeTask(t *model.Task) {
	if t.Repos == nil {
		t.Repos = []model.RepoRef{}
	}
	if t.Branches == nil {
		t.Branches = []model.Branch{}
	}
	if t.Worktrees == nil {
		t.Worktrees = []model.Worktree{}
	}
	if t.PRs == nil {
		t.PRs = []model.PullRequest{}
	}
	if t.Issues == nil {
		t.Issues = []model.Issue{}
	}
	if t.Sessions == nil {
		t.Sessions = []model.Session{}
	}
	if t.Links == nil {
		t.Links = []model.Link{}
	}
	if t.Unresolved == nil {
		t.Unresolved = []model.Unresolved{}
	}
	if t.Lineage.Parents == nil {
		t.Lineage.Parents = []string{}
	}
	if t.Relations.Depends == nil {
		t.Relations.Depends = []string{}
	}
	if t.Relations.Blocks == nil {
		t.Relations.Blocks = []string{}
	}
}

// normalizeBoard ensures every contract-documented array on a Board marshals as
// [] rather than null.
func normalizeBoard(b *model.Board) {
	if b.Questions == nil {
		b.Questions = []model.Question{}
	}
	if b.ADRs == nil {
		b.ADRs = []model.ADR{}
	}
	if b.Appraisals == nil {
		b.Appraisals = []model.Appraisal{}
	}
	for i := range b.ADRs {
		if b.ADRs[i].Alternatives == nil {
			b.ADRs[i].Alternatives = []string{}
		}
	}
}
