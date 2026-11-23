// Package textviewer implements NSGook's syntax-highlighted text, code, encoding, and diff viewer.
package textviewer

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf16"
	"unicode/utf8"

	"golang.org/x/text/encoding/charmap"
	"golang.org/x/text/encoding/japanese"
	"golang.org/x/text/encoding/korean"
	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/encoding/unicode"
)

const (
	// InitialReadLimit caps initial eager reads at 2 MiB so huge logs open instantly.
	InitialReadLimit = 2 << 20
	probeSize        = 16 << 10
)

var wellKnownTextExts = map[string]bool{
	".txt": true, ".log": true, ".ini": true, ".cfg": true, ".conf": true, ".config": true,
	".env": true, ".properties": true, ".toml": true, ".yaml": true, ".yml": true,
	".json": true, ".jsonl": true, ".jsonc": true, ".json5": true, ".xml": true, ".xsd": true,
	".xsl": true, ".xslt": true, ".svg": true, ".xshd": true, ".plist": true, ".reg": true,
	".c": true, ".h": true, ".cc": true, ".cpp": true, ".cxx": true, ".hpp": true, ".hh": true,
	".cs": true, ".csx": true, ".fs": true, ".fsx": true, ".vb": true,
	".go": true, ".mod": true, ".sum": true, ".rs": true, ".py": true, ".pyw": true, ".pyi": true,
	".js": true, ".mjs": true, ".cjs": true, ".jsx": true, ".ts": true, ".mts": true, ".tsx": true,
	".java": true, ".kt": true, ".kts": true, ".scala": true, ".groovy": true, ".gradle": true,
	".swift": true, ".m": true, ".mm": true, ".dart": true, ".rb": true, ".php": true,
	".lua": true, ".pl": true, ".pm": true, ".r": true, ".jl": true, ".ex": true, ".exs": true,
	".erl": true, ".hrl": true, ".hs": true, ".ml": true, ".mli": true, ".clj": true, ".cljs": true,
	".sh": true, ".bash": true, ".zsh": true, ".fish": true, ".ps1": true, ".psm1": true, ".psd1": true,
	".bat": true, ".cmd": true, ".awk": true, ".sed": true,
	".sql": true, ".graphql": true, ".gql": true, ".proto": true, ".thrift": true,
	".css": true, ".scss": true, ".sass": true, ".less": true, ".styl": true,
	".vue": true, ".svelte": true, ".astro": true, ".qml": true,
	".diff": true, ".patch": true, ".rej": true,
	".srt": true, ".sub": true, ".vtt": true, ".ass": true, ".ssa": true, ".lrc": true,
	".cmake": true, ".mk": true, ".mak": true, ".ninja": true, ".dockerfile": true,
	".gitignore": true, ".gitattributes": true, ".gitmodules": true, ".editorconfig": true,
	".npmrc": true, ".yarnrc": true, ".htaccess": true, ".hosts": true,
	".tex": true, ".bib": true, ".sty": true, ".asm": true, ".s": true,
	".hlsl": true, ".glsl": true, ".wgsl": true, ".vert": true, ".frag": true,
	".v": true, ".sv": true, ".vhd": true, ".vhdl": true, ".tcl": true, ".zig": true, ".nim": true,
	".sol": true, ".lean": true, ".nix": true, ".hcl": true, ".tf": true, ".tfvars": true,
}

var wellKnownNames = map[string]bool{
	"makefile": true, "gnumakefile": true, "dockerfile": true, "containerfile": true,
	"cmakelists.txt": true, "license": true, "licence": true, "copying": true,
	"readme": true, "changelog": true, "authors": true, "contributors": true,
	"notice": true, "hosts": true, "jenkinsfile": true, "vagrantfile": true,
	"procfile": true, "gemfile": true, "rakefile": true, "justfile": true,
	".gitignore": true, ".gitattributes": true, ".gitmodules": true, ".dockerignore": true,
	".editorconfig": true, ".env": true, ".eslintrc": true, ".prettierrc": true,
}

// IsLikelyTextFile reports whether path has a text/code extension, well-known name,
// or passes a 16 KiB binary-free content probe.
func IsLikelyTextFile(path string) bool {
	if path == "" || strings.HasPrefix(path, "::") {
		return false
	}
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return false
	}
	base := strings.ToLower(filepath.Base(path))
	if wellKnownNames[base] {
		return true
	}
	ext := strings.ToLower(filepath.Ext(path))
	if wellKnownTextExts[ext] {
		return true
	}

	// Probe first 16 KiB (matching QuickLook TextViewer).
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	buf := make([]byte, probeSize)
	n, err := io.ReadFull(f, buf)
	if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
		return false
	}
	return IsTextData(buf[:n])
}

