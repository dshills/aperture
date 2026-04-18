package bench

import (
	"strings"
	"testing"
	"time"
)

func TestRun_CollectsSamplesAndComputesQuantiles(t *testing.T) {
	count := 0
	res, err := Run("tiny", Config{Iterations: 10}, func() error {
		count++
		time.Sleep(time.Millisecond)
		return nil
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if count != 10 {
		t.Fatalf("expected 10 iterations, got %d", count)
	}
	if len(res.Samples) != 10 {
		t.Fatalf("expected 10 samples, got %d", len(res.Samples))
	}
	// Median ≤ P95 is the only quantile-order invariant we can assert
	// without a controlled time source.
	if res.Median > res.P95 {
		t.Errorf("median %v > p95 %v", res.Median, res.P95)
	}
}

func TestRun_ErrorHaltsFixture(t *testing.T) {
	callCount := 0
	_, err := Run("err", Config{Iterations: 5}, func() error {
		callCount++
		if callCount == 3 {
			return errTestHalt
		}
		return nil
	})
	if err == nil {
		t.Fatal("expected error from halt")
	}
	if callCount != 3 {
		t.Fatalf("expected 3 calls (halt on third), got %d", callCount)
	}
}

// Reports emit both a human-friendly header and a row per fixture so CI
// diffs can pick up regressions at a glance.
func TestReport_FormatCarriesAllFixtures(t *testing.T) {
	r := []Result{
		{Name: "tiny", ColdMs: 5, WarmMs: 1, Median: time.Millisecond, P95: 2 * time.Millisecond},
		{Name: "bigger", ColdMs: 50, WarmMs: 10, Median: 12 * time.Millisecond, P95: 30 * time.Millisecond},
	}
	var buf strings.Builder
	Report(&buf, r)
	out := buf.String()
	for _, s := range []string{"tiny", "bigger", "cold_ms", "p95"} {
		if !strings.Contains(out, s) {
			t.Errorf("report missing %q:\n%s", s, out)
		}
	}
}

type testHaltErr struct{}

func (testHaltErr) Error() string { return "halt" }

var errTestHalt = testHaltErr{}
