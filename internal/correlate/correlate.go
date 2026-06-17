// Package correlate is the pure correlation core: it groups raw discovery
// observations into stable Tasks. It is deterministic and performs no IO.
//
// Dependency rule: correlate imports ONLY internal/model. Anything it needs
// that is not a model type (e.g. the id resolver) is expressed as a local,
// consumer-defined interface over model types — it never imports the store.
package correlate

import (
	"fmt"
	"sort"
	"strings"

	"github.com/unsafe9/clutch/internal/model"
)

// IDResolver mints/resolves/anchors stable task ids from durable representation
// signatures, plus the lifecycle ops the correlation core needs. The file
// backend's store.IDRegistry satisfies this interface; correlate depends on this
// narrow local interface to stay model-pure. (Retire is registry maintenance
// and is intentionally NOT part of this interface.)
type IDResolver interface {
	// Resolve returns the existing id for sig, or ok=false if none is anchored.
	Resolve(sig model.Signature) (id string, ok bool, err error)
	// Mint anchors a new stable id to sig and returns it.
	Mint(sig model.Signature) (id string, err error)
	// Attach anchors an additional signature to an existing id.
	Attach(id string, sig model.Signature) error
	// Merge folds mergeID into keepID and returns the surviving id.
	Merge(keepID, mergeID string) (id string, err error)
}

// AppraisalReader reads back persisted appraisals so correlation can reuse a
// cached classify/relation result instead of recomputing it. It is a
// consumer-defined interface over model types; the file backend satisfies it.
type AppraisalReader interface {
	// Appraisals returns the cached appraisals persisted for taskID.
	Appraisals(taskID string) ([]model.Appraisal, error)
}

// Correlate is the deterministic projection step: raw observations, the id
// resolver, and the appraisal cache in, correlated Tasks out. Pure — no IO, no
// git/fs/LLM.
func Correlate(obs model.Observations, ids IDResolver, appraisals AppraisalReader) ([]model.Task, error) {
	b := newBuilder()

	if err := b.ingestGit(obs.Git, ids); err != nil {
		return nil, err
	}
	if err := b.ingestFS(obs.FS, ids); err != nil {
		return nil, err
	}
	b.ingestSessions(obs.Sessions)

	return b.finalize(appraisals)
}

// builder accumulates per-id task state while observations are ingested, plus
// the path → id index used to associate sessions and the branch-head index used
// to derive lineage parents.
type builder struct {
	byID  map[string]*model.Task
	order []string // first-seen id order, before final ID sort

	// pathToID maps a known repo/worktree path to the task id that owns it.
	pathToID map[string]string
	// branchHead maps "repo-identity@head" to the task id whose branch has that
	// head, used to resolve a child branch's Base to a parent task.
	branchHead map[string]string

	// unmatchedSessions are scan-wide session flags whose cwd matched no known
	// path; they have no owning task (TaskID stays empty).
	unmatchedSessions []model.Unresolved
}

func newBuilder() *builder {
	return &builder{
		byID:       map[string]*model.Task{},
		pathToID:   map[string]string{},
		branchHead: map[string]string{},
	}
}

// task returns the accumulating task for id, creating it (in first-seen order)
// on first use.
func (b *builder) task(id string) *model.Task {
	t, ok := b.byID[id]
	if !ok {
		t = &model.Task{ID: id, Provenance: model.ProvenanceGitDetected}
		b.byID[id] = t
		b.order = append(b.order, id)
	}
	return t
}

func (b *builder) ingestGit(gobs []model.GitObservation, ids IDResolver) error {
	for _, g := range gobs {
		identity := g.Repo.Identity

		if len(g.Branches) == 0 {
			// A repo with no branch context anchors a repo-level task.
			id, err := resolveOrMint(ids, model.Signature{Repo: identity})
			if err != nil {
				return err
			}
			t := b.task(id)
			b.addRepo(t, g.Repo)
			b.indexPath(g.Repo.Path, id)
			b.attachWorktrees(t, g.Worktrees, id)
			b.mergeCommits(t, g.Commits)
			b.attachPRs(t, g.PRs)
			b.attachIssues(t, g.Issues)
			continue
		}

		// Each branch anchors its own task (same signature => same id across
		// scans, so branches sharing a resolved id collapse into one task).
		for _, br := range g.Branches {
			sig := model.Signature{Repo: identity, Branch: br.Name}
			id, err := resolveOrMint(ids, sig)
			if err != nil {
				return err
			}
			t := b.task(id)
			b.addRepo(t, g.Repo)
			b.indexPath(g.Repo.Path, id)
			b.addBranch(t, br)
			b.indexBranchHead(identity, br.Head, id)
			b.mergeCommits(t, g.Commits)
			b.attachPRs(t, g.PRs)
			b.attachIssues(t, g.Issues)
			b.attachWorktrees(t, g.Worktrees, id)
		}
	}
	return nil
}

