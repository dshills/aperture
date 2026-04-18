package eval

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestEmitJSON_Deterministic(t *testing.T) {
	r := &RunReport{
		SchemaVersion:         "1.0",
		ApertureVersion:       "test",
		SelectionLogicVersion: "sel-v1",
		Fixtures: []FixtureResult{
			{Name: "a", Metrics: Metrics{Precision: 1, Recall: 1, F1: 1}},
			{Name: "b", Metrics: Metrics{Precision: 0.5, Recall: 0.5, F1: 0.5}},
		},
		Regressions:  []RegressedFixt{},
		Orphaned:     []string{},
		Unreferenced: []string{},
		PerRunMetadata: PerRunMetadata{
			GeneratedAt:         time.Date(2026, 4, 18, 0, 0, 0, 0, time.UTC).Format(time.RFC3339),
			WallClockDurationMS: 123,
			Host:                "host",
			PID:                 1,
			ApertureVersion:     "test",
		},
	}
	a, err := EmitJSON(r)
	if err != nil {
		t.Fatal(err)
	}
	b, err := EmitJSON(r)
	if err != nil {
		t.Fatal(err)
	}
	if string(a) != string(b) {
		t.Fatal("EmitJSON is not deterministic for the same input")
	}
}

func TestStripPerRunJSON_RemovesKey(t *testing.T) {
	in := []byte(`{"schema_version":"1.0","per_run_metadata":{"pid":1,"host":"h"},"x":1}`)
	out, err := StripPerRunJSON(in)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(out, &m); err != nil {
		t.Fatal(err)
	}
	if _, ok := m[PerRunMetadataJSONKey]; ok {
		t.Error("per_run_metadata not stripped")
	}
	if _, ok := m["x"]; !ok {
		t.Error("other keys dropped by strip")
	}
}

func TestEmitMarkdown_HasPerRunSection(t *testing.T) {
	r := &RunReport{
		SchemaVersion: "1.0",
		Fixtures:      []FixtureResult{{Name: "a", Metrics: Metrics{F1: 1}}},
		PerRunMetadata: PerRunMetadata{
			GeneratedAt: "2026-04-18T00:00:00Z", PID: 1, Host: "h",
		},
	}
	md := string(EmitMarkdown(r))
	if !strings.Contains(md, PerRunMetadataMDHeading) {
		t.Errorf("markdown missing Per-Run Metadata heading")
	}
	stripped := string(StripPerRunMarkdown([]byte(md)))
	if strings.Contains(stripped, PerRunMetadataMDHeading) {
		t.Error("strip did not remove heading")
	}
}
