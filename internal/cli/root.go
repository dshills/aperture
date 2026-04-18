// Package cli wires the Aperture subcommand surface on top of cobra.
package cli

import (
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// Named exit codes per SPEC §16. Use these constants everywhere instead
// of bare integers so the error-code surface stays easy to audit.
const (
	exitCodeInternal           = 1  // unexpected failure
	exitCodeBadArgs            = 2  // invalid command-line arguments
	exitCodeBadTask            = 3  // unreadable task file
	exitCodeBadRepo            = 4  // invalid repository root
	exitCodeBadConfig          = 5  // malformed config
	exitCodeBadManifest        = 6  // manifest serialization / schema failure
	exitCodeFeasibilityBelow   = 7  // --min-feasibility threshold not met
	exitCodeFailOnGaps         = 8  // blocking gap present and --fail-on-gaps set
	exitCodeBudgetUnderflow    = 9  // §7.6.5 underflow
	exitCodeTokenizerMissing   = 10 // recognized-but-unsupported model
	exitCodeUnknownAgent       = 11 // agents.<name> not present
	exitCodeAdapterPreExecFail = 12 // adapter could not start
)

// ExitCodeError carries a structured exit code for the top-level main wrapper.
type ExitCodeError struct {
	Code int
	Err  error
}

func (e *ExitCodeError) Error() string {
	if e.Err == nil {
		return fmt.Sprintf("exit %d", e.Code)
	}
	return e.Err.Error()
}

func exitErr(code int, err error) error {
	return &ExitCodeError{Code: code, Err: err}
}

func usageErr(msg string) error {
	return exitErr(2, fmt.Errorf("%s", msg))
}

// NewRoot assembles the top-level `aperture` command.
func NewRoot() *cobra.Command {
	root := &cobra.Command{
		Use:          "aperture",
		Short:        "Deterministic context planner for coding agents",
		SilenceUsage: true,
	}
	root.AddCommand(
		newPlanCommand(),
		newExplainCommand(),
		newRunCommand(),
		newVersionCommand(),
	)
	return root
}

// Execute runs the root command and returns the exit code the process should
// use. Prints human-readable error text to stderr for non-zero codes.
func Execute() int {
	root := NewRoot()
	if err := root.Execute(); err != nil {
		var ec *ExitCodeError
		if errors.As(err, &ec) {
			if ec.Err != nil {
				fmt.Fprintf(os.Stderr, "aperture: %s\n", ec.Err.Error())
			}
			return ec.Code
		}
		fmt.Fprintf(os.Stderr, "aperture: %s\n", err.Error())
		return 1
	}
	return 0
}
