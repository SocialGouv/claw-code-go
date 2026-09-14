package tools

import (
	"context"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/SocialGouv/claw-code-go/internal/permissions"
)

func TestBashOutputRetainsOnlyBoundedPrefix(t *testing.T) {
	var output bashOutput
	chunk := []byte(strings.Repeat("x", 4096))
	for range 4096 { // drain 16 MiB without retaining it
		n, err := output.Write(chunk)
		if err != nil || n != len(chunk) {
			t.Fatalf("must drain the complete write: n=%d error=%v", n, err)
		}
	}
	if output.size != maxOutputSize || !output.truncated {
		t.Fatalf("capture size=%d truncated=%v", output.size, output.truncated)
	}
	want := strings.Repeat("x", maxOutputSize) + "\n... [output truncated]"
	if got := output.String(); got != want {
		t.Fatalf("wrong retained prefix: length=%d", len(got))
	}
}

func TestBashOutputExactLimitIsNotTruncated(t *testing.T) {
	var output bashOutput
	want := strings.Repeat("x", maxOutputSize)
	_, _ = output.Write([]byte(want))
	_, _ = output.Write(nil)
	if output.truncated || output.String() != want {
		t.Fatal("the exact limit is complete output, not a truncation")
	}
}

// TestExecuteBashNeverCarriesTheWholeOutput exercises the SITE, not the
// writer. The returned string cannot tell the two implementations apart — a
// buffer that accumulates everything and truncates after cmd.Run returns the
// same bytes as one that never kept them. What differs is the memory the run
// holds while the command is still printing, so that is what is asserted:
// drive far more output than the limit through the real path and require the
// heap not to have carried it. Restoring a growing bytes.Buffer at the call
// site turns this red; nothing else here does.
func TestExecuteBashNeverCarriesTheWholeOutput(t *testing.T) {
	const produced = 16 << 20 // 16 MiB, ~1600x the retained prefix

	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)

	out, err := ExecuteBash(
		context.Background(),
		map[string]any{"command": "yes x | head -c " + strconv.Itoa(produced)},
		permissions.ModeAllow, "",
	)
	runtime.ReadMemStats(&after)

	if err != nil {
		t.Fatalf("command must succeed: %v", err)
	}
	want := strings.Repeat("x\n", maxOutputSize/2) + "\n... [output truncated]"
	if out != want {
		t.Fatalf("returned output is not the bounded prefix: length=%d", len(out))
	}

	// TotalAlloc is cumulative and survives the collection of a buffer that
	// has already gone out of scope by the time ExecuteBash returns — which
	// a live-heap reading would miss entirely. A growing buffer costs at
	// least `produced` (and ~2x it while doubling); the bounded writer costs
	// the fixed array plus os/exec's own 32 KiB copy buffer.
	allocated := after.TotalAlloc - before.TotalAlloc
	if allocated > produced/4 {
		t.Fatalf("the run carried the output: allocated %d bytes for %d bytes of output", allocated, produced)
	}
}
