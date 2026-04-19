# Changelog

## v1.1.0 — unreleased

The v1.1 release closes the credibility gaps called out by the
post-v1.0 external review: you can now measure selection quality,
defuse gameable-mention false positives, analyze polyglot repos
with real symbol-level scoring, project plans onto monorepo
subtrees, and debug plan-to-plan deltas with a single command.
It is strictly additive on top of v1.0 — every v1.0 manifest
field, exit code, determinism guarantee, and CLI flag still
works unchanged.

### Added

- **`aperture eval`** command group for selection-quality
  regression testing.
  - `eval run` — scores committed `*.eval.yaml` fixtures
    against `baseline.json`; exit 2 on regression.
  - `eval baseline` — reviewer-only baseline regeneration
    with `--force` override.
  - `eval ripgrep` — compares Aperture against a naive
    ripgrep-top-N baseline at the PLAN's 1.2×-F1 bar.
  - `eval loadmode` — v1.1 §7.5.1 load-mode calibration
    harness; reports symbolic Plan_A vs. Plan_B diffs and
    optional agent_check deltas.
  - See `docs/eval.md` and `docs/loadmode.md`.
- **Mention dampener** — `s_mention` is now clamped by
  `min(1.0, floor + slope · max(s_symbol, s_filename,
  s_import, s_package))` so an incidental path reference
  (or pasted stack trace) can no longer dominate the plan
  when no other signal agrees. Default `floor=0.30`,
  `slope=0.70`. Configurable via
  `scoring.mention_dampener` in `.aperture.yaml`.
- **`selection_logic_version` bumped to `sel-v2`** — cache
  keys invalidate atomically with the dampener default-on
  flip. Any docs-only patch bump leaves the cache warm.
- **`--scope <path>`** on `plan`, `explain`, `run`.
  Narrows candidate generation, scoring evidence, and
  `ambiguous_ownership` resolution to a subtree; emits an
  additive `scope` manifest field that folds into
  `manifest_hash`. Repo fingerprint still covers the full
  tree. See `docs/scope.md`.
- **Tier-2 language analysis** — TypeScript (.ts, .tsx),
  JavaScript (.js, .mjs, .cjs, .jsx), and Python (.py) now
  produce module-level symbols and imports via
  tree-sitter. Contributes to `s_symbol` / `s_import`
  scoring and `ambiguous_ownership` peer counts. Per-
  language opt-out via `languages.<name>.enabled`. New
  `generation_metadata.language_tiers` map in every
  manifest. See `docs/tier2.md`.
- **JS/TS/Python test linking** — `foo.test.<ext>` /
  `foo.spec.<ext>` pair with `foo.<ext>` (with priority
  `.tsx > .ts > .mjs > .cjs > .jsx > .js`); `test_foo.py`
  / `foo_test.py` with `foo.py`. `conftest.py` is
  surfaced as a supplemental file.
- **`aperture diff <A.json> <B.json>`** — section-by-section
  delta between two manifests. Never invokes the planner or
  opens the repo. Markdown and JSON output. Schema-version
  comparison is integer major.minor. See `docs/diff.md`.
- **Binary release pipeline** — `.goreleaser.yml` + GitHub
  Actions workflow build CGo-enabled binaries for
  linux/amd64, linux/arm64, darwin/amd64, darwin/arm64,
  windows/amd64 on tag push.
- **`-tags notier2` build flag** — pure-Go fallback that
  stubs tier-2 analysis (all tier-2 files drop to
  `tier3_lexical`). Useful for `CGO_ENABLED=0` builds or
  security reviews.
- **≥ 10 committed `testdata/eval` fixtures** covering every
  §12.1 category: pure Go, polyglot Go+TS, polyglot
  Go+Python, monorepo subtree, small-repo single-file,
  large-repo budget-pressure, mention-dampener false-
  positive + counter-example, loadmode smoke, and a
  combined polyglot+scope+dampener integration fixture.

### Changed

- `BreakdownEntry` gains an optional `dampener` field (float
  in [floor, 1.0]). Omitted when the mention dampener is
  disabled so v1.0 consumers round-trip byte-identical.
- `GenerationMetadata` gains an optional `language_tiers`
  map.
- `Selection` rationale for an admitted out-of-scope
  supplemental includes `"outside_scope_supplemental"`.
- Scope validation, tier-2 binding, and load-mode harness
  exit-code mappings documented in SPEC §7.7.

### Guarantees preserved

- All v1.0 determinism guarantees hold. 20-run byte-identity
  tests cover `aperture eval run`, `aperture diff`, and
  scope-restricted plans.
- Schema additive: v1.0 consumers reading a v1.1 manifest
  continue to work unchanged.
- No network I/O on repo contents. Tree-sitter grammars are
  vendored through the pinned `smacker/go-tree-sitter`
  module.
- Rule-based gap detection remains rule-based — eval
  measures quality but never writes back into the selector.

### Known limits

- Tier-2 symbol extraction covers module-level declarations
  only. Nested `function`/`class` declarations inside
  function bodies or conditionals are deliberately skipped
  (§7.3.2).
- `export { x }` re-export clauses are not yet emitted as
  separate symbols; the underlying declaration is indexed.
  Tracked for a follow-up release.
- The §7.5.0 viability threshold (0.30) means some very
  small fixture repos never cross into `selections[]` —
  their committed F1 of 0 is a stable regression anchor,
  not a quality signal.
