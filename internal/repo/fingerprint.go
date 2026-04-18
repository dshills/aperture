package repo

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
)

// Fingerprint returns `sha256:` + hex computed per SPEC §6.4.1: a SHA-256
// over the compact JSON representation of {files[], aperture_version}
// where files[] is sorted ascending by normalized path and each entry
// is {path, sha256, size, mtime}.
//
// The computation is deliberately independent of Go's map iteration
// order; the canonical byte stream is constructed by hand.
func Fingerprint(files []FileEntry, apertureVersion string) (string, error) {
	sorted := append([]FileEntry{}, files...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Path < sorted[j].Path })

	var buf bytes.Buffer
	buf.WriteByte('{')
	buf.WriteString(`"aperture_version":`)
	if err := writeJSONString(&buf, apertureVersion); err != nil {
		return "", err
	}
	buf.WriteString(`,"files":[`)
	for i, f := range sorted {
		if i > 0 {
			buf.WriteByte(',')
		}
		// Field order matches the SPEC §6.4.1 literal: path, sha256, size, mtime.
		buf.WriteByte('{')
		buf.WriteString(`"path":`)
		if err := writeJSONString(&buf, f.Path); err != nil {
			return "", err
		}
		fmt.Fprintf(&buf, `,"sha256":%q,"size":%d,"mtime":`, f.SHA256, f.Size)
		if err := writeJSONString(&buf, f.MTime); err != nil {
			return "", err
		}
		buf.WriteByte('}')
	}
	buf.WriteString(`]}`)

	sum := sha256.Sum256(buf.Bytes())
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

func writeJSONString(buf *bytes.Buffer, s string) error {
	b, err := json.Marshal(s)
	if err != nil {
		return err
	}
	buf.Write(b)
	return nil
}
