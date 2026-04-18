# PLAN.md

Phased implementation plan for Aperture derived from `SPEC.md`. Each phase is independently implementable, independently testable, and produces user-visible progress. Phases are ordered strictly by dependency: later phases assume the earlier phases' contracts.

All section references (e.g., §7.6.2.1) point at `SPEC.md`.

---

## Pre-Phase: Repository Bootstrap

**Scope:** one-time project setup; not a testable deliverable, but required before Phase 1 can begin.

Tasks:
- `git init`; add `.gitignore` for `.aperture/`, `bin/`, `coverage/`, `dist/`, `*.out`.
- `go mod init github.com/dshills/aperture` — target Go 1.23 (modern stable per §8.1.2).
- Commit `SPEC.md`, `PLAN.md`, `CLAUDE.md`.
- Create empty placeholder directories with `.gitkeep`: `cmd/aperture/`, `internal/`, `schema/`, `testdata/`.
- Add top-level `Makefile` with empty `build`, `test`, `lint`, `fmt`, `bench` targets; each will be filled out phase-by-phase.
- Add `.golangci.yml` with `depguard` rule enforcing §8.1.1 (forbid ORMs, non-`net/http` clients, logging frameworks beyond `log/slog`, cgo deps).

**Exit check:** `go build ./...` succeeds on an empty skeleton; `make lint` runs without configuration error.

---

## Phase 1 — CLI Skeleton, Config, Task Parsing, Manifest Schema

**Goal:** a runnable `aperture` binary that accepts `plan`, `version`, and `explain` subcommands, loads `.aperture.yaml`, parses a task file into the Task domain object (§6.1, §7.3), and emits a valid (but empty) manifest conforming to the §11.1 schema catalogue. No repo scan, no scoring, no real selections — the pipeline is stubbed with deterministic empty arrays.

**Files to create:**
- `cmd/aperture/main.go` — CLI entrypoint; wires subcommands.
- `internal/cli/root.go`, `internal/cli/plan.go`, `internal/cli/explain.go`, `internal/cli/run.go`, `internal/cli/version.go` — one file per subcommand. `run` and `explain` stub to "not yet implemented" but must parse their flags without error so integration tests can target the final surface from the start.
- `internal/config/config.go` — `.aperture.yaml` parser using `gopkg.in/yaml.v3` in strict mode (`KnownFields(true)` on the decoder). The stdlib does not ship a YAML parser; this dep is justified under §8.1.1 rule 1 ("well-specified format impractical to reimplement"). Strict-mode decoding is required so that unknown keys in `.aperture.yaml` produce exit code 5 per §16.
- `internal/config/defaults.go` — hard-coded v1 default weights (§7.4.2.2), default exclusions (§7.4.3), default agents block (§9.1.2), default reserved-tokens table (§9.1.1).
- `internal/config/validate.go` — weight-sum check (±0.001), agent-map validation, unknown-key rejection.
- `internal/task/parse.go` — deterministic task parsing: action type classifier (§7.3.1.1 table), anchor extractor (§7.3.2 union rules + stopword list), heuristic booleans (§7.3.3), `task_id` as `"tsk_" + sha256(raw_input)[:16]`. Anchor extraction must implement all four §7.3.2 union rules; in particular, rule 3 (backtick-quoted code spans in Markdown task files) is extracted via a deterministic single-pass scan that pairs the first and next unescaped backtick on each line — no external Markdown library. Input-format detection: files ending `.md`, `.markdown`, `.mdx` trigger code-span extraction; other extensions and inline strings skip rule 3.
- `internal/task/stopwords.go` — frozen stopword list.
- `internal/manifest/types.go` — Go structs mirroring the §11.1 field catalogue. Field tags for JSON. Compile-time enum types for `LoadMode`, `ActionType`, `GapType`, `GapSeverity`.
- `internal/manifest/emit.go` — JSON marshal with compact form for hashing and pretty form for writing. Markdown emission can be a stub in this phase.
- `internal/manifest/hash.go` — normalized hashing per §7.9.4 (sorted keys, canonical number format, excluded fields, compact JSON). `manifest_id = "apt_" + hash[:16]`.
- `schema/manifest.v1.json` — **complete** JSON Schema covering every row of the §11.1 field catalogue (all top-level fields, all nested selection/reachable/gap/feasibility/generation_metadata shapes, all enums). Fields populated by later phases (e.g., `side_effects`) default to empty arrays or `null` per the catalogue but are still declared in the schema. A CI test diffs the field catalogue (§11.1) against the schema file on every build; a mismatch fails the build.
- `internal/version/version.go` — build-stamped semver; `--version` output. Variables `Version`, `Commit`, `BuildDate` are package-level `var` declarations set via `-ldflags "-X github.com/dshills/aperture/internal/version.Version=$(VERSION) -X ...Commit=$(git rev-parse HEAD) -X ...BuildDate=$(date -u +%Y-%m-%dT%H:%M:%SZ)"`. The `Makefile`'s `build` target wires these ldflags automatically so plain `go build ./...` yields a development build tagged `dev` and `make build` yields a stamped binary.
- `testdata/task/simple_feature.md`, `testdata/task/bugfix_inline.txt`, `testdata/task/underspecified.md` — fixtures used from this phase onward.

