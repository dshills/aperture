package goanalysis

import (
	"slices"
	"testing"
)

func TestSideEffects_OSImportDoesNotPullProcessTag(t *testing.T) {
	tags := SideEffectsFor([]string{"os"})
	if !slices.Contains(tags, "io:filesystem") {
		t.Errorf("os should have io:filesystem: %v", tags)
	}
	if slices.Contains(tags, "io:process") {
		t.Errorf("os alone must NOT produce io:process: %v", tags)
	}
}

// Critical invariant from §12.2: the `os (excludes: os/exec)` rule means
// an `os/exec` import MUST NOT inherit io:filesystem.
func TestSideEffects_OSExecExcludesFilesystem(t *testing.T) {
	tags := SideEffectsFor([]string{"os/exec"})
	if slices.Contains(tags, "io:filesystem") {
		t.Fatalf("os/exec must NOT inherit io:filesystem via the os prefix: %v", tags)
	}
	if !slices.Contains(tags, "io:process") {
		t.Fatalf("os/exec must produce io:process: %v", tags)
	}
}

// Segment-boundary matching is a property of matchesPrefix itself:
// `net/http` must match `net/http/httputil` but NOT `net/httpfoobar`.
// Using matchesPrefix directly avoids interference from other table
// entries (e.g. the separate `net` rule which legitimately covers any
// net/* descendant).
func TestMatchesPrefix_SegmentBoundary(t *testing.T) {
	cases := []struct {
		imp, prefix string
		want        bool
	}{
		{"net/http", "net/http", true},
		{"net/http/httputil", "net/http", true},
		{"net/httpfoobar", "net/http", false},
		{"net/httptest", "net/http", false},
		{"os", "os", true},
		{"os/exec", "os", true},
		{"oscommon", "os", false},
	}
	for _, tc := range cases {
		if got := matchesPrefix(tc.imp, tc.prefix); got != tc.want {
			t.Errorf("matchesPrefix(%q, %q) = %v, want %v", tc.imp, tc.prefix, got, tc.want)
		}
	}
}

func TestSideEffects_MultipleTagsPerImport(t *testing.T) {
	// net/http brings io:network; combined with database/sql we expect both.
	tags := SideEffectsFor([]string{"net/http", "database/sql"})
	if !slices.Contains(tags, "io:network") || !slices.Contains(tags, "io:database") {
		t.Fatalf("expected both io:network and io:database: %v", tags)
	}
}

func TestSideEffects_SortedDeduped(t *testing.T) {
	tags := SideEffectsFor([]string{"log", "log", "log/slog"})
	if len(tags) != 1 || tags[0] != "io:logging" {
		t.Fatalf("dedup/sort broken: %v", tags)
	}
}
