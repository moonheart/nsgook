package textviewer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/text/encoding/simplifiedchinese"
)

func TestTextEncodingAndProbeDetection(t *testing.T) {
	// UTF-8 BOM
	s, enc := DecodeText([]byte("\xEF\xBB\xBFhello 世界"))
	if enc != "UTF-8 BOM" || s != "hello 世界" {
		t.Fatalf("UTF-8 BOM failed: %q %q", enc, s)
	}

	// UTF-16 LE BOM
	utf16le := []byte{0xFF, 0xFE, 'H', 0, 'i', 0}
	s, enc = DecodeText(utf16le)
	if enc != "UTF-16 LE" || s != "Hi" {
		t.Fatalf("UTF-16 LE failed: %q %q", enc, s)
	}

	// GB18030
	gb, err := simplifiedchinese.GB18030.NewEncoder().Bytes([]byte("你好世界"))
	if err != nil {
		t.Fatal(err)
	}
	s, enc = DecodeText(gb)
	if enc != "GB18030" || s != "你好世界" {
		t.Fatalf("GB18030 failed: %q %q", enc, s)
	}

	// Binary probe
	if IsTextData([]byte{0x7F, 'E', 'L', 'F', 0, 1, 2, 3}) {
		t.Fatal("ELF binary bytes should not be detected as text")
	}
	if !IsTextData([]byte("line1\nline2\tcol\n")) {
		t.Fatal("plain ASCII should be detected as text")
	}
}

func TestDecodeTruncatedUTF8Suffix(t *testing.T) {
	full := []byte("hello 世界")
	// Cut the final 3-byte rune in half, as a capped preview read can do.
	truncated := full[:len(full)-2]
	got, enc := DecodeText(truncated)
	if enc != "UTF-8" || got != "hello 世" {
		t.Fatalf("truncated UTF-8 = (%q, %q), want (\"hello 世\", \"UTF-8\")", got, enc)
	}

	got, enc = DecodeText(full)
	if enc != "UTF-8" || got != "hello 世界" {
		t.Fatalf("complete UTF-8 = (%q, %q), want (\"hello 世界\", \"UTF-8\")", got, enc)
	}
}

func TestDecodeTruncatedGB18030Suffix(t *testing.T) {
	full, err := simplifiedchinese.GB18030.NewEncoder().Bytes([]byte("你好世界"))
	if err != nil {
		t.Fatal(err)
	}
	// Capped reads can cut the final legacy multi-byte character in half.
	truncated := full[:len(full)-1]
	got, enc := DecodeText(truncated)
	if enc != "GB18030" || got != "你好世" {
		t.Fatalf("truncated GB18030 = (%q, %q), want (\"你好世\", \"GB18030\")", got, enc)
	}
}

func TestUTF16ProbeRejectsBinaryTail(t *testing.T) {
	// Mimic an MDict .mdx file: a UTF-16LE-looking header followed by binary.
	buf := make([]byte, probeSize)
	for i := 0; i < 256; i++ {
		buf[i*2] = 'A' + byte(i%26)
		buf[i*2+1] = 0
	}
	for i := 512; i < len(buf); i++ {
		buf[i] = byte(i*37+11) | 1
	}
	if enc, ok := detectUTF16NoBOM(buf); ok {
		t.Fatalf("binary tail detected as %s", enc)
	}
	if IsTextData(buf) {
		t.Fatal("UTF-16 header with binary tail should not be detected as text")
	}

	// A genuinely UTF-16LE sample must still be recognized.
	full := make([]byte, probeSize)
	for i := 0; i < len(full)/2; i++ {
		full[i*2] = 'A' + byte(i%26)
		full[i*2+1] = 0
	}
	if enc, ok := detectUTF16NoBOM(full); !ok || enc != "UTF-16 LE" {
		t.Fatalf("full UTF-16 LE not detected: %q %v", enc, ok)
	}
	if !IsTextData(full) {
		t.Fatal("full UTF-16 LE should be detected as text")
	}
}

func TestLanguageDetectionAndPrettyFormat(t *testing.T) {
	if got := DetectLanguage("Dockerfile", ""); got != "dockerfile" {
		t.Fatalf("got %q, want dockerfile", got)
	}
	if got := DetectLanguage("run_script", "#!/usr/bin/env python3\nprint(1)\n"); got != "python" {
		t.Fatalf("shebang detection got %q, want python", got)
	}

	dir := t.TempDir()
	extless := filepath.Join(dir, "script")
	if err := os.WriteFile(extless, []byte("#!/bin/bash\necho ok\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !IsLikelyTextFile(extless) {
		t.Fatal("extensionless shell script should be recognized as text")
	}

	pretty, ok := PrettyFormat("json", `{"a":1,"b":[2,3]}`)
	if !ok || !strings.Contains(pretty, "\n  \"a\": 1") {
		t.Fatalf("PrettyFormat JSON failed: ok=%v out=%q", ok, pretty)
	}
}
