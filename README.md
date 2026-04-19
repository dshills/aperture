# Aperture

**Deterministic context planning for coding agents.**

Aperture runs *before* your coding agent. Given a task description and a
repository, it picks the files the agent should load — in full, as a
structural summary, as a behavioral summary, or merely as "reachable" — fits
that selection into a token budget, flags missing information, scores how
feasible the task looks, and emits a reproducible, hashed manifest the
agent can consume.

It is not a coding agent. It is not a RAG system. It is a **compiler
front-end for context selection**: deterministic, explainable, cacheable,
and gated.

---

## Why this is useful

Coding agents fail in three predictable ways:

1. **Too much context.** They get the whole repository dumped in, waste
   tokens on `vendor/`, `node_modules/`, and machine-generated files, and
   lose the plot.
2. **Too little context.** They get a single file, miss the callers, miss
   the config, and hallucinate interfaces that don't exist.
3. **Wrong context.** They get something that looks relevant — same
   directory, same verb in the name — but the actual target lives two
   packages over.

Aperture fixes all three in the same way: by reading the task carefully,
scoring every candidate file against a deterministic weighted formula,
assigning each candidate a load mode, and fitting the whole thing into an
explicit token budget — and then writing down exactly what it did and why.

Concretely, Aperture gives you:

- **Reproducibility.** Identical task + identical repo + identical config
  produces a byte-identical `manifest_hash`. The manifest FILE itself
  still carries per-run fields (`generated_at`, `host`, `pid`,
  `wall_clock_started_at`) for operator diagnostics, but those are
  excluded from the hash input — the planning decisions are stable even
  when the surrounding metadata isn't.
- **Explainability.** Every selection, every load mode, every exclusion,
  every gap carries rationale metadata. `aperture explain` renders the
  whole decision tree as plain text.
- **Rule-based gap detection.** Nine categories (missing spec, missing
  tests, missing config, unresolved symbol, ambiguous ownership,
  missing runtime path, missing external contract, oversized context,
  task underspecified) — all rule-based, none LLM-driven.
- **Feasibility scoring.** A 0.0–1.0 score with numeric sub-signals
  (coverage, anchor resolution, task specificity, budget headroom, gap
  penalty), capped at 0.40 when any blocking gap fires.
- **Threshold gates.** Fail a plan with exit 7 when feasibility is below
  `--min-feasibility`, exit 8 when a blocking gap fires with
  `--fail-on-gaps`, exit 9 on budget underflow. Downstream CI can cut a
  run before an agent burns tokens on an unwinnable task.
- **Tokenizer-aware budgeting.** Counts real tokens for the target
  model: `cl100k_base` / `o200k_base` / `p50k_base` / `r50k_base` for
  OpenAI families (embedded, no network), conservative `ceil(len/3.5)`
  for Claude and unrecognized models.
- **Warm-cache speed.** A persistent AST cache under `.aperture/cache/`
  means a second plan on the same tree runs in ~30 ms on 500 files,
  ~320 ms on 5 000 files.
- **Agent integration.** Plan → write a merged markdown manifest +
  task file → invoke `claude` or `codex` with the prompt piped on
  stdin. User-declared custom adapters in `.aperture.yaml` work the
  same way.

If forced to choose between "clever" and "predictable," Aperture chooses
predictable.

---

## Installation

### Prerequisites

