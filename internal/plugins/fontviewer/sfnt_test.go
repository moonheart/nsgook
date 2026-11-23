package fontviewer

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"slices"
	"testing"
)

func buildMinimalTestSFNT() []byte {
	// head table (20 bytes used)
	head := make([]byte, 54)
	binary.BigEndian.PutUint16(head[18:20], 1000)

	// maxp table (6 bytes)
	maxp := make([]byte, 6)
	binary.BigEndian.PutUint16(maxp[4:6], 42)

	// name table: Family="DemoFont", Style="Regular" (UTF-16BE Windows platform 3, enc 1)
	encodeUTF16 := func(s string) []byte {
		var b []byte
		for _, r := range s {
			b = append(b, byte(r>>8), byte(r))
		}
		return b
	}
	famBytes := encodeUTF16("DemoFont")
	styBytes := encodeUTF16("Regular")
	strData := append(append([]byte{}, famBytes...), styBytes...)
	var nameBuf bytes.Buffer
	_ = binary.Write(&nameBuf, binary.BigEndian, uint16(0)) // format
	_ = binary.Write(&nameBuf, binary.BigEndian, uint16(2)) // count
	_ = binary.Write(&nameBuf, binary.BigEndian, uint16(6+2*12))
	// record 0: nameID=1
	_ = binary.Write(&nameBuf, binary.BigEndian, uint16(3))
	_ = binary.Write(&nameBuf, binary.BigEndian, uint16(1))
	_ = binary.Write(&nameBuf, binary.BigEndian, uint16(0x0409))
	_ = binary.Write(&nameBuf, binary.BigEndian, uint16(1))
	_ = binary.Write(&nameBuf, binary.BigEndian, uint16(len(famBytes)))
	_ = binary.Write(&nameBuf, binary.BigEndian, uint16(0))
	// record 1: nameID=2
	_ = binary.Write(&nameBuf, binary.BigEndian, uint16(3))
	_ = binary.Write(&nameBuf, binary.BigEndian, uint16(1))
	_ = binary.Write(&nameBuf, binary.BigEndian, uint16(0x0409))
	_ = binary.Write(&nameBuf, binary.BigEndian, uint16(2))
	_ = binary.Write(&nameBuf, binary.BigEndian, uint16(len(styBytes)))
	_ = binary.Write(&nameBuf, binary.BigEndian, uint16(len(famBytes)))
	nameBuf.Write(strData)

	// cmap format 4 subtable mapping 'A'..'C'
	var cmapBuf bytes.Buffer
	_ = binary.Write(&cmapBuf, binary.BigEndian, uint16(0))  // version
	_ = binary.Write(&cmapBuf, binary.BigEndian, uint16(1))  // numSubtables
	_ = binary.Write(&cmapBuf, binary.BigEndian, uint16(3))  // platform
	_ = binary.Write(&cmapBuf, binary.BigEndian, uint16(1))  // encoding
	_ = binary.Write(&cmapBuf, binary.BigEndian, uint32(12)) // offset
	// subtable format 4 at offset 12: 2 segments ('A'..'C', 0xFFFF..0xFFFF)
	_ = binary.Write(&cmapBuf, binary.BigEndian, uint16(4))
	_ = binary.Write(&cmapBuf, binary.BigEndian, uint16(32)) // length
	_ = binary.Write(&cmapBuf, binary.BigEndian, uint16(0))  // lang
	_ = binary.Write(&cmapBuf, binary.BigEndian, uint16(4))  // segCountX2
	_ = binary.Write(&cmapBuf, binary.BigEndian, uint16(4))  // searchRange
	_ = binary.Write(&cmapBuf, binary.BigEndian, uint16(1))  // entrySelector
	_ = binary.Write(&cmapBuf, binary.BigEndian, uint16(0))  // rangeShift
	_ = binary.Write(&cmapBuf, binary.BigEndian, uint16('C'))
	_ = binary.Write(&cmapBuf, binary.BigEndian, uint16(0xFFFF))
	_ = binary.Write(&cmapBuf, binary.BigEndian, uint16(0)) // reservedPad
	_ = binary.Write(&cmapBuf, binary.BigEndian, uint16('A'))
	_ = binary.Write(&cmapBuf, binary.BigEndian, uint16(0xFFFF))
	_ = binary.Write(&cmapBuf, binary.BigEndian, uint32(0)) // deltas
	_ = binary.Write(&cmapBuf, binary.BigEndian, uint32(0)) // rangeOffsets

	tags := [][4]byte{
		{'c', 'm', 'a', 'p'},
		{'h', 'e', 'a', 'd'},
		{'m', 'a', 'x', 'p'},
		{'n', 'a', 'm', 'e'},
	}
	payloads := [][]byte{cmapBuf.Bytes(), head, maxp, nameBuf.Bytes()}
	return rebuildSFNT([]byte{0x00, 0x01, 0x00, 0x00}, tags, payloads)
}

