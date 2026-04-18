# SPEC.md

## Project Title

Aperture v1.1

## One-Line Summary

Aperture v1.1 is a targeted delta release that closes the credibility
gaps identified by the external critique of v1.0: there was no way to
prove selection quality, the mention signal was trivially gameable,
polyglot repositories degraded to filename-only scoring, monorepos had
no scoping, and debugging "why did this plan differ" required reading
two manifests by hand.

---

## 1. Purpose

v1.0 shipped a deterministic, explainable, gated context planner. The
discipline is sound. What the v1.0 repository could not answer — and
what v1.1 exists to answer — was this set of fair questions from an
informed reviewer:

1. How do you know the selection weights are right?
2. What stops `s_mention` from dominating when a task casually names a
   file that isn't actually the answer?
3. What happens on a 60% TypeScript / 40% Go codebase?
4. In a monorepo with ten services, how does a task targeting one
   service avoid pulling signal from the other nine?
5. When `aperture plan` picked *this* file today and *that* file
   yesterday, what's the debugging path?

v1.1 adds the infrastructure to answer all five. The v1 pipeline
remains the authoritative normative contract; this document specifies
the delta.

v1.1 is **strictly additive.** Every v1 behavior, exit code, manifest
field, deterministic guarantee, and CLI flag documented in
`specs/initial/SPEC.md` remains in force. Nothing in this document
overrides v1 except where explicitly marked as a clarification or
bugfix of a known v1 defect.

---

## 2. Goals

### 2.1 Primary Goals

1. Ship an `aperture eval` harness that scores real selection quality
   against declared ground-truth fixtures, compares against a
   ripgrep-top-N baseline, and fails CI on regression.
2. Introduce a disagreement-aware dampener for `s_mention` so an
   incidental path mention or pasted stack trace cannot dominate the
   selection.
3. Add tier-2 language analysis (TypeScript, Python) via tree-sitter
   so polyglot repositories get real symbol-level scoring, not just
   filename matching.
4. Add an `--scope <path>` flag that narrows candidate generation,
   scoring, and gap detection to a subtree of the repository.
5. Ship `aperture diff` to compare two manifests and explain what
   changed, why, and at what cost.
6. Provide an empirical methodology for calibrating the
   `behavioral_summary` vs `full` boundary so downgrades provably
   preserve agent correctness on the fixture set.

### 2.2 Secondary Goals

1. Preserve every v1 determinism guarantee under the new features.
2. Preserve v1 cache compatibility for Go files; language tier
   additions must not invalidate the existing AST cache.
3. Keep the schema additive: every new manifest field is optional, so
   v1.0 consumers reading a v1.1 manifest still work.
4. Keep all new features local-first. No network access introduced.
5. Keep the rule-based-gap discipline: `aperture eval` measures
   quality but MUST NOT feed its results back into the selector at
   planning time.

### 2.3 Non-Goals

1. v1.1 will not add LLM-driven selection, gap detection, or
   summarization. v1's "no LLM in the pipeline" invariant holds.
2. v1.1 will not introduce semantic embeddings, vector search, or
   any network-dependent retrieval.
3. v1.1 will not add per-symbol budgeting (v1.2 at earliest).
4. v1.1 will not implement TypeScript/Python side-effect tagging.
   Tier-2 languages supply symbols and imports only.
5. v1.1 will not introduce IDE integration, plugins, or a persistent
   daemon.
6. v1.1 will not add a formal trust-gate for `.aperture.yaml`-
   declared custom adapters. That remains deferred to a later
   release (tracked in v1 SPEC §21).

---

## 3. Core User Stories

### 3.1 Selection-quality regression gate

> "I just changed the `s_symbol` weight from 0.20 to 0.18. Did I make
> every plan worse? I want a numeric answer, not a hunch."

A user runs `aperture eval run` against a committed fixture set and
gets precision, recall, F1, and a delta vs. the last committed
baseline. CI fails if any fixture regresses beyond a declared
tolerance.

### 3.2 Gameable-mention defence

> "My task says 'fix the regression in provider.go'. That file is
> now at score 0.91 — but it turns out the real fix is in
> `refresh.go` and `provider.go` just logs the error. The mention
> alone shouldn't dominate when nothing else agrees."

With the v1.1 dampener active, the `s_mention` contribution for
`provider.go` is clamped because `s_symbol`, `s_import`, and
`s_filename` all score it below 0.2 for this task.

### 3.3 Polyglot repository

> "This repo is half TypeScript, half Go. My task is 'add a
> rate-limit header to the GraphQL resolver.' The resolver is in
> `.ts`. v1.0 selected the Go HTTP middleware instead because only
> Go symbols scored."

With tier-2 TS support, `resolver.ts` is in the candidate set with
real symbol-level scoring; `ambiguous_ownership` fires correctly;
`missing_tests` checks for `.test.ts` / `.spec.ts`.

### 3.4 Monorepo scoping

> "My monorepo has 40 services under `services/`. My task targets
> `services/billing`. I want the billing service's `ambiguous_
> ownership` to care about billing only, and I want the token
> budget to be spent on billing files, not on 39 unrelated
> services."

`aperture plan TASK.md --scope services/billing` restricts scoring
to that subtree. The manifest records `scope: "services/billing"`,
which folds into `manifest_hash`, so a plan at the root and a plan
at `--scope services/billing` produce distinct hashes.

### 3.5 Plan-to-plan debugging

> "Yesterday's plan for the same task put `refresh_token.go` in
> `full`. Today it's `behavioral_summary`. Why?"

`aperture diff old.json new.json` surfaces the delta: different
`config_digest` (the user bumped `mention` weight), different
`repo.fingerprint` (one file mtime changed), so the cache missed
and the scoring order shifted. Output is both human-readable
Markdown and machine-consumable JSON.

### 3.6 Load-mode calibration evidence

> "When Aperture demotes a file from `full` to `behavioral_
> summary`, does the downstream agent still succeed?"

`aperture eval loadmode` runs the fixture set twice per fixture —
once with normal budgeting, once with every qualifying file forced
to `full` — and reports the delta in agent-fixture pass rates.
Threshold guidance is published with the tool; the thresholds
themselves stay in `specs/initial/SPEC.md §7.5.0` and may be tuned
based on eval output.

---

## 4. CLI Surface Additions

The v1 CLI surface is unchanged. v1.1 adds:

```
aperture eval
  run            Run the fixture harness; exit 2 on regression
  baseline       Pin current scores as the baseline for future runs
  loadmode       Calibrate behavioral_summary vs full boundary
  ripgrep        Compare against ripgrep-top-N baseline

aperture diff <manifest-A> <manifest-B>
                 Explain what changed between two manifests
```

And one flag on every planning command (`plan`, `explain`, `run`):

```
--scope <path>   Restrict candidate generation to files under <path>.
                 Relative to --repo. Trailing slash optional. Must
                 resolve inside the repository root.
```

### 4.1 `aperture eval run`

```
aperture eval run [--fixtures <path>] [--baseline <path>]
                  [--tolerance <float>] [--format json|markdown]
                  [--out <path>]
```

Defaults:

- `--fixtures` — `testdata/eval/` under `--repo`.
- `--baseline` — `testdata/eval/baseline.json`.
- `--tolerance` — `0.02` on F1 per fixture.
- `--format` — `markdown`.

Behavior:

1. Walk the fixtures directory for `*.eval.yaml` cases (schema in
   §7.1.1).
2. For each case, run the v1 planner against the embedded repo
   snapshot with the case's declared task, budget, and model.
3. Compute precision, recall, and F1 against the declared expected
   selection set.
4. Compare each metric against the baseline; if any fixture's F1
   drops by more than `--tolerance`, emit a regression block and
   exit 2.
5. Exit 0 otherwise and print a summary table.

Exit codes align with the v1 taxonomy (§7.2 of this document).

### 4.2 `aperture eval baseline`

```
aperture eval baseline [--fixtures <path>] [--out <path>] [--force]
```

Writes (or overwrites) `baseline.json` with the current run's
metrics. Intended to be invoked by a human reviewer after a
deliberate scoring change, never by CI.

Bootstrap and override semantics:

1. If no `baseline.json` exists at `--out`, the binary MUST write
   one unconditionally (exit 0) — this is the first-run bootstrap.
2. If `baseline.json` already exists and the current run shows no
   regressions against it (per §7.1.2 tolerance), the binary MUST
   overwrite it (exit 0).
3. If `baseline.json` already exists and the current run regresses,
   the binary MUST refuse to overwrite (exit 1) unless `--force`
   is passed. `--force` makes the overwrite unconditional and is
   the escape hatch for deliberate scoring changes that worsen
   some fixtures in service of improving the set as a whole.

### 4.3 `aperture eval loadmode`

```
aperture eval loadmode [--fixtures <path>]
                       [--format json|markdown] [--out <path>]
