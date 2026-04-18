package oauth

import "testing"

func TestProviderName(t *testing.T) {
	p := NewProvider("github")
	if p.Name() != "github" {
		t.Fatalf("unexpected name %q", p.Name())
	}
}
