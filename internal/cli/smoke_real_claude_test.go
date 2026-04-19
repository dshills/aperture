//go:build smoke

package cli

import (
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestSmoke_RealClaudeAdapter drives `aperture run claude` against the
// small_go fixture with the real `claude` CLI on PATH. It is opt-in —
// build tag `smoke` gates it out of the default `go test ./...` run
// because it hits the network, consumes tokens, and depends on the
// user being authenticated to Claude Code.
//
// Invoke with:  go test -tags smoke -run TestSmoke_RealClaudeAdapter -v ./internal/cli
//
// The fixture is copied into a TempDir so a model that ignores the
// read-only instruction cannot mutate committed fixture files — the
// claude adapter runs with --permission-mode bypassPermissions.
func TestSmoke_RealClaudeAdapter(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("smoke test uses POSIX copy semantics")
	}
	if _, err := exec.LookPath("claude"); err != nil {
		t.Skip("claude CLI not on PATH; skipping real-agent smoke test")
	}

	repo := copyFixture(t, fixtureRepo(t))
	outDir := t.TempDir()

	// Minimal config: the `claude` agent must exist in the resolved
	// agents map for Resolve() to return the built-in adapter.
	cfg := "version: 1\n" +
		"defaults:\n  model: claude-sonnet\n  budget: 120000\n" +
		"agents:\n  claude:\n    command: claude\n"
	cfgPath := filepath.Join(t.TempDir(), "aperture.yaml")
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}

	// Cheap, read-only prompt. The adapter writes claude's response to
	// os.Stdout; run with `go test -v` to eyeball it.
	prompt := "Do not read, create, or modify any files. " +
		"Respond with exactly one line: APERTURE_SMOKE_OK"

	err := runRun(context.Background(), []string{"claude"}, runFlags{
		repo:       repo,
		inline:     prompt,
		configPath: cfgPath,
		outDir:     outDir,
	})
	if err != nil {
		t.Fatalf("runRun against real claude: %v", err)
	}

	// Pipeline artifacts must be persisted regardless of model output.
	var sawJSON, sawMD, sawPrompt bool
	entries, err := os.ReadDir(outDir)
	if err != nil {
		t.Fatalf("read outDir: %v", err)
	}
	for _, e := range entries {
		switch {
		case strings.HasPrefix(e.Name(), "manifest-") && strings.HasSuffix(e.Name(), ".json"):
			sawJSON = true
		case strings.HasPrefix(e.Name(), "manifest-") && strings.HasSuffix(e.Name(), ".md"):
			sawMD = true
		case strings.HasPrefix(e.Name(), "run-") && strings.HasSuffix(e.Name(), ".md"):
			sawPrompt = true
		}
	}
	if !sawJSON || !sawMD || !sawPrompt {
		t.Errorf("missing persisted artifacts: json=%v md=%v prompt=%v", sawJSON, sawMD, sawPrompt)
	}
}

// copyFixture recursively copies src into a new TempDir and returns
// the destination root. Preserves file modes; skips the fixture's own
// .aperture/ cache so the run starts cold.
func copyFixture(t *testing.T, src string) string {
	t.Helper()
	dst := t.TempDir()
	err := filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		if rel == ".aperture" || strings.HasPrefix(rel, ".aperture"+string(filepath.Separator)) {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, info.Mode())
		}
		in, err := os.Open(path) //nolint:gosec // fixture path under test control
		if err != nil {
			return err
		}
		defer in.Close()
		out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, info.Mode())
		if err != nil {
			return err
		}
		defer out.Close()
		_, err = io.Copy(out, in)
		return err
	})
	if err != nil {
		t.Fatalf("copy fixture: %v", err)
	}
	return dst
}
