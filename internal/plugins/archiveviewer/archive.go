// Package archiveviewer implements NSGook's pure-Go archive browser and entry preview plugin.
package archiveviewer

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/bzip2"
	"compress/gzip"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/text/encoding/simplifiedchinese"
)

const MaxInlinePreviewBytes int64 = 512 * 1024

var supportedExts = map[string]bool{
	".zip": true, ".cbz": true, ".jar": true, ".war": true, ".ear": true,
	".whl": true, ".egg": true, ".nupkg": true, ".vsix": true, ".xpi": true,
	".crx": true, ".tar": true, ".tgz": true, ".gz": true, ".bz2": true,
	".tbz2": true, ".cbt": true, ".asar": true, ".pak": true, ".ds_store": true,
}

// IsSupportedPath reports whether p has a supported archive extension or filename.
func IsSupportedPath(p string) bool {
	lower := strings.ToLower(filepath.Base(p))
	if lower == ".ds_store" || strings.HasSuffix(lower, ".tar.gz") || strings.HasSuffix(lower, ".tar.bz2") {
		return true
	}
	return supportedExts[strings.ToLower(filepath.Ext(lower))]
}

// Entry represents a single file or folder inside an archive.
type Entry struct {
	Path       string
	Name       string
	Dir        bool
	Size       int64
	PackedSize int64
	ModTime    time.Time
	ReadFunc   func(limit int64) ([]byte, error)
}

// Archive represents a parsed archive index plus hierarchy helpers.
type Archive struct {
	Format      string
	TotalSize   int64
	PackedTotal int64
	FileCount   int
	Entries     []*Entry
	ByPath      map[string]*Entry
	Roots       []any
	Children    map[string][]any
	AllDirs     map[string]bool
	// Scanning reports that a compressed tar's index is still growing.
	Scanning bool
	// Truncated reports that scanning stopped at ScanLimits.MaxEntries.
	Truncated bool
}

// ScanLimits bounds a compressed tar scan. Compressed tar has no index, so
// reaching each header means decompressing all the data before it. The scan
// emits the first entry immediately and then a growing index every
// BatchEntries entries, stopping at MaxEntries or when Stop reports true.
type ScanLimits struct {
	BatchEntries int
	MaxEntries   int
	Stop         func() bool
}

// OpenArchive parses archive metadata and entry index without extracting full contents.
func OpenArchive(p string) (*Archive, error) {
	lower := strings.ToLower(filepath.Base(p))
	switch {
	case lower == ".ds_store" || strings.HasSuffix(lower, ".ds_store"):
		return parseDSStore(p)
	case strings.HasSuffix(lower, ".tar.gz") || strings.HasSuffix(lower, ".tgz"):
		return parseTarCompressed(p, "TAR.GZ", true)
	case strings.HasSuffix(lower, ".tar.bz2") || strings.HasSuffix(lower, ".tbz2"):
		return parseTarCompressed(p, "TAR.BZ2", false)
	}

	switch strings.ToLower(filepath.Ext(lower)) {
	case ".tar", ".cbt":
		return parseTarFile(p, "TAR")
	case ".gz":
		return parseSingleCompressed(p, "GZIP", true)
	case ".bz2":
		return parseSingleCompressed(p, "BZIP2", false)
	case ".asar":
		return parseASAR(p)
	case ".pak":
		return parsePAK(p)
	case ".crx":
		return parseCRXZip(p)
	default:
		return parseZip(p, strings.TrimPrefix(strings.ToUpper(filepath.Ext(lower)), "."))
	}
}

