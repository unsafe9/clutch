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

// Observe discovers active/recent CC and Codex sessions in the real user home,
// restricted to sessions whose cwd lies within one of the configured search
// roots. Out-of-scope sessions are neither read (where cheaply determinable) nor
// returned, so global sessions unrelated to the scanned roots never surface.
func Observe(roots []string) ([]model.SessionObservation, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	ccProjects := filepath.Join(home, ".claude", "projects")
	codexSessions := filepath.Join(home, ".codex", "sessions")
	codexArchived := filepath.Join(home, ".codex", "archived_sessions")
	return observe(ccProjects, codexSessions, codexArchived, roots, time.Now())
}

// observe is the testable core: it reads sessions from explicit base directories
// rather than the real home, so tests can drive it with fixtures in t.TempDir().
// Missing directories yield no observations and no error; malformed or partial
// files are skipped. Only sessions whose cwd is within one of roots are returned.
func observe(ccProjectsDir, codexSessionsDir, codexArchivedDir string, roots []string, now time.Time) ([]model.SessionObservation, error) {
	scope := newRootScope(roots)
	var out []model.SessionObservation

	// CC: the sanitized bucket name is a cheap out-of-scope pre-filter — a whole
	// bucket whose name cannot map to any search root is skipped without opening
	// a single transcript. Survivors are settled by the definitive cwd check.
	for _, bucket := range globIgnoreMissing(filepath.Join(ccProjectsDir, "*")) {
		if !scope.bucketMaybeInScope(filepath.Base(bucket)) {
			continue
		}
		for _, p := range globIgnoreMissing(filepath.Join(bucket, "*.jsonl")) {
			if s, ok := parseClaudeCode(p, now); ok && scope.contains(s.Cwd) {
				out = append(out, model.SessionObservation{Session: s})
			}
		}
	}
	for _, p := range globIgnoreMissing(filepath.Join(codexSessionsDir, "*", "*", "*", "rollout-*.jsonl")) {
		if s, ok := parseCodex(p, now, false, scope); ok {
			out = append(out, model.SessionObservation{Session: s})
		}
	}
	for _, p := range globIgnoreMissing(filepath.Join(codexArchivedDir, "rollout-*.jsonl")) {
		if s, ok := parseCodex(p, now, true, scope); ok {
			out = append(out, model.SessionObservation{Session: s})
		}
	}
	return out, nil
}

// rootScope decides whether a session cwd falls within any configured search
// root. Roots are cleaned and symlink-resolved so they agree with the canonical
// paths the git/fs producers report; the CC sanitized-directory pre-filter lets
// whole out-of-scope buckets be skipped without opening their transcripts.
type rootScope struct {
	roots     []string // cleaned, symlink-resolved absolute roots
	sanitized []string // roots run through the CC directory-name sanitizer
}

func newRootScope(roots []string) rootScope {
	var s rootScope
	for _, r := range roots {
		clean := filepath.Clean(r)
		if real, err := filepath.EvalSymlinks(clean); err == nil {
			clean = real
		}
		s.roots = append(s.roots, clean)
		s.sanitized = append(s.sanitized, sanitizeCCDir(clean))
	}
	return s
}

// contains reports whether cwd is one of, or nested under, a search root.
func (s rootScope) contains(cwd string) bool {
	cwd = filepath.Clean(cwd)
	for _, r := range s.roots {
		if cwd == r || strings.HasPrefix(cwd, r+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

// bucketMaybeInScope is the cheap CC pre-filter: it reports whether a sanitized
// project-directory name COULD map to a cwd within a search root. The sanitizer
// is lossy (several characters collapse to '-'), so this may yield false
// positives — those are settled by contains() once the real cwd is recovered —
// but never a false negative, so no in-scope transcript is ever skipped unread.
func (s rootScope) bucketMaybeInScope(dirName string) bool {
	for _, sr := range s.sanitized {
		if strings.HasPrefix(dirName, sr) {
			return true
		}
	}
	return false
}

// sanitizeCCDir applies Claude Code's lossy project-directory naming: every '/',
// '_', and '.' in the absolute cwd collapses to '-' (docs/session-format.md).
// sanitize(root+"/sub") == sanitize(root)+"-"+sanitize(sub), so a cwd within a
// root always sanitizes to a string prefixed by the root's sanitized form.
func sanitizeCCDir(path string) string {
	return strings.Map(func(r rune) rune {
		switch r {
		case '/', '_', '.':
			return '-'
		}
		return r
	}, path)
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
// cwd is recoverable from the first line, so an out-of-scope session is dropped
// there — the rest of the file is never read.
func parseCodex(path string, now time.Time, archived bool, scope rootScope) (model.Session, bool) {
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
			// cwd is known from the canonical first line; a session with no cwd or
			// one outside every search root is dropped without reading the rest.
			if cwd == "" || !scope.contains(cwd) {
				return model.Session{}, false
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
