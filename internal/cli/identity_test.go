package cli

import (
	"testing"
	"time"

	"github.com/unsafe9/clutch/internal/model"
)

// baseTaskWithSession builds a task carrying one session at a fixed activity, so
// tests can vary only the wall-clock-derived session fields.
func baseTaskWithSession(last time.Time, running bool) model.Task {
	return model.Task{
		ID:        "T1",
		Lifecycle: model.LifecycleActive,
		Branches:  []model.Branch{{Ref: "branch:acme/app/main", Repo: "acme/app", Name: "main", Head: "aaa"}},
		Sessions: []model.Session{{
			Ref: "session:claude-code/s1", ID: "s1", Host: "claude-code",
			Cwd: "/repos/app", Branch: "main", LastActivity: last, Running: running,
		}},
	}
}

// The stored `updated` advances only on real state change. A session's Running
// flips at the 5-minute recency boundary and its LastActivity advances as it
// writes, both purely from the clock — those must NOT change the fingerprint, or
// `updated` churns across scans of identical on-disk state.
func TestFingerprintIgnoresWallClockSessionState(t *testing.T) {
	base := time.Date(2026, 7, 6, 12, 0, 0, 0, time.UTC)

	within := baseTaskWithSession(base, true)                   // last+within window
	past := baseTaskWithSession(base.Add(7*time.Minute), false) // later activity, dropped out of window

	if fingerprint(within) != fingerprint(past) {
		t.Fatalf("fingerprint changed on wall-clock-only session drift:\n within=%s\n past  =%s",
			fingerprint(within), fingerprint(past))
	}

	// The copy-on-zero must not mutate the caller's task.
	if !within.Sessions[0].LastActivity.Equal(base) || !within.Sessions[0].Running {
		t.Fatalf("fingerprint mutated the input session: %+v", within.Sessions[0])
	}
}

// A genuinely new session (distinct id) is real observed-state change, so it must
// move the fingerprint and advance `updated`.
func TestFingerprintTracksSessionIdentity(t *testing.T) {
	base := time.Date(2026, 7, 6, 12, 0, 0, 0, time.UTC)
	one := baseTaskWithSession(base, true)

	two := baseTaskWithSession(base, true)
	two.Sessions = append(two.Sessions, model.Session{
		Ref: "session:claude-code/s2", ID: "s2", Host: "claude-code", Cwd: "/repos/app",
	})

	if fingerprint(one) == fingerprint(two) {
		t.Fatal("fingerprint did not track a newly-appeared session")
	}
}
