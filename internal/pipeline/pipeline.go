// Package pipeline composes the Phase-2 repo scanner, Go AST analyzer,
// and index-assembly steps into a single entry point the CLI plan command
// can call.
package pipeline

import (
	"fmt"

	"github.com/dshills/aperture/internal/index"
	"github.com/dshills/aperture/internal/lang/goanalysis"
	"github.com/dshills/aperture/internal/repo"
)

// BuildOptions controls Build.
type BuildOptions struct {
	Root            string
	DefaultExcludes []string
	UserExcludes    []string
}

// Result is the full Phase-2 output: the assembled index and the walker's
// exclusion log (already sorted).
type Result struct {
	Index      *index.Index
	Exclusions []repo.Exclusion
}

// Build walks the repo, parses Go files via go/parser concurrently, and
// assembles the deterministic Index.
func Build(opts BuildOptions) (Result, error) {
	wr, err := repo.Walk(repo.WalkOptions{
		Root:            opts.Root,
		DefaultPatterns: opts.DefaultExcludes,
		UserPatterns:    opts.UserExcludes,
	})
	if err != nil {
		return Result{}, fmt.Errorf("walk: %w", err)
	}

	idx := index.FromWalk(wr)

	goPaths := make([]string, 0, len(idx.Files))
	for _, f := range idx.Files {
		if f.Language == "go" {
			goPaths = append(goPaths, f.Path)
		}
	}
	results, err := goanalysis.Analyze(goanalysis.AnalyzeOptions{Root: opts.Root, Paths: goPaths})
	if err != nil {
		return Result{}, fmt.Errorf("analyze: %w", err)
	}

	byPath := make(map[string]goanalysis.FileResult, len(results))
	for _, r := range results {
		byPath[r.Path] = r
	}
	for i := range idx.Files {
		r, ok := byPath[idx.Files[i].Path]
		if !ok {
			continue
		}
		idx.Files[i].PackageName = r.PackageName
		idx.Files[i].Imports = r.Imports
		idx.Files[i].Symbols = r.Symbols
		idx.Files[i].SideEffects = r.SideEffects
		idx.Files[i].ParseError = r.ParseError
	}

	index.BuildPackages(idx)
	index.LinkTests(idx)
	index.DetectSupplemental(idx)

	return Result{Index: idx, Exclusions: wr.Exclusions}, nil
}
