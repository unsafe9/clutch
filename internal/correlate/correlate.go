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

// DesignReader reports whether a task's board carries a non-empty design — the
// board-visibility signal correlation needs to derive the `planned` lifecycle (a
// task with no git activity of its own but a board design was planned, not left
// an idea) and to suppress the new-vs-merged classification flag when a design
// already resolves the ambiguity. It is a consumer-defined interface over model
// types; the file backend satisfies it.
type DesignReader interface {
	// HasDesign reports whether taskID's board design is non-empty.
	HasDesign(taskID string) (bool, error)
}

// InitiatedTaskReader lists clutch-initiated tasks — ones created directly
// through the CLI (`clutch task new`) that may have no git/fs/session
// representation yet. Correlation materializes any that no observation produced
// so a freshly-created task still projects. It is a consumer-defined interface
// over model types; the file backend satisfies it.
type InitiatedTaskReader interface {
	// InitiatedTasks returns the persisted clutch-initiated tasks.
	InitiatedTasks() ([]model.InitiatedTask, error)
}

// Result is the correlation output: the projected tasks plus the scan-wide
// unresolved flags that belong to no single task (e.g. an in-scope session whose
// cwd matched no repo/worktree). Per-task flags live on each Task.Unresolved;
// scan-wide flags are returned here separately rather than parked on an arbitrary
// task. The envelope's diagnostics.unresolved is the union of the two.
type Result struct {
	Tasks    []model.Task
	ScanWide []model.Unresolved
}

// Correlate is the deterministic projection step: raw observations, the id
// resolver, the appraisal cache, the board-design visibility seam, and the
// clutch-initiated task set in, correlated Tasks out. Pure — no IO, no
// git/fs/LLM.
func Correlate(obs model.Observations, ids IDResolver, appraisals AppraisalReader, designs DesignReader, initiated InitiatedTaskReader) (Result, error) {
	b := newBuilder()

	if err := b.ingestGit(obs.Git, ids); err != nil {
		return Result{}, err
	}
	if err := b.ingestFS(obs.FS, ids); err != nil {
		return Result{}, err
	}
	b.ingestSessions(obs.Sessions)

	tasks, err := b.finalize(appraisals, designs, initiated)
	if err != nil {
		return Result{}, err
	}
	scanWide := b.unmatchedSessions
	sortUnresolved(scanWide)
	return Result{Tasks: tasks, ScanWide: scanWide}, nil
}

// builder accumulates per-id task state while observations are ingested, plus
// the path → id index used to associate sessions and the branch-head index used
// to derive lineage parents.
type builder struct {
	byID  map[string]*model.Task
	order []string // first-seen id order, before final ID sort

	// pathToID maps a known repo/worktree path to the task id that owns it
	// (first-wins), the cwd-only session-routing fallback.
	pathToID map[string]string
	// pathToRepo maps a known repo/worktree path to its durable repo identity
	// (first-wins, so the git remote identity set first wins over the fs one),
	// used to resolve a session cwd or fs worktree to the right branch-task.
	pathToRepo map[string]string
	// branchHead maps "repo-identity@head" to the task id whose branch has that
	// head, used to resolve a child branch's Base to a parent task.
	branchHead map[string]string
	// branchNameToID maps "repo-identity@branch-name" to the branch-task id, so a
	// session or worktree can bind to the task of the branch it is actually on.
	branchNameToID map[string]string
	// reposWithBranches records repo identities that anchored branch-tasks, so fs
	// worktrees route by branch there and to the single repo-level task elsewhere.
	reposWithBranches map[string]bool

	// unmatchedSessions are scan-wide session flags whose cwd matched no known
	// path; they have no owning task (TaskID stays empty).
	unmatchedSessions []model.Unresolved
}

