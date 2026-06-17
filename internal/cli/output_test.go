package cli

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"

	"github.com/unsafe9/clutch/internal/model"
)

func sampleEnvelope() model.ProjectionEnvelope {
	return model.ProjectionEnvelope{
		SchemaVersion: model.SchemaVersion,
		GeneratedAt:   time.Date(2026, 6, 16, 0, 0, 0, 0, time.UTC),
		Tasks: []model.Task{
			{
				ID:        "t1",
				Title:     "first",
				Lifecycle: model.Lifecycle("active"),
				Mode:      model.Mode("cruise"),
				Branches:  []model.Branch{{Name: "feature/x"}},
				PRs:       []model.PullRequest{{Number: 7}},
			},
		},
		Diagnostics: model.Diagnostics{
			ScanStats: model.ScanStats{ReposScanned: 1, TasksProjected: 1},
		},
	}
}

func TestEmitJSONDeterministic(t *testing.T) {
	env := sampleEnvelope()
	var a, b bytes.Buffer
	if err := emitJSON(&a, env); err != nil {
		t.Fatal(err)
	}
	if err := emitJSON(&b, env); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(a.Bytes(), b.Bytes()) {
		t.Fatalf("emitJSON not deterministic:\n%s\n---\n%s", a.String(), b.String())
	}
	if !bytes.HasSuffix(a.Bytes(), []byte("\n")) {
		t.Fatal("emitJSON output missing trailing newline")
	}
	if !bytes.Contains(a.Bytes(), []byte("  \"schema_version\"")) {
		t.Fatalf("emitJSON not two-space indented:\n%s", a.String())
	}
}

func TestEmitJSONRoundTrip(t *testing.T) {
	env := sampleEnvelope()
	var buf bytes.Buffer
	if err := emitJSON(&buf, env); err != nil {
		t.Fatal(err)
	}
	var got model.ProjectionEnvelope
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("round-trip unmarshal: %v", err)
	}
	if got.SchemaVersion != env.SchemaVersion {
		t.Fatalf("SchemaVersion = %q, want %q", got.SchemaVersion, env.SchemaVersion)
	}
	if len(got.Tasks) != 1 || got.Tasks[0].ID != "t1" {
		t.Fatalf("Tasks = %v", got.Tasks)
	}
}
