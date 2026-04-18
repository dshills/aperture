# PLAN.md (v1.1)

Phased implementation plan for Aperture **v1.1**, derived from `specs/v1.1/SPEC.md`. v1.1 is a strictly-additive delta on top of v1.0 (`specs/initial/SPEC.md`, `specs/initial/PLAN.md`): every v1 behavior, exit code, manifest field, CLI flag, and determinism guarantee remains in force.

Each phase is independently implementable, independently testable, and produces user-visible progress. Phases are ordered by dependency and match SPEC §13's suggested ordering. All section references `§N.M` point at `specs/v1.1/SPEC.md` unless explicitly prefixed `v1 §…`, which refers to `specs/initial/SPEC.md`.

Conventions used throughout:

- "Create" = new file. "Modify" = existing v1.0 file edited in place.
- Every phase ends with `go build ./...`, `go test ./...`, `go test -race ./...`, `golangci-lint run ./...` all green before the phase is considered done.
- No phase is allowed to break a v1.0 determinism test, v1.0 golden test, or v1.0 fixture — **regression of any v1 acceptance criterion blocks the phase**.

---

## Pre-Phase: v1.1 Bootstrap

**Scope:** one-time setup to shift the repo into v1.1 work. Not an independent testable deliverable, but required before Phase 1.

Tasks:

- Keep v1.0's `SPEC.md` pointer intact. Tooling continues to read `specs/initial/SPEC.md` by default; the v1.1 build is driven by `specs/v1.1/SPEC.md` explicitly in its `Makefile` targets.
- Bump `internal/version.Version` default to `1.1.0-dev` (the build-stamp ldflags from v1 Phase 1 remain the authoritative source of truth; the default just reflects the active stream).
- Update `CLAUDE.md` project-state note to reflect that v1.0 has shipped and v1.1 is in progress; leave v1's non-negotiables untouched.
- Add `testdata/eval/` and `testdata/bench/polyglot/` as empty committed directories with `.gitkeep` so Phase 1 and Phase 4 can land without layout churn.
- Expand `Makefile`:
  - `make eval` — runs `aperture eval run --fixtures testdata/eval/`.
  - `make eval-baseline` — reviewer-only helper, never called from CI.
  - `make bench-polyglot` — extends `make bench` to include the polyglot fixture once Phase 4 is in.
- Add a CI job entry **commented out** until Phase 1 lands (the `aperture eval` subcommand does not yet exist). Phase 1 uncomments the job when the binary supports `eval run`. The activated job is exactly `aperture eval run --fixtures testdata/eval/` — no `--fail-on-regression` flag; `aperture eval run` already exits 2 on regression per §4.1 step 4, and CI fails on any non-zero exit. The flag is explicitly NOT added to the CLI surface.

**Exit check:** `make build test lint` all green; the v1.0 golden and determinism tests still pass byte-identically.

---

## Phase 1 — Eval Harness Skeleton (SPEC §4.1–§4.2, §4.4, §7.1)

**Goal:** ship `aperture eval run`, `aperture eval baseline`, and `aperture eval ripgrep` against a fixture YAML schema. CI can now *measure* selection quality against a committed ground-truth baseline. **Zero changes** to the planner's scoring or selection logic.

### Files to create

