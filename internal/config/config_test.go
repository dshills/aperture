package config

import (
	"strings"
	"testing"
)

func TestDefault_WeightsSumToOne(t *testing.T) {
	cfg := Default()
	if err := cfg.Validate(); err != nil {
		t.Fatalf("default config should validate: %v", err)
	}
}

func TestLoadBytes_RejectsUnknownKey(t *testing.T) {
	_, err := LoadBytes([]byte("version: 1\nmystery_field: true\n"))
	if err == nil {
		t.Fatal("expected unknown-key rejection")
	}
}

func TestValidate_RejectsBadWeightSum(t *testing.T) {
	cfg := Default()
	cfg.Scoring.Weights.Mention = 0.99
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected weight-sum validation to fail")
	} else if !strings.Contains(err.Error(), "weights") {
		t.Fatalf("error should mention weights: %v", err)
	}
}

func TestLoadBytes_MergesUserExclusionsWithDefaults(t *testing.T) {
	cfg, err := LoadBytes([]byte("exclude:\n  - custom/**\n"))
	if err != nil {
		t.Fatalf("LoadBytes: %v", err)
	}
	var hasDefault, hasCustom bool
	for _, e := range cfg.Exclude {
		if e == ".git/**" {
			hasDefault = true
		}
		if e == "custom/**" {
			hasCustom = true
		}
	}
	if !hasDefault || !hasCustom {
		t.Fatalf("union-merge broken: got %v", cfg.Exclude)
	}
}

func TestLoadBytes_DisableDefaultsReplacesExclusions(t *testing.T) {
	cfg, err := LoadBytes([]byte("exclude_disable_defaults: true\nexclude:\n  - only_this/**\n"))
	if err != nil {
		t.Fatalf("LoadBytes: %v", err)
	}
	if !cfg.DisableDefaults {
		t.Fatal("DisableDefaults should be true")
	}
	if len(cfg.Exclude) != 1 || cfg.Exclude[0] != "only_this/**" {
		t.Fatalf("defaults should be replaced when disable_defaults set; got %v", cfg.Exclude)
	}
}

func TestLoadBytes_MalformedYAMLRejected(t *testing.T) {
	_, err := LoadBytes([]byte("version: [not an int\n"))
	if err == nil {
		t.Fatal("expected malformed yaml to produce an error")
	}
}

func TestConfigDigest_Deterministic(t *testing.T) {
	cfg := Default()
	d1, err := cfg.Digest()
	if err != nil {
		t.Fatalf("Digest: %v", err)
	}
	d2, err := cfg.Digest()
	if err != nil {
		t.Fatalf("Digest: %v", err)
	}
	if d1 != d2 {
		t.Fatalf("digest non-deterministic: %s vs %s", d1, d2)
	}
	if !strings.HasPrefix(d1, "sha256:") {
		t.Fatalf("digest missing sha256 prefix: %s", d1)
	}
}