**Key decisions:**
- **CLI framework:** use `github.com/spf13/cobra`. The surface (`plan`, `explain`, `run`, `version`, `cache clear`) has four subcommands with distinct flag sets and a nested `cache` group; stdlib `flag` would require enough hand-rolled dispatch that we'd be rebuilding half of Cobra. Cobra is justified under §8.1.1 rule 3 (replaces >500 lines of in-tree code). Depguard must allowlist `github.com/spf13/cobra` only (not `spf13/viper` — config loading is our own code).
- **YAML parsing:** `gopkg.in/yaml.v3` with `yaml.NewDecoder(r).KnownFields(true)`. Depguard must allowlist this exact import path; no other YAML library is permitted.
- **Task parsing is pure:** no filesystem access beyond reading the task file; no I/O on the repo yet. This keeps the §7.3.1 determinism guarantee airtight.

**Acceptance criteria:**
- `aperture version` prints a semver and build date.
- `aperture plan testdata/task/simple_feature.md --repo .` exits 0 and emits a manifest JSON that:
  - validates against `schema/manifest.v1.json`;
  - contains a populated `task` block with `task_id`, `type`, `anchors[]`, and all five `expects_*` booleans;
  - has empty `selections`, `reachable`, `gaps`, `exclusions`, and a zero-filled `budget` block;
  - has a deterministic `manifest_hash` that is byte-identical across repeated runs.
- `aperture plan` with `--format markdown --out /tmp/foo.md` writes a stub Markdown manifest with all §7.9.3 section headers.
- Config loading rejects: weights that don't sum to 1.0, unknown top-level keys, malformed YAML. Each produces exit code `5` with a clear human-readable message (§16).
- Task parsing unit tests cover every row of the §7.3.1.1 action-type table plus the `unknown` default.
- Every run produces identical `task_id` for identical input text.

**Risks / likely pitfalls:**
- Forgetting to exclude `generated_at`, `host`, `pid`, `wall_clock_started_at` from the hash input. Mitigate with a hash-determinism test that runs the same plan twice and diffs.
- YAML libraries tolerate unknown keys by default — enable strict mode explicitly.
- Action-type classification is strictly rule-order-sensitive per §7.3.1.1: the **first rule in Order** whose pattern matches anywhere in the lowercased task text wins — the *position* of the match in the text is irrelevant. Implement as an explicit ordered priority loop, not a map. Example: `"investigate why the new fix breaks"` matches both Rule 1 (`bugfix`, on `fix`) and Rule 6 (`investigation`, on `investigate`); because Rule 1 has higher priority, the resolved action type is `bugfix`. If the user wanted the task classified as `investigation`, they would omit "fix" or phrase it differently. This matches §7.3.1.1 verbatim and must not be "helpfully" reordered.

---

## Phase 2 — Repository Scanner, Go AST Indexing, Supplemental-File Detection

**Goal:** Aperture can walk the repository, build the index (§7.2.1), extract Go symbols and imports via the `go/parser`, `go/ast`, `go/token` stdlib (§7.2.3), apply exclusions (§7.4.3), compute the repo fingerprint (§6.4.1), and detect supplemental files (§7.1.3). No scoring or selection yet — the index is the deliverable.

**Files to create:**
- `internal/repo/root.go` — repo root discovery (walk up from `--repo` until `.git`, or accept the provided path as-is if `.git` is absent).
- `internal/repo/walker.go` — recursive file walker. Applies the default-exclusion globs, binary detection (NUL byte in first 8 KiB per §7.4.3), >10 MiB cutoff, and `.`-prefixed directory allowlist logic.
- `internal/repo/fingerprint.go` — compute repo fingerprint per §6.4.1 (compact JSON with sorted `files[]`, SHA-256 per file, `aperture_version` included).
- `internal/index/index.go` — canonical `Index` struct: `Files []FileEntry`, `Packages map[string]*Package`, `SupplementalFiles map[SupplementalCategory][]string`.
- `internal/index/supplemental.go` — glob-driven supplemental detection per §7.1.3 table (spec/plan/agents/readme/architecture/lint/test/build/ci).
- `internal/lang/goanalysis/parse.go` — per-file AST parse; emit `package`, `imports[]`, exported symbols (type, interface, func, method, const, var), and test-file linkage (`_test.go`).
- `internal/lang/goanalysis/symbols.go` — symbol-table shaping; case-insensitive index for the `unresolved_symbol_dependency` gap rule.
- `internal/lang/goanalysis/parse_error.go` — handle `parse_error` files per §7.2.3 fallback: record `parse_error: true`, run the minimal non-AST import scan, skip structural summaries.
- `internal/lang/goanalysis/sideeffects.go` — canonical side-effect tag tables from §12.2, including the `!excludes:` semantics (`os` matches `os/*` except `os/exec`). Version recorded as `side-effect-tables-v1`.
- `internal/repo/mtime.go` — record `mtime` as RFC 3339 UTC for every file (per updated §7.2.1).
- `testdata/fixtures/small_go/` — tiny Go project: 1 `cmd/`, 1 `internal/` package with types/functions, a `_test.go`, `go.mod`, `README.md`, `.aperture.yaml` with minimal config. Used in integration tests from here on.
- `testdata/fixtures/non_go/` — repo containing only Markdown, YAML, and shell scripts (zero Go files) to exercise §7.2.2.1.

