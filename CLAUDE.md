# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project State

This repository currently contains only `specs/initial/SPEC.md` — no Go source, no `go.mod`, no Makefile, no git history. Implementation has not started. Treat SPEC.md as the binding contract; defer to it over assumptions drawn from conventional Go project layouts.

## What Aperture Is (and Isn't)

Aperture is a **pre-execution context planner** for coding agents (Claude Code, Codex, etc.). Given a task description and a repo, it outputs a deterministic, token-budgeted manifest telling a downstream agent which files to load full, summarize, or leave reachable.

It is explicitly **not** a coding agent, a RAG system, or a vector-search product. From SPEC §23: "Treat it like a compiler front-end for context selection. If forced to choose between 'clever' and 'predictable,' choose predictable."

## Non-Negotiable Properties

Every design and implementation decision must preserve:

1. **Determinism** — Identical inputs (task text, repo fingerprint, config, engine version, budget params) must produce byte-identical normalized JSON manifests and identical manifest hashes. Tie-break identical scores using normalized path ordering (SPEC §14, §7.9.4, §18.4).
2. **Explainability** — Every selection, load-mode assignment, exclusion, and gap must carry rationale metadata usable by `aperture explain` (SPEC §8.4.1, §13.1.2).
3. **Rule-based gap detection** — v1 gap detection must be rule-based and explainable, not model-driven (SPEC §7.7.3).
4. **Deterministic summaries** — Summary generation must not depend on an LLM in v1. If added later it must be opt-in and clearly flagged (SPEC §12.3).
5. **Local-first, no exfiltration** — No network calls on repo contents unless explicitly configured in a future feature (SPEC §17).

When a shortcut would violate any of these, prefer the slower predictable path.

## Architecture (per SPEC §10)

Planned package layout:

- `cmd/aperture` — CLI entry
- `internal/config` — `.aperture.yaml` loading
- `internal/task` — parse task file/prompt into structured Task (action type, anchors, keywords)
- `internal/repo` — repo root discovery, file walking, exclusions
- `internal/index` — repo-wide index (files, symbols, imports, packages)
- `internal/lang/goanalysis` — Go AST extraction (use `go/parser`, `go/ast` — not regex)
- `internal/relevance` — deterministic candidate scoring
- `internal/summary` — structural and behavioral summary artifacts (deterministic)
- `internal/budget` — token estimation, reserved headroom, slice fitting
- `internal/gaps` — rule-based gap detection (see SPEC §7.7.1 for required categories)
- `internal/feasibility` — 0.0–1.0 score with positives, negatives, blocking conditions
- `internal/manifest` — JSON/Markdown emission + deterministic hash
- `internal/cache` — persistent repo analysis cache under `.aperture/`
- `internal/agent` — adapter bundles for Claude Code, Codex
- `internal/cli` — command wiring (`plan`, `explain`, `run`, `version`)

## Data Flow

1. Parse task (file or inline) → structured Task with action type, anchors, inferred touched modules.
2. Scan repo → index (files, Go symbols/imports, test relationships, docs).
3. Build candidate set from keyword/symbol/import/package/doc/test adjacency.
4. Score each candidate (deterministic, weighted).
5. Estimate tokens for full content and summary variants.
6. Fit selection into effective budget (ceiling minus reserved instructions/reasoning/tool-output/expansion).
7. Assign each selection a LoadMode: `full`, `structural_summary`, `behavioral_summary`, or `reachable`.
8. Run gap-detection rules → list of Gaps with severity (`info`/`warning`/`blocking`) and remediation.
9. Compute feasibility score + rationale.
10. Emit Manifest (JSON and/or Markdown) with deterministic hash computed from normalized content.

See SPEC §11.1 for the authoritative manifest JSON shape.

## Load Modes (SPEC §6.6)

- `full` — raw content loaded; used for highly relevant, reasonably sized, central files.
- `structural_summary` — package/types/interfaces/functions/imports; for files where architecture > exact content.
- `behavioral_summary` — responsibilities, side effects, deps, call paths; for files where behavior > implementation.
- `reachable` — not initially loaded; surfaced as discoverable follow-up.

