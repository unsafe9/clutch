# clutch contract (schema_version 0.1)

> Authoritative spec for the Wave-0 scaffold. The Go types under `internal/model`
> and this document agree field-for-field; if they ever diverge, that is a bug.
> Wave 3's golden e2e MUST include a JSON snapshot/schema golden test asserting
> the emitted envelope/Task shape matches this contract, so the "doc ⇄ code
> agree" promise is mechanically enforced rather than maintained by hand.
> Audience: an LLM or engineer wiring against clutch. Read intent first.

## Intent

clutch is an agent-neutral CLI harness for agent-driven engineering work. A
single `clutch` Go binary is the **authoritative gateway** to a Task+Board
store; external agents/systems consume it via the CLI (JSON), never by importing
Go. It is **deterministic-first**: the deterministic core reads git/fs/sessions
and emits a structured **Task projection**; an LLM "appraisal" layer fills only
the ambiguous remainder, and only its results are written back through the CLI.

---

## Task projection model — 3 classes by derivation & persistence

A Task is one object whose fields fall into three classes by *how they are
derived* and *whether they persist*. This grouping is the core design.
`provenance` (clutch-initiated / git-detected) is a single **field within Class
①** — it is not the axis the classes are cut along. (Go:
`internal/model/task.go`.)

### Class ① Identity & policy — PERSISTED, stable across scans

Lives in the store / id-registry. Set by clutch and the planner/agent layer.

| field        | type          | meaning                                                  |
|--------------|---------------|----------------------------------------------------------|
| `id`         | string        | clutch-assigned identity, independent of any representation, stable |
| `title`      | string        | label (from branch/PR/issue or planner)                  |
| `lifecycle`  | Lifecycle     | enum (see below)                                          |
| `mode`       | Mode          | enum; **effective** value — the projection defaults an unset stored mode to `steer` (see below) |
| `provenance` | Provenance    | enum: clutch-initiated / git-detected                    |
| `board`      | *BoardRef     | pointer/locator to this task's board backend             |
| `created`    | RFC3339 time  | when the identity was first minted/seen; set once, then stable |
| `updated`    | RFC3339 time  | when the task's observed state last changed (see below)  |

**`created` / `updated` derivation.** Both are persisted in the id-registry
(never recomputed from scratch) and filled into the projection each scan.
`created` is stamped once, the first time the id is minted/seen. `updated`
advances only when a scan observes the task's derived state change: the registry
records a **fingerprint** — a digest of the task's Class-② representations and
Class-③ relations — at each update, and a scan whose fingerprint differs from the
stored one advances `updated` and re-records the fingerprint. A freshly-minted
task has `created == updated`. Timestamp determinism does not depend on the wall
clock across scans: unchanged state re-emits the persisted values verbatim.

**`mode` — stored vs. effective.** The **stored** mode is Class-① policy in the
id-registry, written only by an explicit policy action (no setter command exists
yet, so it is currently always unset). The projection emits an **effective**
mode: the stored mode when set, else the default `steer` — the safe
human-in-the-loop default. The broader mode-default policy (project-level default
vs. per-task, and the conflict rule when both are set) stays **open** (README:
*Mode default granularity*).

### Class ② Representations — DERIVED, recomputed each scan, NEVER persisted

Pure deterministic projection of git/fs/session state. **Carries zero LLM.**
Every representation carries a `ref` (a `RepRef`, see below) so Class-③ links,
unresolved flags, and board appraisals can name which representation they
concern.

| field         | type           | shape                                                   |
|---------------|----------------|---------------------------------------------------------|
| `repos`       | []RepoRef      | `{ref, identity, path, remote}` — clones/checkouts spanned |
| `branches`    | []Branch       | `{ref, repo, name, head, base, upstream, ahead, behind, integration}` |
| `worktrees`   | []Worktree     | `{ref, path, branch, repo}`                             |
| `commits`     | CommitSummary  | `{head, count}` — summary, NOT a full commit list       |
| `prs`         | []PullRequest  | `{ref, host, number, url, state, draft, checks, review_decision, mergeable}` |
| `issues`      | []Issue        | `{ref, tracker, key, url, state}` — external (jira/github) |
| `sessions`    | []Session      | `{ref, host, cwd, branch?, last_activity, running}` — host = claude-code\|codex |