**Key decisions:**
- **Parallelism:** parse Go files concurrently (`errgroup` with `runtime.NumCPU()` workers) but assemble the index with a deterministic post-merge sort on normalized path. Determinism is mandatory (§8.3); parallelism is not.
- **Symlinks:** do not follow. Record as excluded with reason `symlink` (extend the `exclusions[]` reason enum).
- **File-content hashing:** compute during the walk, not in a second pass. Store in `FileEntry.SHA256` so the fingerprint step is a pure aggregation.
- **Test-file linkage:** a file `foo.go` with a sibling `foo_test.go` in the same package — link them both directions for later use in `s_test`.

**Acceptance criteria:**
- `aperture plan` on `testdata/fixtures/small_go/` still emits a valid manifest, now with:
  - populated `repo.fingerprint` (`sha256:…`, 64-hex);
  - populated `repo.language_hints` (sorted, deduped);
  - `exclusions[]` containing default patterns that actually hit in the fixture (e.g., `.git/**`);
  - `generation_metadata.side_effect_tables_version = "side-effect-tables-v1"`.
- Running the same plan twice produces byte-identical `repo.fingerprint`.
- Modifying one file in the fixture changes the fingerprint; reverting restores it.
- Unit tests verify: AST parse of every exported-symbol kind (type, interface, func, method, const, var); `parse_error` fallback; side-effect tag matching with `!excludes` semantics (e.g., `os/exec` import does NOT get `io:filesystem`).
- Non-Go fixture produces an index with zero packages, zero symbols, and a valid fingerprint. No panics.

**Risks:**
- `go/parser` error recovery is lenient; make sure "partial parse" files are either fully used or fully dropped — never partially indexed.
- Windows/Unix path separator discrepancies. Normalize to forward-slash at the boundary (repo walker) and never re-insert `filepath.Separator` into manifest paths.
- Case-insensitive filesystems (macOS) can yield duplicate-looking entries after lowercasing; keep canonical case in the index, lowercase only at scoring time.

---

## Phase 3 — Scoring, Tokenization, Load-Mode Assignment, Greedy Selection

**Goal:** end-to-end context selection. This phase implements the scoring formula (§7.4.2.1), two-pass `s_import` (§7.4.2.1 post-tier fix), mandatory greedy selection (§7.6.2.1), load-mode assignment per the quantitative thresholds (§7.5.0–§7.5.4), tokenizer selection with embedded tiktoken tables (§7.6.1.1), and budget underflow handling (§7.6.5). After this phase, Aperture produces manifests that an agent could actually use.

