# SPEC.md

## Project Title

Aperture

## One-Line Summary

Aperture is a Go-based pre-execution context planning tool for coding agents. Given a task description and a repository, it produces a deterministic, token-budgeted context manifest describing which files should be loaded in full, which should be represented via structured summaries, which remain reachable but unloaded, what important gaps exist, and whether the task appears feasible from the selected context slice.

---

## 1. Purpose

Coding agents routinely waste tokens and make bad decisions because they begin with either:

- too much repository context,
- too little repository context,
- or the wrong repository context.

Aperture exists to solve that problem before the agent runs.

It is not the coding agent. It is not a generic vector search product. It is not just a token counter.

It is a **context planner** and **budgeting engine** that sits in front of an agent such as Claude Code or Codex and produces a structured, auditable, reproducible context selection artifact.

---

## 2. Goals

### 2.1 Primary Goals

1. Accept a task description and repository root as input.
2. Analyze repository structure, symbols, imports, and the supplemental file set defined in §7.1.3.
3. Build a candidate set of relevant files and symbols.
4. Fit selected context into a model-specific token budget.
5. Assign a load mode to each selected file or artifact.
6. Detect likely information gaps before agent execution.
7. Produce a deterministic immutable manifest for downstream agent use.
8. Support use in CLI-first workflows for Claude Code, Codex, and similar agent systems.

### 2.2 Secondary Goals

1. Allow reproducible re-planning with identical inputs.
2. Cache repository analysis so repeated (warm-cache) planning meets the performance targets in §8.2.
3. Emit human-readable and machine-readable outputs.
4. Support Go-first implementation but allow future language expansion.
5. Enable future integration with other tools in the user’s workflow stack.

### 2.3 Non-Goals

1. Aperture will not execute code modifications.
2. Aperture will not replace the downstream agent.
3. Aperture will not attempt to fully solve semantic understanding of every programming language in v1.
4. Aperture will not require an IDE integration in v1.
5. Aperture will not depend on external hosted services in v1 unless explicitly configured.

---

## 3. Core User Story

As a developer using a coding agent, I want to run Aperture before an agent task, so that the agent begins with the smallest high-confidence context slice needed to succeed, with explicit visibility into what was included, what was summarized, what was excluded, and what information is missing.

---

## 4. Representative CLI Usage

### 4.1 Basic Planning

```bash
aperture plan TASK.md
```

### 4.2 Specify Repo Root

```bash
aperture plan TASK.md --repo /path/to/repo
```

### 4.3 Specify Model and Budget

```bash
aperture plan TASK.md --model claude-sonnet --budget 120000
```

### 4.4 Emit JSON Manifest

```bash
aperture plan TASK.md --format json --out .aperture/manifest.json
```

### 4.5 Emit Human-Readable Manifest

```bash
aperture plan TASK.md --format markdown --out .aperture/manifest.md
```

### 4.6 Fail if Gaps Exist

```bash
aperture plan TASK.md --fail-on-gaps
```

### 4.7 Require Minimum Feasibility

```bash
aperture plan TASK.md --min-feasibility 0.80
```

### 4.8 Wrapper Execution

```bash
aperture run claude TASK.md
aperture run codex TASK.md
```

---

## 5. High-Level Behavior

Given:

- a task file or task prompt,
- a repository root,
- optional configuration,
- and a target model budget,

Aperture shall:

1. Parse the task into a structured internal task representation.
2. Index the repository if the cache is missing or invalid per §7.11.2.
3. Build a dependency- and relevance-aware candidate file set.
4. Estimate the token cost of including full content or alternative views of each candidate.
5. Choose a context slice under the budget using the deterministic greedy selection algorithm defined in §7.6.2.1.
6. Assign each candidate one of several load modes.
7. Identify likely missing information required for successful execution.
8. Produce a hashed immutable manifest (SHA-256 per §7.9.4). Cryptographic signing is out of scope for v1.
9. Optionally produce companion instructions suitable for direct use by the selected coding agent.

---

## 6. Terminology and Domain Model

## 6.1 Task

A Task is the aggregate root for a planning run.

A Task is derived **solely** from the user-supplied task input, which is exactly one of:

- a path to a markdown task file,
- a path to a plain text file,
- an inline task string.

Supplemental files such as `SPEC.md`, `PLAN.md`, `AGENTS.md`, and `README.md` (§7.1.3) do **not** contribute to Task derivation. They participate in planning only through scoring (§7.4.2.1) and gap detection (§7.7.3). Repository signals likewise do not feed Task parsing — they feed candidate scoring, not anchor/action-type inference. This separation is mandatory for deterministic task parsing.

A Task includes:

- `task_id` — deterministic `"tsk_" + sha256(utf8(raw_input_text))[0:16]`
- `source` — path to the task file or literal `"<inline>"` when supplied via `-p`
- `raw_input_text` — the exact bytes of the user-supplied task content
- parsed `objective` — first non-empty line of the task text, trimmed
- inferred `action_type` (§7.3.1.1)
- `anchors` (§7.3.2)
- booleans `expects_tests`, `expects_config`, `expects_docs`, `expects_migration`, `expects_api_contract` (§7.3.3)

The manifest `task` object (§11.1) must include all fields above. `task_id` is derived deterministically from the raw input text and so is stable across equivalent runs; it is **included** in the hash input.

### 6.1.1 Task Action Types

At minimum v1 shall support:

- bugfix
- feature
- refactor
- test-addition
- documentation
- investigation
- migration
- unknown

---

## 6.2 ContextSlice

A ContextSlice is a selected collection of repository artifacts chosen for inclusion or reachability in the planning result.

A ContextSlice includes:

- selected files
- selected symbols
- summaries
- adjacency metadata
- total estimated token cost
- rationale for selection

---

## 6.3 Budget

A Budget defines the token envelope and selection constraints for a plan.

A Budget includes:

- total token ceiling
- reserved tokens for agent reasoning/instructions
- reserved tokens for tool outputs
- reserved tokens for downstream expansion
- effective context budget

### 6.3.1 Model-Specific Budget

Budget may be derived from:

- explicit `--budget`
- known model presets
- config defaults
- conservative safety margins

---

## 6.4 Manifest

A Manifest is the immutable output of a planning run.

A Manifest includes:

- task metadata
- repo fingerprint (see §6.4.1)
- planning configuration
- selected files and load modes
- token accounting
- feasibility analysis
- gaps
- exclusions
- generation timestamp
- deterministic manifest hash

Manifest must be reproducible given identical inputs.

### 6.4.1 Repository Fingerprint

The repository fingerprint is a deterministic `sha256` over the UTF-8 bytes of the compact JSON representation of:

```
{
  "files": [
    { "path": "<normalized-repo-relative-path>", "sha256": "<hex>", "size": <bytes>, "mtime": "<RFC3339 UTC>" },
    ...
  ],
  "aperture_version": "<semver>"
}
```

The `files` array contains one entry for every non-excluded file in the index (post §7.4.3 exclusion rules), sorted ascending by normalized path (§14). File content hashes use `sha256`. The fingerprint is computed after exclusion filtering and before scoring, and is recorded as `repo.fingerprint = "sha256:" + hex` in the manifest.

---

## 6.5 Gap

A Gap is information believed relevant or necessary to task success that is not sufficiently represented in the selected slice.

Examples:

- missing tests for touched module
- unclear config source
- ambiguous interface ownership
- unresolved external dependency
- missing documentation of a workflow
- spec does not define expected behavior

A Gap includes:

- `id` — stable string identifier (e.g., `gap-1`); ordering is `gap-<N>` in order of emission.
- `type` — one of the categories in §7.7.1.
- `severity` — one of `info`, `warning`, `blocking` (§7.7.2). A gap is blocking iff `severity == "blocking"`; there is no separate boolean field.
- `description` — one-line human-readable summary.
- `evidence` — array of short strings, each citing a concrete repo/task signal.
- `suggested_remediation` — array of concrete action strings (§7.7.4).

---

## 6.6 LoadMode

Each selected artifact must have a load mode.

### 6.6.1 Required Load Modes for v1

- `full`
- `structural_summary`
- `behavioral_summary`
- `reachable`

### 6.6.2 Semantics

- `full`: raw content should be loaded into the downstream agent.
- `structural_summary`: symbol/interface/type/function-oriented summary.
- `behavioral_summary`: a deterministic, statically-derived role sketch (see §12.2). In v1 this is restricted to facts extractable without an LLM: imported packages, side-effect signals inferred from standard-library or well-known package imports (e.g. `os`, `os/exec`, `net/http`, `database/sql`, `io`), exported API surface, and known test-file association. Natural-language "responsibilities" and "call paths" are deferred to a future opt-in LLM mode (§12.3).
- `reachable`: not initially loaded, but explicitly noted as discoverable follow-up context.

---

## 7. Functional Requirements

## 7.1 Input Handling

### 7.1.1 Task Input

The system must accept:

- a path to a markdown task file
- a path to a text file
- a raw inline task string

### 7.1.2 Repository Input

The system must accept:

- explicit repo root
- inferred current working directory repo root

The system must validate that the repo root is a readable directory.

### 7.1.3 Supplemental Files

The system must detect the following supplemental files using the exact glob patterns listed (evaluated relative to the repo root, case-insensitive on the filename component):