Oversized highly relevant files must be summarized, not silently dropped, with the reason recorded in the manifest (SPEC §7.6.4).

## CLI Surface (SPEC §4, §15)

Required commands: `plan`, `explain`, `run`, `version`. Planned flags for `plan`:

- `--repo <path>` (defaults to cwd)
- `--model <id>` / `--budget <tokens>`
- `--format json|markdown` / `--out <path>`
- `--fail-on-gaps`
- `--min-feasibility <float>`

`aperture run <agent> TASK.md` plans, validates thresholds, materializes artifacts, then invokes the downstream adapter. v1 may stub adapter execution.

## Required Gap Categories (SPEC §7.7.1)

v1 must detect at minimum: `missing_spec`, `missing_tests`, `missing_config_context`, `unresolved_symbol_dependency`, `ambiguous_ownership`, `missing_runtime_path`, `missing_external_contract`, `oversized_primary_context`, `task_underspecified`.

## Development Workflow

This repo follows the SPEC → PLAN → CODE pipeline defined in the user's global CLAUDE.md:

1. `/spec-review` — validate `specs/initial/SPEC.md` with `speccritic`.
2. `/plan` — generate phased PLAN.md, validate with `plancritic`.
3. `/implement <phase>` — one phase at a time.
4. `/phase-review` — `prism` → `realitycheck` → `clarion` → `verifier`.
5. `/commit` when a phase passes all gates.

SPEC §20 suggests six phases: (1) CLI skeleton + config + manifest schema, (2) Go indexing + AST, (3) relevance + budgeting, (4) gaps + feasibility + explain, (5) output + agent wrappers + `run`, (6) cache + golden/determinism tests + perf.

## Build & Test Commands

Not yet bootstrapped. SPEC §22 expects `Makefile` with `build`, `test`, `lint`, `fmt` targets. Until those exist, use standard Go tooling once `go.mod` is initialized:

- `go build ./...`
- `go test ./...`
- `go test -run TestName ./internal/<pkg>` — single test
- `golangci-lint run ./...` — required after any Go change (from global CLAUDE.md)

## Testing Expectations (SPEC §18)

- **Unit**: task parsing, repo indexing, Go analysis, relevance scoring, budget fitting, gap detection, manifest hashing, config parsing.
- **Golden**: manifest JSON, markdown rendering, explain output, ordering.
- **Determinism**: repeated runs on identical inputs must produce byte-equivalent normalized JSON.
- **Integration**: small Go repo, docs/config/tests present, budget pressure, oversized files, missing-info scenarios.

## Dependency Philosophy (SPEC §8.1.1)

Standard library first. External dependencies only when they materially improve correctness or velocity. Do not pull in heavy frameworks for problems the stdlib solves.

## Working Directory Convention

Aperture reads/writes under `.aperture/` in the target repo: `index/`, `cache/`, `manifests/`, `logs/`, `summaries/` (SPEC §7.11.3). Do not assume or write to other paths.

## Code Search Protocol

Use this decision tree — in order — before reading any source file:

### Structural questions → atlas (always first)
- "Where is X defined?" → `atlas find symbol X --agent`
- "What calls X?" → `atlas who-calls X --agent`
- "What does X call?" → `atlas calls X --agent`
- "What implements interface X?" → `atlas implementations X --agent`
- "Which tests cover X?" → `atlas tests-for X --agent`
- "What routes exist?" → `atlas list routes --agent`
- "What changed?" → `atlas index --since HEAD~1 && atlas stale --agent`

### Before reading a large file → summarize first
`atlas summarize file <path> --agent`
Only read the file directly if the summary is insufficient.

### Content/pattern questions → rg
- Error strings, log messages, string literals
- Comments, TODOs, inline notes
- Non-Go/TS files (YAML, SQL, Markdown)
- Unstaged files not yet indexed

### Never read source files to answer these questions
If atlas has the answer, do not use Read or Bash(cat).
Atlas is authoritative — its index is maintained by a PostToolUse hook on Write/Edit/MultiEdit.