// OpenArchiveStreaming is OpenArchive for formats whose index grows as it is
// read: a tar, tar.gz or tar.bz2. emit receives the first entry as soon as its
// header is read, then the growing index, and stops the scan when it returns
// false. Other formats have an index already and are returned by OpenArchive
// without calling emit.
func OpenArchiveStreaming(p string, limits ScanLimits, emit func(*Archive) bool) (*Archive, error) {
	lower := strings.ToLower(filepath.Base(p))
	switch {
	case strings.HasSuffix(lower, ".tar.gz") || strings.HasSuffix(lower, ".tgz"):
		return parseTarCompressedStreaming(p, "TAR.GZ", true, limits, emit)
	case strings.HasSuffix(lower, ".tar.bz2") || strings.HasSuffix(lower, ".tbz2"):
		return parseTarCompressedStreaming(p, "TAR.BZ2", false, limits, emit)
	}
	if ext := strings.ToLower(filepath.Ext(lower)); ext == ".tar" || ext == ".cbt" {
		return parseTarFileStreaming(p, "TAR", limits, emit)
	}
	return OpenArchive(p)
}

func finalizeArchive(format string, entries []*Entry) *Archive {
	arch := &Archive{
		Format:   format,
		ByPath:   make(map[string]*Entry, len(entries)),
		Children: make(map[string][]any),
		AllDirs:  make(map[string]bool),
	}

	dirChildren := make(map[string]map[string]struct{})
	addEdge := func(parent, child string) {
		m := dirChildren[parent]
		if m == nil {
			m = make(map[string]struct{})
			dirChildren[parent] = m
		}
		m[child] = struct{}{}
	}

	for _, e := range entries {
		clean := strings.Trim(path.Clean(strings.ReplaceAll(e.Path, "\\", "/")), "/")
		if clean == "" || clean == "." {
			continue
		}
		// A streaming scan finalizes the same entries for each snapshot; only
		// write the cleaned name once so snapshots already handed to the UI
		// never change under it.
		if e.Path != clean {
			e.Path = clean
		}
		if name := path.Base(clean); e.Name != name {
			e.Name = name
		}
		if existing, exists := arch.ByPath[clean]; exists {
			if existing.Dir && !e.Dir {
				arch.ByPath[clean] = e
			}
		} else {
			arch.ByPath[clean] = e
			arch.Entries = append(arch.Entries, e)
		}
		if e.Dir {
			arch.AllDirs[clean] = true
		} else {
			arch.FileCount++
			arch.TotalSize += max(0, e.Size)
			arch.PackedTotal += max(0, e.PackedSize)
		}

		// Ensure parent directory chain exists for tree navigation.
		cur := clean
		for {
			parent := path.Dir(cur)
			if parent == "." || parent == "/" {
				parent = ""
			}
			addEdge(parent, cur)
			if parent == "" {
				break
			}
			if _, ok := arch.ByPath[parent]; !ok {
				de := &Entry{
					Path: parent,
					Name: path.Base(parent),
					Dir:  true,
				}
				arch.ByPath[parent] = de
				arch.AllDirs[parent] = true
			}
			cur = parent
		}
	}

	sortChildren := func(keys map[string]struct{}) []any {
		list := make([]string, 0, len(keys))
		for k := range keys {
			list = append(list, k)
		}
		slices.SortFunc(list, func(a, b string) int {
			ea := arch.ByPath[a]
			eb := arch.ByPath[b]
			da := ea != nil && ea.Dir
			db := eb != nil && eb.Dir
			if da != db {
				if da {
					return -1
				}
				return 1
			}
			return strings.Compare(strings.ToLower(a), strings.ToLower(b))
		})
		out := make([]any, len(list))
		for i, k := range list {
			out[i] = k
		}
		return out
	}

	arch.Roots = sortChildren(dirChildren[""])
	for parent, set := range dirChildren {
		if parent != "" {
			arch.Children[parent] = sortChildren(set)
		}
	}

	// Keep flat Entries sorted deterministically.
	slices.SortFunc(arch.Entries, func(a, b *Entry) int {
		if a.Dir != b.Dir {
			if a.Dir {
				return -1
			}
			return 1
		}
		return strings.Compare(strings.ToLower(a.Path), strings.ToLower(b.Path))
	})
	return arch
}

