// Package git produces raw git observations via `git`/`gh` shell-out.
//
// There is deliberately NO common Discoverer interface: git, fs and session are
// distinct producers exposed as concrete functions. No git Go library is used.
package git

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/unsafe9/clutch/internal/model"
)

// Observe returns git observations for the repos found under roots, shelling
// out to `git` (branches, worktrees, commits, fork-point) and `gh` (PRs).
//
// A single unreadable or broken repo is skipped rather than aborting the scan;
// an error is returned only on a catastrophic failure to enumerate roots.
func Observe(roots []string) ([]model.GitObservation, error) {
	var out []model.GitObservation
	seen := map[string]bool{}
	for _, root := range roots {
		repos, err := findRepos(root)
		if err != nil {
			return nil, err
		}
		for _, repo := range repos {
			// A linked worktree (.git is a FILE pointing into the main repo's
			// .git/worktrees/<name>) shares the main repo's identity and refs.
			// Observing it as its own repo would re-enumerate the parent's
			// branches under a separate path-based identity, spawning phantom
			// duplicate tasks. Resolve the MAIN repo instead; it surfaces the
			// worktree via `git worktree list` as a model.Worktree on its task.
			if main, ok := mainRepoOf(repo); ok {
				repo = main
			}
			// Canonicalize before deduping: a worktree resolves to its main repo
			// via git's already-canonicalized gitdir, while the main repo may be
			// walked under a symlinked root (e.g. /var → /private/var on macOS).
			// Without this the two would not collapse to one observation.
			if real, err := filepath.EvalSymlinks(repo); err == nil {
				repo = real
			}
			if seen[repo] {
				continue
			}
			seen[repo] = true
			obs, ok := observeRepo(repo)
			if !ok {
				continue
			}
			out = append(out, obs)
		}
	}
	return out, nil
}

// mainRepoOf reports, for a linked worktree, the working directory of its MAIN
// repository (resolved from the git common dir). It returns ok=false for a
// primary repo (whose .git is a directory) or any dir that is not a linked
// worktree. A primary repo is identified by its .git being a directory, matching
// the fs discoverer's marker test, so the two producers agree on what counts as
// a worktree.
func mainRepoOf(dir string) (string, bool) {
	info, err := os.Lstat(filepath.Join(dir, ".git"))
	if err != nil || info.IsDir() {
		return "", false
	}
	commonDir, err := git(dir, "rev-parse", "--absolute-git-dir", "--git-common-dir")
	if err != nil {
		return "", false
	}
	// --absolute-git-dir is the worktree's own gitdir; --git-common-dir is the
	// main repo's .git. If they are equal this is not a linked worktree.
	lines := strings.Split(commonDir, "\n")
	if len(lines) != 2 {
		return "", false
	}
	gitDir, common := strings.TrimSpace(lines[0]), strings.TrimSpace(lines[1])
	if common == "" || gitDir == common {
		return "", false
	}
	if !filepath.IsAbs(common) {
		common = filepath.Join(dir, common)
	}
	return filepath.Dir(common), true
}

// findRepos walks root and returns the absolute paths of every git repository
// (a directory containing a `.git` dir or file). It does not descend into `.git`
// directories. A walk error on an individual entry is skipped.
func findRepos(root string) ([]string, error) {
	var repos []string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			if path == root {
				return err
			}
			return nil
		}
		if !d.IsDir() {
			return nil
		}
		if d.Name() == ".git" && path != root {
			return filepath.SkipDir
		}
		if isRepo(path) {
			repos = append(repos, path)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return repos, nil
}

// isRepo reports whether dir contains a `.git` directory or file.
func isRepo(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, ".git"))
	return err == nil
}

// observeRepo gathers a GitObservation for one repo. It returns ok=false when
// the repo is unreadable, so the caller can skip it without aborting the scan.
func observeRepo(repo string) (model.GitObservation, bool) {
	// HEAD objectname doubles as a readability probe.
	head, err := git(repo, "rev-parse", "HEAD")
	if err != nil {
		return model.GitObservation{}, false
	}

	remote, _ := git(repo, "config", "--get", "remote.origin.url")
	identity := deriveIdentity(remote, repo)

	defaultBranch := defaultBranch(repo)
	branches := observeBranches(repo, identity, defaultBranch)
	worktrees := observeWorktrees(repo, identity)

	commits := model.CommitSummary{Head: head}
	if c, err := git(repo, "rev-list", "--count", "HEAD"); err == nil {
		commits.Count, _ = strconv.Atoi(c)
	}

	prs := observePRs(identity, remote)

	return model.GitObservation{
		Repo: model.RepoRef{
			Ref:      model.RepRef("repo:" + identity),
			Identity: identity,
			Path:     repo,
			Remote:   remote,
		},
		Branches:  branches,
		Worktrees: worktrees,
		Commits:   commits,
		PRs:       prs,
	}, true
}

