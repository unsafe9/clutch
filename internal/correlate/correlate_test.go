package correlate

import (
	"reflect"
	"testing"

	"github.com/unsafe9/clutch/internal/model"
)

// fakeIDs is a map-backed IDResolver. Mint assigns ids deterministically by
// minting order so tests can assert reuse vs mint.
type fakeIDs struct {
	anchored map[model.Signature]string
	minted   []model.Signature
	next     int
}

func newFakeIDs() *fakeIDs {
	return &fakeIDs{anchored: map[model.Signature]string{}, next: 1}
}

func (f *fakeIDs) seed(sig model.Signature, id string) { f.anchored[sig] = id }

func (f *fakeIDs) Resolve(sig model.Signature) (string, bool, error) {
	id, ok := f.anchored[sig]
	return id, ok, nil
}

func (f *fakeIDs) Mint(sig model.Signature) (string, error) {
	id := "T" + itoa(f.next)
	f.next++
	f.anchored[sig] = id
	f.minted = append(f.minted, sig)
	return id, nil
}

func (f *fakeIDs) Attach(id string, sig model.Signature) error  { f.anchored[sig] = id; return nil }
func (f *fakeIDs) Merge(keepID, mergeID string) (string, error) { return keepID, nil }

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

// fakeAppraisals returns canned appraisals per task id.
type fakeAppraisals struct {
	byID map[string][]model.Appraisal
}

func (f fakeAppraisals) Appraisals(taskID string) ([]model.Appraisal, error) {
	return f.byID[taskID], nil
}

func gitObs(identity, path string, branches ...model.Branch) model.GitObservation {
	return model.GitObservation{
		Repo:     model.RepoRef{Identity: identity, Path: path, Remote: "git@github.com:" + identity + ".git"},
		Branches: branches,
	}
}

func TestEmptyObservations(t *testing.T) {
	got, err := Correlate(model.Observations{}, newFakeIDs(), fakeAppraisals{})
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if got == nil {
		t.Fatal("want non-nil empty slice, got nil")
	}
	if len(got) != 0 {
		t.Fatalf("want 0 tasks, got %d", len(got))
	}
}

func TestGroupByBranchSignature_MintAndReuse(t *testing.T) {
	ids := newFakeIDs()
	// Pre-anchor one branch so its id is reused; the other is minted.
	ids.seed(model.Signature{Repo: "acme/app", Branch: "main"}, "EXISTING")

	obs := model.Observations{
		Git: []model.GitObservation{
			gitObs("acme/app", "/repos/app",
				model.Branch{Repo: "acme/app", Name: "main", Head: "aaa"},
				model.Branch{Repo: "acme/app", Name: "feature/x", Head: "bbb"},
			),
		},
	}

	got, err := Correlate(obs, ids, fakeAppraisals{})
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 tasks, got %d: %+v", len(got), got)
	}
	// IDs sorted: EXISTING < T1.
	if got[0].ID != "EXISTING" || got[1].ID != "T1" {
		t.Fatalf("ids = %q, %q; want EXISTING, T1", got[0].ID, got[1].ID)
	}
	// Exactly one mint happened (feature/x).
	if len(ids.minted) != 1 {
		t.Fatalf("want 1 mint, got %d: %+v", len(ids.minted), ids.minted)
	}
	if ids.minted[0] != (model.Signature{Repo: "acme/app", Branch: "feature/x"}) {
		t.Fatalf("minted sig = %+v", ids.minted[0])
	}
	// Branch ref and convention link present.
	want := model.RepRef("branch:acme/app/main")
	if got[0].Branches[0].Ref != want {
		t.Fatalf("branch ref = %q, want %q", got[0].Branches[0].Ref, want)
	}
	foundLink := false
	for _, l := range got[0].Links {
		if l.Subject == want && l.Method == model.LinkConvention && l.Confidence == 1.0 {
			foundLink = true
		}
	}
	if !foundLink {
		t.Fatalf("missing convention link for %q: %+v", want, got[0].Links)
	}
}

func TestRepoAnchorAndFSOnly(t *testing.T) {
	ids := newFakeIDs()
	obs := model.Observations{
		// Git repo with no branches => repo-level task.
		Git: []model.GitObservation{
			{Repo: model.RepoRef{Identity: "acme/lib", Path: "/repos/lib"}},
		},
		// FS-only repo => repo-level task.
		FS: []model.FSObservation{
			{Repo: model.RepoRef{Identity: "acme/tool", Path: "/repos/tool"},
				Worktrees: []model.Worktree{{Path: "/repos/tool", Branch: "main", Repo: "acme/tool"}}},
		},
	}
	got, err := Correlate(obs, ids, fakeAppraisals{})
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 tasks, got %d", len(got))
	}
	for _, tk := range got {
		if len(tk.Repos) != 1 {
			t.Fatalf("task %s: want 1 repo, got %d", tk.ID, len(tk.Repos))
		}
		if tk.Repos[0].Ref != model.RepRef("repo:"+tk.Repos[0].Identity) {
			t.Fatalf("bad repo ref %q", tk.Repos[0].Ref)
		}
	}
}

