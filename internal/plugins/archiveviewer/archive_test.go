package archiveviewer

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"encoding/binary"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/text/encoding/simplifiedchinese"
)

func TestParseZipAndReadEntry(t *testing.T) {
	p := filepath.Join(t.TempDir(), "sample.zip")
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create("docs/readme.txt")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = w.Write([]byte("hello zip archive"))
	_ = zw.Close()
	if err := os.WriteFile(p, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}

	a, err := OpenArchive(p)
	if err != nil {
		t.Fatal(err)
	}
	if a.FileCount != 1 {
		t.Fatalf("expected 1 file, got %d", a.FileCount)
	}
	entry := a.ByPath["docs/readme.txt"]
	if entry == nil {
		t.Fatalf("missing docs/readme.txt in %v", a.ByPath)
	}
	got, err := entry.ReadFunc(1024)
	if err != nil || string(got) != "hello zip archive" {
		t.Fatalf("unexpected entry content: %q err=%v", got, err)
	}
}

func TestParseZipLegacyGBKName(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "legacy.zip")
	gbkName, err := simplifiedchinese.GBK.NewEncoder().Bytes([]byte("小新padpro2025解bl文件/readme.txt"))
	if err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	hdr := &zip.FileHeader{Name: string(gbkName), Method: zip.Deflate, NonUTF8: true}
	w, err := zw.CreateHeader(hdr)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = w.Write([]byte("hello legacy zip"))
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}

	a, err := OpenArchive(p)
	if err != nil {
		t.Fatal(err)
	}
	const want = "小新padpro2025解bl文件/readme.txt"
	entry := a.ByPath[want]
	if entry == nil {
		t.Fatalf("missing decoded GBK path %q; entries=%v", want, a.ByPath)
	}
	got, err := entry.ReadFunc(1024)
	if err != nil || string(got) != "hello legacy zip" {
		t.Fatalf("unexpected legacy entry content: %q err=%v", got, err)
	}
}

func TestParseTarGzAndASARAndPAK(t *testing.T) {
	dir := t.TempDir()

	// TAR.GZ
	tgzPath := filepath.Join(dir, "bundle.tar.gz")
	var tgz bytes.Buffer
	gw := gzip.NewWriter(&tgz)
	tw := tar.NewWriter(gw)
	payload := []byte("tar payload data")
	_ = tw.WriteHeader(&tar.Header{Name: "src/main.go", Mode: 0o644, Size: int64(len(payload))})
	_, _ = tw.Write(payload)
	_ = tw.Close()
	_ = gw.Close()
	_ = os.WriteFile(tgzPath, tgz.Bytes(), 0o644)

	ta, err := OpenArchive(tgzPath)
	if err != nil {
		t.Fatal(err)
	}
	if ta.Format != "TAR.GZ" || ta.ByPath["src/main.go"] == nil {
		t.Fatalf("unexpected targz archive: %+v", ta)
	}
	got, err := ta.ByPath["src/main.go"].ReadFunc(1024)
	if err != nil || string(got) != "tar payload data" {
		t.Fatalf("unexpected tar entry: %q err=%v", got, err)
	}

	// ASAR
	asarPath := filepath.Join(dir, "app.asar")
	metaJSON, _ := json.Marshal(map[string]any{
		"files": map[string]any{
			"package.json": map[string]any{"size": 11, "offset": "0"},
		},
	})
	for len(metaJSON)%4 != 0 {
		metaJSON = append(metaJSON, ' ')
	}
	hdrSize := uint32(8 + len(metaJSON))
	var asar bytes.Buffer
	_ = binary.Write(&asar, binary.LittleEndian, uint32(4))
	_ = binary.Write(&asar, binary.LittleEndian, hdrSize)
	_ = binary.Write(&asar, binary.LittleEndian, hdrSize-4)
	_ = binary.Write(&asar, binary.LittleEndian, uint32(len(metaJSON)))
	asar.Write(metaJSON)
	asar.WriteString(`{"a":"bcd"}`)
	_ = os.WriteFile(asarPath, asar.Bytes(), 0o644)

	aa, err := OpenArchive(asarPath)
	if err != nil {
		t.Fatal(err)
	}
	if aa.ByPath["package.json"] == nil {
		t.Fatal("missing package.json in ASAR")
	}
	got, err = aa.ByPath["package.json"].ReadFunc(1024)
	if err != nil || string(got) != `{"a":"bcd"}` {
		t.Fatalf("unexpected ASAR read: %q err=%v", got, err)
	}

	// PAK v5
	pakPath := filepath.Join(dir, "resources.pak")
	var pak bytes.Buffer
	_ = binary.Write(&pak, binary.LittleEndian, uint32(5))
	pak.Write([]byte{1, 0, 0, 0})
	_ = binary.Write(&pak, binary.LittleEndian, uint16(1))
	_ = binary.Write(&pak, binary.LittleEndian, uint16(0))
	dataStart := uint32(12 + 12)
	_ = binary.Write(&pak, binary.LittleEndian, uint16(42))
	_ = binary.Write(&pak, binary.LittleEndian, dataStart)
	_ = binary.Write(&pak, binary.LittleEndian, uint16(0))
	_ = binary.Write(&pak, binary.LittleEndian, dataStart+3)
	pak.Write([]byte("XYZ"))
	_ = os.WriteFile(pakPath, pak.Bytes(), 0o644)

	pa, err := OpenArchive(pakPath)
	if err != nil {
		t.Fatal(err)
	}
	if pa.FileCount != 1 {
		t.Fatalf("expected 1 PAK entry, got %d", pa.FileCount)
	}
	got, err = pa.Entries[0].ReadFunc(1024)
	if err != nil || string(got) != "XYZ" {
		t.Fatalf("unexpected PAK read: %q err=%v", got, err)
	}
}

func TestOpenArchiveStreaming(t *testing.T) {
	dir := t.TempDir()
	tgzPath := filepath.Join(dir, "stream.tar.gz")
	var tgz bytes.Buffer
	gw := gzip.NewWriter(&tgz)
	tw := tar.NewWriter(gw)
	for _, item := range []struct{ name, content string }{
		{"a.txt", "alpha"},
		{"b.txt", "beta"},
		{"c.txt", "gamma"},
	} {
		_ = tw.WriteHeader(&tar.Header{Name: item.name, Mode: 0o644, Size: int64(len(item.content))})
		_, _ = tw.Write([]byte(item.content))
	}
	_ = tw.Close()
	_ = gw.Close()
	if err := os.WriteFile(tgzPath, tgz.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}

	var counts []int
	partial, err := OpenArchiveStreaming(tgzPath, ScanLimits{BatchEntries: 1, MaxEntries: 2}, func(a *Archive) bool {
		counts = append(counts, a.FileCount)
		return true
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(counts) == 0 || counts[0] != 1 {
		t.Fatalf("streamed file counts = %v, want the first entry emitted immediately", counts)
	}
	if !partial.Truncated || partial.Scanning || partial.FileCount != 2 {
		t.Fatalf("partial = truncated:%v scanning:%v files:%d, want a truncated 2-file index", partial.Truncated, partial.Scanning, partial.FileCount)
	}
	if got, err := partial.ByPath["a.txt"].ReadFunc(1024); err != nil || string(got) != "alpha" {
		t.Fatalf("streamed entry read = %q err=%v", got, err)
	}

	full, err := OpenArchive(tgzPath)
	if err != nil || full.FileCount != 3 || full.Truncated {
		t.Fatalf("full archive = %+v err=%v", full, err)
	}
}