func parseZip(p, format string) (*Archive, error) {
	return parseZipAt(p, format, 0)
}

func parseCRXZip(p string) (*Archive, error) {
	f, err := os.Open(p)
	if err != nil {
		return nil, err
	}
	var hdr [16]byte
	n, _ := io.ReadFull(f, hdr[:])
	_ = f.Close()
	if n >= 4 && string(hdr[:4]) == "Cr24" {
		// CRX2: magic(4) ver(4) pubKeyLen(4) sigLen(4); CRX3: magic(4) ver(4) headerLen(4)
		ver := binary.LittleEndian.Uint32(hdr[4:8])
		if ver == 3 && n >= 12 {
			off := int64(12) + int64(binary.LittleEndian.Uint32(hdr[8:12]))
			return parseZipAt(p, "CRX3", off)
		}
		if ver == 2 && n >= 16 {
			off := int64(16) + int64(binary.LittleEndian.Uint32(hdr[8:12])) + int64(binary.LittleEndian.Uint32(hdr[12:16]))
			return parseZipAt(p, "CRX2", off)
		}
	}
	return parseZipAt(p, "CRX", 0)
}

func parseZipAt(p, format string, offset int64) (*Archive, error) {
	info, err := os.Stat(p)
	if err != nil {
		return nil, err
	}
	if offset < 0 || offset >= info.Size() {
		return nil, fmt.Errorf("invalid archive offset")
	}
	f, err := os.Open(p)
	if err != nil {
		return nil, err
	}
	sr := io.NewSectionReader(f, offset, info.Size()-offset)
	zr, err := zip.NewReader(sr, info.Size()-offset)
	_ = f.Close()
	if err != nil {
		return nil, err
	}

	entries := make([]*Entry, 0, len(zr.File))
	for _, zf := range zr.File {
		// Windows Explorer and other legacy ZIP writers store non-ASCII names
		// in the system ANSI code page (GBK on Chinese Windows) without setting
		// the UTF-8 flag. Decode those for display while keeping the raw name
		// for looking the entry up in the archive.
		rawName := zf.Name
		name := decodeZipName(rawName)
		targetPath := p
		entries = append(entries, &Entry{
			Path:       name,
			Dir:        zf.FileInfo().IsDir() || strings.HasSuffix(name, "/"),
			Size:       int64(zf.UncompressedSize64),
			PackedSize: int64(zf.CompressedSize64),
			ModTime:    zf.Modified,
			ReadFunc: func(limit int64) ([]byte, error) {
				file, err := os.Open(targetPath)
				if err != nil {
					return nil, err
				}
				defer file.Close()
				st, err := file.Stat()
				if err != nil {
					return nil, err
				}
				z, err := zip.NewReader(io.NewSectionReader(file, offset, st.Size()-offset), st.Size()-offset)
				if err != nil {
					return nil, err
				}
				for _, item := range z.File {
					if item.Name == rawName {
						rc, err := item.Open()
						if err != nil {
							return nil, err
						}
						defer rc.Close()
						return io.ReadAll(io.LimitReader(rc, limit))
					}
				}
				return nil, fmt.Errorf("entry %q not found", rawName)
			},
		})
	}
	return finalizeArchive(format, entries), nil
}

// decodeZipName converts a legacy ZIP entry name to UTF-8. Go's archive/zip
// leaves names raw when the UTF-8 flag is absent, which renders GBK names as
// replacement characters in the UI.
func decodeZipName(name string) string {
	if utf8.ValidString(name) {
		return name
	}
	if decoded, err := simplifiedchinese.GB18030.NewDecoder().String(name); err == nil &&
		!strings.ContainsRune(decoded, utf8.RuneError) {
		return decoded
	}
	return strings.ToValidUTF8(name, "\uFFFD")
}

