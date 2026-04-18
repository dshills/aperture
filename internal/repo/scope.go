package repo

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// ErrScopeSentinel is a non-error sentinel returned by ParseScopeInput
// when the user explicitly unsets scope via "" or ".". Callers compare
// with errors.Is and proceed as if no scope was set.
var ErrScopeSentinel = errors.New("scope unset via sentinel")

// ScopeValidationError signals a §7.4.4 violation. Callers translate
// this to exit 4 per §7.7 / §7.4.6.
type ScopeValidationError struct {
	Input  string
	Reason string
}

func (e *ScopeValidationError) Error() string {
	return fmt.Sprintf("invalid --scope %q: %s", e.Input, e.Reason)
}

// Scope is the resolved v1.1 §7.4.4 scope projection. Path is the
// canonicalized repo-relative form emitted into the manifest. AbsPath
// is the walker-usable absolute path (never emitted). Empty Scope{}
// means "no scope set" — treat as whole-repo.
type Scope struct {
	Path    string // repo-relative, forward-slash, no leading "./" or trailing "/"
	AbsPath string // absolute, symlink-resolved path under RepoRoot
}

// IsSet reports whether scope is active for this plan.
func (s Scope) IsSet() bool { return s.Path != "" }

// Contains reports whether the given repo-relative path (forward-slash)
// is inside the scope. Always returns true when scope is unset.
func (s Scope) Contains(relPath string) bool {
	if !s.IsSet() {
		return true
	}
	// "services/billing" contains "services/billing/x.go" and
	// "services/billing" itself but not "services/billing-extra/y.go"
	// (segment boundary required).
	if relPath == s.Path {
		return true
	}
	return strings.HasPrefix(relPath, s.Path+"/")
}

// ParseScopeInput runs the §7.4.4 Transformation phase on input.
//   - Sentinel inputs "" and "." return ErrScopeSentinel (not an error).
//   - Any other input is transformed textually (backslash → slash,
//     strip leading "./", strip one trailing "/", collapse /./ → /).
//
// Validation is NOT performed here; call ResolveScope for the full
// transformation + validation + filesystem resolution pipeline.
func ParseScopeInput(input string) (string, error) {
	// Sentinels bypass both phases entirely (§7.4.5). They are not
	// paths; do NOT run §7.4.4 validation on them.
	if input == "" || input == "." {
		return "", ErrScopeSentinel
	}
	// Transformation phase (always succeeds).
	s := strings.ReplaceAll(input, "\\", "/")
	s = strings.TrimPrefix(s, "./")
	s = strings.TrimSuffix(s, "/")
	// Collapse interior "/./" → "/" (repeat until stable).
	for strings.Contains(s, "/./") {
		s = strings.ReplaceAll(s, "/./", "/")
	}
	return s, nil
}

// ResolveScope runs transformation + validation + filesystem resolution
// for input relative to repoRoot. Returns an empty Scope and nil when
// the sentinel is used. Returns *ScopeValidationError for §7.4.4
// violations; the caller translates that to exit 4.
func ResolveScope(repoRoot, input string) (Scope, error) {
	transformed, err := ParseScopeInput(input)
	if err != nil {
		if errors.Is(err, ErrScopeSentinel) {
			return Scope{}, nil
		}
		return Scope{}, err
	}
	if err := validateScopePath(input, transformed); err != nil {
		return Scope{}, err
	}

	// Case determinism: on case-insensitive filesystems, rewrite the
	// typed casing to match the on-disk casing per segment. On
	// case-sensitive filesystems, a typed-vs-actual casing mismatch
	// fails the "must be a directory" check below — no rewriting.
	canonical, err := rewriteCaseToOnDisk(repoRoot, transformed)
	if err != nil {
		return Scope{}, err
	}

	// Filesystem checks. Resolve via Stat (which follows symlinks).
	abs := filepath.Join(repoRoot, filepath.FromSlash(canonical))
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		if os.IsNotExist(err) {
			return Scope{}, &ScopeValidationError{Input: input, Reason: "path not found"}
		}
		return Scope{}, &ScopeValidationError{Input: input, Reason: fmt.Sprintf("resolve: %s", err.Error())}
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return Scope{}, &ScopeValidationError{Input: input, Reason: fmt.Sprintf("stat: %s", err.Error())}
	}
	if !info.IsDir() {
		return Scope{}, &ScopeValidationError{Input: input, Reason: "scope must be a directory"}
	}

	// The resolved walker path must stay under the repo root
	// (§7.4.5 symlink rule). We allow a scope that is itself a
	// symlink whose target is inside the repo, but reject one whose
	// target escapes.
	absRepoRoot, err := filepath.EvalSymlinks(repoRoot)
	if err != nil {
		absRepoRoot = repoRoot
	}
	if !pathWithin(resolved, absRepoRoot) {
		return Scope{}, &ScopeValidationError{Input: input, Reason: "scope resolves outside the repo root"}
	}

	return Scope{Path: canonical, AbsPath: resolved}, nil
}

// validateScopePath implements the §7.4.4 Validation phase.
func validateScopePath(input, transformed string) error {
	if transformed == "" {
		return &ScopeValidationError{Input: input, Reason: "path is empty after normalization"}
	}
	if strings.HasPrefix(transformed, "/") {
		return &ScopeValidationError{Input: input, Reason: "path must not begin with /"}
	}
	if strings.ContainsRune(transformed, 0) {
		return &ScopeValidationError{Input: input, Reason: "path contains a null byte"}
	}
	// Reject any ".." segment, anywhere. Per §7.4.4 there is no
	// legitimate scope use for upward traversal.
	if slices.Contains(strings.Split(transformed, "/"), "..") {
		return &ScopeValidationError{Input: input, Reason: "path must not contain '..' segments"}
	}
	return nil
}

// pathWithin reports whether child is inside (or equal to) parent on
// the filesystem. Both arguments must already be symlink-resolved.
func pathWithin(child, parent string) bool {
	if child == parent {
		return true
	}
	sep := string(filepath.Separator)
	return strings.HasPrefix(child, parent+sep)
}
