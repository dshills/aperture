package eval

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"golang.org/x/text/unicode/norm"
)

// fixtureFingerprintSchema is the opaque schema literal written into the
// hash stream before any file data per §7.1.1. It lets a future bump of
// the algorithm avoid collision with pre-bump fixtures.
const fixtureFingerprintSchema = "fixture-fingerprint-v1"

// SymlinkError signals a fixture whose repo/ subtree contains a symlink —
// §7.1.1 rejects these outright (exit 2).
type SymlinkError struct {
	Path string
}

func (e *SymlinkError) Error() string {
	return fmt.Sprintf("symlink not allowed under fixture repo/: %s", e.Path)
}

// FingerprintRepo computes the normative fixture-repo fingerprint per
// SPEC §7.1.1. Only regular files under repoDir contribute. Directories,
// symlinks, and non-regular entries are either skipped (directories) or
// rejected (symlinks). Paths are NFC-normalized, forward-slash,
// repo-relative without a leading "./" before being fed to the hash.
func FingerprintRepo(repoDir string) (string, error) {
	abs, err := filepath.Abs(repoDir)
	if err != nil {
		return "", fmt.Errorf("abs: %w", err)
	}
	st, err := os.Lstat(abs)
	if err != nil {
		return "", fmt.Errorf("stat fixture repo: %w", err)
	}
	if !st.IsDir() {
		return "", fmt.Errorf("fixture repo is not a directory: %s", abs)
	}

	type entry struct {
		rel string
		sum string
	}
	var entries []entry

	walkErr := filepath.WalkDir(abs, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		// Reject symlinks (§7.1.1).
		info, lerr := d.Info()
		if lerr != nil {
			return lerr
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return &SymlinkError{Path: path}
		}
		if !info.Mode().IsRegular() {
			// Non-regular, non-symlink (device, socket, fifo): also
			// reject — fixtures are source trees only.
			return fmt.Errorf("non-regular file under fixture repo/: %s", path)
		}

		rel, rerr := filepath.Rel(abs, path)
		if rerr != nil {
			return rerr
		}
		rel = filepath.ToSlash(rel)
		rel = strings.TrimPrefix(rel, "./")
		rel = norm.NFC.String(rel)

		sum, herr := sha256File(path)
		if herr != nil {
			return herr
		}
		entries = append(entries, entry{rel: rel, sum: sum})
		return nil
	})
	if walkErr != nil {
		var se *SymlinkError
		if errors.As(walkErr, &se) {
			return "", se
		}
		return "", walkErr
	}

	sort.Slice(entries, func(i, j int) bool { return entries[i].rel < entries[j].rel })

	h := sha256.New()
	h.Write([]byte(fixtureFingerprintSchema))
	h.Write([]byte{0})
	for _, e := range entries {
		h.Write([]byte(e.rel))
		h.Write([]byte{0})
		h.Write([]byte(e.sum))
		h.Write([]byte{0})
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil)), nil
}

// sha256File returns the lowercase hex digest of the file at path.
func sha256File(path string) (string, error) {
	f, err := os.Open(path) //nolint:gosec // path from filepath.WalkDir under fixture root
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// VerifyRepoFingerprint computes the fingerprint of repoDir and returns
// nil iff it equals expected. Callers translate the mismatch error to
// exit 2 per §7.7.
func VerifyRepoFingerprint(repoDir, expected string) error {
	got, err := FingerprintRepo(repoDir)
	if err != nil {
		return err
	}
	if got != expected {
		return fmt.Errorf("fixture repo fingerprint mismatch: expected %s, got %s", expected, got)
	}
	return nil
}
