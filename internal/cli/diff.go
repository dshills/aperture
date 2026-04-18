package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/dshills/aperture/internal/diff"
)

// diffFlags controls the `aperture diff` subcommand.
type diffFlags struct {
	format string
	out    string
}

// newDiffCommand wires `aperture diff A.json B.json` per SPEC §4.5 /
// §7.6. The command NEVER invokes the planner, NEVER opens the repo,
// NEVER recomputes manifest_hash. Exit codes: 0 always (informational);
// 1 only on I/O, parse, or unsupported-schema errors.
func newDiffCommand() *cobra.Command {
	var f diffFlags
	cmd := &cobra.Command{
		Use:   "diff <manifest-A> <manifest-B>",
		Short: "Explain what changed between two manifests",
		Long: `aperture diff reads two manifest JSON files and renders a
deterministic section-by-section delta. Both inputs must declare
schema_version >= 1.0. The command is read-only — it does not open
the repository or re-run the planner.`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDiff(cmd.OutOrStdout(), args[0], args[1], f)
		},
	}
	cmd.Flags().StringVar(&f.format, "format", "markdown", "Output format: json or markdown")
	cmd.Flags().StringVar(&f.out, "out", "", "Write diff to this path (default: stdout)")
	return cmd
}

func runDiff(stdout io.Writer, pathA, pathB string, f diffFlags) error {
	// All LoadManifestFile failures — I/O errors, parse errors, and
	// ErrUnsupportedSchema — map to exit 1 per §7.7 for `aperture
	// diff`. Preserving the sentinel in the unwrap chain means
	// callers still differentiate via errors.Is when they need to.
	a, err := diff.LoadManifestFile(pathA)
	if err != nil {
		return exitErr(exitCodeInternal, err)
	}
	b, err := diff.LoadManifestFile(pathB)
	if err != nil {
		return exitErr(exitCodeInternal, err)
	}

	d := diff.Compute(a, b)

	var buf []byte
	switch f.format {
	case "json":
		buf, err = diff.EmitJSON(d)
		if err != nil {
			return exitErr(exitCodeInternal, err)
		}
	case "markdown":
		buf = diff.EmitMarkdown(d)
	default:
		return usageErr(fmt.Sprintf("unknown --format %q", f.format))
	}
	return writeDiffOutput(stdout, f.out, buf)
}

func writeDiffOutput(stdout io.Writer, out string, data []byte) error {
	if out == "" || out == "-" {
		// EmitJSON and EmitMarkdown both produce data ending in a
		// newline — appending another one would yield a trailing
		// blank line that breaks byte-for-byte diff tooling.
		_, err := stdout.Write(data)
		if err != nil {
			return exitErr(exitCodeInternal, err)
		}
		return nil
	}
	if dir := filepath.Dir(out); dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return exitErr(exitCodeInternal, fmt.Errorf("create output dir: %w", err))
		}
	}
	if err := os.WriteFile(out, data, 0o644); err != nil { //nolint:gosec // user-selected path
		return exitErr(exitCodeInternal, err)
	}
	return nil
}
