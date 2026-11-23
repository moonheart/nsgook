// Package fontviewer implements NSGook's TTF/OTF/TTC/WOFF/WOFF2 font specimen and glyph grid plugin.
package fontviewer

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf16"

	"github.com/andybalholm/brotli"
)

var fontExts = map[string]bool{
	".ttf": true, ".otf": true, ".ttc": true, ".woff": true, ".woff2": true,
}

// IsFontExt reports whether ext is a supported font extension.
func IsFontExt(ext string) bool { return fontExts[strings.ToLower(ext)] }

// FontInfo describes parsed metadata and sample codepoints of a font file.
type FontInfo struct {
	Format            string
	Family            string
	Style             string
	FullName          string
	Version           string
	Designer          string
	Copyright         string
	Weight            int
	GlyphCount        int
	UnitsPerEM        int
	Runes             []rune
	RegisterableBytes []byte
}

// ParseFontFile reads and inspects a font file on disk.
func ParseFontFile(path string) (*FontInfo, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return ParseFontBytes(filepath.Ext(path), raw)
}

// ParseFontBytes inspects TTF/OTF/TTC/WOFF/WOFF2 font bytes.
func ParseFontBytes(ext string, raw []byte) (*FontInfo, error) {
	if len(raw) < 12 {
		return nil, fmt.Errorf("file too small to be a valid font")
	}
	tag := string(raw[:4])
	switch {
	case tag == "wOFF":
		sfnt, err := decompressWOFF1(raw)
		if err != nil {
			return nil, err
		}
		info, err := parseSFNT("WOFF", sfnt, 0)
		if err != nil {
			return nil, err
		}
		info.RegisterableBytes = sfnt
		return info, nil
	case tag == "wOF2":
		return parseWOFF2(raw)
	case tag == "ttcf":
		if len(raw) < 16 {
			return nil, fmt.Errorf("invalid TTC header")
		}
		numFonts := binary.BigEndian.Uint32(raw[8:12])
		if numFonts == 0 || len(raw) < 16 {
			return nil, fmt.Errorf("empty TTC collection")
		}
		off := int(binary.BigEndian.Uint32(raw[12:16]))
		info, err := parseSFNT("TTC", raw, off)
		if err != nil {
			return nil, err
		}
		info.RegisterableBytes = raw
		return info, nil
	default:
		fmtName := strings.TrimPrefix(strings.ToUpper(ext), ".")
		if fmtName == "" {
			fmtName = "SFNT"
		}
		if tag == "OTTO" {
			fmtName = "OTF"
		} else if binary.BigEndian.Uint32(raw[:4]) == 0x00010000 {
			fmtName = "TTF"
		}
		info, err := parseSFNT(fmtName, raw, 0)
		if err != nil {
			return nil, err
		}
		info.RegisterableBytes = raw
		return info, nil
	}
}

type sfntTable struct {
	offset uint32
	length uint32
}

func parseSFNT(format string, data []byte, start int) (*FontInfo, error) {
	if start < 0 || start+12 > len(data) {
		return nil, fmt.Errorf("invalid SFNT header offset")
	}
	numTables := int(binary.BigEndian.Uint16(data[start+4 : start+6]))
	if numTables <= 0 || start+12+numTables*16 > len(data) {
		return nil, fmt.Errorf("invalid SFNT table directory")
	}
	tables := make(map[string]sfntTable, numTables)
	for i := 0; i < numTables; i++ {
		rec := start + 12 + i*16
		name := string(data[rec : rec+4])
		off := binary.BigEndian.Uint32(data[rec+8 : rec+12])
		ln := binary.BigEndian.Uint32(data[rec+12 : rec+16])
		if uint64(off)+uint64(ln) <= uint64(len(data)) {
			tables[name] = sfntTable{offset: off, length: ln}
		}
	}
	info := &FontInfo{Format: format}
	slice := func(tag string) []byte {
		t, ok := tables[tag]
		if !ok {
			return nil
		}
		return data[t.offset : t.offset+t.length]
	}
	populateFromTables(info, slice)
	return info, nil
}

