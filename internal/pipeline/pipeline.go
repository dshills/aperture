// Package pipeline composes the Phase-2 repo scanner, Go AST analyzer,
// and index-assembly steps into a single entry point the CLI plan command
// can call. Phase 6 adds an optional AST cache that lets warm-plan
// invocations skip the parse step entirely.
package pipeline

import (
	"context"
	"fmt"
	"log/slog"
	"runtime"
	"sort"
	"sync"
	"sync/atomic"

	"github.com/dshills/aperture/internal/cache"
	"github.com/dshills/aperture/internal/index"
	"github.com/dshills/aperture/internal/lang"
	"github.com/dshills/aperture/internal/lang/goanalysis"
	"github.com/dshills/aperture/internal/lang/javascript"
	"github.com/dshills/aperture/internal/lang/python"
	"github.com/dshills/aperture/internal/lang/typescript"
	"github.com/dshills/aperture/internal/repo"
)

// BuildOptions controls Build.
type BuildOptions struct {
	Root            string
	DefaultExcludes []string
	UserExcludes    []string

	// Cache, when non-nil, is consulted before invoking the Go analyzer
	// for each Go file. Hits skip the AST parse and reuse the prior
	// result; misses parse normally and write back to the cache.
	Cache *cache.Cache

	// TypeScriptEnabled / JavaScriptEnabled / PythonEnabled mirror the
	// v1.1 §9 `languages.<name>.enabled` config flags. When false, the
	// language's files skip tier-2 analysis and fall back to tier-3
	// lexical (filename / doc tokens only). Default (zero-value) is
	// false — callers MUST explicitly opt in per their resolved config.
	TypeScriptEnabled bool
	JavaScriptEnabled bool
	PythonEnabled     bool
}

// Result is the full Phase-2 output: the assembled index and the walker's
// exclusion log (already sorted). CacheStats reports hit/miss counts so
// callers (CLI --verbose) can expose cache health at a glance.
type Result struct {
	Index      *index.Index
	Exclusions []repo.Exclusion
	CacheStats CacheStats
}

// CacheStats is a bookkeeping struct for the cache integration.
type CacheStats struct {
	Hits   int
	Misses int
	Writes int
}

