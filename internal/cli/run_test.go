package cli

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// fakeClaudeConfigFile writes a .aperture.yaml that points the `claude`
// agent at the testdata/bin/fake-claude.sh stub and returns the path.
// Optional extra env is merged into the agent block so tests can force
// a non-zero exit via APERTURE_EXIT.
func fakeClaudeConfigFile(t *testing.T, extraEnv map[string]string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("run tests exercise POSIX shell script stubs")
	}
	stub, err := filepath.Abs("../../testdata/bin/fake-claude.sh")
	if err != nil {
		t.Fatal(err)
	}
	cfg := "version: 1\n" +
		"defaults:\n  model: claude-sonnet\n  budget: 120000\n" +
		"agents:\n  claude:\n    command: " + stub + "\n    pass_task_as_arg: false\n"
	if len(extraEnv) > 0 {
		cfg += "    env:\n"
		for k, v := range extraEnv {
			cfg += "      " + k + ": " + v + "\n"
		}
	}
	path := filepath.Join(t.TempDir(), "aperture.yaml")
	if err := os.WriteFile(path, []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// fixtureRepo returns the absolute small_go fixture path.
func fixtureRepo(t *testing.T) string {
	t.Helper()
	p, err := filepath.Abs("../../testdata/fixtures/small_go")
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestRun_InvokesFakeAdapter(t *testing.T) {
	cfgPath := fakeClaudeConfigFile(t, nil)
	outDir := t.TempDir()

	err := runRun(context.Background(), []string{"claude"}, runFlags{
		repo:       fixtureRepo(t),
		inline:     "add refresh handling to internal/oauth/provider.go for github oauth",
		configPath: cfgPath,
		outDir:     outDir,
	})
	if err != nil {
		t.Fatalf("runRun: %v", err)
	}
	// The manifest pair and merged prompt must have been persisted.
	entries, err := os.ReadDir(outDir)
	if err != nil {
		t.Fatal(err)
	}
	var sawJSON, sawMD, sawPrompt bool
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
	if !sawJSON {
		t.Error("run should persist manifest-<hash>.json")
	}
	if !sawMD {
		t.Error("run should persist manifest-<hash>.md")
	}
	if !sawPrompt {
		t.Error("run should persist run-<manifest_id>.md merged prompt")
	}
}

// §16 row 11: unknown agent → exit 11.
func TestRun_UnknownAgentExit11(t *testing.T) {
	err := runRun(context.Background(), []string{"no-such-agent"}, runFlags{
		repo:   fixtureRepo(t),
		inline: "x",
		outDir: t.TempDir(),
	})
	if err == nil {
		t.Fatal("expected ExitCodeError, got nil")
	}
	var ec *ExitCodeError
	if !errors.As(err, &ec) {
		t.Fatalf("expected ExitCodeError, got %T: %v", err, err)
	}
	if ec.Code != exitCodeUnknownAgent {
		t.Fatalf("expected exit 11, got %d", ec.Code)
	}
}

// §16 row 12: adapter command missing → exit 12.
func TestRun_PreExecFailExit12(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("pre-exec test uses POSIX exec semantics")
	}
	cfg := "version: 1\n" +
		"defaults:\n  model: claude-sonnet\n  budget: 120000\n" +
		"agents:\n  claude:\n    command: /nonexistent/cmd/that/should/not/exist\n    pass_task_as_arg: false\n"
	cfgPath := filepath.Join(t.TempDir(), "aperture.yaml")
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	err := runRun(context.Background(), []string{"claude"}, runFlags{
		repo:       fixtureRepo(t),
		inline:     "add refresh handling to internal/oauth/provider.go for github oauth",
		configPath: cfgPath,
		outDir:     t.TempDir(),
	})
	if err == nil {
		t.Fatal("expected pre-exec failure error")
	}
	var ec *ExitCodeError
	if !errors.As(err, &ec) {
		t.Fatalf("expected ExitCodeError, got %T: %v", err, err)
	}
	if ec.Code != exitCodeAdapterPreExecFail {
		t.Fatalf("expected exit 12, got %d", ec.Code)
	}
}

// §7.10.4.1: adapter ran and exited non-zero → Aperture exits with the
// adapter's code, verbatim.
func TestRun_PropagatesAdapterExit(t *testing.T) {
	cfgPath := fakeClaudeConfigFile(t, map[string]string{"APERTURE_EXIT": "7"})
	err := runRun(context.Background(), []string{"claude"}, runFlags{
		repo:       fixtureRepo(t),
		inline:     "add refresh handling to internal/oauth/provider.go for github oauth",
		configPath: cfgPath,
		outDir:     t.TempDir(),
	})
	if err == nil {
		t.Fatal("expected non-zero exit from adapter stub")
	}
	var ec *ExitCodeError
	if !errors.As(err, &ec) {
		t.Fatalf("expected ExitCodeError, got %T: %v", err, err)
	}
	if ec.Code != 7 {
		t.Fatalf("expected adapter exit 7 to propagate, got %d", ec.Code)
	}
}
