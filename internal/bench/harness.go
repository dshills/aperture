// Package bench drives the §8.2 performance harness: per-fixture,
// 10 consecutive plan invocations, reporting both p95 and median
// wall-clock timings. The bench binary is invoked by `make bench` and
// never by production code.
package bench

import (
	"fmt"
	"io"
	"math"
	"sort"
	"time"
)

// RunFunc is the function signature each benchmark iteration calls.
// Returning an error aborts the run for that fixture.
type RunFunc func() error

// Result is one fixture's benchmark summary after 10 runs.
type Result struct {
	Name    string
	Samples []time.Duration
	Median  time.Duration
	P95     time.Duration
	ColdMs  int64 // first iteration, usually cold-cache
	WarmMs  int64 // median of remaining samples, typically warm-cache
}

// Config controls a benchmark run.
type Config struct {
	// Iterations is the number of consecutive invocations per fixture.
	// §8.2 fixes this at 10; the field exists so tests can override.
	Iterations int
}

// DefaultConfig returns the §8.2 defaults.
func DefaultConfig() Config { return Config{Iterations: 10} }

// Run executes fn Iterations times, measures wall-clock, and returns
// the computed Result. Each run is allowed to fail: a single failure
// aborts the fixture and surfaces the error to the caller so the bench
// driver can report it without polluting subsequent measurements.
func Run(name string, cfg Config, fn RunFunc) (Result, error) {
	if cfg.Iterations <= 0 {
		cfg.Iterations = 10
	}
	samples := make([]time.Duration, 0, cfg.Iterations)
	for i := 0; i < cfg.Iterations; i++ {
		start := time.Now()
		if err := fn(); err != nil {
			return Result{Name: name, Samples: samples}, fmt.Errorf("iteration %d: %w", i, err)
		}
		samples = append(samples, time.Since(start))
	}

	res := Result{Name: name, Samples: samples}
	res.Median = percentile(samples, 0.50)
	res.P95 = percentile(samples, 0.95)
	if len(samples) > 0 {
		res.ColdMs = samples[0].Milliseconds()
	}
	if len(samples) > 1 {
		res.WarmMs = percentile(samples[1:], 0.50).Milliseconds()
	}
	return res, nil
}

// Report writes a human-friendly line per fixture plus a machine-
// parseable table. Both shapes are stable so CI comparators can diff
// across commits.
func Report(w io.Writer, results []Result) {
	_, _ = fmt.Fprintln(w, "fixture                    cold_ms   warm_ms    median     p95")
	_, _ = fmt.Fprintln(w, "-------                    -------   -------    ------    ----")
	for _, r := range results {
		_, _ = fmt.Fprintf(w, "%-24s  %7d   %7d   %7d   %7d\n",
			r.Name,
			r.ColdMs,
			r.WarmMs,
			r.Median.Milliseconds(),
			r.P95.Milliseconds())
	}
}

// percentile returns the duration at the p-th percentile of samples.
// Uses the standard nearest-rank formula rank = ceil(p · N), which for
// n=10 puts p95 at the 10th sample (largest) and p50 at the 5th. This
// matches the observability-stack convention callers expect — the
// earlier int() truncation put p95 at the 9th sample, understating
// tail latency.
func percentile(samples []time.Duration, p float64) time.Duration {
	if len(samples) == 0 {
		return 0
	}
	sorted := make([]time.Duration, len(samples))
	copy(sorted, samples)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	rank := int(math.Ceil(p * float64(len(sorted))))
	if rank < 1 {
		rank = 1
	}
	if rank > len(sorted) {
		rank = len(sorted)
	}
	return sorted[rank-1]
}