A checkout is discovered by both producers, which assign it **divergent
identities** — git's remote identity (e.g. `github.com/acme/app`) and fs's
path-based `local/<base>` — that only its shared path unifies. The two collapse
into a **single** `repos` entry keyed by path, keeping the durable remote identity
when present; a remote-backed repo therefore yields one rep, not two. Per-repo,
the projected rep count and the path-deduped `scan_stats.repos_scanned` agree.

`base` (fork-point ref) and `integration` (enum: unmerged / merged / conflicts /
behind) are **per-Branch**, not Task-level: one task spans multiple branches/repos
with divergent fork-points and merge states, so a single Task-level scalar cannot
express them. There is **no** Task-level integration rollup — a consumer derives
one from the per-branch values if it wants one.

A `PullRequest` carries detailed external-review status so the worker can act on
it: `review_decision` (`approved` / `changes_requested` / `review_required` /
empty) and `mergeable` (`mergeable` / `conflicting` / `unknown` / empty), in
addition to `state`, `draft`, and the `checks` rollup. PRs are observed across all
states (open / merged / closed), not open-only, so the merged-PR signal is visible.

A `worktrees` entry attaches only to the task whose branch is checked out in that
worktree — not to every branch-task of the repo. A `sessions` entry binds to the
task owning the session's recorded `branch` at its `cwd`'s repo; cwd-only routing
(first match wins) is the fallback used only when the branch is absent or matches
no branch-task there.

> `Session` fields are **finalized** against the reverse-engineered CC/Codex
> on-disk formats; see `docs/session-format.md` for the per-host field mapping,
> cwd recovery, last-activity, and the deterministic `running` rule.

#### RepRef — within-task representation key

`RepRef` is an **opaque, stable, within-task** string key naming one
representation, so a `Link`, `Unresolved` flag, or `Appraisal` can point at the
exact representation it concerns. It is stable within a single task projection;
it is **not** a global identifier. Key scheme:

| representation | `ref` form                     |
|----------------|--------------------------------|
| RepoRef        | `repo:<identity>`              |
| Branch         | `branch:<repo-identity>/<name>` |
| Worktree       | `worktree:<path>`             |
| PullRequest    | `pr:<host>#<number>`          |
| Issue          | `issue:<tracker>/<key>`       |
| Session        | `session:<host>/<cwd>`        |

### Class ③ Relations & correlation — MIXED: derived + declared + appraisal

| field        | type          | shape / derivation                                         |
|--------------|---------------|------------------------------------------------------------|
| `lineage`    | Lineage       | `{parents[]}` — parent task ids (derived from a branch's `base` where possible, else declared) |
| `relations`  | Relations     | `{depends[], blocks[]}` — task-id DAG; declared or appraised |
| `links`      | []Link        | per representation-link: `{subject, method, confidence}` — `subject` is the RepRef this link concerns |
| `unresolved` | []Unresolved  | `{kind, detail, refs?, task_id?}` — `kind` is an `UnresolvedKind`; `refs` are the RepRef(s) the ambiguity concerns; fed to the `classify` orchestrator later |

`Link.method` is one of `convention | appraisal | declared` (LinkMethod enum).
Convention/declared imply confidence 1.0; appraisal < 1.0.

### Who writes what

- **Class ②** carries **zero LLM** — it is recomputed every scan and discarded.
- **Class ③'s appraisal parts** (appraised relations, `appraisal`-method links,
  unresolved resolutions) and **class ①'s planner-set parts** (e.g. `title`,
  `mode`) are written by the agent layer, and **those persist to the board** to
  avoid recomputation.

### Task creation & provenance

`provenance` records the task's birth path, and there are two:

- **git-detected** — the deterministic scan discovers a representation
  (branch / PR / issue / repo) and correlation mints or reuses an id anchored to
  its signature. Class ① is re-derived from observations every scan.
