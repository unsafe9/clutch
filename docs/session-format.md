# Agent session on-disk formats (claude-code, codex)

> Reverse-engineered from real session files on the build machine
> (CC `2.1.178`, Codex `0.139.0`), 2026-06. This is the implementation spec for
> the session discoverer that produces `model.SessionObservation` →
> `model.Session`. Where a fact could not be confirmed from local files it is
> marked **PROVISIONAL**.

## model.Session mapping (shared)

Each discovered session projects to one `model.Session` (`internal/model/task.go`):

| model.Session field | type        | source                                                            |
|---------------------|-------------|-------------------------------------------------------------------|
| `Ref`               | RepRef      | `session:<host>/<cwd>` — built from `Host` + recovered `Cwd`      |
| `Host`              | string      | `claude-code` or `codex` (constant per discoverer)                |
| `Cwd`               | string      | recovered original working dir (see per-host rules)               |
| `Branch`            | string      | git branch recorded in the transcript (`omitempty`)               |
| `LastActivity`      | time.Time   | timestamp of the last activity record (see per-host rules)        |
| `Running`           | bool        | deterministic recency rule (see "Running flag" below)             |

The `Ref` scheme `session:<host>/<cwd>` is fixed by the contract
(`docs/contract.md`, RepRef table). `<cwd>` is the **recovered absolute path**,
not the sanitized directory name.

---

## Host: claude-code

### Location

```
~/.claude/projects/<sanitized-cwd>/<session-uuid>.jsonl
```

Glob: `~/.claude/projects/*/*.jsonl`

- `<session-uuid>` is the CC session id (also echoed as `sessionId` inside).
- A sibling directory `<session-uuid>/` may exist next to the `.jsonl`
  (auxiliary attachment/snapshot storage); ignore it for session discovery —
  only the top-level `*.jsonl` files are transcripts.

### `<sanitized-cwd>` is LOSSY — do not parse it for cwd

The directory name is the absolute cwd with every `/`, `_`, and `.` replaced by
`-`. Examples observed:

| original cwd                         | sanitized dir                                     |
|--------------------------------------|---------------------------------------------------|
| `/Users/wshan/_NOAV/clutch`          | `-Users-wshan--NOAV-clutch`                        |
| `/Users/wshan/.codex-worktrees/20e2/my-terminal` | `-Users-wshan--codex-worktrees-20e2-my-terminal` |

Because `/`, `_`, and `.` all collapse to `-`, the mapping is **not reversible**.
Use the sanitized dir only as a coarse glob bucket; recover the real cwd from a
field inside the JSONL.

### Record format

JSON Lines: one JSON object per line, appended in chronological order. `type`
discriminates the record. Types seen: `agent-setting`, `mode`,
`permission-mode`, `last-prompt`, `ai-title`, `system`, `user`, `assistant`,
`attachment`, `file-history-snapshot`, `queue-operation`. The **first** line is
not guaranteed to carry cwd (it may be `agent-setting` or `mode`).

A `user` / `assistant` record carries the fields we need:

```json
{
  "type": "user",
  "cwd": "/Users/wshan/_NOAV/clutch",
  "gitBranch": "main",
  "timestamp": "2026-06-16T10:21:31.942Z",
  "version": "2.1.178",
  "sessionId": "74cc1ab1-...",
  "uuid": "...",
  "userType": "external"
}
```

Not every line has `cwd`/`gitBranch`/`timestamp` (e.g. `mode`,
`file-history-snapshot` lines do not). In one ~860-line transcript, 626 lines
carried `cwd` and 238 did not.

### Field mapping

| target          | from                                                                    |
|-----------------|-------------------------------------------------------------------------|
| `Cwd`           | `.cwd` of any record that has it (stable within a session)              |
| `Branch`        | `.gitBranch` of any record that has it (may be empty if not in a repo)  |
| `LastActivity`  | the max `.timestamp` (RFC3339, `Z`) across records — in practice the last record bearing a `timestamp` |
| `Host`          | constant `claude-code`                                                  |

Recovery rule for `Cwd`/`Branch`: scan lines (a single pass) and take the value
from the **last** record that carries the field; equivalently the first, since
they are constant per session. `LastActivity` is the maximum `timestamp`.
Reading only the tail is sufficient and cheap for large transcripts (a tail line
typically carries both `cwd` and `timestamp`); fall back to a forward scan if the
tail lines lack the field.

