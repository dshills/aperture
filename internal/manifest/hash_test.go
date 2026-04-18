package manifest

import (
	"encoding/json"
	"strings"
	"testing"
)

func newStubManifest() *Manifest {
	return &Manifest{
		SchemaVersion: SchemaVersion,
		GeneratedAt:   "2026-04-17T00:00:00Z",
		Incomplete:    false,
		Task: Task{
			TaskID:             "tsk_0123456789abcdef",
			Source:             "<inline>",
			RawText:            "add oauth refresh",
			Type:               ActionTypeFeature,
			Objective:          "add oauth refresh",
			Anchors:            []string{"add", "oauth", "refresh"},
			ExpectsTests:       true,
			ExpectsConfig:      false,
			ExpectsDocs:        false,
			ExpectsMigration:   false,
			ExpectsAPIContract: false,
		},
		Repo: Repo{Root: "/repo", Fingerprint: "", LanguageHints: []string{}},
		Budget: Budget{
			Model:                   "claude-sonnet",
			TokenCeiling:            120000,
			Reserved:                Reserved{Instructions: 6000, Reasoning: 20000, ToolOutput: 12000, Expansion: 10000},
			EffectiveContextBudget:  72000,
			EstimatedSelectedTokens: 0,
			Estimator:               "heuristic-3.5",
			EstimatorVersion:        "v1",
		},
		Selections: []Selection{},
		Reachable:  []Reachable{},
		Exclusions: []Exclusion{},
		Gaps:       []Gap{},
		Feasibility: Feasibility{
			Score:              0.0,
			Assessment:         "stub",
			Positives:          []string{},
			Negatives:          []string{},
			BlockingConditions: []string{},
			SubSignals:         map[string]float64{},
		},
		GenerationMetadata: GenerationMetadata{
			ApertureVersion:         "dev",
			SelectionLogicVersion:   SelectionLogicVersion,
			ConfigDigest:            "sha256:" + strings.Repeat("0", 64),
			SideEffectTablesVersion: SideEffectTablesVer,
			Host:                    "testhost",
			PID:                     1234,
			WallClockStartedAt:      "2026-04-17T00:00:00Z",
		},
	}
}

// Hash must be byte-stable across repeated invocations.
func TestHash_Deterministic(t *testing.T) {
	m := newStubManifest()
	h1, id1, err := Hash(m)
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}
	h2, id2, err := Hash(m)
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}
	if h1 != h2 || id1 != id2 {
		t.Fatalf("Hash non-deterministic: %s/%s vs %s/%s", h1, id1, h2, id2)
	}
}

// Changing an excluded field (generated_at / host / pid / aperture_version /
// wall_clock_started_at) must not change the hash.
func TestHash_IgnoresExcludedFields(t *testing.T) {
	base := newStubManifest()
	baseHash, _, err := Hash(base)
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}

	mut := newStubManifest()
	mut.GeneratedAt = "2099-01-01T00:00:00Z"
	mut.GenerationMetadata.Host = "other"
	mut.GenerationMetadata.PID = 9999
	mut.GenerationMetadata.ApertureVersion = "1.2.3"
	mut.GenerationMetadata.WallClockStartedAt = "2099-01-01T00:00:00Z"

	got, _, err := Hash(mut)
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}
	if got != baseHash {
		t.Fatalf("hash changed after mutating excluded-only fields: base=%s mut=%s", baseHash, got)
	}
}

// Changing an included field (selection_logic_version, config_digest,
// side_effect_tables_version, or the task body) MUST change the hash.
func TestHash_ChangesOnIncludedFields(t *testing.T) {
	base := newStubManifest()
	baseHash, _, _ := Hash(base)

	mut := newStubManifest()
	mut.GenerationMetadata.ConfigDigest = "sha256:" + strings.Repeat("f", 64)
	got, _, _ := Hash(mut)
	if got == baseHash {
		t.Fatalf("config_digest change was ignored by hash (included field)")
	}
}

func TestApplyHash_FillsIDDerivedFromHash(t *testing.T) {
	m := newStubManifest()
	if err := ApplyHash(m); err != nil {
		t.Fatalf("ApplyHash: %v", err)
	}
	// manifest_id = "apt_" + manifest_hash[0:16] (hex portion, skipping "sha256:")
	const prefix = "sha256:"
	wantID := "apt_" + m.ManifestHash[len(prefix):len(prefix)+16]
	if m.ManifestID != wantID {
		t.Fatalf("manifest_id derivation broken: got %s want %s", m.ManifestID, wantID)
	}
}

func TestEmitJSON_RoundTrip(t *testing.T) {
	m := newStubManifest()
	if err := ApplyHash(m); err != nil {
		t.Fatalf("ApplyHash: %v", err)
	}
	b, err := EmitJSON(m)
	if err != nil {
		t.Fatalf("EmitJSON: %v", err)
	}
	var parsed map[string]any
	if err := json.Unmarshal(b, &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if parsed["schema_version"] != "1.0" {
		t.Errorf("schema_version not emitted: %v", parsed["schema_version"])
	}
}