**Files to create:**
- `internal/relevance/score.go` — single-pass scoring pipeline: `s_mention`, `s_filename`, `s_symbol`, `s_package`, `s_test`, `s_doc`, `s_config` computed in pass 1; `s_import` computed in pass 2 over pass-1 package scores (§7.4.2.1 two-pass rule).
- `internal/relevance/signals/` — one file per factor (`mention.go`, `filename.go`, `symbol.go`, `importpkg.go`, `packagepkg.go`, `test.go`, `doc.go`, `config.go`). Each exports a pure `Compute(FileEntry, Task, Index) float64`.
- `internal/relevance/breakdown.go` — produce `ScoreBreakdown []Entry` with `{factor, signal, weight, contribution}`, filtered to non-zero signals and sorted by the declaration order of §7.4.2.2 (per refined §11.1 catalogue).
- `internal/budget/tokenizer.go` — tokenizer dispatch per §7.6.1.1 (family resolution `claude-*` / `gpt-*` / `codex-*` / `o*`; unspecified → heuristic; recognized-but-unsupported → exit 10; unrecognized → heuristic).
- `internal/budget/heuristic.go` — `ceil(len(utf8_bytes)/3.5)` with upward rounding.
- `internal/budget/tiktoken/` — embedded BPE tables: `cl100k_base.go`, `o200k_base.go`, `p50k_base.go`, `r50k_base.go`. Tables baked in via `go:embed`; build-time SHA recorded per-encoding.
- `internal/budget/tiktoken/encode.go` — BPE encoder. Use `github.com/pkoukk/tiktoken-go` configured with the in-tree embedded BPE tables (§7.6.1.1 forbids runtime downloads; the library supports `NewCoreBPE` with pre-loaded ranks). Justification under §8.1.1 rule 3: a production-grade BPE implementation with the correct regex pre-tokenizer per encoding (different for `cl100k_base` vs. `o200k_base`) easily exceeds 500 lines of robust code plus the exact pattern regex that OpenAI uses. Correctness parity is verified by running the library against OpenAI's published tiktoken test vectors for all four supported encodings during Phase 3 CI.
- `internal/budget/estimate.go` — per-file token estimate for each load mode: `full` = full content through estimator; `structural_summary` = rendered summary through estimator; `behavioral_summary` = same. Headers + JSON scaffold overhead accounted separately.
- `internal/loadmode/eligibility.go` — encodes §7.5.1–§7.5.4 eligibility rules as pure predicates. Output for each file: ordered slice of eligible (load_mode) entries.
- `internal/selection/greedy.go` — mandatory two-pass greedy algorithm from §7.6.2.1. Pass 1 sorts only budget-consuming `(f, m)` pairs (modes `full`, `structural_summary`, `behavioral_summary`) by efficiency with deterministic tie-breaks, iterates once, enforces one-mode-per-file. Pass 2 takes remaining eligible candidates (plausibly-relevant files and files demoted from Pass 1 due to budget exhaustion) and assigns `reachable`, sorted by normalized path. `reachable` never enters Pass 1, which is what prevents its zero-cost from dominating the efficiency sort and starving budget-consuming modes.
- `internal/selection/underflow.go` — §7.6.5 underflow: emit `oversized_primary_context` blocking gap, set `manifest.incomplete = true`, propagate exit 9.
- `internal/summary/structural.go` — §12.1 structural summary renderer (Go only; non-Go files emit no structural summary per §7.5.2).
- `internal/summary/behavioral.go` — §12.2 deterministic behavioral summary: imports, side-effect tags (Go only), exported API surface, associated test file, size band. No prose, no "responsibilities".

**Key decisions:**
- **Tokenizer implementation:** `github.com/pkoukk/tiktoken-go`, configured to use in-tree `go:embed`-packaged BPE tables (no network, no `$HOME` lookup per §7.6.1.1). Depguard allowlists this exact import path. Correctness locked in by a CI test that runs the library over OpenAI's published tiktoken test vectors for `cl100k_base`, `o200k_base`, `p50k_base`, and `r50k_base` and asserts exact token-count parity.
- **Heuristic rounding:** `math.Ceil(float64(len)/3.5)`, never integer division which silently rounds down.
- **Score caching:** compute `score_pass1` once per file, cache in-struct; pass 2 reads from the cache. Do not re-run pass 1 for `s_import`.
- **Greedy determinism (Pass 1):** use a single `sort.Slice` with a total-order comparator implementing all four tie-break levels from §7.6.2.1 in a single `Less` function, in this exact priority:
  1. descending `efficiency = score · mode_weight / max(cost, 1)`;
  2. ascending `cost(f, m)`;
  3. ascending normalized repository-relative path (§14);
  4. `load_mode` priority `full > structural_summary > behavioral_summary`.

  A single comparator — not stacked sorts — is the only way to guarantee bit-identical output regardless of the input-slice order handed in by the walker. `reachable` does **not** appear in Pass 1 and therefore not in this comparator.
- **Greedy determinism (Pass 2):** sort the remaining eligible candidates by ascending normalized path (single-key comparator). Assign `reachable` to each in order.
- **Property-based test coverage:** shuffle the input slice 100× and assert the final assignment is identical across all permutations.

**Acceptance criteria:**
- On `testdata/fixtures/small_go/` with task "add GitHub OAuth refresh", the emitted manifest selects the right primary Go file as `full`, assigns `structural_summary` to a supporting file, and lists `.md` fixtures as `reachable`.
- `selections[].score_breakdown` contains only non-zero-signal factors, in §7.4.2.2 declaration order.
- Identical inputs → byte-identical manifest JSON (modulo hash-excluded time/host/pid fields). Test in CI.
- Budget underflow scenario: set `--budget 100` on a fixture where no summary fits; Aperture exits 9, emits a manifest with `incomplete: true` and a blocking `oversized_primary_context` gap.
- `--model gpt-4o` → `estimator: "tiktoken:o200k_base"`, `estimator_version: "<table-sha>"`. `--model` omitted → `heuristic-3.5`.
- Unit tests cover every scoring factor with hand-constructed fixtures; greedy algorithm tests cover tie-break paths.

