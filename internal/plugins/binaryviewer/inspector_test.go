package binaryviewer

import "testing"

func TestParseOffsetAndFormatHexAndInspector(t *testing.T) {
	off, err := ParseOffset("0x1A0", 1024)
	if err != nil || off != 0x1A0 {
		t.Fatalf("expected 0x1A0, got %d err=%v", off, err)
	}
	clamped, err := ParseOffset("9999", 512)
	if err != nil || clamped != 511 {
		t.Fatalf("expected clamp to 511, got %d err=%v", clamped, err)
	}

	hx, asc := FormatHexRow([]byte("Hello\x00World!"))
	if asc != "Hello.World!    " {
		t.Fatalf("unexpected ascii: %q", asc)
	}
	if len(hx) < 20 {
		t.Fatalf("unexpected hex row: %q", hx)
	}

	fields := DecodeInspector([]byte{0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0xF0, 0x3F})
	if len(fields) < 5 {
		t.Fatalf("expected decoded fields, got %d", len(fields))
	}
}
