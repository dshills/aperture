package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newRunCommand() *cobra.Command {
	var (
		repo   string
		model  string
		budget int
		inline string
	)
	cmd := &cobra.Command{
		Use:   "run <agent> [TASK_FILE]",
		Short: "Plan and invoke a downstream coding-agent adapter",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return exitErr(1, fmt.Errorf("run is stubbed in Phase 1; full implementation lands in Phase 5"))
		},
	}
	cmd.Flags().StringVar(&repo, "repo", "", "Repository root (defaults to cwd)")
	cmd.Flags().StringVarP(&inline, "prompt", "p", "", "Inline task text")
	cmd.Flags().StringVar(&model, "model", "", "Target model id")
	cmd.Flags().IntVar(&budget, "budget", 0, "Total token budget override")
	return cmd
}
