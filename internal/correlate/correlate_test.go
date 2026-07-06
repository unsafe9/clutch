package correlate

import (
	"reflect"
	"strings"
	"testing"
	"time"

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

// fakeInitiated is a canned InitiatedTaskReader.
type fakeInitiated struct {
	tasks []model.InitiatedTask
}

func (f fakeInitiated) InitiatedTasks() ([]model.InitiatedTask, error) {
	return f.tasks, nil
}

func gitObs(identity, path string, branches ...model.Branch) model.GitObservation {
	return model.GitObservation{
		Repo:     model.RepoRef{Identity: identity, Path: path, Remote: "git@github.com:" + identity + ".git"},
		Branches: branches,
	}
}

func TestEmptyObservations(t *testing.T) {
	res, err := Correlate(model.Observations{}, newFakeIDs(), fakeAppraisals{}, nil)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if res.Tasks == nil {
		t.Fatal("want non-nil empty slice, got nil")
	}
	if len(res.Tasks) != 0 {
		t.Fatalf("want 0 tasks, got %d", len(res.Tasks))
	}
}

func TestMaterializeInitiatedTask(t *testing.T) {
	created := time.Date(2026, 7, 6, 12, 0, 0, 0, time.UTC)
	initiated := fakeInitiated{tasks: []model.InitiatedTask{
		{ID: "INIT1", Title: "spike the parser", Mode: model.ModeSteer, Created: created},
	}}
	// One git-detected task coexists so the merge/ordering with discovered
	// tasks is exercised.
	obs := model.Observations{
		Git: []model.GitObservation{
			gitObs("acme/app", "/repos/app", model.Branch{Repo: "acme/app", Name: "main", Head: "aaa"}),
		},
	}
	res, err := Correlate(obs, newFakeIDs(), fakeAppraisals{}, initiated)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	got := res.Tasks
	if len(got) != 2 {
		t.Fatalf("want 2 tasks (git + initiated), got %d: %+v", len(got), got)
	}
	var init model.Task
	found := false
	for _, tk := range got {
		if tk.ID == "INIT1" {
			init = tk
			found = true
		}
	}
	if !found {
		t.Fatalf("registry-only initiated task not projected: %+v", got)
	}
	if init.Provenance != model.ProvenanceClutchInitiated {
		t.Errorf("provenance = %q, want clutch-initiated", init.Provenance)
	}
	if init.Lifecycle != model.LifecycleIdea {
		t.Errorf("lifecycle = %q, want idea", init.Lifecycle)
	}
	if init.Title != "spike the parser" || init.Mode != model.ModeSteer {
		t.Errorf("title/mode = %q/%q, want %q/steer", init.Title, init.Mode, "spike the parser")
	}
	if !init.Created.Equal(created) || !init.Updated.Equal(created) {
		t.Errorf("created/updated = %v/%v, want %v", init.Created, init.Updated, created)
	}
	if len(init.Branches) != 0 || len(init.Repos) != 0 {
		t.Errorf("registry-only task should carry no representations: %+v", init)
	}
}

func TestInitiatedTaskYieldsToObservation(t *testing.T) {
	// When an observation already produced a task for an initiated id, the
	// observation wins (no duplicate registry-only shell).
	ids := newFakeIDs()
	ids.seed(model.Signature{Repo: "acme/app", Branch: "main"}, "SHARED")
	initiated := fakeInitiated{tasks: []model.InitiatedTask{
		{ID: "SHARED", Title: "planner label", Mode: model.ModeCruise},
	}}
	obs := model.Observations{
		Git: []model.GitObservation{
			gitObs("acme/app", "/repos/app", model.Branch{Repo: "acme/app", Name: "main", Head: "aaa"}),
		},
	}
	res, err := Correlate(obs, ids, fakeAppraisals{}, initiated)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	got := res.Tasks
	if len(got) != 1 {
		t.Fatalf("want 1 task (dedup by id), got %d: %+v", len(got), got)
	}
	// The observation-built task keeps git-detected provenance and its branch.
	if got[0].Provenance != model.ProvenanceGitDetected {
		t.Errorf("provenance = %q, want git-detected (observation wins)", got[0].Provenance)
	}
	if len(got[0].Branches) != 1 {
		t.Errorf("observation task lost its branch: %+v", got[0])
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

	res, err := Correlate(obs, ids, fakeAppraisals{}, nil)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	got := res.Tasks
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
	res, err := Correlate(obs, ids, fakeAppraisals{}, nil)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	got := res.Tasks
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
		// Same path as the git repo: must enrich, not mint a new task. The
		// worktree is on the repo's main branch, so it routes to that branch-task.
		FS: []model.FSObservation{
			{Repo: model.RepoRef{Identity: "acme/app", Path: "/repos/app", Remote: "ssh://x"},
				Worktrees: []model.Worktree{{Path: "/repos/app/wt", Branch: "main", Repo: "acme/app"}}},
		},
	}
	res, err := Correlate(obs, ids, fakeAppraisals{}, nil)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	got := res.Tasks
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

// countRepoLinks returns the number of convention links whose subject is a repo
// rep, and asserts none is spurious (i.e. every repo link names wantRef).
func countRepoLinks(t *testing.T, tk model.Task, wantRef model.RepRef) int {
	t.Helper()
	n := 0
	for _, l := range tk.Links {
		if strings.HasPrefix(string(l.Subject), "repo:") {
			n++
			if l.Subject != wantRef {
				t.Errorf("spurious repo link %q, want only %q", l.Subject, wantRef)
			}
		}
	}
	return n
}

// A remote-backed checkout is observed twice — git under its remote identity and
// fs under the path-based local/<base> identity — but shares one path. The two
// reps must collapse into ONE, keeping the durable remote identity, with no
// spurious second rep or link.
func TestRepoWithRemoteCollapsesToOneRep(t *testing.T) {
	ids := newFakeIDs()
	obs := model.Observations{
		Git: []model.GitObservation{
			{
				Repo: model.RepoRef{
					Identity: "github.com/acme/app",
					Path:     "/repos/app",
					Remote:   "git@github.com:acme/app.git",
				},
				Branches: []model.Branch{{Repo: "github.com/acme/app", Name: "main", Head: "aaa"}},
			},
		},
		// fs re-observes the SAME checkout under the path-based identity.
		FS: []model.FSObservation{
			{Repo: model.RepoRef{Identity: "local/app", Path: "/repos/app"}},
		},
	}
	res, err := Correlate(obs, ids, fakeAppraisals{}, nil)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if len(res.Tasks) != 1 {
		t.Fatalf("want 1 task, got %d: %+v", len(res.Tasks), res.Tasks)
	}
	tk := res.Tasks[0]
	if len(tk.Repos) != 1 {
		t.Fatalf("want 1 repo rep after collapse, got %d: %+v", len(tk.Repos), tk.Repos)
	}
	rep := tk.Repos[0]
	if rep.Identity != "github.com/acme/app" {
		t.Errorf("collapsed rep identity = %q, want remote identity github.com/acme/app", rep.Identity)
	}
	if rep.Remote == "" {
		t.Errorf("collapsed rep lost its remote")
	}
	if rep.Ref != "repo:github.com/acme/app" {
		t.Errorf("collapsed rep ref = %q", rep.Ref)
	}
	if n := countRepoLinks(t, tk, "repo:github.com/acme/app"); n != 1 {
		t.Errorf("want exactly 1 repo link, got %d: %+v", n, tk.Links)
	}
}

// A checkout with no remote yields the SAME local/<base> identity from both
// producers, so it must surface exactly one rep (and one link) — the collapse
// must not depend on a remote being present.
func TestRepoWithoutRemoteSurfacesOnce(t *testing.T) {
	ids := newFakeIDs()
	obs := model.Observations{
		Git: []model.GitObservation{
			{
				Repo:     model.RepoRef{Identity: "local/app", Path: "/repos/app"},
				Branches: []model.Branch{{Repo: "local/app", Name: "main", Head: "aaa"}},
			},
		},
		FS: []model.FSObservation{
			{Repo: model.RepoRef{Identity: "local/app", Path: "/repos/app"}},
		},
	}
	res, err := Correlate(obs, ids, fakeAppraisals{}, nil)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if len(res.Tasks) != 1 {
		t.Fatalf("want 1 task, got %d: %+v", len(res.Tasks), res.Tasks)
	}
	tk := res.Tasks[0]
	if len(tk.Repos) != 1 {
		t.Fatalf("no-remote repo should surface exactly one rep, got %d: %+v", len(tk.Repos), tk.Repos)
	}
	if tk.Repos[0].Identity != "local/app" {
		t.Errorf("rep identity = %q, want local/app", tk.Repos[0].Identity)
	}
	if n := countRepoLinks(t, tk, "repo:local/app"); n != 1 {
		t.Errorf("want exactly 1 repo link, got %d: %+v", n, tk.Links)
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
	res, err := Correlate(obs, ids, fakeAppraisals{}, nil)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	got := res.Tasks
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
	// The unmatched session is scan-wide, not parked on the task: the task carries
	// no unresolved flags of its own, and the flag surfaces in res.ScanWide with an
	// empty TaskID.
	if len(tk.Unresolved) != 0 {
		t.Fatalf("task should carry no unresolved flags, got %+v", tk.Unresolved)
	}
	foundUnres := false
	for _, u := range res.ScanWide {
		if u.Kind == model.UnresolvedSession {
			foundUnres = true
			if u.TaskID != "" {
				t.Fatalf("scan-wide flag should have empty TaskID, got %q", u.TaskID)
			}
		}
	}
	if !foundUnres {
		t.Fatalf("missing scan-wide unresolved-session flag: %+v", res.ScanWide)
	}
}

// tasksByID indexes a projection by task id for per-task assertions.
func tasksByID(tasks []model.Task) map[string]model.Task {
	m := map[string]model.Task{}
	for _, tk := range tasks {
		m[tk.ID] = tk
	}
	return m
}

// A session records the branch it is on, so it must bind to the task owning that
// branch at its cwd's repo — even when a DIFFERENT branch-task was indexed first
// for that cwd (the old cwd-only, first-wins routing would have misbound it). A
// session whose branch matches no branch-task falls back to cwd routing.
func TestSessionBindsByBranch(t *testing.T) {
	ids := newFakeIDs()
	ids.seed(model.Signature{Repo: "acme/app", Branch: "feature/x"}, "T-feat")
	ids.seed(model.Signature{Repo: "acme/app", Branch: "main"}, "T-main")
	obs := model.Observations{
		Git: []model.GitObservation{
			{
				Repo: model.RepoRef{Identity: "acme/app", Path: "/repos/app", Remote: "git@github.com:acme/app.git"},
				// feature/x is enumerated first, so cwd-only routing would send the
				// /repos/app session to T-feat.
				Branches: []model.Branch{
					{Repo: "acme/app", Name: "feature/x", Head: "f"},
					{Repo: "acme/app", Name: "main", Head: "m"},
				},
			},
		},
		Sessions: []model.SessionObservation{
			// branch main at the repo cwd -> must bind to the main task.
			{Session: model.Session{Host: "claude-code", Cwd: "/repos/app", Branch: "main"}},
			// branch not among the repo's branch-tasks -> fall back to cwd routing
			// (first-wins T-feat).
			{Session: model.Session{Host: "codex", Cwd: "/repos/app", Branch: "gone"}},
		},
	}
	res, err := Correlate(obs, ids, fakeAppraisals{}, nil)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	byID := tasksByID(res.Tasks)
	if got := len(byID["T-main"].Sessions); got != 1 {
		t.Errorf("main task session count = %d, want 1 (the branch=main session)", got)
	}
	// T-feat gets exactly the fallback session (branch=gone), not the branch=main one.
	if got := len(byID["T-feat"].Sessions); got != 1 {
		t.Fatalf("feature/x task session count = %d, want 1 (the fallback session)", got)
	}
	if br := byID["T-feat"].Sessions[0].Branch; br != "gone" {
		t.Errorf("feature/x task bound the wrong session (branch %q), want the branch=gone fallback", br)
	}
}

// git worktree list reports every working tree, each on its own branch. A worktree
// rep must attach only to the task of the branch checked out in it — not to every
// branch-task of the repo.
func TestWorktreeAttachesOnlyToItsBranchTask(t *testing.T) {
	ids := newFakeIDs()
	ids.seed(model.Signature{Repo: "acme/app", Branch: "main"}, "T-main")
	ids.seed(model.Signature{Repo: "acme/app", Branch: "feature/x"}, "T-feat")
	obs := model.Observations{
		Git: []model.GitObservation{
			{
				Repo: model.RepoRef{Identity: "acme/app", Path: "/repos/app", Remote: "git@github.com:acme/app.git"},
				Branches: []model.Branch{
					{Repo: "acme/app", Name: "main", Head: "m"},
					{Repo: "acme/app", Name: "feature/x", Head: "f"},
				},
				Worktrees: []model.Worktree{
					{Path: "/repos/app", Branch: "main", Repo: "acme/app"},         // primary, on main
					{Path: "/repos/app-wt", Branch: "feature/x", Repo: "acme/app"}, // linked, on feature/x
				},
			},
		},
	}
	res, err := Correlate(obs, ids, fakeAppraisals{}, nil)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	byID := tasksByID(res.Tasks)
	main, feat := byID["T-main"], byID["T-feat"]
	if len(main.Worktrees) != 1 || main.Worktrees[0].Path != "/repos/app" {
		t.Errorf("main task worktrees = %+v, want only /repos/app", main.Worktrees)
	}
	if len(feat.Worktrees) != 1 || feat.Worktrees[0].Path != "/repos/app-wt" {
		t.Errorf("feature/x task worktrees = %+v, want only /repos/app-wt", feat.Worktrees)
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
	res, err := Correlate(obs, ids, appr, nil)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	tk := res.Tasks[0]
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
	res, err := Correlate(obs, ids, fakeAppraisals{}, nil)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	var child model.Task
	for _, tk := range res.Tasks {
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
	res, err := Correlate(obs, ids, fakeAppraisals{}, nil)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if res.Tasks[0].Lifecycle != model.LifecycleReview {
		t.Fatalf("lifecycle = %q, want review", res.Tasks[0].Lifecycle)
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
		res, err := Correlate(obs, ids, fakeAppraisals{}, nil)
		if err != nil {
			t.Fatalf("err = %v", err)
		}
		return res.Tasks[0].Lifecycle
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
	build := func() Result {
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
		res, err := Correlate(obs, ids, fakeAppraisals{}, nil)
		if err != nil {
			t.Fatalf("err = %v", err)
		}
		return res
	}
	run1 := build()
	run2 := build()
	if !reflect.DeepEqual(run1, run2) {
		t.Fatalf("non-deterministic output:\nrun1=%+v\nrun2=%+v", run1, run2)
	}
	// Tasks sorted by ID.
	for i := 1; i < len(run1.Tasks); i++ {
		if run1.Tasks[i-1].ID > run1.Tasks[i].ID {
			t.Fatalf("tasks not sorted by id: %q before %q", run1.Tasks[i-1].ID, run1.Tasks[i].ID)
		}
	}
}