// observeBranches enumerates local branches and computes per-branch upstream,
// ahead/behind, fork-point base, and integration state.
func observeBranches(repo, identity, defaultBranch string) []model.Branch {
	const sep = "\x1f"
	format := strings.Join([]string{"%(refname:short)", "%(objectname)", "%(upstream:short)"}, sep)
	raw, err := git(repo, "for-each-ref", "--format="+format, "refs/heads")
	if err != nil || raw == "" {
		return nil
	}

	var branches []model.Branch
	for _, line := range strings.Split(raw, "\n") {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, sep, 3)
		if len(parts) < 3 {
			continue
		}
		name, headObj, upstream := parts[0], parts[1], parts[2]

		b := model.Branch{
			Ref:      model.RepRef("branch:" + identity + "/" + name),
			Repo:     identity,
			Name:     name,
			Head:     headObj,
			Upstream: upstream,
		}

		if upstream != "" {
			if lr, err := git(repo, "rev-list", "--left-right", "--count", name+"..."+upstream); err == nil {
				fields := strings.Fields(lr)
				if len(fields) == 2 {
					b.Ahead, _ = strconv.Atoi(fields[0])
					b.Behind, _ = strconv.Atoi(fields[1])
				}
			}
		}

		if defaultBranch != "" && name != defaultBranch {
			if base, err := git(repo, "merge-base", name, defaultBranch); err == nil {
				b.Base = base
			}
		}

		b.Integration = integration(repo, b)
		branches = append(branches, b)
	}
	return branches
}

// integration derives the branch's integration state vs its base. Merged wins
// when the branch tip is an ancestor of its base; otherwise behind when behind>0
// and ahead==0; else unmerged.
func integration(repo string, b model.Branch) model.Integration {
	if b.Base != "" {
		if err := git0(repo, "merge-base", "--is-ancestor", b.Head, b.Base); err == nil {
			return model.IntegrationMerged
		}
	}
	if b.Behind > 0 && b.Ahead == 0 {
		return model.IntegrationBehind
	}
	return model.IntegrationUnmerged
}

// observeWorktrees lists linked worktrees via `git worktree list --porcelain`.
func observeWorktrees(repo, identity string) []model.Worktree {
	raw, err := git(repo, "worktree", "list", "--porcelain")
	if err != nil || raw == "" {
		return nil
	}

	var worktrees []model.Worktree
	var cur model.Worktree
	flush := func() {
		if cur.Path != "" {
			cur.Ref = model.RepRef("worktree:" + cur.Path)
			cur.Repo = identity
			worktrees = append(worktrees, cur)
		}
		cur = model.Worktree{}
	}
	for _, line := range strings.Split(raw, "\n") {
		switch {
		case line == "":
			flush()
		case strings.HasPrefix(line, "worktree "):
			cur.Path = strings.TrimPrefix(line, "worktree ")
		case strings.HasPrefix(line, "branch "):
			cur.Branch = shortBranch(strings.TrimPrefix(line, "branch "))
		}
	}
	flush()
	return worktrees
}

// ghHost is the only PR host clutch knows how to query (via the `gh` CLI).
const ghHost = "github.com"

// observePRs fetches open PRs for the repo via `gh pr list`. It skips cleanly
// (returns nil) when the remote is empty, the identity is not a github.com repo,
// the `gh` binary is absent, or the command fails (auth/network/non-repo) — so
// a missing or unauthenticated gh never aborts or pollutes the scan.
func observePRs(identity, remote string) []model.PullRequest {
	if remote == "" || !strings.HasPrefix(identity, ghHost+"/") {
		return nil
	}
	if _, err := exec.LookPath("gh"); err != nil {
		return nil
	}
	raw, err := run("", "gh", "pr", "list", "-R", identity,
		"--json", "number,url,state,isDraft,statusCheckRollup")
	if err != nil {
		return nil
	}
	return parsePRs(raw, ghHost)
}

