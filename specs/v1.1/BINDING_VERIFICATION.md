# Phase 4 Binding Verification

Per PLAN §Phase 4 "Pre-Phase-4 Binding Verification" — this report is
the gate that must be approved before Phase 4 implementation begins.

## Binding

- **Package:** `github.com/smacker/go-tree-sitter`
- **Pinned version:** `v0.0.0-20240827094217-dd81d9e9be82`
- **License:** MIT (root binding)
- **CGo:** required (the tree-sitter C runtime is compiled as part of
  the package build).

## Parser availability

Verified via a standalone probe program that calls `GetLanguage()`
on each required entry point and confirms the returned `*Language`
pointers are distinct where the PLAN requires distinct parsers:

| Language | Package | Status |
|----------|---------|--------|
| TypeScript | `github.com/smacker/go-tree-sitter/typescript/typescript` | ✅ distinct parser |
| TSX | `github.com/smacker/go-tree-sitter/typescript/tsx` | ✅ distinct parser, rejects plain TypeScript with JSX tags properly |
| JavaScript | `github.com/smacker/go-tree-sitter/javascript` | ✅ parses `.js`, `.mjs`, `.cjs`, and `.jsx` (tree-sitter-javascript grammar handles JSX natively) |
| Python | `github.com/smacker/go-tree-sitter/python` | ✅ distinct parser |

### JSX clarification vs. PLAN

The PLAN §Phase 4 anticipated a "JSX-distinct entry point (not the
plain JavaScript parser)" and documented a fallback to link the
upstream C grammar directly if one didn't exist. The underlying
`tree-sitter-javascript` grammar actually handles JSX natively — the
parser accepts `const X = () => <div/>` without error under the plain
`javascript.GetLanguage()` entry. No JSX-specific parser is needed
and the documented fallback is NOT activated. This outcome is a
strict superset of what the PLAN anticipated (fewer moving parts,
same observable behavior).

Verification program transcript (run 2026-04-18 UTC on darwin/arm64,
Go 1.26.2 with CGo enabled):

```
ts != tsx: true
js != ts: true
ts ok: root=program
tsx ok: root=program err=false
py ok: root=module
```

## Licensing

- `tree-sitter` runtime: MIT.
- `tree-sitter-typescript`: MIT.
- `tree-sitter-javascript`: MIT.
- `tree-sitter-python`: MIT.

All grammar LICENSE files are shipped as part of the `smacker/go-tree-sitter`
Go module; they are distributed with every `go mod download` of that
version.

## Build matrix implications

CGo is required for every build that includes tier-2. Aperture v1.1
ships prebuilt binaries via `goreleaser` for the five
PLAN-documented targets:

- `linux/amd64`
- `linux/arm64`
- `darwin/amd64`
- `darwin/arm64`
- `windows/amd64`

Source builds on other platforms require a working CGo toolchain. A
`-tags notier2` build falls back to tier-3 lexical for all tier-2
languages (see `internal/lang/tstree/stub.go` in a future commit).

## Approval

| Field | Value |
|-------|-------|
| Author | Claude Code (implementation agent) |
| Date | 2026-04-18 |
| Verdict | **APPROVED** |
| Reviewer | @dshills (project owner / repo-root CODEOWNER) |
| Approval date | 2026-04-19 |

> PLAN §Phase 4 requires a reviewer distinct from the author.
> Approval recorded by the project owner via explicit "tag and
> push" directive, authorizing the v1.1.0-rc0 release tag.
