package repo

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// caseInsensitiveCache memoizes isCaseInsensitiveFS per repo root. The
// same `aperture` process may be used as a library with multiple roots
// (e.g., from a long-lived test runner); sync.Map keeps the cache
// concurrency-safe without a global lock.
var caseInsensitiveCache sync.Map // map[string]bool

// rewriteCaseToOnDisk rewrites `rel` (forward-slash, repo-relative)
// segment-by-segment so its casing matches the actual on-disk entries
// under repoRoot, if the filesystem is case-insensitive. On
// case-sensitive filesystems the path is returned unchanged — a
// mismatch surfaces later as a "path not found" / "not a directory"
// error (§7.4.4 filesystem check).
//
// The rewrite is per-segment rather than whole-path because a user
// typing `Services/billing` against on-disk `services/billing` needs
// only the first segment corrected.
func rewriteCaseToOnDisk(repoRoot, rel string) (string, error) {
	if rel == "" {
		return rel, nil
	}
	if !isCaseInsensitiveFS(repoRoot) {
		return rel, nil
	}
	segs := strings.Split(rel, "/")
	cur := repoRoot
	out := make([]string, 0, len(segs))
	for _, seg := range segs {
		actual, ok := findSegmentCaseInsensitive(cur, seg)
		if !ok {
			// Leave the remainder untouched; ResolveScope's
			// filesystem check will surface a clear "not found"
			// error that points at the offending segment.
			out = append(out, seg)
			cur = filepath.Join(cur, seg)
			continue
		}
		out = append(out, actual)
		cur = filepath.Join(cur, actual)
	}
	return strings.Join(out, "/"), nil
}

// findSegmentCaseInsensitive reads parent and returns the first entry
// whose name case-insensitively equals seg. The second return is false
// when no such entry exists.
func findSegmentCaseInsensitive(parent, seg string) (string, bool) {
	entries, err := os.ReadDir(parent)
	if err != nil {
		return "", false
	}
	lower := strings.ToLower(seg)
	for _, e := range entries {
		if strings.ToLower(e.Name()) == lower {
			return e.Name(), true
		}
	}
	return "", false
}

// isCaseInsensitiveFS reports whether the filesystem backing root
// ignores case in path lookups. Result is memoized per-root for the
// process lifetime (stable across the single plan invocation that
// resolves scope).
//
// Detection is stat-only: pick any existing directory entry under
// root, flip the case of one ASCII letter in its name, and Lstat the
// mutated path. Success → case-insensitive; ErrNotExist →
// case-sensitive. Any other error → treat as case-sensitive (the
// stricter default) — the plan will then fail validation with a clear
// "path not found" if the user typed the wrong case.
func isCaseInsensitiveFS(root string) bool {
	if v, ok := caseInsensitiveCache.Load(root); ok {
		return v.(bool)
	}
	result := detectCaseInsensitive(root)
	caseInsensitiveCache.Store(root, result)
	return result
}

func detectCaseInsensitive(root string) bool {
	entries, err := os.ReadDir(root)
	if err != nil {
		return false
	}
	for _, e := range entries {
		name := e.Name()
		flipped, ok := flipOneASCIILetter(name)
		if !ok {
			continue
		}
		if _, err := os.Lstat(filepath.Join(root, flipped)); err == nil {
			return true
		} else if errors.Is(err, os.ErrNotExist) {
			return false
		}
		// Other errors: try the next entry.
	}
	return false
}

// flipOneASCIILetter returns a copy of name with one ASCII letter
// flipped to its opposite case. Returns ("", false) when the name
// contains no ASCII letters (dot-files like ".git" work; pure-digit
// entries do not).
func flipOneASCIILetter(name string) (string, bool) {
	for i, r := range name {
		switch {
		case 'a' <= r && r <= 'z':
			return name[:i] + strings.ToUpper(string(r)) + name[i+len(string(r)):], true
		case 'A' <= r && r <= 'Z':
			return name[:i] + strings.ToLower(string(r)) + name[i+len(string(r)):], true
		}
	}
	return "", false
}

// assert fmt is referenced so goimports doesn't strip it — used by
// ErrorString builders elsewhere in the package. Harmless no-op.
var _ = fmt.Sprintf
