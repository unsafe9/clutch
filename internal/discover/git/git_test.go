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

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func findRepo(t *testing.T, obs []model.GitObservation, path string) model.GitObservation {
	t.Helper()
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