func parseTarFile(p, format string) (*Archive, error) {
	return parseTarFileStreaming(p, format, ScanLimits{}, nil)
}

func parseTarFileStreaming(p, format string, limits ScanLimits, emit func(*Archive) bool) (*Archive, error) {
	f, err := os.Open(p)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return readTarEntriesWith(format, f, func(entryName string, limit int64) ([]byte, error) {
		rf, err := os.Open(p)
		if err != nil {
			return nil, err
		}
		defer rf.Close()
		return extractTarEntry(rf, entryName, limit)
	}, limits, emit)
}

func parseTarCompressed(p, format string, isGzip bool) (*Archive, error) {
	return parseTarCompressedStreaming(p, format, isGzip, ScanLimits{}, nil)
}

func parseTarCompressedStreaming(p, format string, isGzip bool, limits ScanLimits, emit func(*Archive) bool) (*Archive, error) {
	f, err := os.Open(p)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var r io.Reader = f
	if isGzip {
		gz, err := gzip.NewReader(f)
		if err != nil {
			return nil, err
		}
		defer gz.Close()
		r = gz
	} else {
		r = bzip2.NewReader(f)
	}
	return readTarEntriesWith(format, r, func(entryName string, limit int64) ([]byte, error) {
		rf, err := os.Open(p)
		if err != nil {
			return nil, err
		}
		defer rf.Close()
		var rr io.Reader = rf
		if isGzip {
			gz, err := gzip.NewReader(rf)
			if err != nil {
				return nil, err
			}
			defer gz.Close()
			rr = gz
		} else {
			rr = bzip2.NewReader(rf)
		}
		return extractTarEntry(rr, entryName, limit)
	}, limits, emit)
}

// stopReader stops a tar scan between reads, so closing a preview does not
// wait for a multi-gigabyte stream to finish decompressing.
type stopReader struct {
	r    io.Reader
	stop func() bool
}

func (s stopReader) Read(p []byte) (int, error) {
	if s.stop != nil && s.stop() {
		return 0, io.EOF
	}
	return s.r.Read(p)
}

func readTarEntriesWith(format string, r io.Reader, readEntry func(name string, limit int64) ([]byte, error), limits ScanLimits, emit func(*Archive) bool) (*Archive, error) {
	if limits.Stop != nil {
		r = stopReader{r: r, stop: limits.Stop}
	}
	batch := limits.BatchEntries
	if batch <= 0 {
		batch = 200
	}
	tr := tar.NewReader(r)
	var entries []*Entry
	truncated := false
	snapshot := func(scanning bool) *Archive {
		a := finalizeArchive(format, entries)
		a.Scanning = scanning
		a.Truncated = truncated
		return a
	}
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		rawName := hdr.Name
		isDir := hdr.Typeflag == tar.TypeDir || strings.HasSuffix(rawName, "/")
		entries = append(entries, &Entry{
			Path:       rawName,
			Dir:        isDir,
			Size:       hdr.Size,
			PackedSize: hdr.Size,
			ModTime:    hdr.ModTime,
			ReadFunc: func(limit int64) ([]byte, error) {
				return readEntry(rawName, limit)
			},
		})
		if limits.MaxEntries > 0 && len(entries) >= limits.MaxEntries {
			truncated = true
			break
		}
		if emit != nil && (len(entries) == 1 || len(entries)%batch == 0) {
			if !emit(snapshot(true)) {
				return snapshot(false), nil
			}
		}
	}
	return snapshot(false), nil
}

func extractTarEntry(r io.Reader, name string, limit int64) ([]byte, error) {
	tr := tar.NewReader(r)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		if hdr.Name == name {
			return io.ReadAll(io.LimitReader(tr, limit))
		}
	}
	return nil, fmt.Errorf("entry %q not found", name)
}