func (b *builder) ingestFS(fobs []model.FSObservation, ids IDResolver) error {
	for _, f := range fobs {
		identity := f.Repo.Identity

		// An fs-only repo with no git observation still yields a repo-level
		// task; a repo already discovered via git is enriched/confirmed.
		id, ok := b.idForPath(f.Repo.Path)
		if !ok {
			var err error
			id, err = resolveOrMint(ids, model.Signature{Repo: identity})
			if err != nil {
				return err
			}
		}
		t := b.task(id)
		b.addRepo(t, f.Repo)
		b.indexPath(f.Repo.Path, id)
		b.attachWorktrees(t, f.Worktrees, id)
	}
	return nil
}

func (b *builder) ingestSessions(sobs []model.SessionObservation) {
	for _, s := range sobs {
		sess := s.Session
		if id, ok := b.pathToID[sess.Cwd]; ok {
			b.addSession(b.task(id), sess)
			continue
		}
		// No known repo/worktree path matches the session cwd: emit an
		// unresolved flag rather than guessing an association.
		b.unmatchedSessions = append(b.unmatchedSessions, model.Unresolved{
			Kind:   model.UnresolvedSession,
			Detail: fmt.Sprintf("session cwd %q matched no known repo/worktree path", sess.Cwd),
			Refs:   []model.RepRef{sessionRef(sess)},
		})
	}
}

// ── representation helpers (pure projection) ──────────────────────────────────

func (b *builder) addRepo(t *model.Task, r model.RepoRef) {
	r.Ref = repoRef(r.Identity)
	for i := range t.Repos {
		if t.Repos[i].Ref == r.Ref {
			// Enrich/confirm an already-discovered repo without duplicating.
			if r.Path != "" {
				t.Repos[i].Path = r.Path
			}
			if r.Remote != "" {
				t.Repos[i].Remote = r.Remote
			}
			return
		}
	}
	t.Repos = append(t.Repos, r)
	b.addLink(t, r.Ref)
}

func (b *builder) addBranch(t *model.Task, br model.Branch) {
	br.Ref = branchRef(br.Repo, br.Name)
	for i := range t.Branches {
		if t.Branches[i].Ref == br.Ref {
			return
		}
	}
	t.Branches = append(t.Branches, br)
	b.addLink(t, br.Ref)
}

func (b *builder) attachWorktrees(t *model.Task, wts []model.Worktree, id string) {
	for _, w := range wts {
		w.Ref = worktreeRef(w.Path)
		b.indexPath(w.Path, id)
		exists := false
		for i := range t.Worktrees {
			if t.Worktrees[i].Ref == w.Ref {
				// FS enriches/confirms an already-discovered worktree.
				if w.Branch != "" {
					t.Worktrees[i].Branch = w.Branch
				}
				if w.Repo != "" {
					t.Worktrees[i].Repo = w.Repo
				}
				exists = true
				break
			}
		}
		if !exists {
			t.Worktrees = append(t.Worktrees, w)
			b.addLink(t, w.Ref)
		}
	}
}

func (b *builder) mergeCommits(t *model.Task, c model.CommitSummary) {
	// Keep the first non-zero head seen and roll up counts across observations
	// folded into this task.
	if t.Commits.Head == "" {
		t.Commits.Head = c.Head
	}
	t.Commits.Count += c.Count
}

func (b *builder) attachPRs(t *model.Task, prs []model.PullRequest) {
	for _, pr := range prs {
		pr.Ref = prRef(pr.Host, pr.Number)
		dup := false
		for i := range t.PRs {
			if t.PRs[i].Ref == pr.Ref {
				dup = true
				break
			}
		}
		if dup {
			continue
		}
		t.PRs = append(t.PRs, pr)
		b.addLink(t, pr.Ref)
	}
}

func (b *builder) attachIssues(t *model.Task, issues []model.Issue) {
	for _, is := range issues {
		is.Ref = issueRef(is.Tracker, is.Key)
		dup := false
		for i := range t.Issues {
			if t.Issues[i].Ref == is.Ref {
				dup = true
				break
			}
		}
		if dup {
			continue
		}
		t.Issues = append(t.Issues, is)
		b.addLink(t, is.Ref)
	}
}

