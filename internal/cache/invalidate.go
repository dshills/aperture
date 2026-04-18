package cache

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
)

// versionFileName is the dedicated file Aperture writes at the cache
// root to record the schema version. A single os.Stat + ReadFile
// replaces the O(N) directory scan the older implementation needed.
const versionFileName = "VERSION"

// InvalidateAll wipes every entry under c.Dir. Used when the caller
// detects a repo-wide invalidation signal (Aperture version change,
// cache_schema_version drift, or .aperture.yaml digest change). Logged
// at Info so "warm plan feels cold" regressions are easy to diagnose.
func (c *Cache) InvalidateAll(reason string) {
	if c == nil || c.Dir == "" {
		return
	}
	slog.Info("invalidating cache", "dir", c.Dir, "reason", reason)
	if err := c.Clear(); err != nil {
		slog.Warn("cache invalidation failed", "dir", c.Dir, "error", err.Error())
	}
}

// DetectSchemaDrift returns true when the cache root's recorded schema
// version differs from the compiled-in SchemaVersion. The check is a
// single ReadFile of <dir>/VERSION — no directory scan, so it's O(1)
// regardless of cache size. A missing VERSION file triggers drift
// detection iff the cache dir already contains entries (pre-v1 layout);
// an empty directory or missing dir returns false (nothing to drift).
func (c *Cache) DetectSchemaDrift() bool {
	if c == nil || c.Dir == "" {
		return false
	}
	recorded, err := readVersionFile(c.Dir)
	switch {
	case err == nil:
		if recorded != SchemaVersion {
			slog.Info("cache schema drift detected",
				"expected", SchemaVersion, "found", recorded, "dir", c.Dir)
			return true
		}
		return false
	case os.IsNotExist(err):
		// No VERSION file yet. If the directory is populated anyway
		// (e.g. from a pre-v1 build that didn't write one), treat the
		// whole dir as drifted so it gets wiped. The populated check
		// still uses ReadDir but short-circuits after the first hit.
		return cacheDirPopulated(c.Dir)
	default:
		slog.Debug("cache VERSION file unreadable", "dir", c.Dir, "error", err.Error())
		return false
	}
}

// WriteVersionStamp records the current SchemaVersion at the cache
// root. Callers write this after the first successful cache write so
// subsequent DetectSchemaDrift calls can short-circuit. The write is
// atomic via a tempfile + rename so an interrupted process cannot
// leave VERSION truncated and mis-fire drift detection on the next run.
func (c *Cache) WriteVersionStamp() error {
	if c == nil || c.Dir == "" {
		return nil
	}
	if err := os.MkdirAll(c.Dir, 0o755); err != nil {
		return err
	}
	target := filepath.Join(c.Dir, versionFileName)
	tmp, err := os.CreateTemp(c.Dir, versionFileName+".*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	if _, err := tmp.WriteString(SchemaVersion + "\n"); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	if err := os.Rename(tmpPath, target); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	return nil
}

func readVersionFile(dir string) (string, error) {
	b, err := os.ReadFile(filepath.Join(dir, versionFileName)) //nolint:gosec // cache path
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(b)), nil
}

// cacheDirPopulated returns true when dir contains at least one JSON
// cache entry. Runs at most once per plan (only when VERSION is missing
// — the pre-v1 upgrade path), and iterates past non-JSON siblings like
// macOS `.DS_Store` or editor swap files so a single foreign entry in
// the cache root doesn't mask a populated cache from schema-drift.
func cacheDirPopulated(dir string) bool {
	f, err := os.Open(dir) //nolint:gosec // cache path
	if err != nil {
		return false
	}
	defer func() { _ = f.Close() }()
	for {
		batch, err := f.ReadDir(64)
		for _, e := range batch {
			if !e.IsDir() && filepath.Ext(e.Name()) == ".json" {
				return true
			}
		}
		if err != nil {
			// io.EOF or a genuine read error — in either case we've
			// seen every entry that will arrive and none matched.
			return false
		}
	}
}
