// Package fs scans configured roots for repos and their worktrees. Concrete
// functions only — no common Discoverer interface.
package fs

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"

	"github.com/unsafe9/clutch/internal/model"
)

// maxDepth bounds how deep the scan descends below each root.
const maxDepth = 8

// skipDirs are heavy/irrelevant directories the walk never descends into.
var skipDirs = map[string]bool{
	"node_modules": true,
	"vendor":       true,
	"target":       true,
	"dist":         true,
	"build":        true,
	".idea":        true,
	".vscode":      true,
}

// Observe scans roots and returns repo/worktree filesystem observations. A
// primary repo (a `.git` directory) yields a repo observation. A linked worktree
// (a `.git` FILE pointing at <main>/.git/worktrees/<name>) is NOT emitted as its
// own repo — it shares the main repo's identity, so it is resolved back to the
// MAIN repo and surfaced as a model.Worktree attached to that repo's observation.
// Identity is path-based, since the filesystem scan has no remote. A walk error
// on an individual entry is skipped; an error is returned only when a root cannot
// be read.
func Observe(roots []string) ([]model.FSObservation, error) {
	// byRepo dedups observations on the RESOLVED repo path (not the walk path):
	// a linked worktree resolves to its main repo, which may also be scanned in
	// its own right, so both must fold into one observation rather than emit a
	// duplicate. order preserves first-seen sequence for a deterministic result.
	byRepo := map[string]*model.FSObservation{}
	var order []string
	for _, root := range roots {
		if err := scanRoot(root, byRepo, &order); err != nil {
			return nil, err
		}
	}
	out := make([]model.FSObservation, 0, len(order))
	for _, p := range order {
		out = append(out, *byRepo[p])
	}
	return out, nil
}

func scanRoot(root string, byRepo map[string]*model.FSObservation, order *[]string) error {
	rootClean := filepath.Clean(root)

	return filepath.WalkDir(rootClean, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			if path == rootClean {
				return err
			}
			return nil
		}
		if !d.IsDir() {
			return nil
		}
		if path != rootClean {
			name := d.Name()
			if name == ".git" {
				return filepath.SkipDir
			}
			if skipDirs[name] {
				return filepath.SkipDir
			}
		}
		if depth(rootClean, path) > maxDepth {
			return filepath.SkipDir
		}
		obs, ok := detect(path)
		if !ok {
			return nil
		}
		if existing, dup := byRepo[obs.Repo.Path]; dup {
			existing.Worktrees = append(existing.Worktrees, obs.Worktrees...)
			return nil
		}
		cp := obs
		byRepo[obs.Repo.Path] = &cp
		*order = append(*order, obs.Repo.Path)
		return nil
	})
}

// depth returns how many path segments below root path is.
func depth(root, path string) int {
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == "." {
		return 0
	}
	return strings.Count(rel, string(filepath.Separator)) + 1
}

// detect inspects dir's `.git` marker. A `.git` directory is a primary repo. A
// `.git` file pointing at a gitdir is a linked worktree, which is resolved back
// to its MAIN repo (so it shares the main repo's identity) and emitted as a
// model.Worktree attached to that repo, never as its own repo.
func detect(dir string) (model.FSObservation, bool) {
	gitPath := filepath.Join(dir, ".git")
	info, err := os.Lstat(gitPath)
	if err != nil {
		return model.FSObservation{}, false
	}

	if info.IsDir() {
		repo := canonical(dir)
		identity := pathIdentity(repo)
		return model.FSObservation{
			Repo: model.RepoRef{
				Ref:      model.RepRef("repo:" + identity),
				Identity: identity,
				Path:     repo,
			},
		}, true
	}

	gitdir := readGitdir(gitPath)
	if gitdir == "" {
		return model.FSObservation{}, false
	}
	if !filepath.IsAbs(gitdir) {
		gitdir = filepath.Join(dir, gitdir)
	}
	mainRepo, ok := mainRepoFromGitdir(gitdir)
	if !ok {
		return model.FSObservation{}, false
	}
	mainRepo = canonical(mainRepo)
	wt := canonical(dir)
	identity := pathIdentity(mainRepo)
	return model.FSObservation{
		Repo: model.RepoRef{
			Ref:      model.RepRef("repo:" + identity),
			Identity: identity,
			Path:     mainRepo,
		},
		Worktrees: []model.Worktree{{
			Ref:    model.RepRef("worktree:" + wt),
			Path:   wt,
			Repo:   identity,
			Branch: worktreeHEAD(gitdir),
		}},
	}, true
}

// canonical resolves symlinks in path so the fs producer's paths agree with the
// git producer's (git reports already-canonicalized paths, e.g. /var resolves to
// /private/var on macOS). Falls back to the input if resolution fails.
func canonical(path string) string {
	if real, err := filepath.EvalSymlinks(path); err == nil {
		return real
	}
	return path
}

// mainRepoFromGitdir resolves the MAIN repo working directory from a linked
// worktree's gitdir (<main>/.git/worktrees/<name>). The sibling `commondir` file
// names the main repo's .git relative to the gitdir; the main working tree is its
// parent. Returns ok=false if the gitdir is not a worktree gitdir.
func mainRepoFromGitdir(gitdir string) (string, bool) {
	data, err := os.ReadFile(filepath.Join(gitdir, "commondir"))
	if err != nil {
		return "", false
	}
	common := strings.TrimSpace(string(data))
	if common == "" {
		return "", false
	}
	if !filepath.IsAbs(common) {
		common = filepath.Join(gitdir, common)
	}
	return filepath.Dir(filepath.Clean(common)), true
}

// worktreeHEAD reads the linked worktree's HEAD ref (the gitdir's HEAD file) and
// returns its short branch name, or "" if detached/unreadable.
func worktreeHEAD(gitdir string) string {
	data, err := os.ReadFile(filepath.Join(gitdir, "HEAD"))
	if err != nil {
		return ""
	}
	head := strings.TrimSpace(string(data))
	if ref, ok := strings.CutPrefix(head, "ref: "); ok {
		return strings.TrimPrefix(ref, "refs/heads/")
	}
	return ""
}

// readGitdir extracts the gitdir path from a `.git` file's `gitdir:` line.
func readGitdir(gitFile string) string {
	f, err := os.Open(gitFile)
	if err != nil {
		return ""
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if dir, ok := strings.CutPrefix(line, "gitdir:"); ok {
			return strings.TrimSpace(dir)
		}
	}
	return ""
}

// pathIdentity yields a path-consistent identity for a checkout: "local/<base>".
func pathIdentity(dir string) string {
	return "local/" + filepath.Base(dir)
}
