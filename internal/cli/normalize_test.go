package cli

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/unsafe9/clutch/internal/model"
)

// A zero-value Task's every documented array must marshal as [] after
// normalization, never null.
func TestNormalizeTaskArraysRenderEmpty(t *testing.T) {
	var task model.Task
	normalizeTask(&task)
	data, err := json.Marshal(task)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`"repos":[]`, `"branches":[]`, `"worktrees":[]`,
		`"prs":[]`, `"issues":[]`, `"sessions":[]`,
		`"links":[]`, `"unresolved":[]`,
		`"parents":[]`, `"depends":[]`, `"blocks":[]`,
	} {
		if !strings.Contains(string(data), want) {
			t.Errorf("normalized task JSON missing %s:\n%s", want, data)
		}
	}
}

// A Board's adrs/appraisals and each ADR's alternatives must marshal as []
// after normalization, never null.
func TestNormalizeBoardArraysRenderEmpty(t *testing.T) {
	b := &model.Board{ADRs: []model.ADR{{Decision: "d"}}}
	normalizeBoard(b)
	data, err := json.Marshal(b)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"questions":[]`, `"appraisals":[]`, `"alternatives":[]`} {
		if !strings.Contains(string(data), want) {
			t.Errorf("normalized board JSON missing %s:\n%s", want, data)
		}
	}
}
