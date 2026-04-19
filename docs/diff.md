# Plan-to-plan comparison: `aperture diff`

`aperture diff A.json B.json` explains what changed between two
manifests. It's strictly read-only: the command does NOT invoke
the planner, does NOT open the repository, does NOT recompute
the manifest hash.

## Usage

```
aperture diff manifest-a.json manifest-b.json
aperture diff old.json new.json --format json
aperture diff a.json b.json --format markdown --out diff.md
```

Exit codes: **0 always** (diffing is informational). Exit 1 only
on I/O or parse errors, or when a manifest declares an
unsupported `schema_version` (< "1.0").

## Sections

The diff always emits every section in a fixed order per SPEC
§4.5 / §7.6.2. Empty sections render an `_unchanged_` marker so
a consumer can tell "checked, no delta" apart from "not
computed":

- **Hash and ID** — `manifest_hash`, `manifest_id`,
  `config_digest`. When config digests differ, the output
  points at the digest divergence as the authoritative
  signal (v1 manifests don't embed the full resolved config).
- **Task** — anchors added/removed, action-type change,
  first differing line of raw text.
- **Repo** — fingerprint, language hints.
- **Budget** — model, token ceiling, effective context,
  estimator.
- **Scope** — v1.1 `scope.path` changes.
- **Selections** — added, removed, load-mode-changed.
- **Reachable** — added, removed, promoted-to-selection.
- **Gaps** — added, resolved, severity-changed (matched on
  `(Type, Description)` to preserve multiple gaps of the same
  type).
- **Feasibility** — score delta + sub-signal deltas.
- **Generation metadata** — `aperture_version`,
  `selection_logic_version`.

## Semantic equivalence

The top-of-output `semantic_equivalent` banner reports
`manifest_hash(A) == manifest_hash(B)`. When equal, every
structural section is empty except the six per-run fields that
SPEC §7.9.4 explicitly excludes from the hash: `manifest_id`,
`generated_at`, `generation_metadata.{host, pid,
wall_clock_started_at}`, `aperture_version`.

A non-empty delta under hash agreement is a **tool-level bug**
— the manifest emitter diverged from the hash contract. The
diff surfaces this as a `tool_bug_diagnostic` list so silent
emitter regressions can't hide.

## Schema version handling

Both inputs must declare `schema_version ≥ "1.0"`. Comparison
uses major-then-minor integer parsing (so `"1.10"` > `"1.9"`,
not the lexicographic reverse). Single-component versions
(`"2"`) default minor to 0 for forward compatibility.

## Determinism

`aperture diff` on identical inputs produces byte-identical
output. The v1.1 gate runs the command 20 times over a fixed
manifest pair and asserts the results match.