- Go 1.23 or newer.
- A POSIX shell for `make` targets (WSL or Git Bash on Windows).
- **Optional**, only if you invoke `aperture run <name>`: the adapter
  binary for `<name>` must already be on `$PATH`. Aperture does not
  install or bundle `claude` / `codex` / any user-declared adapter — it
  only orchestrates them. For the built-in adapters, see the
  [Claude Code CLI](https://github.com/anthropics/claude-code) and
  the OpenAI Codex CLI respectively; `aperture plan` and
  `aperture explain` need none of these.

### From source

```bash
git clone https://github.com/dshills/aperture.git
cd aperture
make build                         # builds bin/aperture (version-stamped)
```

Or install directly:

```bash
# Go default ($GOBIN or $GOPATH/bin):
make install

# Explicit destination:
make install INSTALL_DIR=/usr/local/bin
make install INSTALL_DIR=~/.local/bin   # ~ expands via /bin/sh
```

`aperture version` prints the semver, git commit, and build timestamp.

### Via `go install`

```bash
go install github.com/dshills/aperture/cmd/aperture@latest
```

(No ldflag stamping this way — the binary reports `dev` / `unknown` /
`unknown`. Use `make install` for a stamped build.)

---

## Quick start

Given any Go repository:

```bash
# 1. Write the task as a file or pass it inline.
cat > TASK.md <<'EOF'
# Add OAuth refresh

Add OAuth refresh handling to the GitHub provider in
internal/oauth/provider.go. Include unit tests and update the
README.
EOF

# 2. Plan. Emits a JSON manifest on stdout.
aperture plan TASK.md --model claude-sonnet --budget 120000

# 3. Read it.
aperture plan TASK.md --format markdown --out .aperture/plan.md

# 4. Explain it.
aperture explain TASK.md

# 5. Run an agent with it.
aperture run claude TASK.md --fail-on-gaps --min-feasibility 0.65
```

The run command:

1. Walks the repo (caching AST analysis under `.aperture/cache/`).
2. Scores every file against the task.
3. Assigns each file a load mode (`full`, `structural_summary`,
   `behavioral_summary`, `reachable`).
4. Detects nine classes of gaps.
5. Emits the JSON manifest + the Markdown manifest + a merged
   `run-<id>.md` prompt under `.aperture/manifests/`.
6. Invokes `claude --print --permission-mode bypassPermissions` with
   the merged prompt piped on stdin.

---

## Core concepts

> Section anchors like §7.4.2.1 or §7.7.3 point at the normative
> specification at [`specs/initial/SPEC.md`](specs/initial/SPEC.md).
> The README summarizes what those sections say; the SPEC is the
> source of truth for every behavior Aperture must preserve.


### Load modes

Every selected file carries one load mode:

| Mode | Meaning |
|---|---|
| `full` | Raw content should be loaded verbatim. Used for highly relevant, reasonably-sized, central files. |
| `structural_summary` | Package / types / interfaces / functions / imports. For Go files where architecture matters more than exact content. |
| `behavioral_summary` | Imports + side-effect tags + exported API surface + test relationships + size band. Deterministic, no LLM. |
| `reachable` | Not loaded by default; surfaced as discoverable follow-up context. Doesn't consume the budget. |

### Relevance scoring

Each file scores 0.0–1.0 via a weighted sum of eight factors:

| Factor | Default weight | What it measures |
|---|---|---|
| `s_mention` | 0.25 | Task text contains the file's path or basename |
| `s_filename` | 0.12 | Jaccard similarity between basename tokens and anchor set |
| `s_symbol` | 0.20 | Anchors matching exported Go identifiers (case-insensitive substring) |
| `s_import` | 0.12 | Two-pass: the file imports packages that score highly on other factors |
| `s_package` | 0.10 | File's package path or sibling matches an anchor |
| `s_test` | 0.08 | Test file associated with a high-scoring production file |
| `s_doc` | 0.07 | Jaccard of doc token bag (first 2 KiB) vs. anchor set |
| `s_config` | 0.06 | Config-shaped filename, weighted by action type |

Weights sum to exactly 1.0. Override in `.aperture.yaml` under
`scoring.weights` (sum must remain 1.0 ± 0.001). The resolved weight
set is part of the manifest hash.

### Action types

Derived from the task text via an ordered rule table:

| Priority | Type | Triggered by (any of) |
|---|---|---|
| 1 | `bugfix` | `fix`, `bug`, `broken`, `regression`, `crash`, `panic`, `error is`, `fails to`, `should not`, `incorrect` |
| 2 | `test-addition` | `add tests`, `write tests`, `test coverage`, `unit tests`, `integration tests`, `missing tests` |
| 3 | `documentation` | `document`, `docs`, `readme`, `comments`, `godoc`, `javadoc` |
| 4 | `migration` | `migrate`, `migration`, `upgrade`, `downgrade`, `backfill`, `rename column`, `drop column`, `schema change` |
| 5 | `refactor` | `refactor`, `rewrite`, `restructure`, `clean up`, `cleanup`, `extract`, `split`, `deduplicate` |
| 6 | `investigation` | `investigate`, `explore`, `understand`, `research`, `look into`, `diagnose`, `why does`, `how does` |
| 7 | `feature` | `add`, `implement`, `support`, `introduce`, `new`, `create`, `enable` |
| 8 | `unknown` | default |

Priority is strict: "investigate why the new fix breaks" classifies as
`bugfix` because rule 1 wins over rule 6.

### Gap categories

| Category | Fires when |
|---|---|
| `missing_spec` | Feature/refactor/migration/investigation task + no SPEC.md / AGENTS.md found |
| `missing_tests` | Feature/bugfix/refactor/migration + no `_test.go` selected at score ≥ 0.50 |
| `missing_config_context` | Task mentions config/env/settings + no config file selected |
| `unresolved_symbol_dependency` | Task names a Go identifier that isn't exported anywhere in the repo |
| `ambiguous_ownership` | Top-scoring file's package has ≥2 peers over 0.60, no clear owner ≥0.80 |
| `missing_runtime_path` | Feature/bugfix/migration + runtime anchors + no selected file carries an `io:*` side-effect tag |
| `missing_external_contract` | Task mentions API/RPC/schema + no `*openapi*` / `*swagger*` / `*schema*` / `*api*` file selected |
| `oversized_primary_context` | Budget underflow, or a highly-relevant file was demoted from `full` |
| `task_underspecified` | No candidate reaches score 0.60. (The SPEC lists three triggers, but the anchors<2 and action=unknown triggers are already folded into feasibility's `task_specificity` sub-signal; firing the gap on them would double-count the same weakness once in the gap list and again in the feasibility math.) |

### Feasibility

```
feasibility = clamp01(
    0.40 · coverage
  + 0.25 · anchor_resolution
  + 0.20 · task_specificity
  + 0.15 · budget_headroom
) - gap_penalty
```

Clamped to `≤0.40` whenever any blocking gap fires. Bands:

- `≥ 0.85` — high feasibility
- `0.65–0.84` — moderate
- `0.40–0.64` — weak
- `< 0.40` — poor

Every sub-signal value is emitted numerically in
`manifest.feasibility.sub_signals` so the score is auditable.

### Determinism

The manifest hash is `sha256` over the compact, lexicographically-
key-sorted JSON form of the manifest with these fields stripped:
`manifest_hash`, `manifest_id`, `generated_at`, `aperture_version`,
`host`, `pid`, `wall_clock_started_at`. Identical inputs → identical
hash, even across Go toolchain versions. The test suite asserts this
over 20 consecutive runs.

Ordering is always ascending, byte-wise, over the normalized
repository-relative path — forward-slash separators, no leading `./`,
NFC Unicode.

---

## CLI reference

```
aperture plan [TASK_FILE]   Generate a manifest
aperture explain [TASK_FILE | --manifest <path>]  Render reasoning
aperture run <agent> [TASK_FILE]   Plan and invoke an adapter
aperture cache clear        Remove .aperture/{cache,index,summaries}
aperture version            Print build identity
```

### `aperture plan` flags

| Flag | Default | Notes |
|---|---|---|
| `--repo <path>` | cwd | Repository root. Honored verbatim. |
| `-p, --prompt <text>` | | Inline task text; mutually exclusive with `TASK_FILE`. |
| `--model <id>` | config or unset | Drives tokenizer dispatch. |
| `--budget <int>` | config | Token ceiling. |
| `--format <json\|markdown>` | `json` | |
| `--out <path>` | stdout | |
| `--fail-on-gaps` | off | Exit 8 on any blocking gap. |
| `--min-feasibility <float>` | 0 (off) | Exit 7 if score below. |
| `--config <path>` | `<repo>/.aperture.yaml` | |

### `aperture run` flags

Same as `plan`, plus `--out-dir <path>` for where to persist the JSON
manifest, Markdown manifest, and merged `run-<id>.md` prompt
(defaults to `.aperture/manifests/` under the repo).

The adapter sees these environment variables:

```
APERTURE_MANIFEST_PATH            # absolute path to JSON manifest
APERTURE_MANIFEST_MARKDOWN_PATH   # absolute path to Markdown manifest
APERTURE_TASK_PATH                # task file or tempfile for inline tasks
APERTURE_PROMPT_PATH              # merged prompt (Markdown + --- + task)
APERTURE_REPO_ROOT
APERTURE_MANIFEST_HASH            # hex sha256, no "sha256:" prefix
APERTURE_VERSION
```

Plus anything in the agent's `env:` block in `.aperture.yaml`.

### Exit codes

| Code | Meaning |
|---|---|
| 0 | Success |
| 1 | Unexpected internal failure |
| 2 | Invalid command-line arguments |
| 3 | Unreadable task file |
| 4 | Invalid repository root |
| 5 | Malformed `.aperture.yaml` |
| 6 | Manifest serialization or schema validation failure |
| 7 | `--min-feasibility` threshold not met |
| 8 | Blocking gap present with `--fail-on-gaps` |
| 9 | Budget underflow — the effective context budget is smaller than the smallest viable cost of the highest-scoring candidate. "Underflow" in the §7.6.5 sense: the budget runs out of headroom before ONE real selection can fit, not a token-count overflow. |
| 10 | Recognized-but-unsupported model (no tokenizer tables) |
| 11 | Unknown agent name in `aperture run` |
| 12 | Adapter failed to start (exec not found / permission denied) |
| * | Any other value: adapter's own exit code, propagated verbatim |

---

## Configuration

`.aperture.yaml` at the repo root (override with `--config`). All fields
optional:

```yaml
version: 1

defaults:
  model: claude-sonnet-4-6     # or gpt-4o, gpt-4, codex-*, o1-mini, …
  budget: 120000
  reserve:
    instructions: 6000
    reasoning:    20000
    tool_output:  12000
    expansion:    10000

# Added to the built-in defaults unless exclude_disable_defaults: true.
exclude:
  - vendor/**
  - node_modules/**
  - "**/*.min.js"

# Set to true to REPLACE the default exclusions entirely.
exclude_disable_defaults: false

languages:
  go:
    enabled: true
  markdown:
    enabled: true

thresholds:
  min_feasibility: 0.70
  fail_on_blocking_gaps: true

output:
  directory: .aperture/manifests
  format: json

# Weights must sum to 1.0 ± 0.001.
scoring:
  weights:
    mention:  0.25
    filename: 0.12
    symbol:   0.20
    import:   0.12
    package:  0.10
    test:     0.08
    doc:      0.07
    config:   0.06

# Promote any of the nine gap categories to blocking severity.
gaps:
  blocking:
    - oversized_primary_context
    - missing_spec

agents:
  claude:
    command: claude
    args: []
    pass_task_as_arg: false          # default for built-in claude/codex
    mode: non-interactive            # or "interactive"

  codex:
    command: codex
    args: []
    pass_task_as_arg: false

  # User-declared adapter — pass_task_as_arg defaults to true here.
  my-wrapper:
    command: /usr/local/bin/my-wrapper
    args: ["--mode", "plan"]
    env:
      MY_TOKEN: xyz
```

Unknown top-level keys are rejected (exit 5). Weights that don't sum
to 1.0 within 0.001 are rejected. The fully-resolved config is hashed
into `manifest.generation_metadata.config_digest`, so a config change
changes the manifest hash.

---

## Manifest shape

```jsonc
{
  "schema_version": "1.0",
  "manifest_id":    "apt_<16 hex>",
  "manifest_hash":  "sha256:<64 hex>",
  "generated_at":   "2026-04-18T…Z",
  "incomplete":     false,

  "task": {
    "task_id":   "tsk_<16 hex>",
    "source":    "TASK.md",
    "raw_text":  "…",
    "type":      "feature",
    "objective": "first non-empty line",
    "anchors":   ["Provider", "RefreshToken", "oauth", …],
    "expects_tests":        true,
    "expects_config":       false,
    "expects_docs":         false,
    "expects_migration":    false,
    "expects_api_contract": false
  },

  "repo": {
    "root":           "/abs/path/to/repo",
    "fingerprint":    "sha256:<64 hex>",
    "language_hints": ["go", "markdown", "yaml"]
  },

  "budget": {
    "model":                     "claude-sonnet-4-6",
    "token_ceiling":             120000,
    "reserved":                  { "instructions": 6000, … },
    "effective_context_budget":  72000,
    "estimated_selected_tokens": 8200,
    "estimator":                 "heuristic-3.5",   // or tiktoken:cl100k_base
    "estimator_version":         "v1"
  },

  "selections": [
    {
      "path":            "internal/oauth/provider.go",
      "kind":            "file",
      "load_mode":       "full",
      "relevance_score": 0.91,
      "score_breakdown": [
        {"factor": "mention", "signal": 1.0, "weight": 0.25, "contribution": 0.25},
        …
      ],
      "estimated_tokens": 405,
      "rationale":        ["direct task mention", "package match", …],
      "side_effects":     ["io:network", "io:time"]
    }
  ],

  "reachable":   [ … ],
  "exclusions":  [ {"path": "vendor/**", "reason": "default_pattern"}, … ],

  "gaps": [
    {
      "id":                    "gap-1",
      "type":                  "missing_tests",
      "severity":              "warning",
      "description":           "…",
      "evidence":              ["…"],
      "suggested_remediation": ["…"]
    }
  ],

  "feasibility": {
    "score":               0.82,
    "assessment":          "moderate feasibility",
    "positives":           ["anchor_resolution=0.86", …],
    "negatives":           ["gap_penalty=0.05"],
    "blocking_conditions": [],
    "sub_signals": {
      "coverage":          0.75,
      "anchor_resolution": 0.86,
      "task_specificity":  1.00,
      "budget_headroom":   0.94,
      "gap_penalty":       0.05
    }
  },

  "generation_metadata": {
    "aperture_version":           "1.0.0",
    "selection_logic_version":    "sel-v1",
    "config_digest":              "sha256:…",
    "side_effect_tables_version": "side-effect-tables-v1",
    "host":                       "runner-01",
    "pid":                        1234,
    "wall_clock_started_at":      "2026-04-18T…Z"
  }
}
```

Every emitted manifest is validated against `schema/manifest.v1.json`
before write. A validation failure exits 6 and nothing is persisted.

**`manifest_id` vs `manifest_hash`.** These two fields do different
jobs and should not be confused:

- `manifest_id` (`apt_<16 hex>`) is a **per-run correlation handle**.
  It's derived from random bytes at generation time and differs on
  every run, even when the planning decisions are byte-identical. Use
  it to tie a manifest to its log lines, its merged-prompt file
  (`run-<id>.md`), and the adapter invocation it drove.
- `manifest_hash` (`sha256:<64 hex>`) is the **semantic identity of
  the plan**. It's computed over the normalized manifest with all
  per-run fields (`manifest_id`, `generated_at`, `host`, `pid`,
  `wall_clock_started_at`, `aperture_version`, `manifest_hash`
  itself) stripped. Identical inputs produce identical hashes across
  runs, across machines, and across patch-level Aperture builds.
  This is the field CI should pin, diff, and gate on — not
  `manifest_id`.

### Reachable-mode plumbing

`reachable` selections are files Aperture believes are relevant but
didn't have budget for — the caller should know they exist and can
pull them in on demand. They don't consume the token budget, but they
are not invisible either. `aperture run` wires them through to the
downstream agent in three places:

1. **JSON manifest `reachable` array.** Each entry carries the path,
   the score, and a `rationale` explaining why it was kept reachable
   rather than dropped (usually "budget exceeded" or "load-mode
   downgraded after full/summary didn't fit"). Agents that parse the
   manifest directly (via `APERTURE_MANIFEST_PATH`) get the full list.
2. **Markdown manifest `## Reachable` section.** The Markdown form
   renders the same list as a bulleted section below `## Selections`,
   with the score and one-line rationale per entry. This is the form
   LLM-based agents typically read, because it's part of the merged
   prompt.
3. **Merged `run-<id>.md` prompt.** The file passed on stdin to the
   adapter is the Markdown manifest concatenated with `---` and the
   task text. The reachable section appears verbatim in that prompt,
   so any downstream agent — whether `claude`, `codex`, or a custom
   wrapper — sees the reachable list without needing to open a second
   file.

This means a coding agent can *ask* for a reachable file by path
during its run and trust that Aperture already vetted it as relevant.
The contract is: reachable files are pre-approved context, they just
didn't fit this budget. They are not speculative suggestions and not
fallbacks — they're the same ranked candidate set as `full` and the
summary modes, re-routed because the ceiling binds.

---

## Security and trust

Aperture runs locally by default. It makes **no network calls** on
repo contents. Tokenizer tables are embedded at build time; there is
no fallback to a remote fetch.

> **⚠ Trust model for `aperture run`.** The `agents.<name>.command`
> entries in `.aperture.yaml` are shell commands Aperture will
> execute with the user's own privileges. A hostile
> `.aperture.yaml` can put arbitrary commands there (e.g. a PR
> that changes `agents.claude.command` to a curl|sh). v1 has **no
> automatic trust gate** — a formal approval flow is deferred to a
> post-v1 release. Until then, the trust contract is:
>
> - `aperture plan` and `aperture explain` are **safe** on any repo
>   — they never exec an adapter.
> - `aperture run <name>` is **unsafe** on untrusted input. Treat
>   `.aperture.yaml` exactly like a Makefile: review it before
>   invoking `aperture run` from a fresh clone or a PR branch.
> - **In CI on fork PRs**: use `aperture plan`, never `aperture run`.
>   The adapter invocation path is explicitly designed to be skipped
>   when the caller can't vouch for `.aperture.yaml`'s provenance.

Inline tasks are written to `$TMPDIR/aperture-task-<id>.txt` with
`O_CREATE|O_EXCL|O_WRONLY`, so a symlink planted at the target path
fails the open rather than being followed. Cleanup runs in three
layers:

1. **Deferred cleanup** on every normal return path.
2. **Signal handlers** (`SIGINT`, `SIGTERM`, `SIGHUP`) on Unix delete
   the tempfile before the process exits. `SIGKILL` intentionally
   bypasses this — the kernel can't be negotiated with, and
3. **A 24-hour orphan sweep** at the start of every `aperture run`
   removes `aperture-task-*.txt` files older than that threshold,
   bounding any SIGKILL leak (and covering Windows where step 2 is a
   no-op). The sweep is one `ReadDir` plus a `Stat` per matching
   entry — O(`$TMPDIR` size) but with a narrow filename-prefix
   filter, and never fails the run.

Adapter commands run with the user's own privileges. Aperture does
not escalate, sandbox, or restrict what the downstream agent does.

---

## Performance

Measured via `make bench` on the reference fixtures:

| Fixture | Files | Cold plan | Warm plan | p95 |
|---|---|---|---|---|
| small  |   500 |   86 ms |  31 ms |  33 ms |
| medium | 5 000 | 1129 ms | 322 ms | 326 ms |

SPEC §8.2 targets: small ≤10 s cold / ≤1 s warm; medium ≤60 s cold /
≤5 s warm. Both fixtures run roughly 100× under target on an Apple
M-series; CI runners will be slower but still comfortably inside the
gates.

The cache is keyed by `sha256(path, size, mtime, selection_logic_version)`
and stored as JSON sidecar files under `.aperture/cache/`. Binding to
`selection_logic_version` (currently `"sel-v1"`) instead of the build
version means a docs-only or CLI-message patch bump of Aperture no
longer invalidates every AST parse on disk — only a change to the
§7.4/§7.6 scoring or selection rules bumps `sel-v1` and wipes the
cache. A schema-drift sentinel at `.aperture/cache/VERSION` still lets
the binary invalidate the whole cache in one stat call when it sees an
older on-disk format.

---

## Development

```bash
make help                 # list all targets
make build                # ./bin/aperture
make test                 # go test ./...
make lint                 # golangci-lint run ./...
make fmt                  # gofmt -s -w .

make bench-prepare        # regenerate testdata/bench/{small,medium}/
make bench                # run the §8.2 harness
make bench-clean          # remove generated fixtures
```

Tests:

- Unit tests in every package.
- **Property tests** over randomized inputs (`task.Parse`
  determinism, `budget.Count` monotonicity, `ExitCodeError` wrap
  semantics).
- **Fuzz tests** for the dispatch and sanitization boundaries
  (`budget.Resolve`, `agent.WriteInlineTaskFileIn`).
- **Golden tests** pinning the §11.1 JSON shape and §7.9.3 Markdown
  section order.
- **Determinism tests** asserting 20 consecutive runs produce
  byte-identical normalized manifests.
- `go test -race ./...` passes on the concurrent cache paths.

Run the fuzz targets locally:

```bash
go test -run '^$' -fuzz FuzzResolve -fuzztime 30s ./internal/budget/...
go test -run '^$' -fuzz FuzzWriteInlineTaskFile_ManifestIDIsPathConstrained \
  -fuzztime 30s ./internal/agent/...
```

---

## Dependency philosophy

Standard library first. External dependencies are added only when
they implement a well-specified format impractical to reimplement
(YAML, JSON Schema) or replace more than 500 lines of in-tree code
(cobra for CLI, tiktoken-go for BPE). No ORMs, no logging frameworks
beyond `log/slog`, no HTTP clients beyond `net/http`, no cgo.
Violations are caught by `depguard` in CI.

---

## What's new in v1.1

v1.1 is a strictly-additive release that closes the credibility
gaps called out by the post-v1.0 external review. The v1.0
contract — manifest shape, exit codes, determinism, CLI flags —
is preserved unchanged.

- **`aperture eval`** — score plans against committed
  ground-truth fixtures, compare against a ripgrep baseline,
  and gate CI on regression.
  See [`docs/eval.md`](docs/eval.md).
- **`aperture eval loadmode`** — empirical calibration of the
  `behavioral_summary` vs `full` demotion rule, with a
  §7.5.2 threshold advisor.
  See [`docs/loadmode.md`](docs/loadmode.md).
- **Mention dampener** — `s_mention` is clamped when no
  other signal agrees, defusing gameable-mention false
  positives. `selection_logic_version` bumped to `sel-v2`.
- **`--scope <path>`** — project a plan onto a monorepo
  subtree. Fingerprint still covers the full tree;
  supplementals stay admissible across the scope boundary.
  See [`docs/scope.md`](docs/scope.md).
- **Tier-2 languages** — TypeScript, JavaScript, and Python
  get module-level symbol/import extraction via tree-sitter.
  See [`docs/tier2.md`](docs/tier2.md).
- **`aperture diff`** — section-by-section comparison of two
  manifests. Read-only — never touches the planner or
  repository. See [`docs/diff.md`](docs/diff.md).
- **`-tags notier2`** — pure-Go fallback build that skips
  tier-2 analysis, for `CGO_ENABLED=0` environments.
- **Prebuilt binaries** for linux/{amd64,arm64},
  darwin/{amd64,arm64}, and windows/amd64 via goreleaser.

See [`CHANGELOG.md`](CHANGELOG.md) for the full list and
[`specs/v1.1/SPEC.md`](specs/v1.1/SPEC.md) for the normative
delta contract.

## Project status

v1.1 feature-complete. All seven v1.1 implementation phases
ship with passing unit + integration + property + fuzz + golden
+ determinism tests under `make test` and `golangci-lint run
./...`. v1's SPEC lives at `specs/initial/SPEC.md`; v1.1's
additive contract at `specs/v1.1/SPEC.md`.

Deferred post-v1.1 work:

- `aperture inspect` subcommand for interactive manifest diffing.
- Formal trust-gate (explicit approval flow + persisted trusted-
  agents file) for `.aperture.yaml`-declared custom adapter commands.
- Secret-pattern redaction of manifest contents.
- Per-symbol (rather than file-level) budgeting.
- Side-effect tagging for tier-2 languages.
- Optional LLM summarization (opt-in, clearly flagged, pinned model).

---

## License

See LICENSE.
