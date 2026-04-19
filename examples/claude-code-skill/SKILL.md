---
name: aperture
description: Deterministic context planner that runs BEFORE a coding agent starts work. Use this skill whenever a coding task is about to begin against a repository — to score which files the agent should load, fit them into a token budget, detect missing spec/tests/config/runtime context, and compute a feasibility score. Trigger this at the start of any non-trivial coding task, after a plan has been approved, before invoking Claude Code / Codex / any agent on a repo task, whenever the user mentions "let's code this up" or "start implementing" or "run the agent", and any time feasibility of a task is in question. This is a gate, not a coding tool — it decides whether an agent should even start.
---

# Aperture — Pre-Agent Context Gate

Aperture is a deterministic context planner. It runs against a task description and a repository, and produces a scored, budgeted, hashed manifest of which files the agent should load (and in what form), what's missing, and whether the task looks feasible at all. No LLM calls in the planning path. Same inputs → byte-identical hash.

This is a **gate tool**, positioned alongside `speccritic` and `plancritic`:

```
SPEC.md → speccritic → PLAN.md → plancritic → [APERTURE] → CODE → realitycheck → prism → clarion
```

Aperture's job is to refuse to start coding on a task that:
- lacks enough anchors to be actionable,
- can't fit in the token budget,
- is missing a spec, tests, config, runtime path, or external contract the task obviously requires,
- has ambiguous ownership across packages.

If aperture's feasibility is too low or a blocking gap fires, **do not launch the agent**. Kick the task back to whichever upstream stage owns the missing input.

---

## When to invoke

Invoke aperture at task kickoff. Concretely:

1. A `PLAN.md` has cleared `plancritic` and a coding task is about to start.
2. The user says some variant of "let's implement this", "start coding", "run the agent", "kick off Claude Code on this".
3. The user asks whether a task is feasible, whether there's enough context, or why an agent keeps going off the rails.
4. The user wants to check the token budget headroom before a long agent run.
5. A previous agent run failed and the suspicion is context starvation or context overload.

Do **not** invoke aperture for:
- Trivial single-file edits where context is obvious.
- Tasks that don't touch a repository (pure Q&A, spec writing, planning).
- Running the agent itself — aperture runs *before* the agent, or orchestrates it via `aperture run`.

---

## Workflow

### 1. Produce or locate a task file

The task file is the input. It must be a markdown file or inline text describing:
- what changes are needed,
- which symbols, paths, or packages are involved (anchors),
- what the success criteria look like (tests, behavior, etc.).

Prefer a `TASK.md` derived from the approved `PLAN.md`. If the user gave a one-liner, either expand it into a task file first or pass it via `-p`.

### 2. Plan

Run:

```bash
aperture plan TASK.md \
  --model <model-id> \
  --budget <token-ceiling> \
  --format json \
  --out .aperture/manifests/latest.json
```

Model dispatch matters — `cl100k_base` / `o200k_base` / `p50k_base` / `r50k_base` tokenizers are embedded for OpenAI families; Claude and unknowns fall back to conservative `ceil(len/3.5)`. If the task is Claude-bound, expect budget headroom to be *pessimistically* reported. That's safe, not a bug.

For Claude Code's current default, use `--model claude-sonnet-4-6` and a budget that leaves room for reserves (instructions + reasoning + tool output + expansion). `.aperture.yaml` `defaults.reserve` controls this — check it before overriding on the CLI.

### 3. Explain

Before deciding whether to run, render reasoning:

```bash
aperture explain TASK.md
```

This walks the selection decisions, load modes, exclusions, and gaps in plain text. Read it. Do not skim. If the top-scoring file isn't the one the task is actually about, the task text is probably underspecified — go back to step 1 and add anchors.

### 4. Decide

Look at three things in the manifest:

**`feasibility.score`** (0.0–1.0):
- `≥ 0.85` — high. Proceed.
- `0.65 – 0.84` — moderate. Proceed if no blocking gaps; otherwise fix gaps first.
- `0.40 – 0.64` — weak. Do not run the agent. Address the lowest sub-signal.
- `< 0.40` — poor. Task is underspecified, budget-infeasible, or ambiguous. Rewrite.