func parseSingleCompressed(p, format string, isGzip bool) (*Archive, error) {
	st, err := os.Stat(p)
	if err != nil {
		return nil, err
	}
	base := filepath.Base(p)
	innerName := strings.TrimSuffix(base, filepath.Ext(base))
	if innerName == "" {
		innerName = base + ".out"
	}
	entries := []*Entry{
		{
			Path:       innerName,
			Size:       st.Size(),
			PackedSize: st.Size(),
			ModTime:    st.ModTime(),
			ReadFunc: func(limit int64) ([]byte, error) {
				f, err := os.Open(p)
				if err != nil {
					return nil, err
				}
				defer f.Close()
				var r io.Reader
				if isGzip {
					gz, err := gzip.NewReader(f)
					if err != nil {
						return nil, err
					}
					defer gz.Close()
					r = gz
				} else {
					r = bzip2.NewReader(f)
				}
				return io.ReadAll(io.LimitReader(r, limit))
			},
		},
	}
	return finalizeArchive(format, entries), nil
}

type asarNode struct {
	Files  map[string]asarNode `json:"files"`
	Size   int64               `json:"size"`
	Offset json.RawMessage     `json:"offset"`
}

func parseASAR(p string) (*Archive, error) {
	f, err := os.Open(p)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	// ASAR Chromium Pickle header: [sizePayload:u32][hdrSize:u32][strLenPayload:u32][jsonSize:u32][json...]
	var hdr [16]byte
	if _, err := io.ReadFull(f, hdr[:]); err != nil {
		return nil, err
	}
	headerSize := binary.LittleEndian.Uint32(hdr[4:8])
	jsonLen := binary.LittleEndian.Uint32(hdr[12:16])
	if headerSize == 0 || jsonLen == 0 || jsonLen > 32<<20 {
		return nil, fmt.Errorf("invalid ASAR header")
	}
	jsonBuf := make([]byte, jsonLen)
	if _, err := io.ReadFull(f, jsonBuf); err != nil {
		return nil, err
	}
	var root asarNode
	if err := json.Unmarshal(jsonBuf, &root); err != nil {
		return nil, err
	}
	baseOffset := int64(8 + headerSize)

	var entries []*Entry
	var walk func(prefix string, node asarNode)
	walk = func(prefix string, node asarNode) {
		for name, child := range node.Files {
			full := path.Join(prefix, name)
			if child.Files != nil {
				entries = append(entries, &Entry{Path: full, Dir: true})
				walk(full, child)
				continue
			}
			var relOff int64
			var s string
			if json.Unmarshal(child.Offset, &s) == nil {
				_, _ = fmt.Sscan(s, &relOff)
			} else {
				_ = json.Unmarshal(child.Offset, &relOff)
			}
			fileSize := child.Size
			absOff := baseOffset + relOff
			entries = append(entries, &Entry{
				Path:       full,
				Size:       fileSize,
				PackedSize: fileSize,
				ReadFunc: func(limit int64) ([]byte, error) {
					rf, err := os.Open(p)
					if err != nil {
						return nil, err
					}
					defer rf.Close()
					n := min(fileSize, limit)
					if n <= 0 {
						return nil, nil
					}
					buf := make([]byte, n)
					rn, err := rf.ReadAt(buf, absOff)
					if err != nil && err != io.EOF {
						return nil, err
					}
					return buf[:rn], nil
				},
			})
		}
	}
	walk("", root)
	return finalizeArchive("ASAR", entries), nil
}

