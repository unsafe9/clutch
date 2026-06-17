package cli

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/unsafe9/clutch/internal/model"
)

// execCmd runs the root command with args against a temp store, returning stdout.
func execCmd(t *testing.T, storeDir string, args ...string) (string, error) {
	t.Helper()
	t.Setenv("CLUTCH_STORE", storeDir)
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
		"--subject", "branch:alpha/main",
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
	if a.Kind != model.AppraisalClassification || a.Subject != "branch:alpha/main" ||
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
}

func TestWriteWithoutYesRejected(t *testing.T) {
	store := t.TempDir()

	if _, err := execCmd(t, store, "board", "set-design", "task4", "-m", "x"); err == nil {
		t.Fatal("set-design without --yes = nil, want rejection")
	}
}