**`feasibility.sub_signals`**: the decomposition. If `coverage` is low, the task touches files aperture couldn't find or score. If `anchor_resolution` is low, task anchors don't map to repo symbols. If `budget_headroom` is low, the budget is too tight for the selected files. If `task_specificity` is low, the task itself is vague.

**`gaps[]`**: any gap with `severity: "blocking"` is a stop. See the gap remediation table below.

### 5. Run (only if gate passes)

```bash
aperture run claude TASK.md \
  --fail-on-gaps \
  --min-feasibility 0.65 \
  --out-dir .aperture/manifests/
```

`--fail-on-gaps` exits 8 if any blocking gap is present. `--min-feasibility` exits 7 below threshold. Let these exits propagate — they are the gate working as designed. Do not bypass them.

The adapter receives these environment variables, which downstream tools (realitycheck, prism, clarion) can read:

```
APERTURE_MANIFEST_PATH
APERTURE_MANIFEST_MARKDOWN_PATH
APERTURE_TASK_PATH
APERTURE_PROMPT_PATH
APERTURE_REPO_ROOT
APERTURE_MANIFEST_HASH
APERTURE_VERSION
```

The `APERTURE_MANIFEST_HASH` is the join key for the rest of the pipeline — any downstream artifact can reference it to prove which planning decisions drove which implementation.

---

## Gap remediation

Every gap category maps to a specific upstream action. Never "work around" a gap by lowering the threshold — fix the input.

| Gap | What to do | Upstream owner |
|---|---|---|
| `missing_spec` | Write or locate a `SPEC.md` / `AGENTS.md`. | `speccritic` |
| `missing_tests` | Add test expectations to the task, or add `_test.go` stubs for the target package. | task author |
| `missing_config_context` | Task mentions config/env/settings — name the specific config file(s) in the task text. | task author |
| `unresolved_symbol_dependency` | Task names a symbol that isn't exported in the repo. Either the symbol needs creating (say so explicitly) or the name is wrong. | task author |
| `ambiguous_ownership` | Multiple packages compete for the top score. Add a package or path anchor to disambiguate. | task author |
| `missing_runtime_path` | Task implies runtime behavior (network, filesystem, time, db) but no file with an `io:*` side-effect tag was selected. Name the runtime touchpoint. | task author |
| `missing_external_contract` | Task mentions an API/RPC/schema but no `*openapi*`/`*swagger*`/`*schema*` file was selected. Either add the contract or reference it by path. | task author or spec |
| `oversized_primary_context` | Budget too tight — either raise `--budget`, split the task, or reduce reserves in `.aperture.yaml`. | task author or config |
| `task_underspecified` | <2 anchors, or action `unknown`, or no candidate scored ≥0.60. Rewrite the task with concrete paths and symbols. | `plancritic` or task author |

Multiple gaps at once usually mean the task came in too vague — go back a stage, don't patch forward.

---

## Exit codes

Respect these. They are the contract with CI and with downstream stages:

| Code | Meaning | Response |
|---|---|---|
| 0 | Success | Proceed. |
| 7 | Below `--min-feasibility` | Fix the task or the repo state; do not lower the threshold. |
| 8 | Blocking gap present | Resolve the gap per table above. |
| 9 | Budget underflow | Raise budget, narrow task scope, or reduce reserves. |
| 10 | Model has no tokenizer table | Use a supported model or let the `heuristic-3.5` fallback carry. |
| 11 | Unknown agent | Check `.aperture.yaml` `agents:` block. |
| 12 | Adapter failed to start | Install the adapter or fix `$PATH`. |
| other | Adapter's own exit | Treat as an agent failure, not an aperture failure. |

---

## Configuration

`.aperture.yaml` at the repo root is authoritative. It is hashed into `manifest.generation_metadata.config_digest`, so a config change produces a new manifest hash (correct — the decisions are different).