// Build walks the repo, parses Go files via the Go analyzer (go/parser)
// concurrently, and assembles the deterministic Index. When opts.Cache is
// non-nil, each file's cached analysis is reused instead of re-parsing.
//
// ctx is propagated to every language analyzer. Cancellation short-
// circuits per-file analysis at the next ctx check; in-flight parses
// already running to completion are not interrupted (go/parser is not
// natively cancelable), but no new file will be picked up after cancel.
func Build(ctx context.Context, opts BuildOptions) (Result, error) {
	wr, err := repo.Walk(repo.WalkOptions{
		Root:            opts.Root,
		DefaultPatterns: opts.DefaultExcludes,
		UserPatterns:    opts.UserExcludes,
	})
	if err != nil {
		return Result{}, fmt.Errorf("walk: %w", err)
	}

	idx := index.FromWalk(wr)

	// Build a path → *FileEntry lookup once so the cache lookup + write-
	// back loops are O(1) per file instead of O(total-files). The walker
	// already returns Files in a sorted, de-duplicated form so one map
	// pass is enough.
	fileByPath := make(map[string]*index.FileEntry, len(idx.Files))
	for i := range idx.Files {
		fileByPath[idx.Files[i].Path] = &idx.Files[i]
	}

	// Assemble the analyzer registry. Go is always present (tier-1);
	// tier-2 analyzers opt in per the v1.1 §9 `languages.<name>.enabled`
	// config flags. Disabled languages skip analysis and land at
	// tier3_lexical via the LanguageTier stamping below. Order is
	// deterministic by construction (ascending Name) so cache hit/miss
	// telemetry and any downstream logs are stable across runs.
	analyzers := []lang.Analyzer{goanalysis.NewAnalyzer()}
	if opts.JavaScriptEnabled {
		analyzers = append(analyzers, javascript.NewAnalyzer())
	}
	if opts.PythonEnabled {
		analyzers = append(analyzers, python.NewAnalyzer())
	}
	if opts.TypeScriptEnabled {
		analyzers = append(analyzers, typescript.NewAnalyzer())
	}

	// One unified pass per analyzer: route files by walker Language,
	// consult cache, parse misses, write back, merge into Index. Cache
	// entries are namespaced by SelectionLogicVersion + AnalyzerVersion
	// via cacheVersionFor so a bump of either invalidates only its own
	// slice of the cache.
	var stats CacheStats
	for _, a := range analyzers {
		paths := filesForAnalyzer(idx, a)
		if len(paths) == 0 {
			continue
		}
		cacheVersion := cacheVersionFor(opts.Cache, a)

		toAnalyze := paths
		cachedResults := map[string]lang.FileResult{}
		if opts.Cache != nil {
			var s CacheStats
			cachedResults, toAnalyze, s = lookupCachedFiles(opts.Cache, paths, fileByPath, cacheVersion)
			stats.Hits += s.Hits
			stats.Misses += s.Misses
		}

		freshResults, err := a.Analyze(ctx, opts.Root, toAnalyze)
		if err != nil {
			return Result{}, fmt.Errorf("analyze %s: %w", a.Name(), err)
		}

		if opts.Cache != nil {
			stats.Writes += writeCachedResults(opts.Cache, freshResults, fileByPath, cacheVersion)
		}

		// Merge cached + fresh results into the Index.
		//
		// SET (not APPEND) is the correct semantic here, by invariant:
		// the walker assigns exactly one Language tag per file, and
		// filesForAnalyzer(idx, a) routes each file to exactly one
		// analyzer whose Name() matches that tag. Analyzer domains do
		// not overlap, so a given path is seen by at most one analyzer
		// per Build — there is no prior value on f.Symbols/f.Imports
		// to preserve via APPEND. If two analyzers ever claimed the
		// same Name(), registry-assembly above would be the bug; the
		// merge would correctly overwrite and the second analyzer's
		// results would win, not silently concatenate and produce
		// duplicate Symbols. Fields the analyzer doesn't populate
		// (PackageName / SideEffects on tier-2) stay zero-valued.
		//
		// r is a lang.FileResult VALUE (see the Analyze interface:
		// `Analyze(...) ([]FileResult, error)` — slice of struct
		// values, not pointers). There is no nil r to guard against.
		//
		// Iterate results rather than idx.Files: with L analyzers
		// the old shape was O(L·N). Now O(analyzer's file count).
		mergeResult := func(r lang.FileResult) {
			f, ok := fileByPath[r.Path]
			if !ok {
				return
			}
			f.PackageName = r.PackageName
			f.Imports = r.Imports
			f.Symbols = r.Symbols
			f.SideEffects = r.SideEffects
			f.ParseError = r.ParseError
		}
		for _, r := range cachedResults {
			mergeResult(r)
		}
		for _, r := range freshResults {
			mergeResult(r)
		}
	}

	// v1.1 §5.4: stamp every FileEntry with its LanguageTier.
	// ResolveTierForLanguage owns the conditional logic — Go returns
	// Tier1Deep unconditionally, TS/JS/Python return Tier2Structural
	// ONLY when their respective *Enabled flag is true (otherwise
	// Tier3Lexical), and every other language returns Tier3Lexical.
	// The loop is therefore correct for disabled languages and for
	// languages without any analyzer; it is NOT unconditionally
	// stamping Tier2Structural.
	for i := range idx.Files {
		idx.Files[i].LanguageTier = string(index.ResolveTierForLanguage(
			idx.Files[i].Language,
			opts.TypeScriptEnabled,
			opts.JavaScriptEnabled,
			opts.PythonEnabled,
		))
	}

	index.BuildPackages(idx)
	index.LinkTests(idx)
	index.DetectSupplemental(idx)

	return Result{Index: idx, Exclusions: wr.Exclusions, CacheStats: stats}, nil
}

// cacheVersionFor composes the SelectionLogicVersion and the analyzer's
// AnalyzerVersion into the single string cache.Key hashes. Bumping
// either invalidates only its own slice of the cache. Returns "" when
// no cache is configured so call sites don't need a nil guard.
func cacheVersionFor(c *cache.Cache, a lang.Analyzer) string {
	if c == nil {
		return ""
	}
	return c.SelectionLogicVersion + "|" + a.AnalyzerVersion()
}

// filesForAnalyzer returns the repo-relative paths whose walker-assigned
// Language matches the analyzer's Name, preserving the walker's
// ascending path order. The walker is authoritative on language
// classification (it already applies extension, shebang, and filename
// heuristics); routing by f.Language keeps the pipeline in lock-step
// with that classification instead of re-deriving it from extensions.
func filesForAnalyzer(idx *index.Index, a lang.Analyzer) []string {
	name := a.Name()
	out := make([]string, 0, len(idx.Files))
	for _, f := range idx.Files {
		if f.Language == name {
			out = append(out, f.Path)
		}
	}
	return out
}