func newBuilder() *builder {
	return &builder{
		byID:              map[string]*model.Task{},
		pathToID:          map[string]string{},
		pathToRepo:        map[string]string{},
		branchHead:        map[string]string{},
		branchNameToID:    map[string]string{},
		reposWithBranches: map[string]bool{},
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
			// A repo with no branch context anchors a repo-level task; with no
			// branch-tasks to route by, every worktree attaches to it.
			id, err := resolveOrMint(ids, model.Signature{Repo: identity})
			if err != nil {
				return err
			}
			t := b.task(id)
			b.addRepo(t, g.Repo)
			b.indexPath(g.Repo.Path, id)
			b.setPathRepo(g.Repo.Path, identity)
			b.attachWorktreesTo(t, g.Worktrees, id)
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
			b.setPathRepo(g.Repo.Path, identity)
			b.addBranch(t, br)
			b.indexBranchHead(identity, br.Head, id)
			b.indexBranchName(identity, br.Name, id)
			b.mergeCommits(t, g.Commits)
			b.attachPRs(t, g.PRs)
			b.attachIssues(t, g.Issues)
		}
		// Attach each worktree to the task of the branch checked out in it —
		// after all branch-tasks exist — rather than to every branch-task.
		b.attachWorktreesByBranch(identity, g.Worktrees)
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
		b.setPathRepo(f.Repo.Path, identity)

		// The fs identity (local/<base>) diverges from the git remote identity for
		// the same checkout; branch-tasks were keyed by the git identity, so route
		// by the identity established for this path. A git-discovered repo routes
		// its fs worktrees by branch; a purely fs-only repo has one repo-level task.
		routeIdentity := identity
		if gitID, ok := b.pathToRepo[f.Repo.Path]; ok {
			routeIdentity = gitID
		}
		if b.reposWithBranches[routeIdentity] {
			b.attachWorktreesByBranch(routeIdentity, f.Worktrees)
		} else {
			b.attachWorktreesTo(t, f.Worktrees, id)
		}
	}
	return nil
}

