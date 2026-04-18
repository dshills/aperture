// Package repo discovers repository roots and walks files for indexing.
package repo

import (
	"fmt"
	"os"
	"path/filepath"
)

// DiscoverRoot walks upward from startPath looking for a `.git` marker. If
// one is found, that directory is returned. If none is found, startPath is
// returned as-is, per SPEC §7.1.2. The returned path is absolute and
// cleaned. A non-directory startPath is an error (exit 4).
func DiscoverRoot(startPath string) (string, error) {
	abs, err := filepath.Abs(startPath)
	if err != nil {
		return "", fmt.Errorf("resolve %q: %w", startPath, err)
	}
	fi, err := os.Stat(abs)
	if err != nil {
		return "", fmt.Errorf("stat %q: %w", abs, err)
	}
	if !fi.IsDir() {
		return "", fmt.Errorf("%q is not a directory", abs)
	}

	cur := abs
	for {
		if info, err := os.Stat(filepath.Join(cur, ".git")); err == nil && (info.IsDir() || info.Mode().IsRegular()) {
			return cur, nil
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			// reached filesystem root without a marker; honor §7.1.2 and
			// return the caller-supplied path as-is.
			return abs, nil
		}
		cur = parent
	}
}
