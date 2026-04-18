package agent

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/dshills/aperture/internal/config"
)

// fakeClaudePath returns the absolute path to the testdata/bin/fake-claude.sh
// stub that the agent test suite uses instead of a real claude binary.
func fakeClaudePath(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fake-claude.sh is a POSIX shell script; skip on Windows")
	}
	p, err := filepath.Abs("../../testdata/bin/fake-claude.sh")
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// newPromptFile writes a minimal merged prompt fixture and returns the
// path so the fake claude has something to cat.
func newPromptFile(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "run-apt_test.md")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestClaudeAdapter_PipesPromptOnStdin(t *testing.T) {
	stub := fakeClaudePath(t)
	prompt := newPromptFile(t, "HELLO-MERGED-PROMPT-BODY\n")

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	req := RunRequest{
		ManifestJSONPath: "/tmp/manifest.json",
		PromptPath:       prompt,
		RepoRoot:         "/repo",
		ManifestHash:     "deadbeef",
		ApertureVersion:  "1.0.0-test",
		AgentConfig:      config.AgentEntry{Command: stub},
		Stdout:           stdout,
		Stderr:           stderr,
	}
	code, err := (&claudeAdapter{}).Invoke(context.Background(), req)
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if code != 0 {
		t.Fatalf("exit code: got %d want 0", code)
	}
	if !strings.Contains(stdout.String(), "HELLO-MERGED-PROMPT-BODY") {
		t.Errorf("stdin body not piped through to stdout:\n%s", stdout.String())
	}
	if !strings.Contains(stdout.String(), "APERTURE_MANIFEST_PATH=/tmp/manifest.json") {
		t.Errorf("APERTURE_MANIFEST_PATH not set in adapter env:\n%s", stdout.String())
	}
	if !strings.Contains(stdout.String(), "APERTURE_PROMPT_PATH="+prompt) {
		t.Errorf("APERTURE_PROMPT_PATH not set correctly:\n%s", stdout.String())
	}
	// --print and --permission-mode are injected by the non-interactive
	// default path.
	if !strings.Contains(stdout.String(), "--print") {
		t.Errorf("expected --print in adapter args:\n%s", stdout.String())
	}
}

func TestClaudeAdapter_InteractiveModePassesPromptAsArg(t *testing.T) {
	stub := fakeClaudePath(t)
	prompt := newPromptFile(t, "INTERACTIVE-BODY")

	stdout := &bytes.Buffer{}
	req := RunRequest{
		PromptPath:      prompt,
		ApertureVersion: "1.0.0-test",
		AgentConfig:     config.AgentEntry{Command: stub, Mode: "interactive"},
		Stdout:          stdout,
		Stderr:          &bytes.Buffer{},
	}
	code, err := (&claudeAdapter{}).Invoke(context.Background(), req)
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if code != 0 {
		t.Fatalf("exit code: got %d want 0", code)
	}
	if !strings.Contains(stdout.String(), "args: INTERACTIVE-BODY") {
		t.Errorf("interactive mode should pass prompt as arg:\n%s", stdout.String())
	}
	// stdin was NOT piped in interactive mode, so cat reads an empty line.
	if strings.Contains(stdout.String(), "INTERACTIVE-BODY\nstdin_end") {
		t.Errorf("interactive mode must not also pipe prompt on stdin:\n%s", stdout.String())
	}
}

func TestClaudeAdapter_PropagatesNonZeroExit(t *testing.T) {
	stub := fakeClaudePath(t)
	prompt := newPromptFile(t, "x")

	stdout := &bytes.Buffer{}
	req := RunRequest{
		PromptPath:  prompt,
		AgentConfig: config.AgentEntry{Command: stub, Env: map[string]string{"APERTURE_EXIT": "7"}},
		Stdout:      stdout,
		Stderr:      &bytes.Buffer{},
	}
	code, err := (&claudeAdapter{}).Invoke(context.Background(), req)
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if code != 7 {
		t.Fatalf("exit code: got %d want 7", code)
	}
}

func TestClaudeAdapter_PreExecFailReturnsError(t *testing.T) {
	stdout := &bytes.Buffer{}
	req := RunRequest{
		AgentConfig: config.AgentEntry{Command: "/nonexistent/path/that/should/not/exist"},
		Stdout:      stdout,
		Stderr:      &bytes.Buffer{},
	}
	code, err := (&claudeAdapter{}).Invoke(context.Background(), req)
	if err == nil {
		t.Fatalf("expected error on missing command, got code=%d", code)
	}
}

