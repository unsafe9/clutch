# clutch

clutch is a harness for agent-driven engineering work — equal parts interactive
workflow and automation surface. It carries work from a rough idea through
principled design to an integrated change, and keeps that work legible across
sessions and machines.

The name is the control it keeps: **cruise** when you trust it, **steer** when it
matters, and ride the clutch — partial engagement — to adopt it a piece at a time.

## Principles

- **No lock-in, incremental adoption.** clutch never demands that work begin inside
  it. It reads git-based work directly, so any branch, worktree, or local change is
  trackable whether or not clutch opened it, and work it merely *detected* is a
  first-class citizen. This holds on shared, team-managed projects too: because
  boards live outside the repo by default and clutch-created artifacts follow
  ordinary git conventions, one person can adopt clutch partially without leaving a
  trace that affects teammates.
- **Broad discovery.** Coverage does not assume a tidy single checkout. It spans
  however the work is actually laid out — multiple clones of the same project,
  worktrees scattered across the filesystem, submodules — and correlates work
  across all of them.
- **Local-first, GitHub-spanned.** Ground truth is the local checkout(s); clutch
  reaches out to GitHub (issues, PRs) when the work does.
- **Cross-session.** One session hands off to the next without re-deriving context.
- **Deterministic first, appraisal for the rest.** Pin down everything that can be
  read mechanically; invoke LLM appraisal only to classify the ambiguous remainder.
- **Plan at the engineering altitude.** Designs capture approach — architecture,
  boundaries, decisions — not code; the worker is an expert and writes the code
  when it runs. Shape and decide before building, and touch only what the goal
  requires.
- **No whole-project spec.** Documentation is opt-in. Knowledge accumulates as
  per-task records you can query, not as a single project spec that rots and leaks.

## Core model

The shared data every component reads from and writes to.

### Task

One flat object with relations.

- A clutch-assigned `id` independent of any single representation.
- Correlates its representations — branch, worktree, commits, PR, external issue,
  board — by convention first (branch prefixes, worktree paths, issue links in
  commits/PRs), with appraisal only for what conventions don't cover. One task may
  span several clones, worktrees, or submodules.
- Relations form a DAG of `depends`/`blocks` edges; each task records its `base`
  ref, parent lineage, and integration (merge) status. No task/subtask hierarchy —
  everything is a task, joined by relationships.
- Lifecycle: `idea → planned → active → review → merged/done`, plus `stale` /
  `superseded`. The `review → merged` transition is mode-dependent (see **Modes**).
- Provenance: `clutch-initiated` or `git-detected`. Mode: `cruise` or `steer`.
- Emits a structured projection so other tools can consume it.

### Board

An abstract planning substrate. Per task, the board holds work principles, an
evolving design that converges toward a final state (decisions overwrite or
accumulate), and ADRs for the tradeoffs made along the way. The board is a
*concept*, not a file format: a backend may be an in-repo file, an out-of-repo
file, an HTML playground, an MCP-backed view, or anything else. Default backends
keep planning out of commits (serving no-lock-in); promotion into the repo is an
opt-in escape hatch. The queryable history across boards is what gives planning its
project knowledge and awareness of related tasks — so a separate long-term memory
system is unnecessary for now.

## Components

### Deterministic core

Discovers and reads git, filesystem, and sessions across every clone, worktree, and
submodule; correlates by convention; emits the structured task projection. Cheap,
always fresh, no LLM.

### Agent layer

Classifies ambiguous state, plans into boards, runs the manager loop, and
dispatches execution. Its judgments persist to the board so they are not recomputed.

### Work tracker

Projects detected work onto the task graph — local changes, scattered worktrees,
multiple clones, submodules — assigning meaning to local-only changes and tracking
base, lineage, dependencies, and merge status. External adapters (Jira, GitHub
issues) plug in.

### Session tracker

Identifies in-progress Claude Code and Codex sessions and infers which task each is
advancing — local-first, but able to follow GitHub PRs. Closely related to the work
tracker and may merge with it.

### Planner

Shapes a task's board at the engineering altitude — architecture, boundaries,
principles, decisions, ADRs — never code. Draws on board history and related tasks,
never a whole-project spec.

### Review gate *(noted — not yet designed)*

A multi-agent gate a change passes through several independent reviewers. In cruise
it *is* the merge gate and the source of the confidence signal; in steer it
complements the human reviewer. When findings pile up it can send work back to be
rewritten rather than patched, and postmortems on failed or heavily-revised changes
feed lessons back. A companion **quality manager** watches accumulated code rather
than a single change and raises refactoring proposals as new tasks.

### Manager / executor

The manager holds the roadmap and idea bank, deep-plans a suggestion into a formal
task or shelves it as a future idea, then drives task-ified work to execution —
judging merge order and safe parallelism, and tracking each task's review state so
nothing blocks silently. The executor runs the work (tmux today). The role is large
and may split along this manager/executor seam.

### Surfaces

Projections of the same task store — tracker, session view, roadmap / idea-bank,
"waiting on you" queue — never separate sources of truth.

## Invariants

Architecture rules that hold across every component, settled up front so they are
not re-litigated per feature.

- **The store is the only authority; the CLI is its sole gateway.** Ground truth is
  the Task + Board store — not a CLI session, not an agent's judgment. One `clutch`
  CLI owns every read and mutation of that store, and with it all determinism,
  invariants, and the safety floor; agents are stateless, and their judgment becomes
  true only once written back through the CLI. Forced by two principles at once:
  *deterministic-first* (authority living in appraisal would be recomputed and
  non-reproducible) and *agent-neutral* (authority living in a skill would let a host
  swap change behavior). Litmus — anything mechanically enforceable, stateful, or
  safety-bearing is a CLI command; only irreducible judgment is a skill.