- `internal/eval/fixture.go` — `Fixture` struct mirroring the §7.1.1 schema. Strict YAML decoding (`KnownFields(true)`), exit-2 on structural errors.
- `internal/eval/fixture_load.go` — loader that walks `--fixtures <dir>` and returns sorted `[]Fixture` (lexicographic on `name`; name duplicates → exit 2).
- `internal/eval/fingerprint.go` — fixture-repo-snapshot hashing per §7.1.1's normative algorithm (`"fixture-fingerprint-v1"` schema literal, null-byte delimiters, NFC-normalized repo-relative paths, SHA-256 of raw bytes). **Deliberately distinct from `repo.fingerprint`** — no `aperture_version` in the stream.
- `internal/eval/score.go` — precision / recall / F1 per §7.1.2, including the 0.5-weighted load-mode tiebreak, forbidden-path hard failure, and gap-presence hard failure.
- `internal/eval/baseline.go` — `baseline.json` reader / writer per §7.1.3. Bootstrap, overwrite-on-clean, refuse-on-regression, `--force` override (§4.2).
- `internal/eval/report.go` — per-fixture Markdown and JSON reports. Sections: summary table, per-fixture breakdown, regressions block, fixtures-missing-from-current-run block. **Per-run metadata format (normative, shared across all v1.1 report emitters):** in JSON, a single top-level key `per_run_metadata` whose value is an object with exactly `generated_at`, `wall_clock_duration_ms`, `host`, `pid`, `aperture_version`. In Markdown, a final `## Per-Run Metadata` section containing a single `- key: value` list with the same five entries. The 20-run byte-identity determinism tests strip this section (a single JSON-path filter or a single Markdown section-header cut) and assert the remainder is byte-identical. Every v1.1 report writer (`eval run`, `eval loadmode`, `eval ripgrep`, and `aperture diff`'s own generation metadata) uses this exact key/section name — this is the contract that makes filtering trivial and consistent.
- `internal/eval/ripgrep.go` — anchor-to-pattern rendering per §4.4's 8-step normative algorithm. Invokes the system `rg` binary; parses `path:count` output; ranks desc by count then asc by normalized path; caps at `--top-n`; feeds the survivors through the existing `internal/budget` fitter with every candidate pinned at `LoadMode=full`.
- `internal/eval/ripgrep_exec.go` — small wrapper around `os/exec` that resolves `rg` from `PATH`, surfaces exit-1 when missing, and captures stderr for diagnostics.
- `internal/cli/eval.go` — `aperture eval` command group: `run`, `baseline`, `ripgrep` subcommands (loadmode lands in Phase 6).
- `testdata/eval/trivial-pass/` — one fixture that the v1 planner already selects correctly on its own repo snapshot.
- `testdata/eval/trivial-fail/` — one fixture whose `expected.selections` contains a path that **does not exist in the repo snapshot** (e.g., `internal/definitely_not_here.go`). This keeps the fixture structurally unpassable regardless of future planner improvements, guaranteeing the harness's failure-detection path stays exercised. A comment block at the top of the YAML file states this invariant explicitly.
- `testdata/eval/baseline.json` — initial baseline covering both trivial fixtures; the file is generated by running `aperture eval baseline` once and committed. The `trivial-fail` fixture's baseline entry records its current (non-passing) F1 so the harness treats it as the stable reference point; the fixture is NOT expected to pass, only to produce a stable failure signature.
- `schema/fixture.v1.json` — JSON Schema for fixture YAML; a CI test validates every committed fixture against it on each build.
- `internal/eval/fixture_fuzz_test.go` — `go test -fuzz=FuzzFixtureLoad` harness over the YAML loader per §11.6. Seed corpus includes truncated YAML, invalid UTF-8, and deeply nested maps; the property: malformed YAML MUST produce an exit-2 structured error, never a panic. CI runs with `-fuzztime=30s`.

### Files to modify

- `internal/cli/root.go` — register `eval` command group.
- `Makefile` — `make eval`, `make eval-baseline` wired to the new binary.
- `.golangci.yml` — allow the new `os/exec` invocation from `internal/eval/ripgrep_exec.go` only (depguard allowlist entry scoped to that package).
- `internal/cli/golden_test.go` — extend golden coverage so eval JSON/Markdown reports are byte-identical across runs.

### Key decisions

- **Fixture repo snapshot lives on disk, not embedded.** `repo/` subdir under each fixture, walked at eval time with the v1 default exclusions; the fingerprint check catches drift (exit 2).
- **`aperture eval run` invokes the v1 planner in-process**, not via a subprocess. The concrete entry point is the existing `internal/pipeline.Build(opts pipeline.BuildOptions) (pipeline.Result, error)` used by `internal/cli/plan.go`. Phase 1 calls it with a `BuildOptions` synthesized per-fixture: repo root set to `<fixture>/repo`, task text taken from `fixture.Task` (or the contents of `task_file`), budget and model from the fixture, config defaults merged with any fixture-local overrides. No user-local `.aperture.yaml` is loaded. Phase 1 MUST NOT fork or duplicate any planner logic; if the existing `BuildOptions` does not expose every parameter the harness needs, Phase 1 extends the struct additively rather than reaching into deeper package internals.
- **Ripgrep comparison uses the real binary.** Implementing ripgrep's Unicode and gitignore semantics inside Aperture would undermine the whole point of the baseline (measuring the naive path). Missing `rg` → exit 1, documented in §7.7's error table.
- **Subprocess environments are restricted allowlists.** `internal/eval/ripgrep_exec.go` (Phase 1) and `internal/eval/agent_check.go` (Phase 6) both set `cmd.Env` explicitly, never inheriting the parent environment. The `rg` invocation passes only `PATH` and `LANG`; `agent_check` passes only the documented `APERTURE_*` variables plus `PATH`. A unit test in each package plants a fictional sentinel variable in the parent env and asserts it is absent from the child process's environment.
- **The eval harness is read-only toward the planner.** No eval code path writes back into scoring weights, cache, or config. §2.2.5 invariant is enforced by package boundary: `internal/eval` imports from `internal/relevance` and `internal/pipeline` but is not imported *by* them.
- **Fixture loader is deterministic.** Sort `name`, reject duplicates, reject ambiguous `task` / `task_file` presence (exactly one required).

### Acceptance criteria

- `aperture eval run --fixtures testdata/eval/` exits 0 with both trivial fixtures present in the committed baseline; emits a markdown report with a per-fixture P/R/F1 row for each.
- Deliberately corrupting one fixture byte changes the computed fingerprint and exits 2 with a structural mismatch message.
- Deleting the baseline and re-running `aperture eval baseline` writes a new `baseline.json` and exits 0 (bootstrap path).
- With an existing baseline, a deliberate scoring-weight perturbation that drops one F1 by > `--tolerance` (default 0.02) causes `eval run` to exit 2 and `eval baseline` (no `--force`) to exit 1. Running `eval baseline --force` overwrites and exits 0.
- `aperture eval ripgrep --fixtures testdata/eval/ --top-n 20` emits a report; the two trivial fixtures have a computed F1 against the same ground truth. No normative threshold is enforced yet — §12.2's 1.2× bar is gated in Phase 7.
- 20 successive runs of `aperture eval run` on identical inputs produce byte-identical reports modulo the per-run section (§8.1).
- **Phase 1 exit check:** `testdata/eval/baseline.json` is committed, validates against the §7.1.3 schema, and contains entries for both `trivial-pass` and `trivial-fail`. Phase 2 depends on this file existing; Phase 2's "atomic commit" is an overwrite of this Phase-1 baseline plus two new entries, not a fresh write.
- Fixture YAML with `task:` and `task_file:` both set → exit 2 with a clear "set exactly one" diagnostic.
- Fixture YAML containing a symlink under `repo/` → exit 2 (§7.1.1).
- **Orphaned baseline entry:** deleting `testdata/eval/trivial-pass/` while leaving its entry in `baseline.json` causes an unfiltered `aperture eval run` to exit 2 with a diagnostic naming the orphaned fixture (§7.1.3). When a future `--fixture <name>` filter flag ships, the orphan check MUST respect subset scoping.
- **Empty-anchors ripgrep case:** a fixture whose `task.anchors` array is empty after dedup causes `aperture eval ripgrep` to report an empty candidate set (precision = 1.0, recall = 0.0 when `E` is non-empty; both 1.0 when `E` is empty) without invoking `rg` (§4.4 step 5).
- **Subprocess env-isolation unit test:** `internal/eval/ripgrep_exec_test.go` spawns `rg` through the wrapper and asserts that a planted `APERTURE_SECRET=leak` parent-env entry does NOT appear in the child process (proven via `/usr/bin/env` on POSIX or an equivalent shim on Windows).

### Risks / likely pitfalls

- Forgetting to exempt per-run fields from report byte-identity; mitigate with a determinism test that diffs two consecutive runs after stripping only the documented per-run section.
- Implementers may be tempted to compute F1 before budget fitting. Don't: §7.1.2's `A` is the **emitted** `selections[*].path` set after the full planner runs, not the pre-budget candidate set. The ripgrep baseline uses its *own* fitter pass for the same reason.
- `rg --count-matches` output can legitimately include zero-count lines for some invocation flags; always filter `count > 0` before ranking.
- The fingerprint algorithm reads raw bytes — on Windows, accidental CRLF insertion in checked-in fixtures will change the hash. Document "fixtures are binary-exact; do not run autocrlf on them" in `testdata/eval/README.md`.

---

## Phase 2 — Mention Dampener (SPEC §7.2, §10.1, §8.3)

**Goal:** land the disagreement-aware `s_mention` dampener behind a config-default-on switch. Bump `selection_logic_version` to `sel-v2`. Add a fixture that v1.0 gets wrong and v1.1 gets right, plus a counter-fixture that v1.0 already got right to prove the change doesn't regress.

### Files to create

- `internal/relevance/dampener.go` — pure function `Dampen(signal, otherMax, floor, slope float64) float64` implementing `min(1.0, floor + slope*otherMax)`. Documented pre/post-conditions: `dampener ∈ [floor, 1.0]`, strictly monotone non-decreasing in `otherMax`.
- `internal/relevance/dampener_test.go` — unit tests per §11.1: all-zeros, all-ones, `enabled=false` pass-through, floor-only, slope-only, and monotonicity check.
- `testdata/eval/mention-dampener-false-positive/` — fixture whose task text says "fix the regression in provider.go" but whose real answer is `refresh.go`. Baseline captures the v1.1 (dampener-on) behavior; running with dampener off reproduces the original v1.0 false positive.
- `testdata/eval/mention-dampener-counter-example/` — fixture where the mention IS the answer and `s_symbol`/`s_filename` also agree. Proves the dampener doesn't penalize correct mentions (its ceiling is 1.0 when any other signal ≥ 1.0).

### Files to modify

- `internal/config/config.go` — add `Scoring.MentionDampener` block (`Enabled bool`, `Floor float64`, `Slope float64`), strict-decoded. Default resolution per §7.2.3: absent block → `{true, 0.30, 0.70}`; present but partial → each field defaulted independently.
- `internal/config/validate.go` — reject `floor ∉ [0.0, 1.0]`, `slope < 0.0`, `slope > 1.0`. Explicit rejection of the "slope defaults to 1-floor" temptation (a review comment on the validator cites §7.2.3).
- `internal/config/defaults.go` — add the `{true, 0.30, 0.70}` default constants.
- `internal/relevance/score.go` — apply the dampener after raw `s_mention` is computed and before the weighted sum. `other_max` is computed over exactly the four signals in §7.2.2 (`s_symbol`, `s_filename`, `s_import`, `s_package`). The dampener is multiplied into `signal` before the weight. The resulting `contribution` goes into the score breakdown.
- `internal/relevance/breakdown.go` — extend the per-factor breakdown entry to include a `dampener` float field (§10.1). Factor `mention` carries the computed dampener; all other factors carry `1.0`. When `enabled: false`, every factor's dampener is `1.0` (pass-through).
- `internal/config/config.go` — the config digest (used in `manifest_hash`) MUST include the resolved `mention_dampener` block so toggling the feature produces a distinct hash (§7.2.3).
- `internal/manifest/types.go` — add the `dampener` field to the score-breakdown struct. JSON-optional for v1.0 round-trip compatibility; when a v1.0 consumer reads a v1.1 manifest the field is simply an unknown key (v1 §7.9.2 already mandates ignore-unknowns).
- `schema/manifest.v1.json` — schema adds optional `dampener: {type: number, minimum: 0, maximum: 1}` to the breakdown entries.
- `internal/relevance/score.go` — bump `selection_logic_version` constant from `"sel-v1"` to `"sel-v2"` in the same commit that flips dampener default to `enabled=true`. **Single-commit atomicity** is a release invariant: the cache key must invalidate at exactly the moment scoring changes.
- `internal/cache/…` — no schema bump (still `cache-v2`); entries written under `sel-v1` simply miss and get recomputed. A short note in the cache godoc explains that the key's `selection_logic_version` component handles the invalidation.
- `internal/cli/plan.go` — `--verbose` output gains per-candidate dampener logging when < 1.0 and the candidate is selected OR reachable (§8.4).

### Key decisions

- **Single dampener activation, not a slow rollout.** The SPEC makes the change atomic with a `selection_logic_version` bump. Phase 2 flips both in the same commit. There is no "dampener enabled but sel-v1" intermediate state — it would be a cache-determinism hazard.
- **Dampener is a clamp, not a redistributor.** No other weight increases to compensate. §15's guidance is treated as normative. Scoring tests verify: if `s_mention` drops by δ, no other `contribution` increases automatically.
- **Counter-example fixture is load-bearing.** Phase 2 does not ship without a fixture that proves dampener-on does not hurt the "mention IS the answer" case. CI failure here is the main guardrail against overfitting.
- **Config digest inclusion.** The resolved block goes into the existing config-digest hash machinery; there is no new hash, just a new input to the existing one. That means toggling `enabled` or tweaking `floor`/`slope` visibly changes `manifest_hash`, as required.
- **Rollback protocol for the golden-regeneration commit.** The `sel-v1` → `sel-v2` bump rewrites every v1.0 golden manifest. To make rollback cheap, Phase 2 is structured as three sequential commits on a dedicated branch. **Atomicity applies to commit 2 only** — the flip of `enabled=true`, the `selection_logic_version` bump, the golden regeneration, and the new fixture commits must land as one atomic commit so no intermediate state exists where `sel-v2` scoring is live but goldens still reflect `sel-v1`. Commits 1 and 3 are setup and tagging steps that do not affect the cache key and therefore do not touch the atomicity invariant.
  1. Introduce dampener code + config + tests, with `selection_logic_version` still `"sel-v1"` and the default `enabled: false`. All v1 goldens unchanged; CI green.
  2. **Atomic commit:** flip default to `enabled: true`, bump `selection_logic_version` to `"sel-v2"`, regenerate goldens, regenerate `testdata/eval/baseline.json` via `aperture eval baseline --force` (run by the committing reviewer immediately before `git add`), and commit the two new dampener fixtures (false-positive + counter-example). All of these artifacts — source, goldens, baseline, fixtures — are staged into the **same** git commit; there is no interim commit with mismatched goldens/baseline/source.
  3. Tag the pre-bump state as `v1.1.0-pre-sel-v2` so a rollback is `git revert <commit-2>` plus `git checkout v1.1.0-pre-sel-v2 -- testdata/golden/`. Document this procedure inline in the commit-2 message.

  **Rollback acceptance test.** After the atomic commit lands on `main`, Phase 2 also adds a `test/rollback/sel-v2-rollback_test.sh` script that a reviewer can run on a clean clone:
    1. Checks out the commit-2 SHA; runs `aperture eval run` → expects exit 0.
    2. Runs `git revert <commit-2-sha>`; `git checkout v1.1.0-pre-sel-v2 -- testdata/golden/`; `go build ./...`.
    3. Runs `aperture eval run` → expects exit 0 again, against the pre-bump baseline.
    4. Verifies `manifest.generation_metadata.selection_logic_version` reads `"sel-v1"` after the revert.
  The script is NOT run in CI (it mutates git state); it is a runbook artifact that proves the rollback path is real. Phase 2 is not considered exit-complete until a reviewer has run the script once and recorded success in the commit-3 (tagging) message.

  **Commit-2 message template** (committed verbatim in the commit body):

  ```
  feat(relevance): enable mention dampener by default; bump sel-v1 → sel-v2

  This commit atomically (a) flips scoring.mention_dampener.enabled default
  to true, (b) bumps selection_logic_version from "sel-v1" to "sel-v2", and
  (c) regenerates every golden manifest to match the new scoring output.

  Rollback procedure if a regression is discovered after merge:
    git revert <this-sha>
    git checkout v1.1.0-pre-sel-v2 -- testdata/golden/

  Pre-commit checklist (reviewer confirms each):
    [ ] `enabled: true` is the resolved default when the block is absent
    [ ] `selection_logic_version` constant reads "sel-v2"
    [ ] Every golden manifest under testdata/golden/ regenerated in the
        same commit (diff is >0 files, all under that tree)
    [ ] `aperture eval run` passes against the updated baseline
    [ ] `aperture eval baseline --force` was run by the committer
  ```

### Acceptance criteria

- Unit tests enumerate §7.2.2's formula on ≥ 20 (signal, other_max, floor, slope) tuples, all passing to tolerance 1e-9.
- The false-positive fixture: running `aperture plan` with `enabled=false` produces a manifest where `provider.go` has a strictly higher `relevance_score` than `refresh.go` in `selections[]`. Running with `enabled=true` (default) produces a manifest where `refresh.go` has a strictly higher `relevance_score` than `provider.go`. Both files appear in `selections[]` in both runs; only the ordering changes.
- The counter-example fixture produces the SAME top-ranked selection (same path at rank 1 in `selections[]` sorted descending by `relevance_score`) whether dampener is on or off. Absolute scores may differ; path identity is what the test asserts.
- `manifest_hash` differs between `enabled=true` and `enabled=false` on the same repo / task / budget.
- `selection_logic_version` in generated manifests is `"sel-v2"` after this phase.
- `aperture eval run` against the baseline (re-generated in a reviewer-issued `baseline --force` run as part of this phase's commit) stays green.
- `aperture plan --verbose` logs the dampener factor for any selected / reachable candidate where it was < 1.0.
- 20-run byte-identity holds for plans on both fixtures.
- **Regression-cycle integration test (§11.4):** a single test scripts the full cycle: (1) `aperture eval run` against the dampener baseline → exit 0; (2) bump a scoring weight to induce F1 regression; (3) `aperture eval run` → exit 2; (4) `aperture eval baseline` (no `--force`) → exit 1, baseline unchanged; (5) `aperture eval baseline --force` → exit 0, baseline overwritten; (6) `aperture eval run` → exit 0. Each step's exit code and side effect (baseline file mtime / SHA-256) is asserted.

### Risks / likely pitfalls

- Leaking non-determinism by computing `other_max` using a map iteration instead of an explicit 4-element expression. Use an explicit `max(max(a,b), max(c,d))`.
- Forgetting the v1.0 golden manifests: they'll all change because `selection_logic_version` bumped. Regenerate in a single, reviewer-gated commit; annotate the diff clearly.
- Accidentally applying the dampener to `s_mention=0` candidates. Short-circuit: if the raw `s_mention` is 0.0, contribution is 0.0 regardless of dampener (a `0 * anything = 0` invariant; implement as a single multiplication, don't special-case).
- Users may tune `slope=0` hoping to "turn off the ramp." That's legal — it clamps `dampener` to exactly `floor`. Document this in the `.aperture.yaml` reference.

---

## Phase 3 — `--scope` Flag (SPEC §4, §7.4, §10.1)

**Goal:** `aperture plan --scope <subtree>` restricts candidate generation, scoring evidence, and gap detection to a repo-relative subtree, while leaving the repo fingerprint and supplemental-file resolution rooted at the repo itself. Emits a new top-level `scope` manifest field that folds into `manifest_hash`.

### Files to create

- `internal/repo/scope.go` — scope path transformation (§7.4.4 phase 1: backslash rewrite, strip leading `./`, strip one trailing `/`, collapse `/./`) and validation (§7.4.4 phase 2: empty, `..` anywhere, leading `/`, null bytes, inside-repo directory, symlink resolution). Sentinel handling (`""`, `.`) short-circuits to "unset" *before* any rewriting.
- `internal/repo/scope_case.go` — cross-platform case-determinism rewrite per §7.4.4: on each path segment, if the on-disk entry's case differs from the typed case, substitute the on-disk casing. Implemented by `os.ReadDir` per parent segment. On case-sensitive filesystems, a mismatch instead fails validation (exit 4). A runtime detection helper `isCaseInsensitiveFS(root string) bool` — **stat-only, no file creation** — picks any existing directory entry under `root` (typically `.git` if present; else the first entry returned by `os.ReadDir(root)`; skipped with `false` if `root` is empty), flips the case of one ASCII letter in its name, and calls `os.Lstat` on the mutated path. Success → case-insensitive; `ErrNotExist` → case-sensitive; any other error → treat as case-sensitive (the stricter default) and log the underlying error at verbose level. Result is memoized per-`root` for the process. No writes to disk; works on read-only repo mounts.
- `internal/repo/scope_test.go` — unit coverage for every `Transformation` and `Validation` rule, plus the sentinel pass-through.
- `testdata/fixtures/monorepo/` — Go monorepo with 3+ top-level `services/<name>/` packages, overlapping package-adjacency signal across them, plus one `specs/<service>/SPEC.md` supplemental per service.
- `testdata/eval/monorepo-scope/` — fixture proving that scoping to `services/billing` resolves `ambiguous_ownership` against billing peers only and excludes non-billing files from selection.

### Files to modify

- `internal/cli/plan.go`, `internal/cli/run.go`, `internal/cli/explain.go` — add `--scope <path>` flag. Flag applies to all three commands per §4.
- `internal/config/config.go` — add `Defaults.Scope` (string). CLI flag overrides config; `""` and `.` sentinels unset either (§7.4.5).
- `internal/pipeline/…` — thread scope through the planner. The walker still walks the whole repo (for fingerprint and supplemental detection), but the candidate-generation step filters by `under(scope, path)`; `missing_external_contract`, `missing_spec` rules continue to scan the full repo so out-of-scope supplementals resolve correctly.
- `internal/relevance/score.go` — restrict the `s_import` second-pass (v1 §7.4.2) to in-scope files when identifying high-scoring targets; restrict `ambiguous_ownership` peer-count (v1 §7.7.3) to the scoped candidate set.
- `internal/repo/supplemental.go` — always admit supplemental files regardless of scope. When a supplemental is outside scope, mark it with an `OutOfScope bool` on the `FileEntry` for the relevance layer.
- `internal/relevance/score.go` — when a supplemental file is marked `OutOfScope`, force `s_symbol`, `s_import`, `s_package`, `s_test` to `0.0`; leave `s_mention`, `s_filename`, `s_doc`, `s_config` computed normally (§7.4.2 table). `other_max` for the dampener is always `max(s_symbol, s_filename, s_import, s_package)` — the §7.2.2 formula, applied unchanged to whatever inputs the scoring stage produced. In the out-of-scope supplemental case, three of those inputs are 0, so the result collapses to `s_filename`; the code path is one expression, not a conditional.
- `internal/manifest/types.go` — add optional top-level `Scope *ScopeField` (struct `{Path string}`). Omitted entirely when unset; when the sentinels unset a config scope, the field is still omitted.
- `internal/manifest/hash.go` — add `scope` to the hashed envelope (it is part of `manifest_hash` input). Two identical plans, one with `--scope` and one without, MUST produce distinct hashes. Existing exclusion rules (per-run fields exempted) unchanged.
- `schema/manifest.v1.json` — additive schema entry for `scope`.
- `internal/cli/pipeline_common.go` — emit exit 4 for validation failures, exit 9 for the "zero planable candidates AND no admissible supplementals" case (§7.4.6). Reuse the v1 exit-9 code path verbatim; do not add a new exit code.
- `internal/cli/plan.go` — `--verbose` logs resolved scope and pre-/post-filter candidate counts (§8.4).

### Key decisions

- **Scope is a projection, not a sub-repo.** The walker, fingerprint, and supplementals remain repo-rooted. Out-of-scope supplementals are admitted with restricted signal, per §7.4.2 — we never pretend a subtree is a standalone repo (§15).
- **Case determinism is per-segment, not whole-path.** A whole-path stat would miss the case where the user types `Services/billing` against on-disk `services/billing`: the first segment alone needs rewriting. Per-segment `ReadDir` is the minimal approach that keeps the manifest byte-identical across hosts.
- **Symlink-resolution happens once, at scope validation.** The resolved directory is what the walker uses internally; the un-resolved (normalized but not symlink-resolved) path is what appears in the manifest so different clones to different absolute paths produce the same `scope.path`.
- **Single scope only.** Multi-scope union (`--scope A --scope B`) is deferred to v1.2 per §14. The flag parser rejects multiple `--scope` flags as a usage error (exit 4 `usage`).
- **Exit 9 reuse.** §7.7 deliberately folds "scope leaves zero planable candidates" into v1's budget-underflow semantics. Implementation reuses the exact same exit-9 call site; only the message varies.

### Acceptance criteria

- `aperture plan --scope services/billing` on the monorepo fixture produces a manifest where every `selections[*].path` is either under `services/billing/` or is a supplemental file (rationale contains `outside_scope_supplemental`).
- `manifest_hash` of the scoped plan differs from the unscoped plan on the same fixture / budget / task.
- `--scope ..` → exit 4 with a diagnostic that names the violated rule.
- `--scope services/missing` → exit 4 (not a directory).
- `--scope services/billing --repo <copy-on-case-insensitive-fs>` with the user typing `Services/Billing` → success on macOS/Windows; the manifest stores `services/billing` (actual on-disk casing).
- The same scenario on a Linux case-sensitive filesystem → exit 4.
- A fixture scoped to a subtree that contains zero files but whose repo root has no admissible supplementals → exit 9.
- A fixture scoped to a subtree with zero files but a repo-root SPEC.md that mentions the subtree → plan proceeds normally, selection contains only the supplemental.
- **Sentinel handling acceptance:** `aperture plan --scope ""` and `aperture plan --scope .` both run as if `--scope` were absent; the emitted manifest has no `scope` field and its `manifest_hash` matches the unscoped plan byte-for-byte. A config-declared `defaults.scope: services/billing` overridden on the CLI by `--scope ""` or `--scope .` also unsets scope (the CLI sentinel wins).
- 20-run byte-identity of scoped plans (§11.3).
- **Verbose-logging acceptance (§8.4):** `aperture plan --scope services/billing --verbose` on the monorepo fixture logs a line containing the resolved scope path and two integers: the pre-scope-filter candidate count and the post-scope-filter candidate count. An acceptance test scrapes the verbose stderr for both integers and asserts the post-count is strictly less than the pre-count.
- **Property test (§11.5):** scope projection is observationally identical modulo supplementals and fingerprint. The test builds two inputs: (A) the monorepo fixture with `--scope services/billing`, and (B) a synthesized standalone repo containing only `services/billing/*`. After masking out supplemental-file selections (which are repo-root-sourced in A and absent in B) and ignoring `repo.fingerprint` (A spans the whole monorepo; B spans only the subtree), the remaining `selections[*].path` sets MUST match exactly.

### Risks / likely pitfalls

- Regressing v1 fingerprint tests by scoping the walker too aggressively. Mitigation: `internal/repo/fingerprint.go` operates on the full walk result; scope is applied downstream only.
- Confusing relative-path normalization with symlink resolution. Mitigation: two distinct functions; normalization is textual only, symlink resolution is an `os.Stat` step, and they run in that order.
- Forgetting the "don't emit `scope` field when sentinel unsets a config default" case. Mitigation: explicit unit test.
- The `s_import` second pass can accidentally leak out-of-scope high scorers into the in-scope file's score. Mitigation: filter the *target* set to in-scope before computing the second-pass contribution; unit tests verify identical behavior when no scope is set.

---

## Phase 4 — Tier-2 Language Analysis (SPEC §7.3, §10.2)

**Goal:** ship TypeScript, JavaScript, and Python symbol/import extraction via vendored tree-sitter grammars. Polyglot repos score real symbols, not just filenames. Cache remains `cache-v2`. A v1.0 binary reading a v1.1 cache ignores the new entries.

### Files to create

- `internal/lang/tstree/` — new package wrapping a tree-sitter Go binding. **Binding decision (locked before Phase 4 starts, not a spike):** `github.com/smacker/go-tree-sitter` (CGo-required) is the pinned choice because it is the only maintained binding that exposes the per-grammar entry points we need (`tsx`, `jsx`, Python), vendors upstream grammars cleanly, and has a permissive license. The chosen version MUST be verified (before Phase 4 begins) to expose:
  - a TSX-distinct entry point (not the plain TypeScript parser), and
  - a JSX-distinct entry point (not the plain JavaScript parser).
  If the verification fails, the plan falls back to the alternative action documented in `internal/lang/tstree/doc.go`: bind to the upstream C grammar directly through cgo shims committed to `third_party/tree-sitter-<lang>/binding.go`.
- **CGo build matrix and source-build fallback.** Aperture v1.1 ships prebuilt binaries via a `goreleaser` config committed at `.goreleaser.yml` (added in Phase 4's files-to-create). Binary targets: `linux/amd64`, `linux/arm64`, `darwin/amd64`, `darwin/arm64`, `windows/amd64`. CI adds a GitHub Actions matrix with one job per target, each running `go build` with `CGO_ENABLED=1` and the appropriate cross-toolchain.
- **`CGO_ENABLED=0` graceful degradation.** Tier-2 code is compiled behind a `//go:build !notier2` tag; a companion `//go:build notier2` stub file in the same package provides no-op implementations that return `Tier3Lexical` for every tier-2 language and log a one-line warning on first invocation. Users building with `-tags notier2` get a working binary with tier-2 silently degraded to tier-3 — documented in `docs/tier2.md`. `CGO_ENABLED=0` builds without the tag fail at link time with the standard Go cgo-missing error; the README points users at `-tags notier2` for that case.
  - `tstree/parse.go` — `Parse(src []byte, lang Lang) (*Tree, bool)` where the bool is `parseError` per §7.3.2 (root node `ERROR` OR `has_error()` true).
  - `tstree/typescript.go` — module-level symbol extraction for `.ts`, `.tsx` (including the function-valued-const rule: RHS node in {`arrow_function`, `function_expression`} → kind `function`; other RHS → kind `variable`). Anonymous `export default` → name `"default"`, kind driven by RHS node type.
  - `tstree/javascript.go` — same ruleset applied to `.js`, `.jsx`, `.mjs`, `.cjs`. CommonJS `require("...")` recognized only when the call satisfies all three conditions in §7.3.2 (bare `require` identifier, single string argument, enclosing statement is a direct child of `program`).
  - `tstree/python.go` — module-level `def`, `async def`, `class`, and single-identifier assignments. Lambda-valued assignments → kind `function`. `_`-prefixed names → `Exported: false`. `import X` and `from X import …` record the module name once; relative imports preserve dot prefix verbatim.
  - `tstree/symbols.go` — common `Symbol` builder that fills `index.Symbol`.
- `internal/lang/tstree/testdata/` — pinned snippets per language (TypeScript, TSX, JavaScript, JSX, Python) + their golden symbol tables for §11.2's golden tests.
- `internal/index/tier.go` — `LanguageTier` enum (`Tier1Deep`, `Tier2Structural`, `Tier3Lexical`) and helpers mapping file extension + config to tier. The enum's string form is the descriptive name per §5.4 (`"tier1_deep"` etc.).
- `testdata/bench/polyglot/gen.go` — idempotent generator materializing 2 000 TypeScript files adjacent to the existing Go `medium` fixture. Output directory is `testdata/bench/polyglot/materialized/` (added to `.gitignore` — NOT committed; materialized files would inflate clone size by ~30 MB). Generator is invoked by `make bench-polyglot` before the benchmark runs; it is pure-function of a `--seed` flag (default `0`) so repeated invocations produce byte-identical file contents. `make clean` removes `materialized/`. A golden hash of the materialized tree (pinned in `testdata/bench/polyglot/FIXTURE.sha256`) is verified by the bench harness before running; drift fails the bench with a regenerate-then-retry diagnostic.
- `testdata/eval/polyglot-resolver/` — fixture per §3.3: Go HTTP middleware + TypeScript GraphQL resolver; task targets the resolver; expected selection pins `resolver.ts` at `load_mode` unconstrained.
- `testdata/fixtures/polyglot/` — integration fixture combining Go, TS, Python, and at least one tier-3 fallback file (e.g. Rust).

### Files to modify

- `internal/repo/walker.go` — route new extensions (`.ts`, `.tsx`, `.js`, `.jsx`, `.mjs`, `.cjs`, `.py`) to the tier-2 analyzer when `languages.<name>.enabled=true`; otherwise fall through to tier-3 lexical. File content still hashed as in v1; only symbol extraction differs.
- `internal/index/index.go` — `FileEntry` gains `LanguageTier string` (stored as the descriptive name). v1.0 cache entries missing this field → defaulted to `tier1_deep` for `.go` and `tier3_lexical` otherwise on read (§10.2).
- `internal/cache/…` — cache key remains `sha256(path, size, mtime, selection_logic_version)`; `Language` field on the cache entry carries the tier name. No schema bump; v1.0 binaries reading these entries simply do not look up non-Go paths (v1 behavior).
- `internal/index/supplemental.go` — add `conftest.py` to the supplemental table (analogous to Go's `testdata/`).
- `internal/index/link_tests.go` — test-to-production linking rules for the JS/TS family (`auth.test.ts` → `auth.ts`, with the fixed intra-family priority `.tsx > .ts > .mjs > .cjs > .jsx > .js`) and Python (`test_foo.py` / `foo_test.py` → `foo.py`). Cross-family links are forbidden.
- `internal/config/config.go` — `languages.<name>.enabled` block (`typescript`, `javascript`, `python`), all defaulting to `true`.
- `internal/relevance/score.go` — tier-2 files contribute to `s_symbol`, `s_import`, `s_package`; they do NOT contribute to side-effect-driven rules in v1.1 (§5.4).
- `internal/gaps/…` — `missing_tests` gap rule extended to recognize `.test.ts`, `.spec.ts`, `.test.tsx`, `.spec.tsx`, `.test.js`, `.spec.js`, `.test.jsx`, `.spec.jsx`, `test_*.py`, `*_test.py` as test files.
- `internal/manifest/types.go` — `generation_metadata` gains optional `LanguageTiers map[string]string` (language hint → descriptive tier name) and optional `Grammars map[string]string` (e.g., `tree_sitter_typescript: "v0.x.y"`). Both fields fold into `manifest_hash` input.
- `schema/manifest.v1.json` — additive schema entries for the two new generation-metadata maps.
- `Makefile` — `make vendor-grammars` target that re-emits the vendored grammar sources from pinned upstream tarballs. Each `third_party/tree-sitter-<lang>/REVISION` file contains two lines: the upstream commit SHA and the SHA-256 hex digest of the tarball downloaded from that commit's GitHub archive URL. The `vendor-grammars` script fetches the tarball, verifies the SHA-256 before extracting (abort on mismatch), and logs both values to stdout. Also add `make check-grammar-licenses` as a short shell script that asserts every `third_party/tree-sitter-*/` directory contains a non-empty `LICENSE` file; the target exits non-zero on any violation. CI wires `check-grammar-licenses` as a required step but never invokes `vendor-grammars`.
- `Makefile` — extend `make bench` to also emit the Phase 4 `polyglot / medium` cold- and warm-plan ratios in the same bench run (via the `bench-polyglot` target). Ratios are reported on stdout in a machine-parseable form (`POLYGLOT_COLD_RATIO=1.15`, `POLYGLOT_WARM_RATIO=1.07`) so CI can grep for them and gate on the §7.3.5 thresholds.
- `internal/lang/goanalysis/…` — **no changes.** Go continues to use `go/parser`, `go/ast`, `go/token`. Tier-2 is strictly additive.

### Key decisions

- **Grammars vendored, not downloaded.** §7.3.1 is a hard requirement — no network, identical grammar bytes on every platform. The `vendor-grammars` Makefile target is a reviewer-only workflow; CI never invokes it.
- **Binding choice is a single decision, not a long-running evaluation.** Spike before Phase 4 lands; record the decision in `internal/lang/tstree/doc.go` with a short justification (licensing, no-cgo if possible, determinism of the bindings across platforms). If a no-cgo path is unavailable, accept cgo for tier-2 only and document the implication for cross-compilation.
- **Module-level only, period.** No nested-function extraction, no class-body extraction. §7.3.2 makes this a determinism and boundedness rule, not an "easy to extend later" concession. Nested symbols are silently ignored.
- **Parse errors fall back to mention/filename/doc scoring.** Tree-sitter always returns a tree; we treat `has_error()` or `ERROR` root as "no structural symbols" and still admit the file as a candidate with the remaining signals (§7.3.2).
- **Cache-schema version is NOT bumped.** §7.3.4 mandates `cache-v2` stays. The cache format change is additive (a previously-ignored extension now has its own entry), which is backward-compatible by construction.
- **`selection_logic_version` already bumped in Phase 2; no further bump in Phase 4.** The cache-key collision worry is definitionally absent: the key is `sha256(path, size, mtime, selection_logic_version)`, and Go paths (`*.go`) cannot share a key with TypeScript or Python paths (`*.ts`, `*.py`) because `path` is part of the key. Dispatch to the tier-1 vs tier-2 analyzer is by file extension, not by cache contents; a cache entry is always decoded by the analyzer that owns the extension, and the entry's `Language` field discriminates internally so a cross-analyzer misread is impossible. This reasoning is documented in the `internal/cache` godoc alongside the key definition, and a unit test constructs a Go and a TypeScript entry with colliding-but-for-extension paths to prove their keys differ.
- **Cache entry carries `Language` as a discriminator.** §7.3.4's "`Language` field distinguishes tiers" appears in the value, not the key. A deserializer that reads a cache entry with an unexpected `Language` for its requested path treats the entry as a miss and re-analyzes; this is the safety net if the rationale above is ever invalidated by a future path-collision bug.
- **Config-driven tier toggle invalidation.** Toggling `languages.<name>.enabled` changes the resolved config, which already flows into the config digest used in `manifest_hash`. But cache entries are keyed on `(path, size, mtime, selection_logic_version)` — not on config — so a tier-2 entry written while `enabled: true` would otherwise be served to a later run with `enabled: false`. To close this hole, the cache reader compares the requested **expected tier** (computed from the current config + extension) against the entry's `Language` field before returning a hit. Any mismatch is a miss: the file is re-analyzed under the new tier and the cache entry is overwritten. A unit test toggles `languages.typescript.enabled` between `true` and `false` on the same fixture and asserts the resulting cache entries' `Language` field tracks the toggle, and that the `manifest.generation_metadata.language_tiers["typescript"]` reflects the resolved tier after each toggle.

### Acceptance criteria

- `polyglot-resolver` fixture: `resolver.ts` selected at `relevance_score ≥ 0.70`; `missing_tests` rule considers `.test.ts` siblings.
- Golden tests for each language's pinned snippet produce byte-identical symbol tables across platforms.
- `manifest.generation_metadata.language_tiers` reflects actual tiers on a mixed repo.
- `make bench-polyglot` shows cold-plan ratio `polyglot / medium ≤ 1.20` and warm-plan ratio `≤ 1.20`, with warm-plan absolute time inside v1's 5 s `medium` target.
- `languages.typescript.enabled: false` in config → `.ts` files fall back to tier-3, no symbol extraction, and `generation_metadata.language_tiers["typescript"] == "tier3_lexical"`.
- A cache written by v1.0 and read by v1.1 is accepted; entries with a missing `LanguageTier` default to the v1.0-equivalent tier per §10.2.
- Fuzz harness (`go test -fuzz ./internal/lang/tstree/... -fuzztime=30s` during CI) on the TypeScript grammar runs without panic or unbounded memory (§11.6).
- **Verbose-logging acceptance (§8.4):** `aperture plan --verbose` on the polyglot fixture logs a per-language parse-statistics block with two integers per language (`files_parsed`, `parse_errors`) for every tier-2 language as well as Go. An acceptance test greps the verbose stderr for the expected line per language.

### Risks / likely pitfalls

- Grammar license headers missing in the vendored bundle. Mitigation: `make vendor-grammars` copies LICENSE files and a CI check fails if any `third_party/tree-sitter-*/` lacks one.
- CGo-induced cross-compile breakage. Mitigation: if the chosen binding requires cgo, document the Linux / macOS / Windows build matrix we actually ship and warn users attempting unsupported cross-compiles. No Windows-on-ARM promise.
- `.mjs` and `.cjs` files parsed by `tree-sitter-javascript` generally work, but module-syntax edge cases (top-level `await`, dynamic import) may produce parse trees we don't expect. Mitigation: golden tests over a curated snippet set; real-world weird files fall through to the `parseError` path instead of producing wrong symbols.
- Test-linking priority `.tsx > .ts > .mjs > .cjs > .jsx > .js` is fixed; there's a strong temptation to "help the user" by preferring whichever sibling exists today. Resist. The rule is deterministic and documented; it's correct even when "the opposite order would be nicer here."
- CommonJS `require()` recognition must be strict — any one of the three conditions missing disqualifies the call. A permissive match would collect `require()` calls from inside functions and conditionals, destroying determinism for files that branch on environment.

---

## Phase 5 — `aperture diff` (SPEC §4.5, §7.6)

**Goal:** a single command that explains what changed between two manifests — hash, task, repo, budget, selections, reachable, gaps, feasibility, generation metadata — in deterministic Markdown and JSON. Pure file-in, file-out: does NOT invoke the planner, does NOT read the repository.

### Files to create

- `internal/diff/diff.go` — top-level entry: parse both manifests (any `schema_version ≥ "1.0"`), compute section-by-section deltas, emit in the fixed §4.5 order.
- `internal/diff/sections.go` — one function per section (hash/id, task, repo, budget, selections, reachable, gaps, feasibility, generation-metadata). Each returns a deterministic struct that both the Markdown and JSON emitters consume.
- `internal/diff/semantic.go` — `manifest_hash`-equality fast-path. When hashes are equal, the diff output contains the `semantic_equivalent: true` top-level field, empty section bodies, and — in the Markdown output — "unchanged" markers. The permitted-to-differ per-run field list is exactly the six fields excluded from `manifest_hash` per v1 §7.9.4 and restated in §7.6.3: `manifest_id`, `generated_at`, `generation_metadata.host`, `generation_metadata.pid`, `generation_metadata.wall_clock_started_at`, `aperture_version`. Any non-empty delta in any other field under hash equality triggers a "hash agreement + content disagreement" diagnostic flagged as a tool-level bug (not a user error).
- `internal/diff/markdown.go` — Markdown emitter. Fixed section ordering, lexicographic per-path ordering within sections, stable "unchanged" markers for empty sections.
- `internal/diff/json.go` — JSON emitter. Compact form for hashing-of-diff-output (if future work requires it); pretty form for disk output.
- `internal/cli/diff.go` — command wiring; `--format json|markdown` (default markdown), `--out <path>` (default stdout).
- `testdata/fixtures/manifests/` — four manifest pairs for golden tests per §11.2 plus Phase 3's scope acceptance: identical-hashes, config-digest-difference, selection-set-difference, and `scope-delta/` (one manifest with `scope.path: services/billing`, one with scope absent; diff must surface both the scope delta and the consequent selection delta).

### Files to modify

- `internal/cli/root.go` — register `aperture diff`.
- `internal/manifest/types.go` — no semantic changes, but surface a helper that reports per-run fields so `internal/diff/semantic.go` can filter without string-literal duplication.
- `schema/manifest.v1.json` — no changes; diff is a consumer, not a producer, of the schema.

### Key decisions

- **Diff does not invoke the planner, does not open the repo, does not re-hash.** §7.6.1, §7.6.3, and §4.5 all converge on this. The command's exit-code table (§7.7) is "0 always, 1 only on I/O or parse errors" — informational tool.
- **All sections always emitted.** §7.6.2 forbids omission. Empty sections render an "unchanged" marker so consumers can distinguish "checked, nothing changed" from "not computed."
- **Equal hashes ⇒ everything else empty, by construction, except the per-run exempt list.** If the implementation observes a non-empty delta under equal hashes, it MUST log a tool-bug diagnostic. §7.6.3 calls this out explicitly; our diff tool is the only sensor that would catch this class of manifest-emitter regression.
- **No fuzzy matching on selections.** Path equality is exact. A selection that moved file-paths is reported as "removed" + "added," not "renamed." §4.5's spec is deliberately path-identity.
- **`manifest_id` equality is observed, not assumed.** §4.5: when the same file is passed on both sides, the tool reports "equal"; when two files happen to share `manifest_id` (trivially, if diffing a manifest against itself), the tool also reports "equal." No assumption of inequality.
- **Schema-version check on input.** §7.7 error table: `schema_version < "1.0"` on either side → exit 1. Comparison uses **major-then-minor integer parsing**, not lexicographic string compare: split on `.`, parse each component as `int`, compare pairwise. `"1.10"` is accepted (greater than `"1.0"`); `"0.9"` is rejected; malformed versions (non-numeric components, missing minor) exit 1 with a clear diagnostic. A unit test covers `"1.0"`, `"1.10"`, `"2.0"`, `"0.9"`, and malformed inputs.

### Acceptance criteria

- `aperture diff A.json A.json` prints `semantic_equivalent: true`, empty section bodies, exit 0.
- Diffing two back-to-back plans of the identical fixture produces empty non-metadata deltas (§12.6).
- Diffing a pair where the `config_digest` differs prints the resolved config weights for both sides (§4.5).
- Diffing a pair where one manifest has `scope` set and the other doesn't prints the scope delta and the resulting selection delta.
- 20-run byte-identity over two fixed manifests (§11.3).
- Feeding a manifest with an invalid / missing `schema_version` → exit 1.
- Feeding a file that isn't JSON → exit 1 with a parse-error diagnostic.
- Performance: `aperture diff` on two 2 MB manifests completes within `≤ 2 × the unmarshal time` measured in the same bench run (§8.2).

### Risks / likely pitfalls

- Non-determinism leaking in via Go's map iteration order when emitting per-section deltas. Mitigation: sort every emitted slice immediately before rendering; lint rule forbids `fmt.Fprintf` over unsorted map ranges in `internal/diff/`.
- Forgetting to cover the "hash equal, content unequal" tool-bug path; the diagnostic is required because otherwise a latent emitter regression will be silent. Add a deliberately-buggy test pair that triggers the diagnostic, check the diagnostic text.
- Markdown rendering may drift if reviewers hand-edit committed golden files. Mitigation: commit only the JSON goldens and render Markdown at test time, diffing against a small committed Markdown template that names sections but not contents.

---

## Phase 6 — Load-Mode Calibration (SPEC §4.3, §7.5)

**Goal:** ship `aperture eval loadmode` as an empirical measurement tool. For each fixture, produce "Plan_A (normal)" and "Plan_B (forced-full)," report symbolic differences always, and pass/fail deltas when the fixture declares `agent_check`. Does not change the planner. Emits an advisory threshold-tuning recommendation, never applies one automatically.

### Files to create

- `internal/eval/loadmode.go` — orchestrator: run the planner twice per fixture (normal, then with a "no-demotion shim" that keeps every full-eligible candidate at `full`); collect symbolic-difference structs; invoke `agent_check` (when declared) once per plan; format the per-fixture report.
- `internal/pipeline/no_demotion.go` — adds a single boolean `SuppressDemotion bool` field to `pipeline.BuildOptions`. When `true`, the existing demotion pass (invoked inside `Build`) is skipped; budget overflow is tolerated and the overflow token count is written into `pipeline.Result.LoadModeReport`. When `false` (the zero value used by every non-eval caller), behavior is byte-identical to v1.0 `Build`. **Implementation strategy (chosen to avoid both code duplication and a separate shim function):** one `if opts.SuppressDemotion { skip demotion pass } else { run demotion pass }` branch inside the existing `Build` function. No second entry point; no separate package. The planner body is shared, and the only difference between a normal plan and a loadmode-no-demotion plan is one boolean on `BuildOptions`.
- **Guard against production misuse:** `cmd/aperture/main.go` and every file under `internal/cli/` MUST NOT set `SuppressDemotion: true`. This is enforced by a `forbidigo` lint rule (added to `.golangci.yml` in Phase 6) that forbids the literal string `SuppressDemotion: true` in any file outside `internal/eval/` and `internal/pipeline/*_test.go`. A unit test asserts that no code path reachable from `cmd/aperture` sets the field.
- `internal/eval/agent_check.go` — wraps `os/exec` to run the fixture's declared command. `cmd.Env` is built as an **explicit allowlist** and never inherits the parent environment: the seven `APERTURE_*` variables documented in §7.1.1 plus `PATH` (so the shell can find the command's own dependencies). Any other parent-env variable — including any credential-bearing names the CI environment may expose — is stripped. Timeout parsed with `time.ParseDuration`; integer timeout values rejected (exit 2). SIGKILL on wall-clock exceed (fail); exit-0 → pass; non-zero → fail; command-not-found → eval aborts with exit 1.
- `internal/eval/agent_check_test.go` — unit test that plants two sentinel variables named `APERTURE_SECRET_SENTINEL` and `FAKE_PROVIDER_TOKEN_SENTINEL` in the parent env (both with the literal value `should-not-leak`) before invoking the wrapper on a small shell script that dumps its own `env` to a file. The test asserts neither sentinel appears in the dumped environment. Sentinel names are deliberately fictional so the test never implies a real token is involved.
- `testdata/eval/loadmode-improvement/` — one fixture where `Plan_A` (normal budget) fails `agent_check` and `Plan_B` (forced-full) passes; exercises `IMPROVEMENT`.
- `testdata/eval/loadmode-no-change-pass/` — one fixture where both `Plan_A` and `Plan_B` pass `agent_check`; exercises `NO_CHANGE_PASS`.
- `testdata/eval/loadmode-regression/` — one fixture where `Plan_A` passes and `Plan_B` fails (constructed by forcing-full deliberately exceeding the declared budget in a way the agent-check detects); exercises `REGRESSION`.
- `testdata/eval/loadmode-no-change-fail/` — one fixture where both `Plan_A` and `Plan_B` fail (the `agent_check` targets behavior unrelated to selection, proving the delta classification is not conflated with unrelated failures); exercises `NO_CHANGE_FAIL`.
- `internal/eval/advisory.go` — threshold-tuning advisor per §7.5.1 / §7.5.2: averages agent-check pass rates across fixtures that declared `agent_check` and emits the `+25% avg_size_kb` recommendation only when `Plan_A` rate ≥ 10 pp below `Plan_B`. Advisory only; never applied.
- `testdata/eval/loadmode-smoke/` — one fixture with a small `agent_check` that always exits 0 (smoke test for the env-var contract). This fixture does not exercise the threshold tuning rule.

### Files to modify

- `internal/cli/eval.go` — register `loadmode` subcommand; flags `--fixtures`, `--format`, `--out`.
- `internal/pipeline/…` — expose the no-demotion entry point via a new internal function; the public planner API is unchanged.
- `internal/manifest/…` — no changes; the loadmode report is its own artifact, not a manifest.
- `schema/loadmode.v1.json` — JSON Schema for the loadmode report, validated by the golden tests.

### Key decisions

- **Symbolic differences are always reported.** §7.5.1 makes this unconditional. `agent_check` is a *layer* on top, not a precondition.
- **Advisory only.** The +25% rule is the single threshold-tuning signal, produced across *all* fixtures that declared `agent_check`. No per-fixture or per-file variant. The recommendation is machine-readable in JSON and printed prominently in Markdown, but the planner does not ingest it.
- **`forced_full_would_underflow` is a boolean data point.** §7.5.1 explicitly says: this does not raise an error or stop the report. The loadmode command records it and moves on.
- **Timeouts on `agent_check` fail the fixture, don't abort eval.** §7.7 error table draws the line: timeout = recorded fixture fail, eval continues; command-not-found = misconfigured harness, eval aborts exit 1.
- **Delta enum is a closed set of four values.** `IMPROVEMENT`, `REGRESSION`, `NO_CHANGE_PASS`, `NO_CHANGE_FAIL` (§7.5.1). Any other value is a tool bug.

### Acceptance criteria

- `aperture eval loadmode --fixtures testdata/eval/` runs all fixtures, emits per-fixture reports with: demoted-in-A-held-in-B paths, overflow token count, feasibility delta, gap delta, `forced_full_would_underflow` boolean, and (when `agent_check` declared) the four-valued delta enum.
- All four delta-enum values have fixture coverage: `IMPROVEMENT` (loadmode-improvement), `REGRESSION` (loadmode-regression), `NO_CHANGE_PASS` (loadmode-no-change-pass), `NO_CHANGE_FAIL` (loadmode-no-change-fail). A unit test enumerates the four cases against mocked pass/fail outcomes to catch classification bugs independent of real subprocess behavior.
- Fixture whose `agent_check.timeout: 30s` declares a command that exceeds the budget → fixture marked fail, eval exits 0, report enumerates the timeout clearly.
- Fixture whose `agent_check.command` does not exist → eval exits 1 with a clear error.
- Fixture declaring `agent_check.timeout: 30` (integer, no units) → fixture rejected with exit 2 at load.
- Advisory rule emits a recommendation only when the averaged delta ≥ 10 pp; unit tests cover both "emit" and "suppress" sides.
- Loadmode report JSON validates against `schema/loadmode.v1.json`.
- 20-run byte-identity of the loadmode report on the smoke fixture modulo the per-run section (same rule as eval).

### Risks / likely pitfalls

- The no-demotion behavior is a liability if reachable from `aperture plan`. Mitigation: the feature is a single boolean on `BuildOptions` whose zero value is the safe one, plus a `forbidigo` rule forbidding `SuppressDemotion: true` anywhere outside `internal/eval/` and `internal/pipeline/*_test.go`, plus a unit test that asserts no production code path reachable from `cmd/aperture` sets the field to `true`.
- Agent-check commands are untrusted user code. Mitigation: run them in a subprocess with the env vars only (no stdin from Aperture); document clearly that `aperture eval loadmode` executes user-provided shell.
- Implementers may try to cache `agent_check` outcomes across runs. Don't: §7.5.1 requires two invocations per fixture per run for determinism of the report's pass/fail layer.
- `time.ParseDuration` accepts `"1h"` which is almost certainly too long for `agent_check` timeouts. The SPEC does not cap; users are trusted. A warning is emitted when the parsed duration exceeds 5 minutes, but no error.

---

## Phase 7 — Hardening, Fixture Set, and Release (SPEC §12)

**Goal:** reach the full v1.1 release bar. Bring the committed fixture set to ≥ 10 cases covering the §12.1 categories, gate the 1.2× ripgrep ratio (§12.2), re-verify every determinism and performance target, refresh the docs, cut `v1.1.0`.

### Files to create

- `testdata/eval/*` — additional fixtures to reach ≥ 10 covering:
  - pure Go (reuse two of Phase 2's);
  - polyglot Go+TS (the resolver fixture from Phase 4);
  - polyglot Go+Python;
  - monorepo subtree (the billing fixture from Phase 3);
  - small-repo single-file task;
  - large-repo budget-pressure task;
  - **combined polyglot + scope + dampener integration fixture (§11.4):** a repo with Go + TypeScript + Python files plus one Rust file (tier-3 fallback); `--scope services/<name>` set on the plan invocation; dampener at default; `agent_check` declared so the fixture also participates in `aperture eval loadmode`. This fixture exercises the full v1.1 feature intersection end-to-end; its acceptance is enumerated as a standalone criterion in §12 below.
  - one more small-repo single-file task to round out §12.1's category list.
- `docs/eval.md`, `docs/scope.md`, `docs/tier2.md`, `docs/diff.md`, `docs/loadmode.md` — user-facing documentation for each new feature.
- `CHANGELOG.md` — v1.1 entry enumerating: `aperture eval {run,baseline,loadmode,ripgrep}`, `aperture diff`, `--scope`, tier-2 TS/JS/Python, mention dampener, `selection_logic_version` bump.

### Files to modify

- `README.md` — add v1.1 section with 5-minute tour of `aperture eval run`, `--scope`, `aperture diff`, and tier-2.
- `docs/` — regenerated via `clarion pack` after release-branch freeze.
- `Makefile` — `make release` target that runs build + test + race + lint + bench + eval in one shot and refuses to proceed if any of those fails. **`clarion verify` runs as a final informational step** (non-blocking): its exit code is captured and printed but does not fail `make release`. This resolves the tension between project-practice gates and the normative release contract (§12): the normative bar is the 11 acceptance criteria; `clarion` is an advisory overlay. If the project later promotes `clarion` to a release contract, that promotion is a separate SPEC change, not a PLAN decision.
- `.github/workflows/ci.yml` — CI gate steps: `aperture eval run --fixtures testdata/eval/` (exit 2 on regression fails CI); `aperture eval ripgrep` with a threshold check enforcing §12.2 (Aperture F1 ≥ 1.2 × ripgrep F1 on every fixture); determinism-harness 20-run byte-identity suite; `make bench` performance-ratio check; `make check-grammar-licenses`.

### Key decisions

- **The 1.2× ripgrep bar is enforced, not aspirational.** §12.2 names the exact threshold, the `--top-n 20` comparison point, and the scope (every fixture). Phase 7 CI fails the build if any fixture violates it.
- **Determinism gate runs the 20-run identity suite for `aperture eval run`, `aperture diff`, and scope-restricted plans** — §11.3's exact enumeration. If any produces a byte-diff, release blocks.
- **`selection_logic_version` bump already landed in Phase 2** — no further bump at release time. Phase 7 verifies only that generated manifests read `"sel-v2"`.
- **External pipeline gates (`prism`, `realitycheck`, `clarion`, `verifier`) are project practice, not release contract** — §12's final note is explicit. The project CI pipeline still runs them because CLAUDE.md requires it, but their green/red status is informational for the v1.1 contract.

### Acceptance criteria (mirrors §12)

1. Committed fixture set ≥ 10 cases covering every §12.1 category.
2. `aperture eval ripgrep` shows Aperture F1 ≥ 1.2 × ripgrep F1 on every fixture at `--top-n 20`.
3. Mention-dampener fixture pair (false-positive + counter-example) is part of the committed fixture set.
4. `polyglot-resolver` fixture selects `resolver.ts` at `relevance_score ≥ 0.70`; `missing_tests` considers `.test.ts`.
5. Monorepo scoped plan produces distinct `manifest_hash`; `ambiguous_ownership` resolution stays inside the subtree.
6. `aperture diff` reports empty non-metadata deltas on two identical back-to-back plans.
7. Manifests generated by the release binary read `selection_logic_version: "sel-v2"`.
8. Every v1 acceptance criterion (initial SPEC §19) still passes.
9. `go test ./...`, `golangci-lint run ./...`, `go test -race ./...` all green.
10. `make bench` reports §7.3.5 and §8.2 performance ratios within target — specifically, the Phase-4 `polyglot:medium` cold- and warm-plan ratios (≤ 1.20), the `aperture eval run` overhead ratio (≤ 1.5× the sum of per-fixture cold-plan wall times, §8.2), and the `aperture diff` ratio (≤ 2× the unmarshal time of the two input manifests, §8.2).
11. §11.3's 20-run byte-identity suites all pass (`aperture eval run`, `aperture diff`, scope-restricted plans).

Release tag: `v1.1.0`.

### Risks / likely pitfalls

- Fixture accretion causes CI to run long. Mitigation: target total `eval run` wall time under 1 minute on the reference machine; if any single fixture dominates, split it or shrink its `repo/` snapshot.
- A hardware-specific `make bench` target failing on CI runners but passing on developer laptops. Mitigation: §7.3.5 and §8.2 target *ratios*, not absolute times; CI must compare `polyglot:medium` ratios within the same run, not to a historical baseline.
- Late-discovered v1 regression from one of the v1.1 phases (e.g., a dampener-induced shift in a v1 fixture's top pick). Mitigation: every phase's exit check already requires v1 acceptance to still pass; Phase 7 re-runs the v1 suite end-to-end as the final gate.
- The ripgrep 1.2× bar is surprisingly tight on short-anchor tasks; a fixture author may unknowingly write anchors that defeat it. Mitigation: when `aperture eval ripgrep` runs in `--verbose` mode it already prints per-fixture F1s and the top-N ripgrep candidates; no new flag is added. The Phase 7 fixture authoring workflow documents this diagnostic path in `docs/eval.md`.

---

## Phase Ordering and Dependencies

Phases are strictly sequential. A phase MUST NOT begin until the previous phase has merged to `main` with all exit checks passed. Specifically:

- Phase 2 requires Phase 1's eval harness and the committed `baseline.json` from Phase 1 — the dampener fixtures register against that baseline when Phase 2 commits.
- Phase 3 requires Phase 1 (monorepo fixture is committed with a baseline entry).
- Phase 4 requires Phase 1 and a **pre-Phase-4 binding verification task** (below). It does not depend on Phase 2 or Phase 3. **Phase 4 exit check (hard gate, not advisory):** `specs/v1.1/BINDING_VERIFICATION.md` is committed to the repository and has been reviewed and approved (signoff recorded at the bottom of the file); `.goreleaser.yml` is committed; a `v1.1.0-rc0` release tag has produced all five platform artifacts successfully (the smoke-test run under Pre-Phase-4 CGo Toolchain Setup). A PR that attempts to merge Phase 4 code without all three conditions met is blocked by a `pr-gate` CI check that greps for the verification file and artifact manifest.
- Phase 5 requires Phase 3 (the scope-delta golden pair depends on `scope` being in the manifest schema).
- Phase 6 requires Phase 1 (eval harness) and Phase 4 (tier-2 support for polyglot loadmode fixtures, if any).
- Phase 7 requires Phases 1–6 complete.

### Pre-Phase-4 Binding Verification

A blocking prerequisite to Phase 4 that MUST complete before Phase 4 implementation begins:

- Clone the current tip of `github.com/smacker/go-tree-sitter`.
- Verify that `typescript.GetLanguage()` and `tsx.GetLanguage()` resolve to distinct, non-equal parsers; likewise `javascript.GetLanguage()` and `jsx.GetLanguage()` (if the JSX parser exposes a dedicated entry; if not, the plan's documented fallback — linking the upstream C grammar directly — activates).
- Pin the verified commit SHA and module version in `go.mod` on a short-lived branch.
- Commit the verification report (a one-page Markdown file at `specs/v1.1/BINDING_VERIFICATION.md`) enumerating: commit SHA, resolved parser addresses or a clear "fallback path required" statement, Linux/macOS/Windows build test output, and licensing confirmation.
- **Failure-path decision tree** (executed at verification time, recorded in the report):
  - If TSX is not a distinct parser from TypeScript → activate the fallback path: add `third_party/tree-sitter-typescript/tsx_binding.go` with a direct cgo binding to the upstream C grammar's `tree_sitter_tsx()` entry. Document in the report.
  - If JSX is not a distinct parser from JavaScript → same fallback for `tree_sitter_tsx` / `tree_sitter_javascript` entry (upstream commonly bundles JSX under a distinct name even when the Go binding does not expose it).
  - If Python parser is unavailable or licensing-incompatible → Python is dropped from v1.1 tier-2; `languages.python.enabled` defaults to `false` and the SPEC §5.4 tier-2 list is updated to exclude Python. This outcome MUST be escalated to the project owner before Phase 4 proceeds, since it changes the v1.1 acceptance criteria in §12.1 (Go+Python polyglot fixture).
  - If all three fallbacks are required and all cgo-link cleanly → Phase 4 proceeds with the fallback bindings, the verification report documents the shift, and `internal/lang/tstree/doc.go` records "fallback path active."
  - If any required grammar cannot be linked at all → Phase 4 is **blocked**; escalate to project owner; v1.1 release is deferred until an alternative binding is identified or the SPEC is amended to drop the affected language.
- **Approval authority.** `specs/v1.1/BINDING_VERIFICATION.md` ends with a `## Approval` section containing the approving reviewer's GitHub handle, the approval date (ISO 8601 UTC), and an explicit `APPROVED` or `BLOCKED` verdict. The approver is the **project owner** listed in `CODEOWNERS` for `internal/lang/` (falling back to the repository-root `CODEOWNERS` entry if no path-specific owner exists). The approver may not be the same person who authored the verification report. A `pr-gate` CI check scans the file on any PR that touches `internal/lang/tstree/` and fails the PR if the `APPROVED` verdict is missing or the approver matches the PR author.

### Pre-Phase-4 CGo Toolchain Setup

Also blocking Phase 4:

- Choose between (a) **goreleaser-with-zig** for all five target platforms (lets x86_64 GHA runners produce arm64 Linux binaries without a native runner) or (b) **native GHA runners** per target (slower CI but conventional). The plan recommends (a) unless zig cross-compilation proves unreliable for cgo during the verification step.
- Commit `.goreleaser.yml` with the chosen configuration.
- Add a GHA workflow matrix under `.github/workflows/release.yml` that runs `goreleaser release --snapshot --clean` on tag pushes and uploads artifacts to GitHub Releases.
- Smoke-test the matrix on a pre-release tag (`v1.1.0-rc0`) to confirm all five platform artifacts build and run `aperture version` successfully. **Smoke-test acceptance criterion (explicit):** the GitHub Actions run for the `v1.1.0-rc0` tag shows all five matrix jobs green, the release draft lists five artifacts (one per target), and a manual download-and-run check on Linux/amd64 and macOS/arm64 (the two most common developer platforms) confirms `./aperture version` prints a semver string and exits 0. The run URL is recorded in `specs/v1.1/BINDING_VERIFICATION.md` alongside the binding verdict.

---

## Cross-Cutting Invariants

These apply to every phase and are checked in every phase's exit gate:

1. **v1 determinism is never degraded.** `manifest_hash` for a v1.0-compatible plan input (no scope, no dampener config change, no tier-2 files) remains byte-identical to what v1.0 produced — except where `selection_logic_version` changes deliberately bump the value (Phase 2 only).
2. **No network I/O on repo contents.** v1.1 introduces `os/exec` (`rg`, `agent_check`) but no HTTP, no DNS, no package fetching. The `vendor-grammars` Makefile target is the one network-touching workflow and is reviewer-only.
3. **Rule-based-gap discipline holds.** Eval measures quality but does not feed its results back into the selector (§2.2.5).
4. **Additive schema only.** Every manifest field added in v1.1 is optional; v1.0 consumers MUST keep working.
5. **If forced to choose between "clever" and "predictable," choose predictable.** v1 §23, restated for v1.1.