// IsTextData reports whether a sample buffer looks like human-readable text.
func IsTextData(sample []byte) bool {
	if len(sample) == 0 {
		return true
	}
	// UTF BOM checks.
	if bytes.HasPrefix(sample, []byte{0xEF, 0xBB, 0xBF}) ||
		bytes.HasPrefix(sample, []byte{0xFF, 0xFE}) ||
		bytes.HasPrefix(sample, []byte{0xFE, 0xFF}) {
		return true
	}
	// Heuristic UTF-16 without BOM: NULs only in odd or even slots.
	if looksLikeUTF16(sample) {
		return true
	}
	// NUL bytes in non-UTF-16 data indicate binary content.
	if bytes.IndexByte(sample, 0) >= 0 {
		return false
	}
	// Check control-byte ratio.
	ctrl := 0
	for _, b := range sample {
		if b < 0x20 && b != '\t' && b != '\n' && b != '\r' && b != '\f' && b != '\b' {
			ctrl++
		}
	}
	return float64(ctrl)/float64(len(sample)) <= 0.02
}

// DecodeText converts raw bytes to UTF-8 text and returns the detected encoding label.
func DecodeText(raw []byte) (string, string) {
	if len(raw) == 0 {
		return "", "UTF-8"
	}
	// Capped previews can cut the final multi-byte rune in half. Drop that
	// incomplete tail so the whole buffer does not fall through to a legacy
	// single-byte encoding and turn into mojibake.
	if !utf8.Valid(raw) {
		raw = trimIncompleteUTF8Suffix(raw)
	}
	if bytes.HasPrefix(raw, []byte{0xEF, 0xBB, 0xBF}) {
		return string(raw[3:]), "UTF-8 BOM"
	}
	if bytes.HasPrefix(raw, []byte{0xFF, 0xFE}) {
		if s, err := unicode.UTF16(unicode.LittleEndian, unicode.UseBOM).NewDecoder().String(string(raw)); err == nil {
			return s, "UTF-16 LE"
		}
	}
	if bytes.HasPrefix(raw, []byte{0xFE, 0xFF}) {
		if s, err := unicode.UTF16(unicode.BigEndian, unicode.UseBOM).NewDecoder().String(string(raw)); err == nil {
			return s, "UTF-16 BE"
		}
	}
	if enc, ok := detectUTF16NoBOM(raw); ok {
		return decodeUTF16(raw, enc == "UTF-16 LE"), enc
	}
	if utf8.Valid(raw) {
		return string(raw), "UTF-8"
	}

	// Score legacy encodings: prefer valid GB18030 / Shift-JIS / EUC-KR without replacement runes, else Windows-1252.
	candidates := []struct {
		name string
		dec  func([]byte) ([]byte, error)
	}{
		{"GB18030", simplifiedchinese.GB18030.NewDecoder().Bytes},
		{"Shift-JIS", japanese.ShiftJIS.NewDecoder().Bytes},
		{"EUC-KR", korean.EUCKR.NewDecoder().Bytes},
	}
	for _, cand := range candidates {
		if s, ok := decodeLegacyText(cand.dec, raw); ok {
			return s, cand.name
		}
	}
	if s, err := charmap.Windows1252.NewDecoder().Bytes(raw); err == nil {
		return string(s), "Windows-1252"
	}
	return strings.ToValidUTF8(string(raw), "\uFFFD"), "Binary/Lossy"
}

// decodeLegacyText decodes raw with dec, retrying after dropping up to three
// trailing bytes when a capped read cut the final multi-byte character.
func decodeLegacyText(dec func([]byte) ([]byte, error), raw []byte) (string, bool) {
	for trim := 0; trim <= 3 && trim < len(raw); trim++ {
		b, err := dec(raw[:len(raw)-trim])
		if err != nil {
			continue
		}
		s := string(b)
		if !strings.ContainsRune(s, utf8.RuneError) {
			return s, true
		}
	}
	return "", false
}

// trimIncompleteUTF8Suffix removes the leading bytes of a truncated final
// UTF-8 sequence. It only trims when the remaining prefix is valid UTF-8 and
// the removed bytes form an incomplete, otherwise well-formed sequence.
func trimIncompleteUTF8Suffix(raw []byte) []byte {
	for n := 1; n <= 3 && n < len(raw); n++ {
		prefix := raw[:len(raw)-n]
		if !utf8.Valid(prefix) {
			continue
		}
		suffix := raw[len(prefix):]
		need := 0
		switch lead := suffix[0]; {
		case lead >= 0xC2 && lead <= 0xDF:
			need = 2
		case lead >= 0xE0 && lead <= 0xEF:
			need = 3
		case lead >= 0xF0 && lead <= 0xF4:
			need = 4
		default:
			return raw
		}
		if len(suffix) >= need {
			return raw
		}
		for _, c := range suffix[1:] {
			if c < 0x80 || c > 0xBF {
				return raw
			}
		}
		return prefix
	}
	return raw
}

