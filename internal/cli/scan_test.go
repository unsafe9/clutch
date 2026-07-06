package cli

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/unsafe9/clutch/internal/model"
)

// promoteUnresolved unions per-task flags (in projection task order, each keeping
// its own TaskID) with the scan-wide flags, which are passed separately and
// appended last with their empty TaskID intact.
func TestPromoteUnresolved(t *testing.T) {
	tasks := []model.Task{
		{ID: "t1", Unresolved: []model.Unresolved{
			{Kind: model.UnresolvedIdentity, Detail: "bound to t1", TaskID: "t1"},
		}},
		{ID: "t2"},
		{ID: "t3", Unresolved: []model.Unresolved{
			{Kind: model.UnresolvedLineage, Detail: "bound to t3", TaskID: "t3"},
		}},
	}
	scanWide := []model.Unresolved{
		{Kind: model.UnresolvedSession, Detail: "scan-wide", TaskID: ""},
	}

	got := promoteUnresolved(tasks, scanWide)
	if len(got) != 3 {
		t.Fatalf("promoteUnresolved len = %d, want 3: %+v", len(got), got)
	}
	want := []struct {
		detail string
		taskID string
	}{
		{"bound to t1", "t1"},
		{"bound to t3", "t3"},
		{"scan-wide", ""},
	}
	for i, w := range want {
		if got[i].Detail != w.detail || got[i].TaskID != w.taskID {
			t.Errorf("flag[%d] = {%q, %q}, want {%q, %q}",
				i, got[i].Detail, got[i].TaskID, w.detail, w.taskID)
		}
	}
}

// An empty remainder must marshal as [] per the contract's machine shape, so the
// helper returns a non-nil slice (nil would render null).
func TestPromoteUnresolvedEmpty(t *testing.T) {
	got := promoteUnresolved([]model.Task{{ID: "t1"}}, nil)
	if len(got) != 0 {
		t.Fatalf("promoteUnresolved with no flags = %+v, want empty", got)
	}
	if got == nil {
		t.Fatal("promoteUnresolved with no flags = nil, want non-nil empty slice")
	}
	b, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if string(b) != "[]" {
		t.Fatalf("empty unresolved marshals as %s, want []", b)
	}
}

// scanStats dedups repos and worktrees by path across the overlapping git and fs
// producers, counts every discovered session (matched or not), reports the task
// count, and carries the duration in whole milliseconds.
func TestScanStats(t *testing.T) {
	obs := model.Observations{
		Git: []model.GitObservation{
			{
				Repo:      model.RepoRef{Path: "/r/alpha"},
				Worktrees: []model.Worktree{{Path: "/r/alpha"}, {Path: "/r/alpha-wt"}},
			},
			{
				Repo:      model.RepoRef{Path: "/r/beta"},
				Worktrees: []model.Worktree{{Path: "/r/beta"}},
			},
		},
		FS: []model.FSObservation{
			// Overlaps the git observation of alpha (same paths) — must not double
			// count.
			{
				Repo:      model.RepoRef{Path: "/r/alpha"},
				Worktrees: []model.Worktree{{Path: "/r/alpha-wt"}},
			},
			// An fs-only repo git never observed still counts as scanned.
			{Repo: model.RepoRef{Path: "/r/gamma"}},
		},
		Sessions: []model.SessionObservation{{}, {}},
	}
	tasks := []model.Task{{ID: "t1"}, {ID: "t2"}}

	got := scanStats(obs, tasks, 1500*time.Millisecond)
	want := model.ScanStats{
		ReposScanned:   3, // alpha, beta, gamma
		Worktrees:      3, // alpha, alpha-wt, beta
		Sessions:       2,
		TasksProjected: 2,
		DurationMS:     1500,
	}
	if got != want {
		t.Fatalf("scanStats = %+v, want %+v", got, want)
	}
}
