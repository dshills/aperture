# Scope projection: `--scope`

The v1.1 `--scope <path>` flag narrows candidate generation,
scoring evidence, and `ambiguous_ownership` detection to a
subtree of the repo. It's a **projection**, not a sub-repo: the
walker still walks the whole repo, `repo.fingerprint` still
covers the full tree, and supplemental files (`SPEC.md`,
`README.md`, top-level config) stay admissible even outside
scope.

## Usage

```
aperture plan TASK.md --scope services/billing
aperture explain TASK.md --scope services/billing
aperture run claude TASK.md --scope services/billing
```

Also supported in `.aperture.yaml`:

```yaml
defaults:
  scope: services/billing
```

CLI `--scope` overrides config. The sentinels `--scope ""` and
`--scope .` explicitly unset any config-declared scope for that
invocation.

## Path rules

- Repo-relative, forward-slash. Leading `./` and trailing `/`
  are stripped. `/./` collapses to `/`.
- `..` anywhere in the path is rejected (exit 4).
- Absolute paths (`/etc/...`) are rejected.
- Must resolve (through symlinks) to a directory inside the
  repo root.
- On case-insensitive filesystems (macOS default APFS, NTFS)
  the typed casing is per-segment rewritten to match the
  on-disk casing so the stored `scope.path` is byte-identical
  across clones on case-sensitive hosts.

## What gets emitted

A scoped plan carries a new top-level field:

```json
"scope": { "path": "services/billing" }
```

The `scope` is part of the manifest hash. Two plans over the
same task, budget, and repo — one with `--scope`, one without
— produce distinct `manifest_hash` values.

Supplementals admitted outside scope carry
`"outside_scope_supplemental"` as a rationale token so the
manifest stays auditable about why a non-scoped file made the
selection set.

## Exit codes

- Invalid scope path → exit 4 (`exitCodeBadRepo`).
- Scope leaves zero planable candidates AND no supplemental
  admits → exit 9 (reuses the v1 §7.6.5 budget-underflow code
  per §7.7 clarification).
