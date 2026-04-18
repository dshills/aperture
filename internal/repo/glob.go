package repo

import (
	"path"
	"strings"
)

// compiledGlob is a preprocessed form of a glob pattern. The walker uses a
// small, purpose-built matcher instead of Go's stdlib `path.Match` because
// the spec's default-exclusion list uses `**` (recursive wildcard) which
// `path.Match` does not support. Matching is case-sensitive except for
// patterns that explicitly look like filenames (no `/`), which match both
// the basename and the full relative path.
type compiledGlob struct {
	pattern string
	// hasDoublestar is true when the pattern contains `**`.
	hasDoublestar bool
	// filenameOnly is true when the pattern contains no `/` — we then
	// also try matching it against the file's basename.
	filenameOnly bool
}

func compileGlobs(patterns []string) []compiledGlob {
	out := make([]compiledGlob, 0, len(patterns))
	for _, p := range patterns {
		out = append(out, compiledGlob{
			pattern:       p,
			hasDoublestar: strings.Contains(p, "**"),
			filenameOnly:  !strings.Contains(p, "/"),
		})
	}
	return out
}

func matchAny(rel string, globs []compiledGlob) bool {
	for _, g := range globs {
		if globMatch(g, rel) {
			return true
		}
	}
	return false
}

// globMatch tests rel against g.pattern. Paths are forward-slash and
// repo-relative; a trailing slash on rel marks it as a directory path for
// directory-scoped patterns like `vendor/**`.
func globMatch(g compiledGlob, rel string) bool {
	relNoSlash := strings.TrimSuffix(rel, "/")

	if g.filenameOnly {
		if ok, _ := path.Match(g.pattern, path.Base(relNoSlash)); ok {
			return true
		}
		if ok, _ := path.Match(g.pattern, relNoSlash); ok {
			return true
		}
		return false
	}

	if g.hasDoublestar {
		return matchDoublestar(g.pattern, relNoSlash)
	}

	ok, _ := path.Match(g.pattern, relNoSlash)
	return ok
}

// matchDoublestar handles patterns with at most one `**`. A multi-`**`
// pattern (e.g., `a/**/b/**/c`) is treated as non-matching rather than
// silently matching by accident; v1's built-in pattern set contains no
// such patterns, and user-supplied ones via .aperture.yaml therefore
// degrade to a no-op rather than matching false positives.
func matchDoublestar(pattern, rel string) bool {
	parts := strings.Split(pattern, "**")
	if len(parts) == 2 && parts[0] == "" && parts[1] == "" {
		return true
	}
	if len(parts) != 2 {
		return false
	}

	// `vendor/**` → prefix "vendor/", suffix "" meaning "rel starts with
	// vendor/". `**/foo` → suffix "/foo" meaning "rel ends with /foo or
	// rel == foo". The prefix test enforces a path-boundary so `.git/**`
	// matches `.git/x` but not `.github/x`.
	prefix, suffix := parts[0], parts[1]
	tail := rel
	if prefix != "" {
		trimmedPrefix := strings.TrimSuffix(prefix, "/")
		switch {
		case rel == trimmedPrefix:
			tail = ""
		case strings.HasPrefix(rel, trimmedPrefix+"/"):
			tail = strings.TrimPrefix(rel, trimmedPrefix+"/")
		default:
			return false
		}
	}
	if suffix == "" {
		return true
	}
	suffix = strings.TrimPrefix(suffix, "/")
	return matchSuffixAtBoundary(suffix, tail)
}

// matchSuffixAtBoundary checks whether suffix glob-matches the tail at any
// path-component boundary (including tail itself).
func matchSuffixAtBoundary(suffix, tail string) bool {
	if ok, _ := path.Match(suffix, tail); ok {
		return true
	}
	// Try every "/a/b/c" → "b/c" suffix.
	s := tail
	for {
		idx := strings.Index(s, "/")
		if idx < 0 {
			return false
		}
		s = s[idx+1:]
		if ok, _ := path.Match(suffix, s); ok {
			return true
		}
	}
}
