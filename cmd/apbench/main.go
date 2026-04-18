// apbench is the §8.2 performance harness entry point. It drives
// `aperture plan` over the committed benchmark fixtures and emits a
// stable report so CI regression diffs work.
//
// Usage: apbench [-fixture small] [-iterations 10] [-bin ./bin/aperture]
//
// The binary is invoked by `make bench`. It deliberately lives under
// cmd/ (not tests/) so it can be built without pulling in the test
// stdlib and because its outputs are artifacts, not pass/fail results.
package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/dshills/aperture/internal/bench"
)

// apertureExitBudgetUnderflow mirrors SPEC §16 exit code 9. Duplicated
// here because cmd/ binaries can't import internal/cli (which owns the
// canonical constants); keeping the literal centralized avoids scattered
// magic numbers in the bench driver.
const apertureExitBudgetUnderflow = 9

func main() {
	bin := flag.String("bin", "bin/aperture", "path to aperture binary (built via `make build`)")
	fixtureDir := flag.String("fixtures", "testdata/bench", "root directory containing benchmark fixtures")
	iterations := flag.Int("iterations", 10, "iterations per fixture (§8.2 fixes at 10)")
	fixturesFlag := flag.String("only", "", "comma-separated subset of fixtures to run (default: all)")
	flag.Parse()

	if _, err := os.Stat(*bin); err != nil {
		fmt.Fprintf(os.Stderr, "apbench: aperture binary missing at %q — run `make build` first\n", *bin)
		os.Exit(2)
	}

	fixtures, err := selectFixtures(*fixtureDir, *fixturesFlag)
	if err != nil {
		fmt.Fprintln(os.Stderr, "apbench:", err)
		os.Exit(2)
	}
	if len(fixtures) == 0 {
		// Exit non-zero so CI catches a forgotten `make bench-prepare`
		// rather than silently reporting zero fixtures as a success.
		fmt.Fprintf(os.Stderr, "apbench: no fixtures found under %s — run `make bench-prepare` first\n", *fixtureDir)
		os.Exit(2)
	}

	absBin, err := filepath.Abs(*bin)
	if err != nil {
		fmt.Fprintln(os.Stderr, "apbench:", err)
		os.Exit(1)
	}

	cfg := bench.Config{Iterations: *iterations}
	results := make([]bench.Result, 0, len(fixtures))
	for _, fixture := range fixtures {
		name := filepath.Base(fixture)
		res, runErr := bench.Run(name, cfg, func() error {
			return runAperturePlan(absBin, fixture)
		})
		if runErr != nil {
			fmt.Fprintf(os.Stderr, "apbench: fixture %s aborted: %v\n", name, runErr)
		}
		results = append(results, res)
	}

	bench.Report(os.Stdout, results)
}

// runAperturePlan shells out to the aperture binary with --repo at the
// given fixture, -p forcing an inline task so we don't depend on the
// fixture owning a TASK file. stdout/stderr go to buffers so the
// harness output stays clean; we discard the manifest JSON because the
// harness measures wall-clock, not correctness (tests already cover
// correctness).
func runAperturePlan(bin, fixture string) error {
	const inlineTask = "add refresh handling to internal/oauth/provider.go"
	cmd := exec.Command(bin, "plan", "--repo", fixture, "-p", inlineTask)
	var outBuf, errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf
	if err := cmd.Run(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == apertureExitBudgetUnderflow {
			// Budget underflow is a legitimate outcome on intentionally
			// tight fixtures; the manifest is still emitted and the
			// measurement is meaningful.
			return nil
		}
		return fmt.Errorf("%w: stderr=%q", err, errBuf.String())
	}
	return nil
}

// selectFixtures returns the list of fixture directories under root.
// When filter is empty, every sub-directory counts as a fixture. A
// comma-separated filter restricts the set by basename.
func selectFixtures(root, filter string) ([]string, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read fixture root: %w", err)
	}
	var want map[string]struct{}
	if filter != "" {
		want = map[string]struct{}{}
		for _, f := range strings.Split(filter, ",") {
			want[strings.TrimSpace(f)] = struct{}{}
		}
	}
	var out []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if want != nil {
			if _, ok := want[e.Name()]; !ok {
				continue
			}
		}
		out = append(out, filepath.Join(root, e.Name()))
	}
	return out, nil
}