---

## Host: codex

### Location

```
~/.codex/sessions/<YYYY>/<MM>/<DD>/rollout-<ISO8601>-<session-uuid>.jsonl
```

Glob: `~/.codex/sessions/*/*/*/rollout-*.jsonl`

- Filename: `rollout-2026-06-15T03-22-43-019ec75f-299b-74e3-985c-bc5ecf8c7f0e.jsonl`
  (timestamp uses `-` instead of `:`; trailing UUID is the session id).
- Archived sessions live flat under `~/.codex/archived_sessions/rollout-*.jsonl`
  — these are inactive by definition; treat as not-running regardless of mtime.
- `~/.codex/history.jsonl` is a flat global prompt history (`{session_id, ts,
  text}`), and `~/.codex/session_index.jsonl` is a name index (`{id,
  thread_name, updated_at}`). Neither carries cwd/branch; do **not** use them as
  the primary source. They are optional cross-references only.

### Record format

JSON Lines. Each line: `{ "timestamp": "<RFC3339 Z>", "type": "<t>", "payload":
{...} }`. Types seen: `session_meta`, `turn_context`, `event_msg`,
`response_item`. The **first** line is always `session_meta`:

```json
{
  "timestamp": "2026-06-14T18:22:44.355Z",
  "type": "session_meta",
  "payload": {
    "id": "019ec75f-299b-74e3-985c-bc5ecf8c7f0e",
    "timestamp": "2026-06-14T18:22:43.135Z",
    "cwd": "/Users/wshan/_NOAV/my-terminal",
    "originator": "codex_exec",
    "cli_version": "0.139.0",
    "git": {
      "commit_hash": "baffdfcd...",
      "branch": "main",
      "repository_url": "git@github.com:unsafe9/my-terminal.git"
    }
  }
}
```

`turn_context` records also carry `cwd` (and `workspace_roots`), so cwd is
cross-confirmable, but `session_meta` is the canonical source.

### Field mapping

| target          | from                                                                |
|-----------------|---------------------------------------------------------------------|
| `Cwd`           | `session_meta.payload.cwd` (first line) — **directly recoverable, not lossy** |
| `Branch`        | `session_meta.payload.git.branch` (absent when not in a git repo)   |
| `LastActivity`  | top-level `.timestamp` of the **last** record in the file           |
| `Host`          | constant `codex`                                                     |

Recovery rule: read the **first** line for `cwd`/`git.branch` and the **last**
line for `LastActivity`. No full-file scan needed.

---

## Running flag — deterministic recency rule

Neither host writes a per-session lock or pid file. Confirmed absent:

- CC: `~/.claude/ide/` is empty; no `*.lock`/`*.pid` keyed to a session uuid
  (the only `.lock` files are MCP-refresh locks, unrelated to sessions).
- Codex: only `~/.codex/.tmp/plugins.sync.lock` exists (global, unrelated);
  no per-session lock.

Therefore `Running` is derived deterministically from recency:

```
Running := now - LastActivity <= RunningThreshold
```

with **`RunningThreshold = 5 minutes`**. Rationale: an active agent appends
records (CC user/assistant turns, Codex event/response items) continuously while
working; a session idle for over 5 minutes is treated as not running. This is a
heuristic, but it is deterministic given the clock and the file contents. The
threshold is a single named constant so it can be tuned in one place.

Additional deterministic exclusions:
- Codex `archived_sessions/*` → always `Running = false`.

> **PROVISIONAL:** the 5-minute threshold is a chosen default, not read from any
> host config; if either CLI later exposes an authoritative liveness signal
> (lock/pid/socket), the discoverer should prefer it over the recency rule.

---

## Notes / provisional items

- All timestamps observed are RFC3339 with a `Z` (UTC) suffix; parse with
  `time.RFC3339Nano`.
- CC `version` (`2.1.178`) and Codex `cli_version`/`session_meta.payload` field
  set are version-dependent; the fields used here (`cwd`, `gitBranch`/`git`,
  `timestamp`) have been stable across the on-disk samples on this machine but
  could be renamed by a future host release — the discoverer should tolerate a
  missing field rather than panic.
- A discoverer may further restrict by configured roots: keep only sessions
  whose recovered `Cwd` is within a scan root.