func populateFromTables(info *FontInfo, getTable func(string) []byte) {
	if head := getTable("head"); len(head) >= 20 {
		info.UnitsPerEM = int(binary.BigEndian.Uint16(head[18:20]))
	}
	if maxp := getTable("maxp"); len(maxp) >= 6 {
		info.GlyphCount = int(binary.BigEndian.Uint16(maxp[4:6]))
	}
	if os2 := getTable("OS/2"); len(os2) >= 6 {
		info.Weight = int(binary.BigEndian.Uint16(os2[4:6]))
	}
	if nameTbl := getTable("name"); len(nameTbl) >= 6 {
		parseNameTable(nameTbl, info)
	}
	if cmapTbl := getTable("cmap"); len(cmapTbl) >= 4 {
		info.Runes = parseCmapRunes(cmapTbl, 1024)
	}
	if info.FullName == "" {
		info.FullName = strings.TrimSpace(info.Family + " " + info.Style)
	}
}

func parseNameTable(tbl []byte, info *FontInfo) {
	if len(tbl) < 6 {
		return
	}
	count := int(binary.BigEndian.Uint16(tbl[2:4]))
	strOff := int(binary.BigEndian.Uint16(tbl[4:6]))
	if strOff > len(tbl) {
		return
	}
	type nameScore struct {
		val   string
		score int
	}
	best := make(map[uint16]nameScore)
	for i := 0; i < count; i++ {
		rec := 6 + i*12
		if rec+12 > len(tbl) {
			break
		}
		platID := binary.BigEndian.Uint16(tbl[rec : rec+2])
		encID := binary.BigEndian.Uint16(tbl[rec+2 : rec+4])
		langID := binary.BigEndian.Uint16(tbl[rec+4 : rec+6])
		nameID := binary.BigEndian.Uint16(tbl[rec+6 : rec+8])
		l := int(binary.BigEndian.Uint16(tbl[rec+8 : rec+10]))
		o := int(binary.BigEndian.Uint16(tbl[rec+10 : rec+12]))
		if strOff+o+l > len(tbl) || l == 0 {
			continue
		}
		rawStr := tbl[strOff+o : strOff+o+l]
		var decoded string
		var score int
		switch platID {
		case 3: // Windows
			decoded = decodeUTF16BE(rawStr)
			score = 100
			if encID == 1 || encID == 10 {
				score += 20
			}
			if langID == 0x0409 {
				score += 10
			}
		case 0: // Unicode
			decoded = decodeUTF16BE(rawStr)
			score = 90
		case 1: // Macintosh Roman
			if encID == 0 {
				decoded = string(rawStr)
				score = 50
			}
		}
		decoded = strings.TrimSpace(strings.TrimRight(decoded, "\x00"))
		if decoded == "" {
			continue
		}
		if cur, ok := best[nameID]; !ok || score > cur.score {
			best[nameID] = nameScore{val: decoded, score: score}
		}
	}
	info.Copyright = best[0].val
	info.Family = best[1].val
	if info.Family == "" {
		info.Family = best[16].val
	}
	info.Style = best[2].val
	if info.Style == "" {
		info.Style = best[17].val
	}
	info.FullName = best[4].val
	info.Version = best[5].val
	info.Designer = best[9].val
}

func decodeUTF16BE(b []byte) string {
	u := make([]uint16, 0, len(b)/2)
	for i := 0; i+1 < len(b); i += 2 {
		v := binary.BigEndian.Uint16(b[i : i+2])
		if v == 0 {
			continue
		}
		u = append(u, v)
	}
	return string(utf16.Decode(u))
}

func parseCmapRunes(tbl []byte, limit int) []rune {
	if len(tbl) < 4 {
		return nil
	}
	numSub := int(binary.BigEndian.Uint16(tbl[2:4]))
	bestFmt12 := -1
	bestFmt4 := -1
	for i := 0; i < numSub; i++ {
		rec := 4 + i*8
		if rec+8 > len(tbl) {
			break
		}
		off := int(binary.BigEndian.Uint32(tbl[rec+4 : rec+8]))
		if off+2 > len(tbl) {
			continue
		}
		fmtID := binary.BigEndian.Uint16(tbl[off : off+2])
		if fmtID == 12 && bestFmt12 < 0 {
			bestFmt12 = off
		} else if fmtID == 4 && bestFmt4 < 0 {
			bestFmt4 = off
		}
	}
	var runes []rune
	addRune := func(r rune) bool {
		if r < 0x21 || r > 0x10FFFF || (r >= 0xD800 && r <= 0xDFFF) || !unicode.IsPrint(r) {
			return len(runes) < limit
		}
		runes = append(runes, r)
		return len(runes) < limit
	}
	if bestFmt12 >= 0 && bestFmt12+16 <= len(tbl) {
		nGroups := int(binary.BigEndian.Uint32(tbl[bestFmt12+12 : bestFmt12+16]))
		for g := 0; g < nGroups && bestFmt12+16+(g+1)*12 <= len(tbl); g++ {
			base := bestFmt12 + 16 + g*12
			startCode := rune(binary.BigEndian.Uint32(tbl[base : base+4]))
			endCode := rune(binary.BigEndian.Uint32(tbl[base+4 : base+8]))
			for r := startCode; r <= endCode; r++ {
				if !addRune(r) {
					return runes
				}
			}
		}
		return runes
	}
	if bestFmt4 >= 0 && bestFmt4+14 <= len(tbl) {
		segCountX2 := int(binary.BigEndian.Uint16(tbl[bestFmt4+6 : bestFmt4+8]))
		segCount := segCountX2 / 2
		endOff := bestFmt4 + 14
		startOff := endOff + segCountX2 + 2
		if startOff+segCountX2 > len(tbl) {
			return nil
		}
		for s := 0; s < segCount; s++ {
			endCode := rune(binary.BigEndian.Uint16(tbl[endOff+s*2 : endOff+s*2+2]))
			startCode := rune(binary.BigEndian.Uint16(tbl[startOff+s*2 : startOff+s*2+2]))
			if startCode == 0xFFFF && endCode == 0xFFFF {
				continue
			}
			for r := startCode; r <= endCode; r++ {
				if !addRune(r) {
					return runes
				}
			}
		}
	}
	return runes
}

