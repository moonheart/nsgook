package devviewers

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHashFileSHA256(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "model.pth")
	data := []byte("PK\x03\x04hello world")
	if err := os.WriteFile(p, data, 0o644); err != nil {
		t.Fatal(err)
	}

	sum := sha256.Sum256(data)
	want := strings.ToUpper(hex.EncodeToString(sum[:]))
	got, err := hashFileSHA256(p, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("hash = %q, want %q", got, want)
	}

	// A cancelled computation returns early without hashing.
	got, err = hashFileSHA256(p, func() bool { return true })
	if err != nil {
		t.Fatal(err)
	}
	if got != "" {
		t.Fatalf("cancelled hash = %q, want empty", got)
	}
}

func TestInspectMediaAndFileHasNoEagerHash(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "file.bin")
	if err := os.WriteFile(p, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	rep, err := InspectMediaAndFile(p, "Summary", "Details")
	if err != nil {
		t.Fatal(err)
	}
	for _, sec := range rep.Sections {
		for _, row := range sec.Rows {
			if row.Key == "SHA-256" {
				t.Fatalf("metadata report should not include an eager SHA-256 row")
			}
		}
	}
}