func (b *builder) addSession(t *model.Task, s model.Session) {
	s.Ref = sessionRef(s)
	for i := range t.Sessions {
		if t.Sessions[i].Ref == s.Ref {
			return
		}
	}
	t.Sessions = append(t.Sessions, s)
	// A convention-method link is full confidence: the cwd path match is
	// deterministic, not appraised.
	t.Links = append(t.Links, model.Link{Subject: s.Ref, Method: model.LinkConvention, Confidence: 1.0})
}

// addLink records, at full confidence, that a representation was associated to
// the task by deterministic convention.
func (b *builder) addLink(t *model.Task, subject model.RepRef) {
	t.Links = append(t.Links, model.Link{Subject: subject, Method: model.LinkConvention, Confidence: 1.0})
}

func (b *builder) indexPath(path, id string) {
	if path == "" {
		return
	}
	if _, ok := b.pathToID[path]; !ok {
		b.pathToID[path] = id
	}
}

func (b *builder) idForPath(path string) (string, bool) {
	if path == "" {
		return "", false
	}
	id, ok := b.pathToID[path]
	return id, ok
}

func (b *builder) indexBranchHead(repoIdentity, head, id string) {
	if head == "" {
		return
	}
	b.branchHead[repoIdentity+"@"+head] = id
}

// ── finalize: lineage, appraisal fold, deterministic ordering ─────────────────

func (b *builder) finalize(appraisals AppraisalReader) ([]model.Task, error) {
	// Lineage: a branch's Base mapping to another task's branch head makes that
	// other task a parent.
	for _, id := range b.order {
		t := b.byID[id]
		seen := map[string]bool{}
		for _, br := range t.Branches {
			if br.Base == "" {
				continue
			}
			parent, ok := b.branchHead[br.Repo+"@"+br.Base]
			if !ok || parent == id || seen[parent] {
				continue
			}
			seen[parent] = true
			t.Lineage.Parents = append(t.Lineage.Parents, parent)
		}
		sort.Strings(t.Lineage.Parents)
	}

	out := make([]model.Task, 0, len(b.order))
	for _, id := range b.order {
		t := b.byID[id]

		t.Lifecycle = deriveLifecycle(t)
		t.Title = deriveTitle(t)

		if err := foldAppraisals(t, appraisals); err != nil {
			return nil, err
		}

		sortReps(t)
		out = append(out, *t)
	}

	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })

	// Scan-wide unmatched-session flags have no owning task. The contract models
	// such flags with an empty TaskID; the only Task-bound carrier is a task, so
	// they are attached to the lexically-first task (after the ID sort) for
	// deterministic surfacing, with TaskID left empty to mark them scan-wide.
	if len(b.unmatchedSessions) > 0 && len(out) > 0 {
		out[0].Unresolved = append(out[0].Unresolved, b.unmatchedSessions...)
		sortReps(&out[0])
	}

	return out, nil
}

// foldAppraisals applies cached classify/relation/link appraisals — the only
// sub-1.0-confidence inputs — onto the task.
func foldAppraisals(t *model.Task, appraisals AppraisalReader) error {
	if appraisals == nil {
		return nil
	}
	apps, err := appraisals.Appraisals(t.ID)
	if err != nil {
		return err
	}
	sort.SliceStable(apps, func(i, j int) bool {
		if apps[i].Kind != apps[j].Kind {
			return apps[i].Kind < apps[j].Kind
		}
		return apps[i].Subject < apps[j].Subject
	})
	for _, a := range apps {
		switch a.Kind {
		case model.AppraisalClassification:
			// A cached classification overrides the deterministic default
			// lifecycle when present.
			if a.Result != "" {
				t.Lifecycle = model.Lifecycle(a.Result)
			}
		case model.AppraisalRelation:
			applyRelationAppraisal(t, a)
		case model.AppraisalLink:
			t.Links = append(t.Links, model.Link{
				Subject:    a.Subject,
				Method:     model.LinkAppraisal,
				Confidence: a.Confidence,
			})
		default:
			// Extensible kind we do not recognize: tolerate by ignoring.
		}
	}
	return nil
}

// applyRelationAppraisal decodes a relation appraisal Result of the form
// "depends:<taskID>" or "blocks:<taskID>" into the task's Relations.
func applyRelationAppraisal(t *model.Task, a model.Appraisal) {
	kind, target, ok := strings.Cut(a.Result, ":")
	if !ok || target == "" {
		return
	}
	switch kind {
	case "depends":
		if !contains(t.Relations.Depends, target) {
			t.Relations.Depends = append(t.Relations.Depends, target)
			sort.Strings(t.Relations.Depends)
		}
	case "blocks":
		if !contains(t.Relations.Blocks, target) {
			t.Relations.Blocks = append(t.Relations.Blocks, target)
			sort.Strings(t.Relations.Blocks)
		}
	}
}