func TestFSEnrichesGitRepoNoNewTask(t *testing.T) {
	ids := newFakeIDs()
	obs := model.Observations{
		Git: []model.GitObservation{
			gitObs("acme/app", "/repos/app", model.Branch{Repo: "acme/app", Name: "main", Head: "aaa"}),
		},
		// Same path as the git repo: must enrich, not mint a new task.
		FS: []model.FSObservation{
			{Repo: model.RepoRef{Identity: "acme/app", Path: "/repos/app", Remote: "ssh://x"},
				Worktrees: []model.Worktree{{Path: "/repos/app/wt", Branch: "feature/x", Repo: "acme/app"}}},
		},
	}
	got, err := Correlate(obs, ids, fakeAppraisals{})
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("want 1 task, got %d: %+v", len(got), got)
	}
	if len(got[0].Worktrees) != 1 {
		t.Fatalf("want worktree from fs enrichment, got %+v", got[0].Worktrees)
	}
	if got[0].Repos[0].Remote != "ssh://x" {
		t.Fatalf("repo remote not enriched: %q", got[0].Repos[0].Remote)
	}
}

func TestSessionAssociationAndUnresolved(t *testing.T) {
	ids := newFakeIDs()
	obs := model.Observations{
		Git: []model.GitObservation{
			gitObs("acme/app", "/repos/app", model.Branch{Repo: "acme/app", Name: "main", Head: "aaa"}),
		},
		Sessions: []model.SessionObservation{
			// Matches the repo path => associated.
			{Session: model.Session{Host: "claude-code", Cwd: "/repos/app"}},
			// Matches nothing => unresolved.
			{Session: model.Session{Host: "codex", Cwd: "/elsewhere"}},
		},
	}
	got, err := Correlate(obs, ids, fakeAppraisals{})
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("want 1 task, got %d", len(got))
	}
	tk := got[0]
	if len(tk.Sessions) != 1 {
		t.Fatalf("want 1 associated session, got %+v", tk.Sessions)
	}
	wantRef := model.RepRef("session:claude-code//repos/app")
	if tk.Sessions[0].Ref != wantRef {
		t.Fatalf("session ref = %q, want %q", tk.Sessions[0].Ref, wantRef)
	}
	// Convention link of confidence 1.0 for the session.
	foundLink := false
	for _, l := range tk.Links {
		if l.Subject == wantRef && l.Method == model.LinkConvention && l.Confidence == 1.0 {
			foundLink = true
		}
	}
	if !foundLink {
		t.Fatalf("missing session convention link: %+v", tk.Links)
	}
	// Unresolved session flag surfaced.
	foundUnres := false
	for _, u := range tk.Unresolved {
		if u.Kind == model.UnresolvedSession {
			foundUnres = true
			if u.TaskID != "" {
				t.Fatalf("scan-wide flag should have empty TaskID, got %q", u.TaskID)
			}
		}
	}
	if !foundUnres {
		t.Fatalf("missing unresolved-session flag: %+v", tk.Unresolved)
	}
}

func TestAppraisalFold(t *testing.T) {
	ids := newFakeIDs()
	ids.seed(model.Signature{Repo: "acme/app", Branch: "main"}, "T-app")
	obs := model.Observations{
		Git: []model.GitObservation{
			gitObs("acme/app", "/repos/app", model.Branch{Repo: "acme/app", Name: "main", Head: "aaa"}),
		},
	}
	appr := fakeAppraisals{byID: map[string][]model.Appraisal{
		"T-app": {
			{Kind: model.AppraisalClassification, Result: "stale", Confidence: 0.7},
			{Kind: model.AppraisalRelation, Result: "depends:T-other", Confidence: 0.6},
			{Kind: model.AppraisalLink, Subject: "branch:acme/app/main", Confidence: 0.8},
			{Kind: model.AppraisalKind("future"), Result: "ignore-me", Confidence: 0.5},
		},
	}}
	got, err := Correlate(obs, ids, appr)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	tk := got[0]
	if tk.Lifecycle != model.LifecycleStale {
		t.Fatalf("classification not applied: lifecycle = %q", tk.Lifecycle)
	}
	if !reflect.DeepEqual(tk.Relations.Depends, []string{"T-other"}) {
		t.Fatalf("relation not applied: %+v", tk.Relations)
	}
	foundAppraisalLink := false
	for _, l := range tk.Links {
		if l.Method == model.LinkAppraisal && l.Subject == "branch:acme/app/main" && l.Confidence == 0.8 {
			foundAppraisalLink = true
		}
	}
	if !foundAppraisalLink {
		t.Fatalf("appraisal link not added: %+v", tk.Links)
	}
}