// parsePRs decodes `gh pr list --json ...` output into PullRequest reps. A
// decode failure yields nil (best-effort). checks is a coarse rollup of the
// statusCheckRollup states.
func parsePRs(raw, host string) []model.PullRequest {
	var rows []struct {
		Number            int    `json:"number"`
		URL               string `json:"url"`
		State             string `json:"state"`
		IsDraft           bool   `json:"isDraft"`
		StatusCheckRollup []struct {
			State      string `json:"state"`
			Conclusion string `json:"conclusion"`
		} `json:"statusCheckRollup"`
	}
	if err := json.Unmarshal([]byte(raw), &rows); err != nil {
		return nil
	}
	var prs []model.PullRequest
	for _, r := range rows {
		prs = append(prs, model.PullRequest{
			Ref:    model.RepRef("pr:" + host + "#" + strconv.Itoa(r.Number)),
			Host:   host,
			Number: r.Number,
			URL:    r.URL,
			State:  r.State,
			Draft:  r.IsDraft,
			Checks: rollupChecks(r.StatusCheckRollup),
		})
	}
	return prs
}

// rollupChecks reduces per-check states into a single coarse label.
func rollupChecks(checks []struct {
	State      string `json:"state"`
	Conclusion string `json:"conclusion"`
}) string {
	if len(checks) == 0 {
		return ""
	}
	failed, pending := false, false
	for _, c := range checks {
		st := c.Conclusion
		if st == "" {
			st = c.State
		}
		switch strings.ToUpper(st) {
		case "SUCCESS", "NEUTRAL", "SKIPPED":
		case "FAILURE", "ERROR", "CANCELLED", "TIMED_OUT", "ACTION_REQUIRED", "STARTUP_FAILURE":
			failed = true
		default:
			pending = true
		}
	}
	switch {
	case failed:
		return "failure"
	case pending:
		return "pending"
	default:
		return "success"
	}
}

// deriveIdentity returns a durable identity for the repo. It normalizes git and
// https remotes to host/owner/repo; with no remote it falls back to a stable
// path-based identity (NOT the checkout path semantics — just a last resort).
func deriveIdentity(remote, repo string) string {
	if id := normalizeRemote(remote); id != "" {
		return id
	}
	return "local/" + filepath.Base(repo)
}

// normalizeRemote canonicalizes a git remote URL to host/owner/repo, dropping
// any scheme, userinfo, port, and trailing .git. Returns "" if remote is empty.
func normalizeRemote(remote string) string {
	remote = strings.TrimSpace(remote)
	if remote == "" {
		return ""
	}
	s := remote

	switch {
	case strings.HasPrefix(s, "git@"):
		// git@host:owner/repo(.git)
		s = strings.TrimPrefix(s, "git@")
		s = strings.Replace(s, ":", "/", 1)
	case strings.Contains(s, "://"):
		// scheme://[user@]host[:port]/owner/repo(.git)
		s = s[strings.Index(s, "://")+3:]
		if at := strings.LastIndex(s, "@"); at != -1 {
			s = s[at+1:]
		}
	default:
		// host:owner/repo or already host/owner/repo
		s = strings.Replace(s, ":", "/", 1)
	}

	s = strings.TrimSuffix(s, "/")
	s = strings.TrimSuffix(s, ".git")

	// Drop a port on the host segment (host:port/owner/repo).
	if i := strings.Index(s, "/"); i != -1 {
		host, rest := s[:i], s[i:]
		if c := strings.Index(host, ":"); c != -1 {
			host = host[:c]
		}
		s = host + rest
	}
	return s
}

// defaultBranch resolves the repo's default branch: origin/HEAD's target, then
// main, then master. Empty if none resolve.
func defaultBranch(repo string) string {
	if ref, err := git(repo, "symbolic-ref", "--quiet", "--short", "refs/remotes/origin/HEAD"); err == nil && ref != "" {
		return strings.TrimPrefix(ref, "origin/")
	}
	for _, name := range []string{"main", "master"} {
		if err := git0(repo, "rev-parse", "--verify", "--quiet", "refs/heads/"+name); err == nil {
			return name
		}
	}
	return ""
}

// shortBranch trims a full ref (refs/heads/x) to its short name.
func shortBranch(ref string) string {
	return strings.TrimPrefix(ref, "refs/heads/")
}

// git runs `git -C <repo> args...` and returns trimmed stdout.
func git(repo string, args ...string) (string, error) {
	full := append([]string{"-C", repo}, args...)
	return run("", "git", full...)
}

// git0 runs a git command for its exit status only.
func git0(repo string, args ...string) error {
	_, err := git(repo, args...)
	return err
}

// run executes name with args (optionally in dir) and returns trimmed stdout.
func run(dir, name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	if dir != "" {
		cmd.Dir = dir
	}
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}
