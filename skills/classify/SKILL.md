---
name: classify
description: "clutch orchestrator owning the classify workflow verb. Use to resolve the ambiguous remainder the deterministic core could not pin down — tasks carrying unresolved[] flags or an uncertain lifecycle — by making the irreducible judgment and persisting each verdict as a cached board appraisal through the clutch CLI. Trigger when a clutch scan reports unresolved diagnostics or you are asked to classify/appraise clutch tasks."
---

# classify

You are the `classify` orchestrator for clutch — the LLM appraisal layer that
fills the gap the deterministic core leaves behind. clutch reads git/fs/sessions
and projects everything it can resolve mechanically; what convention cannot
settle it surfaces for you. You make those judgments and write them back so a
later scan reuses them instead of re-deriving them.

This skill carries everything you need to act — what the projection holds, the
judgment you owe, and the exact appraisal you write back. Work from it directly;
do not go reading repo docs to recover the contract.

## Operating assumptions

- **You hold no state.** The store, reached only through the clutch CLI, is the
  sole authority. Your judgment becomes true only once written back through the
  CLI. Nothing you keep in context counts.
- **You write ONLY through the clutch CLI.** Never edit the store files, the
  board, or any projection directly, and never touch git/fs to record a verdict.
  Every mutation goes through a CLI command and its safety gate, or it did not
  happen.
- **You are idempotent given unchanged inputs.** Persist a `fingerprint` over the
  inputs you judged from. A later scan folds the cached verdict back into the
  projection unconditionally — the deterministic core does not re-check your
  fingerprint. The fingerprint is *your* token: on a re-run you compare it to the
  current inputs, leave the verdict untouched while it still holds, and re-appraise
  (the upsert replaces the stale record) once it changes. Re-running classify on
  unchanged inputs reproduces the same record.

## When you run

Invoked against the current projection, typically after a scan reports
unresolved work. The projection is the stable, schema-versioned envelope from the
clutch CLI scan in machine (JSON) form — read it; do not re-derive it from git.

## Inputs — the ambiguous remainder

From the scanned envelope, take only what the deterministic core could not
resolve:

1. **Unresolved flags.** Tasks (and the envelope diagnostics) carry
   `unresolved[]` entries. Each names a `kind` (classification / lineage /
   relation / link / identity / session), a `detail`, and the `refs` (RepRefs)
   the ambiguity concerns. These are addressed to you.
2. **Uncertain lifecycle.** Tasks whose deterministic lifecycle default is not
   confident — the mechanical signals underdetermine which lifecycle value
   applies.

Ignore everything the core already resolved by convention or declaration; that
is settled and persisted at confidence 1.0. Touch only the remainder.

## The judgment

For each item, make the call the deterministic core could not — this irreducible
judgment is the whole reason classify is a skill and not more deterministic code.
Reason from the representations the projection gives you (branches, PRs, issues,
sessions, lineage hints) and the task's board knowledge if relevant. Produce one
of the three appraisal kinds:

- **classification** → a lifecycle verdict. `result` is one of the lifecycle
  values: `idea`, `planned`, `active`, `review`, `merged`, `done`, `stale`,
  `superseded`.
- **relation** → a task-DAG edge. `result` is `depends:<taskID>` or
  `blocks:<taskID>`.
- **link** → which representation a link concerns. `result` is the subject ref
  the link resolves to.

If the evidence does not support a confident call, lower the confidence rather
than inventing certainty; do not fabricate edges or links the projection does not
support.

## Persisting a verdict — the appraisal contract

Write each verdict as a cached appraisal through the clutch CLI's board appraise
command — the store's only write path:

```
clutch board appraise <task-id> --kind <k> --subject <ref> \
  --result <r> --confidence <c> --fingerprint <fp> --yes
```

- `<task-id>` — the task the appraisal concerns.
- `--kind` — one of `classification | relation | link`.
- `--subject` — the RepRef the appraisal concerns, per kind:
  - **classification** → always `task:<task-id>`, the task itself — a lifecycle
    verdict judges the whole task, not one representation. Use the same id you
    pass as `<task-id>`; the CLI rejects any other subject for this kind.
  - **relation / link** → the representation RepRef the edge or link concerns,
    taken from the unresolved `refs`. RepRefs are keyed `repo:<identity>`,
    `branch:<identity>/<name>`, `worktree:<path>`, `pr:<host>#<number>`,
    `issue:<tracker>/<key>`, or `session:<host>/<id>`.
- `--result` — per the kind, in the formats above.
- `--confidence` — a value in `[0,1)`; appraisal is never 1.0 (that is reserved
  for deterministic convention/declared verdicts). Use it to say how strongly the
  evidence supports the call.
- `--fingerprint` — a hash over the exact inputs you judged from. The
  deterministic core folds your cached verdict back on every scan **without**
  re-checking this hash; it is the token *you* compare on your next run to notice
  the inputs changed and re-appraise. A folded verdict also suppresses the task's
  `classification` flag, so a stale verdict is refreshed only when you re-run and
  supersede it — the scan will not re-flag the task to prompt you.
- `--yes` — the safety gate refuses a mutating command without it.

The store upserts by `kind`+`subject`: a fresh verdict for the same pair replaces
the prior one (a recomputation supersedes), so re-running is safe.

## Confidence convention

- Deterministic convention / declared verdicts: confidence **1.0** (the core's,
  not yours).
- Your appraisals: confidence **< 1.0**, always.

## Done

You produce no state of your own and report no separate artifact. Success is: the
ambiguous remainder you judged is persisted as board appraisals through the CLI,
each carrying a fingerprint, so the next scan reuses them. Items you could not
judge confidently stay unresolved for a future pass — leaving them is correct,
guessing is not.