| Category            | Glob patterns (v1)                                                                                                                                                                             |
|---------------------|------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| Spec                | `SPEC.md`, `specs/**/SPEC.md`, `docs/spec*.md`                                                                                                                                                 |
| Plan                | `PLAN.md`, `specs/**/PLAN.md`, `docs/plan*.md`                                                                                                                                                 |
| Agents guidance     | `AGENTS.md`, `CLAUDE.md`, `.cursor/rules/*.md`, `.cursorrules`, `.github/copilot-instructions.md`                                                                                              |
| Readme              | `README.md`, `README`, `README.rst`, `README.adoc`                                                                                                                                             |
| Architecture docs   | `docs/architecture*.md`, `docs/design*.md`, `docs/adr/**/*.md`, `docs/decisions/**/*.md`, `ARCHITECTURE.md`, `DESIGN.md`                                                                       |
| Lint config         | `.golangci.yml`, `.golangci.yaml`, `.eslintrc*`, `.prettierrc*`, `.editorconfig`, `ruff.toml`, `pyproject.toml`, `tsconfig.json`, `biome.json`                                                 |
| Test config         | `.verifier.yaml`, `jest.config.*`, `vitest.config.*`, `playwright.config.*`, `pytest.ini`, `tox.ini`, `go.test.yaml`                                                                           |
| Build files         | `Makefile`, `makefile`, `*.mk`, `go.mod`, `go.sum`, `Dockerfile`, `docker-compose.y*ml`, `package.json`, `yarn.lock`, `pnpm-lock.yaml`, `Cargo.toml`, `pyproject.toml`, `Taskfile.y*ml`         |
| CI config           | `.github/workflows/*.y*ml`, `.gitlab-ci.yml`, `.circleci/config.yml`                                                                                                                            |

Detection produces a `supplemental_files` map in the index. Files in this map remain subject to the scoring and selection rules in §7.4 and §7.5; they are not auto-selected by virtue of being supplemental, but their detection feeds the `s_doc` and `s_config` signals in §7.4.2.1.

---

## 7.2 Repository Analysis

### 7.2.1 Repository Scanner

Aperture must scan the repository and construct an index containing at least:

- file paths
- file sizes
- file types
- language inference
- import relationships where supported
- symbol tables where supported
- package/module relationships where supported
- last modification timestamp (`mtime`) for every indexed file, recorded as RFC 3339 UTC
- test file relationships where inferable

### 7.2.2 Language Support

v1 must prioritize Go.

For all non-Go file types v1 provides **path-level indexing only** as a mandatory minimum: path, size, detected language tag, modification time, and raw-bytes eligibility for `full` load (per §7.2.2.1). No symbol extraction, no import graph, no language-specific summarization is required in v1.

Files in the following formats must additionally contribute to scoring via the doc/config signals defined in §7.4.2.1:

- Markdown (`.md`, `.markdown`), reStructuredText (`.rst`), AsciiDoc (`.adoc`) — feed `s_doc`.
- YAML (`.yaml`, `.yml`), JSON (`.json`), TOML (`.toml`) — feed `s_config`.
- `Makefile`, `*.mk`, `go.mod`, `go.sum`, `Dockerfile`, shell scripts (`.sh`, `.bash`, `.zsh`) — feed `s_config`.

TypeScript, JavaScript, and SQL receive path-level indexing only in v1; no language-specific parsing. Language-specific AST support for these (and for Python) is explicitly a Future Enhancement (§21) and is not a v1 requirement.

### 7.2.2.1 Non-Go Repository Behavior

Aperture must not fail when a repository contains no Go files. For non-Go files in v1:

- The indexer must record file path, byte size, detected language tag, and modification time.
- Symbol extraction, import graphs, and package relationships are not required.
- Non-Go files remain eligible for selection and for `full` load mode when their relevance score meets the threshold in §7.5.1.
- Non-Go files are not eligible for `structural_summary` in v1; they may receive `behavioral_summary` only via the deterministic rules in §12.2.
- A repository with zero Go files must still produce a valid manifest; feasibility scoring must account for reduced analysis fidelity in its rationale.

### 7.2.3 Go-Specific Analysis

For Go files, Aperture must attempt to extract:

- package name
- imports
- exported types
- interfaces
- functions
- methods
- receiver types
- test functions
- file-to-package mapping

AST parsing via the Go standard library (`go/parser`, `go/ast`, `go/token`) is mandatory for all files ending in `.go` that parse successfully. Regex-based extraction is forbidden for Go source. Files that fail to parse must be recorded with a `parse_error` tag in the index and:

