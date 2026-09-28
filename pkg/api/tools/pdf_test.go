package tools

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeSimplePDF writes a minimal VALID one-page PDF (proper /Length,
// xref and startxref — the scraper is stricter than poppler) with an
// uncompressed BT/ET text object. The compressed/escaped variants are
// covered by the internal package's own tests, which this wrapper
// delegates to.
func writeSimplePDF(t *testing.T, dir string) string {
	t.Helper()
	contentStream := "BT\n/F1 12 Tf\n(reference spec text for the wrapper) Tj\nET"

	p := filepath.Join(dir, "spec.pdf")
	var pdf strings.Builder
	pdf.WriteString("%PDF-1.4\n")
	obj1 := pdf.Len()
	pdf.WriteString("1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n")
	obj2 := pdf.Len()
	pdf.WriteString("2 0 obj\n<< /Type /Pages /Kids [3 0 R] /Count 1 >>\nendobj\n")
	obj3 := pdf.Len()
	pdf.WriteString("3 0 obj\n<< /Type /Page /Parent 2 0 R /Contents 4 0 R >>\nendobj\n")
	obj4 := pdf.Len()
	pdf.WriteString(fmt.Sprintf("4 0 obj\n<< /Length %d >>\nstream\n%s\nendstream\nendobj\n", len(contentStream), contentStream))
	xref := pdf.Len()
	pdf.WriteString("xref\n0 5\n0000000000 65535 f \n")
	for _, off := range []int{obj1, obj2, obj3, obj4} {
		pdf.WriteString(fmt.Sprintf("%010d 00000 n \n", off))
	}
	pdf.WriteString("trailer\n<< /Size 5 /Root 1 0 R >>\n")
	pdf.WriteString(fmt.Sprintf("startxref\n%d\n%%%%EOF\n", xref))

	if err := os.WriteFile(p, []byte(pdf.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestExtractPDFTextReturnsTheTextObject(t *testing.T) {
	p := writeSimplePDF(t, t.TempDir())
	text, err := ExtractPDFText(p)
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	if !strings.Contains(text, "reference spec text for the wrapper") {
		t.Fatalf("text object missing from extraction: %q", text)
	}
}

func TestExtractPDFTextOnNonPDFBytesIsEmptyNotAnError(t *testing.T) {
	// The documented contract: bytes with no BT/ET content streams
	// (e.g. a text file) yield an empty string, not an error — only a
	// READ failure errors.
	p := filepath.Join(t.TempDir(), "not-a-pdf.pdf")
	if err := os.WriteFile(p, []byte("plain text"), 0o600); err != nil {
		t.Fatal(err)
	}
	text, err := ExtractPDFText(p)
	if err != nil {
		t.Fatalf("non-PDF bytes should not error: %v", err)
	}
	if text != "" {
		t.Fatalf("expected empty extraction, got %q", text)
	}
}

func TestExtractPDFTextOnAMissingFileIsAnError(t *testing.T) {
	if _, err := ExtractPDFText(filepath.Join(t.TempDir(), "absent.pdf")); err == nil {
		t.Fatal("expected an error for a missing file")
	}
}
