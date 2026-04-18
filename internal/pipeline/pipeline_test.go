package pipeline

import (
	"path/filepath"
	"slices"
	"testing"

	"github.com/dshills/aperture/internal/config"
	"github.com/dshills/aperture/internal/index"
	"github.com/dshills/aperture/internal/repo"
)

const smallGoFixture = "../../testdata/fixtures/small_go"
const nonGoFixture = "../../testdata/fixtures/non_go"

func TestBuild_SmallGoFixture(t *testing.T) {
	root, err := filepath.Abs(smallGoFixture)
	if err != nil {
		t.Fatal(err)
	}
	res, err := Build(BuildOptions{
		Root:            root,
		DefaultExcludes: config.DefaultExclusions(),
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	// Language hints include Go and markdown (plus yaml from .aperture.yaml).
	hints := res.Index.LanguageHints()
	for _, want := range []string{"go", "markdown", "yaml"} {
		if !slices.Contains(hints, want) {
			t.Errorf("missing language hint %q: %v", want, hints)
		}
	}

	// Supplemental: README and build_files.
	if got := res.Index.SupplementalFiles[index.SupReadme]; !slices.Contains(got, "README.md") {
		t.Errorf("README.md missing from supplemental: %v", got)
	}
	if got := res.Index.SupplementalFiles[index.SupBuildFiles]; !slices.Contains(got, "go.mod") {
		t.Errorf("go.mod missing from build_files: %v", got)
	}

	// AST: provider.go should export GitHubProvider, Provider, NewProvider, etc.
	f := res.Index.File("internal/oauth/provider.go")
	if f == nil {
		t.Fatalf("provider.go not found in index: %v", paths(res.Index))
	}
	names := symbolNames(f.Symbols)
	for _, want := range []string{"Provider", "GitHubProvider", "NewProvider", "Name", "RefreshToken", "ErrExpired", "DefaultTimeout"} {
		if !slices.Contains(names, want) {
			t.Errorf("symbol %q not extracted from provider.go: %v", want, names)
		}
	}

	// Side-effects: provider.go imports net/http → io:network; time → io:time.
	if !slices.Contains(f.SideEffects, "io:network") || !slices.Contains(f.SideEffects, "io:time") {
		t.Errorf("provider.go missing io:network or io:time: %v", f.SideEffects)
	}

	// Test-file linkage: provider_test.go <-> provider.go
	ftest := res.Index.File("internal/oauth/provider_test.go")
	if ftest == nil {
		t.Fatalf("provider_test.go not found in index")
	}
	if !slices.Contains(f.TestLinks, "internal/oauth/provider_test.go") {
		t.Errorf("provider.go missing test link to provider_test.go: %v", f.TestLinks)
	}
	if !slices.Contains(ftest.TestLinks, "internal/oauth/provider.go") {
		t.Errorf("provider_test.go missing reverse test link: %v", ftest.TestLinks)
	}

	// §12.2 !excludes carve-out: os/exec → io:process, NOT io:filesystem.
	exec := res.Index.File("internal/exec_only/exec.go")
	if exec == nil {
		t.Fatalf("exec_only/exec.go missing from index")
	}
	if !slices.Contains(exec.SideEffects, "io:process") {
		t.Errorf("exec_only should carry io:process: %v", exec.SideEffects)
	}
	if slices.Contains(exec.SideEffects, "io:filesystem") {
		t.Errorf("exec_only must NOT carry io:filesystem via os excludes: %v", exec.SideEffects)
	}
}

func TestBuild_NonGoFixtureProducesNoPanics(t *testing.T) {
	root, err := filepath.Abs(nonGoFixture)
	if err != nil {
		t.Fatal(err)
	}
	res, err := Build(BuildOptions{Root: root, DefaultExcludes: config.DefaultExclusions()})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	for _, f := range res.Index.Files {
		if f.Language == "go" {
			t.Fatalf("non_go fixture should have zero Go files: got %s", f.Path)
		}
	}
	if len(res.Index.Packages) != 0 {
		t.Fatalf("non_go fixture should have zero Go packages: %v", res.Index.Packages)
	}

	// Fingerprint still computes and is valid hex.
	fp, err := repo.Fingerprint(walkerFiles(res.Index), "1.0.0")
	if err != nil {
		t.Fatal(err)
	}
	if len(fp) != len("sha256:")+64 {
		t.Fatalf("invalid fingerprint shape: %s", fp)
	}
}

func paths(idx *index.Index) []string {
	out := make([]string, 0, len(idx.Files))
	for _, f := range idx.Files {
		out = append(out, f.Path)
	}
	return out
}

func symbolNames(symbols []index.Symbol) []string {
	out := make([]string, 0, len(symbols))
	for _, s := range symbols {
		out = append(out, s.Name)
	}
	return out
}

func walkerFiles(idx *index.Index) []repo.FileEntry {
	out := make([]repo.FileEntry, 0, len(idx.Files))
	for _, f := range idx.Files {
		out = append(out, repo.FileEntry{Path: f.Path, Size: f.Size, SHA256: f.SHA256, MTime: f.MTime})
	}
	return out
}
