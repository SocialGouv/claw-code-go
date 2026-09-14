package tools

import (
	"strings"
	"testing"
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
