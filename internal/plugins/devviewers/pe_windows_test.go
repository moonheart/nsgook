//go:build windows

package devviewers

import (
	"os"
	"testing"
)

func TestInspectPESignatureIsDeferred(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	report, err := InspectPE(exe, "Summary", "Sections", "Imports", "Symbols", "Details")
	if err != nil {
		t.Fatal(err)
	}
	if report.Later == nil {
		t.Fatal("InspectPE did not defer the signature check")
	}
	for _, section := range report.Sections {
		for _, row := range section.Rows {
			if row.Key == "Digital Signature" {
				if row.Value != "Checking…" {
					t.Fatalf("initial signature = %q, want Checking…", row.Value)
				}
				return
			}
		}
	}
	t.Fatal("initial report has no Digital Signature row")
}

func TestPEAttributeAndLanguageLabels(t *testing.T) {
	if got := attributesString(0); got != "Normal" {
		t.Fatalf("attributesString(0) = %q, want Normal", got)
	}
	if got := attributesString(0x1 | 0x2); got != "Read-only, Hidden" {
		t.Fatalf("attributesString = %q, want Read-only, Hidden", got)
	}
	if got := languageName(0); got != "Language Neutral" {
		t.Fatalf("languageName(0) = %q", got)
	}
	if got := languageName(0x0804); got != "Chinese (Simplified)" {
		t.Fatalf("languageName(0x0804) = %q", got)
	}
}