func TestParseMinimalTTFAndWOFF(t *testing.T) {
	ttf := buildMinimalTestSFNT()
	fi, err := ParseFontBytes(".ttf", ttf)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Family != "DemoFont" || fi.Style != "Regular" || fi.GlyphCount != 42 || fi.UnitsPerEM != 1000 {
		t.Fatalf("unexpected font info: %+v", fi)
	}
	if !slices.Contains(fi.Runes, 'A') || !slices.Contains(fi.Runes, 'C') {
		t.Fatalf("expected 'A' and 'C' in cmap runes: %v", fi.Runes)
	}

	// Pack same tables into a valid WOFF1 wrapper using zlib compression.
	var tblData bytes.Buffer
	var compRecords bytes.Buffer
	type wrec struct {
		tag     [4]byte
		comp    []byte
		origLen uint32
	}
	raws := []struct {
		tag  [4]byte
		data []byte
	}{
		{[4]byte{'c', 'm', 'a', 'p'}, ttf[0:1]}, // skipped, extract via slices directly
	}
	_ = raws
	// Compress whole SFNT tables simply from known tags built by buildMinimalTestSFNT:
	var wrecs []wrec
	numTbl := int(binary.BigEndian.Uint16(ttf[4:6]))
	baseOff := 44 + numTbl*20
	for i := 0; i < numTbl; i++ {
		rec := 12 + i*16
		var tag [4]byte
		copy(tag[:], ttf[rec:rec+4])
		off := binary.BigEndian.Uint32(ttf[rec+8 : rec+12])
		ln := binary.BigEndian.Uint32(ttf[rec+12 : rec+16])
		orig := ttf[off : off+ln]
		var zbuf bytes.Buffer
		zw := zlib.NewWriter(&zbuf)
		_, _ = zw.Write(orig)
		_ = zw.Close()
		comp := orig
		if zbuf.Len() < len(orig) {
			comp = zbuf.Bytes()
		}
		wrecs = append(wrecs, wrec{tag: tag, comp: comp, origLen: ln})
	}
	_ = baseOff
	curOff := uint32(44 + len(wrecs)*20)
	for _, w := range wrecs {
		compRecords.Write(w.tag[:])
		_ = binary.Write(&compRecords, binary.BigEndian, curOff)
		_ = binary.Write(&compRecords, binary.BigEndian, uint32(len(w.comp)))
		_ = binary.Write(&compRecords, binary.BigEndian, w.origLen)
		_ = binary.Write(&compRecords, binary.BigEndian, uint32(0))
		tblData.Write(w.comp)
		for tblData.Len()%4 != 0 {
			tblData.WriteByte(0)
		}
		curOff += uint32((len(w.comp) + 3) &^ 3)
	}
	var woff bytes.Buffer
	woff.WriteString("wOFF")
	woff.Write([]byte{0x00, 0x01, 0x00, 0x00})
	_ = binary.Write(&woff, binary.BigEndian, uint32(44+compRecords.Len()+tblData.Len()))
	_ = binary.Write(&woff, binary.BigEndian, uint16(len(wrecs)))
	_ = binary.Write(&woff, binary.BigEndian, uint16(0))
	_ = binary.Write(&woff, binary.BigEndian, uint32(len(ttf)))
	woff.Write(make([]byte, 24))
	woff.Write(compRecords.Bytes())
	woff.Write(tblData.Bytes())

	wfi, err := ParseFontBytes(".woff", woff.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if wfi.Family != "DemoFont" || len(wfi.RegisterableBytes) == 0 {
		t.Fatalf("unexpected WOFF font info: %+v", wfi)
	}
}