// writeCachedResults writes miss results back to the cache in parallel
// across a bounded worker pool. Each file's Put is independent, so
// serialization behind fsync latency would have been pure overhead;
// parallelizing keeps the cold-plan hot path I/O-bound on the walker,
// not the cache. Returns the number of successful writes for stats
// reporting. Failures are slog.Warn'd but never abort the plan —
// warming the cache on the next run is a first-class fallback.
func writeCachedResults(
	c *cache.Cache,
	freshResults []lang.FileResult,
	fileByPath map[string]*index.FileEntry,
	cacheVersion string,
) int {
	workers := runtime.NumCPU()
	if workers < 1 {
		workers = 1
	}
	if workers > len(freshResults) {
		workers = len(freshResults)
	}
	if workers == 0 {
		return 0
	}

	// Bounded jobs buffer (256 entries) + feeder goroutine instead of
	// pre-filling the channel. For 50 000-file repos the older
	// pre-fill path allocated a 50 000-entry chan on the heap even
	// though workers drain it in pipeline order; this shape caps the
	// allocation regardless of fixture size.
	jobs := make(chan lang.FileResult, 256)
	go func() {
		defer close(jobs)
		for _, r := range freshResults {
			jobs <- r
		}
	}()

	var (
		wg    sync.WaitGroup
		count atomic.Int64
	)
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for r := range jobs {
				entry := fileByPath[r.Path]
				if entry == nil {
					continue
				}
				key := cache.Key(r.Path, entry.Size, entry.MTime, cacheVersion)
				ce := &cache.Entry{
					Path:        r.Path,
					Size:        entry.Size,
					MTime:       entry.MTime,
					SHA256:      entry.SHA256,
					PackageName: r.PackageName,
					Imports:     r.Imports,
					Symbols:     r.Symbols,
					SideEffects: r.SideEffects,
					ParseError:  r.ParseError,
				}
				if err := c.Put(key, ce); err != nil {
					slog.Warn("cache write failed", "path", r.Path, "error", err.Error())
					continue
				}
				count.Add(1)
			}
		}()
	}
	wg.Wait()
	return int(count.Load())
}

// lookupCachedFiles runs per-file cache Gets concurrently across a
// bounded worker pool and returns the hit map, the miss list, and
// aggregated stats. Sequential lookups blocked every other goroutine
// on disk latency — for 5 000 Go files that's ~1 second of pure Stat +
// ReadFile overhead. Parallelizing recovers it.
func lookupCachedFiles(
	c *cache.Cache,
	goPaths []string,
	fileByPath map[string]*index.FileEntry,
	cacheVersion string,
) (map[string]lang.FileResult, []string, CacheStats) {
	type resultRow struct {
		path string
		res  lang.FileResult
		hit  bool
	}

	workers := runtime.NumCPU()
	if workers < 1 {
		workers = 1
	}
	if workers > len(goPaths) {
		workers = len(goPaths)
	}

	// Bounded jobs buffer + feeder goroutine. Same motivation as
	// writeCachedResults: cap the channel allocation so 50 000-file
	// repos don't allocate proportional scratch space just to ferry
	// paths to workers.
	jobs := make(chan string, 256)
	go func() {
		defer close(jobs)
		for _, p := range goPaths {
			jobs <- p
		}
	}()

	// Bounded results buffer so a 50 000-file repo doesn't force a
	// 50 000-entry channel allocation. Workers block briefly when the
	// drain goroutine below can't keep up, which is fine — the
	// bottleneck is disk I/O, not channel throughput.
	outBuf := 4 * workers
	if outBuf < 32 {
		outBuf = 32
	}
	out := make(chan resultRow, outBuf)
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for p := range jobs {
				entry := fileByPath[p]
				if entry == nil {
					out <- resultRow{path: p, hit: false}
					continue
				}
				key := cache.Key(p, entry.Size, entry.MTime, cacheVersion)
				ce, err := c.Get(key)
				if err != nil || ce == nil {
					out <- resultRow{path: p, hit: false}
					continue
				}
				// No metadata re-check needed — cache.Key already
				// folds path, size, mtime, and tool_version into the
				// sha256 digest, so a successful Get implies an exact
				// metadata match (barring a sha256 collision).
				out <- resultRow{
					path: p,
					hit:  true,
					res: lang.FileResult{
						Path:        ce.Path,
						PackageName: ce.PackageName,
						Imports:     ce.Imports,
						Symbols:     ce.Symbols,
						SideEffects: ce.SideEffects,
						ParseError:  ce.ParseError,
					},
				}
			}
		}()
	}
	go func() {
		wg.Wait()
		close(out)
	}()

	hits := make(map[string]lang.FileResult, len(goPaths))
	misses := make([]string, 0, len(goPaths))
	var stats CacheStats
	for row := range out {
		if row.hit {
			hits[row.path] = row.res
			stats.Hits++
		} else {
			misses = append(misses, row.path)
			stats.Misses++
		}
	}
	// Preserve deterministic miss ordering (the walker handed us sorted
	// paths; concurrent drain breaks it). sort.Strings is O(N log N);
	// the analyzer re-sorts internally but restoring order here keeps
	// any observable side-effects of miss-list iteration stable across
	// runs.
	sort.Strings(misses)
	return hits, misses, stats
}