Common knobs:

```yaml
defaults:
  model: claude-sonnet-4-6
  budget: 120000
  reserve:
    instructions: 6000
    reasoning:    20000
    tool_output:  12000
    expansion:    10000

thresholds:
  min_feasibility: 0.70
  fail_on_blocking_gaps: true

gaps:
  blocking:            # promote specific gaps to blocking in this repo
    - missing_spec
    - oversized_primary_context

scoring:
  weights:             # must sum to 1.0 ± 0.001
    mention:  0.25
    filename: 0.12
    symbol:   0.20
    import:   0.12
    package:  0.10
    test:     0.08
    doc:      0.07
    config:   0.06
```

Only adjust `scoring.weights` if there's concrete evidence the defaults misfire on this repo. Weight changes alter every manifest hash in the project — they are not a casual knob.

---

## Security posture

`aperture plan` and `aperture explain` are safe on any repository — they make no network calls on repo contents and they never exec an adapter.

`aperture run` executes whatever command is in `.aperture.yaml` `agents.<name>.command`. Treat `.aperture.yaml` like a `Makefile`: review before running from a fresh clone or a PR branch. In CI on fork PRs, **use `plan`, never `run`**.

Never include secrets in task files. The merged `run-<id>.md` prompt is persisted to `.aperture/manifests/` and is fed verbatim to the agent.

---

## Common invocations

**Quick feasibility check, no persistence:**
```bash
aperture plan TASK.md --model claude-sonnet-4-6 --budget 120000 --format markdown
```

**Full pipeline-integrated run:**
```bash
aperture run claude TASK.md \
  --model claude-sonnet-4-6 \
  --budget 120000 \
  --fail-on-gaps \
  --min-feasibility 0.70 \
  --out-dir .aperture/manifests/
```

**Inline task (no TASK.md):**
```bash
aperture plan -p "Add OAuth refresh to internal/oauth/provider.go with unit tests" \
  --model claude-sonnet-4-6 --budget 120000
```

**Inspect why a selection happened:**
```bash
aperture explain TASK.md | less
```

**Clear caches (after tool upgrade or on cache-format drift):**
```bash
aperture cache clear
```

---

## Interpretation cheatsheet

- **Same task, same repo, same config → same `manifest_hash`.** If the hash changes, something upstream changed. That's a feature, not a nuisance.
- **`load_mode: full`** means the agent gets the whole file. **`structural_summary`** gives package/types/interfaces/functions/imports only. **`behavioral_summary`** gives imports + side-effect tags + exported API surface + test relationships + size band. **`reachable`** is not loaded — it's a discoverable follow-up.
- **`relevance_score` without a matching `load_mode: full`** on a top-ranked file usually means budget pressure. Check `budget_headroom` sub-signal.
- **`side_effects: ["io:network", "io:time", ...]`** are deterministic tags, not LLM inference. They come from a pinned side-effect table (version in `manifest.generation_metadata.side_effect_tables_version`).
- **Feasibility capped at 0.40** in the output means a blocking gap fired — the cap is by design. The raw score is in `sub_signals`; the cap is the policy layer.

---

## Anti-patterns

- **Don't lower `--min-feasibility` to get past a gate.** If a task isn't feasible at 0.65, it isn't feasible at 0.50. The agent will fail later and waste more tokens.
- **Don't strip `--fail-on-gaps` for "just this one run".** Blocking gaps exist because the downstream cost of ignoring them exceeds the upstream cost of fixing them.
- **Don't re-run `plan` repeatedly while tweaking flags looking for a better feasibility score.** The score is deterministic — flag changes that affect it also change the manifest hash, and you're gaming the gate. Fix the *task* or the *repo*, not the thresholds.
- **Don't treat `reachable` files as loaded.** They aren't. If the agent needs one, it has to request it — which requires either a follow-up load or an agent prompt that teaches it to ask.
- **Don't use `aperture run` on an untrusted `.aperture.yaml`.** The `agents.<n>.command` is shell — treat it like a Makefile you didn't write.
