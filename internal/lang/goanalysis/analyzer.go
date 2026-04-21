package goanalysis

import (
	"context"

	"github.com/dshills/aperture/internal/lang"
)

// analyzerVersion bumps whenever the extraction rules in parse.go /
// symbols.go / sideeffects.go change in a way that alters emitted
// Symbols / Imports / SideEffects. Folded into cache keys so a bump
// invalidates only Go entries, not tier-2 ones.
const analyzerVersion = "go-v1"

// NewAnalyzer returns a lang.Analyzer implementation that wraps the
// package-level Analyze function. The existing Analyze / AnalyzeOptions /
// FileResult API is preserved as the package-internal implementation; the
// adapter converts at the package boundary so the pipeline only speaks in
// lang types.
func NewAnalyzer() lang.Analyzer { return analyzerAdapter{} }

type analyzerAdapter struct{}

func (analyzerAdapter) Name() string            { return "go" }
func (analyzerAdapter) Tier() lang.Tier         { return lang.TierDeep }
func (analyzerAdapter) Extensions() []string    { return []string{".go"} }
func (analyzerAdapter) AnalyzerVersion() string { return analyzerVersion }

// Analyze satisfies lang.Analyzer by delegating to the package-level
// Analyze. The lang.Analyzer contract requires path-ascending result
// order; the underlying Analyze sorts before returning (see
// parse.go: "sort.Slice(results, ...)"), so no re-sort is needed here.
//
// ctx is threaded through to the worker pool: each file's parse is
// preceded by a ctx.Err() check, so cancel short-circuits the remaining
// file set. A file that has already entered go/parser will run to
// completion (go/parser is not natively cancelable).
func (analyzerAdapter) Analyze(ctx context.Context, root string, paths []string) ([]lang.FileResult, error) {
	res, err := Analyze(ctx, AnalyzeOptions{Root: root, Paths: paths})
	if err != nil {
		return nil, err
	}
	out := make([]lang.FileResult, len(res))
	for i, r := range res {
		out[i] = lang.FileResult{
			Path:        r.Path,
			PackageName: r.PackageName,
			Imports:     r.Imports,
			Symbols:     r.Symbols,
			SideEffects: r.SideEffects,
			ParseError:  r.ParseError,
		}
	}
	return out, nil
}
