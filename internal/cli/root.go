// Package cli wires the Aperture subcommand surface on top of cobra.
package cli

import (
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"
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