func parsePAK(p string) (*Archive, error) {
	f, err := os.Open(p)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var hdr [12]byte
	if _, err := io.ReadFull(f, hdr[:]); err != nil {
		return nil, err
	}
	version := binary.LittleEndian.Uint32(hdr[0:4])
	var count int
	var entryStart int64
	switch version {
	case 4:
		count = int(binary.LittleEndian.Uint32(hdr[4:8]))
		entryStart = 9 // version(4) + count(4) + encoding(1)
	case 5:
		// version(4) + encoding(1) + padding(3) + resourceCount(2) + aliasCount(2)
		count = int(binary.LittleEndian.Uint16(hdr[8:10]))
		entryStart = 12
	default:
		return nil, fmt.Errorf("unsupported PAK version %d", version)
	}
	if count <= 0 || count > 200000 {
		return nil, fmt.Errorf("invalid PAK resource count %d", count)
	}
	type pakItem struct {
		id     uint16
		offset uint32
	}
	table := make([]pakItem, count+1)
	if _, err := f.Seek(entryStart, io.SeekStart); err != nil {
		return nil, err
	}
	for i := 0; i <= count; i++ {
		var row [6]byte
		if _, err := io.ReadFull(f, row[:]); err != nil {
			return nil, err
		}
		table[i] = pakItem{
			id:     binary.LittleEndian.Uint16(row[0:2]),
			offset: binary.LittleEndian.Uint32(row[2:6]),
		}
	}
	sort.SliceStable(table[:count], func(i, j int) bool {
		return table[i].offset < table[j].offset
	})
	entries := make([]*Entry, 0, count)
	for i := 0; i < count; i++ {
		item := table[i]
		sz := int64(0)
		if table[i+1].offset > item.offset {
			sz = int64(table[i+1].offset - item.offset)
		}
		off := int64(item.offset)
		entries = append(entries, &Entry{
			Path:       fmt.Sprintf("resource_%05d.bin", item.id),
			Size:       sz,
			PackedSize: sz,
			ReadFunc: func(limit int64) ([]byte, error) {
				rf, err := os.Open(p)
				if err != nil {
					return nil, err
				}
				defer rf.Close()
				n := min(sz, limit)
				buf := make([]byte, max(0, n))
				rn, err := rf.ReadAt(buf, off)
				if err != nil && err != io.EOF {
					return nil, err
				}
				return buf[:rn], nil
			},
		})
	}
	return finalizeArchive(fmt.Sprintf("PAK v%d", version), entries), nil
}

func parseDSStore(p string) (*Archive, error) {
	raw, err := os.ReadFile(p)
	if err != nil {
		return nil, err
	}
	if len(raw) < 36 || string(raw[4:8]) != "Bud1" {
		return nil, fmt.Errorf("not a valid macOS .DS_Store Buddy file")
	}
	// Extract UTF-16BE readable record strings / FourCC record tags for inspection.
	seen := make(map[string]bool)
	var entries []*Entry
	for i := 36; i+16 < len(raw); i++ {
		// Look for plausible UTF-16BE filename runs >= 3 ASCII chars prefixed by BE uint32 length.
		charLen := int(binary.BigEndian.Uint32(raw[i : i+4]))
		if charLen < 2 || charLen > 128 || i+4+charLen*2+4 > len(raw) {
			continue
		}
		ok := true
		var b strings.Builder
		for k := 0; k < charLen; k++ {
			hi, lo := raw[i+4+k*2], raw[i+4+k*2+1]
			if hi != 0 || lo < 0x20 || lo > 0x7E {
				ok = false
				break
			}
			b.WriteByte(lo)
		}
		if !ok {
			continue
		}
		name := b.String()
		if seen[name] {
			continue
		}
		seen[name] = true
		tag := string(bytes.Map(func(r rune) rune {
			if r >= 32 && r <= 126 {
				return r
			}
			return '.'
		}, raw[i+4+charLen*2:min(len(raw), i+8+charLen*2)]))
		summary := fmt.Sprintf("Record entry: %s\nType tag: %s\n", name, tag)
		entries = append(entries, &Entry{
			Path:       name,
			Size:       int64(len(summary)),
			PackedSize: int64(charLen * 2),
			ReadFunc: func(int64) ([]byte, error) {
				return []byte(summary), nil
			},
		})
	}
	return finalizeArchive(".DS_Store", entries), nil
}
