package markdownviewer

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCanHandleRejectsBinaryMdx(t *testing.T) {
	dir := t.TempDir()

	// MDict .mdx dictionaries start with UTF-16LE-looking header text but are binary.
	bin := make([]byte, 16<<10)
	for i := 0; i < 256; i++ {
		bin[i*2] = 'A' + byte(i%26)
		bin[i*2+1] = 0
	}
	for i := 512; i < len(bin); i++ {
		bin[i] = byte(i*37+11) | 1
	}
	binaryPath := filepath.Join(dir, "dictionary.mdx")
	if err := os.WriteFile(binaryPath, bin, 0o644); err != nil {
		t.Fatal(err)
	}
	if (Plugin{}).CanHandle(binaryPath) {
		t.Fatal("binary .mdx dictionary should not be handled as Markdown")
	}

	textPath := filepath.Join(dir, "component.mdx")
	if err := os.WriteFile(textPath, []byte("# Hello\n\n<Component />\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !(Plugin{}).CanHandle(textPath) {
		t.Fatal("text .mdx document should be handled as Markdown")
	}
}
