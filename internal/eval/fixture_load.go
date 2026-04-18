package eval

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
)

// LoadFixtures walks fixturesDir and returns every discovered *.eval.yaml
// case in deterministic order (lexicographic by fixture name). Duplicate
// names across sibling directories are rejected with an error (exit 2).
// The fixtures directory itself must exist; an empty directory is not an
// error — it returns an empty slice and nil.
func LoadFixtures(fixturesDir string) ([]Fixture, error) {
	abs, err := filepath.Abs(fixturesDir)
	if err != nil {
		return nil, fmt.Errorf("abs: %w", err)
	}
	st, err := os.Stat(abs)
	if err != nil {
		return nil, fmt.Errorf("fixtures dir: %w", err)
	}
	if !st.IsDir() {
		return nil, fmt.Errorf("fixtures path is not a directory: %s", abs)
	}

	var yamlPaths []string
	walkErr := filepath.WalkDir(abs, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".eval.yaml") {
			return nil
		}
		// Skip anything nested under a fixture's repo/ subdir — only
		// top-of-fixture YAMLs count.
		rel, rerr := filepath.Rel(abs, path)
		if rerr != nil {
			return rerr
		}
		parts := strings.Split(filepath.ToSlash(rel), "/")
		if slices.Contains(parts, "repo") {
			return nil
		}
		yamlPaths = append(yamlPaths, path)
		return nil
	})
	if walkErr != nil {
		return nil, walkErr
	}

	sort.Strings(yamlPaths)

	fixtures := make([]Fixture, 0, len(yamlPaths))
	seen := make(map[string]string, len(yamlPaths))
	for _, p := range yamlPaths {
		fx, err := LoadFixture(p)
		if err != nil {
			return nil, err
		}
		if prior, dup := seen[fx.Name]; dup {
			return nil, fmt.Errorf("fixture name %q declared twice: %s and %s", fx.Name, prior, p)
		}
		seen[fx.Name] = p
		fixtures = append(fixtures, fx)
	}
	// Final sort by name — file system order within a directory isn't
	// guaranteed to match alphabetical even after sorting paths. §8.1.
	sort.Slice(fixtures, func(i, j int) bool { return fixtures[i].Name < fixtures[j].Name })
	return fixtures, nil
}

// ResolveTaskText returns the task text for a fixture, reading task_file
// from the fixture's repo/ subdir when needed. Always returns the
// resolved text and a "source" string suitable for task.ParseOptions.
func ResolveTaskText(fx Fixture) (text, source string, isMarkdown bool, err error) {
	if fx.Task != "" {
		return fx.Task, "<inline>", false, nil
	}
	repoDir := filepath.Join(fx.Dir, "repo")
	p := filepath.Join(repoDir, filepath.FromSlash(fx.TaskFile))
	// Guard: task_file must resolve under repo/.
	absRepo, err := filepath.Abs(repoDir)
	if err != nil {
		return "", "", false, err
	}
	absTask, err := filepath.Abs(p)
	if err != nil {
		return "", "", false, err
	}
	if !strings.HasPrefix(absTask+string(os.PathSeparator), absRepo+string(os.PathSeparator)) && absTask != absRepo {
		return "", "", false, fmt.Errorf("task_file %q escapes fixture repo/", fx.TaskFile)
	}
	b, err := os.ReadFile(absTask) //nolint:gosec // path confined to fixture repo/
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", "", false, fmt.Errorf("task_file %q not found under repo/", fx.TaskFile)
		}
		return "", "", false, err
	}
	lowered := strings.ToLower(fx.TaskFile)
	md := strings.HasSuffix(lowered, ".md") || strings.HasSuffix(lowered, ".markdown") || strings.HasSuffix(lowered, ".mdx")
	return string(b), fx.TaskFile, md, nil
}
