package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/unsafe9/clutch/internal/correlate"
	"github.com/unsafe9/clutch/internal/discover/fs"
	"github.com/unsafe9/clutch/internal/discover/git"
	"github.com/unsafe9/clutch/internal/model"
	"github.com/unsafe9/clutch/internal/store/file"
)

// TestGoldenE2E drives the real deterministic core end-to-end against fixture
// git repos built on disk: git.Observe + fs.Observe (with an EMPTY session set,
// since session.Observe reads the real user home and would be non-deterministic)
// → correlate.Correlate over a fresh file backend → ProjectionEnvelope → emitJSON.
//
// It asserts the schema version, the tasks correlated from the fixtures, and —
// because the emitted JSON embeds absolute t.TempDir() paths that vary per run —
// determinism is checked by re-running the whole pipeline and requiring the two
// renders be BYTE-IDENTICAL, rather than freezing a byte-golden with baked-in
// temp paths.
func TestGoldenE2E(t *testing.T) {
	root := t.TempDir()
	storeDir := t.TempDir()

	// alpha: a repo with two branches (main + feature/x) and a linked worktree.
	alpha := filepath.Join(root, "alpha")
	initRepo(t, alpha)
	commit(t, alpha, "a1.txt", "one")
	gitRun(t, alpha, "branch", "feature/x")
	wt := filepath.Join(root, "alpha-wt")
	gitRun(t, alpha, "worktree", "add", wt, "feature/x")
	// Give feature/x a divergent commit so it is genuinely ahead of main
	// (its tip is not an ancestor of main → unmerged, not merged).
	commit(t, wt, "x1.txt", "feature work")

	// beta: a second, independent single-branch repo.
	beta := filepath.Join(root, "beta")
	initRepo(t, beta)
	commit(t, beta, "b1.txt", "one")

	render := func() []byte {
		gitObs, err := git.Observe([]string{root})
		if err != nil {
			t.Fatalf("git.Observe: %v", err)
		}
		fsObs, err := fs.Observe([]string{root})
		if err != nil {
			t.Fatalf("fs.Observe: %v", err)
		}
		obs := model.Observations{Git: gitObs, FS: fsObs, Sessions: nil}

		backend := file.New(storeDir)
		res, err := correlate.Correlate(obs, backend, backend, backend, backend)
		if err != nil {
			t.Fatalf("correlate.Correlate: %v", err)
		}
		// Stamp Class-① identity metadata (created/updated/mode) from the store,
		// exactly as project() does. Across the two renders these come from the
		// SAME persisted registry (first render mints+stamps, second reuses), so
		// they stay byte-identical without freezing the store clock.
		if err := fillIdentity(res.Tasks, backend); err != nil {
			t.Fatalf("fillIdentity: %v", err)
		}
		// Coerce empty arrays to [] exactly as project() does, so the contract's
		// arrays-never-null rule is exercised end-to-end.
		for i := range res.Tasks {
			normalizeTask(&res.Tasks[i])
		}
		// GeneratedAt and scan duration are the only non-deterministic envelope
		// inputs (a wall clock); freeze both so the two renders stay
		// byte-identical, mirroring how project() reads the clock in one place.
		env := model.ProjectionEnvelope{
			SchemaVersion: model.SchemaVersion,
			GeneratedAt:   time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
			Tasks:         res.Tasks,
			Diagnostics: model.Diagnostics{
				Unresolved: promoteUnresolved(res.Tasks, res.ScanWide),
				ScanStats:  scanStats(obs, res.Tasks, 0),
			},
		}

		var buf bytes.Buffer
		if err := emitJSON(&buf, env); err != nil {
			t.Fatalf("emitJSON: %v", err)
		}
		return buf.Bytes()
	}

	first := render()

	var env model.ProjectionEnvelope
	if err := json.Unmarshal(first, &env); err != nil {
		t.Fatalf("unmarshal rendered JSON: %v", err)
	}

	if env.SchemaVersion != "0.1" {
		t.Fatalf("schema_version = %q, want %q", env.SchemaVersion, "0.1")
	}

	// The deterministic correlation anchors one task per (repo-identity, branch)
	// signature. The linked worktree alpha-wt is NOT a separate repo identity: it
	// resolves back to alpha's main repo (identity-correctness fix) and surfaces
	// only as a model.Worktree on alpha's task(s). So the task set is exactly the
	// distinct branch signatures: alpha/main, alpha/feature/x, and beta/main —
	// three tasks. Pre-fix, alpha-wt minted a phantom local/alpha-wt identity and
	// re-enumerated alpha's two branches, inflating this to five.
	if got := len(env.Tasks); got != 3 {
		t.Fatalf("tasks = %d, want 3\n%s", got, first)
	}

	// Envelope diagnostics: generated_at round-trips the frozen clock, and
	// scan_stats reflects the fixtures — alpha + beta are two repos; git's
	// worktree list surfaces three working trees (alpha's primary, alpha's linked
	// alpha-wt, beta's primary); no sessions were fed; three tasks were projected.
	// With no sessions there is nothing the core cannot resolve, so unresolved is
	// empty.
	if !env.GeneratedAt.Equal(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("generated_at = %v, want frozen 2026-01-01", env.GeneratedAt)
	}
	stats := env.Diagnostics.ScanStats
	if stats.ReposScanned != 2 {
		t.Errorf("scan_stats.repos_scanned = %d, want 2", stats.ReposScanned)
	}
	if stats.Worktrees != 3 {
		t.Errorf("scan_stats.worktrees = %d, want 3", stats.Worktrees)
	}
	if stats.Sessions != 0 {
		t.Errorf("scan_stats.sessions = %d, want 0", stats.Sessions)
	}
	if stats.TasksProjected != len(env.Tasks) {
		t.Errorf("scan_stats.tasks_projected = %d, want %d", stats.TasksProjected, len(env.Tasks))
	}
	if stats.DurationMS != 0 {
		t.Errorf("scan_stats.duration_ms = %d, want frozen 0", stats.DurationMS)
	}
	if len(env.Diagnostics.Unresolved) != 0 {
		t.Errorf("diagnostics.unresolved = %v, want empty", env.Diagnostics.Unresolved)
	}
	// The contract renders empty collections as [], never null — a null in the
	// emitted JSON would unmarshal back to a nil slice.
	if env.Diagnostics.Unresolved == nil {
		t.Errorf("diagnostics.unresolved rendered as null, want []")
	}

	// Every task carries a minted id, git-detected provenance, an active
	// lifecycle (each fixture branch has a commit head), and exactly one branch.
	branchNames := map[string]bool{}
	worktreeSeen := false
	for _, tk := range env.Tasks {
		if tk.ID == "" {
			t.Errorf("task has empty id: %+v", tk)
		}
		if tk.Provenance != model.ProvenanceGitDetected {
			t.Errorf("task %s provenance = %q, want git-detected", tk.ID, tk.Provenance)
		}
		if tk.Lifecycle != model.LifecycleActive {
			t.Errorf("task %s lifecycle = %q, want active", tk.ID, tk.Lifecycle)
		}
		// Identity metadata: created/updated are stamped from the store (a
		// freshly-minted task has created == updated, both non-zero), and the
		// effective mode defaults to the human-in-the-loop "steer" since no
		// policy mode is stored.
		if tk.Created.IsZero() || !tk.Created.Equal(tk.Updated) {
			t.Errorf("task %s created/updated = %v/%v, want equal non-zero", tk.ID, tk.Created, tk.Updated)
		}
		if tk.Mode != model.ModeSteer {
			t.Errorf("task %s mode = %q, want steer", tk.ID, tk.Mode)
		}
		// Arrays-never-null: every contract-documented per-task array must
		// round-trip as a non-nil slice (a null would unmarshal back to nil).
		// The fixtures leave prs/issues/sessions/lineage/relations empty, so
		// these directly catch a regressed [] → null.
		if tk.Repos == nil || tk.Branches == nil || tk.Worktrees == nil ||
			tk.PRs == nil || tk.Issues == nil || tk.Sessions == nil ||
			tk.Links == nil || tk.Unresolved == nil ||
			tk.Lineage.Parents == nil ||
			tk.Relations.Depends == nil || tk.Relations.Blocks == nil {
			t.Errorf("task %s has a null array (want [] for all documented arrays): %+v", tk.ID, tk)
		}
		if len(tk.Branches) != 1 {
			t.Errorf("task %s has %d branches, want 1", tk.ID, len(tk.Branches))
			continue
		}
		branchNames[tk.Branches[0].Name] = true
		if len(tk.Worktrees) > 0 {
			worktreeSeen = true
		}
	}
	for _, want := range []string{"main", "feature/x"} {
		if !branchNames[want] {
			t.Errorf("expected a task for branch %q; got branches %v", want, branchNames)
		}
	}
	if !worktreeSeen {
		t.Errorf("expected the linked worktree to be correlated onto a task")
	}

	// Determinism: a second full run over the same fixtures and the same store
	// must produce byte-identical JSON.
	second := render()
	if !bytes.Equal(first, second) {
		t.Fatalf("non-deterministic output:\n--- first ---\n%s\n--- second ---\n%s", first, second)
	}
}

func initRepo(t *testing.T, dir string) {
	t.Helper()
	if err := exec.Command("git", "init", "-b", "main", dir).Run(); err != nil {
		t.Fatalf("git init %s: %v", dir, err)
	}
	gitRun(t, dir, "config", "user.email", "test@example.com")
	gitRun(t, dir, "config", "user.name", "clutch test")
}

func commit(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	gitRun(t, dir, "add", name)
	gitRun(t, dir, "commit", "-m", "add "+name)
}

func gitRun(t *testing.T, dir string, args ...string) {
	t.Helper()
	full := append([]string{"-C", dir}, args...)
	out, err := exec.Command("git", full...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}
