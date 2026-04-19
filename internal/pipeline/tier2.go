package pipeline

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"sync"
	"sync/atomic"

	"github.com/dshills/aperture/internal/cache"
	"github.com/dshills/aperture/internal/index"
	"github.com/dshills/aperture/internal/lang/tstree"
)

// tier2Target names a file plus its resolved tstree grammar Lang —
// the package-level type lets tier2Target values flow between
// helpers without a nominal-type mismatch against anonymous structs.
type tier2Target struct {
	Path string
	Lang tstree.Lang
}

// runTier2Analysis parses TS/TSX/JS/JSX/MJS/CJS/Python files via
// tree-sitter (or the `notier2` stub) and merges the results into
// idx. Cache integration mirrors the Go path: the key is the same
// (path+size+mtime+selection_logic_version) and each entry carries
// a Language discriminator (§7.3.4). A language whose
// `languages.<name>.enabled` flag is false has its files silently
// skipped — they'll land at tier3_lexical via LanguageTier stamping.
//
// Returns the cache statistics so Build can merge them into the
// overall CacheStats total.
func runTier2Analysis(opts BuildOptions, idx *index.Index, fileByPath map[string]*index.FileEntry) CacheStats {
	// Partition target files by (language, lang-enum), skipping
	// anything whose language is disabled in config.
	targets := make([]tier2Target, 0)
	for _, f := range idx.Files {
		lang := tstree.LanguageForExtension(f.Extension)
		if lang == 0 {
			continue
		}
		switch f.Language {
		case "typescript":
			if !opts.TypeScriptEnabled {
				continue
			}
		case "javascript":
			if !opts.JavaScriptEnabled {
				continue
			}
		case "python":
			if !opts.PythonEnabled {
				continue
			}
		default:
			continue
		}
		targets = append(targets, tier2Target{Path: f.Path, Lang: lang})
	}
	if len(targets) == 0 {
		return CacheStats{}
	}
	sort.Slice(targets, func(i, j int) bool { return targets[i].Path < targets[j].Path })

	// Cache lookup (serial — tier-2 fixture sets are small enough
	// that the complexity of parallel cache I/O isn't worth it).
	var stats CacheStats
	hits := map[string]tstree.Result{}
	misses := make([]tier2Target, 0, len(targets))
	if opts.Cache != nil {
		for _, t := range targets {
			f := fileByPath[t.Path]
			if f == nil {
				misses = append(misses, t)
				continue
			}
			key := cache.Key(t.Path, f.Size, f.MTime, opts.Cache.SelectionLogicVersion)
			entry, err := opts.Cache.Get(key)
			if err != nil || entry == nil || entry.Language != f.Language {
				misses = append(misses, t)
				stats.Misses++
				continue
			}
			hits[t.Path] = tstree.Result{
				Path:       entry.Path,
				Symbols:    entry.Symbols,
				Imports:    entry.Imports,
				ParseError: entry.ParseError,
			}
			stats.Hits++
		}
	} else {
		misses = targets
	}

	// Parse misses in a bounded worker pool.
	fresh := parseTier2Misses(opts.Root, misses)

	// Write misses back to cache concurrently (per-file independent
	// fsync). Failures are slog.Warn'd but never abort the plan.
	if opts.Cache != nil {
		written := writeTier2Cache(opts.Cache, fresh, fileByPath)
		stats.Writes += written
	}

	// Merge into idx.Files.
	byPath := make(map[string]tstree.Result, len(hits)+len(fresh))
	for p, r := range hits {
		byPath[p] = r
	}
	for _, r := range fresh {
		byPath[r.Path] = r
	}
	for i := range idx.Files {
		r, ok := byPath[idx.Files[i].Path]
		if !ok {
			continue
		}
		idx.Files[i].Symbols = append(idx.Files[i].Symbols, r.Symbols...)
		if len(idx.Files[i].Imports) == 0 {
			idx.Files[i].Imports = r.Imports
		} else {
			// Merge; tier-2 files generally have no pre-existing
			// Imports from the Go path, but being defensive here
			// keeps the merge safe against future analyzer overlap.
			idx.Files[i].Imports = append(idx.Files[i].Imports, r.Imports...)
		}
		idx.Files[i].ParseError = r.ParseError || idx.Files[i].ParseError
	}
	return stats
}

// parseTier2Misses invokes tstree.Parse on each miss in parallel
// across a bounded worker pool. Returns the fresh results in
// deterministic (path-sorted) order for downstream merging.
func parseTier2Misses(root string, misses []tier2Target) []tstree.Result {
	if len(misses) == 0 {
		return nil
	}
	workers := runtime.NumCPU()
	if workers < 1 {
		workers = 1
	}
	if workers > len(misses) {
		workers = len(misses)
	}

	jobs := make(chan tier2Target, 64)
	go func() {
		defer close(jobs)
		for _, m := range misses {
			jobs <- m
		}
	}()

	outBuf := 4 * workers
	if outBuf < 32 {
		outBuf = 32
	}
	out := make(chan tstree.Result, outBuf)
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range jobs {
				abs := filepath.Join(root, filepath.FromSlash(j.Path))
				src, err := os.ReadFile(abs) //nolint:gosec // walker-verified path
				if err != nil {
					out <- tstree.Result{Path: j.Path, ParseError: true}
					continue
				}
				r := tstree.Parse(context.Background(), j.Path, j.Lang, src)
				out <- *r
			}
		}()
	}
	go func() { wg.Wait(); close(out) }()

	results := make([]tstree.Result, 0, len(misses))
	for r := range out {
		results = append(results, r)
	}
	sort.Slice(results, func(i, j int) bool { return results[i].Path < results[j].Path })
	return results
}

// writeTier2Cache writes fresh tier-2 results to the cache in
// parallel. The entry's Language field is populated from the file's
// walker-tagged language so a v1.0 binary reading a v1.1 cache sees
// a discriminator and skips the entry (§7.3.4).
func writeTier2Cache(c *cache.Cache, fresh []tstree.Result, fileByPath map[string]*index.FileEntry) int {
	workers := runtime.NumCPU()
	if workers < 1 {
		workers = 1
	}
	if workers > len(fresh) {
		workers = len(fresh)
	}
	if workers == 0 {
		return 0
	}
	jobs := make(chan tstree.Result, 64)
	go func() {
		defer close(jobs)
		for _, r := range fresh {
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
					Path:       r.Path,
					Size:       entry.Size,
					MTime:      entry.MTime,
					SHA256:     entry.SHA256,
					Imports:    r.Imports,
					Symbols:    r.Symbols,
					ParseError: r.ParseError,
					Language:   entry.Language,
				}
				if err := c.Put(key, ce); err != nil {
					slog.Warn("tier-2 cache write failed", "path", r.Path, "error", err.Error())
					continue
				}
				count.Add(1)
			}
		}()
	}
	wg.Wait()
	return int(count.Load())
}
