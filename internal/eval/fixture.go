// Package eval ships the v1.1 fixture harness: deterministic regression
// scoring for the Aperture planner against a committed ground-truth set
// (SPEC §4.1, §7.1). The harness is read-only toward the planner — it
// invokes internal/pipeline.Build and the manifest assembler in-process
// but never feeds its measurements back into scoring.
package eval

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Fixture is the parsed form of a single `*.eval.yaml` case. Paths are
// repo-relative (forward-slash) per SPEC §7.1.1. Either Task or TaskFile
// is set, never both.
type Fixture struct {
	// Name is the fixture identifier (also becomes the test-case id).
	Name string
	// Dir is the absolute path to the fixture directory containing the
	// YAML file and the adjacent repo/ snapshot.
	Dir string
	// YAMLPath is the absolute path to the fixture's YAML file (for
	// diagnostics).
	YAMLPath string

	Task              string // inline task text (mutually exclusive with TaskFile)
	TaskFile          string // repo-relative path under repo/ (mutually exclusive with Task)
	Budget            int
	Model             string
	RepoFingerprint   string
	Expected          Expected
	AgentCheckCommand string        // optional; empty if not declared
	AgentCheckTimeout time.Duration // parsed from agent_check.timeout
}

// Expected captures the ground-truth assertions a fixture makes against
// the emitted manifest (SPEC §7.1.1).
type Expected struct {
	Selections []ExpectedSelection
	Forbidden  []string
	Gaps       []string
}

// ExpectedSelection names a required selection. LoadMode is optional; an
// empty string means "any mode counts" (§7.1.2 tiebreaker).
type ExpectedSelection struct {
	Path     string
	LoadMode string
}

// rawFixture mirrors the YAML schema with pointer fields where needed to
// detect unset vs. zero. yaml.v3 KnownFields(true) rejects unknown keys.
type rawFixture struct {
	Name            string         `yaml:"name"`
	Task            *string        `yaml:"task"`
	TaskFile        *string        `yaml:"task_file"`
	Budget          int            `yaml:"budget"`
	Model           string         `yaml:"model"`
	RepoFingerprint string         `yaml:"repo_fingerprint"`
	Expected        rawExpected    `yaml:"expected"`
	AgentCheck      *rawAgentCheck `yaml:"agent_check"`
}

type rawExpected struct {
	Selections []rawExpectedSelection `yaml:"selections"`
	Forbidden  []rawForbiddenEntry    `yaml:"forbidden"`
	Gaps       []rawGapEntry          `yaml:"gaps"`
}

type rawExpectedSelection struct {
	Path     string `yaml:"path"`
	LoadMode string `yaml:"load_mode"`
}

type rawForbiddenEntry struct {
	Path string `yaml:"path"`
}

type rawGapEntry struct {
	Type string `yaml:"type"`
}

type rawAgentCheck struct {
	Command string `yaml:"command"`
	Timeout string `yaml:"timeout"`
}

// LoadError carries a fixture-path prefix for diagnostics and the inner
// cause. Callers translate it to exit 2 per §7.7.
type LoadError struct {
	Path string
	Err  error
}

func (e *LoadError) Error() string { return fmt.Sprintf("%s: %s", e.Path, e.Err.Error()) }
func (e *LoadError) Unwrap() error { return e.Err }

// LoadFixture parses a single `*.eval.yaml` at yamlPath and returns the
// populated Fixture. The YAML is decoded in strict mode; unknown keys
// produce a LoadError. The fixture's repo/ subdirectory is NOT walked
// here — fingerprint verification is a separate step (see VerifyRepoFingerprint).
func LoadFixture(yamlPath string) (Fixture, error) {
	abs, err := filepath.Abs(yamlPath)
	if err != nil {
		return Fixture{}, &LoadError{Path: yamlPath, Err: fmt.Errorf("abs: %w", err)}
	}
	f, err := os.Open(abs) //nolint:gosec // path supplied by fixture loader
	if err != nil {
		return Fixture{}, &LoadError{Path: abs, Err: err}
	}
	defer func() { _ = f.Close() }()

	var raw rawFixture
	dec := yaml.NewDecoder(f)
	dec.KnownFields(true)
	if err := dec.Decode(&raw); err != nil {
		if errors.Is(err, io.EOF) {
			return Fixture{}, &LoadError{Path: abs, Err: fmt.Errorf("empty fixture file")}
		}
		return Fixture{}, &LoadError{Path: abs, Err: fmt.Errorf("parse yaml: %w", err)}
	}

	fx, err := normalizeFixture(raw, abs)
	if err != nil {
		return Fixture{}, &LoadError{Path: abs, Err: err}
	}
	return fx, nil
}