func TestResolve_UnknownAgentIsFalse(t *testing.T) {
	_, _, ok := Resolve("no-such", map[string]config.AgentEntry{})
	if ok {
		t.Fatal("unknown agent must resolve to !ok")
	}
}

func TestResolve_BuiltInClaudeAndCustomFallthrough(t *testing.T) {
	agents := map[string]config.AgentEntry{
		"claude":  {Command: "claude"},
		"my-tool": {Command: "tool"},
	}
	claude, _, ok := Resolve("claude", agents)
	if !ok {
		t.Fatal("claude should resolve")
	}
	if _, isClaude := claude.(*claudeAdapter); !isClaude {
		t.Fatalf("claude must resolve to claudeAdapter, got %T", claude)
	}
	custom, _, ok := Resolve("my-tool", agents)
	if !ok {
		t.Fatal("my-tool should resolve")
	}
	if _, isCustom := custom.(*customAdapter); !isCustom {
		t.Fatalf("unknown named agent must resolve to customAdapter, got %T", custom)
	}
}

func TestApertureEnv_ExportsAllFields(t *testing.T) {
	req := RunRequest{
		ManifestJSONPath:     "/a.json",
		ManifestMarkdownPath: "/a.md",
		TaskPath:             "/t.txt",
		PromptPath:           "/p.md",
		RepoRoot:             "/r",
		ManifestHash:         "abc",
		ApertureVersion:      "1.0.0",
		AgentConfig:          config.AgentEntry{Env: map[string]string{"X": "Y"}},
	}
	env := apertureEnv(req)
	want := []string{
		"APERTURE_MANIFEST_PATH=/a.json",
		"APERTURE_MANIFEST_MARKDOWN_PATH=/a.md",
		"APERTURE_TASK_PATH=/t.txt",
		"APERTURE_PROMPT_PATH=/p.md",
		"APERTURE_REPO_ROOT=/r",
		"APERTURE_MANIFEST_HASH=abc",
		"APERTURE_VERSION=1.0.0",
		"X=Y",
	}
	for _, w := range want {
		if !slicesContains(env, w) {
			t.Errorf("missing env entry %q in %v", w, env)
		}
	}
}

func TestWriteInlineTaskFile_CleanupRemovesFile(t *testing.T) {
	path, cleanup, err := WriteInlineTaskFile("apt_testid", "hello body")
	if err != nil {
		t.Fatalf("WriteInlineTaskFile: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("tempfile should exist: %v", err)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "hello body" {
		t.Fatalf("body mismatch: %q", body)
	}
	cleanup()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("cleanup should remove tempfile; got err=%v", err)
	}
}

func TestSweepOrphanTempfiles_RemovesOldLeavesNew(t *testing.T) {
	// Build our own TMPDIR scoped to this test so we don't touch the
	// real temp directory's contents.
	dir := t.TempDir()
	t.Setenv("TMPDIR", dir)

	oldPath := filepath.Join(dir, tempfilePrefix+"stale_id.txt")
	freshPath := filepath.Join(dir, tempfilePrefix+"fresh_id.txt")
	unrelatedPath := filepath.Join(dir, "not-ours.txt")
	if err := os.WriteFile(oldPath, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(freshPath, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(unrelatedPath, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	oldMtime := time.Now().Add(-48 * time.Hour)
	if err := os.Chtimes(oldPath, oldMtime, oldMtime); err != nil {
		t.Fatal(err)
	}

	SweepOrphanTempfiles()

	if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
		t.Errorf("old tempfile should have been swept; err=%v", err)
	}
	if _, err := os.Stat(freshPath); err != nil {
		t.Errorf("fresh tempfile must be preserved: %v", err)
	}
	if _, err := os.Stat(unrelatedPath); err != nil {
		t.Errorf("unrelated file must NOT be swept: %v", err)
	}
}

// slicesContains is a tiny local helper because `slices` isn't imported
// in this file and we only need a single check.
func slicesContains(haystack []string, needle string) bool {
	for _, h := range haystack {
		if h == needle {
			return true
		}
	}
	return false
}