**Risks:**
- Greedy algorithm tie-break bugs are the most common source of determinism failures. Write property-based tests (shuffle input order, assert stable output) before any other selection test.
- Tokenizer table licensing: check each embedded BPE table's license before committing. `cl100k_base` and `o200k_base` are openly distributed but confirm.
- `s_import` two-pass requires index-wide package scoring before per-file pass 2. Wiring this wrong produces correct individual scores but wrong `s_import` under certain graph shapes. Write a graph test with ≥3 packages forming a chain `A → B → C` where only `C` is task-relevant.
- Go prohibits import cycles, but a malformed vendored tree or a parse-error state could present one transiently to the indexer. The transitive-closure walk in pass 2 (`s_import` 2-hop) must detect cycles and cap recursion; add a test fixture with a synthetic import cycle (two files declaring mutual imports) and assert the walker terminates and emits a `Warn`-level log without panicking.

---

## Phase 4 — Gap Detection, Feasibility, Explain Mode

**Goal:** Aperture produces honest, rule-based gaps (§7.7) and a deterministic feasibility score with rationale (§7.8.2.1). `aperture explain` renders selection reasoning and rule traces.

**Files to create:**
- `internal/gaps/rules.go` — one function per required gap type (§7.7.1, §7.7.3 table): `missingSpec`, `missingTests`, `missingConfigContext`, `unresolvedSymbolDependency`, `ambiguousOwnership`, `missingRuntimePath`, `missingExternalContract`, `oversizedPrimaryContext`, `taskUnderspecified`. Each returns `[]Gap` (possibly empty).
- `internal/gaps/engine.go` — orchestrates rule evaluation, assigns stable `gap-N` IDs in emission order, applies `gaps.blocking` config upgrades, sorts output deterministically.
- `internal/gaps/remediation.go` — concrete remediation strings per rule (§7.7.4).
- `internal/feasibility/compute.go` — §7.8.2.1 algorithm verbatim: coverage, anchor_resolution, task_specificity, budget_headroom, gap_penalty. Uses the `expected_primary_files` table by action type.
- `internal/feasibility/rationale.go` — build `positives[]`, `negatives[]`, `blocking_conditions[]`, `sub_signals{}` with numeric values (not prose guesses, per §7.8.2.1).
- `internal/cli/explain.go` — fill in the stub from Phase 1: load a prior manifest (via `--manifest <path>` or the latest in `.aperture/manifests/`), or accept the same inputs as `plan` and render reasoning without writing a new manifest. Output human-readable trace: why each file was selected, why each gap fired, how budget was spent (§8.4.1).

**Key decisions:**
- **Gap suppression on non-Go repos:** `unresolved_symbol_dependency` and `missing_runtime_path` are Go-dependent; skip when the index has zero Go files (per the round-7 fixes).
- **Feasibility clamping:** if `count_gaps_blocking > 0`, clamp score to ≤0.40 (§7.8.2.1). Do this after the weighted sum, before returning.
- **Explain output format:** plain-text for now; Markdown later if needed. Do NOT introduce TTY color in v1 — it complicates golden tests.

**Acceptance criteria:**
- Each of the 9 gap rules has a dedicated test fixture that triggers exactly that gap and no others.
- Feasibility on the "add OAuth refresh" fixture lands in the moderate band (0.65–0.84) with positives/negatives enumerated as numeric sub-signal contributions.
- `--fail-on-gaps` exits 8 when a blocking gap is present; `--min-feasibility 0.85` exits 7 when the score is below threshold.
- `aperture explain` on a prior manifest reproduces the selection rationale, gap list, and budget breakdown deterministically (golden-file test).

**Risks:**
- `task_underspecified` has three OR-ed triggers; make sure the message evidence reflects *which* trigger(s) fired.
- `ambiguous_ownership` depends on accurate package-level scoring from Phase 3; regressions here surface as ownership false-positives.

---

## Phase 5 — Manifest Output, Agent Wrappers, `run` Command

**Goal:** Aperture emits both JSON and Markdown manifests, validates output against the embedded JSON Schema, ships the `claude` adapter end-to-end, and implements `aperture run <agent> TASK.md`.

