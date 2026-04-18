package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Fuzz coverage for WriteInlineTaskFile, scaffolded in response to
// TESTREC-370EDA67. The unit test suite already covers the happy path
// and the orphan-sweep semantics; the fuzz target locks in the
// sanitizeID path-boundary invariant: no manifest_id — however
// malformed — can cause WriteInlineTaskFile to write OUTSIDE
// os.TempDir() via a path-traversal payload.

func FuzzWriteInlineTaskFile_ManifestIDIsPathConstrained(f *testing.F) {
	// Seed with the canonical shape plus a handful of attack-flavored
	// inputs. sanitizeID strips anything outside [A-Za-z0-9_-], so
	// path-traversal bytes should be elided rather than escape $TMPDIR.
	seeds := []string{
		"apt_abcdef0123456789",
		"../../../etc/passwd",
		"..\\..\\windows\\system32",
		"foo/bar",
		"foo\x00bar",
		"",
		"!@#$%^&*()",
		strings.Repeat("A", 2048),
	}
	for _, s := range seeds {
		f.Add(s)
	}

	// One base directory per fuzz run (not per iteration — t.TempDir
	// inside f.Fuzz tanks throughput and Go forbids t.Setenv there).
	// We route writes through WriteInlineTaskFileIn instead of
	// os.Setenv("TMPDIR", …) so the test never mutates the process-
	// global env that parallel test packages may depend on.
	baseDir, err := os.MkdirTemp("", "aperture-fuzz-*")
	if err != nil {
		f.Fatalf("create base tempdir: %v", err)
	}
	f.Cleanup(func() { _ = os.RemoveAll(baseDir) })

	f.Fuzz(func(t *testing.T, manifestID string) {
		// Per-iteration subdir so concurrent fuzz workers don't collide
		// on the deterministic `aperture-task-<sanitized>.txt` name.
		// os.MkdirTemp + RemoveAll is cheaper than t.TempDir because
		// we skip the testing framework's cleanup-defer chain.
		iterDir, err := os.MkdirTemp(baseDir, "iter-*")
		if err != nil {
			t.Fatalf("mkdir per-iter: %v", err)
		}
		defer func() { _ = os.RemoveAll(iterDir) }()
		absIterDir, _ := filepath.Abs(iterDir)

		path, cleanup, err := WriteInlineTaskFileIn(iterDir, manifestID, "fuzz-body")
		if err != nil {
			// A failure is acceptable (e.g. hitting a filesystem quota
			// during fuzzing); what we must NEVER accept is a success
			// that writes outside iterDir.
			return
		}
		defer cleanup()

		// Invariant 1: the file lives under iterDir. Any escape means
		// sanitizeID let a path-traversal byte through and the write
		// reached a different directory.
		absPath, _ := filepath.Abs(path)
		if !strings.HasPrefix(absPath, absIterDir+string(filepath.Separator)) && absPath != absIterDir {
			t.Fatalf("WriteInlineTaskFileIn(%q, %q) escaped its directory:\n  dir=%s\n  path=%s",
				iterDir, manifestID, absIterDir, absPath)
		}

		// Invariant 2: the filename matches the §7.10.4.1 shape
		// "aperture-task-<sanitized>.txt" — no path separators leak
		// into the basename.
		base := filepath.Base(path)
		if !strings.HasPrefix(base, tempfilePrefix) || !strings.HasSuffix(base, ".txt") {
			t.Fatalf("filename outside the aperture-task-*.txt shape: %q", base)
		}
		if strings.ContainsAny(base, "/\\") {
			t.Fatalf("filename contains path separators: %q", base)
		}

		// Invariant 3: the file actually exists with the body we wrote.
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("written tempfile unreadable: %v", err)
		}
		if string(body) != "fuzz-body" {
			t.Fatalf("body mismatch: %q", body)
		}
	})
}
