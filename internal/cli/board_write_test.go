package cli

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/unsafe9/clutch/internal/model"
)

// execCmd runs the root command with args against a temp store, returning stdout.
func execCmd(t *testing.T, storeDir string, args ...string) (string, error) {
	t.Helper()
	t.Setenv("CLUTCH_STORE", storeDir)
	t.Setenv(confirmEnv, "")
	// Reset package-level flag globals between invocations.
	cfgPath, jsonOutput, assumeYes = "", false, false

	root := NewRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs(args)
	err := root.Execute()
	return out.String(), err
}

func TestSetPrinciplesThenReadReflectsIt(t *testing.T) {
	store := t.TempDir()

	if _, err := execCmd(t, store, "board", "set-principles", "task1",
		"-m", "ship small, verify often", "--yes"); err != nil {
		t.Fatalf("set-principles: %v", err)
	}

	out, err := execCmd(t, store, "board", "task1", "--json")
	if err != nil {
		t.Fatalf("board read: %v", err)
	}
	var b model.Board
	if err := json.Unmarshal([]byte(out), &b); err != nil {
		t.Fatalf("unmarshal board: %v\n%s", err, out)
	}
	if b.Principles != "ship small, verify often" {
		t.Fatalf("principles = %q, want %q", b.Principles, "ship small, verify often")
	}
}

func TestAppraiseWritesAndReadsBack(t *testing.T) {
	store := t.TempDir()

	if _, err := execCmd(t, store, "board", "appraise", "task2",
		"--kind", "classification",
		"--subject", "task:task2",
		"--result", "active",
		"--confidence", "0.8",
		"--fingerprint", "fp1",
		"--yes"); err != nil {
		t.Fatalf("appraise: %v", err)
	}

	out, err := execCmd(t, store, "board", "task2", "--json")
	if err != nil {
		t.Fatalf("board read: %v", err)
	}
	var b model.Board
	if err := json.Unmarshal([]byte(out), &b); err != nil {
		t.Fatalf("unmarshal board: %v\n%s", err, out)
	}
	if len(b.Appraisals) != 1 {
		t.Fatalf("appraisals = %d, want 1\n%s", len(b.Appraisals), out)
	}
	a := b.Appraisals[0]
	if a.Kind != model.AppraisalClassification || a.Subject != "task:task2" ||
		a.Result != "active" || a.Confidence != 0.8 || a.InputFingerprint != "fp1" {
		t.Fatalf("appraisal mismatch: %+v", a)
	}
	if a.ComputedAt.IsZero() {
		t.Fatalf("ComputedAt was not set")
	}
}

func TestAppraiseRejectsBadKindAndConfidence(t *testing.T) {
	store := t.TempDir()

	if _, err := execCmd(t, store, "board", "appraise", "task3",
		"--kind", "bogus", "--confidence", "0.5", "--yes"); err == nil {
		t.Fatal("appraise with unknown kind = nil, want error")
	}
	if _, err := execCmd(t, store, "board", "appraise", "task3",
		"--kind", "link", "--confidence", "1.5", "--yes"); err == nil {
		t.Fatal("appraise with confidence > 1 = nil, want error")
	}
	if _, err := execCmd(t, store, "board", "appraise", "task3",
		"--kind", "link", "--confidence", "1.0", "--yes"); err == nil {
		t.Fatal("appraise with confidence == 1.0 = nil, want error")
	}
	if _, err := execCmd(t, store, "board", "appraise", "task3",
		"--kind", "classification", "--result", "bogus", "--confidence", "0.5", "--yes"); err == nil {
		t.Fatal("appraise classification with non-lifecycle result = nil, want error")
	}
}

