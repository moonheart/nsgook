//go:build windows

package devviewers

import "testing"

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
