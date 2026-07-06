package cli

import (
	"encoding/json"
	"testing"

	"github.com/unsafe9/clutch/internal/model"
)

// createTask runs `task new` and returns the minted id from the confirm JSON.
func createTask(t *testing.T, store string, args ...string) string {
	t.Helper()
	full := append([]string{"task", "new", "--json", "--yes"}, args...)
	out, err := execCmd(t, store, full...)
	if err != nil {
		t.Fatalf("task new %v: %v\n%s", args, err, out)
	}
	var confirm struct {
		TaskID string `json:"task_id"`
		Action string `json:"action"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal([]byte(out), &confirm); err != nil {
		t.Fatalf("unmarshal confirm: %v\n%s", err, out)
	}
	if confirm.TaskID == "" || confirm.Status != "ok" {
		t.Fatalf("confirm = %+v, want a task_id and ok status", confirm)
	}
	return confirm.TaskID
}

func TestTaskNewMintsID(t *testing.T) {
	store := t.TempDir()
	id := createTask(t, store, "--title", "spike the parser")
	if len(id) == 0 {
		t.Fatal("empty id")
	}
}

// Each invocation mints a distinct id even with an identical title: a task id is
// independent of any label.
func TestTaskNewDistinctIDsForSameTitle(t *testing.T) {
	store := t.TempDir()
	a := createTask(t, store, "--title", "same title")
	b := createTask(t, store, "--title", "same title")
	if a == b {
		t.Fatalf("two `task new` runs reused id %q, want distinct ids", a)
	}
}

func TestTaskNewWithoutYesRejected(t *testing.T) {
	store := t.TempDir()
	if _, err := execCmd(t, store, "task", "new", "--title", "x"); err == nil {
		t.Fatal("task new without --yes = nil, want safety-gate rejection")
	}
}

func TestTaskNewRequiresTitle(t *testing.T) {
	store := t.TempDir()
	if _, err := execCmd(t, store, "task", "new", "--yes"); err == nil {
		t.Fatal("task new without --title = nil, want error")
	}
}

func TestTaskNewRejectsUnknownMode(t *testing.T) {
	store := t.TempDir()
	if _, err := execCmd(t, store, "task", "new", "--title", "x", "--mode", "bogus", "--yes"); err == nil {
		t.Fatal("task new with unknown mode = nil, want error")
	}
}

// A registry-only clutch-initiated task must fold into the projection emitted by
// `tasks` and be resolvable by `task <id>`, even with no git representation. The
// scan is made hermetic by pointing search roots and HOME at empty temp dirs, so
// git/fs/session discovery yields nothing and only the registry-only task shows.
func TestTaskNewProjectsInTasksAndShow(t *testing.T) {
	store := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CLUTCH_SEARCH_ROOTS", t.TempDir())

	id := createTask(t, store, "--title", "spike the parser", "--mode", "steer")

	out, err := execCmd(t, store, "tasks", "--json")
	if err != nil {
		t.Fatalf("tasks: %v\n%s", err, out)
	}
	var env model.ProjectionEnvelope
	if err := json.Unmarshal([]byte(out), &env); err != nil {
		t.Fatalf("unmarshal envelope: %v\n%s", err, out)
	}
	var got *model.Task
	for i := range env.Tasks {
		if env.Tasks[i].ID == id {
			got = &env.Tasks[i]
		}
	}
	if got == nil {
		t.Fatalf("registry-only task %q absent from tasks projection:\n%s", id, out)
	}
	if got.Provenance != model.ProvenanceClutchInitiated {
		t.Errorf("provenance = %q, want clutch-initiated", got.Provenance)
	}
	if got.Lifecycle != model.LifecycleIdea {
		t.Errorf("lifecycle = %q, want idea", got.Lifecycle)
	}
	if got.Title != "spike the parser" || got.Mode != model.ModeSteer {
		t.Errorf("title/mode = %q/%q, want %q/steer", got.Title, got.Mode, "spike the parser")
	}
	if got.Created.IsZero() {
		t.Errorf("created not set")
	}

	// `task <id>` resolves the same registry-only task.
	showOut, err := execCmd(t, store, "task", id, "--json")
	if err != nil {
		t.Fatalf("task show: %v\n%s", err, showOut)
	}
	var showEnv model.ProjectionEnvelope
	if err := json.Unmarshal([]byte(showOut), &showEnv); err != nil {
		t.Fatalf("unmarshal show envelope: %v\n%s", err, showOut)
	}
	if len(showEnv.Tasks) != 1 || showEnv.Tasks[0].ID != id {
		t.Fatalf("task show returned %d tasks, want exactly task %q", len(showEnv.Tasks), id)
	}
}
