package session

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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
		[]string{dir},
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

	obs, err := observe(cc, codex, archived, []string{"/work"}, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(obs) != 3 {
		t.Fatalf("want 3 sessions, got %d: %+v", len(obs), obs)
	}

	alpha := find(t, obs, "claude-code", "/work/alpha")
	if alpha.ID != "sess-1" {
		t.Errorf("cc id = %q, want sess-1 (transcript filename stem)", alpha.ID)
	}
	if alpha.Ref != "session:claude-code/sess-1" {
		t.Errorf("cc ref = %q", alpha.Ref)
	}
	if !alpha.LastActivity.Equal(now.Add(-1 * time.Minute)) {
		t.Errorf("cc LastActivity = %v, want max timestamp", alpha.LastActivity)
	}
	if !alpha.Running {
		t.Errorf("cc within threshold must be running")
	}

	beta := find(t, obs, "codex", "/work/beta")
	if beta.ID != "abc" {
		t.Errorf("codex id = %q, want abc (session_meta.payload.id)", beta.ID)
	}
	if beta.Ref != "session:codex/abc" {
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

// Two CC transcripts in the SAME cwd are two distinct sessions: each keeps its
// own id (filename stem) and a ref keyed by that id, so a later cwd-keyed dedup
// cannot collapse them and hide the running one behind a stale one.
func TestConcurrentSessionsSameCwdDistinctRefs(t *testing.T) {
	dir := t.TempDir()
	cc := filepath.Join(dir, "cc")
	now := time.Date(2026, 6, 16, 12, 0, 0, 0, time.UTC)
	recent := now.Add(-1 * time.Minute).Format(time.RFC3339Nano)
	stale := now.Add(-30 * time.Minute).Format(time.RFC3339Nano)

	// Same cwd, two transcripts: one stale, one running.
	writeFile(t, filepath.Join(cc, "-work-shared", "old.jsonl"),
		`{"type":"user","cwd":"/work/shared","timestamp":"`+stale+`"}`+"\n")
	writeFile(t, filepath.Join(cc, "-work-shared", "live.jsonl"),
		`{"type":"user","cwd":"/work/shared","timestamp":"`+recent+`"}`+"\n")

	obs, err := observe(cc, filepath.Join(dir, "n1"), filepath.Join(dir, "n2"), []string{"/work"}, now)
	if err != nil {
		t.Fatal(err)
	}
	byRef := map[model.RepRef]model.Session{}
	for _, o := range obs {
		if o.Session.Cwd == "/work/shared" {
			byRef[o.Session.Ref] = o.Session
		}
	}
	if len(byRef) != 2 {
		t.Fatalf("want 2 distinct sessions for the shared cwd, got %d: %+v", len(byRef), obs)
	}
	old, oldOK := byRef["session:claude-code/old"]
	live, liveOK := byRef["session:claude-code/live"]
	if !oldOK || !liveOK {
		t.Fatalf("distinct refs missing, got %+v", byRef)
	}
	if old.Running {
		t.Errorf("stale session must not be running")
	}
	if !live.Running {
		t.Errorf("recent session must be running (must not be masked by the stale one)")
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

	obs, err := observe(cc, filepath.Join(dir, "none"), filepath.Join(dir, "none2"), []string{"/work"}, now)
	if err != nil {
		t.Fatal(err)
	}
	s := find(t, obs, "claude-code", "/work/stale")
	if s.Running {
		t.Errorf("activity older than threshold must be not running")
	}
}

// TestRunningRuleEdges nails the boundary semantics of the recency rule:
// activity exactly at the threshold is running (the rule is `<=`), and a session
// whose records carry no parseable timestamp (zero LastActivity) is never
// running rather than spuriously alive.
func TestRunningRuleEdges(t *testing.T) {
	dir := t.TempDir()
	cc := filepath.Join(dir, "cc")
	now := time.Date(2026, 6, 16, 12, 0, 0, 0, time.UTC)

	// Exactly at the threshold edge -> running (boundary is inclusive). The
	// bucket name is the sanitized cwd so it survives the in-scope pre-filter.
	edgeTS := now.Add(-RunningThreshold).Format(time.RFC3339Nano)
	writeFile(t, filepath.Join(cc, "-work-edge", "s.jsonl"),
		`{"type":"user","cwd":"/work/edge","timestamp":"`+edgeTS+`"}`+"\n")

	// A record with a cwd but no timestamp -> zero LastActivity -> not running.
	writeFile(t, filepath.Join(cc, "-work-nots", "s.jsonl"),
		`{"type":"user","cwd":"/work/nots"}`+"\n")

	obs, err := observe(cc, filepath.Join(dir, "n1"), filepath.Join(dir, "n2"), []string{"/work"}, now)
	if err != nil {
		t.Fatal(err)
	}
	if edge := find(t, obs, "claude-code", "/work/edge"); !edge.Running {
		t.Errorf("activity exactly at threshold must be running")
	}
	nots := find(t, obs, "claude-code", "/work/nots")
	if !nots.LastActivity.IsZero() {
		t.Errorf("no-timestamp session LastActivity = %v, want zero", nots.LastActivity)
	}
	if nots.Running {
		t.Errorf("session with no activity timestamp must not be running")
	}
}

// TestArchivedNeverRunningRecent guards the archived exclusion specifically when
// recency WOULD say running: an archived Codex session with a fresh timestamp is
// still not running.
func TestArchivedNeverRunningRecent(t *testing.T) {
	dir := t.TempDir()
	archived := filepath.Join(dir, "archived")
	now := time.Date(2026, 6, 16, 12, 0, 0, 0, time.UTC)
	fresh := now.Format(time.RFC3339Nano)
	writeFile(t, filepath.Join(archived, "rollout-2026-06-16T11-59-59-z.jsonl"),
		`{"timestamp":"`+fresh+`","type":"session_meta","payload":{"id":"z","cwd":"/work/arch"}}`+"\n"+
			`{"timestamp":"`+fresh+`","type":"response_item","payload":{}}`+"\n")

	obs, err := observe(filepath.Join(dir, "n1"), filepath.Join(dir, "n2"), archived, []string{"/work"}, now)
	if err != nil {
		t.Fatal(err)
	}
	s := find(t, obs, "codex", "/work/arch")
	if s.Running {
		t.Errorf("archived session must never be running even with fresh activity")
	}
}

func TestMalformedFilesSkipped(t *testing.T) {
	dir := t.TempDir()
	cc := filepath.Join(dir, "cc")
	codex := filepath.Join(dir, "codex")
	now := time.Now()

	// Garbage lines: not JSON. CC needs a cwd, so this file is skipped entirely.
	// Buckets are named to survive the in-scope pre-filter so the parse-skip path
	// (not the scope filter) is what drops these files.
	writeFile(t, filepath.Join(cc, "-work-bad", "broken.jsonl"), "not json\n{also bad\n")
	// CC valid file with no cwd anywhere -> skipped.
	writeFile(t, filepath.Join(cc, "-work-nocwd", "n.jsonl"), `{"type":"mode"}`+"\n")
	// Codex file whose first line is not session_meta -> no cwd -> skipped.
	writeFile(t, filepath.Join(codex, "2026", "06", "16", "rollout-x.jsonl"),
		`{"timestamp":"`+now.Format(time.RFC3339Nano)+`","type":"event_msg","payload":{}}`+"\n")

	obs, err := observe(cc, codex, filepath.Join(dir, "none"), []string{"/work"}, now)
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

	// Use the symlink-resolved repo path so the recorded cwd, the sanitized CC
	// bucket, and the (symlink-resolved) search root all agree.
	if real, err := filepath.EvalSymlinks(repo); err == nil {
		repo = real
	}

	dir := t.TempDir()
	cc := filepath.Join(dir, "cc")
	now := time.Now()
	transcript := `{"type":"user","cwd":"` + repo + `","timestamp":"` + now.Format(time.RFC3339Nano) + `"}` + "\n"
	writeFile(t, filepath.Join(cc, sanitizeCCDir(repo), "s.jsonl"), transcript)

	obs, err := observe(cc, filepath.Join(dir, "none"), filepath.Join(dir, "none2"), []string{repo}, now)
	if err != nil {
		t.Fatal(err)
	}
	s := find(t, obs, "claude-code", repo)
	if s.Branch != "feature/x" {
		t.Errorf("Branch = %q, want feature/x", s.Branch)
	}
}

// TestObserveScopedToRoots verifies that only sessions whose cwd lies within a
// configured search root are returned — both hosts — and out-of-scope sessions
// (the common case: global CC/Codex sessions unrelated to the scanned roots) are
// dropped rather than surfaced.
func TestObserveScopedToRoots(t *testing.T) {
	dir := t.TempDir()
	cc := filepath.Join(dir, "cc")
	codex := filepath.Join(dir, "codex")
	now := time.Now()
	ts := now.Format(time.RFC3339Nano)

	// In-scope CC session under /root.
	writeFile(t, filepath.Join(cc, "-root-in", "s.jsonl"),
		`{"type":"user","cwd":"/root/in","timestamp":"`+ts+`"}`+"\n")
	// Out-of-scope CC session: bucket and cwd both outside /root.
	writeFile(t, filepath.Join(cc, "-other-out", "s.jsonl"),
		`{"type":"user","cwd":"/other/out","timestamp":"`+ts+`"}`+"\n")
	// In-scope Codex session under /root.
	writeFile(t, filepath.Join(codex, "2026", "06", "16", "rollout-a.jsonl"),
		`{"timestamp":"`+ts+`","type":"session_meta","payload":{"id":"a","cwd":"/root/beta"}}`+"\n")
	// Out-of-scope Codex session -> dropped at the first line.
	writeFile(t, filepath.Join(codex, "2026", "06", "16", "rollout-b.jsonl"),
		`{"timestamp":"`+ts+`","type":"session_meta","payload":{"id":"b","cwd":"/elsewhere"}}`+"\n")

	obs, err := observe(cc, codex, filepath.Join(dir, "none"), []string{"/root"}, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(obs) != 2 {
		t.Fatalf("want 2 in-scope sessions, got %d: %+v", len(obs), obs)
	}
	for _, o := range obs {
		if !strings.HasPrefix(o.Session.Cwd, "/root") {
			t.Errorf("out-of-scope session leaked: %q", o.Session.Cwd)
		}
	}
}

// TestObserveBucketPrefilterSettledByCwd proves the two-stage CC scope check: a
// sibling directory (/root/apple) sanitizes to a bucket prefixed by the root's
// sanitized form (/root/app -> "-root-app"), so it survives the cheap pre-filter
// but is dropped by the definitive path check, since it is not nested under the
// root.
func TestObserveBucketPrefilterSettledByCwd(t *testing.T) {
	dir := t.TempDir()
	cc := filepath.Join(dir, "cc")
	now := time.Now()
	ts := now.Format(time.RFC3339Nano)

	writeFile(t, filepath.Join(cc, "-root-apple", "s.jsonl"),
		`{"type":"user","cwd":"/root/apple","timestamp":"`+ts+`"}`+"\n")

	obs, err := observe(cc, filepath.Join(dir, "n1"), filepath.Join(dir, "n2"), []string{"/root/app"}, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(obs) != 0 {
		t.Fatalf("sibling path surviving the pre-filter must be settled out, got %+v", obs)
	}
}
