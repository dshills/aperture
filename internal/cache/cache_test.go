package cache

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dshills/aperture/internal/index"
)

func sampleEntry() *Entry {
	return &Entry{
		Path:        "internal/foo.go",
		Size:        123,
		MTime:       "2026-04-17T00:00:00Z",
		SHA256:      "deadbeef",
		PackageName: "foo",
		Imports:     []string{"fmt", "net/http"},
		Symbols: []index.Symbol{
			{Name: "Hello", Kind: index.SymbolFunc},
		},
		SideEffects: []string{"io:network"},
	}
}

func TestPutGet_Roundtrip(t *testing.T) {
	c := New(t.TempDir(), "1.0.0")
	e := sampleEntry()
	key := Key(e.Path, e.Size, e.MTime, c.ToolVersion)
	if err := c.Put(key, e); err != nil {
		t.Fatalf("Put: %v", err)
	}
	got, err := c.Get(key)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Path != e.Path || got.PackageName != "foo" || len(got.Symbols) != 1 {
		t.Fatalf("entry mismatch: %+v", got)
	}
	if got.SchemaVersion != SchemaVersion {
		t.Errorf("schema version not stamped on Put: %q", got.SchemaVersion)
	}
}

func TestGet_MissReturnsErrMiss(t *testing.T) {
	c := New(t.TempDir(), "1.0.0")
	_, err := c.Get("nonexistent")
	if err != ErrMiss {
		t.Fatalf("expected ErrMiss, got %v", err)
	}
}

func TestGet_ToolVersionMismatchIsMiss(t *testing.T) {
	dir := t.TempDir()
	c1 := New(dir, "1.0.0")
	e := sampleEntry()
	key := Key(e.Path, e.Size, e.MTime, c1.ToolVersion)
	if err := c1.Put(key, e); err != nil {
		t.Fatal(err)
	}

	c2 := New(dir, "2.0.0")
	if _, err := c2.Get(key); err != ErrMiss {
		t.Fatalf("tool-version bump must be a miss, got %v", err)
	}
}

func TestGet_CorruptEntryIsRemovedAndMiss(t *testing.T) {
	dir := t.TempDir()
	c := New(dir, "1.0.0")
	path := filepath.Join(dir, "abc.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Get("abc"); err != ErrMiss {
		t.Fatalf("corrupt entry must return ErrMiss, got %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("corrupt file should have been removed: %v", err)
	}
}

func TestDetectSchemaDrift_WipeOnVERSIONMismatch(t *testing.T) {
	dir := t.TempDir()
	c := New(dir, "1.0.0")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Hand-stamp an older schema version so DetectSchemaDrift fires.
	if err := os.WriteFile(filepath.Join(dir, "VERSION"), []byte("cache-v0\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if !c.DetectSchemaDrift() {
		t.Fatal("drift should be detected when VERSION file is older")
	}
	c.InvalidateAll("test")
	entries, _ := os.ReadDir(dir)
	if len(entries) != 0 {
		t.Fatalf("InvalidateAll should empty dir; got %d entries", len(entries))
	}
}

// When VERSION is missing BUT the cache dir already holds JSON entries
// (upgrade from a pre-v1 build), drift detection must still fire so the
// stale entries get wiped.
func TestDetectSchemaDrift_MissingVERSIONOnPopulatedCacheDrifts(t *testing.T) {
	dir := t.TempDir()
	c := New(dir, "1.0.0")
	if err := os.WriteFile(filepath.Join(dir, "abc.json"), []byte(`{"path":"x"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if !c.DetectSchemaDrift() {
		t.Fatal("populated cache without VERSION should be treated as drifted")
	}
}

// Put must write the VERSION stamp so subsequent DetectSchemaDrift
// short-circuits without scanning the directory.
func TestPut_StampsVERSION(t *testing.T) {
	dir := t.TempDir()
	c := New(dir, "1.0.0")
	if err := c.Put("k", sampleEntry()); err != nil {
		t.Fatal(err)
	}
	stamp, err := os.ReadFile(filepath.Join(dir, "VERSION"))
	if err != nil {
		t.Fatalf("VERSION file not written: %v", err)
	}
	if got := strings.TrimSpace(string(stamp)); got != SchemaVersion {
		t.Fatalf("VERSION content = %q, want %q", got, SchemaVersion)
	}
	// Drift must return false because the VERSION matches.
	if c.DetectSchemaDrift() {
		t.Fatal("no drift should be detected after Put writes VERSION")
	}
}

func TestApertureDir_ClearDerivedKeepsAudit(t *testing.T) {
	root := filepath.Join(t.TempDir(), ".aperture")
	for _, sub := range []string{"cache", "index", "summaries", "manifests", "logs"} {
		if err := os.MkdirAll(filepath.Join(root, sub), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, sub, "marker"), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	dir := ApertureDir{Root: root}
	removed, err := dir.ClearApertureDerived(false)
	if err != nil {
		t.Fatalf("ClearApertureDerived: %v", err)
	}
	if removed != 3 {
		t.Errorf("expected 3 derived dirs removed, got %d", removed)
	}
	for _, derived := range []string{"cache", "index", "summaries"} {
		if _, err := os.Stat(filepath.Join(root, derived)); !os.IsNotExist(err) {
			t.Errorf("%s should have been removed", derived)
		}
	}
	for _, preserved := range []string{"manifests", "logs"} {
		if _, err := os.Stat(filepath.Join(root, preserved)); err != nil {
			t.Errorf("%s must be preserved by default: %v", preserved, err)
		}
	}
}

func TestApertureDir_PurgeRemovesEverything(t *testing.T) {
	root := filepath.Join(t.TempDir(), ".aperture")
	for _, sub := range []string{"cache", "manifests", "logs"} {
		if err := os.MkdirAll(filepath.Join(root, sub), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	dir := ApertureDir{Root: root}
	removed, err := dir.ClearApertureDerived(true)
	if err != nil {
		t.Fatalf("ClearApertureDerived: %v", err)
	}
	if removed != 3 {
		t.Errorf("--purge should remove 3 dirs (cache + manifests + logs), got %d", removed)
	}
}

func TestApertureDir_MissingRootIsNotAnError(t *testing.T) {
	dir := ApertureDir{Root: filepath.Join(t.TempDir(), "does-not-exist")}
	removed, err := dir.ClearApertureDerived(false)
	if err != nil {
		t.Fatalf("missing .aperture/ must not be an error: %v", err)
	}
	if removed != 0 {
		t.Errorf("no dirs to remove; got %d", removed)
	}
}