**Files to create:**
- `internal/manifest/markdown.go` — §7.9.3 Markdown renderer. Sections: task summary, planning assumptions, selected full context, selected summaries, reachable context, gaps, feasibility, token accounting, usage instructions.
- `internal/manifest/schema.go` — embed `schema/manifest.v1.json` via `go:embed`; validate every manifest before write (exit 6 on failure, §16).
- `internal/agent/adapter.go` — defines:

  ```go
  type RunRequest struct {
      ManifestJSONPath     string            // absolute, APERTURE_MANIFEST_PATH
      ManifestMarkdownPath string            // absolute, APERTURE_MANIFEST_MARKDOWN_PATH (may be "")
      TaskPath             string            // absolute, APERTURE_TASK_PATH (tempfile for inline tasks)
      PromptPath           string            // absolute, APERTURE_PROMPT_PATH for built-in adapters
      RepoRoot             string            // absolute, APERTURE_REPO_ROOT
      ManifestHash         string            // hex sha256, APERTURE_MANIFEST_HASH
      ApertureVersion      string            // APERTURE_VERSION
      AgentConfig          config.AgentEntry // resolved §9.1.2 entry for this agent
      Stdout, Stderr       io.Writer         // passthrough streams (not captured)
  }

  type Adapter interface {
      Invoke(ctx context.Context, req RunRequest) (exitCode int, err error)
  }
  ```

  Every field maps directly to a contract in §7.10.4.1; keeping the struct flat avoids coupling adapters to the wider `Manifest` type.
- `internal/agent/claude.go` — built-in Claude adapter per §7.10.2 updated: produce merged prompt `.aperture/manifests/run-<manifest_id>.md` (Markdown manifest + `---` + task verbatim), invoke `claude --print --permission-mode bypassPermissions < <prompt>` in `non-interactive` mode (default) or interactive with prompt as arg.
- `internal/agent/codex.go` — analogous for Codex.
- `internal/agent/custom.go` — user-declared adapters from config (§9.1.2).
- `internal/agent/env.go` — set `APERTURE_MANIFEST_PATH`, `APERTURE_MANIFEST_MARKDOWN_PATH`, `APERTURE_TASK_PATH`, `APERTURE_REPO_ROOT`, `APERTURE_MANIFEST_HASH`, `APERTURE_VERSION`, `APERTURE_PROMPT_PATH`.
- `internal/agent/tempfile.go` — §7.10.4.1: inline-task tempfile under `$TMPDIR/aperture-task-<manifest_id>.txt`; signal handler deletes on `SIGINT|SIGTERM|SIGHUP`; `defer`-based cleanup on panic.
- `internal/cli/run.go` — fill in the stub from Phase 1: resolve `<agent>` name against the config-resolved agent map first (§9.1.2) — exit 11 if unknown per §16 **before** any planning work happens; then plan → threshold-check (exit 7 / 8) → write manifests → dispatch to adapter. Pre-exec failures (executable not found, permission denied on the resolved `command`) → exit 12. Adapter started and exited non-zero → propagate the adapter's exit code unchanged.
- `schema/manifest.v1.json` — full JSON Schema. Every field in §11.1 catalogue encoded; run a catalogue-vs-schema diff test on each `make build`.

**Key decisions:**
- **Markdown manifest is both human- and agent-consumable:** the built-in `claude` adapter piggybacks on it, so its format stability matters. Snapshot-test it.
- **Exit-code propagation:** `aperture run` inherits the adapter's exit code when the adapter itself ran; only `12` for pre-exec failures. Do not re-map codes.
- **Non-interactive default for Claude:** per §7.10.2, stdin piping is the default. Support `agents.claude.mode: interactive` for workflows that want to watch the adapter.
- **Adapter-command trust:** SPEC §17 places security at "local-first" and "no exfiltration"; it does not currently require a trust prompt for custom adapter commands. v1 therefore relies on the user's normal trust-on-first-use review of a repository's `.aperture.yaml` before running `aperture run`. The README shipped with v1 must warn users about this and recommend inspecting `agents.<name>.command` entries on first use of a new repo. In CI environments — especially on PRs from forks — the README must explicitly recommend *not* invoking `aperture run` on untrusted branches, and must document `aperture plan` (which never executes adapter commands) as the CI-safe mode. A formal trust-gate (explicit approval flow, persisted trusted-agents file) is deferred to a post-v1 follow-up.

**Acceptance criteria:**
- `aperture plan … --format markdown` emits a manifest whose sections match §7.9.3 and that golden-tests byte-stable across runs (strip timestamps first).
- `aperture run claude testdata/task/simple_feature.md` on the small-Go fixture invokes a stubbed `claude` binary (a test helper on PATH) with the merged prompt piped on stdin; the stub echoes the merged prompt and exits 0; `aperture run` exits 0.
- Pre-exec failure test: `agents.claude.command: /nonexistent` → exit code 12.
- Adapter-failure propagation: stub `claude` exits 7 → `aperture run` exits 7.
- Schema validation rejection: artificially corrupt a manifest field and assert write fails with exit 6.
- Tempfile lifecycle test: send `SIGTERM` to a running `aperture run` and assert the tempfile is deleted.
- Orphaned-tempfile sweep test: pre-create `aperture-task-<fake-id>.txt` in `$TMPDIR` with an `mtime` 48 hours in the past, then invoke `aperture run` on the small fixture and assert the old tempfile was removed while a fresh (< 1 h old) decoy tempfile is preserved.