- **clutch-initiated** — created directly through the CLI, the primitive for
  clutch-initiated work that starts before any git representation exists:

  ```
  clutch task new --title <title> [--mode cruise|steer] [--base <ref>] --yes
  ```

  It is a **mutating** action, so it passes the safety gate (`--yes` /
  `CLUTCH_ASSUME_YES`). It mints an id through the **same IDRegistry** as
  scan-discovered tasks but anchors **no signature yet** (it has no
  representation). Its Class ① identity/policy — `title`, optional `mode`/`base`,
  and `created` — is **persisted** in the registry and folded into every
  subsequent `scan` / `tasks` projection as a **registry-only** task (empty
  Class ②). Each invocation mints a distinct id; the title is a label, not a key.

  A clutch-initiated task **starts at the `idea` lifecycle**. (Board-driven
  derivation to `planned` is added in a later phase.) When a later scan discovers
  a branch that correlates to the task, that branch's signature is meant to
  attach to the same id so the representations join it — this attach-by-convention
  linkage is **not yet implemented**, so until then a clutch-initiated task and a
  later-created branch project as separate ids.

---

## Enums

String-typed; the JSON wire form is exactly the listed value. (Go:
`internal/model/enums.go`.) These values are part of the machine contract — keep
them stable.

| enum           | values                                                                 |
|----------------|------------------------------------------------------------------------|
| Lifecycle      | `idea` `planned` `active` `review` `merged` `done` `stale` `superseded` |
| Mode           | `cruise` `steer`                                                        |
| Provenance     | `clutch-initiated` `git-detected`                                       |
| Integration    | `unmerged` `merged` `conflicts` `behind`                                |
| LinkMethod     | `convention` `appraisal` `declared`                                     |
| UnresolvedKind | `lineage` `relation` `link` `identity` `session` — **extensible**       |
| AppraisalKind  | `classification` `relation` `link` — **extensible**                    |

`UnresolvedKind` and `AppraisalKind` sets are **extensible**: consumers MUST
tolerate kinds they do not recognize.

---

## Board (state behind the BoardStore port)

Durable per-task knowledge at engineering altitude — **NO code**. (Go:
`internal/model/board.go`.)

| field        | type          | meaning                                                   |
|--------------|---------------|-----------------------------------------------------------|
| `principles` | string        | work principles for the task                              |
| `design`     | string        | evolving design that converges to final; decisions overwrite/accumulate; engineering altitude, NO code |
| `adrs`       | []ADR         | `{decision, context, alternatives[], consequence}`        |
| `appraisals` | []Appraisal   | `{kind, subject, result, confidence, input_fingerprint, computed_at}` — cache of classify / inferred-relation results; `kind` is an `AppraisalKind`, `subject` is the RepRef appraised, `input_fingerprint` + `computed_at` let a cached appraisal be invalidated when its inputs change |

### BoardStore port

(Go: `internal/store/board.go`.) The CLI is its sole gateway (invariant 1).

- `Get(taskID) -> *Board`
- `SetPrinciples(taskID, principles)`
- `SetDesign(taskID, design)`
- `AppendDecision(taskID, Decision{summary, detail})`
- `AddADR(taskID, ADR)`
- `AddAppraisal(taskID, Appraisal)` — upsert a cached appraisal: an existing
  appraisal with the same `kind`+`subject` is **replaced** (a recomputation,
  with fresh `input_fingerprint`/`computed_at`, supersedes it), else appended.
  `appraisals` is kept deterministically ordered on write (by `kind`, then
  `subject`).
- `Query(Query{text, task_ids?}) -> QueryResult{tasks[], decisions[], adrs[]}`
  — cross-board query → related tasks / prior decisions = project knowledge.

Default backend = out-of-repo file store (`internal/store/file`).

---

## Identity registry (IDRegistry port)

Anchors a stable clutch `id` to durable **representation signatures** so the same
signature yields the same id across scans. Lives beside the board store. (Go:
`internal/store/board.go`; signature in `internal/model/observation.go`.)

- `Signature{repo?, branch?, issue_link?}` — e.g. repo identity + branch, or an
  issue link. One anchoring strategy populated. A `Signature` is **one** durable
  key, not a bundle.
- `Resolve(sig) -> (id, ok, err)` — existing id, or `ok=false` if none anchored.
- `Mint(sig) -> (id, err)` — anchor a new stable id to `sig`.
- `Attach(id, sig) -> err` — anchor an **additional** signature to an existing id
  (also covers aliasing). This is how **multi-representation anchoring** works:
  one id may have MANY signatures (one per representation), via the registry, not
  via a multi-field `Signature`.
- `Merge(keepID, mergeID) -> (id, err)` — two ids found to be one task; returns
  the surviving id.
- `Retire(id) -> err` — task gone; the id is **NOT deleted** (board knowledge
  persists), only marked retired.

