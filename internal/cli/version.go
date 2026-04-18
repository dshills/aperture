package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/dshills/aperture/internal/version"
)

func newVersionCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the Aperture build version",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			_, err := fmt.Fprintln(cmd.OutOrStdout(), version.Full())
			return err
		},
	}
}
