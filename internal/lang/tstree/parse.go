//go:build !notier2

package tstree

import (
	"context"
	"sync"

	sitter "github.com/smacker/go-tree-sitter"
	"github.com/smacker/go-tree-sitter/javascript"
	"github.com/smacker/go-tree-sitter/python"
	"github.com/smacker/go-tree-sitter/typescript/tsx"
	"github.com/smacker/go-tree-sitter/typescript/typescript"

	"github.com/dshills/aperture/internal/index"
)

// Lang identifies a tier-2 grammar. Aperture's repo walker maps file
// extensions to these values before calling Parse.
type Lang int

const (
	LangTypeScript Lang = iota + 1
	LangTSX
	LangJavaScript
	LangPython
)

// Result is the per-file output of Parse. Path is the repo-relative
// path used as the cache key; Symbols / Imports are the extracted
// module-level views per §7.3.2.
type Result struct {
	Path       string
	Symbols    []index.Symbol
	Imports    []string
	ParseError bool
}

// parserPool per grammar so we amortize *sitter.Parser allocation
// across files in a single Analyze call. Tree-sitter parsers are
// goroutine-unsafe; the pool lets us pick up a parser per worker.
var parserPools = [5]*sync.Pool{
	{}, // Lang=0 unused
	{New: func() any { p := sitter.NewParser(); p.SetLanguage(typescript.GetLanguage()); return p }},
	{New: func() any { p := sitter.NewParser(); p.SetLanguage(tsx.GetLanguage()); return p }},
	{New: func() any { p := sitter.NewParser(); p.SetLanguage(javascript.GetLanguage()); return p }},
	{New: func() any { p := sitter.NewParser(); p.SetLanguage(python.GetLanguage()); return p }},
}

// LanguageForExtension maps a lowercased file extension (including
// the leading dot) to the Lang value routed to Parse. Returns zero
// when the extension is not a tier-2 target.
func LanguageForExtension(ext string) Lang {
	switch ext {
	case ".ts":
		return LangTypeScript
	case ".tsx":
		return LangTSX
	case ".js", ".mjs", ".cjs", ".jsx":
		return LangJavaScript
	case ".py":
		return LangPython
	}
	return 0
}

// Parse runs the tree-sitter grammar for lang against src. ParseError
// is true when the root node has type "ERROR" or has_error() reports
// any sub-error. On parse error, Symbols and Imports are nil — the
// file still participates in s_mention / s_filename / s_doc scoring
// via the outer pipeline.
//
// `path` is carried through untouched so callers can key caches and
// results without reconstructing the value.
func Parse(ctx context.Context, path string, lang Lang, src []byte) *Result {
	r := &Result{Path: path}
	if lang < LangTypeScript || lang > LangPython {
		r.ParseError = true
		return r
	}
	pool := parserPools[lang]
	parser := pool.Get().(*sitter.Parser)
	defer pool.Put(parser)

	tree, err := parser.ParseCtx(ctx, nil, src)
	if err != nil || tree == nil {
		r.ParseError = true
		return r
	}
	// Tree wraps C-allocated memory — must be explicitly released
	// or every Parse call leaks a grammar-sized chunk of C heap on
	// every file. Deferred Close runs after extraction completes;
	// Symbols / Imports are value-copies of strings that don't
	// reference tree-owned memory past this function.
	defer tree.Close()

	root := tree.RootNode()
	if root == nil || root.Type() == "ERROR" || root.HasError() {
		// §7.3.2: "failed to parse" ⇒ no symbols emitted.
		r.ParseError = true
		return r
	}

	switch lang {
	case LangTypeScript, LangTSX, LangJavaScript:
		r.Symbols, r.Imports = extractTSJS(root, src, lang)
	case LangPython:
		r.Symbols, r.Imports = extractPython(root, src)
	}
	return r
}

// nodeText returns the byte-for-byte source text of n, given the
// source the tree was parsed from. Used for identifier names and
// string literals throughout extraction.
func nodeText(n *sitter.Node, src []byte) string {
	if n == nil {
		return ""
	}
	start := n.StartByte()
	end := n.EndByte()
	// Defensive bounds checks: tree-sitter normally returns valid
	// [start, end) ranges, but a malformed tree (e.g. from a
	// fuzz-discovered edge case) could yield start > end or
	// start > len(src). Either would panic the bare slice
	// expression below; clamp into a safe empty/truncated form
	// instead.
	if int(start) > len(src) || start > end {
		return ""
	}
	if int(end) > len(src) {
		end = uint32(len(src))
	}
	return string(src[start:end])
}
