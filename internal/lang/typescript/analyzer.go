// Package typescript adapts the tstree grammar layer to the
// lang.Analyzer interface for walker-tagged "typescript" files
// (.ts and .tsx). Parsing is delegated to tstree.ParseBatch; this
// package holds only the thin identity/version metadata the pipeline
// needs to route, cache, and merge results.
//
// Under -tags notier2, tstree.Parse returns ParseError=true for every
// file and affected files fall through to tier-3 lexical scoring
// downstream. No CGO is touched in this package directly.
package typescript

import (
	"context"

	"github.com/dshills/aperture/internal/lang"
	"github.com/dshills/aperture/internal/lang/tstree"
)

// analyzerVersion bumps when TypeScript extraction rules in tstree
// change in a way that alters emitted Symbols / Imports. Folded into
// the cache key so a TS-only bump invalidates only TS entries, not
// Go or Python.
const analyzerVersion = "ts-v1"

// NewAnalyzer returns the lang.Analyzer implementation for TypeScript.
func NewAnalyzer() lang.Analyzer { return analyzerAdapter{} }

type analyzerAdapter struct{}

func (analyzerAdapter) Name() string            { return "typescript" }
func (analyzerAdapter) Tier() lang.Tier         { return lang.TierStructural }
func (analyzerAdapter) Extensions() []string    { return []string{".ts", ".tsx"} }
func (analyzerAdapter) AnalyzerVersion() string { return analyzerVersion }

// Analyze delegates to tstree.ParseBatch with a TS-only grammar
// closure (.tsx routes to the TSX grammar; .ts routes to the base
// TypeScript grammar). Results are converted to lang.FileResult —
// SideEffects and PackageName stay zero since tier-2 structural
// analysis does not produce those fields.
func (analyzerAdapter) Analyze(ctx context.Context, root string, paths []string) ([]lang.FileResult, error) {
	results, err := tstree.ParseBatch(ctx, root, paths, langForExt)
	if err != nil {
		return nil, err
	}
	out := make([]lang.FileResult, len(results))
	for i, r := range results {
		out[i] = lang.FileResult{
			Path:       r.Path,
			Imports:    r.Imports,
			Symbols:    r.Symbols,
			ParseError: r.ParseError,
		}
	}
	return out, nil
}

// langForExt assumes ext is already lowercased — tstree.ParseBatch
// owns that normalization so every LangForExt closure can avoid
// redundant per-file ToLower calls in the hot path.
func langForExt(ext string) tstree.Lang {
	switch ext {
	case ".ts":
		return tstree.LangTypeScript
	case ".tsx":
		return tstree.LangTSX
	}
	return 0
}