func decompressWOFF1(raw []byte) ([]byte, error) {
	if len(raw) < 44 || string(raw[:4]) != "wOFF" {
		return nil, fmt.Errorf("invalid WOFF header")
	}
	flavor := raw[4:8]
	numTables := int(binary.BigEndian.Uint16(raw[12:14]))
	if numTables <= 0 || 44+numTables*20 > len(raw) {
		return nil, fmt.Errorf("invalid WOFF table directory")
	}
	tags := make([][4]byte, 0, numTables)
	payloads := make([][]byte, 0, numTables)
	for i := 0; i < numTables; i++ {
		rec := 44 + i*20
		off := int(binary.BigEndian.Uint32(raw[rec+4 : rec+8]))
		compLen := int(binary.BigEndian.Uint32(raw[rec+8 : rec+12]))
		origLen := binary.BigEndian.Uint32(raw[rec+12 : rec+16])
		if off+compLen > len(raw) {
			return nil, fmt.Errorf("truncated WOFF table")
		}
		comp := raw[off : off+compLen]
		var payload []byte
		if uint32(compLen) < origLen {
			zr, err := zlib.NewReader(bytes.NewReader(comp))
			if err != nil {
				return nil, err
			}
			dec, err := io.ReadAll(io.LimitReader(zr, int64(origLen)+16))
			_ = zr.Close()
			if err != nil {
				return nil, err
			}
			payload = dec
		} else {
			payload = comp
		}
		var tag [4]byte
		copy(tag[:], raw[rec:rec+4])
		tags = append(tags, tag)
		payloads = append(payloads, payload)
	}
	return rebuildSFNT(flavor, tags, payloads), nil
}

// rebuildSFNT builds a flat SFNT binary so WOFF1 tables can be parsed uniformly and registered via cx.RegisterFont.
func rebuildSFNT(flavor []byte, tags [][4]byte, payloads [][]byte) []byte {
	n := len(tags)
	var out bytes.Buffer
	out.Write(flavor[:4])
	_ = binary.Write(&out, binary.BigEndian, uint16(n))
	searchRange, entrySel, rangeShift := sfntSearchParams(n)
	_ = binary.Write(&out, binary.BigEndian, searchRange)
	_ = binary.Write(&out, binary.BigEndian, entrySel)
	_ = binary.Write(&out, binary.BigEndian, rangeShift)

	off := uint32(12 + n*16)
	type rec struct {
		tag    [4]byte
		sum    uint32
		offset uint32
		length uint32
		data   []byte
	}
	recs := make([]rec, n)
	for i := 0; i < n; i++ {
		d := payloads[i]
		recs[i] = rec{
			tag:    tags[i],
			sum:    sfntChecksum(d),
			offset: off,
			length: uint32(len(d)),
			data:   d,
		}
		padded := (uint32(len(d)) + 3) &^ 3
		off += padded
	}
	for _, r := range recs {
		out.Write(r.tag[:])
		_ = binary.Write(&out, binary.BigEndian, r.sum)
		_ = binary.Write(&out, binary.BigEndian, r.offset)
		_ = binary.Write(&out, binary.BigEndian, r.length)
	}
	for _, r := range recs {
		out.Write(r.data)
		for out.Len()%4 != 0 {
			out.WriteByte(0)
		}
	}
	return out.Bytes()
}

