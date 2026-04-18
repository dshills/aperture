package manifest

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

// hashExcludedPaths is the set of manifest field paths (dotted) that are
// stripped before hashing per §7.9.4. They are removed from the normalized
// map representation prior to compact-JSON serialization.
var hashExcludedPaths = []string{
	"manifest_hash",
	"manifest_id",
	"generated_at",
	"generation_metadata.aperture_version",
	"generation_metadata.host",
	"generation_metadata.pid",
	"generation_metadata.wall_clock_started_at",
}

// Hash returns (manifest_hash, manifest_id) for m. The manifest_hash is
// "sha256:" + hex over the compact, lexicographically-key-sorted JSON form
// of m with the §7.9.4 excluded fields removed. manifest_id is
// "apt_" + manifest_hash[0:16] computed over the raw hex (not the "sha256:"
// prefix), per §11.1.
func Hash(m *Manifest) (hashStr, id string, err error) {
	raw, err := marshalCanonical(m)
	if err != nil {
		return "", "", fmt.Errorf("canonical marshal: %w", err)
	}
	normalized, err := stripHashExcluded(raw)
	if err != nil {
		return "", "", fmt.Errorf("normalize: %w", err)
	}
	compact, err := compactSortedJSON(normalized)
	if err != nil {
		return "", "", fmt.Errorf("compact: %w", err)
	}
	sum := sha256.Sum256(compact)
	hexSum := hex.EncodeToString(sum[:])
	return "sha256:" + hexSum, "apt_" + hexSum[:16], nil
}

// ApplyHash fills m.ManifestHash and m.ManifestID in place.
func ApplyHash(m *Manifest) error {
	h, id, err := Hash(m)
	if err != nil {
		return err
	}
	m.ManifestHash = h
	m.ManifestID = id
	return nil
}
