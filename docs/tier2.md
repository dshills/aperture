# Tier-2 languages: TypeScript, JavaScript, Python

v1.1 adds tree-sitter-backed structural analysis for three new
languages. Module-level symbols and import specifiers from
`.ts`/`.tsx`/`.js`/`.jsx`/`.mjs`/`.cjs`/`.py` files now
contribute to `s_symbol` and `s_import` scoring alongside Go.

## Tier matrix

| Tier | Name | Capabilities | Languages |
|------|------|--------------|-----------|
| 1 | `tier1_deep` | symbols + imports + side-effect tags + test linking | Go |
| 2 | `tier2_structural` | symbols + imports + test linking | TypeScript, JavaScript, Python |
| 3 | `tier3_lexical` | filename + doc tokens only | everything else |

Every manifest carries a `generation_metadata.language_tiers`
map recording which tier each observed language landed at:

```json
"language_tiers": {
  "go": "tier1_deep",
  "typescript": "tier2_structural",
  "python": "tier2_structural",
  "markdown": "tier3_lexical"
}
```

## Enabling / disabling

Defaults are on for all three tier-2 languages. Opt out per
language in `.aperture.yaml`:

```yaml
languages:
  typescript:
    enabled: false
```

A disabled language drops to `tier3_lexical` — filename + doc
tokens only. Useful when a fixture hits a grammar edge case or
when a security review hasn't cleared the tree-sitter dependency.

## CGo and the `notier2` escape hatch

Tier-2 is implemented via
`github.com/smacker/go-tree-sitter` (MIT, pinned commit recorded
in `specs/v1.1/BINDING_VERIFICATION.md`). It links the
tree-sitter C runtime, so builds require CGo.

For environments without a C toolchain, build with
`-tags notier2`:

```
go build -tags notier2 ./cmd/aperture
```

Under `notier2` every tier-2 language silently falls back to
`tier3_lexical` and the binary builds pure-Go. Prebuilt
binaries (from `make release` via goreleaser) always have CGo
enabled and tier-2 available.

## What's extracted

### TypeScript / TSX / JavaScript

Only module-level declarations. Nested `function`/`class`/`const`
inside function bodies, class bodies, conditionals, etc. are
skipped intentionally.

- `export function`, `export class`, `export interface`,
  `export type` — emitted with `Exported: true`.
- `export const NAME = <arrow-fn | function-expression>` →
  `SymbolFunc` kind.
- `export const NAME = <anything else>` → `SymbolVar` kind.
- `export default function|class|<expr>` → symbol name
  `"default"`, kind reflects the RHS.
- Non-exported module-level declarations → `Exported: false`.
- `import ... from "X"` — specifier `X` recorded verbatim.
- `require("X")` — recorded only when callee is a bare
  `require` identifier, single string literal argument, and
  the enclosing statement is directly under `program`.

### Python

- Module-level `def`, `async def`, `class` → symbols.
- Module-level assignment `NAME = RHS`: `SymbolFunc` if RHS is
  a direct `lambda`, else `SymbolVar`.
- Visibility: name starting with `_` → `Exported: false`.
- `import X` / `from X import ...` record module name `X`.
- Relative imports (`from . import foo`, `from ..util import
  bar`) preserve the literal dot-prefixed string.
- Imports inside functions or conditionals are NOT recorded.

## Test linking

- JS/TS: `<stem>.test.<ext>` and `<stem>.spec.<ext>` link to
  sibling `<stem>.<ext>` with priority
  `.tsx > .ts > .mjs > .cjs > .jsx > .js` when multiple
  production candidates exist.
- Python: `test_<stem>.py` or `<stem>_test.py` link to
  sibling `<stem>.py`.
- `conftest.py` is admitted as a supplemental file (always
  reachable in its subtree).
- Cross-language linking is forbidden — a `.ts` test cannot
  link to a `.py` production file even if the basename stems
  match.