// normalizeFixture applies the §7.1.1 validation rules.
func normalizeFixture(raw rawFixture, yamlPath string) (Fixture, error) {
	if strings.TrimSpace(raw.Name) == "" {
		return Fixture{}, fmt.Errorf("name is required")
	}
	// Exactly one of task / task_file (§7.1.1): setting both or neither
	// is a structural error.
	if raw.Task == nil && raw.TaskFile == nil {
		return Fixture{}, fmt.Errorf("exactly one of `task` or `task_file` must be set")
	}
	if raw.Task != nil && raw.TaskFile != nil {
		return Fixture{}, fmt.Errorf("set exactly one of `task` or `task_file`; both were provided")
	}
	if raw.Task != nil && strings.TrimSpace(*raw.Task) == "" {
		return Fixture{}, fmt.Errorf("`task` is set but empty; leave unset or provide text")
	}
	if raw.TaskFile != nil && strings.TrimSpace(*raw.TaskFile) == "" {
		return Fixture{}, fmt.Errorf("`task_file` is set but empty; leave unset or provide a path")
	}

	if raw.Budget <= 0 {
		return Fixture{}, fmt.Errorf("budget must be > 0 (got %d)", raw.Budget)
	}
	if strings.TrimSpace(raw.Model) == "" {
		return Fixture{}, fmt.Errorf("model is required")
	}
	if !strings.HasPrefix(raw.RepoFingerprint, "sha256:") || len(raw.RepoFingerprint) != len("sha256:")+64 {
		return Fixture{}, fmt.Errorf("repo_fingerprint must be sha256:<64-hex>")
	}

	fx := Fixture{
		Name:            raw.Name,
		Dir:             filepath.Dir(yamlPath),
		YAMLPath:        yamlPath,
		Budget:          raw.Budget,
		Model:           raw.Model,
		RepoFingerprint: raw.RepoFingerprint,
	}
	if raw.Task != nil {
		fx.Task = *raw.Task
	}
	if raw.TaskFile != nil {
		fx.TaskFile = *raw.TaskFile
	}

	for _, s := range raw.Expected.Selections {
		if strings.TrimSpace(s.Path) == "" {
			return Fixture{}, fmt.Errorf("expected.selections[*].path is required")
		}
		fx.Expected.Selections = append(fx.Expected.Selections, ExpectedSelection{
			Path: normalizeRepoRelPath(s.Path), LoadMode: s.LoadMode,
		})
	}
	for _, f := range raw.Expected.Forbidden {
		if strings.TrimSpace(f.Path) == "" {
			return Fixture{}, fmt.Errorf("expected.forbidden[*].path is required")
		}
		fx.Expected.Forbidden = append(fx.Expected.Forbidden, normalizeRepoRelPath(f.Path))
	}
	for _, g := range raw.Expected.Gaps {
		if strings.TrimSpace(g.Type) == "" {
			return Fixture{}, fmt.Errorf("expected.gaps[*].type is required")
		}
		fx.Expected.Gaps = append(fx.Expected.Gaps, g.Type)
	}

	if raw.AgentCheck != nil {
		if strings.TrimSpace(raw.AgentCheck.Command) == "" {
			return Fixture{}, fmt.Errorf("agent_check.command is required when agent_check is declared")
		}
		if strings.TrimSpace(raw.AgentCheck.Timeout) == "" {
			return Fixture{}, fmt.Errorf("agent_check.timeout is required when agent_check is declared")
		}
		// §7.2.3 bare-integer rejection: ParseDuration accepts "30s", rejects "30".
		d, err := time.ParseDuration(raw.AgentCheck.Timeout)
		if err != nil {
			return Fixture{}, fmt.Errorf("agent_check.timeout: %w", err)
		}
		if d <= 0 {
			return Fixture{}, fmt.Errorf("agent_check.timeout must be > 0")
		}
		fx.AgentCheckCommand = raw.AgentCheck.Command
		fx.AgentCheckTimeout = d
	}
	return fx, nil
}

// normalizeRepoRelPath strips a leading "./" and rewrites backslashes to
// forward slashes. Further validation lives in the caller; this keeps
// comparisons stable against manifest.Selection.Path.
func normalizeRepoRelPath(p string) string {
	p = strings.ReplaceAll(p, "\\", "/")
	p = strings.TrimPrefix(p, "./")
	return p
}