func TestLineageFromBranchBase(t *testing.T) {
	ids := newFakeIDs()
	ids.seed(model.Signature{Repo: "acme/app", Branch: "main"}, "T-parent")
	ids.seed(model.Signature{Repo: "acme/app", Branch: "feature/x"}, "T-child")
	obs := model.Observations{
		Git: []model.GitObservation{
			gitObs("acme/app", "/repos/app",
				model.Branch{Repo: "acme/app", Name: "main", Head: "PARENTHEAD"},
				model.Branch{Repo: "acme/app", Name: "feature/x", Head: "childhead", Base: "PARENTHEAD"},
			),
		},
	}
	got, err := Correlate(obs, ids, fakeAppraisals{})
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	var child model.Task
	for _, tk := range got {
		if tk.ID == "T-child" {
			child = tk
		}
	}
	if !reflect.DeepEqual(child.Lineage.Parents, []string{"T-parent"}) {
		t.Fatalf("lineage parents = %+v, want [T-parent]", child.Lineage.Parents)
	}
}

func TestLifecycleFromPRState(t *testing.T) {
	ids := newFakeIDs()
	obs := model.Observations{
		Git: []model.GitObservation{
			{
				Repo:     model.RepoRef{Identity: "acme/app", Path: "/repos/app"},
				Branches: []model.Branch{{Repo: "acme/app", Name: "main", Head: "aaa"}},
				PRs:      []model.PullRequest{{Host: "github", Number: 7, State: "open", Draft: false}},
			},
		},
	}
	got, err := Correlate(obs, ids, fakeAppraisals{})
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if got[0].Lifecycle != model.LifecycleReview {
		t.Fatalf("lifecycle = %q, want review", got[0].Lifecycle)
	}
}

func TestLifecycleFromPRDetailedStatus(t *testing.T) {
	lifecycleFor := func(pr model.PullRequest) model.Lifecycle {
		ids := newFakeIDs()
		obs := model.Observations{
			Git: []model.GitObservation{
				{
					Repo:     model.RepoRef{Identity: "acme/app", Path: "/repos/app"},
					Branches: []model.Branch{{Repo: "acme/app", Name: "feature", Head: "aaa"}},
					PRs:      []model.PullRequest{pr},
				},
			},
		}
		got, err := Correlate(obs, ids, fakeAppraisals{})
		if err != nil {
			t.Fatalf("err = %v", err)
		}
		return got[0].Lifecycle
	}

	// A draft PR with changes_requested is under external review: review, not planned.
	if got := lifecycleFor(model.PullRequest{Host: "github", Number: 1, State: "open", Draft: true, ReviewDecision: "changes_requested"}); got != model.LifecycleReview {
		t.Errorf("draft+changes_requested lifecycle = %q, want review", got)
	}
	// review_required likewise pulls toward review.
	if got := lifecycleFor(model.PullRequest{Host: "github", Number: 2, State: "open", Draft: true, ReviewDecision: "review_required"}); got != model.LifecycleReview {
		t.Errorf("draft+review_required lifecycle = %q, want review", got)
	}
	// A plain draft with no review decision stays planned.
	if got := lifecycleFor(model.PullRequest{Host: "github", Number: 3, State: "open", Draft: true}); got != model.LifecyclePlanned {
		t.Errorf("plain draft lifecycle = %q, want planned", got)
	}
	// A merged PR (now observable via --state all) drives merged.
	if got := lifecycleFor(model.PullRequest{Host: "github", Number: 4, State: "merged"}); got != model.LifecycleMerged {
		t.Errorf("merged PR lifecycle = %q, want merged", got)
	}
}

func TestDeterministicOrderingAcrossRuns(t *testing.T) {
	build := func() []model.Task {
		ids := newFakeIDs()
		ids.seed(model.Signature{Repo: "z/repo", Branch: "main"}, "Z1")
		ids.seed(model.Signature{Repo: "a/repo", Branch: "dev"}, "A1")
		ids.seed(model.Signature{Repo: "m/repo", Branch: "main"}, "M1")
		obs := model.Observations{
			Git: []model.GitObservation{
				gitObs("z/repo", "/z", model.Branch{Repo: "z/repo", Name: "main", Head: "z"}),
				gitObs("a/repo", "/a", model.Branch{Repo: "a/repo", Name: "dev", Head: "a"}),
				gitObs("m/repo", "/m", model.Branch{Repo: "m/repo", Name: "main", Head: "m"}),
			},
			Sessions: []model.SessionObservation{
				{Session: model.Session{Host: "codex", Cwd: "/nope1"}},
				{Session: model.Session{Host: "claude-code", Cwd: "/nope2"}},
			},
		}
		got, err := Correlate(obs, ids, fakeAppraisals{})
		if err != nil {
			t.Fatalf("err = %v", err)
		}
		return got
	}
	run1 := build()
	run2 := build()
	if !reflect.DeepEqual(run1, run2) {
		t.Fatalf("non-deterministic output:\nrun1=%+v\nrun2=%+v", run1, run2)
	}
	// Tasks sorted by ID.
	for i := 1; i < len(run1); i++ {
		if run1[i-1].ID > run1[i].ID {
			t.Fatalf("tasks not sorted by id: %q before %q", run1[i-1].ID, run1[i].ID)
		}
	}
}
