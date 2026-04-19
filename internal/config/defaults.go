package config

// DefaultWeights are the v1 scoring weights from SPEC §7.4.2.2. They must
// sum to 1.0 (±0.001 after any user overrides).
func DefaultWeights() Weights {
	return Weights{
		Mention:  0.25,
		Filename: 0.12,
		Symbol:   0.20,
		Import:   0.12,
		Package:  0.10,
		Test:     0.08,
		Doc:      0.07,
		Config:   0.06,
	}
}

// DefaultMentionDampener returns the v1.1 §7.2.3 defaults. These are the
// values that apply when the `scoring.mention_dampener` block is absent
// from a user's .aperture.yaml. Per §7.2.3, each field also defaults
// independently when the block is present but partial.
func DefaultMentionDampener() MentionDampener {
	return MentionDampener{
		Enabled: true,
		Floor:   0.30,
		Slope:   0.70,
	}
}

// DefaultReserve is the §9.1.1 reserved-token baseline.
func DefaultReserve() Reserve {
	return Reserve{
		Instructions: 6000,
		Reasoning:    20000,
		ToolOutput:   12000,
		Expansion:    10000,
	}
}

// DefaultExclusions returns the mandatory v1 exclusion glob set (§7.4.3).
// User-supplied exclusions are unioned with these unless
// exclude.disable_defaults: true.
func DefaultExclusions() []string {
	return []string{
		".git/**",
		".hg/**",
		".svn/**",
		".aperture/**",
		"node_modules/**",
		"vendor/**",
		"dist/**",
		"build/**",
		"out/**",
		"target/**",
		"bin/**",
		"obj/**",
		"coverage/**",
		".coverage/**",
		"htmlcov/**",
		".next/**",
		".nuxt/**",
		".cache/**",
		".venv/**",
		"venv/**",
		"__pycache__/**",
		".pytest_cache/**",
		".mypy_cache/**",
		".tox/**",
		".gradle/**",
		".idea/**",
		".vscode/**",
		"*.min.js",
		"*.min.css",
		"*.map",
		"*.wasm",
		"*.exe",
		"*.dll",
		"*.so",
		"*.dylib",
		"*.a",
		"*.o",
		"*.class",
		"*.jar",
		"*.war",
		"*.pyc",
		"*.pyo",
		"*.pdb",
		"*.lock",
		"package-lock.json",
		"yarn.lock",
		"pnpm-lock.yaml",
		"Cargo.lock",
		"poetry.lock",
		"uv.lock",
	}
}

// DefaultAgents is the built-in agents block from §9.1.2. The built-in
// claude and codex entries default to `pass_task_as_arg: false` because the
// task text is already embedded in the merged prompt file.
func DefaultAgents() map[string]AgentEntry {
	f := false
	return map[string]AgentEntry{
		"claude": {
			Command:       "claude",
			Args:          []string{},
			PassTaskAsArg: &f,
			Mode:          "non-interactive",
		},
		"codex": {
			Command:       "codex",
			Args:          []string{},
			PassTaskAsArg: &f,
		},
	}
}

// DefaultThresholds sets the v1 thresholds to unset (no gate).
func DefaultThresholds() Thresholds {
	return Thresholds{
		MinFeasibility:     0.0,
		FailOnBlockingGaps: false,
	}
}

// Default returns the fully-populated default configuration.
func Default() Config {
	return Config{
		Version: 1,
		Defaults: Defaults{
			Model:   "",
			Budget:  0,
			Reserve: DefaultReserve(),
		},
		Exclude:         DefaultExclusions(),
		DisableDefaults: false,
		Languages: Languages{
			Go:         LanguageEntry{Enabled: true},
			Markdown:   LanguageEntry{Enabled: true},
			TypeScript: LanguageEntry{Enabled: true},
			JavaScript: LanguageEntry{Enabled: true},
			Python:     LanguageEntry{Enabled: true},
		},
		Thresholds: DefaultThresholds(),
		Output: Output{
			Directory: ".aperture/manifests",
			Format:    "json",
		},
		Scoring: Scoring{
			Weights:         DefaultWeights(),
			MentionDampener: DefaultMentionDampener(),
		},
		Gaps:   Gaps{Blocking: []string{}},
		Agents: DefaultAgents(),
	}
}
