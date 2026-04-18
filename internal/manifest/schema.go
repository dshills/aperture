package manifest

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"

	"github.com/dshills/aperture/schema"
	"github.com/santhosh-tekuri/jsonschema/v5"
)

func bytesReader(b []byte) io.Reader { return bytes.NewReader(b) }

// Validate checks a manifest JSON payload against the embedded v1 schema.
// Returns nil on success. Callers map a validation failure to exit code 6.
func Validate(payload []byte) error {
	s, err := loadSchema()
	if err != nil {
		return fmt.Errorf("load schema: %w", err)
	}
	var doc any
	if err := json.Unmarshal(payload, &doc); err != nil {
		return fmt.Errorf("parse manifest json: %w", err)
	}
	if err := s.Validate(doc); err != nil {
		return fmt.Errorf("manifest schema validation: %w", err)
	}
	return nil
}

func loadSchema() (*jsonschema.Schema, error) {
	compiler := jsonschema.NewCompiler()
	if err := compiler.AddResource("manifest.v1.json", bytesReader(schema.ManifestV1())); err != nil {
		return nil, err
	}
	return compiler.Compile("manifest.v1.json")
}
