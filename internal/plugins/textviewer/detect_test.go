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
