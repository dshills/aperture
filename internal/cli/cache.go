package cli

import (
	"fmt"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/dshills/aperture/internal/cache"
)

type cacheClearFlags struct {
	repo  string
	purge bool
}

// newCacheCommand returns the `aperture cache` parent group.
// v1 only ships the `clear` subcommand per §15.1.
func newCacheCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cache",
		Short: "Manage the persistent .aperture/ working directory",
	}
	cmd.AddCommand(newCacheClearCommand())
	return cmd
}

func newCacheClearCommand() *cobra.Command {
	var f cacheClearFlags
	cmd := &cobra.Command{
		Use:   "clear",
		Short: "Remove derived cache/index/summaries under .aperture/",
		Long: `Clears the mechanically re-derivable subdirectories of .aperture/
(cache/, index/, summaries/). The audit trail — .aperture/manifests/
and .aperture/logs/ — is preserved by default so prior runs remain
reviewable. Pass --purge to extend the removal to those directories as
well (destructive: prior manifests and log files are deleted).

Missing directories are not an error; the command exits 0 as long as
it could inspect the repo's .aperture/ root.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCacheClear(cmd, f)
		},
	}
	cmd.Flags().StringVar(&f.repo, "repo", "", "Repository root (defaults to cwd)")
	cmd.Flags().BoolVar(&f.purge, "purge", false, "Also remove manifests/ and logs/ (destructive)")
	return cmd
}

func runCacheClear(cmd *cobra.Command, f cacheClearFlags) error {
	repoRoot, err := resolveRepoRoot(f.repo)
	if err != nil {
		return exitErr(exitCodeBadRepo, err)
	}
	dir := cache.ApertureDir{Root: filepath.Join(repoRoot, ".aperture")}
	removed, err := dir.ClearApertureDerived(f.purge)
	if err != nil {
		// §15.1: exit 6 "only if the command could not open the target
		// dir at all". A per-path Warn during the sweep doesn't abort.
		return exitErr(exitCodeBadManifest, err)
	}
	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "aperture cache clear: removed %d subdirectories under %s\n", removed, dir.Root)
	return nil
}