Besides the signature→id anchoring, the registry persists per-id **identity
metadata** — `created` / `updated` timestamps, the `updated` fingerprint, and the
stored `mode` — read back each scan to fill the projection's Class-① fields (see
*Class ① → created/updated derivation* and *mode — stored vs. effective*).

**Clutch-initiated ids:** an id may be minted with **zero signatures anchored** —
a `clutch task new` task has an identity before it has any representation. The
backend persists that task's Class ① identity/policy beside the signature index
and lists it back so correlation can materialize it (see *Task creation &
provenance*). A correlating signature is attached later, once a representation
appears.

**Vanished-representation behavior:** when a previously-anchored signature stops
appearing in scans, the id is **retained** (board history is the knowledge
pillar), the representation drops from the projection, and the task may become
`stale`. **Split** (one id discovered to be two tasks) is **deferred/open** — no
method exists for it yet.

`store.IDRegistry` satisfies `correlate.IDResolver` (Resolve/Mint/Attach/Merge)
and adds the maintenance op `Retire` (NOT in `IDResolver`, since the pure
correlation core does not perform registry maintenance). So the file backend
wires straight into the pure correlation core (see Dependency rule).

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

`schema_version` follows the policy below.

`scan_stats` summarizes the run: `repos_scanned` and `worktrees` are distinct
paths across the git and fs producers (deduped by path, since the two assign a
repo divergent identities that only its path unifies); `sessions` counts only the
sessions **within a configured search root** (out-of-scope sessions are neither
read nor counted); `tasks_projected` is the projected task count; `duration_ms`
is the wall-clock scan time.

`diagnostics.unresolved` is the whole ambiguous remainder: each task's own
`unresolved` flags (every one carrying that task's `task_id`) unioned with the
**scan-wide** flags that belong to no single task. Scan-wide flags (empty
`task_id`, e.g. an in-scope session that matched no repo/worktree) surface **only**
here — they are returned separately by the correlation core, never parked on an
arbitrary task, so no task's `unresolved` list holds a flag that is not its own.

A `session`-kind `unresolved` flag is emitted only for an **in-scope** session
whose `cwd` matched no discovered repo/worktree; sessions whose `cwd` lies outside
every search root are dropped, never flagged — they are permanent noise the
classify layer cannot act on.

**Arrays are never null.** Every array documented in this contract renders as `[]`
when empty, never `null` — at the envelope level (`tasks`,
`diagnostics.unresolved`) and within each Task (`repos`, `branches`, `worktrees`,
`prs`, `issues`, `sessions`, `links`, `unresolved`, `lineage.parents`,
`relations.depends`, `relations.blocks`) and Board (`adrs`, `appraisals`,
`adrs[].alternatives`). Fields marked `?` (e.g. `Unresolved.refs`) are optional
and are omitted when absent rather than emitted as `null`.

---

## Schema versioning policy

`schema_version` is `MAJOR.MINOR`:

- **MAJOR** bumps on a **breaking** change: a field removed, renamed, or retyped,
  or a field's semantics changed.
- **MINOR** bumps on an **additive** change: a new field.
- Consumers **MUST ignore unknown fields** and **MUST NOT** assume any field
  beyond their pinned MAJOR.
- **Pre-1.0 (`0.x`) is unstable**: while MAJOR is `0`, a MINOR bump MAY break.

Current: `schema_version = "0.1"`. (Go: `internal/model/projection.go`.)

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
  DTOs and `Signature` live there); anything else is a **consumer-defined
  interface over model types**. Three such seams exist:
  - `IDResolver{Resolve, Mint, Attach, Merge}` — id lifecycle for the core.
  - `AppraisalReader{Appraisals(taskID) -> []Appraisal}` — reads persisted
    appraisals back so a cached classify/relation result is reused, not
    recomputed.
  - `InitiatedTaskReader{InitiatedTasks() -> []InitiatedTask}` — lists
    clutch-initiated tasks so ones with no git/fs/session representation yet
    still project.
  Entry point: `Correlate(obs Observations, ids IDResolver, appraisals AppraisalReader, initiated InitiatedTaskReader) -> (Result{Tasks, ScanWide}, error)` — `ScanWide` holds the scan-wide unresolved flags that belong to no task (the CLI unions them with per-task flags into `diagnostics.unresolved`).
  The file backend satisfies all three seams (asserted in `internal/cli/wire.go`).
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
