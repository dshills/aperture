package cache

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
)

// ApertureDir is the canonical per-repo working directory layout
// (§7.11.3). ClearApertureDerived removes the sub-directories whose
// contents are mechanically re-derivable from a fresh plan run; the
// audit trail (manifests/, logs/) survives by default so prior runs
// remain reviewable.
type ApertureDir struct {
	// Root is the absolute path to .aperture/ under the repo.
	Root string
}

// DerivedSubdirs is the list of sub-directories `aperture cache clear`
// wipes by default. Keeps the list authoritative so both the command
// code and any integration test that builds the layout agree.
var DerivedSubdirs = []string{"cache", "index", "summaries"}

// AuditSubdirs enumerate the directories preserved by default but
// removed by --purge. manifests/ is the auditability home for prior
// plan runs; logs/ captures slog output per run.
var AuditSubdirs = []string{"manifests", "logs"}

// ClearApertureDerived removes the derived-analysis sub-directories
// from a.Root. Returns the number of removable targets it actually
// removed. Never aborts on a single failure: permission-locked paths
// are logged at Warn and the sweep continues. A top-level Open failure
// (e.g. .aperture/ itself unreadable) returns an error so the caller
// can map to exit code 6.
func (a ApertureDir) ClearApertureDerived(purge bool) (int, error) {
	if a.Root == "" {
		return 0, errors.New("ApertureDir.Root is empty")
	}
	if _, err := os.Stat(a.Root); err != nil {
		if os.IsNotExist(err) {
			// Nothing to clear. Not an error — §15.1 says "exits 0 even
			// when those subdirectories didn't exist to begin with".
			return 0, nil
		}
		return 0, fmt.Errorf("stat %s: %w", a.Root, err)
	}

	targets := append([]string{}, DerivedSubdirs...)
	if purge {
		targets = append(targets, AuditSubdirs...)
	}

	removed := 0
	for _, sub := range targets {
		p := filepath.Join(a.Root, sub)
		if _, err := os.Stat(p); err != nil {
			if os.IsNotExist(err) {
				continue
			}
			slog.Warn("cache clear: stat failed", "path", p, "error", err.Error())
			continue
		}
		if err := os.RemoveAll(p); err != nil {
			slog.Warn("cache clear: remove failed", "path", p, "error", err.Error())
			continue
		}
		removed++
	}
	return removed, nil
}