func (b *builder) ingestSessions(sobs []model.SessionObservation) {
	for _, s := range sobs {
		sess := s.Session
		if id, ok := b.sessionTaskID(sess); ok {
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

// sessionTaskID routes a session to its task. When the session records a branch,
// it binds to the task owning that branch at the cwd's repo; cwd-only routing
// (first-wins) is the fallback when the branch is absent or matches no branch-task
// at that repo.
func (b *builder) sessionTaskID(sess model.Session) (string, bool) {
	if sess.Branch != "" {
		if identity, ok := b.pathToRepo[sess.Cwd]; ok {
			if id, ok := b.branchNameToID[identity+"@"+sess.Branch]; ok {
				return id, true
			}
		}
	}
	id, ok := b.pathToID[sess.Cwd]
	return id, ok
}

// ── representation helpers (pure projection) ──────────────────────────────────

func (b *builder) addRepo(t *model.Task, r model.RepoRef) {
	r.Ref = repoRef(r.Identity)
	for i := range t.Repos {
		existing := &t.Repos[i]
		// The same checkout is discovered by both producers under DIVERGENT
		// identities — git's remote identity (e.g. github.com/acme/app) and fs's
		// path-based local/<base> — that only the shared path unifies. Collapse by
		// identity OR by path so a remote-backed repo yields ONE rep, not two.
		if existing.Ref == r.Ref || (r.Path != "" && existing.Path == r.Path) {
			b.mergeRepo(t, existing, r)
			return
		}
	}
	t.Repos = append(t.Repos, r)
	b.addLink(t, r.Ref)
}

// mergeRepo folds another observation of an already-recorded repo (matched by
// identity or by shared checkout path) into the surviving rep. The remote
// identity is the durable one and wins over the fs local/<base> identity: when
// the two disagree, the rep carrying a remote is kept and the other's convention
// link is re-keyed to it, so no spurious second rep or link survives.
func (b *builder) mergeRepo(t *model.Task, existing *model.RepoRef, r model.RepoRef) {
	if r.Remote != "" && existing.Remote == "" && existing.Ref != r.Ref {
		// Incoming carries the durable remote identity; it supersedes the
		// path-based rep. Preserve the known path, re-key the existing link.
		b.rekeyLink(t, existing.Ref, r.Ref)
		if r.Path == "" {
			r.Path = existing.Path
		}
		*existing = r
		return
	}
	// Existing identity survives; enrich any fields it is missing.
	if r.Path != "" {
		existing.Path = r.Path
	}
	if r.Remote != "" {
		existing.Remote = r.Remote
	}
}

// rekeyLink repoints every convention link whose subject is oldRef to newRef,
// used when a repo rep's identity is superseded so its link is not left dangling.
func (b *builder) rekeyLink(t *model.Task, oldRef, newRef model.RepRef) {
	for i := range t.Links {
		if t.Links[i].Subject == oldRef {
			t.Links[i].Subject = newRef
		}
	}
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

// attachWorktreesByBranch attaches each worktree to the task that owns the branch
// checked out in it, so a worktree rep lands only on that branch's task instead of
// on every branch-task of the repo. A worktree whose branch matches no branch-task
// (detached HEAD, or a branch not enumerated) is left unattached.
func (b *builder) attachWorktreesByBranch(identity string, wts []model.Worktree) {
	for _, w := range wts {
		id, ok := b.branchNameToID[identity+"@"+w.Branch]
		if !ok {
			continue
		}
		b.attachWorktree(b.task(id), w, id)
	}
}

// attachWorktreesTo attaches every worktree to one task, used for a repo-level
// task that has no branch-tasks to route by.
func (b *builder) attachWorktreesTo(t *model.Task, wts []model.Worktree, id string) {
	for _, w := range wts {
		b.attachWorktree(t, w, id)
	}
}

// attachWorktree records one worktree rep on a task (deduping by ref, enriching an
// already-seen one) and indexes its path so a session in that worktree can route.
func (b *builder) attachWorktree(t *model.Task, w model.Worktree, id string) {
	w.Ref = worktreeRef(w.Path)
	b.indexPath(w.Path, id)
	b.setPathRepo(w.Path, w.Repo)
	for i := range t.Worktrees {
		if t.Worktrees[i].Ref == w.Ref {
			// FS enriches/confirms an already-discovered worktree.
			if w.Branch != "" {
				t.Worktrees[i].Branch = w.Branch
			}
			if w.Repo != "" {
				t.Worktrees[i].Repo = w.Repo
			}
			return
		}
	}
	t.Worktrees = append(t.Worktrees, w)
	b.addLink(t, w.Ref)
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

// setPathRepo records the durable repo identity for a path (first-wins, so the
// git remote identity set during ingestGit is not overwritten by the fs one).
func (b *builder) setPathRepo(path, identity string) {
	if path == "" || identity == "" {
		return
	}
	if _, ok := b.pathToRepo[path]; !ok {
		b.pathToRepo[path] = identity
	}
}

// indexBranchName maps a repo's branch name to its branch-task id (first-wins) and
// marks the repo as having branch-tasks, so sessions and fs worktrees can route by
// branch.
func (b *builder) indexBranchName(repoIdentity, name, id string) {
	b.reposWithBranches[repoIdentity] = true
	key := repoIdentity + "@" + name
	if _, ok := b.branchNameToID[key]; !ok {
		b.branchNameToID[key] = id
	}
}

// ── finalize: lineage, appraisal fold, deterministic ordering ─────────────────

func (b *builder) finalize(appraisals AppraisalReader, designs DesignReader, initiated InitiatedTaskReader) ([]model.Task, error) {
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

		// An undiverged branch (tip == its merge-base with base) reads as `merged`
		// deterministically, but git cannot tell a freshly-branched task from a
		// fully-merged one. The board resolves the ambiguity: a non-empty design
		// means the task was planned, not merged. A merged PR or a cached
		// classification appraisal is a definitive signal that pre-empts the
		// heuristic.
		undiverged := undivergedBranches(t)
		ambiguous := t.Lifecycle == model.LifecycleMerged &&
			len(undiverged) > 0 && !anyMergedPR(t)
		hasDesign := false
		if ambiguous && designs != nil {
			has, err := designs.HasDesign(t.ID)
			if err != nil {
				return nil, err
			}
			hasDesign = has
		}

		classified, err := foldAppraisals(t, appraisals)
		if err != nil {
			return nil, err
		}

		switch {
		case !ambiguous || classified:
			// A definitive derivation or classify's folded verdict already
			// resolves the task: keep the lifecycle, emit no flag.
		case hasDesign:
			// The board design resolves the ambiguity: planned, not merged.
			t.Lifecycle = model.LifecyclePlanned
		default:
			// Nothing resolves it: keep the deterministic `merged` default and flag
			// the new-vs-merged ambiguity so `classify` can judge and persist a
			// verdict, which suppresses this flag on later scans.
			t.Unresolved = append(t.Unresolved, model.Unresolved{
				Kind:   model.UnresolvedClassification,
				Detail: "branch tip equals its merge-base with base: cannot distinguish a branch freshly cut at the base tip from a fully merged one; classify to resolve",
				Refs:   undiverged,
				TaskID: t.ID,
			})
		}

		sortReps(t)
		out = append(out, *t)
	}

	// Materialize clutch-initiated tasks that no observation produced, so a
	// freshly-created `clutch task new` task still projects. Such a task is
	// registry-only: its Class ② representations stay empty until a branch is
	// later linked to the id. It starts at the idea lifecycle, or planned once its
	// board carries a design (the task has been planned, not merely conceived).
	if initiated != nil {
		its, err := initiated.InitiatedTasks()
		if err != nil {
			return nil, err
		}
		for _, it := range its {
			if _, built := b.byID[it.ID]; built {
				continue // an observation already produced a task for this id
			}
			lifecycle := model.LifecycleIdea
			if designs != nil {
				hasDesign, err := designs.HasDesign(it.ID)
				if err != nil {
					return nil, err
				}
				if hasDesign {
					lifecycle = model.LifecyclePlanned
				}
			}
			out = append(out, model.Task{
				ID:         it.ID,
				Title:      it.Title,
				Lifecycle:  lifecycle,
				Mode:       it.Mode,
				Provenance: model.ProvenanceClutchInitiated,
				Created:    it.Created,
				Updated:    it.Created,
			})
		}
	}

	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })

	return out, nil
}

// foldAppraisals applies cached classify/relation/link appraisals — the only
// sub-1.0-confidence inputs — onto the task. It reports whether a classification
// appraisal was folded, so the board-driven derivation can defer to classify's
// explicit verdict (see finalize).
func foldAppraisals(t *model.Task, appraisals AppraisalReader) (classified bool, err error) {
	if appraisals == nil {
		return false, nil
	}
	apps, err := appraisals.Appraisals(t.ID)
	if err != nil {
		return false, err
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
			// A classification is a task-level judgment keyed by the task:<id>
			// subject (the store upsert guarantees exactly one per task). Ignore
			// any whose subject is not this task; a matching one overrides the
			// deterministic default lifecycle.
			if a.Subject == taskRef(t.ID) && a.Result != "" {
				t.Lifecycle = model.Lifecycle(a.Result)
				classified = true
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
	return classified, nil
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

// undivergedBranches returns the refs of the task's branches whose tip equals
// their merge-base with base — the deterministic layer records this as
// IntegrationMerged, but it cannot tell a new branch sitting at the base tip from
// one that was genuinely merged. Empty when the task has no such branch.
func undivergedBranches(t *model.Task) []model.RepRef {
	var refs []model.RepRef
	for _, br := range t.Branches {
		if br.Integration == model.IntegrationMerged {
			refs = append(refs, br.Ref)
		}
	}
	return refs
}

// anyMergedPR reports whether the task has a merged pull request — a definitive
// merged signal, unlike the ambiguous branch-integration case.
func anyMergedPR(t *model.Task) bool {
	for _, pr := range t.PRs {
		if strings.ToLower(pr.State) == "merged" {
			return true
		}
	}
	return false
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
	sortUnresolved(t.Unresolved)
}

// sortUnresolved orders unresolved flags by (kind, detail) so the projection is
// reproducible across runs. Shared by per-task sorting and the scan-wide list.
func sortUnresolved(u []model.Unresolved) {
	sort.SliceStable(u, func(i, j int) bool {
		if u[i].Kind != u[j].Kind {
			return u[i].Kind < u[j].Kind
		}
		return u[i].Detail < u[j].Detail
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
	return model.RepRef("session:" + s.Host + "/" + s.ID)
}

// taskRef keys the task itself, the subject of a classification appraisal.
func taskRef(id string) model.RepRef { return model.RepRef("task:" + id) }
