// Package config loads and validates the .aperture.yaml configuration.
package config

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// Config is the fully-resolved effective configuration after merging
// built-in defaults, the on-disk .aperture.yaml, and CLI overrides.
type Config struct {
	Version         int                   `yaml:"version" json:"version"`
	Defaults        Defaults              `yaml:"defaults" json:"defaults"`
	Exclude         []string              `yaml:"exclude" json:"exclude"`
	DisableDefaults bool                  `yaml:"exclude_disable_defaults" json:"exclude_disable_defaults"`
	Languages       Languages             `yaml:"languages" json:"languages"`
	Thresholds      Thresholds            `yaml:"thresholds" json:"thresholds"`
	Output          Output                `yaml:"output" json:"output"`
	Scoring         Scoring               `yaml:"scoring" json:"scoring"`
	Gaps            Gaps                  `yaml:"gaps" json:"gaps"`
	Agents          map[string]AgentEntry `yaml:"agents" json:"agents"`
}

type Defaults struct {
	Model   string  `yaml:"model" json:"model"`
	Budget  int     `yaml:"budget" json:"budget"`
	Reserve Reserve `yaml:"reserve" json:"reserve"`
}

type Reserve struct {
	Instructions int `yaml:"instructions" json:"instructions"`
	Reasoning    int `yaml:"reasoning" json:"reasoning"`
	ToolOutput   int `yaml:"tool_output" json:"tool_output"`
	Expansion    int `yaml:"expansion" json:"expansion"`
}

type Languages struct {
	Go       LanguageEntry `yaml:"go" json:"go"`
	Markdown LanguageEntry `yaml:"markdown" json:"markdown"`
}

type LanguageEntry struct {
	Enabled bool `yaml:"enabled" json:"enabled"`
}

type Thresholds struct {
	MinFeasibility     float64 `yaml:"min_feasibility" json:"min_feasibility"`
	FailOnBlockingGaps bool    `yaml:"fail_on_blocking_gaps" json:"fail_on_blocking_gaps"`
}

type Output struct {
	Directory string `yaml:"directory" json:"directory"`
	Format    string `yaml:"format" json:"format"`
}

type Scoring struct {
	Weights Weights `yaml:"weights" json:"weights"`
}

type Weights struct {
	Mention  float64 `yaml:"mention" json:"mention"`
	Filename float64 `yaml:"filename" json:"filename"`
	Symbol   float64 `yaml:"symbol" json:"symbol"`
	Import   float64 `yaml:"import" json:"import"`
	Package  float64 `yaml:"package" json:"package"`
	Test     float64 `yaml:"test" json:"test"`
	Doc      float64 `yaml:"doc" json:"doc"`
	Config   float64 `yaml:"config" json:"config"`
}

// Sum returns the sum of all weights.
func (w Weights) Sum() float64 {
	return w.Mention + w.Filename + w.Symbol + w.Import + w.Package + w.Test + w.Doc + w.Config
}

type Gaps struct {
	Blocking []string `yaml:"blocking" json:"blocking"`
}

// AgentEntry describes one row of the §9.1.2 agents map.
// PassTaskAsArg is a pointer so the loader can distinguish "unset" from
// "explicitly false", which matters for applying the different built-in
// defaults for claude/codex vs. user-declared adapters.
type AgentEntry struct {
	Command       string            `yaml:"command" json:"command"`
	Args          []string          `yaml:"args" json:"args"`
	PassTaskAsArg *bool             `yaml:"pass_task_as_arg" json:"pass_task_as_arg,omitempty"`
	Mode          string            `yaml:"mode" json:"mode,omitempty"`
	Env           map[string]string `yaml:"env" json:"env,omitempty"`
}

// LoadOptions controls Load behavior.
type LoadOptions struct {
	// Path is the resolved path to .aperture.yaml. If empty, Load returns
	// the default configuration.
	Path string
}

// Load reads the file at opts.Path (if any) and merges it with the v1
// built-in defaults. Unknown top-level YAML keys are rejected (§16 exit 5).
func Load(opts LoadOptions) (Config, error) {
	cfg := Default()
	if opts.Path == "" {
		return cfg, nil
	}
	f, err := os.Open(opts.Path) //nolint:gosec // config path supplied by user
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return cfg, nil
		}
		return cfg, fmt.Errorf("open config: %w", err)
	}
	defer func() { _ = f.Close() }()
	return decode(f, cfg)
}

