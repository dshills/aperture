package repo

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

// ExclusionReason explains why a given path was excluded from indexing.
// Values mirror the enum in schema/manifest.v1.json.
type ExclusionReason string

const (
	ExcludeDefaultPattern ExclusionReason = "default_pattern"
	ExcludeUserPattern    ExclusionReason = "user_pattern"
	ExcludeBinary         ExclusionReason = "binary"
	ExcludeOversize       ExclusionReason = "oversize_cutoff"
	ExcludeHiddenDir      ExclusionReason = "hidden_dir"
	ExcludeSymlink        ExclusionReason = "symlink"
	ExcludeReadError      ExclusionReason = "read_error"
)

// sizeCutoff is the SPEC §7.4.3 10 MiB per-file cap.
const sizeCutoff int64 = 10 * 1024 * 1024

// binaryProbeBytes controls how many leading bytes are scanned for NUL
// bytes during binary detection (SPEC §7.4.3).
const binaryProbeBytes = 8 * 1024

// hiddenAllowList enumerates the dot-prefixed directories that v1
// indexes rather than excluding as `hidden_dir` (SPEC §7.4.3).
var hiddenAllowList = map[string]struct{}{
	".github":        {},
	".cursor":        {},
	".claude":        {},
	".aperture.yaml": {},
}

// FileEntry is a scanned file ready for indexing. Paths are normalized to
// forward slashes and are relative to the repo root.
type FileEntry struct {
	Path      string
	Size      int64
	SHA256    string
	MTime     string // RFC 3339 UTC
	Extension string // lowercased with the leading dot
	Language  string // derived from extension; "" for unknown
}

// Exclusion records a single filtered path along with its reason.
type Exclusion struct {
	Path   string
	Reason ExclusionReason
}

// WalkOptions controls the Walk function.
type WalkOptions struct {
	// Root is an absolute path to the repo root.
	Root string
	// DefaultPatterns are the v1 default glob exclusions (SPEC §7.4.3).
	DefaultPatterns []string
	// UserPatterns are the config-provided extra exclusions (post-union).
	// Matching a user pattern takes precedence over matching a default
	// pattern for reporting purposes.
	UserPatterns []string
}

// WalkResult bundles the walker's outputs. Files and Exclusions are both
// sorted ascending by normalized path for deterministic downstream ordering.
type WalkResult struct {
	Files      []FileEntry
	Exclusions []Exclusion
}

// Walk recursively descends from opts.Root, applying the §7.4.3 exclusion
// rules and returning a deterministic, sorted WalkResult. Each included
// file's SHA-256 and mtime are recorded inline so later stages (fingerprint,
// cache) do not need a second pass.
func Walk(opts WalkOptions) (WalkResult, error) {
	if opts.Root == "" {
		return WalkResult{}, fmt.Errorf("repo.Walk: Root is required")
	}

	defaultSet := compileGlobs(opts.DefaultPatterns)
	userSet := compileGlobs(opts.UserPatterns)

	files := make([]FileEntry, 0, 256)
	excl := make([]Exclusion, 0, 64)

	err := filepath.WalkDir(opts.Root, func(p string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			// Unreadable directory or file: skip it and log an exclusion
			// with the read_error reason so the condition is surfaced in
			// the manifest without aborting the entire walk.
			rel := normalizeRel(opts.Root, p)
			if rel != "." {
				excl = append(excl, Exclusion{Path: rel, Reason: ExcludeReadError})
			}
			if d != nil && d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if p == opts.Root {
			return nil
		}

		rel := normalizeRel(opts.Root, p)

		// Symlinks are not followed. Record them as excluded with reason
		// "symlink" (§7.4.3 extension).
		if d.Type()&os.ModeSymlink != 0 {
			excl = append(excl, Exclusion{Path: rel, Reason: ExcludeSymlink})
			return nil
		}

		if d.IsDir() {
			if reason, skip := shouldSkipDir(rel, d.Name(), defaultSet, userSet); skip {
				excl = append(excl, Exclusion{Path: rel + "/", Reason: reason})
				return filepath.SkipDir
			}
			return nil
		}

		if reason, skip := matchAnyPattern(rel, defaultSet, userSet); skip {
			excl = append(excl, Exclusion{Path: rel, Reason: reason})
			return nil
		}

		fi, err := d.Info()
		if err != nil {
			excl = append(excl, Exclusion{Path: rel, Reason: ExcludeReadError})
			return nil
		}
		if fi.Size() > sizeCutoff {
			excl = append(excl, Exclusion{Path: rel, Reason: ExcludeOversize})
			return nil
		}

		hash, isBinary, err := hashAndProbe(p)
		if err != nil {
			excl = append(excl, Exclusion{Path: rel, Reason: ExcludeReadError})
			return nil
		}
		if isBinary {
			excl = append(excl, Exclusion{Path: rel, Reason: ExcludeBinary})
			return nil
		}

		ext := strings.ToLower(path.Ext(rel))
		files = append(files, FileEntry{
			Path:      rel,
			Size:      fi.Size(),
			SHA256:    hash,
			MTime:     mtimeRFC3339UTC(fi),
			Extension: ext,
			Language:  languageForExt(ext, path.Base(rel)),
		})
		return nil
	})
	if err != nil {
		return WalkResult{}, fmt.Errorf("walk %q: %w", opts.Root, err)
	}

	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	sort.Slice(excl, func(i, j int) bool {
		if excl[i].Path == excl[j].Path {
			return excl[i].Reason < excl[j].Reason
		}
		return excl[i].Path < excl[j].Path
	})
	return WalkResult{Files: files, Exclusions: excl}, nil
}

