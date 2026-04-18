// Package cache persists per-file AST analysis results so repeated
// `aperture plan` invocations on the same tree skip the expensive parse
// phase. The on-disk format is JSON per §7.11.3; layout is one sidecar
// file per cache key under .aperture/cache/.
package cache

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"

	"github.com/dshills/aperture/internal/index"
)

// SchemaVersion identifies the on-disk format. Bump whenever the Entry
// struct changes shape in a way that would produce garbage on read by an
// older binary. A mismatch triggers a full .aperture/cache/ wipe.
const SchemaVersion = "cache-v1"

// Entry is the cached form of a single file's AST analysis. It mirrors
// the subset of index.FileEntry that goanalysis populates; non-AST
// fields (Size, SHA256, MTime, Extension, Language) stay on the walker
// side because they're recomputed per run anyway.
type Entry struct {
	SchemaVersion string `json:"cache_schema_version"`
	ToolVersion   string `json:"tool_version"`
	Path          string `json:"path"`
	Size          int64  `json:"size"`
	MTime         string `json:"mtime"`
	SHA256        string `json:"sha256"`

	PackageName string         `json:"package_name,omitempty"`
	Imports     []string       `json:"imports,omitempty"`
	Symbols     []index.Symbol `json:"symbols,omitempty"`
	SideEffects []string       `json:"side_effects,omitempty"`
	ParseError  bool           `json:"parse_error,omitempty"`
}

// Key derives the per-file cache-key hash per §7.11.2:
// sha256(path + "\x00" + size + "\x00" + mtime + "\x00" + tool_version).
// Returns the hex digest used as the cache-file basename.
func Key(path string, size int64, mtime, toolVersion string) string {
	h := sha256.New()
	h.Write([]byte(path))
	h.Write([]byte{0})
	_, _ = fmt.Fprintf(h, "%d", size)
	h.Write([]byte{0})
	h.Write([]byte(mtime))
	h.Write([]byte{0})
	h.Write([]byte(toolVersion))
	return hex.EncodeToString(h.Sum(nil))
}

// Cache is the filesystem-backed KV store. A zero Cache with a Dir set
// is fully usable; no constructor is strictly required but New wraps
// the common setup.
type Cache struct {
	// Dir is the absolute path to the cache root (typically .aperture/
	// cache/). Created lazily on first write via initDir.
	Dir string
	// ToolVersion is baked into every key + entry so a version bump
	// invalidates everything.
	ToolVersion string

	initOnce sync.Once
	initErr  error
	// dirReady is toggled true by initDir after a successful MkdirAll;
	// Clear() toggles it false so the next Put re-creates the directory
	// exactly once. Skipping MkdirAll on every Put matters at scale:
	// 50 000-file repos would otherwise issue 50 000 redundant
	// directory-probing syscalls during cold-cache runs.
	dirReady atomic.Bool
}

// New constructs a Cache rooted at dir with the given tool version.
func New(dir, toolVersion string) *Cache {
	return &Cache{Dir: dir, ToolVersion: toolVersion}
}

// ErrMiss is returned by Get when no cache entry exists for the key or
// the entry is no longer valid (wrong schema, wrong tool version).
var ErrMiss = errors.New("cache miss")

// Get returns the cached Entry for key if one exists and is valid. A
// missing or version-mismatched entry returns ErrMiss; corruption
// (unreadable JSON) also returns ErrMiss after removing the bad file.
func (c *Cache) Get(key string) (*Entry, error) {
	if c == nil || c.Dir == "" {
		return nil, ErrMiss
	}
	path := filepath.Join(c.Dir, key+".json")
	b, err := os.ReadFile(path) //nolint:gosec // cache dir under our control
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrMiss
		}
		return nil, fmt.Errorf("read cache %s: %w", path, err)
	}
	var e Entry
	if err := json.Unmarshal(b, &e); err != nil {
		slog.Info("cache entry corrupt, removing", "path", path, "error", err.Error())
		_ = os.Remove(path)
		return nil, ErrMiss
	}
	if e.SchemaVersion != SchemaVersion {
		// A single-entry schema mismatch means the whole cache is from
		// a different Aperture version; the caller's InvalidateAll path
		// will wipe the directory on the next operation.
		slog.Info("cache entry schema mismatch",
			"path", path, "expected", SchemaVersion, "got", e.SchemaVersion)
		return nil, ErrMiss
	}
	if e.ToolVersion != c.ToolVersion {
		slog.Debug("cache entry tool-version mismatch, removing",
			"path", path, "expected", c.ToolVersion, "got", e.ToolVersion)
		// Remove the stale entry so repeated tool-version churn doesn't
		// accumulate orphaned JSON files under .aperture/cache/. Failure
		// is logged and swallowed — the cache miss result is all the
		// caller actually needs to proceed.
		if rmErr := os.Remove(path); rmErr != nil && !os.IsNotExist(rmErr) {
			slog.Debug("failed to remove stale cache entry", "path", path, "error", rmErr.Error())
		}
		return nil, ErrMiss
	}
	return &e, nil
}

