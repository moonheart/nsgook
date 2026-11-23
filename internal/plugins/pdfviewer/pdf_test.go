package pdfviewer

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInspectPDF(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "sample.pdf")
	content := "%PDF-1.7\n1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n2 0 obj\n<< /Type /Pages /Count 3 >>\nendobj\n3 0 obj\n<< /Type /Page >>\nendobj\n4 0 obj\n<< /Title (Annual Report) /Author (Ada Lovelace) >>\nendobj\n"
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	meta := InspectPDF(p)
	if meta.Version != "1.7" {
		t.Fatalf("version = %q, want 1.7", meta.Version)
	}
	if meta.Pages < 1 {
		t.Fatalf("expected pages >= 1, got %d", meta.Pages)
	}
	if meta.Title != "Annual Report" || meta.Author != "Ada Lovelace" {
		t.Fatalf("unexpected pdf metadata: %+v", meta)
	}
}