**Risks:**
- Shelling out to `claude` in CI is not viable; all run-command tests use a `testdata/bin/fake-claude.sh` stub on PATH. Document clearly.
- Signal handling on Windows is limited; scope signal-based cleanup to Unix (`runtime.GOOS != "windows"`) and rely on `defer` elsewhere. To bound orphaned-tempfile accumulation on Windows (and as a belt-and-braces measure on Unix), every invocation of `aperture run` must sweep `$TMPDIR` for `aperture-task-*.txt` files whose `mtime` is older than 24 hours and delete them at startup. The sweep is best-effort and never fails the run.

---

## Phase 6 — Cache, Determinism Hardening, Performance Harness

**Goal:** persistent repo analysis cache (§7.11) delivers the warm-plan performance targets (§8.2); golden/determinism tests lock the manifest contract; `make bench` produces the benchmark artifacts for §19 acceptance.

**Files to create:**
- `internal/cache/cache.go` — persistent cache under `.aperture/cache/`. Key: SHA-256 over `(path, size, mtime, tool_version)` per §7.11.2. Value: cached `FileEntry` with symbol table and imports.
- `internal/cache/invalidate.go` — invalidation rules: mtime change, size change, content hash change (for critical files like `SPEC.md`, `.aperture.yaml`, `go.mod`), Aperture version change, `.aperture.yaml` digest change. Every cache entry also records a top-level `cache_schema_version` string (starting at `"cache-v1"`). On read, a mismatch between the entry's `cache_schema_version` and the constant compiled into the current binary triggers a full `.aperture/cache/` wipe and rebuild, logged at `Info` level. This protects users across v1.x releases that change the cache on-disk format without requiring a manual `aperture cache clear`.
- `internal/cache/manager.go` — cache manager with configurable location; `aperture cache clear` CLI command per §15.1.
- `cmd/aperture/commands.go` — wire the `cache` subcommand.
- `testdata/bench/small/`, `testdata/bench/medium/` — reference fixtures for §8.2 targets. `small` = ~500 files, `medium` = ~5,000 files. Both are committed as generator scripts + checksums to avoid bloating the repo.
- `internal/bench/harness.go` — `make bench` driver. For each fixture, run 10 consecutive `aperture plan` invocations and emit both p95 and median timings per fixture. The **p95 over 10 runs** is the authoritative metric on dedicated hardware (fails `make bench` at >20% regression per §8.2). On shared CI, the median of three consecutive same-commit runs governs the fail gate (>50% regression per §8.2). Both metrics are always reported so developers can cross-reference; only the one matching the environment gates failure.
- `tests/determinism/` — cross-run determinism tests: same inputs twice, assert byte-identical normalized JSON.
- `tests/golden/` — golden-file tests for: manifest JSON, manifest Markdown, explain output, exclusion list, gap emission order.

**Key decisions:**
- **Cache corruption handling:** detect (hash mismatch, malformed entry) → invalidate and rebuild; never crash on a bad cache. Every invalidation event must be logged via `log/slog` at `Info` level for corruption/version mismatch and at `Debug` level for routine mtime/size invalidations, including the cache key, the triggering reason, and the file path. This is essential for diagnosing "warm plan feels cold" regressions in the field.
- **Cache format:** JSON only. Stdlib-only, inspectable, debuggable. If benchmarks later show JSON marshal/unmarshal is the bottleneck, optimizing **within** JSON (streaming decoder, pooled buffers) is preferred to switching formats. Do not introduce gob or any binary serialization in v1 — the auditability goal (§8.4) outweighs a marginal speed win.
- **Benchmark fixtures:** commit as generator scripts to keep the repo small; `make bench-prepare` materializes them in CI.

**Acceptance criteria:**
- Cold plan on `testdata/bench/small/` completes under 10 s p95 on the reference GH runners (§8.2); warm plan under 1 s p95.
- Same for `medium` under 60 s cold / 5 s warm.
- Running `aperture plan` twice on the same fixture → second run reads from cache (observable via `--verbose` log) and finishes in a fraction of the first run's time.
- Determinism test: 20 consecutive runs of the same plan → 20 byte-identical normalized JSON manifests.
- Golden tests cover every manifest section and catch accidental format drift.
- `aperture cache clear` removes `.aperture/cache/` **and** `.aperture/index/` **and** `.aperture/summaries/` (every derived-analysis subdirectory, per the §7.11.3 working-directory layout). It deliberately preserves `.aperture/manifests/` and `.aperture/logs/` so prior runs remain auditable. Exits 0 even when those subdirectories didn't exist to begin with. Robustness rules: permission-locked files cause best-effort removal — each failure is logged at `Warn` level with the offending path but does not abort the command (exit 0 as long as *any* removal succeeded; exit 6 only if the command could not open the target dir at all). An opt-in `--purge` flag extends the removal to `.aperture/manifests/` and `.aperture/logs/` for users who want to reclaim disk space (documented in the command help as destructive to audit history).

