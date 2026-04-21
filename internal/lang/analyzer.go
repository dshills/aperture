// Package lang defines the analyzer contract every language implementation
// satisfies. The pipeline iterates analyzers; it does not special-case
// grammars or ASTs.
//
// v1.1 ships with two implementations: internal/lang/goanalysis (tier-1
// deep analysis via go/parser + go/ast) and internal/lang/tstree (tier-2
// structural analysis via tree-sitter). Future languages land as new
// packages that satisfy Analyzer without touching callers.
package lang

import (
	"context"

	"github.com/dshills/aperture/internal/index"
)

// Tier matches SPEC §5.4. An analyzer declares its tier so feasibility
// scoring and FileEntry.LanguageTier stamping stay honest without
// pipeline-side heuristics.
type Tier int

const (
	// TierDeep produces resolved imports and full symbol information from
	// a real type-aware AST (currently only Go via go/parser + go/types).
	TierDeep Tier = 1
	// TierStructural produces module-level symbols and imports from a
	// concrete syntax tree (tree-sitter). No cross-file type resolution.
	TierStructural Tier = 2
)

// FileResult is the unified per-file output every analyzer returns.
// Whether produced by go/parser + go/ast or by a tree-sitter grammar, the
// downstream index, relevance, and summary stages read the same shape.
//
// Analyzers that do not produce a given field (e.g. tree-sitter grammars
// do not resolve a Go package name or tag §12.2 side-effects) leave it
// zero-valued.
type FileResult struct {
	Path        string
	PackageName string
	Imports     []string
	Symbols     []index.Symbol
	SideEffects []string
	ParseError  bool
}

// Analyzer is the single seam between the pipeline and a language
// implementation. One analyzer per language (or per grammar family —
// TS/TSX/JS/JSX/MJS/CJS may share one impl).
type Analyzer interface {
	// Name is the stable identifier used in manifests, logs, and cache
	// discriminators ("go", "typescript", ...). Must match the walker's
	// Language tag so FileEntry.Language routes files here.
	Name() string

	// Tier reports what this analyzer produces. The pipeline uses it to
	// stamp FileEntry.LanguageTier and to weight cross-language rationale.
	Tier() Tier

	// Extensions lists file extensions (including the leading dot,
	// lowercased) this analyzer can parse. Informational metadata —
	// the pipeline routes files by FileEntry.Language (set by the
	// walker) matched against Name(), not by extension. Extensions is
	// exposed for diagnostics (e.g. surfacing "no analyzer registered
	// for .rs") and for tests that want to probe analyzer claims
	// without running the pipeline.
	Extensions() []string

	// AnalyzerVersion composes into the cache key alongside
	// SelectionLogicVersion so an analyzer bump invalidates only its own
	// cache entries. Bump when extraction rules change in a way that
	// alters emitted Symbols/Imports/SideEffects.
	AnalyzerVersion() string

	// Analyze parses the given repo-relative paths. Implementations own
	// their internal concurrency (Go uses go/parser workers; tree-sitter
	// uses per-grammar parser pools) but MUST return results sorted
	// ascending by Path so downstream stages stay deterministic
	// regardless of goroutine scheduling. ctx cancellation must be
	// honored by long-running parses.
	Analyze(ctx context.Context, root string, paths []string) ([]FileResult, error)
}