func TestAppraiseClassificationSubjectRules(t *testing.T) {
	store := t.TempDir()

	// Accept: classification whose subject is the task itself (task:<task-id>).
	if _, err := execCmd(t, store, "board", "appraise", "taskC",
		"--kind", "classification", "--subject", "task:taskC",
		"--result", "active", "--confidence", "0.5", "--yes"); err != nil {
		t.Fatalf("classification with task:<id> subject rejected: %v", err)
	}

	// Reject: subject naming a different task.
	if _, err := execCmd(t, store, "board", "appraise", "taskC",
		"--kind", "classification", "--subject", "task:other",
		"--result", "active", "--confidence", "0.5", "--yes"); err == nil {
		t.Fatal("classification with foreign task subject = nil, want error")
	}

	// Reject: a representation-ref subject.
	if _, err := execCmd(t, store, "board", "appraise", "taskC",
		"--kind", "classification", "--subject", "branch:acme/app/main",
		"--result", "active", "--confidence", "0.5", "--yes"); err == nil {
		t.Fatal("classification with representation subject = nil, want error")
	}

	// Reject: missing subject.
	if _, err := execCmd(t, store, "board", "appraise", "taskC",
		"--kind", "classification",
		"--result", "active", "--confidence", "0.5", "--yes"); err == nil {
		t.Fatal("classification with empty subject = nil, want error")
	}

	// Unchanged: relation/link keep representation-ref subjects.
	if _, err := execCmd(t, store, "board", "appraise", "taskC",
		"--kind", "relation", "--subject", "branch:acme/app/main",
		"--result", "depends:taskD", "--confidence", "0.5", "--yes"); err != nil {
		t.Fatalf("relation with representation subject rejected: %v", err)
	}
	if _, err := execCmd(t, store, "board", "appraise", "taskC",
		"--kind", "link", "--subject", "branch:acme/app/main",
		"--result", "branch:acme/app/main", "--confidence", "0.5", "--yes"); err != nil {
		t.Fatalf("link with representation subject rejected: %v", err)
	}
}

func TestWriteWithoutYesRejected(t *testing.T) {
	store := t.TempDir()

	if _, err := execCmd(t, store, "board", "set-design", "task4", "-m", "x"); err == nil {
		t.Fatal("set-design without --yes = nil, want rejection")
	}
}

func TestAddDecisionThenReadReflectsIt(t *testing.T) {
	store := t.TempDir()

	if _, err := execCmd(t, store, "board", "add-decision", "task5",
		"--summary", "use file store", "--detail", "simplest backend", "--yes"); err != nil {
		t.Fatalf("add-decision: %v", err)
	}

	out, err := execCmd(t, store, "board", "task5", "--json")
	if err != nil {
		t.Fatalf("board read: %v", err)
	}
	var b model.Board
	if err := json.Unmarshal([]byte(out), &b); err != nil {
		t.Fatalf("unmarshal board: %v\n%s", err, out)
	}
	if !strings.Contains(b.Design, "use file store") || !strings.Contains(b.Design, "simplest backend") {
		t.Fatalf("design = %q, want decision folded in", b.Design)
	}
}

func TestAddADRThenReadReflectsIt(t *testing.T) {
	store := t.TempDir()

	if _, err := execCmd(t, store, "board", "add-adr", "task6",
		"--decision", "adopt cobra", "--context", "need a CLI", "--consequence", "one dep", "--yes"); err != nil {
		t.Fatalf("add-adr: %v", err)
	}

	out, err := execCmd(t, store, "board", "task6", "--json")
	if err != nil {
		t.Fatalf("board read: %v", err)
	}
	var b model.Board
	if err := json.Unmarshal([]byte(out), &b); err != nil {
		t.Fatalf("unmarshal board: %v\n%s", err, out)
	}
	if len(b.ADRs) != 1 || b.ADRs[0].Decision != "adopt cobra" ||
		b.ADRs[0].Context != "need a CLI" || b.ADRs[0].Consequence != "one dep" {
		t.Fatalf("adrs = %+v, want one matching ADR", b.ADRs)
	}
}

func TestAddDecisionAndADRRequireFlags(t *testing.T) {
	store := t.TempDir()

	if _, err := execCmd(t, store, "board", "add-decision", "task7", "--yes"); err == nil {
		t.Fatal("add-decision without --summary = nil, want error")
	}
	if _, err := execCmd(t, store, "board", "add-adr", "task7", "--yes"); err == nil {
		t.Fatal("add-adr without --decision = nil, want error")
	}
}
