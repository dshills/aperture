// Package schema embeds the authoritative Aperture manifest JSON Schema.
package schema

import _ "embed"

//go:embed manifest.v1.json
var manifestV1 []byte

// ManifestV1 returns the raw bytes of the embedded v1 manifest schema.
func ManifestV1() []byte {
	out := make([]byte, len(manifestV1))
	copy(out, manifestV1)
	return out
}
