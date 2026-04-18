package agent

import (
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// tempfilePrefix is the §7.10.4.1 naming convention for inline-task
// tempfiles. Every file Aperture writes in $TMPDIR for an inline task
// begins with this prefix so the orphan sweep can identify stragglers.
const tempfilePrefix = "aperture-task-"

// orphanAge is the §7.10.4.1 sweep threshold. Tempfiles older than this
// are considered orphans (most likely left behind by a hard kill or
// Windows where signal handlers don't fire) and are removed at startup.
const orphanAge = 24 * time.Hour

// WriteInlineTaskFile writes body under $TMPDIR with the §7.10.4.1
// inline-task naming convention and returns the absolute path. Callers
// are responsible for deleting the file when the run completes — the
// returned path AND a cleanup helper give both options.
//
// Security: $TMPDIR is a shared, world-writable directory on most
// systems, and the §7.10.4.1 filename shape is deterministic for a
// given manifest_id. To defeat symlink-attacks where an attacker pre-
// creates the target path pointing at a sensitive file, we open with
// O_CREATE|O_EXCL|O_WRONLY so the call fails if any entry (file,
// symlink, directory) already exists at the path. On first conflict
// we attempt one Remove + retry to tolerate a stale orphan from a
// prior crashed invocation; a second failure surfaces the error.
func WriteInlineTaskFile(manifestID, body string) (string, func(), error) {
	name := tempfilePrefix + sanitizeID(manifestID) + ".txt"
	path := filepath.Join(os.TempDir(), name)

	if err := writeTaskFileExcl(path, body); err != nil {
		// One retry — the canonical case is a leftover file from a
		// prior run that the orphan sweep hasn't reached yet. The
		// remove is deliberately a non-symlink-following op so we
		// don't follow an attacker's link into a sensitive target.
		if removeErr := os.Remove(path); removeErr == nil {
			if err2 := writeTaskFileExcl(path, body); err2 != nil {
				return "", nil, err2
			}
		} else {
			return "", nil, err
		}
	}
	cleanup := func() {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			slog.Warn("failed to remove inline-task tempfile", "path", path, "error", err.Error())
		}
	}
	return path, cleanup, nil
}

// writeTaskFileExcl opens path with O_CREATE|O_EXCL|O_WRONLY|O_TRUNC so
// the file is created exclusively (fails on any pre-existing entry,
// including symlinks). Mode 0600 matches the private-to-user convention.
func writeTaskFileExcl(path, body string) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY|os.O_TRUNC, 0o600) //nolint:gosec // mode 0600 is deliberate
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	if _, err := f.WriteString(body); err != nil {
		return err
	}
	return nil
}

// SweepOrphanTempfiles removes $TMPDIR/aperture-task-*.txt files whose
// mtime is older than the orphan threshold. It runs best-effort: the
// sweep never fails the caller. Per §7.10.4.1 this runs at the start of
// every `aperture run` invocation so orphaned files (from prior crashes,
// SIGKILL, or Windows) do not accumulate indefinitely.
func SweepOrphanTempfiles() {
	dir := os.TempDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		slog.Debug("orphan-tempfile sweep: readdir failed", "dir", dir, "error", err.Error())
		return
	}
	cutoff := time.Now().Add(-orphanAge)
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if !strings.HasPrefix(e.Name(), tempfilePrefix) || !strings.HasSuffix(e.Name(), ".txt") {
			continue
		}
		path := filepath.Join(dir, e.Name())
		info, err := e.Info()
		if err != nil {
			continue
		}
		if info.ModTime().After(cutoff) {
			continue
		}
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			slog.Debug("orphan-tempfile sweep: remove failed", "path", path, "error", err.Error())
		}
	}
}

// SignalsSupported is true on Unix platforms where SIGINT/SIGTERM/SIGHUP
// are deliverable and exposed via os/signal.Notify. On Windows we rely
// on defer-based cleanup plus the orphan sweep on the next invocation.
var SignalsSupported = runtime.GOOS != "windows"

// sanitizeID strips any path separator or byte outside the expected
// manifest-id character set so crafting a manifest_id can never produce
// a tempfile path pointing outside $TMPDIR. manifest_id is
// "apt_" + hex[:16] so in practice this is a no-op; the defensive check
// is here because the source is ultimately derived from user input.
func sanitizeID(id string) string {
	var b strings.Builder
	b.Grow(len(id))
	for _, r := range id {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case r == '_' || r == '-':
		default:
			continue
		}
		b.WriteRune(r)
	}
	if b.Len() == 0 {
		return "unknown"
	}
	return b.String()
}
