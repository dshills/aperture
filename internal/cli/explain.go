package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newExplainCommand() *cobra.Command {
	var manifestPath string
	cmd := &cobra.Command{
		Use:   "explain [TASK_FILE]",
		Short: "Render selection reasoning for a prior manifest or planning run",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return exitErr(1, fmt.Errorf("explain is stubbed in Phase 1; full implementation lands in Phase 4"))
		},
	}
	cmd.Flags().StringVar(&manifestPath, "manifest", "", "Path to a prior manifest JSON (optional)")
	return cmd
}
