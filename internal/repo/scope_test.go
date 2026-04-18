package repo

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestParseScopeInput_Sentinels(t *testing.T) {
	for _, s := range []string{"", "."} {
		_, err := ParseScopeInput(s)
		if !errors.Is(err, ErrScopeSentinel) {
			t.Errorf("ParseScopeInput(%q) → %v, want ErrScopeSentinel", s, err)
		}
	}
}

func TestParseScopeInput_Transformation(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"services/billing", "services/billing"},
		{"./services/billing", "services/billing"},
		{"services/billing/", "services/billing"},
		{"services\\billing", "services/billing"},
		{"services/./billing", "services/billing"},
		{"services/././billing", "services/billing"},
	}
	for _, c := range cases {
		got, err := ParseScopeInput(c.in)
		if err != nil {
			t.Fatalf("ParseScopeInput(%q) → err %v", c.in, err)
		}
		if got != c.want {
			t.Errorf("ParseScopeInput(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestResolveScope_ValidSubdir(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "services", "billing"), 0o755); err != nil {
		t.Fatal(err)
	}
	s, err := ResolveScope(root, "services/billing")
	if err != nil {
		t.Fatalf("ResolveScope: %v", err)
	}
	if s.Path != "services/billing" {
		t.Errorf("Path=%q, want services/billing", s.Path)
	}
	if s.AbsPath == "" {
		t.Error("AbsPath empty")
	}
}

func TestResolveScope_RejectsDotDot(t *testing.T) {
	root := t.TempDir()
	_, err := ResolveScope(root, "services/../..")
	var sv *ScopeValidationError
	if !errors.As(err, &sv) {
		t.Fatalf("want ScopeValidationError, got %v", err)
	}
}

func TestResolveScope_RejectsLeadingSlash(t *testing.T) {
	root := t.TempDir()
	_, err := ResolveScope(root, "/services/billing")
	var sv *ScopeValidationError
	if !errors.As(err, &sv) {
		t.Fatalf("want ScopeValidationError, got %v", err)
	}
}

func TestResolveScope_RejectsMissingDir(t *testing.T) {
	root := t.TempDir()
	_, err := ResolveScope(root, "services/missing")
	var sv *ScopeValidationError
	if !errors.As(err, &sv) {
		t.Fatalf("want ScopeValidationError, got %v", err)
	}
}

func TestResolveScope_RejectsFileNotDirectory(t *testing.T) {
	root := t.TempDir()
	f := filepath.Join(root, "README.md")
	if err := os.WriteFile(f, []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := ResolveScope(root, "README.md")
	var sv *ScopeValidationError
	if !errors.As(err, &sv) {
		t.Fatalf("want ScopeValidationError, got %v", err)
	}
}

func TestResolveScope_SymlinkInsideRoot(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "real"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "real"), filepath.Join(root, "link")); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}
	s, err := ResolveScope(root, "link")
	if err != nil {
		t.Fatalf("expected symlink-to-inside to resolve, got %v", err)
	}
	// Scope.Path stores the user's canonicalized input, not the
	// symlink target. AbsPath is the resolved dir.
	if s.Path != "link" {
		t.Errorf("Path=%q, want link", s.Path)
	}
}

func TestResolveScope_SymlinkEscapingRoot(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "escape")); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}
	_, err := ResolveScope(root, "escape")
	var sv *ScopeValidationError
	if !errors.As(err, &sv) {
		t.Fatalf("want ScopeValidationError for escaping symlink, got %v", err)
	}
}

func TestScope_Contains(t *testing.T) {
	s := Scope{Path: "services/billing"}
	cases := []struct {
		path string
		want bool
	}{
		{"services/billing", true},
		{"services/billing/x.go", true},
		{"services/billing/sub/y.go", true},
		{"services/billing-extra/y.go", false},
		{"services/other", false},
		{"root.go", false},
	}
	for _, c := range cases {
		if got := s.Contains(c.path); got != c.want {
			t.Errorf("Contains(%q) = %v, want %v", c.path, got, c.want)
		}
	}
	// Empty scope contains everything.
	empty := Scope{}
	if !empty.Contains("any/path") {
		t.Error("empty scope should contain every path")
	}
}