// LoadBytes decodes b on top of the defaults. Used by tests.
func LoadBytes(b []byte) (Config, error) {
	return decode(strings.NewReader(string(b)), Default())
}

func decode(r io.Reader, base Config) (Config, error) {
	dec := yaml.NewDecoder(r)
	dec.KnownFields(true)

	// Use an intermediate type that mirrors Config but with pointer fields
	// so we can detect which sections were explicitly supplied and merge
	// them onto the defaults without erasing unset fields.
	var raw rawConfig
	if err := dec.Decode(&raw); err != nil {
		if errors.Is(err, io.EOF) {
			return base, nil
		}
		return base, fmt.Errorf("parse config yaml: %w", err)
	}
	return raw.merge(base)
}

// rawConfig uses pointer fields so the YAML decoder distinguishes "absent"
// from "zero-valued". Unknown fields are rejected because KnownFields is
// enabled on the decoder.
type rawConfig struct {
	Version         *int                  `yaml:"version"`
	Defaults        *rawDefaults          `yaml:"defaults"`
	Exclude         *[]string             `yaml:"exclude"`
	DisableDefaults *bool                 `yaml:"exclude_disable_defaults"`
	Languages       *Languages            `yaml:"languages"`
	Thresholds      *Thresholds           `yaml:"thresholds"`
	Output          *Output               `yaml:"output"`
	Scoring         *rawScoring           `yaml:"scoring"`
	Gaps            *Gaps                 `yaml:"gaps"`
	Agents          map[string]AgentEntry `yaml:"agents"`
}

type rawDefaults struct {
	Model   *string  `yaml:"model"`
	Budget  *int     `yaml:"budget"`
	Reserve *Reserve `yaml:"reserve"`
}

type rawScoring struct {
	Weights *Weights `yaml:"weights"`
}

func (r rawConfig) merge(base Config) (Config, error) {
	if r.Version != nil {
		base.Version = *r.Version
	}
	if r.Defaults != nil {
		if r.Defaults.Model != nil {
			base.Defaults.Model = *r.Defaults.Model
		}
		if r.Defaults.Budget != nil {
			base.Defaults.Budget = *r.Defaults.Budget
		}
		if r.Defaults.Reserve != nil {
			base.Defaults.Reserve = *r.Defaults.Reserve
		}
	}
	if r.DisableDefaults != nil {
		base.DisableDefaults = *r.DisableDefaults
	}
	if r.Exclude != nil {
		if base.DisableDefaults {
			base.Exclude = append([]string{}, *r.Exclude...)
		} else {
			base.Exclude = unionExclusions(base.Exclude, *r.Exclude)
		}
	} else if base.DisableDefaults {
		base.Exclude = []string{}
	}
	if r.Languages != nil {
		base.Languages = *r.Languages
	}
	if r.Thresholds != nil {
		base.Thresholds = *r.Thresholds
	}
	if r.Output != nil {
		if r.Output.Directory != "" {
			base.Output.Directory = r.Output.Directory
		}
		if r.Output.Format != "" {
			base.Output.Format = r.Output.Format
		}
	}
	if r.Scoring != nil && r.Scoring.Weights != nil {
		base.Scoring.Weights = *r.Scoring.Weights
	}
	if r.Gaps != nil {
		base.Gaps = *r.Gaps
	}
	if len(r.Agents) > 0 {
		for name, entry := range r.Agents {
			base.Agents[name] = entry
		}
	}
	return base, nil
}

func unionExclusions(defaults, user []string) []string {
	set := map[string]struct{}{}
	for _, s := range defaults {
		set[s] = struct{}{}
	}
	for _, s := range user {
		set[s] = struct{}{}
	}
	out := make([]string, 0, len(set))
	for s := range set {
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

// Digest returns `sha256:` + hex over the compact, key-sorted JSON form of
// the fully-resolved effective config. Used as the manifest's config_digest.
func (c Config) Digest() (string, error) {
	buf, err := json.Marshal(c)
	if err != nil {
		return "", err
	}
	var generic any
	if err := json.Unmarshal(buf, &generic); err != nil {
		return "", err
	}
	canon, err := canonicalJSON(generic)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(canon)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}
