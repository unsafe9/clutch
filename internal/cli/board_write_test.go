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

func TestAddQuestionThenResolveReadFlow(t *testing.T) {
	store := t.TempDir()

	// Three questions.
	for _, text := range []string{"which backend?", "how to key sessions?", "which timezone?"} {
		if _, err := execCmd(t, store, "board", "add-question", "taskQ",
			"--text", text, "--yes"); err != nil {
			t.Fatalf("add-question %q: %v", text, err)
		}
	}

	// Resolve #1, defer #2, leave #3 open.
	if _, err := execCmd(t, store, "board", "resolve-question", "taskQ",
		"--id", "1", "--resolution", "the file store", "--yes"); err != nil {
		t.Fatalf("resolve-question: %v", err)
	}
	if _, err := execCmd(t, store, "board", "resolve-question", "taskQ",
		"--id", "2", "--resolution", "out of scope", "--defer", "--yes"); err != nil {
		t.Fatalf("resolve-question --defer: %v", err)
	}

	// JSON read: three questions, 1-based ids, resolved/deferred/open statuses.
	out, err := execCmd(t, store, "board", "taskQ", "--json")
	if err != nil {
		t.Fatalf("board read json: %v", err)
	}
	var b model.Board
	if err := json.Unmarshal([]byte(out), &b); err != nil {
		t.Fatalf("unmarshal board: %v\n%s", err, out)
	}
	if len(b.Questions) != 3 {
		t.Fatalf("questions = %d, want 3\n%s", len(b.Questions), out)
	}
	if b.Questions[0].ID != 1 || b.Questions[0].Status != model.QuestionResolved ||
		b.Questions[0].Resolution != "the file store" {
		t.Fatalf("q1 = %+v", b.Questions[0])
	}
	if b.Questions[1].ID != 2 || b.Questions[1].Status != model.QuestionDeferred ||
		b.Questions[1].Resolution != "out of scope" {
		t.Fatalf("q2 = %+v", b.Questions[1])
	}
	if b.Questions[2].ID != 3 || b.Questions[2].Status != model.QuestionOpen {
		t.Fatalf("q3 = %+v", b.Questions[2])
	}

	// Human read: the Questions section renders open and closed forms.
	human, err := execCmd(t, store, "board", "taskQ")
	if err != nil {
		t.Fatalf("board read human: %v", err)
	}
	for _, want := range []string{
		"Questions:",
		"- #1 [resolved] which backend? -> the file store",
		"- #2 [deferred] how to key sessions? -> out of scope",
		"- #3 [open] which timezone?",
	} {
		if !strings.Contains(human, want) {
			t.Errorf("human board missing %q:\n%s", want, human)
		}
	}
}

func TestQuestionCommandsRequireFlags(t *testing.T) {
	store := t.TempDir()

	// add-question requires --text.
	if _, err := execCmd(t, store, "board", "add-question", "t", "--yes"); err == nil {
		t.Fatal("add-question without --text = nil, want error")
	}
	// resolve-question requires --id > 0.
	if _, err := execCmd(t, store, "board", "resolve-question", "t",
		"--resolution", "x", "--yes"); err == nil {
		t.Fatal("resolve-question without --id = nil, want error")
	}
	if _, err := execCmd(t, store, "board", "resolve-question", "t",
		"--id", "0", "--resolution", "x", "--yes"); err == nil {
		t.Fatal("resolve-question with --id 0 = nil, want error")
	}
	// resolve-question requires --resolution.
	if _, err := execCmd(t, store, "board", "resolve-question", "t",
		"--id", "1", "--yes"); err == nil {
		t.Fatal("resolve-question without --resolution = nil, want error")
	}
	// Unknown id surfaces the store's not-found error.
	if _, err := execCmd(t, store, "board", "add-question", "t", "--text", "q", "--yes"); err != nil {
		t.Fatalf("seed add-question: %v", err)
	}
	if _, err := execCmd(t, store, "board", "resolve-question", "t",
		"--id", "99", "--resolution", "x", "--yes"); err == nil {
		t.Fatal("resolve-question with unknown id = nil, want error")
	}
}

func TestQuestionWritesWithoutYesRejected(t *testing.T) {
	store := t.TempDir()

	if _, err := execCmd(t, store, "board", "add-question", "t", "--text", "q"); err == nil {
		t.Fatal("add-question without --yes = nil, want rejection")
	}
	if _, err := execCmd(t, store, "board", "resolve-question", "t",
		"--id", "1", "--resolution", "x"); err == nil {
		t.Fatal("resolve-question without --yes = nil, want rejection")
	}
}
