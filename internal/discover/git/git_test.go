package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/unsafe9/clutch/internal/model"
)

// gh-dependent PR fetching is intentionally untested here: it requires the `gh`
// binary, an authenticated github remote, and network access, none of which are
// available in a hermetic unit test. observePRs is explicitly best-effort and
// returns nil on any such absence, so the rest of Observe is exercised without
// it. parsePRs (the pure decode/rollup half) IS covered below.

func gitCmd(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	// Hermetic identity + no signing/hooks/global config bleed-through.
	cmd.Env = append(cmd.Environ(),
		"GIT_AUTHOR_NAME=clutch-test",
		"GIT_AUTHOR_EMAIL=test@clutch.invalid",
		"GIT_COMMITTER_NAME=clutch-test",
		"GIT_COMMITTER_EMAIL=test@clutch.invalid",
		"GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_CONFIG_SYSTEM=/dev/null",
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func TestObserve(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	root := t.TempDir()
	repo := filepath.Join(root, "myrepo")
	gitCmd(t, root, "init", "-b", "main", "myrepo")
	gitCmd(t, repo, "config", "remote.origin.url", "git@github.com:acme/myrepo.git")

	writeFile(t, filepath.Join(repo, "a.txt"), "hello")
	gitCmd(t, repo, "add", "a.txt")
	gitCmd(t, repo, "commit", "-m", "first")

	writeFile(t, filepath.Join(repo, "b.txt"), "world")
	gitCmd(t, repo, "add", "b.txt")
	gitCmd(t, repo, "commit", "-m", "second")

	// A feature branch off main with one extra commit.
	gitCmd(t, repo, "checkout", "-b", "feature")
	writeFile(t, filepath.Join(repo, "c.txt"), "feat")
	gitCmd(t, repo, "add", "c.txt")
	gitCmd(t, repo, "commit", "-m", "feat")

	// A linked worktree on its own branch.
	wt := filepath.Join(root, "wt")
	gitCmd(t, repo, "worktree", "add", "-b", "wtbranch", wt)

	gitCmd(t, repo, "checkout", "main")

	obs, err := Observe([]string{root})
	if err != nil {
		t.Fatalf("Observe: %v", err)
	}

	// The worktree's checkout (.git file) is also a repo dir, but it shares the
	// same gitdir; we assert against the primary repo observation.
	primary := findRepo(t, obs, repo)

	if got, want := primary.Repo.Identity, "github.com/acme/myrepo"; got != want {
		t.Errorf("identity = %q, want %q", got, want)
	}
	if got, want := primary.Repo.Ref, "repo:github.com/acme/myrepo"; string(got) != want {
		t.Errorf("repo ref = %q, want %q", got, want)
	}
	if got, want := primary.Repo.Remote, "git@github.com:acme/myrepo.git"; got != want {
		t.Errorf("remote = %q, want %q", got, want)
	}

	// commits on main HEAD: first + second = 2.
	if got := primary.Commits.Count; got != 2 {
		t.Errorf("commit count = %d, want 2", got)
	}
	if primary.Commits.Head == "" {
		t.Error("commit head empty")
	}

	// Branches: main, feature, wtbranch.
	byName := map[string]bool{}
	for _, b := range primary.Branches {
		byName[b.Name] = true
		want := model.RepRef("branch:" + b.Repo + "/" + b.Name)
		if b.Ref != want {
			t.Errorf("branch %q ref = %q, want %q", b.Name, b.Ref, want)
		}
	}
	for _, want := range []string{"main", "feature", "wtbranch"} {
		if !byName[want] {
			t.Errorf("missing branch %q in %v", want, byName)
		}
	}

	// feature is ahead of main by 1 commit -> unmerged, base set.
	feat := branchByName(t, primary.Branches, "feature")
	if feat.Base == "" {
		t.Error("feature base empty")
	}
	if feat.Integration != "unmerged" {
		t.Errorf("feature integration = %q, want unmerged", feat.Integration)
	}

	// Worktrees: primary repo + linked wt. Assert the linked one is present.
	foundWT := false
	for _, w := range primary.Worktrees {
		if w.Branch == "wtbranch" {
			foundWT = true
			if w.Ref != model.RepRef("worktree:"+w.Path) {
				t.Errorf("worktree ref = %q", w.Ref)
			}
			if w.Repo != "github.com/acme/myrepo" {
				t.Errorf("worktree repo = %q", w.Repo)
			}
		}
	}
	if !foundWT {
		t.Errorf("linked worktree not found in %v", primary.Worktrees)
	}
}

// TestLinkedWorktreeNoPhantomRepo asserts the core identity-correctness fix: a
// linked worktree (whose .git is a FILE pointing into the main repo's
// .git/worktrees) is NEVER emitted as its own repo observation. It resolves to
// the MAIN repo and surfaces only as a model.Worktree on the main repo's obs —
// so its shared branches are not re-enumerated under a phantom identity.
func TestLinkedWorktreeNoPhantomRepo(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	root := t.TempDir()
	repo := filepath.Join(root, "main")
	gitCmd(t, root, "init", "-b", "main", "main")
	writeFile(t, filepath.Join(repo, "a.txt"), "1")
	gitCmd(t, repo, "add", "a.txt")
	gitCmd(t, repo, "commit", "-m", "one")
	gitCmd(t, repo, "branch", "feature/x")

	wt := filepath.Join(root, "main-wt")
	gitCmd(t, repo, "worktree", "add", wt, "feature/x")

	obs, err := Observe([]string{root})
	if err != nil {
		t.Fatalf("Observe: %v", err)
	}

	repo = realPath(t, repo)
	wt = realPath(t, wt)

	// Exactly ONE repo observation (the main repo). The worktree must not appear
	// as a second observation keyed to its own path.
	if len(obs) != 1 {
		t.Fatalf("want 1 repo observation, got %d: %+v", len(obs), obs)
	}
	if obs[0].Repo.Path != repo {
		t.Fatalf("observation path = %q, want main repo %q", obs[0].Repo.Path, repo)
	}
	for _, o := range obs {
		if o.Repo.Path == wt {
			t.Errorf("linked worktree emitted as its own repo: %q", wt)
		}
	}

	// The worktree surfaces as a model.Worktree on the main repo's observation.
	found := false
	for _, w := range obs[0].Worktrees {
		if w.Path == wt {
			found = true
			if w.Repo != obs[0].Repo.Identity {
				t.Errorf("worktree repo = %q, want main identity %q", w.Repo, obs[0].Repo.Identity)
			}
		}
	}
	if !found {
		t.Errorf("worktree %q not attached to main repo: %+v", wt, obs[0].Worktrees)
	}
}

func TestObserveLocalOnlyIdentity(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	root := t.TempDir()
	repo := filepath.Join(root, "solo")
	gitCmd(t, root, "init", "-b", "main", "solo")
	writeFile(t, filepath.Join(repo, "x"), "1")
	gitCmd(t, repo, "add", "x")
	gitCmd(t, repo, "commit", "-m", "c")

	obs, err := Observe([]string{root})
	if err != nil {
		t.Fatalf("Observe: %v", err)
	}
	p := findRepo(t, obs, repo)
	if got, want := p.Repo.Identity, "local/solo"; got != want {
		t.Errorf("local identity = %q, want %q", got, want)
	}
	if p.Repo.Remote != "" {
		t.Errorf("remote = %q, want empty", p.Repo.Remote)
	}
}

func TestNormalizeRemote(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"", ""},
		{"git@github.com:acme/myrepo.git", "github.com/acme/myrepo"},
		{"git@github.com:acme/myrepo", "github.com/acme/myrepo"},
		{"https://github.com/acme/myrepo.git", "github.com/acme/myrepo"},
		{"https://user@github.com/acme/myrepo", "github.com/acme/myrepo"},
		{"ssh://git@github.com:22/acme/myrepo.git", "github.com/acme/myrepo"},
		{"https://gitlab.example.com:8443/grp/sub/repo.git", "gitlab.example.com/grp/sub/repo"},
	}
	for _, tt := range tests {
		if got := normalizeRemote(tt.in); got != tt.want {
			t.Errorf("normalizeRemote(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestParsePRs(t *testing.T) {
	raw := `[
      {"number":7,"url":"https://github.com/acme/r/pull/7","state":"OPEN","isDraft":true,
       "statusCheckRollup":[{"state":"COMPLETED","conclusion":"SUCCESS"}]},
      {"number":9,"url":"https://github.com/acme/r/pull/9","state":"OPEN","isDraft":false,
       "statusCheckRollup":[{"state":"COMPLETED","conclusion":"FAILURE"}]}
    ]`
	prs := parsePRs(raw, "github.com")
	if len(prs) != 2 {
		t.Fatalf("len = %d, want 2", len(prs))
	}
	if prs[0].Ref != "pr:github.com#7" {
		t.Errorf("ref = %q", prs[0].Ref)
	}
	if !prs[0].Draft {
		t.Error("pr7 should be draft")
	}
	if prs[0].Checks != "success" {
		t.Errorf("pr7 checks = %q, want success", prs[0].Checks)
	}
	if prs[1].Checks != "failure" {
		t.Errorf("pr9 checks = %q, want failure", prs[1].Checks)
	}
}

func TestParsePRsBadJSON(t *testing.T) {
	if prs := parsePRs("not json", "github.com"); prs != nil {
		t.Errorf("want nil on bad json, got %v", prs)
	}
}

// TestParsePRsDetailedStatus covers the detailed-status mapping: reviewDecision
// and mergeable normalized from gh's UPPER_CASE to lower_snake_case, and a
// merged-state PR (only observable now that --state all is queried).
func TestParsePRsDetailedStatus(t *testing.T) {
	raw := `[
      {"number":4,"url":"u4","state":"OPEN","isDraft":false,
       "reviewDecision":"CHANGES_REQUESTED","mergeable":"CONFLICTING","statusCheckRollup":[]},
      {"number":5,"url":"u5","state":"OPEN","isDraft":false,
       "reviewDecision":"APPROVED","mergeable":"MERGEABLE","statusCheckRollup":[]},
      {"number":6,"url":"u6","state":"MERGED","isDraft":false,
       "reviewDecision":"","mergeable":"UNKNOWN","statusCheckRollup":[]}
    ]`
	prs := parsePRs(raw, "github.com")
	if len(prs) != 3 {
		t.Fatalf("len = %d, want 3", len(prs))
	}
	if prs[0].ReviewDecision != "changes_requested" || prs[0].Mergeable != "conflicting" {
		t.Errorf("pr4 = %q/%q, want changes_requested/conflicting", prs[0].ReviewDecision, prs[0].Mergeable)
	}
	if prs[1].ReviewDecision != "approved" || prs[1].Mergeable != "mergeable" {
		t.Errorf("pr5 = %q/%q, want approved/mergeable", prs[1].ReviewDecision, prs[1].Mergeable)
	}
	if prs[2].State != "MERGED" || prs[2].ReviewDecision != "" || prs[2].Mergeable != "unknown" {
		t.Errorf("pr6 = %q/%q/%q, want MERGED/empty/unknown", prs[2].State, prs[2].ReviewDecision, prs[2].Mergeable)
	}
}

func TestParsePRsStatesAndChecks(t *testing.T) {
	// Exercises the full state/checks mapping: merged + closed states, an empty
	// rollup (no checks → ""), and a pending (in-progress) rollup.
	raw := `[
      {"number":1,"url":"u1","state":"MERGED","isDraft":false,"statusCheckRollup":[]},
      {"number":2,"url":"u2","state":"CLOSED","isDraft":false,
       "statusCheckRollup":[{"state":"IN_PROGRESS","conclusion":""}]},
      {"number":3,"url":"u3","state":"OPEN","isDraft":false,
       "statusCheckRollup":[{"state":"COMPLETED","conclusion":"SKIPPED"},
                            {"state":"COMPLETED","conclusion":"SUCCESS"}]}
    ]`
	prs := parsePRs(raw, "github.com")
	if len(prs) != 3 {
		t.Fatalf("len = %d, want 3", len(prs))
	}
	if prs[0].State != "MERGED" || prs[0].Checks != "" {
		t.Errorf("pr1 = %q/%q, want MERGED/empty checks", prs[0].State, prs[0].Checks)
	}
	if prs[1].Checks != "pending" {
		t.Errorf("pr2 checks = %q, want pending", prs[1].Checks)
	}
	if prs[2].Checks != "success" {
		t.Errorf("pr3 checks = %q, want success", prs[2].Checks)
	}
}

// TestObservePRsSkipsCleanly asserts observePRs never errors and returns nil for
// the cases it must skip: empty remote, and a non-github identity. (The live
// github path needs gh + auth + network, so it stays guarded out of unit tests;
// parsePRs covers the parse/mapping it feeds.)
func TestObservePRsSkipsCleanly(t *testing.T) {
	if prs := observePRs("local/solo", ""); prs != nil {
		t.Errorf("empty remote: want nil, got %v", prs)
	}
	if prs := observePRs("gitlab.com/acme/app", "git@gitlab.com:acme/app.git"); prs != nil {
		t.Errorf("non-github identity: want nil, got %v", prs)
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// realPath canonicalizes a fixture path the way Observe does (EvalSymlinks), so
// path comparisons survive /var → /private/var style symlinks (macOS tmpdirs).
func realPath(t *testing.T, path string) string {
	t.Helper()
	real, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatalf("EvalSymlinks %s: %v", path, err)
	}
	return real
}

func findRepo(t *testing.T, obs []model.GitObservation, path string) model.GitObservation {
	t.Helper()
	path = realPath(t, path)
	for _, o := range obs {
		if o.Repo.Path == path {
			return o
		}
	}
	t.Fatalf("no observation for repo %q in %d observations", path, len(obs))
	return model.GitObservation{}
}

func branchByName(t *testing.T, branches []model.Branch, name string) model.Branch {
	t.Helper()
	for _, b := range branches {
		if b.Name == name {
			return b
		}
	}
	t.Fatalf("branch %q not found", name)
	return model.Branch{}
}
