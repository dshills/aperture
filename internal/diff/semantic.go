package diff

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/dshills/aperture/internal/manifest"
)

// ErrUnsupportedSchema is returned by LoadManifestFile when the input
// manifest declares a schema_version older than the minimum v1.1
// `aperture diff` understands. Callers translate to exit 1 per §7.7.
var ErrUnsupportedSchema = errors.New("unsupported schema_version")

// minSchemaVersion is the oldest schema_version this tool accepts.
// Comparison is major-then-minor integer: "1.0" < "1.10", not the
// lexicographic result.
const minSchemaVersion = "1.0"

// LoadManifestFile reads path, parses the manifest JSON, and enforces
// the §7.7 schema_version >= "1.0" rule. Returns *ErrUnsupportedSchema
// (wrapped) for malformed or too-old versions.
func LoadManifestFile(path string) (*manifest.Manifest, error) {
	b, err := os.ReadFile(path) //nolint:gosec // path from user
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var m manifest.Manifest
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	ok, err := schemaVersionAtLeast(m.SchemaVersion, minSchemaVersion)
	if err != nil {
		return nil, fmt.Errorf("%w: %s has invalid schema_version %q: %s",
			ErrUnsupportedSchema, path, m.SchemaVersion, err.Error())
	}
	if !ok {
		return nil, fmt.Errorf("%w: %s declares schema_version %q; minimum is %q",
			ErrUnsupportedSchema, path, m.SchemaVersion, minSchemaVersion)
	}
	return &m, nil
}

// schemaVersionAtLeast parses both v and min as major.minor integers
// and returns whether v >= min. Lexicographic string comparison would
// incorrectly order "1.10" below "1.9"; this function uses pairwise
// integer comparison instead.
func schemaVersionAtLeast(v, minVer string) (bool, error) {
	vMaj, vMin, err := parseMajorMinor(v)
	if err != nil {
		return false, err
	}
	mMaj, mMin, err := parseMajorMinor(minVer)
	if err != nil {
		return false, err
	}
	if vMaj != mMaj {
		return vMaj > mMaj, nil
	}
	return vMin >= mMin, nil
}

func parseMajorMinor(s string) (int, int, error) {
	parts := strings.SplitN(s, ".", 3)
	if len(parts) == 0 || parts[0] == "" {
		return 0, 0, fmt.Errorf("empty version: %q", s)
	}
	maj, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, 0, fmt.Errorf("major component %q: %w", parts[0], err)
	}
	// Single-component versions (e.g. "2") default minor to 0 for
	// forward compatibility — a future manifest that declares just
	// "2" reads as 2.0 >= 1.0.
	if len(parts) < 2 {
		return maj, 0, nil
	}
	minPart, err := strconv.Atoi(parts[1])
	if err != nil {
		return 0, 0, fmt.Errorf("minor component %q: %w", parts[1], err)
	}
	return maj, minPart, nil
}
