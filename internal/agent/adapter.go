// Package agent implements the §7.10 coding-agent adapter layer. Each
// Adapter wraps a downstream CLI (claude, codex, or a user-declared one)
// and bridges the materialized manifest/prompt/task files into that CLI's
// startup conventions.
package agent

import (
	"context"
	"io"

	"github.com/dshills/aperture/internal/config"
)

// RunRequest is the §7.10.4.1 contract passed to every Adapter.Invoke
// call. All paths are absolute so the adapter doesn't need to resolve
// them against a working directory of its own.
type RunRequest struct {
	// ManifestJSONPath is the absolute path to the JSON manifest,
	// persisted under output.directory so the run is auditable.
	ManifestJSONPath string
	// ManifestMarkdownPath is the absolute path to the Markdown manifest
	// when written; empty when --format=json was used without an explicit
	// markdown counterpart.
	ManifestMarkdownPath string
	// TaskPath is the absolute path to the resolved task file. For
	// inline tasks this is the tempfile the CLI created and will clean
	// up after Invoke returns; for file-supplied tasks it is the
	// original path, not to be deleted.
	TaskPath string
	// PromptPath is the absolute path to the merged prompt file produced
	// by built-in adapters (markdown manifest + "---" + task text). Empty
	// for custom adapters that don't materialize a merged prompt.
	PromptPath string
	// RepoRoot is the absolute path to the repository root used for
	// planning.
	RepoRoot string
	// ManifestHash is the hex sha256 from §7.9.4 (i.e. the digits after
	// the `sha256:` prefix of the manifest's manifest_hash field).
	ManifestHash string
	// ApertureVersion is the current binary's semver/`dev` identity.
	ApertureVersion string
	// AgentConfig is the resolved §9.1.2 entry for this agent — the
	// command, args, pass_task_as_arg flag, mode, and extra env.
	AgentConfig config.AgentEntry
	// Stdout / Stderr are the passthrough streams (§7.10.4.1 step 4 —
	// "do not capture"). Invoke writes directly to them.
	Stdout io.Writer
	Stderr io.Writer
	// Stdin is the optional input stream to feed the adapter on its
	// standard input. Non-interactive claude piping passes the merged
	// prompt via this field.
	Stdin io.Reader
}

// Adapter is the agent-integration interface. Implementations translate
// a RunRequest into a subprocess invocation and return the adapter's
// OS exit code. Pre-exec failures (executable missing, permission denied)
// must be returned via err so the CLI can map them to exit code 12;
// successful starts that exit non-zero must be returned as (code, nil)
// so the CLI propagates them verbatim.
type Adapter interface {
	Invoke(ctx context.Context, req RunRequest) (exitCode int, err error)
}

// Resolve returns the Adapter implementation for the named agent per the
// §9.1.2 resolved agents map. Built-in names ("claude", "codex") map to
// the bundled implementations; everything else falls through to the
// custom-adapter wrapper. An empty name or one not present in the map
// returns (nil, false) so the CLI can map to exit code 11.
func Resolve(name string, agents map[string]config.AgentEntry) (Adapter, config.AgentEntry, bool) {
	if name == "" {
		return nil, config.AgentEntry{}, false
	}
	entry, ok := agents[name]
	if !ok {
		return nil, config.AgentEntry{}, false
	}
	switch name {
	case "claude":
		return &claudeAdapter{}, entry, true
	case "codex":
		return &codexAdapter{}, entry, true
	}
	return &customAdapter{}, entry, true
}
