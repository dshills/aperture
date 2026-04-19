# Selection quality: `aperture eval`

The `eval` command group regression-tests the planner's selection
quality against committed ground-truth fixtures. It's the v1.1
answer to "how do I know the weights are right?" — now you can
score every plan against a declared expected selection set and
catch drops in CI.

## Commands

```
aperture eval run            # run fixtures, exit 2 on regression
aperture eval baseline       # regenerate baseline.json (reviewer-only)
aperture eval ripgrep        # compare vs. a naive ripgrep-top-N baseline
aperture eval loadmode       # §7.5.1 load-mode calibration harness
```

## Fixture layout

Each fixture lives in its own directory:

```
testdata/eval/<name>/
├── <name>.eval.yaml     # task, budget, model, expected selections
└── repo/                # fingerprinted snapshot the planner runs against
    └── ...
```

`repo_fingerprint` pins the snapshot so accidental edits fail the
run (exit 2). The fingerprint is computed as a SHA-256 over every
regular file's (relative path + content hash) under `repo/`,
with a stable schema prefix per SPEC §7.1.1. Symlinks inside
`repo/` are rejected.

## Scoring

For a fixture with expected selection set `E` and the planner's
emitted selection set `A`:

- **Precision** `|E ∩ A| / |A|` (1.0 when A is empty).
- **Recall** `|E ∩ A| / |E|` (1.0 when E is empty).
- **F1** harmonic mean.

When the fixture pins a `load_mode` on an expected selection, a
mismatch counts 0.5 toward the intersection instead of 1.0. A
file in the fixture's `forbidden:` list appearing at
`relevance_score ≥ 0.30` is a **hard failure** independent of
F1. A required gap type (`expected.gaps:`) absent from the
emitted manifest is also a hard failure.

## Regression gate

`aperture eval run` compares each fixture's F1 against
`baseline.json`. A drop exceeding `--tolerance` (default 0.02)
exits 2. Orphaned baseline entries (fixture referenced in
baseline but missing from the run, unfiltered) also exit 2.

Regenerating the baseline is a deliberate reviewer action:

```
aperture eval baseline --force   # overwrite even if some fixtures regressed
```

## Ripgrep baseline (`aperture eval ripgrep`)

The comparator renders each fixture's task anchors into a single
alternation pattern, invokes `rg --count-matches --ignore-case`,
ranks hits by match count then path, truncates to `--top-n`
(default 20), and feeds survivors through the same budget fitter
Aperture itself uses — with every candidate pinned at
`load_mode=full` (ripgrep has no notion of summarization).

The PLAN's normative bar: Aperture F1 ≥ 1.2 × ripgrep F1 on every
fixture at `--top-n 20`. The committed fixture set serves as the
regression anchor.

## Per-run metadata

Every eval report JSON carries a top-level `per_run_metadata`
object with `generated_at`, `wall_clock_duration_ms`, `host`,
`pid`, and `aperture_version`. Markdown reports carry the same
info in a trailing `## Per-Run Metadata` section. Determinism
tests strip exactly that section before comparing byte-identity
across runs.