- are still eligible for `full` load mode;
- must not contribute a `structural_summary`;
- when selected for `behavioral_summary`, the summary body contains only the subset of §12.2 derivable without an AST: import path list (parsed from the file's `import` block via a minimal non-AST scan permitted **only for this fallback** — not for general extraction), file size band, associated test file (by filename convention `<name>_test.go`), and an explicit `parse_error: true` flag. Side-effect tags may be emitted only when import paths are recoverable.

---

## 7.3 Task Understanding

### 7.3.1 Task Parsing

Aperture must parse the task into an internal Task structure. All inference in this section is **deterministic keyword/pattern matching** against the lowercased task text. No LLM is used.

The "task text" input to the parsing rules is the concatenation, in this exact order, of:

1. The raw task content (markdown file, plain text file, or inline `-p` string).
2. *Nothing else.*

Supplemental files (as defined in §7.1.3) are **not** concatenated into the task text for anchor/action-type inference. They influence planning only through the scoring signals in §7.4.2.1 (where they feed `s_doc` and `s_config`) and through the `missing_spec` gap rule in §7.7.3. This separation keeps task parsing deterministic for a given user-supplied task input regardless of repository state.

It must produce:

- `action_type` — resolved per §7.3.1.1
- `anchors` — the set defined in §7.3.2
- `expects_tests`, `expects_config`, `expects_docs`, `expects_migration`, `expects_api_contract` — booleans per §7.3.3

#### 7.3.1.1 Action Type Classification

The resolver evaluates the following rules in order. The first rule whose pattern matches the lowercased task text determines `action_type`. Patterns are matched against whole-word boundaries (`\b`).

| Order | Action type      | Pattern (any of)                                                                                                |
|-------|------------------|------------------------------------------------------------------------------------------------------------------|
| 1     | `bugfix`         | `fix`, `bug`, `broken`, `regression`, `crash`, `panic`, `error is`, `fails to`, `should not`, `incorrect`        |
| 2     | `test-addition`  | `add tests?`, `write tests?`, `test coverage`, `unit tests?`, `integration tests?`, `missing tests?`            |
| 3     | `documentation`  | `document`, `docs?`, `readme`, `comments?`, `godoc`, `javadoc`                                                    |
| 4     | `migration`      | `migrate`, `migration`, `upgrade`, `downgrade`, `backfill`, `rename column`, `drop column`, `schema change`      |
| 5     | `refactor`       | `refactor`, `rewrite`, `restructure`, `clean up`, `cleanup`, `extract`, `split`, `deduplicate`                   |
| 6     | `investigation`  | `investigate`, `explore`, `understand`, `research`, `look into`, `diagnose`, `why does`, `how does`              |
| 7     | `feature`        | `add`, `implement`, `support`, `introduce`, `new`, `create`, `enable`                                            |
| 8     | `unknown`        | default when no rule above matches                                                                                |

### 7.3.2 Keywords and Anchors

The planner must produce the anchor set deterministically by taking the union of:

1. All tokens in the task text matching the identifier pattern `[A-Z][A-Za-z0-9_]{2,}` (likely type or symbol names).
2. All bare filenames and paths matching `[A-Za-z0-9_./-]+\.(go|md|yaml|yml|json|toml|proto|sql|ts|tsx|js|py|sh)` (case-insensitive).
3. All backtick-quoted code spans in Markdown task files.
4. The lowercased, alphanumeric-only form of every word of length ≥ 4 in the task text, minus a fixed stopword list: `the, and, for, with, that, this, from, into, when, then, will, must, should, would, could, have, make, take, need, want, user, file, code, task, they, them, their`.

The anchor set is a sorted, deduplicated list of strings. Ordering is ascending byte-wise.

### 7.3.3 Heuristic Inference

Booleans set by deterministic rules on lowercased task text:

- `expects_tests = true` if `action_type ∈ {feature, bugfix, refactor, migration, test-addition}` **or** the task mentions `test`, `spec`, `verify`, `assert`.
- `expects_config = true` if any anchor matches `config`, `env`, `environment`, `settings`, `flag`, `flags`, `secret`, `database`, `db`, `port`, `host`, `url`, `token`, `yaml`, `yml`, `toml`, `dotenv`.
- `expects_docs = true` if `action_type = documentation` **or** the task mentions `document`, `readme`, `doc`, `docs`, `adr`, `design doc`.
- `expects_migration = true` if `action_type = migration` **or** the task mentions `migration`, `schema`, `column`, `table`, `index`, `backfill`.
- `expects_api_contract = true` if any anchor matches `api`, `rpc`, `grpc`, `openapi`, `swagger`, `graphql`, `proto`, `protobuf`, `contract`, `schema`, `interface`.

These booleans feed the gap detection rules in §7.7.3 and the feasibility algorithm in §7.8.2.1.

---

## 7.4 Candidate Retrieval

### 7.4.1 Initial Candidate Set

The planner must build an initial candidate set using:

- direct filename/path matches
- keyword/path similarity
- symbol matches
- import adjacency
- package adjacency
- doc adjacency
- test adjacency

### 7.4.2 Relevance Scoring

Each candidate artifact must receive a relevance score in `[0.0, 1.0]`.

#### 7.4.2.1 Score Formula

The score is a deterministic weighted sum of normalized per-factor signals, clamped to `[0.0, 1.0]`:

```
score(f) = clamp01(
    w_mention     * s_mention(f)
  + w_filename    * s_filename(f)
  + w_symbol      * s_symbol(f)
  + w_import      * s_import(f)
  + w_package     * s_package(f)
  + w_test        * s_test(f)
  + w_doc         * s_doc(f)
  + w_config      * s_config(f)
)
```

Each `s_*(f)` is a deterministic function returning a value in `[0.0, 1.0]`:

- `s_mention`: `1.0` if the lowercased task text contains the lowercased normalized repository-relative path of `f` or the lowercased basename of `f`; `0.0` otherwise. Matching is always case-insensitive (ASCII lowercase) regardless of host filesystem case-sensitivity, so results are identical across platforms.
- `s_filename`: Jaccard similarity between the set of lowercase alphanumeric tokens in the file basename (split on non-alphanumeric) and the task anchor set (§7.3.2).
- `s_symbol`: for Go files, the fraction of task anchors that case-insensitively match any exported identifier in the file's symbol table, capped at `1.0`.
- `s_import`: computed in two passes. In pass 1, every file receives a score from the other seven factors (with `s_import = 0`) producing `score_pass1(f)`. Package-level relevance is then defined as `score_pass1(pkg) = max(score_pass1(f) for f in files(pkg))`. In pass 2, `s_import(f)` is the **maximum** value among the applicable tier rules (tiers are evaluated independently; the highest matching tier wins):
  - `1.0` if any package directly imported by `f` has `score_pass1(pkg) ≥ 0.80`;
  - else `0.7` if any package directly imported by `f` has `score_pass1(pkg) ≥ 0.60`;
  - else `0.4` if any package within 2 import hops (transitive) of `f` has `score_pass1(pkg) ≥ 0.60`;
  - else `0.0`.
  The two-pass structure and highest-matching-tier rule must be applied deterministically and are part of the scoring contract.
- `s_package`: `1.0` if the file's package import path or final path segment matches a task anchor (case-insensitive); `0.7` if the file's package directory is a sibling of such a package (same parent directory); `0.0` otherwise.
- `s_test`: `1.0` if the file is a test file for a candidate scoring ≥ 0.5 under the other factors; `0.5` if it is a test file for any package referenced by the task; `0.0` otherwise.
- `s_doc`: for `.md`/`.rst`/`.adoc` files, Jaccard similarity between the document's lowercased alphanumeric token bag (first 2 KiB) and the task anchor set.
- `s_config`: `1.0` if the filename matches any of `Makefile`, `*.mk`, `go.mod`, `go.sum`, `*.yaml`, `*.yml`, `*.toml`, `*.json` under repo root or under a directory named `config`/`configs`, and the task's inferred action type is one of `feature`, `migration`, `refactor`; `0.5` under the same filename condition for other action types; `0.0` otherwise.

#### 7.4.2.2 Default Weights

Default weights (must sum to exactly `1.0`):

| Weight       | Value |
|--------------|-------|
| `w_mention`  | 0.25  |
| `w_filename` | 0.12  |
| `w_symbol`   | 0.20  |
| `w_import`   | 0.12  |
| `w_package`  | 0.10  |
| `w_test`     | 0.08  |
| `w_doc`      | 0.07  |
| `w_config`   | 0.06  |

Weights may be overridden in `.aperture.yaml` under `scoring.weights`, but the override set must still sum to `1.0` ± `0.001`; otherwise Aperture must fail with a config validation error (§16). The resolved weight set is part of the manifest hash input (§7.9.4).

#### 7.4.2.3 Determinism

The scoring algorithm must be deterministic: the same (repository snapshot, task, resolved config) must always produce the same per-candidate score.

### 7.4.3 Exclusions

The planner must support excluding files/directories via:

- CLI flags
- config file
- built-in defaults

The mandatory v1 default exclusion glob set, applied unless the user sets `exclude.disable_defaults: true` in `.aperture.yaml`:

```
.git/**
.hg/**
.svn/**
.aperture/**
node_modules/**
vendor/**
dist/**
build/**
out/**
target/**
bin/**
obj/**
coverage/**
.coverage/**
htmlcov/**
.next/**
.nuxt/**
.cache/**
.venv/**
venv/**
__pycache__/**
.pytest_cache/**
.mypy_cache/**
.tox/**
.gradle/**
.idea/**
.vscode/**
*.min.js
*.min.css
*.map
*.wasm
*.exe
*.dll
*.so
*.dylib
*.a
*.o
*.class
*.jar
*.war
*.pyc
*.pyo
*.pdb
*.lock
package-lock.json
yarn.lock
pnpm-lock.yaml
Cargo.lock
poetry.lock
uv.lock
```

Files also excluded if they match any of: binary files detected by a NUL byte in the first 8 KiB; files larger than 10 MiB regardless of extension; files under a directory whose name begins with `.` and is not in the allow list `{.github, .cursor, .claude, .aperture.yaml}`.

User-supplied `exclude` patterns in `.aperture.yaml` are **added to** the defaults (set union), not replacing them, unless `exclude.disable_defaults: true` is set.

---

## 7.5 Load-Mode Generation

### 7.5.0 Quantitative Thresholds

Relevance bands (score is 0.0–1.0, per §13.1):

- `highly_relevant`: score ≥ 0.80
- `moderately_relevant`: 0.60 ≤ score < 0.80
- `plausibly_relevant`: 0.30 ≤ score < 0.60
- `low_relevance`: score < 0.30 (excluded from selection by default)

Size bands (applied to source bytes or equivalent):

- `small`: ≤ 8 KiB **and** estimated tokens ≤ 2,000
- `medium`: ≤ 32 KiB **and** estimated tokens ≤ 8,000
- `large`: anything above `medium`

These bands are the canonical definitions of "reasonably sized", "highly relevant", and "plausibly relevant" used throughout §7.5. The planner must reference these bands — not qualitative adjectives — when making load-mode decisions.

### 7.5.1 Full Load Eligibility

A candidate must be assigned `full` when all of the following hold:

- `highly_relevant` **or** explicitly mentioned by path/filename in the task text
- `small` or `medium` size band
- remaining effective context budget (§7.6) can accommodate its estimated tokens

### 7.5.2 Structural Summary Eligibility

A candidate must be assigned `structural_summary` when:

- `highly_relevant` or `moderately_relevant`
- not eligible for `full` because size band is `large` **or** budget is insufficient
- the file is a Go source file with a non-empty symbol table from §7.2.3

Non-Go files are not eligible for `structural_summary` in v1 (see §7.2.2.1).

### 7.5.3 Behavioral Summary Eligibility

A candidate must be assigned `behavioral_summary` when **all** of the following hold:

- it is `moderately_relevant` or higher;
- it is **not** eligible for `full` (per §7.5.1);
- **at least one** of the following is true:
  1. it is not eligible for `structural_summary` (per §7.5.2) — e.g., it is a non-Go file, or a Go file with an empty symbol table, or failed AST parse;
  2. its side-effect signal set (§12.2) contains **two or more** tags from `io:filesystem`, `io:process`, `io:network`, `io:database`, `io:time`, `io:randomness`, `io:logging`;
  3. the file's basename matches one of the deterministic role patterns: `main.go`, `*_main.go`, `cmd/*/main.go`, `config.go`, `config_*.go`, `*_config.go`, `server.go`, `handler*.go`, `router.go`, or any file with extension `.md`, `.rst`, `.adoc`.

When both structural and behavioral summaries would be eligible, the planner must prefer `structural_summary` for Go files with a non-empty symbol table and `behavioral_summary` otherwise.

### 7.5.4 Reachable Eligibility

A candidate must be listed as `reachable` when both of the following hold:

- it is `plausibly_relevant` (0.30 ≤ score < 0.60, per §7.5.0);
- it was not assigned `full`, `structural_summary`, or `behavioral_summary` by the selection algorithm in §7.6.2.1.

A `moderately_relevant` or `highly_relevant` candidate that was not selected for any budget-consuming mode because the budget was exhausted must also be listed as `reachable` with a `reason: "budget_exhausted"` annotation.

---

## 7.6 Budgeting

### 7.6.1 Token Estimation

Aperture must estimate token usage for:

- full file inclusion
- generated summaries
- manifest overhead
- instructions overhead

#### 7.6.1.1 Tokenizer Selection

Token estimation must be model-aware and fully self-contained. The planner must select an estimator based on the resolved `--model` value (or config default):

- **Claude family** (`claude-*`): use the conservative heuristic `ceil(len(utf8_bytes) / 3.5)` in v1. Recorded as `estimator: "heuristic-3.5"`. (A native Anthropic tokenizer may be introduced in a future version; when added, its encoding tables must be **embedded in the Aperture binary** and pinned to a specific table version recorded in `budget.estimator_version`.)
- **OpenAI / Codex family** (`gpt-*`, `codex-*`, `o*`): use a tiktoken-compatible BPE tokenizer. v1 must embed exactly the following encoding tables at build time: `cl100k_base`, `o200k_base`, `p50k_base`, `r50k_base`. The model→encoding map for v1 is: `gpt-4o*` → `o200k_base`, `gpt-4*` / `gpt-3.5-turbo*` → `cl100k_base`, `codex-*` → `p50k_base`, `o1*` / `o3*` → `o200k_base`. No network access, no `$HOME` lookup, no OS-package dependency is permitted in v1. Recorded as `estimator: "tiktoken:<encoding-name>"` with `estimator_version` set to the pinned build-time table hash.
- **Unspecified model** (no `--model` flag and no config default): use the conservative heuristic `ceil(len(utf8_bytes) / 3.5)`. Recorded as `estimator: "heuristic-3.5"`.

A "recognized" model is one whose name matches one of the family patterns above (`claude-*`, `gpt-*`, `codex-*`, `o*`). A "recognized but unsupported" model is one whose family matches but whose required tokenizer table is not embedded in the current Aperture build (e.g., a future OpenAI encoding added to the spec but not yet to the released binary). An "unrecognized" model is one whose name matches no family pattern.

Behavior:

- **Unrecognized** model: use the heuristic. Recorded as `estimator: "heuristic-3.5"`.
- **Recognized but unsupported** model: fail with exit code `10` (§16); do **not** fall back to the heuristic. Runtime tokenizer download, external lookup, or other fallback is forbidden.

All token counts must be biased upward ("conservative"): on ties or rounding, round up. Selected estimator identity and version must be recorded in `budget.estimator` / `budget.estimator_version` in the manifest and are inputs to the manifest hash (§7.9.4).

Exact token parity with the downstream model is not required in v1, but estimates must be conservative and deterministic for a given (estimator, estimator_version, input) triple on any host.

### 7.6.2 Budget Optimization

The planner must fit selected context into the effective context budget (§6.3) by solving a deterministic 0/1 selection over candidate (file, load_mode) pairs.

#### 7.6.2.1 Objective Function

For each candidate file `f` and each load mode `m ∈ {full, structural_summary, behavioral_summary, reachable}` for which `f` is eligible (per §7.5), let:

- `score(f)` = relevance score in `[0.0, 1.0]` (§13.1)
- `mode_weight(m)` = fixed mode weight used in the efficiency sort (Pass 1 of §7.6.2.1): `full = 1.00`, `structural_summary = 0.60`, `behavioral_summary = 0.40`. `reachable` is assigned in Pass 2 after budget-consuming modes and does not participate in the efficiency sort; for manifest consistency its nominal weight is recorded as `0.05` but this value is never used in selection math.
- `cost(f, m)` = estimated tokens for `f` under load mode `m` (§7.6.1)

The planner must select a subset `S` of (f, m) pairs, with at most one mode per file, using the mandatory deterministic greedy algorithm in §7.6.2.1. That algorithm approximates — but is not required to reach — the maximum of the objective `Σ score(f) · mode_weight(m)` subject to the budget constraint. v1 treats the greedy output as the canonical, reproducible selection; there is no optimality obligation beyond exact reproduction of the greedy result.

Budget constraint: `Σ cost(f, m) ≤ effective_context_budget` considering only modes that consume budget (`full`, `structural_summary`, `behavioral_summary`; `reachable` does not consume budget).

The planner **must** use the following deterministic two-pass greedy algorithm (no other algorithm is permitted for v1). The algorithm is split into two passes to prevent the zero-cost `reachable` mode from dominating the efficiency sort.

**Pass 1 — budget-consuming modes:**

1. Enumerate all (f, m) pairs where `f` is a candidate and `m ∈ {full, structural_summary, behavioral_summary}` is a load mode `f` is eligible for under §7.5. `reachable` is **excluded** from this pass.
2. Sort pairs in descending order of `efficiency(f, m) = score(f) · mode_weight(m) / max(cost(f, m), 1)`. Ties break by ascending `cost(f, m)`, then by ascending normalized repository-relative path (§14), then by `load_mode` priority `full > structural_summary > behavioral_summary`.
3. Iterate through the sorted list. For each pair `(f, m)`:
   - If `f` already has an assigned mode, skip.
   - If the pair's `cost(f, m)` is ≤ remaining effective context budget, assign `m` to `f` and deduct the cost.
   - Else skip.

**Pass 2 — reachable assignment:**

4. Enumerate all candidates `f` that (a) did not receive an assignment in Pass 1, (b) are eligible for `reachable` under §7.5.4 (which includes `plausibly_relevant` files and files demoted from higher modes due to budget exhaustion).
5. Sort these candidates by ascending normalized repository-relative path (§14).
6. Assign `reachable` to each. `reachable` does not consume budget.

After both passes, the assigned set is the final selection. Files eligible for neither pass are excluded.

Dynamic programming, branch-and-bound, or any other non-greedy optimizer is forbidden in v1. This guarantees bit-identical selections across implementations.

#### 7.6.2.2 Preference Order

The objective function above already encodes the preference order: high-relevance full loads dominate, summaries serve as secondary context, and reachable entries cover peripheral items without consuming budget.

### 7.6.3 Reserved Headroom

The planner must reserve configurable budget for:

- downstream system instructions
- agent thoughts/reasoning
- command outputs
- follow-up expansion

### 7.6.4 Oversized Files

If a `highly_relevant` file (§7.5.0) is too large to include in full:

- it must be summarized rather than silently dropped,
- and the manifest must indicate why (e.g., `demotion_reason: "size_band=large"` or `demotion_reason: "budget_insufficient"`).

### 7.6.5 Budget Underflow

If the effective context budget (after §7.6.3 reservations) is smaller than the estimated token cost of even the smallest viable summary of the highest-scoring candidate:

- the planner must not silently emit an empty or near-empty manifest;
- it must emit an `oversized_primary_context` gap with severity `blocking`;
- it must mark the manifest with `"incomplete": true` at the top level;
- it must exit non-zero regardless of whether `--fail-on-gaps` or `--min-feasibility` were set;
- the manifest must still be emitted (JSON and/or Markdown) so that the failure is auditable.

---

## 7.7 Gap Detection

### 7.7.1 Required Gap Categories

v1 must support at least:

- missing\_spec
- missing\_tests
- missing\_config\_context
- unresolved\_symbol\_dependency
- ambiguous\_ownership
- missing\_runtime\_path
- missing\_external\_contract
- oversized\_primary\_context
- task\_underspecified

### 7.7.2 Gap Severity

Gaps shall include severity:

- info
- warning
- blocking

### 7.7.3 Gap Detection Rules

Gap detection must be rule-based and explainable. v1 must implement **all** of the following rules. Each rule produces at most one gap instance per planning run unless noted otherwise.

| Gap type                      | Trigger rule                                                                                                                                                                                                                                                                           | Default severity |
|-------------------------------|------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|------------------|
| `missing_spec`                | Action type is `feature`, `refactor`, `migration`, or `investigation` **and** the repository contains no file matching `SPEC.md`, `specs/**/SPEC.md`, `docs/spec*.md`, or `AGENTS.md`.                                                                                                   | warning          |
| `missing_tests`               | Action type is `feature`, `bugfix`, `refactor`, or `migration` **and** no file ending in `_test.go` (Go) — or language-appropriate test filename (`*.test.ts`, `*_test.py`) — was assigned any load mode with `score(f) ≥ 0.50`.                                                        | warning          |
| `missing_config_context`      | Task anchors overlap with any of `config`, `env`, `environment`, `settings`, `flag`, `flags`, `secret`, `database`, `db`, `port`, `host`, `url`, `token` **and** no file with `s_config > 0` (§7.4.2.1) was selected.                                                                    | warning          |
| `unresolved_symbol_dependency`| The index contains at least one Go file **and** the task text contains a token matching `^[A-Z][A-Za-z0-9_]{2,}$` (likely type/identifier) **and** no Go file in the index exports that identifier or a case-insensitive match. Emit one gap per unresolved symbol, ordered by ascending byte-wise comparison of the symbol string, and truncated to at most the first 5. This rule is suppressed when the index contains zero Go files (since symbol extraction is Go-only in v1). |
| `ambiguous_ownership`         | The highest-scoring selected file's Go package contains ≥ 2 other files with score ≥ 0.60 **and** no single file in the package has score ≥ 0.80 **and** action type is `bugfix`, `feature`, or `refactor`.                                                                              | info             |
| `missing_runtime_path`        | The index contains at least one Go file **and** action type is `feature`, `bugfix`, or `migration` **and** no selected Go file has any `io:*` side-effect tag (§12.2) **and** task anchors overlap with `request`, `handler`, `route`, `endpoint`, `server`, `db`, `query`, `write`, `read`, `send`, `publish`, `consume`. Suppressed when the index contains zero Go files. | warning |
| `missing_external_contract`   | Task anchors overlap with `api`, `rpc`, `grpc`, `openapi`, `graphql`, `protobuf`, `proto`, `interface`, `contract`, `schema` **and** no file with extension in `{.proto, .graphql, .yaml, .yml, .json}` matching `*openapi*`, `*swagger*`, `*schema*`, or `*api*` was selected.         | warning          |
| `oversized_primary_context`   | Budget underflow occurred (§7.6.5), **or** a `highly_relevant` file was demoted from `full` solely due to size band `large` (§7.5.0) **or** budget insufficiency. Emit with `blocking` severity only when the underflow condition in §7.6.5 fires; otherwise `warning`.                 | warning/blocking |
| `task_underspecified`         | Task has fewer than 2 anchors **or** resolves to action type `unknown` **or** no candidate reaches `score ≥ 0.60`.                                                                                                                                                                     | warning          |

Severity may be upgraded to `blocking` when `--fail-on-gaps` is set and the gap type is configured as blocking in `.aperture.yaml` under `gaps.blocking: [<type>, ...]`. All rule triggers, inputs, and thresholds above are mandatory — implementations may not add or remove rules in v1.

### 7.7.4 Gap Recommendations

Each gap should include a concrete recommendation such as:

- load file X
- inspect package Y
- ask user question Z
- search for missing contract
- lower confidence due to absence of tests

---

## 7.8 Feasibility Analysis

### 7.8.1 Feasibility Output

The planner must emit a feasibility section.

It must not be a magic-number nonsense box with no explanation.

### 7.8.2 Required Feasibility Components

Manifest must include:

- numerical score from 0.0 to 1.0
- textual assessment
- contributing positives
- contributing negatives
- blocking conditions
- confidence rationale

#### 7.8.2.1 Feasibility Algorithm

Feasibility is a deterministic weighted combination of four sub-signals, each in `[0.0, 1.0]`, clamped:

```
feasibility = clamp01(
    0.40 * coverage
  + 0.25 * anchor_resolution
  + 0.20 * task_specificity
  + 0.15 * budget_headroom
)
- gap_penalty
```

- `coverage`: `min(1.0, (count_full + 0.5 * count_structural + 0.3 * count_behavioral) / max(3, expected_primary_files))` where `expected_primary_files` is the task-type baseline from the table below.
- `anchor_resolution`: fraction of task anchors (§7.3.2) that appear in at least one selected file's symbol table, path, or content token bag.
- `task_specificity`: `1.0` if the task has ≥ 3 anchors, a resolved action type in {`bugfix`, `feature`, `refactor`, `test-addition`, `migration`}, and ≥ 1 explicit file/path mention; `0.7` if it has ≥ 2 anchors and a resolved action type; `0.4` if it has ≥ 1 anchor; `0.1` otherwise (and action type defaults to `unknown`).
- `budget_headroom`: `clamp01((effective_context_budget − estimated_selected_tokens) / effective_context_budget)`.
- `gap_penalty`: `0.05 * count_gaps_warning + 0.20 * count_gaps_blocking`, capped at `0.50`.

Expected-primary-files baseline by action type:

| Action type       | `expected_primary_files` |
|-------------------|--------------------------|
| `bugfix`          | 3                        |
| `feature`         | 4                        |
| `refactor`        | 5                        |
| `test-addition`   | 2                        |
| `documentation`   | 2                        |
| `investigation`   | 3                        |
| `migration`       | 4                        |
| `unknown`         | 3                        |

The `positives`, `negatives`, and `blocking_conditions` fields must enumerate the concrete sub-signal contributions (with numeric values) that drove the score — not prose guesses. If `count_gaps_blocking > 0`, feasibility must be clamped to at most `0.40`.

### 7.8.3 Feasibility Interpretation

Suggested bands:

- `0.85 - 1.00`: high feasibility
- `0.65 - 0.84`: moderate feasibility
- `0.40 - 0.64`: weak feasibility
- `< 0.40`: poor feasibility

This scoring is advisory and must be explainable.

### 7.8.4 Fail Conditions

If configured, the command must exit non-zero when:

- blocking gaps exist
- feasibility below threshold
- manifest generation fails

---

## 7.9 Manifest Generation

### 7.9.1 Required Output Formats

v1 must support:

- JSON
- Markdown

### 7.9.2 JSON Manifest Requirements

JSON output must include:

- schema version
- manifest id
- manifest hash
- task
- repo fingerprint
- budget
- selections
- reachable
- exclusions
- gaps
- feasibility
- generation metadata

### 7.9.3 Markdown Manifest Requirements

Markdown output must be readable by humans and usable as agent instructions.

It should include sections:

- task summary
- planning assumptions
- selected full context
- selected summaries
- reachable context
- gaps
- feasibility
- token accounting
- usage instructions

### 7.9.4 Deterministic Hashing

The manifest hash is `sha256` over the UTF-8 bytes of the **normalized manifest document**.

Normalization rules (all must hold):

1. JSON object keys sorted lexicographically (byte-wise) at every level.
2. Array ordering preserved as produced by the planner's deterministic selection logic (§14).
3. The following fields are **excluded** from the hash input: `manifest_hash`, `manifest_id`, `generated_at`, `generation_metadata.aperture_version`, `generation_metadata.host`, `generation_metadata.pid`, `generation_metadata.wall_clock_started_at`. `aperture_version` is excluded because developers running `go build` (dev builds marked `"dev"`) must produce hashes that match CI's stamped builds for the same source tree; algorithm identity is captured instead by `generation_metadata.selection_logic_version` (a constant baked into the binary, incremented only when selection/scoring rules change). `manifest_id` is derived deterministically from the manifest hash as `"apt_" + manifest_hash[0:16]` (first 16 hex characters), so it is stable across equivalent runs despite being excluded from the hash input.
4. All other generation metadata (tool version, selection-logic version, config digest) is **included** in the hash input.
5. Numbers emitted in canonical form (integers without decimal point; floats using Go `strconv.FormatFloat(f, 'f', -1, 64)`).
6. Strings emitted with escaped control characters per RFC 8259; no trailing whitespace; LF line endings if the normalized form is pretty-printed (the hash is computed over the compact form regardless).
7. The hash is computed over the **compact** (no-whitespace) JSON form, not the human-facing pretty-printed output.

Equivalent runs with identical:

- task text
- repo fingerprint
- config (after resolution)
- selection logic version
- aperture version
- tokenizer identity (§7.6.1.1)

must produce identical manifest hash.

---

## 7.10 Agent Integration

### 7.10.1 General Principle

Aperture must be agent-agnostic in core architecture.

### 7.10.2 Claude Code Integration

The system should support generating a manifest/instruction bundle suitable for Claude Code startup or wrapper use.

### 7.10.3 Codex Integration

The system should support generating a manifest/instruction bundle suitable for Codex CLI or similar agent startup.

### 7.10.4 Run Wrapper

`aperture run <agent> TASK.md` should:

1. generate a plan,
2. validate thresholds,
3. materialize output artifacts,
4. invoke the selected downstream command using a configurable adapter.

v1 must ship a **fully functional** built-in adapter for `claude` (invoking the `claude` CLI) that conforms to §7.10.4.1 and can be verified end-to-end against the integration tests in §18.3.

The `claude` adapter works as follows:

1. Aperture produces a merged prompt file at `.aperture/manifests/run-<manifest_id>.md` containing: (a) the Markdown manifest body (§7.9.3), followed by `---`, followed by (b) the user's task text verbatim.
2. The adapter invokes `claude` with the merged prompt piped on stdin (non-interactive mode, default): `claude --print --permission-mode bypassPermissions < run-<manifest_id>.md`. When the user sets `agents.claude.mode: interactive`, `claude` is invoked with the merged prompt as the initial message argument.
3. For built-in `claude` and `codex` adapters, `pass_task_as_arg` defaults to `false` because the task text is already included in the merged prompt file. Appending the task path again would cause duplicate prompting. Users may override to `true` via `.aperture.yaml` only if they have replaced the default `command` with a wrapper script that expects a separate task argument.
4. `APERTURE_MANIFEST_PATH` still points to the JSON manifest for auditability; the prompt file path is exposed as `APERTURE_PROMPT_PATH`.

The built-in `codex` adapter is optional in v1; when present it must also conform to §7.10.4.1 and use an analogous merged-prompt mechanism. User-declared custom adapters in config must conform to §7.10.4.1 and are responsible for deciding how to pass manifest content to their target agent (via the paths exposed in `APERTURE_MANIFEST_PATH`, `APERTURE_MANIFEST_MARKDOWN_PATH`, or `APERTURE_PROMPT_PATH`). Stubbed adapter execution is not acceptable for the `claude` adapter in v1.

#### 7.10.4.1 Adapter Contract

An adapter is an executable resolved from the config block `agents.<name>.command` (see §9.1.2 for the schema). The built-in defaults are:

- `claude`: `command: claude` — the Anthropic-published Claude Code CLI (https://github.com/anthropics/claude-code), resolvable on the user's `$PATH`.
- `codex`: `command: codex` — the OpenAI Codex CLI, resolvable on the user's `$PATH`.

When `aperture run` invokes an adapter, it must:

1. Resolve an absolute path to the written manifest (JSON form). The manifest must be written under the config-resolved `output.directory` (default `.aperture/manifests/`, see §9.1) as `manifest-<manifest_hash>.json`. The file is **persisted** after the adapter exits so the run is auditable and reproducible; it is not a tempfile. If `--out <path>` was supplied on the CLI, that path takes precedence and is the absolute path passed via `APERTURE_MANIFEST_PATH`.
2. Set the following environment variables in the adapter's process:
   - `APERTURE_MANIFEST_PATH` — absolute path to the JSON manifest.
   - `APERTURE_MANIFEST_MARKDOWN_PATH` — absolute path to the Markdown manifest if one was written; unset otherwise.
   - `APERTURE_TASK_PATH` — absolute path to the resolved task file. When the task was supplied inline (via `-p`), Aperture must write it to a tempfile under `$TMPDIR/aperture-task-<manifest_id>.txt`, pass that path, and delete the tempfile after the adapter process exits (success or failure). Tempfile cleanup must also run on the following termination conditions for the Aperture process itself: receipt of `SIGINT`, `SIGTERM`, `SIGHUP` (registered via a signal handler that deletes the tempfile before exiting), and panics recovered in `main` (via `defer`). Aperture must not rely on `SIGKILL` cleanup — under `SIGKILL` the tempfile may leak, which is acceptable. When the task came from a user-supplied file, that file's path is passed as-is and must not be deleted.
   - `APERTURE_REPO_ROOT` — absolute path to the repository root used for planning.
   - `APERTURE_MANIFEST_HASH` — the hex `sha256` hash from §7.9.4.
   - `APERTURE_VERSION` — the Aperture build version.
   - `APERTURE_PROMPT_PATH` — absolute path to the merged prompt file `run-<manifest_id>.md` when produced by a built-in adapter (§7.10.2–3); unset for custom adapters unless they choose to emit one.
3. Pass the task path as the final positional argument to the adapter (`<adapter> [configured-args...] <TASK_PATH>`), unless the adapter's config sets `pass_task_as_arg: false`.
4. Stream the adapter's stdout and stderr through unmodified; do not capture.
5. Propagate the adapter's exit code as Aperture's exit code, unless threshold validation in step 2 already failed (in which case Aperture exits non-zero before invoking the adapter).

Built-in adapters for `claude` and `codex` must conform to this contract. Custom adapters may be declared in config under `agents.<name>` with fields `command` (string), `args` (string array), and `pass_task_as_arg` (bool, default true).

---

## 7.11 Caching

### 7.11.1 Repository Analysis Cache

Aperture should cache repository analysis artifacts to avoid repeated expensive indexing.

### 7.11.2 Cache Invalidation

Cache invalidation should consider:

- file modification times
- file size changes
- content hashes for critical files
- tool version changes
- config changes

### 7.11.3 Output Directory

Default working directory:

```text
.aperture/
```

Potential contents:

- index/
- cache/
- manifests/
- logs/
- summaries/

---

## 8. Non-Functional Requirements

## 8.1 Language and Runtime

The system must be implemented in Go.

### 8.1.1 Dependency Philosophy

Standard library first. New external (non-stdlib) dependencies require that at least one of the following be true, documented in the PR that introduces them:

1. The dependency implements a well-specified format Aperture must emit or parse (e.g., YAML, TOML) that is impractical to reimplement correctly from scratch.
2. The dependency provides a tokenizer table (§7.6.1.1) that must be embedded.
3. The dependency replaces code that would exceed 500 lines of in-tree reimplementation.

v1 explicitly forbids the following dependency categories: ORMs, logging frameworks beyond `log/slog`, HTTP client libraries beyond `net/http`, and any package requiring cgo. Violations must be caught by `golangci-lint` depguard config committed to the repo.

### 8.1.2 Go Version

Target a modern stable Go release supported by current toolchains.

---

## 8.2 Performance

Performance targets are measured on a **standardized benchmark harness** rather than loose hardware descriptions:

- The acceptance-test suite (§19) must include a `make bench` target that runs the planning pipeline against the three reference repository fixtures committed under `testdata/bench/{small,medium,large}/`.
- The target CI runner for v1 is a GitHub Actions `ubuntu-latest` x86-64 runner (currently 4 vCPU, 16 GiB RAM), and secondarily a macOS `macos-14` runner (Apple M-series). Targets below apply when the harness is executed on either of these runners with no other concurrent benchmark jobs.
- Targets are wall-clock p95 over 10 consecutive runs with a warm filesystem cache but a cold Aperture cache for "cold plan" measurements, and a warm Aperture cache for "warm plan" measurements.
- On dedicated hardware (a developer workstation matching the reference profile, running the benchmark harness with no competing load), regressions of more than 20% over the target must fail the local `make bench` run.
- On shared CI runners, where neighbor-noise variance is well-documented, the benchmark job emits timings for observability but does **not** fail on a single regressed run. A CI regression gate fires only when three consecutive runs on the same commit median a regression of more than 50% over the target; single outliers are recorded as warnings, not failures.

Developers running `make bench` locally on faster hardware are expected to meet the targets trivially; developers on significantly slower hardware should treat the CI measurement as authoritative.

Repository size bands:

- `small` repo: ≤ 1,000 source files, ≤ 50 MiB total indexed bytes.
- `medium` repo: ≤ 10,000 source files, ≤ 500 MiB total indexed bytes.
- `large` repo: anything above `medium` (no hard target in v1).

Targets:

- Cold plan on a `small` repo: **≤ 10 s**.
- Cold plan on a `medium` repo: **≤ 60 s**.
- Warm-cache plan on a `small` repo: **≤ 1 s**.
- Warm-cache plan on a `medium` repo: **≤ 5 s**.

These targets are v1 requirements for the acceptance tests in §19. Misses must be reported as regressions, not excused as environmental noise.

---

## 8.3 Determinism

Equivalent inputs must produce equivalent outputs. Ordering of selections must be stable. Scores must be stable. Hashes must be stable.

---

## 8.4 Observability

The tool must support:

- debug logging
- verbose mode
- explain mode for selection rationale

### 8.4.1 Explain Mode

Explain mode should show:

- why files were selected
- why load modes were assigned
- why files were excluded or demoted
- which rules triggered gaps
- how budget was spent

---

## 8.5 Reliability

The tool must fail clearly on:

- unreadable inputs
- invalid repo root
- unsupported output path
- malformed config
- internal analysis errors

It must not silently emit garbage and call it strategy.

---

## 9. Configuration

## 9.1 Config File

Aperture should support a repository config file such as:

```text
.aperture.yaml
```

### 9.1.1 Example Configuration

```yaml
version: 1

defaults:
  model: claude-sonnet
  budget: 120000
  reserve:
    instructions: 6000
    reasoning: 20000
    tool_output: 12000
    expansion: 10000

exclude:
  - vendor/**
  - node_modules/**
  - dist/**
  - coverage/**
  - "**/*.min.js"

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

scoring:
  weights:
    mention: 0.25
    filename: 0.12
    symbol: 0.20
    import: 0.12
    package: 0.10
    test: 0.08
    doc: 0.07
    config: 0.06

gaps:
  blocking: [oversized_primary_context]

agents:
  claude:
    command: claude
    args: []
    pass_task_as_arg: false
    mode: non-interactive
  codex:
    command: codex
    args: []
    pass_task_as_arg: false
  my-custom-agent:
    command: /usr/local/bin/my-agent
    args: ["--mode", "plan"]
    pass_task_as_arg: true
```

### 9.1.2 Agents Block Schema

The `agents` block maps agent names (the `<name>` argument to `aperture run <name>`) to adapter configurations. Each entry must conform to:

| Field              | Type           | Required | Default | Description                                                                                                        |
|--------------------|----------------|----------|---------|--------------------------------------------------------------------------------------------------------------------|
| `command`          | string         | yes      | —       | Absolute path or `$PATH`-resolvable executable name invoked by `aperture run`.                                     |
| `args`             | string array   | no       | `[]`    | Static arguments inserted between `command` and the trailing task path.                                            |
| `pass_task_as_arg` | bool           | no       | `true` for user-declared adapters; `false` for the built-in `claude` and `codex` adapters (they deliver the task via the merged prompt file per §7.10.2–3). | When `true`, the resolved task path is appended as the final positional argument. When `false`, it is omitted.      |
| `env`              | map[string]string | no    | `{}`    | Extra environment variables, merged on top of the base `APERTURE_*` variables from §7.10.4.1.                      |

Built-in defaults for `claude` and `codex` are equivalent to the config shown above (`command: claude` and `command: codex`, both with empty `args` and `pass_task_as_arg: true`) and may be overridden by the user. `aperture run <name>` must fail with a clear error (§16) if `<name>` is not present in the resolved agent map.

---

## 10. Internal Architecture

## 10.1 Main Components

Suggested package/module boundaries:

- `cmd/aperture`
- `internal/config`
- `internal/task`
- `internal/repo`
- `internal/index`
- `internal/lang/goanalysis`
- `internal/relevance`
- `internal/summary`
- `internal/budget`
- `internal/gaps`
- `internal/feasibility`
- `internal/manifest`
- `internal/cache`
- `internal/agent`
- `internal/cli`

### 10.1.1 Responsibilities

- `task`: parse raw task into structured form
- `repo`: repo discovery and file walking
- `index`: repository-wide index structures
- `goanalysis`: Go AST extraction
- `relevance`: candidate scoring
- `summary`: summary artifact generation
- `budget`: token estimation and slice fitting
- `gaps`: rule-based missing info detection
- `feasibility`: score and rationale generation
- `manifest`: output schema and hashing
- `agent`: adapter generation/wrappers
- `cache`: persistent repo analysis cache

---

## 11. Data Model

## 11.1 Manifest JSON Shape

The JSON example near the end of this section is **illustrative**. The **normative** schema for the manifest is the field catalogue table below, together with the semantics in §6 and §7 to which each row links. The catalogue is the authoritative source of truth: any column (type, required, description) is binding on implementations.

For tooling convenience, Aperture must also ship a JSON Schema file at `schema/manifest.v1.json` in the Aperture repository that mechanically encodes the same rules as the field catalogue. When the two disagree, the catalogue wins and the schema file is considered buggy and must be corrected. Every `plan` run must validate its own output against the embedded schema file before writing it (validation failure is exit code `6` per §16).

The field catalogue:

| Path                                       | Type                          | Required | Description                                                                     |
|--------------------------------------------|-------------------------------|----------|---------------------------------------------------------------------------------|
| `schema_version`                           | string                        | yes      | Semantic version of the manifest schema (`"1.0"` for v1).                        |
| `manifest_id`                              | string                        | yes      | `"apt_" + manifest_hash[0:16]` (§7.9.4).                                          |
| `manifest_hash`                            | string                        | yes      | `"sha256:" + hex` of the normalized manifest (§7.9.4).                            |
| `generated_at`                             | string (RFC 3339 UTC)         | yes      | Wall-clock timestamp; excluded from hash.                                         |
| `incomplete`                               | boolean                       | yes      | `true` iff §7.6.5 underflow triggered; else `false`.                              |
| `task`                                     | object                        | yes      | `{task_id, source, raw_text, type, objective, anchors[], expects_tests, expects_config, expects_docs, expects_migration, expects_api_contract}` (§6.1, §7.3.1). |
| `repo`                                     | object                        | yes      | `{root, fingerprint, language_hints[]}`.                                          |
| `budget`                                   | object                        | yes      | `{model, token_ceiling, reserved{}, effective_context_budget, estimated_selected_tokens, estimator, estimator_version}` (§7.6). |
| `selections`                               | array of Selection            | yes      | Sorted ascending by normalized path (§14).                                        |
| `selections[].path`                        | string                        | yes      | Normalized repo-relative path.                                                    |
| `selections[].kind`                        | string                        | yes      | `"file"` in v1; reserved for future `"symbol"`.                                   |
| `selections[].load_mode`                   | string                        | yes      | One of `full`, `structural_summary`, `behavioral_summary`.                        |
| `selections[].relevance_score`             | number                        | yes      | In `[0.0, 1.0]`.                                                                  |
| `selections[].score_breakdown`             | array of object               | yes      | Per §13.1.2; one entry per scoring factor defined in §7.4.2.1 whose `signal > 0`. Factors with `signal == 0` must be omitted (not emitted with zero). Entries sorted in the factor declaration order of §7.4.2.2. Each entry: `{factor, signal, weight, contribution}`. |
| `selections[].estimated_tokens`            | integer                       | yes      | From the recorded estimator.                                                      |
| `selections[].rationale`                   | array of string               | yes      | Human-readable reason strings; stable ordering.                                   |
| `selections[].demotion_reason`             | string \| null                | no       | Present when a file was demoted from `full` (§7.6.4).                             |
| `selections[].side_effects`                | array of string \| null       | no       | Side-effect tags (§12.2) for Go files; `null` for non-Go.                         |
| `reachable`                                | array of object               | yes      | `{path, relevance_score, reason, score_breakdown[]}` sorted by ascending path. `score_breakdown` follows the same shape as for selections. |
| `exclusions`                               | array of object               | yes      | `{path, reason}` for files excluded with reason `default_pattern`, `user_pattern`, `binary`, `oversize_cutoff`, or `hidden_dir`. |
| `gaps`                                     | array of Gap                  | yes      | `{id, type, severity, description, evidence[], suggested_remediation[]}` (§7.7). A gap is considered "blocking" iff its `severity == "blocking"`; there is no separate `blocking` boolean field. |
| `feasibility`                              | object                        | yes      | `{score, assessment, positives[], negatives[], blocking_conditions[], sub_signals{}}` (§7.8.2.1). |
| `generation_metadata`                      | object                        | yes      | `{aperture_version, selection_logic_version, config_digest, side_effect_tables_version, host, pid, wall_clock_started_at}` — see §11.1.1 for field semantics. |

Fields inside `generation_metadata` that are excluded from the hash input (per §7.9.4): `aperture_version`, `host`, `pid`, `wall_clock_started_at`. Fields that are included: `selection_logic_version`, `config_digest`, `side_effect_tables_version`.

### 11.1.1 Generation Metadata Field Semantics

- `aperture_version` — exact Aperture build semver (e.g., `"1.0.0"`) or `"dev"` for unstamped builds. Recorded in the manifest for operator diagnostics; **excluded** from the hash input (§7.9.4) so that the same source code produces identical hashes whether built via `go build` (dev) or `make build` (stamped).
- `selection_logic_version` — a hard-coded string baked into the Aperture binary identifying the version of the selection algorithm in §7.6.2.1 and scoring formula in §7.4.2. Incremented whenever the spec revises scoring, tie-break, or selection rules in a way that would change manifests for identical inputs. v1 value: `"sel-v1"`. **Included** in the hash input.
- `config_digest` — `"sha256:" + hex` of the compact JSON representation of the fully-resolved effective configuration (after merging defaults, `.aperture.yaml`, and CLI overrides), with keys sorted lexicographically. Includes the resolved scoring weights, reserve values, exclusion set, and agent map. Does not include file paths to the config sources.
- `side_effect_tables_version` — current fixed value: `"side-effect-tables-v1"` per §12.2.
- `host` — short host identifier (e.g., `uname -n` result); excluded from hash.
- `pid` — Aperture process PID; excluded from hash.
- `wall_clock_started_at` — RFC 3339 UTC timestamp of planning start; excluded from hash.

The JSON example below is non-normative; the normative contract is the field catalogue above and the JSON Schema at `schema/manifest.v1.json`.

```json
{
  "schema_version": "1.0",
  "manifest_id": "apt_a1b2c3d4e5f60718",
  "manifest_hash": "sha256:a1b2c3d4e5f60718a1b2c3d4e5f60718a1b2c3d4e5f60718a1b2c3d4e5f60718",
  "generated_at": "2026-04-17T00:00:00Z",
  "incomplete": false,
  "task": {
    "task_id": "tsk_9f8e7d6c5b4a3210",
    "source": "TASK.md",
    "raw_text": "Add OAuth refresh handling to the GitHub provider",
    "type": "feature",
    "objective": "Add OAuth refresh handling to the GitHub provider",
    "anchors": ["add", "github", "handling", "oauth", "provider", "refresh"],
    "expects_tests": true,
    "expects_config": false,
    "expects_docs": false,
    "expects_migration": false,
    "expects_api_contract": false
  },
  "repo": {
    "root": "/repo",
    "fingerprint": "sha256:…",
    "language_hints": ["go", "markdown"]
  },
  "budget": {
    "model": "claude-sonnet-4-6",
    "token_ceiling": 120000,
    "reserved": {
      "instructions": 6000,
      "reasoning": 20000,
      "tool_output": 12000,
      "expansion": 10000
    },
    "effective_context_budget": 72000,
    "estimated_selected_tokens": 8200,
    "estimator": "heuristic-3.5",
    "estimator_version": "v1"
  },
  "selections": [
    {
      "path": "internal/auth/oauth.go",
      "kind": "file",
      "load_mode": "full",
      "relevance_score": 0.98,
      "score_breakdown": [
        {"factor": "mention", "signal": 1.0, "weight": 0.25, "contribution": 0.25},
        {"factor": "filename", "signal": 0.6, "weight": 0.12, "contribution": 0.072},
        {"factor": "symbol", "signal": 1.0, "weight": 0.20, "contribution": 0.20}
      ],
      "estimated_tokens": 4200,
      "rationale": ["direct keyword match", "central auth implementation"],
      "side_effects": ["io:network"]
    },
    {
      "path": "internal/providers/github/client.go",
      "kind": "file",
      "load_mode": "full",
      "relevance_score": 0.90,
      "score_breakdown": [
        {"factor": "package", "signal": 1.0, "weight": 0.10, "contribution": 0.10},
        {"factor": "import", "signal": 1.0, "weight": 0.12, "contribution": 0.12}
      ],
      "estimated_tokens": 3500,
      "rationale": ["provider implementation", "import adjacency"],
      "side_effects": ["io:network"]
    },
    {
      "path": "internal/store/token_store.go",
      "kind": "file",
      "load_mode": "structural_summary",
      "relevance_score": 0.74,
      "score_breakdown": [
        {"factor": "symbol", "signal": 0.5, "weight": 0.20, "contribution": 0.10}
      ],
      "estimated_tokens": 500,
      "rationale": ["token persistence likely relevant"],
      "side_effects": ["io:database"]
    }
  ],
  "reachable": [
    {
      "path": "docs/auth.md",
      "relevance_score": 0.42,
      "reason": "supporting documentation",
      "score_breakdown": [
        {"factor": "doc", "signal": 0.6, "weight": 0.07, "contribution": 0.042}
      ]
    }
  ],
  "exclusions": [
    {"path": "node_modules/**", "reason": "default_pattern"}
  ],
  "gaps": [
    {
      "id": "gap-1",
      "type": "missing_tests",
      "severity": "warning",
      "description": "No refresh-path integration tests were selected.",
      "evidence": ["feature task in auth subsystem", "no matching test file above threshold"],
      "suggested_remediation": ["inspect internal/auth/oauth_test.go"]
    }
  ],
  "feasibility": {
    "score": 0.82,
    "assessment": "Moderate feasibility",
    "positives": ["coverage=0.75", "anchor_resolution=0.83"],
    "negatives": ["gap_penalty=0.05"],
    "blocking_conditions": [],
    "sub_signals": {
      "coverage": 0.75,
      "anchor_resolution": 0.83,
      "task_specificity": 0.7,
      "budget_headroom": 0.88,
      "gap_penalty": 0.05
    }
  },
  "generation_metadata": {
    "aperture_version": "1.0.0",
    "selection_logic_version": "sel-v1",
    "config_digest": "sha256:…",
    "side_effect_tables_version": "side-effect-tables-v1",
    "host": "runner-01",
    "pid": 1234,
    "wall_clock_started_at": "2026-04-17T00:00:00Z"
  }
}
```

---

## 12. Summary Generation Requirements

## 12.1 Structural Summary

For code files, structural summary should include:

- package/module
- key types
- key interfaces
- exported/unexported functions relevant to task
- imports of note
- test relationship if known

## 12.2 Behavioral Summary

Behavioral summary in v1 is a **statically-derived, rule-based** artifact. It must not depend on an LLM (see §12.3). It must include only facts extractable from static analysis and filesystem metadata:

- imported packages (for Go: full import paths; for other languages: file-level import statements where parseable)
- **side-effect signals** — tags emitted when the file imports an import path that matches the per-tag table below. Matching semantics:

  1. An entry may be a **bare import path** (e.g., `net/http`, `time`) which matches that exact path and any descendant subpackage (`net/http/httputil`, etc.). Matching on a "path segment boundary" is required: `net/http` matches `net/http` and `net/http/x`, but does **not** match `net/httptest` (different segment).
  2. An entry may carry an explicit `!excludes:` list to carve out sub-paths that should not inherit the tag. For example, the `io:filesystem` entry `os (excludes: os/exec)` matches `os` and any `os/*` descendant **except** `os/exec`.
  3. When multiple tables match a single import path, all matching tags are emitted. An import path can carry zero, one, or many tags.
  4. Matching is evaluated after Aperture has resolved the import path to its canonical Go form (no aliasing considered).

The v1 tables:

| Tag               | Import path prefixes (Go)                                                                                                                                   |
|-------------------|-------------------------------------------------------------------------------------------------------------------------------------------------------------|
| `io:filesystem`   | `os` (excludes: `os/exec`), `io/fs`, `io/ioutil`, `path`, `path/filepath`, `embed`                                                                            |
| `io:process`      | `os/exec`, `syscall`, `golang.org/x/sys`                                                                                                                      |
| `io:network`      | `net`, `net/http`, `net/url`, `net/rpc`, `net/smtp`, `net/textproto`, `net/mail`, `net/httptest`, `google.golang.org/grpc`, `nhooyr.io/websocket`              |
| `io:database`     | `database/sql`, `database/sql/driver`, `github.com/jackc/pgx`, `github.com/lib/pq`, `github.com/go-sql-driver/mysql`, `github.com/mattn/go-sqlite3`, `go.mongodb.org/mongo-driver`, `github.com/redis/go-redis`, `github.com/redis/redis` |
| `io:time`         | `time` (all usages)                                                                                                                                           |
| `io:randomness`   | `math/rand`, `math/rand/v2`, `crypto/rand`                                                                                                                    |
| `io:logging`      | `log`, `log/slog`, `go.uber.org/zap`, `github.com/sirupsen/logrus`, `github.com/rs/zerolog`                                                                   |

These tables are the v1 canonical sets and apply **only to Go files**. Side-effect tagging for non-Go languages is explicitly out of scope for v1: non-Go files receive an empty `side_effects` tag set. Per-language tables for TypeScript, JavaScript, Python, etc. are a Future Enhancement (§21).

The tables are part of the scoring/summary contract and must not be silently extended by an implementation; additions require a spec revision. The current table version (`side-effect-tables: v1`) must be recorded in the manifest and is an input to the manifest hash (§7.9.4).
- exported API surface (exported symbol names with kinds: type, interface, function, method, const, var)
- associated test files (as determined by §7.2.3)
- file size band (§7.5.0) and estimated token cost

Behavioral summary in v1 must **not** attempt to describe "responsibilities", "call paths", "edit risk", or any natural-language synthesis of file purpose. Those are explicit non-goals for v1 and are deferred to a future opt-in LLM summarization mode.

## 12.3 Summary Determinism

Summaries must be generated deterministically from file contents and the static analysis pipeline. v1 must not invoke any LLM for summary generation. If optional LLM summarization is introduced in a future version, it must be:

- disabled by default,
- opt-in via an explicit config flag (e.g., `summaries.llm.enabled: true`),
- clearly marked in the manifest (e.g., `summary_source: "llm:<model-id>"` vs. `summary_source: "static"`),
- excluded from the deterministic-hash input unless the LLM and its configuration are themselves pinned and recorded.

---

## 13. Scoring Requirements

## 13.1 Relevance Score

Each candidate must receive a score in `[0.0, 1.0]` computed by the formula and default weights defined in §7.4.2.1 and §7.4.2.2. This section is non-normative and points to the authoritative definition in §7.4.

### 13.1.1 Inputs to Scoring

See §7.4.2.1 for the enumerated per-factor signals (`s_mention`, `s_filename`, `s_symbol`, `s_import`, `s_package`, `s_test`, `s_doc`, `s_config`) and §7.4.2.2 for the default weight table. Overrides go through `.aperture.yaml` (§9.1) and must satisfy the sum-to-1.0 constraint in §7.4.2.2.

### 13.1.2 Score Transparency

Each selected or reachable file must retain per-factor score breakdown metadata (factor name, signal value, weight, weighted contribution) in the manifest's `score_breakdown` field on that selection or reachable entry (§11.1). The `rationale` field is a separate, human-readable string array (not a score breakdown) and must contain short explanations such as `"direct keyword match"` or `"import adjacency"`. `aperture explain` (§8.4.1) renders both fields to the user.

---

## 14. Deterministic Planning Rules

The same inputs must produce the same plan.

Inputs include:

- task text
- repo content
- config
- aperture version or planning engine version
- target budget parameters

When tie-breaking candidates with identical scores, the system must order them by ascending lexicographic (byte-wise) comparison of their normalized repository-relative paths. "Normalized" means: relative to the repository root, forward-slash separators on all platforms, no leading `./`, and NFC Unicode normalization. This ordering rule is mandatory everywhere determinism is required — not illustrative.

---

## 15. CLI Requirements

## 15.1 Commands

Required commands:

- `aperture plan`
- `aperture explain`
- `aperture run`
- `aperture version`

Optional but useful:

- `aperture inspect`
- `aperture cache clear`

### 15.1.1 `plan`

Generate manifest.

### 15.1.2 `explain`

Show reasoning for a prior manifest or current planning run.

### 15.1.3 `run`

Plan then execute downstream adapter.

### 15.1.4 `version`

Print version/build metadata.

---

## 16. Error Handling

The system must return non-zero exit codes on all of the following conditions. Specific exit codes for v1:

| Exit code | Condition                                                                                                    |
|-----------|--------------------------------------------------------------------------------------------------------------|
| `0`       | Success; manifest written; no threshold or gap failure.                                                      |
| `1`       | Generic unexpected failure (internal panic, unforeseen I/O error).                                           |
| `2`       | Invalid command-line arguments.                                                                              |
| `3`       | Unreadable task file (not found, permission denied, binary).                                                 |
| `4`       | Invalid repository root (not a directory, not readable).                                                     |
| `5`       | Malformed config (`.aperture.yaml` parse error, weight sum ≠ 1.0, schema validation failure).                 |
| `6`       | Manifest serialization or write failure.                                                                     |
| `7`       | Feasibility threshold failure when `--min-feasibility` or `thresholds.min_feasibility` is set and not met.   |
| `8`       | Blocking gaps exist when `--fail-on-gaps` or `gaps.blocking` triggers.                                        |
| `9`       | Budget underflow per §7.6.5.                                                                                  |
| `10`      | Embedded tokenizer tables required by the resolved model are missing from the current Aperture build (§7.6.1.1). |
| `11`      | Unknown agent name in `aperture run <agent>` that is not present in the resolved agent map (§9.1.2).          |
| `12`      | Adapter failed to start (executable not found on `$PATH`, permission denied, or exec returned an OS-level error before the adapter's own process body ran). |

When `aperture run` successfully starts the adapter but the adapter itself exits non-zero, Aperture must propagate the adapter's exit code unchanged (i.e., Aperture's own exit code equals the adapter's). Code `12` applies only to pre-exec failures. Adapter exit code `0` is propagated as `0`.

Human-readable errors must clearly state:

- what failed
- why it failed
- what the user can do next

---

## 17. Security and Privacy

## 17.1 Local-First

v1 should operate locally by default.

## 17.2 Data Handling

The system must not exfiltrate repository contents unless explicitly configured to do so in future optional features.

## 17.3 Sensitive Files

The system should allow explicit exclusion of sensitive files or directories.

---

## 18. Test Requirements

## 18.1 Unit Tests

Must cover:

- task parsing
- repo indexing
- Go analysis
- relevance scoring
- budget fitting
- gap detection
- manifest hashing
- config parsing

## 18.2 Golden Tests

Use golden files for:

- manifest output
- markdown rendering
- explain output
- deterministic ordering

## 18.3 Integration Tests

Should cover:

- small Go repo planning
- repo with docs/config/tests
- budget pressure scenarios
- oversized file handling
- missing info scenarios

## 18.4 Determinism Tests

Repeated runs with identical inputs must produce byte-equivalent normalized JSON manifest output.

---

## 19. Acceptance Criteria

A coding agent implementation is acceptable when all of the following are true:

1. `aperture plan TASK.md` works on a real Go repository.
2. The tool produces valid JSON and Markdown manifests.
3. The manifest includes load modes, scores, budget accounting, gaps, and feasibility.
4. Repository scanning and scoring are deterministic.
5. Go AST analysis is used for Go file structure extraction.
6. Large files are summarized rather than silently ignored.
7. Reachable but unloaded files are surfaced distinctly.
8. Gap detection identifies at least the required v1 categories.
9. Threshold-based failure behavior works.
10. Warm-cache performance materially improves over cold analysis on non-trivial repos.
11. Unit, integration, and golden tests pass.
12. The project builds and runs with standard Go tooling.

---

## 20. Suggested Implementation Phases

## Phase 1: Core Planning Skeleton

- CLI scaffolding
- config loading
- repo discovery
- manifest schema
- basic task parsing

## Phase 2: Go Repository Indexing

- file walker
- Go AST extraction
- package/symbol/import index
- test file linking

## Phase 3: Relevance and Budgeting

- candidate generation
- scoring engine
- token estimator
- load-mode assignment
- deterministic selection

## Phase 4: Gap Detection and Feasibility

- rule-based gap engine
- feasibility scoring
- explain mode

## Phase 5: Output and Integration

- JSON/Markdown manifests
- manifest hashing
- agent wrapper generation
- run command

## Phase 6: Cache and Hardening

- persistent cache
- golden tests
- determinism tests
- performance tuning

---

## 21. Future Enhancements (Not Required for v1)

- TypeScript AST support
- Python AST support
- symbol-level rather than file-level budgeting
- dynamic call graph hints
- git diff awareness
- explicit test-impact analysis
- optional embedding-based semantic retrieval
- IDE plugins
- agent feedback loop from failed runs
- manifest replay and comparison
- historical plan quality evaluation

---

## 22. Build and Tooling Expectations

The project should include:

- `Makefile`
- `README.md`
- `SPEC.md`
- sample config
- example repo fixtures for tests

Suggested Make targets:

- `make build`
- `make test`
- `make lint`
- `make fmt`

---

## 23. Final Notes for the Coding Agent

Do not implement Aperture as a vague RAG toy. Do not reduce it to “search plus token estimate.” Treat it like a compiler front-end for context selection.

The critical properties are:

- determinism
- explainability
- useful load modes
- honest gap detection
- reproducible manifests

If forced to choose between “clever” and “predictable,” choose predictable. Nobody needs another mysterious agent accessory with nice branding and bad judgment.

