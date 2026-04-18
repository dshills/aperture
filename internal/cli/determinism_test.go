package cli

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/dshills/aperture/internal/manifest"
)

// §8.3 / §18.4: 20 consecutive plan runs on identical inputs must
// produce byte-equivalent normalized JSON manifests. We run
// BuildManifest directly so the cache + pipeline machinery is covered
// end-to-end; generated_at, host, pid, and wall_clock_started_at vary
// per run but are excluded from the hash (§7.9.4), so we compare
// manifest_hash + manifest_id instead of the raw bytes.
func TestPlan_TwentyRun_ByteIdenticalHash(t *testing.T) {
	if testing.Short() {
		t.Skip("short mode: skipping 20-run determinism suite")
	}
	const task = "add refresh handling to internal/oauth/provider.go for github oauth"
	var (
		firstHash string
		firstID   string
	)
	for i := 0; i < 20; i++ {
		in := buildFixtureInputs(t, task, "", 120000)
		m, err := BuildManifest(in)
		if err != nil {
			t.Fatalf("run %d: BuildManifest: %v", i, err)
		}
		if i == 0 {
			firstHash, firstID = m.ManifestHash, m.ManifestID
			continue
		}
		if m.ManifestHash != firstHash {
			t.Fatalf("run %d: hash diverged: %s vs %s", i, m.ManifestHash, firstHash)
		}
		if m.ManifestID != firstID {
			t.Fatalf("run %d: id diverged: %s vs %s", i, m.ManifestID, firstID)
		}
	}
}

// Scrub-then-compare determinism: emit JSON twice, strip the hash-
// excluded fields, and assert the remaining bodies are byte-identical.
// Catches drifts that the hash-only check above would miss because two
// manifests can share the same hash while emitting different ignored
// fields — we care about both.
func TestPlan_HashExcludedScrubIsByteIdentical(t *testing.T) {
	const task = "add refresh handling to internal/oauth/provider.go for github oauth"
	a := emitScrubbed(t, task)
	b := emitScrubbed(t, task)
	if !bytes.Equal(a, b) {
		t.Fatalf("normalized JSON diverged across runs:\n--- run1 ---\n%s\n--- run2 ---\n%s", a, b)
	}
}

// emitScrubbed runs BuildManifest once, emits JSON, unmarshals it, and
// removes every field §7.9.4 excludes from the hash input so a direct
// byte comparison reflects the stable portion of the manifest.
func emitScrubbed(t *testing.T, task string) []byte {
	t.Helper()
	in := buildFixtureInputs(t, task, "", 120000)
	m, err := BuildManifest(in)
	if err != nil {
		t.Fatalf("BuildManifest: %v", err)
	}
	raw, err := manifest.EmitJSON(m)
	if err != nil {
		t.Fatalf("EmitJSON: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	// §7.9.4-excluded top-level and nested fields.
	for _, k := range []string{"generated_at", "manifest_id", "manifest_hash"} {
		delete(doc, k)
	}
	if meta, ok := doc["generation_metadata"].(map[string]any); ok {
		for _, k := range []string{"aperture_version", "host", "pid", "wall_clock_started_at"} {
			delete(meta, k)
		}
	}
	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	return out
}
