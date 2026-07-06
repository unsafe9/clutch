package cli

import (
	"time"

	"github.com/spf13/cobra"

	"github.com/unsafe9/clutch/internal/config"
	"github.com/unsafe9/clutch/internal/correlate"
	"github.com/unsafe9/clutch/internal/discover/fs"
	"github.com/unsafe9/clutch/internal/discover/git"
	"github.com/unsafe9/clutch/internal/discover/session"
	"github.com/unsafe9/clutch/internal/model"
	"github.com/unsafe9/clutch/internal/store/file"
)

// newScanCmd builds `clutch scan`: discover → correlate → project.
func newScanCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "scan",
		Short: "Scan configured roots and project the current Task set",
		RunE:  runScan,
	}
}

func runScan(cmd *cobra.Command, _ []string) error {
	env, err := project()
	if err != nil {
		return err
	}
	return emitEnvelope(cmd, env)
}

// project is the deterministic core pipeline and the composition root's wiring:
// load config → discover (git/fs/session) → correlate → envelope. The clock is
// the only non-deterministic input, read here (generated_at, scan duration) so
// the core stays reproducible.
func project() (model.ProjectionEnvelope, error) {
	start := time.Now()
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return model.ProjectionEnvelope{}, err
	}
	backend := file.New(cfg.StoreLocation)
	gitObs, err := git.Observe(cfg.SearchRoots)
	if err != nil {
		return model.ProjectionEnvelope{}, err
	}
	fsObs, err := fs.Observe(cfg.SearchRoots)
	if err != nil {
		return model.ProjectionEnvelope{}, err
	}
	sessObs, err := session.Observe(cfg.SearchRoots)
	if err != nil {
		return model.ProjectionEnvelope{}, err
	}
	obs := model.Observations{Git: gitObs, FS: fsObs, Sessions: sessObs}
	res, err := correlate.Correlate(obs, backend, backend, backend)
	if err != nil {
		return model.ProjectionEnvelope{}, err
	}
	tasks := res.Tasks
	if err := fillIdentity(tasks, backend); err != nil {
		return model.ProjectionEnvelope{}, err
	}
	generatedAt := time.Now()
	// The contract's machine shape renders empty collections as [], never null —
	// both the top-level Tasks slice and every documented array within each Task.
	if tasks == nil {
		tasks = []model.Task{}
	}
	for i := range tasks {
		normalizeTask(&tasks[i])
	}
	return model.ProjectionEnvelope{
		SchemaVersion: model.SchemaVersion,
		GeneratedAt:   generatedAt,
		Tasks:         tasks,
		Diagnostics: model.Diagnostics{
			Unresolved: promoteUnresolved(tasks, res.ScanWide),
			ScanStats:  scanStats(obs, tasks, generatedAt.Sub(start)),
		},
	}, nil
}

// promoteUnresolved builds the envelope's whole ambiguous remainder: every task's
// own Unresolved flags (in projection task order, then per-task order) followed by
// the scan-wide flags that belong to no task. The classify orchestrator reads it
// from diagnostics without walking each task. Each flag keeps its TaskID — a
// per-task flag carries its task's id; a scan-wide flag keeps the empty id that
// marks it. The result is never nil so an empty remainder marshals as [] per the
// contract's machine shape.
func promoteUnresolved(tasks []model.Task, scanWide []model.Unresolved) []model.Unresolved {
	out := []model.Unresolved{}
	for _, t := range tasks {
		out = append(out, t.Unresolved...)
	}
	out = append(out, scanWide...)
	return out
}

// scanStats summarizes the scan run. Repos and worktrees are distinct paths
// across BOTH the git and fs producers, which overlap (each surfaces the same
// checkouts); they are deduped by path, not identity, because git and fs assign a
// repo divergent identities (remote-based vs path-based) that only its path
// unifies. Worktrees counts every working tree git enumerates, the primary
// checkout included. Sessions counts every in-scope discovered session (those
// within a configured search root), matched to a task or not; out-of-scope
// sessions are dropped upstream and never reach here.
func scanStats(obs model.Observations, tasks []model.Task, d time.Duration) model.ScanStats {
	repos := map[string]bool{}
	worktrees := map[string]bool{}
	for _, g := range obs.Git {
		repos[g.Repo.Path] = true
		for _, w := range g.Worktrees {
			worktrees[w.Path] = true
		}
	}
	for _, f := range obs.FS {
		repos[f.Repo.Path] = true
		for _, w := range f.Worktrees {
			worktrees[w.Path] = true
		}
	}
	return model.ScanStats{
		ReposScanned:   len(repos),
		Worktrees:      len(worktrees),
		Sessions:       len(obs.Sessions),
		TasksProjected: len(tasks),
		DurationMS:     d.Milliseconds(),
	}
}
