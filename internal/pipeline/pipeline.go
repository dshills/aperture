// Package pipeline composes the Phase-2 repo scanner, Go AST analyzer,
// and index-assembly steps into a single entry point the CLI plan command
// can call. Phase 6 adds an optional AST cache that lets warm-plan
// invocations skip the parse step entirely.
package pipeline

import (
	"fmt"
	"log/slog"
	"runtime"
	"sort"
	"sync"
	"sync/atomic"

	"github.com/dshills/aperture/internal/cache"
	"github.com/dshills/aperture/internal/index"
	"github.com/dshills/aperture/internal/lang/goanalysis"
	"github.com/dshills/aperture/internal/repo"
)

// BuildOptions controls Build.
type BuildOptions struct {
	Root            string
	DefaultExcludes []string
	UserExcludes    []string

	// Cache, when non-nil, is consulted before invoking goanalysis for
	// each Go file. Hits skip the AST parse and reuse the prior result;
	// misses parse normally and write back to the cache.
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

// Build walks the repo, parses Go files via go/parser concurrently, and
// assembles the deterministic Index. When opts.Cache is non-nil, each
// file's cached AST analysis is reused instead of re-parsing.
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

	// Build a path → *FileEntry lookup once so the cache lookup + write-
	// back loops are O(1) per file instead of O(total-files). The walker
	// already returns Files in a sorted, de-duplicated form so one map
	// pass is enough.
	fileByPath := make(map[string]*index.FileEntry, len(idx.Files))
	for i := range idx.Files {
		fileByPath[idx.Files[i].Path] = &idx.Files[i]
	}

	goPaths := make([]string, 0, len(idx.Files))
	for _, f := range idx.Files {
		if f.Language == "go" {
			goPaths = append(goPaths, f.Path)
		}
	}

	var stats CacheStats
	toAnalyze := goPaths
	cachedResults := map[string]goanalysis.FileResult{}
	if opts.Cache != nil {
		cachedResults, toAnalyze, stats = lookupCachedFiles(opts.Cache, goPaths, fileByPath)
	}

	freshResults, err := goanalysis.Analyze(goanalysis.AnalyzeOptions{Root: opts.Root, Paths: toAnalyze})
	if err != nil {
		return Result{}, fmt.Errorf("analyze: %w", err)
	}

	// Write miss results back to the cache concurrently. The miss list
	// typically runs hundreds or thousands of entries on a cold repo;
	// serializing them behind fsync latency would negate the benefit of
	// the concurrent Analyze phase that produced them. Writes are
	// independent per-file so a bounded worker pool scales cleanly.
	if opts.Cache != nil {
		written := writeCachedResults(opts.Cache, freshResults, fileByPath)
		stats.Writes += written
	}

	// Merge cached + fresh results by path.
	byPath := make(map[string]goanalysis.FileResult, len(cachedResults)+len(freshResults))
	for p, r := range cachedResults {
		byPath[p] = r
	}
	for _, r := range freshResults {
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

	// v1.1 tier-2: analyze TS/JS/Python files via tree-sitter. Runs
	// in parallel with the Go-analyze results already merged into
	// idx.Files. §7.3.4 keys the cache the same way as Go, with a
	// Language discriminator on the entry.
	tier2Stats := runTier2Analysis(opts, idx, fileByPath)
	stats.Hits += tier2Stats.Hits
	stats.Misses += tier2Stats.Misses
	stats.Writes += tier2Stats.Writes

	// v1.1 §5.4: stamp every FileEntry with its LanguageTier.
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

// writeCachedResults writes miss results back to the cache in parallel
// across a bounded worker pool. Each file's Put is independent, so
// serialization behind fsync latency would have been pure overhead;
// parallelizing keeps the cold-plan hot path I/O-bound on the walker,
// not the cache. Returns the number of successful writes for stats
// reporting. Failures are slog.Warn'd but never abort the plan —
// warming the cache on the next run is a first-class fallback.
func writeCachedResults(
	c *cache.Cache,
	freshResults []goanalysis.FileResult,
	fileByPath map[string]*index.FileEntry,
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
	jobs := make(chan goanalysis.FileResult, 256)
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
				key := cache.Key(r.Path, entry.Size, entry.MTime, c.SelectionLogicVersion)
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
) (map[string]goanalysis.FileResult, []string, CacheStats) {
	type resultRow struct {
		path string
		res  goanalysis.FileResult
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
				key := cache.Key(p, entry.Size, entry.MTime, c.SelectionLogicVersion)
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
					res: goanalysis.FileResult{
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

	hits := make(map[string]goanalysis.FileResult, len(goPaths))
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
	// goanalysis.Analyze re-sorts internally but restoring order here
	// keeps any observable side-effects of miss-list iteration stable
	// across runs.
	sort.Strings(misses)
	return hits, misses, stats
}