func sfntSearchParams(n int) (uint16, uint16, uint16) {
	maxPow := 1
	sel := uint16(0)
	for maxPow*2 <= n {
		maxPow *= 2
		sel++
	}
	sr := uint16(maxPow * 16)
	rs := uint16(n*16) - sr
	return sr, sel, rs
}

func sfntChecksum(b []byte) uint32 {
	var sum uint32
	for i := 0; i+3 < len(b); i += 4 {
		sum += binary.BigEndian.Uint32(b[i : i+4])
	}
	rem := len(b) % 4
	if rem > 0 {
		var tail [4]byte
		copy(tail[:], b[len(b)-rem:])
		sum += binary.BigEndian.Uint32(tail[:])
	}
	return sum
}

var woff2KnownTags = []string{
	"cmap", "head", "hhea", "hmtx", "maxp", "name", "OS/2", "post", "cvt ",
	"fpgm", "glyf", "loca", "prep", "CFF ", "VORG", "EBDT", "EBLC", "gasp",
	"hdmx", "kern", "LTSH", "PCLT", "VDMX", "vhea", "vmtx", "BASE", "GDEF",
	"GPOS", "GSUB", "EBSC", "JSTF", "MATH", "CBDT", "CBLC", "COLR", "CPAL",
	"SVG ", "sbix", "acnt", "avar", "bdat", "bloc", "bsln", "cvar", "fdsc",
	"feat", "fmtx", "fvar", "gvar", "hsty", "just", "lcar", "mort", "morx",
	"opbd", "prop", "trak", "Zapf", "Silf", "Glat", "Gloc", "Feat", "Sill",
}

func parseWOFF2(raw []byte) (*FontInfo, error) {
	if len(raw) < 48 || string(raw[:4]) != "wOF2" {
		return nil, fmt.Errorf("invalid WOFF2 header")
	}
	numTables := int(binary.BigEndian.Uint16(raw[12:14]))
	totalCompressed := int(binary.BigEndian.Uint32(raw[20:24]))
	pos := 48
	type w2Entry struct {
		tag    string
		length uint32
	}
	entries := make([]w2Entry, 0, numTables)
	for i := 0; i < numTables && pos < len(raw); i++ {
		flags := raw[pos]
		pos++
		idx := int(flags & 0x3F)
		var tag string
		if idx == 63 {
			if pos+4 > len(raw) {
				break
			}
			tag = string(raw[pos : pos+4])
			pos += 4
		} else if idx < len(woff2KnownTags) {
			tag = woff2KnownTags[idx]
		}
		origLen, ok := readUIntBase128(raw, &pos)
		if !ok {
			break
		}
		txVer := (flags >> 6) & 0x03
		hasTxLen := ((tag == "glyf" || tag == "loca") && txVer != 3) || (tag != "glyf" && tag != "loca" && txVer != 0)
		var streamLen uint32 = origLen
		if hasTxLen {
			txLen, ok := readUIntBase128(raw, &pos)
			if !ok {
				break
			}
			streamLen = txLen
		}
		entries = append(entries, w2Entry{tag: tag, length: streamLen})
	}

	if totalCompressed <= 0 || pos+totalCompressed > len(raw) {
		return &FontInfo{Format: "WOFF2"}, nil
	}
	br := brotli.NewReader(bytes.NewReader(raw[pos : pos+totalCompressed]))
	decompressed, err := io.ReadAll(io.LimitReader(br, 64<<20))
	if err != nil {
		return &FontInfo{Format: "WOFF2"}, nil
	}

	tables := make(map[string][]byte, len(entries))
	cursor := 0
	for _, e := range entries {
		end := cursor + int(e.length)
		if end > len(decompressed) {
			break
		}
		tables[e.tag] = decompressed[cursor:end]
		cursor = end
	}

	info := &FontInfo{Format: "WOFF2"}
	populateFromTables(info, func(tag string) []byte { return tables[tag] })
	return info, nil
}

func readUIntBase128(buf []byte, pos *int) (uint32, bool) {
	var accum uint32
	for i := 0; i < 5; i++ {
		if *pos >= len(buf) {
			return 0, false
		}
		b := buf[*pos]
		*pos++
		if i == 0 && b == 0x80 {
			return 0, false
		}
		if accum&0xFE000000 != 0 {
			return 0, false
		}
		accum = (accum << 7) | uint32(b&0x7F)
		if b&0x80 == 0 {
			return accum, true
		}
	}
	return 0, false
}
