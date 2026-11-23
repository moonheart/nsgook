package pdfviewer

import (
	"bytes"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
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

func TestServeLocalPDFSpecialFilename(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "20286 - 编译原理（第2版） (final).pdf")
	content := []byte("%PDF-1.7\n% local preview payload\n")
	if err := os.WriteFile(p, content, 0o644); err != nil {
		t.Fatal(err)
	}

	urlStr, stop, err := serveLocalPDF(p)
	if err != nil {
		t.Fatal(err)
	}
	defer stop()

	resp, err := http.Get(urlStr)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(body, content) {
		t.Fatalf("served %q, want %q", body, content)
	}

	u, err := url.Parse(urlStr)
	if err != nil {
		t.Fatal(err)
	}
	bad, err := http.Get("http://" + u.Host + "/not-the-token")
	if err != nil {
		t.Fatal(err)
	}
	defer bad.Body.Close()
	if bad.StatusCode != http.StatusNotFound {
		t.Fatalf("unknown path status = %d, want 404", bad.StatusCode)
	}
	if strings.Contains(urlStr, "编译原理") {
		t.Fatalf("file name leaked into the local URL: %q", urlStr)
	}
}
