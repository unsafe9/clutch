package session

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/unsafe9/clutch/internal/model"
)

// writeFile writes content to path, creating parent dirs.
func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func find(t *testing.T, obs []model.SessionObservation, host, cwd string) model.Session {
	t.Helper()
	for _, o := range obs {
		if o.Session.Host == host && o.Session.Cwd == cwd {
			return o.Session
		}
	}
	t.Fatalf("no session for host=%s cwd=%s in %+v", host, cwd, obs)
	return model.Session{}
}

func TestObserveMissingDirs(t *testing.T) {
	dir := t.TempDir()
	obs, err := observe(
		filepath.Join(dir, "nope-cc"),
		filepath.Join(dir, "nope-codex"),
		filepath.Join(dir, "nope-archived"),
		time.Now(),
	)
	if err != nil {
		t.Fatalf("missing dirs must not error: %v", err)
	}
	if len(obs) != 0 {
		t.Fatalf("missing dirs must yield no sessions, got %d", len(obs))
	}
}

func TestObserveParsesBothHosts(t *testing.T) {
	dir := t.TempDir()
	cc := filepath.Join(dir, "cc")
	codex := filepath.Join(dir, "codex")
	archived := filepath.Join(dir, "archived")

	now := time.Date(2026, 6, 16, 12, 0, 0, 0, time.UTC)
	recent := now.Add(-1 * time.Minute).Format(time.RFC3339Nano)
	stale := now.Add(-30 * time.Minute).Format(time.RFC3339Nano)

	// CC: sanitized dir bucket + jsonl transcript. First lines lack cwd/ts;
	// cwd/branch are constant; LastActivity is the max timestamp.
	ccTranscript := `{"type":"mode","mode":"default"}
{"type":"user","cwd":"/work/alpha","gitBranch":"main","timestamp":"` + stale + `"}
{"type":"file-history-snapshot"}
{"type":"assistant","cwd":"/work/alpha","gitBranch":"main","timestamp":"` + recent + `"}
`
	writeFile(t, filepath.Join(cc, "-work-alpha", "sess-1.jsonl"), ccTranscript)

	// Codex: nested YYYY/MM/DD dir; first line session_meta carries cwd; last
	// line top-level timestamp is LastActivity.
	codexRollout := `{"timestamp":"` + stale + `","type":"session_meta","payload":{"id":"abc","cwd":"/work/beta","git":{"branch":"dev"}}}
{"timestamp":"` + recent + `","type":"response_item","payload":{}}
`
	writeFile(t, filepath.Join(codex, "2026", "06", "16", "rollout-2026-06-16T11-00-00-abc.jsonl"), codexRollout)

	// Codex archived: always not-running regardless of recency.
	archivedRollout := `{"timestamp":"` + recent + `","type":"session_meta","payload":{"id":"old","cwd":"/work/gamma"}}
{"timestamp":"` + recent + `","type":"response_item","payload":{}}
`
	writeFile(t, filepath.Join(archived, "rollout-2026-06-15T01-00-00-old.jsonl"), archivedRollout)

	obs, err := observe(cc, codex, archived, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(obs) != 3 {
		t.Fatalf("want 3 sessions, got %d: %+v", len(obs), obs)
	}

	alpha := find(t, obs, "claude-code", "/work/alpha")
	if alpha.Ref != "session:claude-code//work/alpha" {
		t.Errorf("cc ref = %q", alpha.Ref)
	}
	if !alpha.LastActivity.Equal(now.Add(-1 * time.Minute)) {
		t.Errorf("cc LastActivity = %v, want max timestamp", alpha.LastActivity)
	}
	if !alpha.Running {
		t.Errorf("cc within threshold must be running")
	}

	beta := find(t, obs, "codex", "/work/beta")
	if beta.Ref != "session:codex//work/beta" {
		t.Errorf("codex ref = %q", beta.Ref)
	}
	if !beta.LastActivity.Equal(now.Add(-1 * time.Minute)) {
		t.Errorf("codex LastActivity = %v, want last record timestamp", beta.LastActivity)
	}
	if !beta.Running {
		t.Errorf("codex within threshold must be running")
	}

	gamma := find(t, obs, "codex", "/work/gamma")
	if gamma.Running {
		t.Errorf("archived codex session must never be running")
	}
}

func TestRunningThreshold(t *testing.T) {
	dir := t.TempDir()
	cc := filepath.Join(dir, "cc")
	now := time.Date(2026, 6, 16, 12, 0, 0, 0, time.UTC)
	staleTS := now.Add(-RunningThreshold - time.Second).Format(time.RFC3339Nano)

	transcript := `{"type":"user","cwd":"/work/stale","gitBranch":"x","timestamp":"` + staleTS + `"}
`
	writeFile(t, filepath.Join(cc, "-work-stale", "s.jsonl"), transcript)

	obs, err := observe(cc, filepath.Join(dir, "none"), filepath.Join(dir, "none2"), now)
	if err != nil {
		t.Fatal(err)
	}
	s := find(t, obs, "claude-code", "/work/stale")
	if s.Running {
		t.Errorf("activity older than threshold must be not running")
	}
}

func TestMalformedFilesSkipped(t *testing.T) {
	dir := t.TempDir()
	cc := filepath.Join(dir, "cc")
	codex := filepath.Join(dir, "codex")
	now := time.Now()

	// Garbage lines: not JSON. CC needs a cwd, so this file is skipped entirely.
	writeFile(t, filepath.Join(cc, "-bad", "broken.jsonl"), "not json\n{also bad\n")
	// CC valid file with no cwd anywhere -> skipped.
	writeFile(t, filepath.Join(cc, "-nocwd", "n.jsonl"), `{"type":"mode"}`+"\n")
	// Codex file whose first line is not session_meta -> no cwd -> skipped.
	writeFile(t, filepath.Join(codex, "2026", "06", "16", "rollout-x.jsonl"),
		`{"timestamp":"`+now.Format(time.RFC3339Nano)+`","type":"event_msg","payload":{}}`+"\n")

	obs, err := observe(cc, codex, filepath.Join(dir, "none"), now)
	if err != nil {
		t.Fatal(err)
	}
	if len(obs) != 0 {
		t.Fatalf("malformed/partial files must be skipped, got %+v", obs)
	}
}

// TestBranchFromGit exercises the git shell-out: a CC session whose recovered
// cwd is a real git repo should report that repo's branch.
func TestBranchFromGit(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	repo := t.TempDir()
	run := func(args ...string) {
		cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-q", "-b", "feature/x")
	writeFile(t, filepath.Join(repo, "f.txt"), "hi")
	run("add", ".")
	run("commit", "-q", "-m", "init")

	dir := t.TempDir()
	cc := filepath.Join(dir, "cc")
	now := time.Now()
	transcript := `{"type":"user","cwd":"` + repo + `","timestamp":"` + now.Format(time.RFC3339Nano) + `"}` + "\n"
	writeFile(t, filepath.Join(cc, "-repo", "s.jsonl"), transcript)

	obs, err := observe(cc, filepath.Join(dir, "none"), filepath.Join(dir, "none2"), now)
	if err != nil {
		t.Fatal(err)
	}
	s := find(t, obs, "claude-code", repo)
	if s.Branch != "feature/x" {
		t.Errorf("Branch = %q, want feature/x", s.Branch)
	}
}