// initDir performs per-Cache setup: ensure the cache directory exists
// and stamp the VERSION file. Both syscalls are gated so they run at
// most once per Cache instance between Clear() calls.
//
// dirReady is an atomic.Bool that Clear() resets to false — so the
// next Put after invalidation re-creates the directory, but
// steady-state Puts (the hot path on 5 000-file warm runs) skip
// MkdirAll entirely. The VERSION stamp is gated separately by
// sync.Once and is re-armed by Clear() via an initOnce reset so it
// stamps at most once per lifecycle.
func (c *Cache) initDir() error {
	if !c.dirReady.Load() {
		if err := os.MkdirAll(c.Dir, 0o755); err != nil {
			return fmt.Errorf("create cache dir: %w", err)
		}
		c.dirReady.Store(true)
	}
	c.initOnce.Do(func() {
		if err := c.WriteVersionStamp(); err != nil {
			c.initErr = fmt.Errorf("write cache VERSION: %w", err)
		}
	})
	return c.initErr
}

// Put writes e to the cache under key. Creates the cache directory on
// first write and stamps VERSION once via sync.Once so every subsequent
// Put skips both syscalls. Uses os.CreateTemp for the intermediate file
// so concurrent Aperture invocations on the same repo don't collide on
// a shared ".tmp" path.
func (c *Cache) Put(key string, e *Entry) error {
	if c == nil || c.Dir == "" {
		return errors.New("cache.Put: nil cache or empty dir")
	}
	if err := c.initDir(); err != nil {
		return err
	}
	e.SchemaVersion = SchemaVersion
	e.ToolVersion = c.ToolVersion
	b, err := json.Marshal(e)
	if err != nil {
		return fmt.Errorf("marshal cache entry: %w", err)
	}
	tmpFile, err := os.CreateTemp(c.Dir, key+".*.json.tmp")
	if err != nil {
		return fmt.Errorf("create cache tempfile: %w", err)
	}
	tmp := tmpFile.Name()
	// Defer cleanup so any early return (write failure, close failure,
	// rename failure) or panic removes the tempfile. renamed is flipped
	// to true on the successful rename path so the defer becomes a
	// no-op when the file has been atomically published.
	renamed := false
	defer func() {
		if !renamed {
			_ = os.Remove(tmp)
		}
	}()
	if _, writeErr := tmpFile.Write(b); writeErr != nil {
		_ = tmpFile.Close()
		return fmt.Errorf("write cache tempfile: %w", writeErr)
	}
	// We deliberately do NOT fsync: this is a cache, and the cost of
	// an fsync per file on a 5 000-file cold run is measured in tens of
	// seconds. A crash between rename and flush means losing the NEW
	// entry — which re-analyzes on the next run, the exact scenario a
	// cache is designed to tolerate. The atomic tempfile+rename still
	// protects against readers seeing a partial file.
	if closeErr := tmpFile.Close(); closeErr != nil {
		return fmt.Errorf("close cache tempfile: %w", closeErr)
	}
	path := filepath.Join(c.Dir, key+".json")
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("rename cache tempfile: %w", err)
	}
	renamed = true
	return nil
}

// Clear removes every entry under Dir. Missing Dir is not an error.
// Resets the init gate so the next Put re-creates the directory AND
// re-stamps VERSION — otherwise a post-Clear Put would skip the
// stamp because sync.Once already fired, leaving DetectSchemaDrift
// unable to short-circuit via the VERSION file on the next run.
func (c *Cache) Clear() error {
	if c == nil || c.Dir == "" {
		return nil
	}
	if err := os.RemoveAll(c.Dir); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("clear cache %s: %w", c.Dir, err)
	}
	c.initOnce = sync.Once{}
	c.initErr = nil
	c.dirReady.Store(false)
	return nil
}
