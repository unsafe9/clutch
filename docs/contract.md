# clutch contract (schema_version 0.1)

> Authoritative spec for the Wave-0 scaffold. The Go types under `internal/model`
> and this document agree field-for-field; if they ever diverge, that is a bug.
> Audience: an LLM or engineer wiring against clutch. Read intent first.

## Intent

clutch is an agent-neutral CLI harness for agent-driven engineering work. A
single `clutch` Go binary is the **authoritative gateway** to a Task+Board
store; external agents/systems consume it via the CLI (JSON), never by importing
Go. It is **deterministic-first**: the deterministic core reads git/fs/sessions
and emits a structured **Task projection**; an LLM "appraisal" layer fills only
the ambiguous remainder, and only its results are written back through the CLI.

---

## Task projection model — 3 provenance classes

A Task is one object whose fields fall into three classes by *where they come
from* and *whether they persist*. This grouping is the core design. (Go:
`internal/model/task.go`.)

### Class ① Identity & policy — PERSISTED, stable across scans

Lives in the store / id-registry. Set by clutch and the planner/agent layer.

| field        | type          | meaning                                                  |
|--------------|---------------|----------------------------------------------------------|
| `id`         | string        | clutch-assigned identity, independent of any representation, stable |
| `title`      | string        | label (from branch/PR/issue or planner)                  |
| `lifecycle`  | Lifecycle     | enum (see below)                                          |
| `mode`       | Mode          | enum; project default → task override                    |
| `provenance` | Provenance    | enum: clutch-initiated / git-detected                    |
| `board`      | *BoardRef     | pointer/locator to this task's board backend             |
| `created`    | RFC3339 time  | when the identity was minted                             |
| `updated`    | RFC3339 time  | when persisted identity/policy last changed              |

### Class ② Representations — DERIVED, recomputed each scan, NEVER persisted

Pure deterministic projection of git/fs/session state. **Carries zero LLM.**

| field         | type           | shape                                                   |
|---------------|----------------|---------------------------------------------------------|
| `repos`       | []RepoRef      | `{identity, path, remote}` — clones/checkouts spanned    |
| `branches`    | []Branch       | `{repo, name, head, upstream, ahead, behind}`           |
| `worktrees`   | []Worktree     | `{path, branch, repo}`                                   |
| `base`        | string         | fork-point ref                                          |
| `commits`     | CommitSummary  | `{head, count}` — summary, NOT a full commit list       |
| `prs`         | []PullRequest  | `{host, number, url, state, draft, checks}`             |
| `issues`      | []Issue        | `{tracker, key, url, state}` — external (jira/github)   |
| `integration` | Integration    | enum: unmerged / merged / conflicts / behind            |
| `sessions`    | []Session      | `{host, cwd, branch?, last_activity, running}` — host = claude-code\|codex |

> **PROVISIONAL (TODO wave1-b):** `Session` fields are not final; they will be
> fixed after the CC/Codex on-disk session formats are reverse-engineered.

### Class ③ Relations & correlation — MIXED: derived + declared + appraisal

| field        | type          | shape / provenance                                         |
|--------------|---------------|------------------------------------------------------------|
| `lineage`    | Lineage       | `{parents[]}` — parent task ids (derived from `base` where possible, else declared) |
| `relations`  | Relations     | `{depends[], blocks[]}` — task-id DAG; declared or appraised |
| `links`      | []Link        | per representation-link: `{method, confidence}`            |
| `unresolved` | []Unresolved  | `{kind, detail, task_id?}` — ambiguity flags fed to the `classify` orchestrator later |

`Link.method` is one of `convention | appraisal | declared` (LinkMethod enum).
Convention/declared imply confidence 1.0; appraisal < 1.0.

### Who writes what

- **Class ②** carries **zero LLM** — it is recomputed every scan and discarded.
- **Class ③'s appraisal parts** (appraised relations, `appraisal`-method links,
  unresolved resolutions) and **class ①'s planner-set parts** (e.g. `title`,
  `mode`) are written by the agent layer, and **those persist to the board** to
  avoid recomputation.

---

## Enums

String-typed; the JSON wire form is exactly the listed value. (Go:
`internal/model/enums.go`.) These values are part of the machine contract — keep
them stable.

| enum        | values                                                                 |
|-------------|------------------------------------------------------------------------|
| Lifecycle   | `idea` `planned` `active` `review` `merged` `done` `stale` `superseded` |
| Mode        | `cruise` `steer`                                                        |
| Provenance  | `clutch-initiated` `git-detected`                                       |
| Integration | `unmerged` `merged` `conflicts` `behind`                                |
| LinkMethod  | `convention` `appraisal` `declared`                                     |

---

## Board (state behind the BoardStore port)

