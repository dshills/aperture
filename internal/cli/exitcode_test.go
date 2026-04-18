package cli

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

// Property coverage for ExitCodeError, scaffolded in response to
// TESTREC-39543A2A. The CLI relies on the wrapping behaviour of
// ExitCodeError to propagate SPEC §16 exit codes through cobra's
// RunE surface; these properties pin the invariants the rest of the
// code assumes.

// Invariant: Error() never returns the empty string so downstream
// formatters ("aperture: %s", err) never emit a bare colon.
func TestProperty_ExitCodeError_ErrorNonEmpty(t *testing.T) {
	cases := []error{
		&ExitCodeError{Code: 1, Err: fmt.Errorf("oops")},
		&ExitCodeError{Code: 2},                      // nil inner err
		&ExitCodeError{Code: 0, Err: errors.New("")}, // zero-valued inner
	}
	for _, ec := range cases {
		msg := ec.Error()
		if msg == "" {
			t.Errorf("ExitCodeError.Error() returned empty string for %+v", ec)
		}
	}
}

// Invariant: errors.As unwraps a wrapped ExitCodeError regardless of
// how many layers wrap it. The CLI uses errors.As() at the top level
// (see cli.Execute) and the threshold gates (exitCodeFailOnGaps, etc.)
// bubble through fmt.Errorf, so this must keep working across the
// stack.
func TestProperty_ExitCodeError_AsUnwrapsThroughWraps(t *testing.T) {
	original := exitErr(exitCodeBudgetUnderflow, errors.New("underflow"))
	wrapped1 := fmt.Errorf("layer1: %w", original)
	wrapped2 := fmt.Errorf("layer2: %w", wrapped1)

	var ec *ExitCodeError
	if !errors.As(wrapped2, &ec) {
		t.Fatalf("errors.As must unwrap ExitCodeError from wrap chain")
	}
	if ec.Code != exitCodeBudgetUnderflow {
		t.Errorf("code lost through wraps: got %d", ec.Code)
	}
}

// Invariant: exitErr always returns an *ExitCodeError carrying the
// given code, even when the inner error is nil. This protects the
// runPlan/runRun paths that construct errors from bare messages.
func TestProperty_ExitErr_AlwaysCarriesCode(t *testing.T) {
	codes := []int{
		exitCodeInternal,
		exitCodeBadArgs,
		exitCodeBadTask,
		exitCodeBadRepo,
		exitCodeBadConfig,
		exitCodeBadManifest,
		exitCodeFeasibilityBelow,
		exitCodeFailOnGaps,
		exitCodeBudgetUnderflow,
		exitCodeTokenizerMissing,
		exitCodeUnknownAgent,
		exitCodeAdapterPreExecFail,
	}
	for _, code := range codes {
		err := exitErr(code, fmt.Errorf("reason"))
		var ec *ExitCodeError
		if !errors.As(err, &ec) {
			t.Fatalf("exitErr(%d, ...) should be an *ExitCodeError", code)
		}
		if ec.Code != code {
			t.Errorf("exitErr(%d, ...) carried code %d", code, ec.Code)
		}
		if ec.Err == nil {
			t.Errorf("exitErr(%d, non-nil) dropped inner error", code)
		}
	}
}

// Invariant: the Error() rendering must include the inner message when
// one exists so users see actionable context ("read task: permission
// denied"), not just "exit 3".
func TestProperty_ExitCodeError_IncludesInnerMessage(t *testing.T) {
	inner := errors.New("can't read the file")
	ec := &ExitCodeError{Code: exitCodeBadTask, Err: inner}
	if !strings.Contains(ec.Error(), "can't read the file") {
		t.Errorf("Error() should surface inner message; got %q", ec.Error())
	}
}