**Risks:**
- Cache invalidation is famously hard; err on the side of more invalidation. A cache miss is cheap; a stale manifest is a determinism bug.
- Benchmark variance on shared CI: follow the 3-run-median rule from §8.2 rather than gating on a single run.
- Goroutine leaks in the walker/parser will show up as flaky benchmarks; run `go test -race` on the whole repo before declaring the phase done.

---

## Cross-Cutting Concerns (apply to every phase)

- **Error messages** must state what failed, why, and what the user can do next (§16).
- **Exit codes** are fixed per the §16 table — never invent new codes.
- **Logging**: `log/slog` only (§8.1.1). No `fmt.Println` in library code. `--verbose` raises slog level to `Debug`. Every major pipeline stage (`scan`, `index`, `score_pass1`, `score_pass2`, `select`, `gaps`, `feasibility`, `manifest_emit`, `adapter_invoke`) must emit a structured `Debug`-level timing log with `stage` and `duration_ms` fields at stage exit. This gives `--verbose` runs built-in profiling without pulling in a tracing library.
- **Golangci-lint** runs green at the end of every phase (`make lint`).
- **Tests**: each phase lands with unit + integration coverage for its new surface. Golden/determinism tests are owned by Phase 6 but seeded incrementally.
- **No LLMs** anywhere in the planning pipeline in v1 (§12.3). Gap detection, summaries, feasibility, scoring are all static and deterministic.
- **No comment-bloat**: per CLAUDE.md, default to no comments. Only add when the "why" is non-obvious.
- **Scope — `aperture inspect`:** §15.1 lists `inspect` as "Optional but useful". v1 does **not** implement `inspect`; it is deferred to post-v1. The command surface for v1 is exactly: `plan`, `explain`, `run`, `version`, `cache clear`.
- **Post-v1 deferrals** (items raised during review that are not required by SPEC.md v1 and have been explicitly deferred): secret-pattern redaction of manifest contents, explicit trust-gate for custom adapter commands, `aperture inspect` command. Each should be reconsidered alongside a SPEC revision if it becomes a user-facing requirement.

---

## Phase Dependency Graph

```
Pre-Phase
   │
   ▼
Phase 1 (CLI + config + task + manifest schema)
   │
   ▼
Phase 2 (repo scan + Go AST + fingerprint)
   │
   ▼
Phase 3 (scoring + tokens + load modes + greedy)
   │
   ├─────────────────────┐
   ▼                     ▼
Phase 4               Phase 5
(gaps +               (Markdown manifest +
 feasibility +         agent adapters +
 explain)              run command)
   │                     │
   └──────────┬──────────┘
              ▼
         Phase 6 (cache + bench + determinism)
```

Phases 4 and 5 can be developed in parallel after Phase 3 lands, since neither depends on the other's outputs.

---

## Acceptance for v1 (rolls up §19)

Aperture v1 ships when **every phase's acceptance criteria pass** and the §19 checklist is green in CI on both `ubuntu-latest` and `macos-14` runners:

- `aperture plan TASK.md` works on `testdata/fixtures/small_go/` and on at least one real external Go repo used as a smoke test.
- Valid JSON manifests (validate against `schema/manifest.v1.json`) and valid Markdown manifests (match §7.9.3 section list).
- Manifest includes load modes, scores with per-factor breakdowns, budget accounting with tokenizer identity, all 9 v1 gap categories implemented (§7.7.1), and feasibility with explicit numeric sub-signals.
- Scoring, selection, and hashing are deterministic: 20 repeated runs produce byte-identical normalized JSON.
- Go AST analysis via `go/parser`, `go/ast`, `go/token` only; no regex for Go source.
- Oversized highly-relevant files are demoted to summaries with `demotion_reason` recorded, never silently dropped.
- `reachable` list is distinct from `selections` and never overlaps.
- Threshold failures exit 7 (min-feasibility), 8 (fail-on-gaps), 9 (budget underflow) per §16.
- **Warm-cache p95 on `testdata/bench/small/` ≤ 1 s and on `testdata/bench/medium/` ≤ 5 s**; **cold-cache p95 ≤ 10 s and ≤ 60 s** respectively (§8.2).
- `make lint`, `make test`, `make bench`, `go build ./...`, and `go test -race ./...` all pass.
