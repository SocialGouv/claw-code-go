package tools

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/SocialGouv/claw-code-go/internal/permissions"
)

// TestExecuteBashKillsOrphanGrandchild guards against the hang where a
// grandchild process inherits stdout/stderr, outlives its bash parent
// after the timeout SIGKILL, and keeps the pipes open — wedging
// cmd.Wait() forever. The Setpgid + cmd.Cancel + WaitDelay combo must
// bring ExecuteBash back well under the 32s budget.
//
// The command spawns a bash that backgrounds a long-sleeping child
// inheriting stdout, then sleeps long enough to outlast the 30s bash
// timeout if the orphan held the pipe open. Without the fix this test
// would hang past `time` 60s. With the fix it returns inside ~32s
// (30s ctx timeout + 2s WaitDelay) with a typed timeout error.
func TestExecuteBashKillsOrphanGrandchild(t *testing.T) {
	start := time.Now()
	out, err := ExecuteBash(
		context.Background(),
		map[string]any{"command": "sleep 120 & sleep 120"},
		permissions.ModeAllow, "",
	)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatalf("expected timeout error, got nil (output=%q)", out)
	}
	if !strings.Contains(err.Error(), "timed out") {
		t.Errorf("expected 'timed out' in error, got: %v", err)
	}
	// 30s timeout + 2s WaitDelay = ceiling 32s; give some slack for CI.
	if elapsed > 40*time.Second {
		t.Errorf("ExecuteBash hung past WaitDelay: took %s", elapsed)
	}
}

// TestExecuteBashHonorsCallerCancel verifies that cancelling the
// caller's context propagates to the spawned bash. Pre-fix, ctx was
// silently dropped and only the internal 30s timeout could stop a
// runaway command.
func TestExecuteBashHonorsCallerCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(200 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	_, err := ExecuteBash(
		ctx,
		map[string]any{"command": "sleep 60"},
		permissions.ModeAllow, "",
	)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected cancellation error, got nil")
	}
	// Should return within ~2s of cancel (200ms + WaitDelay margin).
	if elapsed > 5*time.Second {
		t.Errorf("ExecuteBash didn't honor caller cancel: took %s", elapsed)
	}
}

// TestBashTimeoutResolvesTheKnob covers the parse forms and the fallbacks
// without spawning anything: an unset or unparsable value must land on the
// default rather than on zero, since zero means "no bound here" and a typo
// must never silently remove the bound.
func TestBashTimeoutResolvesTheKnob(t *testing.T) {
	for _, tc := range []struct {
		name string
		env  string
		want time.Duration
	}{
		{"unset falls back to the default", "", DefaultBashTimeout},
		{"a Go duration", "15m", 15 * time.Minute},
		{"a bare number is seconds", "90", 90 * time.Second},
		{"zero leaves the caller's context as the only bound", "0", 0},
		{"a typo must not remove the bound", "15minutes", DefaultBashTimeout},
		{"empty after trimming", "   ", DefaultBashTimeout},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("CLAW_BASH_TIMEOUT", tc.env)
			if got := bashTimeout(); got != tc.want {
				t.Errorf("bashTimeout() = %s, want %s", got, tc.want)
			}
		})
	}
}

// TestExecuteBashHonorsTheConfiguredTimeout proves the knob reaches the call
// and not only the resolver. The budget is deliberately SHORTER than
// DefaultBashTimeout: a test that raised it would pass just as well with the
// env ignored, since the command would finish either way.
func TestExecuteBashHonorsTheConfiguredTimeout(t *testing.T) {
	t.Setenv("CLAW_BASH_TIMEOUT", "1s")

	start := time.Now()
	_, err := ExecuteBash(
		context.Background(),
		map[string]any{"command": "sleep 20"},
		permissions.ModeAllow, "",
	)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected a timeout error")
	}
	if !strings.Contains(err.Error(), "timed out") {
		t.Errorf("error = %v, want a timeout", err)
	}
	if !strings.Contains(err.Error(), "CLAW_BASH_TIMEOUT") {
		t.Errorf("error = %v, want it to name the way out — a bound that does not say how to raise it sends the reader to the wrong place", err)
	}
	// 1s budget + 2s WaitDelay; anything near 30s means the env was ignored.
	if elapsed > 10*time.Second {
		t.Errorf("took %s — the configured budget was not applied", elapsed)
	}
}
