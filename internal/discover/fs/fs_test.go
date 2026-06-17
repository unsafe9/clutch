package fs

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/unsafe9/clutch/internal/model"
)

func realPath(t *testing.T, path string) string {
	t.Helper()
	real, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatalf("EvalSymlinks %s: %v", path, err)
	}
	return real
}

func mkdir(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
}

func write(t *testing.T, path, content string) {
	t.Helper()
	mkdir(t, filepath.Dir(path))
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func TestObserve(t *testing.T) {
	root := t.TempDir()

	// A plain repo: directory with a .git DIR.
	repoA := filepath.Join(root, "repoA")
	mkdir(t, filepath.Join(repoA, ".git"))
	// noise inside .git that must NOT be descended into / detected.
	mkdir(t, filepath.Join(repoA, ".git", "modules", "x", ".git"))
	// noise dir that must be skipped.
	mkdir(t, filepath.Join(repoA, "node_modules", "pkg"))

	// A linked worktree: directory with a .git FILE pointing at a gitdir under
	// repoA's .git/worktrees. A real worktree gitdir carries HEAD + commondir
	// (the latter names the main repo's .git, relative to the gitdir).
	gitdir := filepath.Join(repoA, ".git", "worktrees", "wt")
	write(t, filepath.Join(gitdir, "HEAD"), "ref: refs/heads/feature-x\n")
	write(t, filepath.Join(gitdir, "commondir"), "../..\n")
	wt := filepath.Join(root, "wt")
	write(t, filepath.Join(wt, ".git"), "gitdir: "+gitdir+"\n")

	// A non-repo dir.
	mkdir(t, filepath.Join(root, "plain", "nested"))

	obs, err := Observe([]string{root})
	if err != nil {
		t.Fatalf("Observe: %v", err)
	}

	// Observe canonicalizes paths (EvalSymlinks) so they agree with the git
	// producer; resolve the fixture paths the same way before comparing.
	repoA = realPath(t, repoA)
	wt = realPath(t, wt)

	byPath := map[string]model.FSObservation{}
	for _, o := range obs {
		byPath[o.Repo.Path] = o
	}

	// The linked worktree must NOT become its own repo observation: identity
	// correctness requires it to resolve back to repoA. So there is exactly one
	// observation keyed to repoA, and none keyed to the worktree path.
	if _, ok := byPath[wt]; ok {
		t.Errorf("linked worktree must not be emitted as its own repo: %v", byPath)
	}

	a, ok := byPath[repoA]
	if !ok {
		t.Fatalf("repoA not detected; got %v", byPath)
	}
	if a.Repo.Identity != "local/repoA" {
		t.Errorf("repoA identity = %q, want local/repoA", a.Repo.Identity)
	}
	if a.Repo.Ref != "repo:local/repoA" {
		t.Errorf("repoA ref = %q", a.Repo.Ref)
	}

	// The worktree surfaces ONLY as a model.Worktree attached to repoA, carrying
	// repoA's identity (not a fresh path-based identity).
	if len(a.Worktrees) != 1 {
		t.Fatalf("repoA should carry 1 linked worktree, got %d: %v", len(a.Worktrees), a.Worktrees)
	}
	got := a.Worktrees[0]
	if got.Path != wt {
		t.Errorf("worktree path = %q, want %q", got.Path, wt)
	}
	if got.Ref != model.RepRef("worktree:"+wt) {
		t.Errorf("worktree ref = %q", got.Ref)
	}
	if got.Repo != "local/repoA" {
		t.Errorf("worktree repo = %q, want local/repoA", got.Repo)
	}
	if got.Branch != "feature-x" {
		t.Errorf("worktree branch = %q, want feature-x", got.Branch)
	}

	// The .git internals must never be detected as repos.
	for p := range byPath {
		if filepath.Base(filepath.Dir(p)) == ".git" || filepath.Base(p) == "modules" {
			t.Errorf("detected something inside .git: %q", p)
		}
		if filepath.Base(p) == "node_modules" || filepath.Dir(p) == filepath.Join(repoA, "node_modules") {
			t.Errorf("descended into node_modules: %q", p)
		}
	}
}

func TestObserveDepthBound(t *testing.T) {
	root := t.TempDir()
	// Build a path deeper than maxDepth with a repo at the bottom.
	deep := root
	for i := 0; i <= maxDepth+2; i++ {
		deep = filepath.Join(deep, "d")
	}
	mkdir(t, filepath.Join(deep, ".git"))

	obs, err := Observe([]string{root})
	if err != nil {
		t.Fatalf("Observe: %v", err)
	}
	for _, o := range obs {
		if o.Repo.Path == deep {
			t.Errorf("repo below maxDepth should not be detected: %q", deep)
		}
	}
}
