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

// Observe scans roots and returns repo/worktree filesystem observations. It
// detects repos (a `.git` directory) and linked worktrees (a `.git` FILE that
// points at a gitdir), using a path-consistent identity (path-based, since the
// filesystem scan has no remote). A walk error on an individual entry is
// skipped; an error is returned only when a root cannot be read.
func Observe(roots []string) ([]model.FSObservation, error) {
	var out []model.FSObservation
	seen := map[string]bool{}
	for _, root := range roots {
		obs, err := scanRoot(root, seen)
		if err != nil {
			return nil, err
		}
		out = append(out, obs...)
	}
	return out, nil
}

func scanRoot(root string, seen map[string]bool) ([]model.FSObservation, error) {
	var repos []model.FSObservation
	rootClean := filepath.Clean(root)

	err := filepath.WalkDir(rootClean, func(path string, d os.DirEntry, err error) error {
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
		if obs, ok := detect(path); ok {
			if !seen[path] {
				seen[path] = true
				repos = append(repos, obs)
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return repos, nil
}

// depth returns how many path segments below root path is.
func depth(root, path string) int {
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == "." {
		return 0
	}
	return strings.Count(rel, string(filepath.Separator)) + 1
}

// detect inspects dir's `.git` marker. A `.git` directory is a repo; a `.git`
// file pointing at a gitdir is a linked worktree.
func detect(dir string) (model.FSObservation, bool) {
	gitPath := filepath.Join(dir, ".git")
	info, err := os.Lstat(gitPath)
	if err != nil {
		return model.FSObservation{}, false
	}

	identity := pathIdentity(dir)

	if info.IsDir() {
		return model.FSObservation{
			Repo: model.RepoRef{
				Ref:      model.RepRef("repo:" + identity),
				Identity: identity,
				Path:     dir,
			},
		}, true
	}

	if !gitFilePointsAtGitdir(gitPath) {
		return model.FSObservation{}, false
	}
	return model.FSObservation{
		Repo: model.RepoRef{
			Ref:      model.RepRef("repo:" + identity),
			Identity: identity,
			Path:     dir,
		},
		Worktrees: []model.Worktree{{
			Ref:    model.RepRef("worktree:" + dir),
			Path:   dir,
			Repo:   identity,
			Branch: worktreeBranch(gitPath),
		}},
	}, true
}

// gitFilePointsAtGitdir reports whether a `.git` file contains a `gitdir:` line.
func gitFilePointsAtGitdir(gitFile string) bool {
	f, err := os.Open(gitFile)
	if err != nil {
		return false
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if strings.HasPrefix(strings.TrimSpace(sc.Text()), "gitdir:") {
			return true
		}
	}
	return false
}

// worktreeBranch reads the linked worktree's HEAD ref (the gitdir's HEAD file)
// and returns its short branch name, or "" if detached/unreadable.
func worktreeBranch(gitFile string) string {
	gitdir := readGitdir(gitFile)
	if gitdir == "" {
		return ""
	}
	if !filepath.IsAbs(gitdir) {
		gitdir = filepath.Join(filepath.Dir(gitFile), gitdir)
	}
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