// normalizeRel returns rel relative to root with forward-slash separators
// and no leading "./" — §14 determinism contract.
func normalizeRel(root, p string) string {
	rel, err := filepath.Rel(root, p)
	if err != nil {
		return filepath.ToSlash(p)
	}
	return filepath.ToSlash(rel)
}

func shouldSkipDir(rel, name string, defaults, user []compiledGlob) (ExclusionReason, bool) {
	if strings.HasPrefix(name, ".") {
		if _, ok := hiddenAllowList[name]; !ok {
			return ExcludeHiddenDir, true
		}
	}
	if reason, skip := matchAnyPattern(rel+"/", defaults, user); skip {
		return reason, true
	}
	return "", false
}

// matchAnyPattern returns the first matching exclusion reason. User
// patterns win over default patterns when both match — the manifest then
// attributes the exclusion to the user's configuration.
func matchAnyPattern(rel string, defaults, user []compiledGlob) (ExclusionReason, bool) {
	if matchAny(rel, user) {
		return ExcludeUserPattern, true
	}
	if matchAny(rel, defaults) {
		return ExcludeDefaultPattern, true
	}
	return "", false
}

func hashAndProbe(path string) (hashHex string, isBinary bool, err error) {
	f, err := os.Open(path) //nolint:gosec // walker-resolved path
	if err != nil {
		return "", false, err
	}
	defer func() { _ = f.Close() }()

	h := sha256.New()
	buf := make([]byte, 64*1024)
	var probed bool
	for {
		n, readErr := f.Read(buf)
		if n > 0 {
			if !probed {
				probe := buf[:n]
				if len(probe) > binaryProbeBytes {
					probe = probe[:binaryProbeBytes]
				}
				if bytes.IndexByte(probe, 0) >= 0 {
					return "", true, nil
				}
				probed = true
			}
			if _, werr := h.Write(buf[:n]); werr != nil {
				return "", false, werr
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return "", false, readErr
		}
	}
	return hex.EncodeToString(h.Sum(nil)), false, nil
}

// languageForExt returns a coarse language hint. Empty string for unknown
// extensions so callers can treat "" as "no hint".
func languageForExt(ext, base string) string {
	lowBase := strings.ToLower(base)
	switch lowBase {
	case "makefile", "dockerfile":
		return lowBase
	case "go.mod", "go.sum":
		return "go"
	}
	switch ext {
	case ".go":
		return "go"
	case ".md", ".markdown", ".mdx":
		return "markdown"
	case ".rst":
		return "rst"
	case ".adoc":
		return "asciidoc"
	case ".yaml", ".yml":
		return "yaml"
	case ".json":
		return "json"
	case ".toml":
		return "toml"
	case ".proto":
		return "proto"
	case ".sql":
		return "sql"
	case ".ts", ".tsx":
		return "typescript"
	case ".js", ".jsx", ".mjs", ".cjs":
		return "javascript"
	case ".py":
		return "python"
	case ".sh", ".bash", ".zsh":
		return "shell"
	case ".mk":
		return "make"
	}
	return ""
}
