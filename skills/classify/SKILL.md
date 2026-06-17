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

Read `docs/contract.md` once before acting — it is the authoritative shape of the
projection, the appraisal record, and the enums you produce. This skill leads
with what that contract cannot tell you: when you run, the judgment you owe, and
the contract you must honor on write.

## Operating assumptions

- **You hold no state.** The store, reached only through the clutch CLI, is the
  sole authority. Your judgment becomes true only once written back through the
  CLI. Nothing you keep in context counts.
- **You write ONLY through the clutch CLI.** Never edit the store files, the
  board, or any projection directly, and never touch git/fs to record a verdict.
  Every mutation goes through a CLI command and its safety gate, or it did not
  happen.
- **You are idempotent given unchanged inputs.** Persist a `fingerprint` over the
  inputs you judged from. A later scan folds the cached appraisal back in and
  skips recomputation while that fingerprint holds; re-running classify on
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

- **classification** → a lifecycle verdict. `result` is a `Lifecycle` enum value
  (see `docs/contract.md` for the set, e.g. `active`, `review`, `stale`).
- **relation** → a task-DAG edge. `result` is `depends:<taskID>` or
  `blocks:<taskID>`.
- **link** → which representation a link concerns. `result` is the subject ref
  the link resolves to.

If the evidence does not support a confident call, lower the confidence rather
than inventing certainty; do not fabricate edges or links the projection does not
support.

## Persisting a verdict — the appraisal contract

Write each verdict as a cached appraisal through the clutch CLI's board appraise
command. Pass, per item:

- the task id it concerns
- `--kind` one of `classification | relation | link`
- `--subject` the RepRef the appraisal concerns (from the unresolved `refs`)
- `--result` per the kind, in the formats above
- `--confidence` a value in `[0,1)` — appraisal is never 1.0 (that is reserved
  for deterministic convention/declared verdicts); use it to express how strongly
  the evidence supports the call
- `--fingerprint` a hash over the exact inputs you judged from, so the cache can
  be reused while inputs hold and invalidated when they change
- confirm the mutation non-interactively (the safety gate requires it)

Run `clutch board appraise --help` (and `clutch scan --help`) for the exact flag
spelling, the confirmation flag, and config selection — point there rather than
trusting any spelling restated here. The store upserts by `kind`+`subject`: a
fresh verdict for the same pair replaces the prior one (a recomputation
supersedes), so re-running is safe.

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
