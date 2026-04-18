package index

import (
	"slices"
	"testing"
)

func newIndexFromPaths(paths ...string) *Index {
	files := make([]FileEntry, 0, len(paths))
	for _, p := range paths {
		files = append(files, FileEntry{Path: p})
	}
	return &Index{Files: files, SupplementalFiles: map[SupplementalCategory][]string{}}
}

func TestDetectSupplemental_SpecAndPlan(t *testing.T) {
	idx := newIndexFromPaths(
		"SPEC.md",
		"specs/initial/SPEC.md",
		"specs/initial/PLAN.md",
		"docs/spec-overview.md",
	)
	DetectSupplemental(idx)
	if !slices.Equal(idx.SupplementalFiles[SupSpec], []string{"SPEC.md", "docs/spec-overview.md", "specs/initial/SPEC.md"}) {
		t.Fatalf("unexpected spec bucket: %v", idx.SupplementalFiles[SupSpec])
	}
	if !slices.Contains(idx.SupplementalFiles[SupPlan], "specs/initial/PLAN.md") {
		t.Fatalf("PLAN bucket missing entry: %v", idx.SupplementalFiles[SupPlan])
	}
}

func TestDetectSupplemental_BuildFilesMultiHit(t *testing.T) {
	idx := newIndexFromPaths("Makefile", "go.mod", "pyproject.toml", "Dockerfile")
	DetectSupplemental(idx)

	build := idx.SupplementalFiles[SupBuildFiles]
	for _, want := range []string{"Makefile", "go.mod", "pyproject.toml", "Dockerfile"} {
		if !slices.Contains(build, want) {
			t.Errorf("build bucket missing %s: %v", want, build)
		}
	}

	// pyproject.toml also matches the lint_config pattern.
	if !slices.Contains(idx.SupplementalFiles[SupLintConfig], "pyproject.toml") {
		t.Errorf("pyproject.toml should also land in lint_config")
	}
}

func TestDetectSupplemental_CIConfigDoubleStarWorkflows(t *testing.T) {
	idx := newIndexFromPaths(".github/workflows/ci.yml", ".github/workflows/release.yaml")
	DetectSupplemental(idx)
	got := idx.SupplementalFiles[SupCIConfig]
	if !slices.Contains(got, ".github/workflows/ci.yml") || !slices.Contains(got, ".github/workflows/release.yaml") {
		t.Fatalf("ci_config bucket missing workflows: %v", got)
	}
}

func TestDetectSupplemental_CaseInsensitiveReadme(t *testing.T) {
	idx := newIndexFromPaths("README.md", "readme.md")
	DetectSupplemental(idx)
	got := idx.SupplementalFiles[SupReadme]
	if len(got) != 2 {
		t.Fatalf("expected both case variants to match, got %v", got)
	}
}