- **Two skill kinds: orchestrators are few, capabilities are many.** Orchestrators
  own a workflow verb and its state transitions (classify, plan, review, manage) —
  clutch-specific, stateless, writing back through the CLI, added only to justify a
  genuinely new flow. Capabilities are domain-general expertise and review lenses
  (abstraction, boundaries, concurrency, protocol, …) — owning no flow and no state,
  invoked by an orchestrator like a library, and living in a shared host-neutral
  library that clutch *references* rather than absorbs (reuse outside clutch,
  no-lock-in at the skill layer). Litmus — a new flow or decision-point is an
  orchestrator (rare); a new lens or body of expertise is a capability (expected to
  grow).
- **The CLI is a public substrate, not a private interface.** Internal orchestrators,
  external capability skills, and foreign systems are all clients of one CLI
  contract — at once authority gateway, data-provision layer (the tool-consumable
  projection), and capability adapter; so *agent-neutral* generalizes to
  system-neutral. This requires: machine output (stable, schema-versioned) kept
  distinct from human/TTY output; a caller-agnostic safety gate; orthogonal,
  pipeable primitives over mega-commands; and further surfaces (MCP, file emit,
  dashboards) as thin projections of the one store, never parallel truth. The shape
  is adopted now; public-API stability is promised only once a real external
  consumer exists.

## Modes

Every task runs in one of two modes — clutch's autonomy policy made concrete.

- **cruise — autonomous, hands-off.** Auto-merges once *grounded* confidence clears
  the bar (CI/tests green plus multi-lens review consensus, never a self-asserted
  score). Sub-threshold work escalates or holds.
- **steer — human-in-the-loop, surgical.** Interactive design and coding; nothing
  merges without human approval.

Mode is a task property, defaulting per project and overridable per task, with an
escalation path: a cruise task that drops below confidence escalates to steer, and
a steer task can hand delegable sub-work down to cruise. A **non-negotiable safety
floor** applies in both — force-push, commits to `main`/`master`, production,
secrets, and destructive actions are always gated, however high cruise confidence
runs.

## Workflow

Work does not travel a single line. A task enters at different points, then runs
the flow for its mode, with feedback loops pulling it backward or sideways.

### Entry points

- **Clutch-initiated** work starts at planning.
- **Git-detected** work folds onto the task graph at the stage it is actually in —
  an in-progress branch enters `active`, an open PR enters `review` — without
  back-filling a plan it never had.

### cruise flow — autonomous

The manager drives it end to end; no human in the loop until escalation.

1. **Plan.** The planner shapes an engineering design on the board.
2. **Dispatch & execute.** The manager picks ready tasks, judges parallelism and
   merge order, and dispatches an executor within a token budget; it implements
   one-shot (see *Execution budget*).
3. **Review.** The review gate runs multi-agent and produces grounded confidence.
4. **Merge.** Above the confidence bar → auto-merge and integrate. Below →
   escalate to steer, hold, or send back to be rewritten. The safety floor always
   applies.

### steer flow — interactive

The human holds the wheel; clutch assists rather than drives. It is a back-and-forth,
not a dispatch-and-forget pipeline.

1. **Design.** Planning and design happen interactively with the human on the
   board, surgically.
2. **Code.** Interactive, human-in-the-loop implementation.
3. **Review & merge.** The review gate complements the human reviewer; nothing
   merges without human approval. The manager tracks review/approval state and
   surfaces the task in the "waiting on you" queue. On approval → merge and
   integrate.

### Feedback loops

These cross both modes.

- **Revalidation / bounce-back.** At execution start a worker can reject a stale
  design and send the task back to planning rather than build on it.
- **Reconciliation.** Plan and reality are re-checked as work proceeds —
  just-in-time per task; a global rescan cadence is open (cost).
- **Escalation.** A cruise task that drops below confidence escalates to steer; a
  steer task hands delegable sub-work down to cruise.
- **Learning.** Refactor proposals and postmortems feed the idea bank, becoming new
  tasks.

### Execution budget & self-improvement *(noted)*

Primarily a cruise / autonomous-dispatch concern — steer work is human-paced. A
task is launched with a token budget set up front — e.g. 300k, around the
context-window point where a model starts losing the thread and auto-compaction
kicks in. The aim is to finish "one shot," inside that budget, before quality
degrades. A budget overrun is a *signal*, not just a retry trigger: clutch
diagnoses why the one-shot failed before re-running —

- Is the task too big? → split it.
- If it can't be split — is it underspecified (needs more design) or has its scope
  bloated? → tighten or re-plan.
- Any other reason it stalled? → analyze, then enter a self-improvement procedure
  rather than blindly retrying.

## Deferred / open

- **Long-term memory system** — likely redundant with queryable board history;
  deferred.
- **Cruise sub-threshold behavior** — escalate to steer, hold as a refactor
  candidate, or retry? Undecided.
- **Mode default granularity** — project-level default vs. per-task, and the
  conflict rule when both are set. Undecided.
- **Reconciliation cadence vs. cost** — just-in-time per-task revalidation is
  cheap, but a manager rescan of *every* task on each event may be too costly to
  run every time. Cadence undecided.
- **Execution beyond tmux** (CI, GitHub-triggered) — under consideration.
- **Store & board persistence across machines** — backend-dependent; not yet
  decided.
