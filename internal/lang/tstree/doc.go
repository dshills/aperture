// Package tstree provides v1.1 tier-2 (§5.4 tier2_structural) source
// analysis for TypeScript, JavaScript, and Python via tree-sitter.
//
// Binding: github.com/smacker/go-tree-sitter, pinned at
// v0.0.0-20240827094217-dd81d9e9be82 (see
// specs/v1.1/BINDING_VERIFICATION.md).
//
// Scope of extraction (per SPEC §7.3.2):
//
//   - Only MODULE-LEVEL declarations are emitted. Declarations nested
//     inside function bodies, class bodies, if/try/switch blocks, etc.
//     are silently skipped.
//   - TypeScript/JavaScript: function, class, interface, type,
//     function-valued const (RHS is arrow_function or
//     function_expression), plain const/let/var (kind=variable).
//     Anonymous `export default` uses the literal name "default".
//   - Python: def, async def, class, and single-identifier assignments.
//     Lambda-valued assignments → kind=function. Names starting with
//     `_` are Exported=false.
//   - Imports: every `import ... from "..."` specifier, verbatim, and
//     for CommonJS `.js`/`.cjs`: a top-level `require("...")` call
//     (strictly: callee is the identifier `require`, single string arg,
//     enclosing statement is a direct child of the program node).
//   - Relative import paths are NOT resolved in v1.1.
//
// Parse errors: tree-sitter always returns a tree. A parse is
// "failed" iff the root node is ERROR or has_error() is true. On
// failure, Parse returns (nil, ParseError=true). The caller records
// ParseError=true on the FileEntry and the file contributes
// s_mention / s_filename / s_doc only (no symbol / import signal).
//
// CGo: this package triggers a CGo build of the tree-sitter C runtime.
// Building with `-tags notier2` activates the stub in stub.go which
// returns (nil, ParseError=true) for every input — useful for
// CGO_ENABLED=0 or cross-compiles without a C toolchain.
package tstree