```

Runs each fixture case twice:

1. **Control:** normal planner output.
2. **Forced-full:** the same candidate set, but every candidate
   that was eligible for `full` before demotion is kept at `full`;
   budget overflow is tolerated and recorded.

Emits a per-fixture report showing which files were demoted, the
estimated tokens gained by forcing, and (if the fixture declares
an agent-check command — see §7.1.2) the pass/fail delta. Full
output schema and "symbolic differences" definition in §7.5.1.

### 4.4 `aperture eval ripgrep`

```
aperture eval ripgrep [--fixtures <path>] [--top-n <int>]
                      [--format json|markdown] [--out <path>]
```

For each fixture, runs a **deterministic rendering of the task's
anchors as a single ripgrep pattern** and selects up to `--top-n`
unique file hits, then fits them into the fixture's budget using
Aperture's own budgeter. Reports precision, recall, and F1 vs.
the same expected-selection ground truth. Default `--top-n` is
20.

**Anchor-to-pattern rendering** (normative, deterministic):

1. Take the fixture's `task.anchors` array from v1 task parsing.
2. Deduplicate case-insensitively; preserve first-seen order.
3. Regex-escape each anchor using Go's `regexp.QuoteMeta`.
4. Do NOT add word-boundary anchors. The v1.0 ripgrep baseline
   deliberately uses unbounded substring matching for every
   anchor — this is more *generous* to the ripgrep side than a
   word-bounded match would be, which makes "Aperture ≥ 1.2×
   ripgrep F1" a stricter bar to clear, not a looser one.
   Earlier drafts wrapped anchors in `\b`, but that approach
   failed on two common code-identifier shapes: anchors
   starting or ending with punctuation (e.g. `.js`) never match
   under `\b`, and anchors whose edge is an underscore (e.g.
   `foo_`) refuse to match against adjacent word characters
   (`foo_bar`). Dropping `\b` avoids both pathologies and keeps
   the baseline a single, obviously-defined operation.
5. Join with `|` into a single alternation.
   If the anchors array after dedup is empty, the baseline
   candidate set is **empty** and ripgrep MUST NOT be invoked.
   An empty pattern passed to ripgrep matches every line of
   every file; treating empty-anchors as "match everything"
   would produce a nonsense top-N selection. The fixture's
   precision and recall against expected selections still
   compute normally (precision = 1.0 on A=∅; recall = 0.0
   if E is non-empty, 1.0 if E=∅).
6. Invoke ripgrep with `--case-sensitive=false`, `--count-matches`
   (which emits `path:count` lines, one per matching file), the
   project's v1 exclusion patterns, and the rendered pattern.
   `--count-matches` is deliberately used in place of `--count`
   because `--count` returns matching *lines* — a line containing
   two anchors would be counted once, distorting the ranking
   when multiple anchors collide on the same line (stack traces,
   log lines). `--files-with-matches` MUST NOT be passed
   alongside `--count-matches`: the former suppresses the count
   column.
7. Parse the `path:count` output into (path, count) pairs and
   rank by `count` descending; break ties by normalized
   repo-relative path ascending.
8. Keep the top `--top-n` files as the candidate set.

**Budget fitting** (normative):

Ripgrep-baseline files are fit into the fixture's
`effective_context_budget` using the **same token estimator,
reserved-headroom logic, and greedy two-pass selector defined in
v1 §7.6.2** — with one restriction: every candidate is treated
as `load_mode=full` (ripgrep has no notion of summarization, so
the comparison would otherwise be unfair to Aperture). Files
that don't fit at `full` are dropped from the baseline selection.

This makes the F1 comparison apples-to-apples: the same tokens,
the same budget ceiling, the same fitting algorithm — only the
candidate-generation strategy differs.

This subcommand is a measurement tool; it does NOT change the
Aperture selector.

### 4.5 `aperture diff`

```
aperture diff <manifest-A> <manifest-B> [--format json|markdown]
              [--out <path>]
```

Both operands are JSON manifests (schema v1.0 or later). Output
sections:

- **Hash and ID.** `manifest_hash` equality, `manifest_id`
  equality (when the same manifest file is passed on both sides,
  they share their `manifest_id`) or inequality (the common
  case, two separate runs), `config_digest` equality. The diff
  tool does NOT assume `manifest_id` inequality; it reports
  whichever it observes.
- **Task delta.** Anchors added/removed (set difference on
  `task.anchors`); action-type change (string inequality on
  `task.type`); raw-text diff elided to first differing line
  (unified-diff-style, first line only).
- **Repo delta.** `fingerprint` equality, language-hint changes.
- **Budget delta.** Model, ceiling, effective budget, estimator.
- **Selection delta.** Added / removed / load-mode-changed files,
  each with score deltas and rationale changes.
- **Reachable delta.** Added / removed / promoted-to-selection.
- **Gap delta.** Added / resolved / severity-changed.
- **Feasibility delta.** Score delta and sub-signal deltas.
- **Generation-metadata delta.** `aperture_version`,
  `selection_logic_version`, config-digest (if digest changed,
  the diff MUST print the resolved config weights for both
  sides).

The command MUST NOT re-run the planner and MUST NOT open the
repository; it operates purely on the two manifest files.

Exit codes: 0 always (diffing is informational); 1 only on I/O
or parse errors.

---

## 5. Terminology (Delta from v1 §6)

### 5.1 Fixture Case

An `(task, repo-snapshot, expected-selection)` triple used by
`aperture eval`. Stored as `*.eval.yaml` in the fixtures directory
with an adjacent `repo/` subdirectory containing the snapshot.

### 5.2 Expected Selection

The set of repository-relative paths the fixture author asserts
should appear in the manifest's `selections` array for the plan to
be considered correct. Each entry may optionally pin a `load_mode`
(strict) or leave it unpinned (any mode counts).

### 5.3 Scope

An absolute-or-relative path under `--repo` that restricts the
candidate pool, scoring evidence, and gap-detection domain. A
scope of `""` or `"."` is equivalent to "the whole repo" (v1
behavior). A scope outside the repo root is an error (exit 4).

### 5.4 Language Tier

The analysis capability tier for a file's language. The numeric
tier and its descriptive name are one-to-one:

| Tier | Descriptive name | Capabilities | v1.1 members |
|------|------------------|--------------|--------------|
| 1 | `tier1_deep`       | symbols, imports, side-effect tags, test linking | `go` |
| 2 | `tier2_structural` | symbols, imports, test linking                   | `typescript`, `javascript`, `python` |
| 3 | `tier3_lexical`    | filename, doc tokens only                        | everything else |

The descriptive names are deliberately `tier*_*` prefixed to
avoid collision with the `full` / `structural_summary` /
`behavioral_summary` / `reachable` **load-mode** names defined
in v1 §6.6. A tier describes how much analysis Aperture does
to a file; a load mode describes how much of the analyzed
content is passed to the downstream agent. The two axes are
orthogonal — a Go (`tier1_deep`) file may be loaded in any of
the four load modes, and a Python (`tier2_structural`) file
may be loaded in any load mode its tier's signals support.

The internal `index.FileEntry.LanguageTier` field stores the
descriptive-name string. The `generation_metadata.language_tiers`
manifest field maps each language hint to its descriptive-name
string. "Tier N" and the descriptive name are interchangeable
throughout this document.

Tier-2 files contribute to `s_symbol`, `s_import`, and `s_package`
scoring; they do NOT contribute to side-effect-tag-driven rules
(`missing_runtime_path`, etc.) in v1.1.

### 5.5 Mention-Dampening Factor

A per-candidate scalar in `[0.3, 1.0]` applied to `s_mention`
before its weight is multiplied in. Defined in §7.4.2.

---

## 6. High-Level Behavior (Delta)

The v1 pipeline remains:

```
task → repo scan → candidate set → scoring → budgeting
     → gap detection → feasibility → manifest
