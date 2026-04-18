package manifest

import (
	"testing"
)

// A freshly-emitted Phase-1 stub manifest must validate against the embedded
// JSON Schema. This catches schema/catalogue drift as soon as any emitted
// field deviates.
func TestValidate_AcceptsStubManifest(t *testing.T) {
	m := newStubManifest()
	if err := ApplyHash(m); err != nil {
		t.Fatalf("ApplyHash: %v", err)
	}
	b, err := EmitJSON(m)
	if err != nil {
		t.Fatalf("EmitJSON: %v", err)
	}
	if err := Validate(b); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

// An obviously malformed payload (bad manifest_hash prefix) must be rejected.
func TestValidate_RejectsMalformedHash(t *testing.T) {
	bad := []byte(`{"schema_version":"1.0","manifest_id":"apt_aaaaaaaaaaaaaaaa","manifest_hash":"not-a-hash","generated_at":"2026-04-17T00:00:00Z","incomplete":false,"task":{},"repo":{},"budget":{},"selections":[],"reachable":[],"exclusions":[],"gaps":[],"feasibility":{},"generation_metadata":{}}`)
	if err := Validate(bad); err == nil {
		t.Fatal("expected schema validation error for bad manifest_hash")
	}
}
