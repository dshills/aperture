package tstree

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
)

// LangForExt selects a tree-sitter grammar for a given file extension.
// Callers pass their own closure so each per-language analyzer can
// scope the grammar set it handles. Returning 0 flags the file as
// unroutable; ParseBatch will emit it with ParseError=true so the
// file still participates in tier-3 lexical scoring, but no symbols
// or imports are produced.
//
// Contract: ext is ALREADY LOWERCASED by ParseBatch before the
// closure is called. Implementations must compare case-sensitively
// (`switch ext { case ".ts": ... }`) and must NOT re-lowercase in
// the hot path. This saves a strings.ToLower per file across every
// tier-2 language on large repos.
type LangForExt func(ext string) Lang

// ParseBatch reads each listed file from disk, dispatches to Parse
// via langForExt, and returns results in Path-ascending order. The
// worker pool is sized to NumCPU, capped at len(paths). Cancellation
// via ctx short-circuits remaining files and is surfaced as an
// error — never as ParseError-flagged results, so an incomplete
// cancelled run cannot be confused with a clean build containing
// syntax errors.
//
// Build-tag-free: calls the public Parse and LanguageForExtension
// symbols, which have stub implementations under -tags notier2.
// Under notier2, Parse returns ParseError=true for every file and
// the file falls through to tier-3 lexical scoring downstream.
func ParseBatch(ctx context.Context, root string, paths []string, langForExt LangForExt) ([]Result, error) {
	if len(paths) == 0 {
		return nil, nil
	}
	workers := runtime.NumCPU()
	if workers < 1 {
		workers = 1
	}
	if workers > len(paths) {
		workers = len(paths)
	}

	jobs := make(chan string, 64)
	go func() {
		defer close(jobs)
		for _, p := range paths {
			// Honor cancel on the feeder side too: if workers
			// have exited via ctx.Err() and the buffer fills,
			// a naked `jobs <- p` would block forever, leaking
			// this goroutine for the life of the process.
			select {
			case jobs <- p:
			case <-ctx.Done():
				return
			}
		}
	}()

	outBuf := 4 * workers
	if outBuf < 32 {
		outBuf = 32
	}
	out := make(chan Result, outBuf)
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for p := range jobs {
				if ctx.Err() != nil {
					// Exit on cancel rather than draining.
					// Surfaces via ctx.Err() at the bottom.
					return
				}
				ext := strings.ToLower(filepath.Ext(p))
				lang := langForExt(ext)
				if lang == 0 {
					out <- Result{Path: p, ParseError: true}
					continue
				}
				abs := filepath.Join(root, filepath.FromSlash(p))
				src, err := os.ReadFile(abs) //nolint:gosec // walker-verified path
				if err != nil {
					out <- Result{Path: p, ParseError: true}
					continue
				}
				// Parse's contract: it ALWAYS returns a non-nil
				// *Result — both the CGo implementation in parse.go
				// and the notier2 stub in stub.go unconditionally
				// construct and return &Result{...} (on parse
				// failure they set ParseError=true rather than
				// returning nil). The deref is safe by invariant;
				// no nil guard is needed.
				r := Parse(ctx, p, lang, src)
				out <- *r
			}
		}()
	}
	go func() { wg.Wait(); close(out) }()

	results := make([]Result, 0, len(paths))
	for r := range out {
		results = append(results, r)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	sort.Slice(results, func(i, j int) bool { return results[i].Path < results[j].Path })
	return results, nil
}