Durable per-task knowledge at engineering altitude — **NO code**. (Go:
`internal/model/board.go`.)

| field        | type          | meaning                                                   |
|--------------|---------------|-----------------------------------------------------------|
| `principles` | string        | work principles for the task                              |
| `design`     | string        | evolving design that converges to final; decisions overwrite/accumulate; engineering altitude, NO code |
| `adrs`       | []ADR         | `{decision, context, alternatives[], consequence}`        |
| `appraisals` | []Appraisal   | `{kind, subject, result, confidence}` — cache of classify / inferred-relation results (avoid recomputation) |

### BoardStore port

(Go: `internal/store/board.go`.) The CLI is its sole gateway (invariant 1).

- `Get(taskID) -> *Board`
- `SetPrinciples(taskID, principles)`
- `SetDesign(taskID, design)`
- `AppendDecision(taskID, Decision{summary, detail})`
- `AddADR(taskID, ADR)`
- `Query(Query{text, task_ids?}) -> QueryResult{tasks[], decisions[], adrs[]}`
  — cross-board query → related tasks / prior decisions = project knowledge.

Default backend = out-of-repo file store (`internal/store/file`).

---

## Identity registry (IDRegistry port)

Anchors a stable clutch `id` to a durable **representation signature** so the
same signature yields the same id across scans. Lives beside the board store.
(Go: `internal/store/board.go`; signature in `internal/model/observation.go`.)

- `Signature{repo?, branch?, issue_link?}` — e.g. repo identity + branch, or an
  issue link. One anchoring strategy populated.
- `Resolve(sig) -> (id, ok, err)` — existing id, or `ok=false` if none anchored.
- `Mint(sig) -> (id, err)` — anchor a new stable id to `sig`.

`store.IDRegistry`'s method set matches `correlate.IDResolver` exactly, so the
file backend wires straight into the pure correlation core (see Dependency rule).

---

## Projection envelope (the `--json` machine contract)

The sole public data shape; MCP/file/dashboard surfaces are thin projections of
it. (Go: `internal/model/projection.go`.)

```json
{
  "schema_version": "0.1",
  "generated_at": "<RFC3339>",
  "tasks": [ /* Task */ ],
  "diagnostics": {
    "unresolved": [ /* Unresolved */ ],
    "scan_stats": {
      "repos_scanned": 0, "worktrees": 0, "sessions": 0,
      "tasks_projected": 0, "duration_ms": 0
    }
  }
}
```

`schema_version` is bumped on any breaking change to the envelope or Task shape.

---

## Architecture invariants

1. **Store is the only authority; the CLI is its sole gateway.** Ground truth is
   the Task+Board store — not a session, not an agent's judgment. The `clutch`
   CLI owns every read/mutation, and with it determinism, the invariants, and
   the safety floor. Agents are stateless; their judgment becomes true only once
   written back through the CLI. *Enforced structurally via invariant 3 + the
   dependency rule.*

2. **Two skill kinds: orchestrators vs capabilities.** Orchestrators are few and
   each owns a workflow verb. Capabilities are many, domain-general, an external
   library — referenced, not absorbed. *(Doc only — no code in this scaffold.)*

3. **The CLI is a public substrate, not a private interface.** Requires:
   machine output (stable, schema-versioned) kept **distinct** from human/TTY
   output; a **caller-agnostic safety gate**; **orthogonal, pipeable** primitives
   over mega-commands; further surfaces (MCP/file/dashboard) as **thin
   projections** of the one store. *Enforced: everything is under `internal/` so
   Go-import is blocked and the CLI/JSON is the only boundary; `internal/cli` has
   a distinct machine-vs-TTY output path (`output.go`) and a safety-gate
   placeholder (`safety.go`).*

---

## Dependency rule (enforced by package structure)

- `internal/model` imports **no other internal package** (stdlib `time` only).
- `internal/correlate` imports **ONLY** `internal/model` — pure, no IO. If it
  would need a non-model type, that type belongs in `model` (hence observation
  DTOs and `Signature` live there; the id resolver is a consumer-defined
  interface over model types).
- `internal/discover/*`, `internal/store/*`, `internal/adapter/*` import `model`
  (+ their own port).
- `internal/cli` (and `cmd/clutch`) is the **composition root** — the only place
  that imports across all packages.

## Ports (only where genuinely multi-backend)

- `store.BoardStore`, `store.IDRegistry` — file backend now; in-repo/MCP later.
- `adapter.IssueTracker` — github now (via `gh` shell-out); jira later.
- **No** unified `Discoverer` interface: git/fs/session are distinct producers
  exposed as concrete funcs returning model observations.

## External access & dependencies

- git/gh access is **shell-out** only — no go-git or any git library.
- The only third-party dependency is **cobra** (CLI).
