// Package binaryviewer implements NSGook's virtualized hex viewer and multi-endian data inspector.
package binaryviewer

import (
	"encoding/binary"
	"fmt"
	"math"
	"strconv"
	"strings"
	"unicode/utf8"
)

// InspectorField describes one decoded interpretation of the bytes at an offset.
type InspectorField struct {
	Label string
	Value string
}

// ParseOffset parses hex ("0x1A0") or decimal offsets and clamps against maxSize.
func ParseOffset(raw string, maxSize int64) (int64, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return 0, fmt.Errorf("empty offset")
	}
	base := 10
	if strings.HasPrefix(s, "0x") || strings.HasPrefix(s, "0X") {
		s = s[2:]
		base = 16
	} else if strings.HasSuffix(s, "h") || strings.HasSuffix(s, "H") {
		s = s[:len(s)-1]
		base = 16
	}
	off, err := strconv.ParseInt(s, base, 64)
	if err != nil || off < 0 {
		return 0, fmt.Errorf("invalid offset %q", raw)
	}
	if maxSize > 0 && off >= maxSize {
		off = maxSize - 1
	}
	return off, nil
}

// FormatHexRow renders up to 16 bytes into hex columns and printable ASCII text.
func FormatHexRow(data []byte) (hexPart, asciiPart string) {
	var hb strings.Builder
	var ab strings.Builder
	for i := 0; i < 16; i++ {
		if i == 8 {
			hb.WriteByte(' ')
		}
		if i < len(data) {
			b := data[i]
			fmt.Fprintf(&hb, "%02X ", b)
			if b >= 0x20 && b <= 0x7E {
				ab.WriteByte(b)
			} else {
				ab.WriteByte('.')
			}
		} else {
			hb.WriteString("   ")
			ab.WriteByte(' ')
		}
	}
	return strings.TrimRight(hb.String(), " "), ab.String()
}

// DecodeInspector interprets up to 8 bytes starting at offset 0 of buf.
func DecodeInspector(buf []byte) []InspectorField {
	if len(buf) == 0 {
		return nil
	}
	var fields []InspectorField
	b0 := buf[0]
	fields = append(fields,
		InspectorField{Label: "int8", Value: strconv.Itoa(int(int8(b0)))},
		InspectorField{Label: "uint8 / hex", Value: fmt.Sprintf("%d (0x%02X)", b0, b0)},
	)
	if len(buf) >= 2 {
		uLE := binary.LittleEndian.Uint16(buf[:2])
		uBE := binary.BigEndian.Uint16(buf[:2])
		fields = append(fields,
			InspectorField{Label: "int16 LE / BE", Value: fmt.Sprintf("%d / %d", int16(uLE), int16(uBE))},
			InspectorField{Label: "uint16 LE / BE", Value: fmt.Sprintf("%d / %d", uLE, uBE)},
		)
	}
	if len(buf) >= 4 {
		uLE := binary.LittleEndian.Uint32(buf[:4])
		uBE := binary.BigEndian.Uint32(buf[:4])
		fLE := math.Float32frombits(uLE)
		fields = append(fields,
			InspectorField{Label: "int32 LE / BE", Value: fmt.Sprintf("%d / %d", int32(uLE), int32(uBE))},
			InspectorField{Label: "uint32 LE / BE", Value: fmt.Sprintf("%d / %d", uLE, uBE)},
			InspectorField{Label: "float32 LE", Value: fmt.Sprintf("%g", fLE)},
		)
	}
	if len(buf) >= 8 {
		uLE := binary.LittleEndian.Uint64(buf[:8])
		uBE := binary.BigEndian.Uint64(buf[:8])
		fLE := math.Float64frombits(uLE)
		fields = append(fields,
			InspectorField{Label: "int64 LE", Value: fmt.Sprintf("%d", int64(uLE))},
			InspectorField{Label: "uint64 LE", Value: fmt.Sprintf("%d (0x%X)", uLE, uLE)},
			InspectorField{Label: "uint64 BE", Value: fmt.Sprintf("%d", uBE)},
			InspectorField{Label: "float64 LE", Value: fmt.Sprintf("%g", fLE)},
		)
	}
	r, rLen := utf8.DecodeRune(buf)
	if r != utf8.RuneError && rLen > 0 {
		fields = append(fields, InspectorField{Label: "UTF-8", Value: fmt.Sprintf("%q (U+%04X)", r, r)})
	}
	return fields
}
