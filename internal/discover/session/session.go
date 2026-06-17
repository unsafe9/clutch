// Package session produces Claude Code (CC) and Codex session observations.
// Concrete functions only — no common Discoverer interface.
//
// On-disk locations and parsing rules follow docs/session-format.md.
package session

import (
	"bufio"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/unsafe9/clutch/internal/model"
)

// RunningThreshold is the recency window: a session whose last activity is
// within this window of "now" is treated as running. No host writes a per-
// session lock/pid file, so running is a deterministic recency derivation
// (docs/session-format.md, "Running flag").
const RunningThreshold = 5 * time.Minute

const (
	hostClaudeCode = "claude-code"
	hostCodex      = "codex"
)

// Observe discovers active/recent CC and Codex sessions in the real user home.
func Observe() ([]model.SessionObservation, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	ccProjects := filepath.Join(home, ".claude", "projects")
	codexSessions := filepath.Join(home, ".codex", "sessions")
	codexArchived := filepath.Join(home, ".codex", "archived_sessions")
	return observe(ccProjects, codexSessions, codexArchived, time.Now())
}

// observe is the testable core: it reads sessions from explicit base directories
// rather than the real home, so tests can drive it with fixtures in t.TempDir().
// Missing directories yield no observations and no error; malformed or partial
// files are skipped.
func observe(ccProjectsDir, codexSessionsDir, codexArchivedDir string, now time.Time) ([]model.SessionObservation, error) {
	var out []model.SessionObservation

	for _, p := range globIgnoreMissing(filepath.Join(ccProjectsDir, "*", "*.jsonl")) {
		if s, ok := parseClaudeCode(p, now); ok {
			out = append(out, model.SessionObservation{Session: s})
		}
	}
	for _, p := range globIgnoreMissing(filepath.Join(codexSessionsDir, "*", "*", "*", "rollout-*.jsonl")) {
		if s, ok := parseCodex(p, now, false); ok {
			out = append(out, model.SessionObservation{Session: s})
		}
	}
	for _, p := range globIgnoreMissing(filepath.Join(codexArchivedDir, "rollout-*.jsonl")) {
		if s, ok := parseCodex(p, now, true); ok {
			out = append(out, model.SessionObservation{Session: s})
		}
	}
	return out, nil
}

func globIgnoreMissing(pattern string) []string {
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return nil
	}
	return matches
}

// parseClaudeCode scans a CC JSONL transcript. The sanitized directory name is
// lossy, so cwd/branch/timestamp are recovered from record fields: cwd/branch
// are constant per session (take any record bearing them), LastActivity is the
// max timestamp. Returns ok=false when no usable record carries a cwd.
func parseClaudeCode(path string, now time.Time) (model.Session, bool) {
	f, err := os.Open(path)
	if err != nil {
		return model.Session{}, false
	}
	defer f.Close()

	type ccRecord struct {
		Cwd       string `json:"cwd"`
		GitBranch string `json:"gitBranch"`
		Timestamp string `json:"timestamp"`
	}

	var cwd string
	var last time.Time
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var r ccRecord
		if json.Unmarshal(line, &r) != nil {
			continue
		}
		if r.Cwd != "" {
			cwd = r.Cwd
		}
		if r.Timestamp != "" {
			if ts, err := time.Parse(time.RFC3339Nano, r.Timestamp); err == nil && ts.After(last) {
				last = ts
			}
		}
	}
	if cwd == "" {
		return model.Session{}, false
	}
	return buildSession(hostClaudeCode, cwd, last, now, false), true
}

// parseCodex reads a Codex rollout file: session_meta (first line) is the
// canonical cwd/branch source, and the last record's top-level timestamp is the
// last activity. archived marks the session as never-running regardless of mtime.
func parseCodex(path string, now time.Time, archived bool) (model.Session, bool) {
	f, err := os.Open(path)
	if err != nil {
		return model.Session{}, false
	}
	defer f.Close()

	type codexRecord struct {
		Timestamp string `json:"timestamp"`
		Type      string `json:"type"`
		Payload   struct {
			Cwd string `json:"cwd"`
		} `json:"payload"`
	}

	var cwd string
	var last time.Time
	first := true
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var r codexRecord
		if json.Unmarshal(line, &r) != nil {
			continue
		}
		if first {
			first = false
			if r.Type == "session_meta" {
				cwd = r.Payload.Cwd
			}
		}
		if r.Timestamp != "" {
			if ts, err := time.Parse(time.RFC3339Nano, r.Timestamp); err == nil && ts.After(last) {
				last = ts
			}
		}
	}
	if cwd == "" {
		return model.Session{}, false
	}
	return buildSession(hostCodex, cwd, last, now, archived), true
}

func buildSession(host, cwd string, last, now time.Time, forceNotRunning bool) model.Session {
	running := false
	if !forceNotRunning && !last.IsZero() {
		running = now.Sub(last) <= RunningThreshold
	}
	return model.Session{
		Ref:          model.RepRef("session:" + host + "/" + cwd),
		Host:         host,
		Cwd:          cwd,
		Branch:       gitBranch(cwd),
		LastActivity: last,
		Running:      running,
	}
}

// gitBranch best-effort resolves the current branch of cwd via git shell-out;
// empty on any failure (not a repo, detached HEAD, git absent, missing dir).
func gitBranch(cwd string) string {
	cmd := exec.Command("git", "-C", cwd, "rev-parse", "--abbrev-ref", "HEAD")
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	branch := strings.TrimSpace(string(out))
	if branch == "HEAD" {
		return ""
	}
	return branch
}