```

v1.1 modifies three steps:

1. **Repo scan** MUST route `.ts`, `.tsx`, `.js`, `.jsx`, `.mjs`,
   `.cjs`, `.py` files through the tier-2 analyzer when available.
2. **Scoring** MUST apply the mention-dampening factor after the
   raw `s_mention` is computed and before the weighted sum.
3. **Candidate set** MUST be empty outside `--scope` when scope is
   set; walker exclusions still apply inside scope.

`aperture eval` is a separate command that drives the planner but
does not alter its logic. `aperture diff` does not invoke the
planner at all.

---

## 7. Functional Requirements

### 7.1 Eval Harness

#### 7.1.1 Fixture Schema

Each `*.eval.yaml` file:

```yaml
# Required fields.
name:      short-name                      # becomes the test-case ID

# Exactly one of `task` (inline text) or `task_file` (path under
# repo/) MUST be set; setting both or neither is a fixture error
# (exit 2). This removes the "is this a string or a path"
# ambiguity entirely — each field has exactly one meaning.
task:      |
  inline task text
# task_file: TASK.md                       # path relative to this
                                           # fixture's repo/ dir

budget:    120000                          # int, tokens (v1 §6.3 units)
model:     claude-sonnet-4-6               # string

# Repo is materialized from the adjacent repo/ directory. The
# fixture tool hashes the subdirectory and pins the expected
# fingerprint so accidental edits to the snapshot are caught.
#
# Fingerprint algorithm (normative, NOT v1 §6.4.1 — the build
# version is deliberately omitted so fixtures survive Aperture
# upgrades).
#
# Scope: only REGULAR FILES under repo/ contribute. Directories
# are not hashed (their presence is implied by contained file
# paths). Symlinks are NOT followed and are not hashed — a
# fixture containing symlinks is rejected with exit 2. Empty
# directories are invisible in the fingerprint (consistent with
# git's own behavior) and MUST NOT be relied upon.
#
# contentSHA256(file) := the lowercase hex-encoded SHA-256 of
# the file's raw byte stream, computed with the stdlib sha256
# Hash (equivalent to `shasum -a 256 <file>` on POSIX). No
# transformation of line endings or encoding.
#
# In Go-pseudocode:
#
#   h := sha256.New()
#   h.Write([]byte("fixture-fingerprint-v1"))
#   h.Write([]byte{0})
#   for each regular file under repo/, in ascending order of
#       its NFC-normalized, forward-slash, no-leading-"./"
#       repo-relative path:
#       h.Write([]byte(relPath))
#       h.Write([]byte{0})
#       h.Write([]byte(contentSHA256(file)))  # 64 hex chars
#       h.Write([]byte{0})
#   fingerprint := "sha256:" + hex(h.Sum(nil))
#
# Null-byte delimiters eliminate the ambiguity between
# concatenated paths and hashes. The leading schema literal
# "fixture-fingerprint-v1" lets the spec bump the algorithm
# without collision with pre-bump fixtures. The walker and
# exclusion rules for the fixture's repo/ subdirectory are
# exactly the v1 defaults (no user .aperture.yaml applies
# inside repo/).
repo_fingerprint: sha256:<64 hex>

