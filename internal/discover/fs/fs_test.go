package fs

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/unsafe9/clutch/internal/model"
)

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

	// A linked worktree: directory with a .git FILE pointing at a gitdir.
	gitdir := filepath.Join(repoA, ".git", "worktrees", "wt")
	write(t, filepath.Join(gitdir, "HEAD"), "ref: refs/heads/feature-x\n")
	wt := filepath.Join(root, "wt")
	write(t, filepath.Join(wt, ".git"), "gitdir: "+gitdir+"\n")

	// A non-repo dir.
	mkdir(t, filepath.Join(root, "plain", "nested"))

	obs, err := Observe([]string{root})
	if err != nil {
		t.Fatalf("Observe: %v", err)
	}

	byPath := map[string]model.FSObservation{}
	for _, o := range obs {
		byPath[o.Repo.Path] = o
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
	if len(a.Worktrees) != 0 {
		t.Errorf("repoA should have no linked worktrees, got %v", a.Worktrees)
	}

	w, ok := byPath[wt]
	if !ok {
		t.Fatalf("worktree not detected; got %v", byPath)
	}
	if len(w.Worktrees) != 1 {
		t.Fatalf("worktree obs should carry 1 worktree, got %d", len(w.Worktrees))
	}
	got := w.Worktrees[0]
	if got.Path != wt {
		t.Errorf("worktree path = %q, want %q", got.Path, wt)
	}
	if got.Ref != model.RepRef("worktree:"+wt) {
		t.Errorf("worktree ref = %q", got.Ref)
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