func detectUTF16NoBOM(raw []byte) (string, bool) {
	if len(raw) < 4 {
		return "", false
	}
	evenNull, oddNull := 0, 0
	// Probe beyond the header: UTF-16-looking headers (e.g. MDict .mdx dictionaries)
	// must not make an otherwise binary file look like text.
	limit := min(len(raw), probeSize) &^ 1
	for i := 0; i < limit; i += 2 {
		if raw[i] == 0 && raw[i+1] != 0 {
			evenNull++
		}
		if raw[i+1] == 0 && raw[i] != 0 {
			oddNull++
		}
	}
	pairs := limit / 2
	if oddNull > pairs/3 && evenNull == 0 {
		return "UTF-16 LE", true
	}
	if evenNull > pairs/3 && oddNull == 0 {
		return "UTF-16 BE", true
	}
	return "", false
}

func looksLikeUTF16(raw []byte) bool {
	_, ok := detectUTF16NoBOM(raw)
	return ok
}

func decodeUTF16(raw []byte, le bool) string {
	n := len(raw) / 2
	u := make([]uint16, n)
	for i := 0; i < n; i++ {
		if le {
			u[i] = uint16(raw[i*2]) | uint16(raw[i*2+1])<<8
		} else {
			u[i] = uint16(raw[i*2])<<8 | uint16(raw[i*2+1])
		}
	}
	return string(utf16.Decode(u))
}

// DetectLanguage infers the Chroma/editor language identifier from path and first-line shebang.
func DetectLanguage(path, content string) string {
	base := strings.ToLower(filepath.Base(path))
	switch base {
	case "dockerfile", "containerfile":
		return "dockerfile"
	case "makefile", "gnumakefile":
		return "makefile"
	case "cmakelists.txt":
		return "cmake"
	}
	ext := strings.ToLower(filepath.Ext(path))
	if ext != "" {
		switch ext {
		case ".jsonl", ".jsonc", ".json5":
			return "json"
		case ".psm1", ".psd1":
			return "powershell"
		case ".patch", ".rej":
			return "diff"
		case ".csx":
			return "cs"
		case ".mts", ".cts":
			return "ts"
		case ".mjs", ".cjs":
			return "js"
		case ".svg":
			return "xml"
		case ".reg":
			return "ini"
		case ".wgsl", ".hlsl":
			return "glsl"
		default:
			return strings.TrimPrefix(ext, ".")
		}
	}
	if strings.HasPrefix(content, "#!") {
		firstLine := content
		if idx := strings.IndexByte(content, '\n'); idx >= 0 {
			firstLine = content[:idx]
		}
		lower := strings.ToLower(firstLine)
		switch {
		case strings.Contains(lower, "python"):
			return "python"
		case strings.Contains(lower, "node"), strings.Contains(lower, "deno"), strings.Contains(lower, "bun"):
			return "js"
		case strings.Contains(lower, "pwsh"), strings.Contains(lower, "powershell"):
			return "powershell"
		case strings.Contains(lower, "ruby"):
			return "ruby"
		case strings.Contains(lower, "perl"):
			return "perl"
		case strings.Contains(lower, "bash"), strings.Contains(lower, "zsh"), strings.Contains(lower, "/sh"):
			return "bash"
		}
	}
	trimmed := strings.TrimSpace(content)
	if strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[") {
		if json.Valid([]byte(trimmed)) {
			return "json"
		}
	}
	if strings.HasPrefix(trimmed, "<?xml") {
		return "xml"
	}
	return ""
}

// PrettyFormat attempts to format JSON or XML text cleanly.
func PrettyFormat(lang, text string) (string, bool) {
	trimmed := strings.TrimSpace(text)
	if lang == "json" || strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[") {
		var buf bytes.Buffer
		if err := json.Indent(&buf, []byte(trimmed), "", "  "); err == nil {
			return buf.String(), true
		}
	}
	if lang == "xml" || strings.HasPrefix(trimmed, "<") {
		dec := xml.NewDecoder(strings.NewReader(trimmed))
		var out bytes.Buffer
		enc := xml.NewEncoder(&out)
		enc.Indent("", "  ")
		for {
			tok, err := dec.Token()
			if err == io.EOF {
				_ = enc.Flush()
				return out.String(), true
			}
			if err != nil {
				return text, false
			}
			if err := enc.EncodeToken(tok); err != nil {
				return text, false
			}
		}
	}
	return text, false
}
