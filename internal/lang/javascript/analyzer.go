// Package javascript adapts the tstree grammar layer to the
// lang.Analyzer interface for walker-tagged "javascript" files
// (.js, .mjs, .cjs, .jsx). Parsing is delegated to tstree.ParseBatch;
// this package holds only the thin identity/version metadata the
// pipeline needs to route, cache, and merge results.
//
// Under -tags notier2, tstree.Parse returns ParseError=true for every
// file and affected files fall through to tier-3 lexical scoring
// downstream. No CGO is touched in this package directly.
package javascript

import (
	"context"

	"github.com/dshills/aperture/internal/lang"
	"github.com/dshills/aperture/internal/lang/tstree"
)

// analyzerVersion bumps when JavaScript extraction rules in tstree
// change in a way that alters emitted Symbols / Imports. Folded into
// the cache key so a JS-only bump invalidates only JS entries, not
// TypeScript or Python.
const analyzerVersion = "js-v1"

// NewAnalyzer returns the lang.Analyzer implementation for JavaScript.
func NewAnalyzer() lang.Analyzer { return analyzerAdapter{} }

type analyzerAdapter struct{}

func (analyzerAdapter) Name() string            { return "javascript" }
func (analyzerAdapter) Tier() lang.Tier         { return lang.TierStructural }
func (analyzerAdapter) Extensions() []string    { return []string{".js", ".mjs", ".cjs", ".jsx"} }
func (analyzerAdapter) AnalyzerVersion() string { return analyzerVersion }

// Analyze delegates to tstree.ParseBatch with a JS-only grammar
// closure. All four variants (.js, .mjs, .cjs, .jsx) currently share
// the tree-sitter JavaScript grammar — tstree does not distinguish
// them at the grammar level. Results convert to lang.FileResult;
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
	case ".js", ".mjs", ".cjs", ".jsx":
		return tstree.LangJavaScript
	}
	return 0
}