expected:
  # Selections compared against the emitted manifest's
  # `selections[*]` array (v1 §11.1). Match key is `path`.
  selections:
    - path: internal/auth/refresh.go
      load_mode: full                      # optional; any mode ok if omitted
    - path: internal/auth/refresh_test.go
    - path: specs/auth/SPEC.md
  # Files that MUST NOT appear in the emitted manifest with
  # `selections[*].relevance_score >= 0.30`. (The eval harness
  # reads the same `selections[*].relevance_score` field defined
  # in v1 §11.1; no new manifest data is required.)
  forbidden:
    - path: vendor/github.com/foo/bar/**
  # Gaps compared against the emitted manifest's `gaps[]` array
  # (v1 §6.5 / §11.1). Match key is `type`; exact string match
  # against `gaps[*].type`.
  gaps:
    - type: missing_external_contract

# Forbidden-path glob uses the v1 walker-exclusion syntax
# (doublestar/gitignore-style): '**' matches any number of path
# segments; '*' matches any non-separator characters; patterns
# are evaluated against the normalized repo-relative path.

# Optional: command that evaluates agent behavior when this
# fixture is replayed through `aperture eval loadmode`. The
# command receives the full env-var set from v1 §7.10.4.1
# (APERTURE_MANIFEST_PATH, APERTURE_MANIFEST_MARKDOWN_PATH,
# APERTURE_PROMPT_PATH, APERTURE_TASK_PATH, APERTURE_REPO_ROOT,
# APERTURE_MANIFEST_HASH, APERTURE_VERSION) — same contract as
# `aperture run`. The script is expected to exec a downstream
# agent against the merged prompt at APERTURE_PROMPT_PATH and
# exit 0 on "agent completed the fixture task correctly" or
# non-zero otherwise.
#
# Pass/fail semantics (normative):
#   - exit 0                                → pass
#   - any non-zero exit (including exec-
#     failure exit codes like 127)          → fail
#   - timeout (wall-clock exceeded)         → fail; the harness
#     SIGKILLs the process and records fail
#   - command-not-found at invocation time
#     (the binary cannot be exec'd at all)  → eval run aborts,
#     exit 1 (distinct from a fixture fail;
#     a misconfigured harness is not a
#     legitimate fixture outcome)
agent_check:
  command: scripts/loadmode-check.sh
  timeout: 30s
```

All paths are repository-relative and forward-slash-separated
regardless of platform. The tool MUST reject fixtures where
`repo_fingerprint` disagrees with the computed fingerprint of
`repo/` (exit 2).

#### 7.1.2 Scoring

For a fixture with expected selections `E` and actual selections
`A` (the set of `selections[*].path` in the emitted manifest):

- **Precision:** `|E ∩ A| / |A|` (1.0 when `A = ∅`).
- **Recall:** `|E ∩ A| / |E|` (1.0 when `E = ∅`).
- **F1:** harmonic mean; 0 when either P or R is 0.

Load-mode pins, when present, count only as a tiebreaker: a
selection whose path matches but whose load mode disagrees
contributes 0.5 to intersection count instead of 1.0.

A file in the fixture's `forbidden` list that appears at score
≥ 0.30 in the actual run is a **hard failure** — the fixture
fails regardless of F1.

A gap in the fixture's `expected.gaps` list that does not appear
in the actual manifest is a **hard failure**.

#### 7.1.3 Baseline Format

`baseline.json`:

```json
{
  "schema_version": "1.0",
  "generated_at":   "2026-04-18T...Z",
  "aperture_version": "1.1.0",
  "selection_logic_version": "sel-v1",
  "fixtures": {
    "oauth-refresh":  { "precision": 0.92, "recall": 0.88, "f1": 0.90 },
    "polyglot-resolver": { "precision": 0.86, "recall": 0.83, "f1": 0.84 }
  }
}
```

A fixture name in `baseline.json` that does not exist in the
current run is an error (exit 2) **when the current run was
issued without filtering** (e.g. no explicit subset flag).
When a subset is explicitly selected (via a future
`--fixture <name>` flag, or by pointing `--fixtures` at a
subdirectory), baseline entries outside the current subset
are silently ignored. This preserves the invariant — "an
unintentionally orphaned baseline entry is an error" — while
still allowing targeted debugging runs.

A fixture name in the current run that is NOT in `baseline.json`
is a soft warning with exit 0.

### 7.2 Mention Dampener

#### 7.2.1 Problem Statement

`s_mention` at weight 0.25 is the single highest weight and
trivially triggered. Any task text containing a path or basename
boosts the matching file independent of all other signals. Pasted
stack traces, error messages, or conversational asides ("the bug
is NOT in provider.go") all drive false positives.

#### 7.2.2 Formula

The signals `s_mention`, `s_symbol`, `s_filename`, `s_import`,
and `s_package` are the `[0.0, 1.0]`-valued per-file relevance
factors computed by v1 §7.4.2 — Aperture v1.0's scoring
pipeline. v1.1 does NOT introduce new factors; the dampener
consumes exactly the v1 values and produces a modified
`s_mention` contribution.

For each candidate file `f`, let:

```
s_mention(f)    = v1 §7.4.2 value in [0.0, 1.0]
other_max(f)    = max(s_symbol(f), s_filename(f), s_import(f), s_package(f))
                  (exactly these four signals; v1.1 does not
                  extend this list, even if future v1.x releases
                  add new scoring factors — changing other_max's
                  composition requires a selection_logic_version
                  bump per §8.3)
dampener(f)     = min(1.0, floor + slope * other_max(f))
s_mention'(f)   = s_mention(f) * dampener(f)
```

`floor` and `slope` are resolved from the `scoring.mention_dampener`
config block (§7.2.3). The default values (`floor = 0.30`,
`slope = 0.70`) produce the specific ramp `dampener = min(1.0,
0.3 + 0.7 * other_max)` — zero to `floor`-clamped when nothing
else agrees with the mention, unity when at least one other
signal agrees fully. The formula is stated in terms of the
configurable names so no implementation hard-codes the 0.3/0.7
constants; a user who tunes the config changes the actual math.

The weighted sum then uses `s_mention'` in place of `s_mention`.

Rationale for the defaults:

- When `other_max = 1.0`, `dampener = 1.0` — zero change from v1.
- When `other_max = 0.0`, `dampener = floor (0.3)` — mention-only
  matches receive 30% of the v1 contribution.
- The linear ramp between 0.0 and 1.0 preserves monotonicity:
  increasing any other signal never decreases the total score.

#### 7.2.3 Configurability

The dampener is controlled by a new `.aperture.yaml` block:

```yaml
scoring:
  mention_dampener:
    enabled: true                # default true in v1.1
    floor:   0.30                # lower bound on the dampener
    slope:   0.70                # see note on defaults below
```

**Default resolution.** When the block is absent entirely,
`enabled=true`, `floor=0.30`, and `slope=0.70`. When the block
is present but partial, each field falls back to its default
independently: setting `floor: 0.40` while omitting `slope`
yields `slope=0.70` (NOT `slope = 1 - 0.40 = 0.60`). The
common-sense "slope defaults to complement the floor" rule is
deliberately rejected in favor of two independent, explicit
defaults — it makes config review unambiguous and allows
`floor > 0` with `slope < 1 - floor` (a flatter ramp that
never reaches 1.0) as a legitimate tuning choice.

**Timeout duration format.** The `agent_check.timeout` field
(§7.1.1) is a string parseable by Go's
[`time.ParseDuration`](https://pkg.go.dev/time#ParseDuration):
`"30s"`, `"1m30s"`, `"500ms"` all work. An integer value is
NOT accepted (parse error exit 2); this removes any ambiguity
about which unit a bare number means.

`enabled: false` restores v1.0 semantics exactly. The config
digest MUST include the resolved mention_dampener block so a
disabled-or-enabled change produces a distinct `manifest_hash`.

#### 7.2.4 Determinism

The dampener is a pure function of v1 scoring signals. It MUST
NOT introduce any ordering, I/O, or randomness.

### 7.3 Tier-2 Language Analysis

#### 7.3.1 Implementation Approach

Tier-2 analyzers run against a pinned tree-sitter grammar per
language. Grammars are vendored into the binary at build time —
no runtime download, no network fetch. The tool MUST ship with
identical grammar bytes on every platform.

Tree-sitter is selected because it:
- is deterministic (same input → same parse tree),
- handles partial/broken syntax gracefully (relevant for
  in-progress edits),
- has permissive licensing for the grammars Aperture needs
  (MIT/Apache for `tree-sitter-typescript`, `tree-sitter-javascript`,
  `tree-sitter-python`).

The Go tier-1 analyzer is NOT replaced; `go/parser` remains the
primary Go pipeline. Tier-2 is strictly for non-Go files.

#### 7.3.2 Symbol Extraction per Language

**Extension coverage and grammar selection.** The
TypeScript/JavaScript rules below apply uniformly to every
extension enumerated in §6.1's tier-2 list: `.ts`, `.tsx`,
`.js`, `.jsx`, `.mjs`, `.cjs`. The grammar bindings are:

| Extension | tree-sitter grammar binding |
|-----------|-----------------------------|
| `.ts`     | `tree-sitter-typescript` (the `typescript` parser) |
| `.tsx`    | `tree-sitter-typescript` (the `tsx` parser — distinct entry point in the same repo) |
| `.js`, `.mjs`, `.cjs` | `tree-sitter-javascript` |
| `.jsx`    | `tree-sitter-javascript` (the `jsx` variant — a distinct parser bundled in the same repo; NOT the plain `javascript` parser, which does not parse JSX tags) |
| `.py`     | `tree-sitter-python` |

The `tsx` and `jsx` parsers are separately-exposed entry
points in their respective repositories; a build that links
only the base `typescript` or `javascript` parser will fail
to parse files with JSX content and MUST surface that file
with `ParseError: true` rather than silently truncating
symbols. Python rules apply to `.py` only.

Tree-sitter always returns a parse tree, even for malformed
input. "Failed to parse" in v1.1 means: the root CST node's
`has_error()` method returns true OR the root node itself
has type `ERROR`. Either condition sets `ParseError: true`
and the symbol list for that file is empty (analogous to
Go's v1 behavior). The file still participates in
`s_mention`, `s_filename`, and `s_doc` scoring. Files that
parse with minor recoverable diagnostics but whose root is
not an ERROR MUST produce symbols normally; `ParseError`
tracks structural failure, not syntactic cleanliness.

**Scope of extraction.** Only **module-level** declarations are
extracted — those that are direct children of the grammar's
top-level node (`program` for TS/JS, `module` for Python).
Declarations nested inside function bodies, class bodies, or
`if`/`try` blocks are NOT recorded as symbols. This bounds the
symbol-table size deterministically and matches the semantics
of the Go tier-1 extractor.

**TypeScript / JavaScript (.ts, .tsx, .js, .jsx, .mjs, .cjs)**
extract as `index.Symbol` entries:

- Exported: `export function`, `export class`,
  `export const` (function-valued — see below),
  `export interface`, `export type`, `export default`.
- "**Function-valued const**" means: `export const NAME = RHS`
  where the RHS node type in the tree-sitter-typescript /
  tree-sitter-javascript CST is one of `arrow_function` or
  `function_expression`. (The `function_declaration` node
  type exists only at statement position and cannot appear
  as a variable-declarator initializer; it is deliberately
  excluded.) No type inference is performed; the RHS must be
  a direct function-expression node to qualify. Any other
  RHS — call expression, object literal, identifier,
  parenthesized expression, etc. — produces a symbol with
  kind `variable`, not `function`.
- **Anonymous `export default`** is recorded with the literal
  symbol name `"default"` and kind matching the RHS (`function`
  for `export default function() {}`, `class` for
  `export default class {}`, `variable` otherwise).
- Non-exported **module-level** declarations are recorded with
  `Exported: false`. Declarations nested below module level
  are skipped entirely.
- Kinds: `function`, `class`, `interface`, `type`, `variable`.
- Imports: every `import ... from "..."` specifier is recorded
  verbatim; relative specifiers are NOT resolved in v1.1.
  CommonJS `require("...")` calls are recorded as import
  specifiers for `.cjs` and `.js` files if and only if the
  `call_expression` node satisfies **all** of:
  (a) its callee is an `identifier` with text `require`;
  (b) its argument is a single `string` literal;
  (c) the closest ancestor statement node — either an
  `expression_statement`, a `variable_declarator`'s
  initializer, or a `lexical_declaration`'s initializer — is
  itself a direct child of the `program` node (i.e., the
  `require` call appears at module top level, wrapped in the
  usual `const foo = require("bar")` or bare
  `require("bar")` idiom).
  `require` calls whose containing statement is nested inside
  functions, conditionals, switch cases, or object literals
  are NOT recorded.

**Python (.py)** extracts:

- Module-level `def`, `async def`, `class`, and assignments
  whose target is a single `identifier` node at module scope.
- Names prefixed with `_` are flagged `Exported: false` (PEP 8
  convention). **All other module-level names are flagged
  `Exported: true`.**
- Kinds are assigned as follows:
  - `def` / `async def` → `function`
  - `class` → `class`
  - Module-level assignment `NAME = RHS` → `function` if
    the RHS node is a `lambda` expression (direct child
    — no parentheses, no conditional); otherwise `variable`.
- Imports: `import X` records the dotted module name `X`
  verbatim. `from X import a, b, c` records the module name
  `X` (once) — the specific names `a, b, c` are NOT separate
  imports in v1.1. Relative imports (`from . import foo`,
  `from ..util import bar`) record the literal dot-prefixed
  string (`"."`, `"..util"`, etc.); no resolution against
  the package layout is performed in v1.1. Imports inside
  functions or conditionals are NOT recorded.

#### 7.3.3 Test Linking

Extended rules for `index.LinkTests`:

- `*.test.ts`, `*.test.tsx`, `*.test.js`, `*.test.jsx` →
  sibling production file with the same basename stem.
- `*.spec.ts`, `*.spec.tsx`, `*.spec.js`, `*.spec.jsx` →
  sibling production file with the same basename stem.
- `test_<name>.py` or `<name>_test.py` → sibling
  `<name>.py` in the same directory.
- pytest `conftest.py` is marked as supplemental (always
  reachable in its package subtree) — analogous to Go's
  `testdata/`.

**Extension priority for ambiguous links.** Test-to-production
linking is constrained **within language families**, never
across them. The families are:

- **JS/TS family:** `.ts`, `.tsx`, `.js`, `.jsx`, `.mjs`, `.cjs`.
- **Python family:** `.py`.
- **Go family:** `.go` (handled by v1 tier-1 linking).

A JS/TS test file (`*.test.ts`, `*.spec.tsx`, etc.) links only
to JS/TS-family production siblings; a Python test
(`test_<name>.py`, `<name>_test.py`) links only to a `.py`
sibling. Cross-family matches (a `.ts` test "linking" to a
`.py` production file merely because the basename stems
agree) are explicitly forbidden.

When a JS/TS test has more than one same-family candidate in
the directory (e.g., `auth.test.ts` with both `auth.ts` and
`auth.tsx` present), the test is linked to the single highest-
priority candidate by this fixed intra-family order:

```
.tsx  >  .ts  >  .mjs  >  .cjs  >  .jsx  >  .js
```

Python tests have exactly one possible production sibling
(`.py`), so no intra-family tie-break is needed.

Ties are impossible (one extension per candidate). Candidates
whose extension is not in the test's own family are ignored.
This rule is deterministic and folds into the same cache key
as the rest of the tier-2 analysis.

#### 7.3.4 Cache Interaction

Tier-2 analysis MUST be cached under the same
`.aperture/cache/` directory as Go, keyed the same way: `sha256(
path, size, mtime, selection_logic_version)` where
`selection_logic_version` is the same `"sel-v1"` constant
defined in v1.0 (unchanged by the tier-2 feature) — UNTIL the
mention dampener §7.2 ships in a release, at which point the
constant bumps to `"sel-v2"` (see §8.3). The cache entry's
`Language` field distinguishes tiers. The v1 cache schema
version `cache-v2` is NOT bumped by this feature because existing
cache-v2 entries remain valid; v1.1 merely adds new entries for
previously-ignored files. A v1.0 binary reading a v1.1 cache
will see `language != "go"` entries and MUST IGNORE them (this
is the existing behavior: it only looked up paths it chose to
analyze).

#### 7.3.5 Performance Target

Tier-2 parsing MUST run in parallel with Go parsing (same
worker-pool machinery as v1 `goanalysis.Analyze`).

Performance is measured by the **v1 `make bench` harness
(§8.2) extended with a new `polyglot` fixture** — the
existing `testdata/bench/medium` fixture (5 000 Go files)
plus a generator-materialized 2 000-file TypeScript
companion under `testdata/bench/polyglot/`. The harness runs
a fixed task against the fixture, reports cold-plan and
warm-plan wall times, and gates on the ratio relative to the
Go-only `medium` baseline on the same machine in the same
run. The normative requirement is:

- `polyglot` cold-plan ≤ 1.20 × `medium` cold-plan.
- `polyglot` warm-plan ≤ 1.20 × `medium` warm-plan.
- `polyglot` warm-plan remains under the v1 `medium` 5 s warm
  target defined in v1 §8.2.

Gating on the within-run ratio rather than on absolute wall
time makes the target hardware-independent: the M-series
reference numbers from v1 §8.2 are indicative, not
normative, and CI runners simply compare the two fixtures
under identical load.

### 7.4 `--scope` Flag

#### 7.4.1 Semantics

`--scope <path>` is a **candidate-generation and scoring-domain
filter**. `<path>` is a single repo-relative directory path
(not a glob, not a list). v1.1 supports exactly one scope per
plan; multi-scope union is deferred to §14.

When set:

1. The repo walker still walks the whole repo (needed for
   `missing_spec`, `missing_external_contract`, and fingerprint
   computation against the full tree — see §7.4.3).
2. Any file NOT under `<path>` is excluded from the candidate
   set before scoring, **except supplemental files as defined
   in §7.4.2**, which are admitted under the restricted-scoring
   rules of that subsection.
3. `ambiguous_ownership` (v1 §7.7.3) considers only
   in-scope files as potential owners — its "top-scoring
   file's package has ≥2 peers over 0.60" check is evaluated
   against the scoped candidate set, not the repo-wide set.
   Aperture does not read OWNERS/CODEOWNERS files in v1.0 or
   v1.1; "ownership" here is strictly the scoring-based
   heuristic v1 already specifies.
4. `s_import`'s second pass (v1 §7.4.2) — "file imports packages
   that score highly on other factors" — considers only
   in-scope files when identifying high-scoring targets.
5. Tokenizer budgeting, load-mode assignment, and gap detection
   run against the scoped candidate set.

#### 7.4.2 Supplemental Files and Scope

Files matching v1 §7.1.3 supplemental patterns (SPEC.md,
AGENTS.md, README.md, top-level config) are considered
supplemental irrespective of scope and are eligible for
candidate generation even when outside scope. Rationale: a task
scoped to `services/billing` still needs the repo-root SPEC.md
if it references that service.

For a supplemental file located outside `--scope`, scoring is
restricted to **directory-independent signals only**:

| Factor | Out-of-scope supplemental contribution |
|--------|---------------------------------------|
| `s_mention`  | computed normally |
| `s_filename` | computed normally |
| `s_doc`      | computed normally |
| `s_symbol`   | forced to 0.0 |
| `s_import`   | forced to 0.0 |
| `s_package`  | forced to 0.0 |
| `s_test`     | forced to 0.0 |
| `s_config`   | computed normally |

The mention-dampener (§7.2) still applies, but `other_max`
is computed over the non-zero factors only. This keeps the
per-file weighted-sum formula unchanged — the zeroed factors
contribute nothing to the total — and avoids the undefined
question of what "package-adjacency" means for a file in a
different subtree.

A supplemental file outside scope that is selected appears in
the `selections` array with rationale containing
`"outside_scope_supplemental"` and its `score_breakdown` shows
the zeroed factors explicitly (so the manifest remains
auditable).

#### 7.4.3 Repo Fingerprint

`repo.fingerprint` MUST be computed over the ENTIRE repository
(not just the scoped subtree). Rationale: fingerprint answers
"did the repo change"; scope is a per-plan projection, not a
property of the repo. Identical scope against an identical
full-tree fingerprint is what makes the plan reproducible.

#### 7.4.4 Manifest Field

A new top-level manifest field:

```json
"scope": {
  "path": "services/billing"
}
```

**Path processing** happens in two strict phases: a
**transformation** phase that rewrites cosmetic variation
into canonical form, and a **validation** phase that rejects
paths that remain invalid after transformation.

Transformation (always succeeds, never aborts):

1. Replace backslashes with forward slashes.
2. Strip a single leading `./` if present.
3. Strip one trailing `/` if present.
4. Collapse any interior `/./` segment to `/` (so
   `services/./billing` → `services/billing`).

Validation (runs after transformation; any failure is exit 4):

- Path must not be empty (post-transformation).
- Path must not contain any `..` segment anywhere (including
  leading `..` — we reject upward traversal outright; there
  is no legitimate scope use for it).
- Path must not begin with `/`.
- Path must not contain any null bytes.
- Path (after transformation) must resolve inside the repo
  root (post-symlink-resolution) and must be a directory.

The `--scope ""` and `--scope .` sentinels defined in §7.4.5
are handled *before* this processing — they unset scope and
never enter either phase; they do NOT fail validation.

**Cross-platform case determinism.** A user typing `Services/
Billing` may get different outcomes depending on filesystem
case sensitivity:

- **Case-insensitive FS** (macOS APFS default, NTFS default):
  the walk succeeds against the on-disk directory named
  `services/billing`. To preserve byte-identical
  `manifest_hash` across platforms, the `scope.path` stored
  in the manifest MUST be the actual on-disk casing, obtained
  by reading the parent directory's entries and substituting
  the entry whose name case-insensitively matches the typed
  segment. This is done per-segment.
- **Case-sensitive FS** (Linux ext4, macOS APFS with explicit
  case-sensitivity enabled): validation's "must be a
  directory" check fails if the typed casing does not match
  an on-disk entry, and the plan exits 4. There is no case
  rewriting; the typed casing that succeeds validation IS the
  casing stored in the manifest.

In both cases, the casing stored in the manifest is the
actual on-disk casing. The determinism invariant holds across
a repo cloned to a case-sensitive and a case-insensitive host
as long as the host successfully validates the same
user-provided scope.

The absolute path is deliberately NOT included in the
manifest — absolute paths are host-dependent and would break
the byte-identical determinism invariant across machines.
Implementations compute the absolute path internally for
walker use but do not emit it.

Field is present only when `--scope` was set to a non-sentinel
value. Presence and contents MUST be included in the
`manifest_hash` input using the same canonicalization rule v1
§7.9.4 applies to the rest of the manifest: compact,
lexicographically key-sorted JSON. A plan at the repo root
and a plan scoped to a subtree produce distinct hashes.

When absent, v1.0 consumers see no new key.

#### 7.4.5 Config

`.aperture.yaml`:

```yaml
defaults:
  scope: services/billing     # single repo-relative path, §7.4.4 normalization
```

CLI `--scope` overrides config scope. The special CLI values
`--scope ""` and `--scope .` are **clear-scope sentinels**:
they explicitly unset any config-declared scope for this
invocation. The sentinels bypass §7.4.4 normalization (they
are not paths), do not produce a `scope` manifest field, and
do not trigger the exit-4 "dot segments" check. Any other
value goes through §7.4.4 normalization.

Validation rules for a non-sentinel config value are
identical to those for a non-sentinel CLI flag: the value
runs through the §7.4.4 **Transformation** phase (cosmetic
rewrites, never aborts) and then the §7.4.4 **Validation**
phase (all four checks apply). A failing config value
produces the same exit codes as a failing CLI flag.
Validation runs before planning, so an invalid config scope
fails fast regardless of which planning command was invoked.

**Symlinks:** a scope path that resolves via symlink to a
directory inside the repo root is accepted and treated as the
target directory for walking. A scope path that is itself a
symlink whose target is outside the repo root is rejected
(exit 4) — the invariant is that the resolved walk stays
within the repo, not that the input is a literal subpath.

**Permissions:** if the scope path exists and is a directory
but is not readable by the current process, the walker's
existing v1 permission-denied handling applies (recorded as an
exclusion with reason `permission_denied`); this is not a
separate exit code.

#### 7.4.6 Exit Codes

- `--scope` path (or `defaults.scope`) outside the repo: exit
  4 (invalid repo input).
- `--scope` path inside the repo but not a directory: exit 4.
- `--scope` path fails §7.4.4 normalization (e.g., contains
  `..`): exit 4.
- Scope results in zero candidates after walker exclusions
  AND no supplemental files are admissible: exit 9 (budget
  underflow — consistent with "nothing to plan for"). If any
  supplemental file remains admissible under §7.4.2, the plan
  proceeds normally and exit 9 is NOT triggered.

### 7.5 Load-Mode Calibration

#### 7.5.1 Methodology

`aperture eval loadmode` does not change the planner; it
measures the cost of v1 §7.5's demotion rules empirically. For
each fixture:

1. Run planner normally → `Plan_A`.
2. Run planner with a **no-demotion shim:** every file eligible
   for `full` is kept at `full`, budget overflow is tolerated,
   and the overflow size is recorded. → `Plan_B`.
3. **Symbolic differences are always reported** — see the
   definition below. If the fixture additionally declares
   `agent_check`, execute that command twice (once per plan's
   merged prompt, per §7.1.1's env-var contract) and record
   pass/fail for each, layered on top of the symbolic report.
   When `agent_check` is absent, the report contains symbolic
   differences only.

**Symbolic differences** are the structural manifest deltas
computable without a downstream agent:

- The set of paths demoted from `full` in `Plan_A` but held at
  `full` in `Plan_B`, listed with both their v1 scores and
  their sizes.
- The delta in `estimated_selected_tokens` between `Plan_A`
  and `Plan_B`.
- The delta in the `feasibility.score` and its sub-signals.
- The set of gaps that fired in `Plan_A` but not `Plan_B` (or
  vice versa) as a consequence of the load-mode forcing.
- Whether the forced-full `Plan_B` would have triggered the
  v1 §7.6.5 budget-underflow condition (the planner-level
  definition: no candidate at the highest priority fits
  within the effective context budget). Recorded as a boolean
  `forced_full_would_underflow`, never raised as an actual
  error — `eval loadmode` completes the report regardless.
  Exit-code mapping for this condition in normal planning
  runs is addressed in §7.7; here it is a data point, not an
  abort.

Symbolic differences are emitted as structured data in the
JSON output and as a bulleted section in the Markdown output.
They are always reported, whether or not `agent_check` is
declared; `agent_check` adds the pass/fail lines on top.

Report fields per fixture:

- Files demoted in `Plan_A` but held at `full` in `Plan_B`.
- Budget overflow tokens in `Plan_B` (how wrong the budget was
  for forcing `full`).
- Agent-check pass/fail for A, B, and the delta (only when
  `agent_check` is declared). The delta is a string enum with
  exactly four values:
  - `"IMPROVEMENT"` — Plan_A failed, Plan_B passed.
  - `"REGRESSION"`  — Plan_A passed, Plan_B failed.
  - `"NO_CHANGE_PASS"` — both plans passed.
  - `"NO_CHANGE_FAIL"` — both plans failed.
- Recommended `§7.5.0` threshold change, if any, computed by
  the following rule (advisory only; never auto-applied):
  **if the agent-check pass rate for `Plan_A` is ≥ 10
  percentage points below `Plan_B` averaged across all
  fixtures that declare `agent_check`, emit a recommendation
  to raise the `avg_size_kb` threshold by 25% (rounded to
  the nearest integer KB).** Absent `agent_check` declarations
  across all fixtures, no recommendation is emitted. The rule
  is the simplest possible monotonic advisor; it is
  deliberately conservative.

#### 7.5.2 Threshold Tuning Rule

The advisory rule in §7.5.1 is the only threshold-tuning rule
in v1.1: the **aggregate** pass-rate delta across all fixtures
that declare `agent_check`, compared point-wise. No per-file
or per-fixture variant exists — the aggregate is the only
signal. Any recommendation produced is advisory; changing the
`behavioral_summary` eligibility threshold (v1 §7.5.0
`avg_size_kb ≥ 16`) is a release-gate decision that requires
human review and a reviewer-issued `aperture eval baseline` to
reset the selection baseline.

### 7.6 `aperture diff`

#### 7.6.1 Determinism

`aperture diff A.json B.json` must produce byte-identical
output when run against the same two manifests. Diff ordering
is lexicographic on path; sections are emitted in the fixed
order of §4.5.

#### 7.6.2 Required Output Sections

All sections in §4.5 are REQUIRED in both JSON and Markdown
output formats. Empty sections are emitted with an "unchanged"
marker rather than omitted, so a consumer can tell "unchanged"
apart from "not checked."

#### 7.6.3 Semantic Equivalence Check

The first line of the Markdown output and the top-level
`semantic_equivalent` field of the JSON output MUST state
whether `manifest_hash(A) == manifest_hash(B)`. When equal,
everything else in the diff MUST be empty EXCEPT the
following fields, which are **per-run** and always permitted
to differ:

- `manifest_id`
- `generated_at`
- `generation_metadata.host`
- `generation_metadata.pid`
- `generation_metadata.wall_clock_started_at`
- `aperture_version`

These are exactly the fields v1 §7.9.4 excludes from the
manifest-hash computation — equal-hash by definition means
everything *except* these fields is byte-identical. Any
non-empty delta in any other field when hashes are equal is
a tool-level bug, not a user error, and the diff tool MUST
emit a clear "hash agreement + content disagreement"
diagnostic in that case.

**agent_check success criterion** (referenced here for E
slice readers): a pass is exit 0, anything else is a fail.
This is stated normatively in §7.1.1; the diff tool does not
re-invoke agent_check, but load-mode reports it reads must
use the same criterion.

---

## 7.7 Error Handling (Delta from v1 §16)

v1.1 reuses the v1 exit-code taxonomy. No new exit codes are
introduced.

**Exit 9 (budget underflow) clarification.** v1 §7.6.5 defines
exit 9 as "the effective context budget runs out of headroom
before one real selection can fit." v1.1's `--scope`-leaves-
zero-planable-candidates condition is not a separate failure
mode — it IS that same condition, reached a slightly earlier
way: a scoped candidate set of size 0 trivially cannot fit
any selection, because there are no selections to fit. Exit 9
therefore continues to mean exactly one thing (nothing can be
planned), and the §7.7 table row "scope leaves zero
candidates AND no admissible supplementals" is the v1.1
trigger for that same meaning.

New error conditions map as follows:

| Condition | Exit |
|-----------|------|
| `aperture eval run` regression vs. baseline | 2 |
| `aperture eval run` missing baseline fixture (unfiltered run) | 2 |
| `aperture eval baseline` refused (regression, no `--force`) | 1 |
| `aperture eval baseline` bootstrap (no baseline exists) | 0 |
| `aperture eval loadmode` fixture fingerprint mismatch | 2 |
| `aperture eval loadmode` `agent_check` command not found | 1 |
| `aperture eval loadmode` `agent_check` timeout (duration declared in the fixture's `agent_check.timeout` per §7.1.1; no global default) | 0 (recorded as fixture fail; eval continues) |
| `aperture eval ripgrep` `rg` binary not on PATH | 1 |
| `aperture eval ripgrep` ripgrep returns non-zero (e.g., malformed pattern) | 1 |
| `aperture diff` parse error on either manifest | 1 |
| `aperture diff` unsupported schema version (either manifest has `schema_version < "1.0"`) | 1 |
| Fixture `repo_fingerprint` mismatch | 2 |
| `--scope` path (CLI or config) outside repo | 4 |
| `--scope` path resolves to a file | 4 |
| `--scope` path fails §7.4.4 normalization | 4 |
| `--scope` leaves zero in-scope candidates AND no supplemental file (per §7.4.2's definition) survives scope filtering as an admissible candidate | 9 |
| Tier-2 grammar load failure (build corruption) | 1 |

---

## 8. Non-Functional Requirements

### 8.1 Determinism

All v1 determinism guarantees (§8.3) hold unchanged. v1.1 adds:

- The mention dampener is a pure function of v1 scoring
  signals.
- Tier-2 tree-sitter parses are deterministic per grammar
  build. The tool MUST pin grammar versions and commit the
  pinned revision in the repository's build metadata.
- `aperture eval run` against an identical fixture set and
  identical binary MUST produce byte-identical **semantic
  content** in its reports. As with the v1 manifest hash
  (§7.9.4), a small set of per-run fields is exempted from
  the byte-identity requirement: `generated_at` (ISO 8601
  timestamp), `wall_clock_duration_ms` (integer milliseconds
  for the overall run and for each fixture), `host`, and
  `pid`. All other fields — verdicts, fixture order, per-
  fixture P/R/F1 to the declared precision, regression
  summaries, baseline-comparison results — MUST be byte-
  identical across identical runs. The eval harness MUST
  document which fields are per-run and MUST place them in a
  dedicated section of the report so consumers can strip
  them with a simple filter when diffing.
- `aperture diff` against identical inputs MUST produce
  byte-identical output.

### 8.2 Performance

v1 performance targets hold. Additional v1.1 targets, all
measured via the v1 `make bench` harness extended with the
fixtures defined in this document:

- `polyglot` cold- and warm-plan ratios per §7.3.5.
- `aperture eval run` over the committed fixture set
  completes in **≤ 1.5 × the sum of per-fixture cold-plan
  wall times**, measured in the same bench run. Anything
  inside the 1.5× headroom is harness overhead (fixture YAML
  parse, scoring math, baseline compare, report formatting).
  Ratio-against-sum keeps the target machine-independent and
  scales linearly with the fixture count.
- `aperture diff` is I/O- and parse-bound. It MUST complete
  in **≤ 2 × the time required to JSON-unmarshal both input
  manifests** on the same machine (measured via a bench
  micro-benchmark that reports the unmarshal time as the
  denominator).

Absolute wall-time expectations on an Apple M-series machine
are published in the v1 `make bench` output for context, but
are not normative; CI gates on the ratios above.

### 8.3 Backward Compatibility

- The v1.0 manifest schema (`schema_version: "1.0"`) is
  preserved; v1.1 adds only optional fields (`scope`, language
  tier annotations in `generation_metadata`).
- v1.0 consumers reading a v1.1 manifest MUST NOT break.
  Unknown keys MUST be ignorable by any schema-conformant
  consumer (v1 §7.9.2 already requires this).
- `selection_logic_version` remains `"sel-v1"` UNTIL the
  mention dampener ships. On dampener activation, bump to
  `"sel-v2"` — the dampener changes scoring decisions, and
  the cache key MUST invalidate accordingly.
- Config shape: the `scoring.mention_dampener` block is
  optional. A v1.0-style config with no dampener block MUST
  resolve with dampener defaults from §7.2.3.

### 8.4 Observability

`aperture plan --verbose` (v1 behavior preserved) MUST
additionally log:

- The resolved scope (or "none") and the candidate count
  before / after scope filtering.
- Per-candidate dampener factor when it is below 1.0 and
  the candidate was selected OR reachable.
- Tier-2 parse statistics (files parsed per language, parse
  errors per language) alongside the existing Go stats.

---

## 9. Configuration

The v1.0 `.aperture.yaml` schema is preserved. v1.1 adds two
optional blocks:

```yaml
defaults:
  scope: services/billing        # any path under repo, forward-slash

scoring:
  mention_dampener:
    enabled: true
    floor:   0.30
    slope:   0.70

languages:
  typescript:
    enabled: true                # default true in v1.1
  javascript:
    enabled: true
  python:
    enabled: true
```

`languages.<name>.enabled: false` MUST suppress tier-2 analysis
for that language and fall the language back to tier 3
(`tier3_lexical`). This is how a user with a broken grammar or
a security requirement can opt out.

Unknown keys are rejected per v1 §9 (exit 5).

---

## 10. Data Model (Delta)

### 10.1 Manifest

Additive fields (all optional):

```jsonc
{
  "scope": {                          // §7.4.4
    "path": "services/billing"        // repo-relative, normalized
  },

  "selections": [
    {
      // all v1 fields unchanged
      "score_breakdown": [
        {
          "factor":       "mention",
          "signal":       1.0,
          "weight":       0.25,
          "contribution": 0.075,        // signal * dampener * weight
          "dampener":     0.30          // NEW: float in [floor, 1.0];
                                        // 1.0 when the factor is NOT
                                        // "mention" OR when the
                                        // dampener is disabled
        },
        ...
      ]
    }
  ],

  "generation_metadata": {
    // all v1 fields unchanged
    "language_tiers": {                 // NEW
      "go":         "tier1_deep",
      "typescript": "tier2_structural",
      "python":     "tier2_structural"
    },
    "grammars": {                       // NEW
      "tree_sitter_typescript": "v0.x.y",
      "tree_sitter_python":     "v0.x.y"
    }
  }
}
```

All new fields are part of the `manifest_hash` input. A plan
with `--scope` set produces a distinct hash from one without;
a plan with tier-2 languages enabled produces a distinct hash
from one with them disabled.

### 10.2 FileEntry (Internal)

`index.FileEntry` gains:

- `LanguageTier: string` — `"tier1_deep" | "tier2_structural" | "tier3_lexical"`.

Populated by the repo scanner based on the resolved language
and config. Visible through `structural_summary` emission and
cache serialization. Cache entries written by v1.0 lack this
field and MUST be treated as `LanguageTier="tier1_deep"` for
Go and `"tier3_lexical"` otherwise when read by v1.1 —
matching v1.0's behavior exactly under the v1.1 enum names.

---

## 11. Test Requirements

v1 test requirements (§18) remain in force. v1.1 adds:

Terms referenced below:

- `signal` = the raw factor value in `[0.0, 1.0]` computed
  per v1 §7.4.2 for a given factor of a given file.
- `other_max` = as defined in §7.2.2: the maximum over the
  `s_symbol`, `s_filename`, `s_import`, and `s_package`
  factors of the same file.
- `dampener` = the per-candidate scalar from §7.2.2,
  `min(1.0, floor + slope * other_max)`.
- `contribution` = `signal * dampener * weight`.

### 11.1 Unit Tests

- Mention dampener formula: known (signal, other_max) inputs
  map to known contributions. Include boundary cases: all
  zeros, all ones, and the `enabled: false` pass-through.
- Tier-2 symbol extraction: deterministic golden output for a
  pinned snippet per language (TS, JS, Python).
- Scope filter: known candidate set + known scope → known
  survivor set.
- Supplemental-file scoping: a scope-internal task still sees
  the repo-root SPEC.md.

### 11.2 Golden Tests

- Fixture-harness scoring reports for a trivial known-good
  fixture and a trivial known-bad fixture.
- `aperture diff` markdown and JSON output for three manifest
  pairs: identical hashes, config-digest difference,
  selection-set difference.
- Per-language tier-2 symbol tables for `typescript.ts.golden`,
  `python.py.golden`.

### 11.3 Determinism Tests

- 20-run byte-identity of `aperture eval run` over the whole
  fixture set on identical inputs.
- 20-run byte-identity of `aperture diff` over two fixed
  manifests.
- 20-run byte-identity of scope-restricted plans.

### 11.4 Integration Tests

- End-to-end plan of a polyglot repo (Go + TypeScript +
  Python) with dampener enabled, scope set to a subtree, and
  one file in the tier-3 fallback.
- `aperture eval run` against a regression baseline: pass,
  then a deliberate weight bump, then a fail, then a
  reviewer-issued `baseline` reset.
- `aperture diff` reports hash equality for two runs of the
  same plan on identical inputs, and non-empty deltas after
  a config change.

### 11.5 Property Tests

- Dampener monotonicity: for any candidate, increasing any
  `other_max` component never decreases total score.
- Scope projection: `plan --scope A` on a repo is
  observationally identical (per manifest hash) to
  `plan --repo A_as_standalone_repo`, ignoring supplemental
  files and fingerprint.

### 11.6 Fuzz Tests

- Tree-sitter TypeScript harness on random bytes:
  no panics, no non-deterministic parse, bounded memory.
- Fixture YAML loader on malformed YAML: structured error
  exits (exit 2), never a panic.

---

## 12. Acceptance Criteria

v1.1 ships when ALL of the following hold:

1. `aperture eval run` passes against a committed fixture set of
   at least 10 cases covering: pure Go, polyglot (Go+TS),
   polyglot (Go+Python), monorepo subtree, small-repo
   single-file task, large-repo budget-pressure task.
2. `aperture eval ripgrep` (defined in §4.4, including the
   normative anchor-to-pattern rendering, budget-fitting rule,
   and F1 formula from §7.1.2) shows Aperture v1.1's F1 ≥ 1.2×
   the ripgrep-baseline F1 on every fixture in the committed
   set, with `--top-n 20` as the normative comparison point.
3. The mention-dampener behavior is covered by at least one
   fixture whose v1.0 plan is wrong (mention dominates
   incorrectly) and whose v1.1 plan is correct (dampener
   suppresses the false positive).
4. The polyglot-resolver fixture (per §3.3) is correctly
   handled: `resolver.ts` appears at score ≥ 0.70 and
   `missing_tests` considers `.test.ts`.
5. `aperture plan --scope <subtree>` on the monorepo fixture
   produces a manifest with `scope` set, distinct hash from
   the root-scoped plan, and `ambiguous_ownership` resolution
   confined to the subtree.
6. `aperture diff` reports empty non-metadata deltas when
   comparing two back-to-back plans of the same fixture.
7. `selection_logic_version` is bumped to `"sel-v2"` in the
   commit that enables the dampener by default; the v1.1 cache
   schema version remains `cache-v2` unchanged.
8. All v1 acceptance criteria (§19) remain satisfied.
9. `go test ./...`, `golangci-lint run ./...`, and
   `go test -race ./...` pass.
10. The `make bench` harness reports the §7.3.5 and §8.2
    performance ratios within target on the developer
    machine used to cut the release.
11. The determinism tests in §11.3 (20-run byte-identity for
    `aperture eval run`, `aperture diff`, and scope-restricted
    plans) all pass.

External development-pipeline gates (`prism`, `realitycheck`,
`clarion`) are recommended project practice documented in
CLAUDE.md but are not part of this specification's normative
acceptance criteria — their presence or absence does not
block a v1.1 release against this contract.

---

## 13. Suggested Implementation Phases

### Phase 1: Eval Harness Skeleton

- Fixture YAML schema + loader.
- `aperture eval run` scoring math against a committed baseline.
- `aperture eval baseline` writer.
- One trivial pass-fixture and one trivial fail-fixture.
- `aperture eval ripgrep` baseline comparator.

Artifact: CI can now measure selection quality. No change to
planner.

### Phase 2: Mention Dampener

- Formula + config block.
- `score_breakdown.dampener` field.
- `selection_logic_version` bumped to `"sel-v2"`.
- New fixture demonstrating the false-positive and its fix.

Artifact: `aperture eval run` shows a measured quality lift on
a target fixture.

### Phase 3: `--scope` Flag

- Walker / candidate filter.
- Supplemental-file exemption.
- `scope` manifest field + hash folding.
- Config key.
- Monorepo fixture.

Artifact: monorepo fixture now plans correctly with bounded
competition.

### Phase 4: Tier-2 Language Analysis

- Tree-sitter vendor + build wiring.
- TypeScript, JavaScript, Python symbol extraction.
- Test-linking rules.
- Polyglot fixture.
- Cache integration.

Artifact: polyglot fixture shows non-Go files scoring on symbols
and imports.

### Phase 5: `aperture diff`

- Manifest parser.
- Section emitters (markdown + JSON).
- Hash-equality fast-path.

Artifact: plan-to-plan debugging is a single command away.

### Phase 6: Load-Mode Calibration

- `aperture eval loadmode` implementation.
- Agent-check runner (stub acceptable for v1.1; real runner
  wired through `aperture run` adapter).
- Threshold-tuning advisory report.

Artifact: first empirical evidence on whether the v1 §7.5.0
thresholds are right.

### Phase 7: Hardening and Release

- Full fixture set ≥ 10 cases (§12.1).
- Determinism and performance targets re-verified.
- Documentation: README section on eval, scope, tier-2
  languages, and diff.
- Clarion docs refresh.
- Tag `v1.1.0`.

---

## 14. Future Enhancements (Not Required for v1.1)

- Per-symbol budgeting (deferred to v1.2 earliest).
- Side-effect tagging for TS/Python (requires a different
  model than Go's import-heuristic; research first).
- Fixture authoring UI — today users write YAML by hand; a
  `aperture eval capture` subcommand could record a plan as
  an expected-selection fixture.
- Multi-scope plans: `--scope A --scope B` as a union of
  subtrees. v1.1 accepts only a single scope.
- Formal trust-gate for `.aperture.yaml` custom adapters.
- `aperture inspect` interactive manifest exploration
  (complements `aperture diff`).
- Coverage-aware scoring: a file whose tests fail under the
  declared test selector gets scored higher than one whose
  tests pass unchanged.

---

## 15. Final Notes for the Coding Agent

v1.1 is measurement and honesty, not expansion. The temptation
will be to jam more features in because the eval harness makes
it easy to prove a feature "helps." Resist that.

- The eval harness is a gate. Don't game it. Don't add weights
  tuned to a single fixture. Every change to scoring MUST
  include a fixture that exhibits the behavior being fixed
  AND a counter-fixture showing the behavior doesn't degrade
  the opposite case.

- The dampener is a clamp, not a redistributor. It takes
  weight away from `s_mention` in specific conditions. It does
  NOT increase other weights to compensate. If the resulting
  top candidate scores lower than v1.0, that is the correct
  outcome — v1.0 was overconfident.

- Tier-2 language support is a capability, not a promise.
  Tier-2 analyzers may miss constructs a mature compiler would
  see. That is acceptable as long as it is deterministic,
  documented, and visible in `generation_metadata.language_tiers`.

- Scope is a projection, not a sub-repo. The fingerprint still
  covers the full tree; supplemental files still resolve at
  the repo root. Users reach for `--scope` to reduce noise,
  not to pretend a subtree is a standalone repository.

- `aperture diff` is inspection, not intervention. It does not
  re-plan, does not re-hash, does not modify anything. If a
  reviewer wants to debug a plan change, they read the diff and
  act on it manually.

If forced to choose between "clever" and "predictable,"
continue to choose predictable.