// deriveLifecycle is the documented deterministic mapping from integration/PR
// state to a lifecycle, used before any appraisal override:
//
//	any PR merged                          → merged
//	else any PR under review               → review
//	  (open & non-draft, OR review_decision is changes_requested/review_required)
//	else any PR open & draft               → planned
//	else any branch merged                 → merged
//	else any branch has a head             → active
//	else any commits                       → active
//	else                                   → idea
//
// A review_decision of changes_requested/review_required means external review is
// in flight and the worker must act on it, so it counts as review even on a draft.
func deriveLifecycle(t *model.Task) model.Lifecycle {
	hasOpenDraft, hasReview, hasMergedPR := false, false, false
	for _, pr := range t.PRs {
		switch pr.ReviewDecision {
		case "changes_requested", "review_required":
			hasReview = true
		}
		switch strings.ToLower(pr.State) {
		case "open":
			if pr.Draft {
				hasOpenDraft = true
			} else {
				hasReview = true
			}
		case "merged":
			hasMergedPR = true
		}
	}
	switch {
	case hasMergedPR:
		return model.LifecycleMerged
	case hasReview:
		return model.LifecycleReview
	case hasOpenDraft:
		return model.LifecyclePlanned
	}
	for _, br := range t.Branches {
		if br.Integration == model.IntegrationMerged {
			return model.LifecycleMerged
		}
	}
	for _, br := range t.Branches {
		if br.Head != "" {
			return model.LifecycleActive
		}
	}
	if t.Commits.Head != "" {
		return model.LifecycleActive
	}
	return model.LifecycleIdea
}

// deriveTitle picks a human label from the available representations, preferring
// the most specific: branch name, then issue key, then repo identity.
func deriveTitle(t *model.Task) string {
	if len(t.Branches) > 0 {
		return t.Branches[0].Name
	}
	if len(t.Issues) > 0 {
		return t.Issues[0].Key
	}
	if len(t.Repos) > 0 {
		return t.Repos[0].Identity
	}
	return ""
}

// sortReps sorts every representation slice and the link/unresolved slices by a
// stable key so the projection is reproducible across runs.
func sortReps(t *model.Task) {
	sort.SliceStable(t.Repos, func(i, j int) bool { return t.Repos[i].Ref < t.Repos[j].Ref })
	sort.SliceStable(t.Branches, func(i, j int) bool { return t.Branches[i].Ref < t.Branches[j].Ref })
	sort.SliceStable(t.Worktrees, func(i, j int) bool { return t.Worktrees[i].Ref < t.Worktrees[j].Ref })
	sort.SliceStable(t.PRs, func(i, j int) bool { return t.PRs[i].Ref < t.PRs[j].Ref })
	sort.SliceStable(t.Issues, func(i, j int) bool { return t.Issues[i].Ref < t.Issues[j].Ref })
	sort.SliceStable(t.Sessions, func(i, j int) bool { return t.Sessions[i].Ref < t.Sessions[j].Ref })
	sort.SliceStable(t.Links, func(i, j int) bool {
		if t.Links[i].Subject != t.Links[j].Subject {
			return t.Links[i].Subject < t.Links[j].Subject
		}
		return t.Links[i].Method < t.Links[j].Method
	})
	sort.SliceStable(t.Unresolved, func(i, j int) bool {
		if t.Unresolved[i].Kind != t.Unresolved[j].Kind {
			return t.Unresolved[i].Kind < t.Unresolved[j].Kind
		}
		return t.Unresolved[i].Detail < t.Unresolved[j].Detail
	})
}

// ── small pure utilities ──────────────────────────────────────────────────────

func resolveOrMint(ids IDResolver, sig model.Signature) (string, error) {
	if id, ok, err := ids.Resolve(sig); err != nil {
		return "", err
	} else if ok {
		return id, nil
	}
	return ids.Mint(sig)
}

func contains(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}

// RepRef key constructors per the contract scheme.

func repoRef(identity string) model.RepRef { return model.RepRef("repo:" + identity) }

func branchRef(repoIdentity, name string) model.RepRef {
	return model.RepRef("branch:" + repoIdentity + "/" + name)
}

func worktreeRef(path string) model.RepRef { return model.RepRef("worktree:" + path) }

func prRef(host string, number int) model.RepRef {
	return model.RepRef(fmt.Sprintf("pr:%s#%d", host, number))
}

func issueRef(tracker, key string) model.RepRef {
	return model.RepRef("issue:" + tracker + "/" + key)
}

func sessionRef(s model.Session) model.RepRef {
	return model.RepRef("session:" + s.Host + "/" + s.Cwd)
}
