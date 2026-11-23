package devviewers

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/icons/lucide"

	"nsgook/internal/plugins/pluginui"
	"nsgook/internal/viewer"
	"nsgook/locales"
)

var sqliteExts = map[string]bool{
	".db": true, ".sqlite": true, ".sqlite3": true, ".db3": true, ".s3db": true,
}

// SqlitePlugin inspects SQLite 3 database headers and sqlite_schema/sqlite_master definitions in pure Go.
type SqlitePlugin struct{}

func (SqlitePlugin) ID() string              { return "dbviewer" }
func (SqlitePlugin) Name() string            { return "DbViewer" }
func (SqlitePlugin) Priority() int           { return 82 }
func (SqlitePlugin) CanHandle(p string) bool { return hasExt(p, sqliteExts) }

func (SqlitePlugin) Prepare(path string, ctx *viewer.Context) {
	ctx.PreferredSize = core.Size{Width: 800, Height: 600}
	ctx.Title.Set(viewer.DisplayName(path))
}

func (SqlitePlugin) View(cx *core.Scope, path string, ctx *viewer.Context) core.Node {
	tr := locales.For(cx)
	return renderReportViewer(cx, path, ctx, lucide.Database, func() (*Report, error) {
		return InspectSQLite(path, tr.DevSummary(), tr.DevTables())
	})
}

// SchemaEntry represents one row parsed from sqlite_schema on page 1.
type SchemaEntry struct {
	Type string
	Name string
	Tbl  string
	SQL  string
}

// InspectSQLite reads the SQLite file header and page-1 B-tree schema records in pure Go.
func InspectSQLite(path, titleSummary, titleTables string) (*Report, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return nil, err
	}
	var hdr [100]byte
	if _, err := f.ReadAt(hdr[:], 0); err != nil {
		return nil, err
	}
	if !bytes.Equal(hdr[:16], []byte("SQLite format 3\x00")) {
		return nil, fmt.Errorf("not a SQLite 3 database header")
	}

	pageSize := int(binary.BigEndian.Uint16(hdr[16:18]))
	if pageSize == 1 {
		pageSize = 65536
	}
	pageCount := int(binary.BigEndian.Uint32(hdr[28:32]))
	schemaVer := binary.BigEndian.Uint32(hdr[40:44])
	encCode := binary.BigEndian.Uint32(hdr[56:60])
	appID := binary.BigEndian.Uint32(hdr[68:72])

	encoding := "UTF-8"
	switch encCode {
	case 2:
		encoding = "UTF-16LE"
	case 3:
		encoding = "UTF-16BE"
	}

	page1 := make([]byte, pageSize)
	n, _ := f.ReadAt(page1, 0)
	entries := parseSQLitePage1Schema(page1[:n])

	summary := []pluginui.KV{
		{Key: "Format", Value: "SQLite 3"},
		{Key: "File Size", Value: viewer.FormatBytes(uint64(max(0, st.Size())))},
		{Key: "Page Size", Value: fmt.Sprintf("%d B", pageSize)},
		{Key: "Pages", Value: strconv.Itoa(pageCount)},
		{Key: "Encoding", Value: encoding},
		{Key: "Schema Cookie", Value: strconv.FormatUint(uint64(schemaVer), 10)},
	}
	if appID != 0 {
		summary = append(summary, pluginui.KV{Key: "Application ID", Value: fmt.Sprintf("0x%08X", appID)})
	}

	var tabs []ReportTab
	var schemaSummary []pluginui.KV
	for i, e := range entries {
		schemaSummary = append(schemaSummary, pluginui.KV{Key: strings.ToUpper(e.Type), Value: e.Name})
		if e.SQL != "" {
			tabs = append(tabs, ReportTab{
				ID:    fmt.Sprintf("obj-%d", i),
				Label: e.Name,
				Text:  e.SQL,
			})
		}
	}
	secs := []Section{{Title: titleSummary, Rows: summary}}
	if len(schemaSummary) > 0 {
		secs = append(secs, Section{Title: titleTables, Rows: schemaSummary})
	}

	return &Report{
		TitleSuffix: "SQLite 3",
		Badges:      []string{"SQLite 3", encoding, fmt.Sprintf("%d B pages", pageSize)},
		Sections:    secs,
		Tabs:        tabs,
	}, nil
}

// parseSQLitePage1Schema decodes leaf table B-tree cells on page 1 when sqlite_schema fits in one page.
func parseSQLitePage1Schema(page []byte) []SchemaEntry {
	const base = 100
	if len(page) < base+8 || page[base] != 0x0D { // 0x0D = leaf table btree page
		return nil
	}
	cellCount := int(binary.BigEndian.Uint16(page[base+3 : base+5]))
	var entries []SchemaEntry
	for i := 0; i < cellCount; i++ {
		ptrOff := base + 8 + i*2
		if ptrOff+2 > len(page) {
			break
		}
		cellOff := int(binary.BigEndian.Uint16(page[ptrOff : ptrOff+2]))
		if cellOff < base+8 || cellOff >= len(page) {
			continue
		}
		pos := cellOff
		_, ok := readSQLiteVarint(page, &pos) // payload size
		if !ok {
			continue
		}
		_, ok = readSQLiteVarint(page, &pos) // rowid
		if !ok || pos >= len(page) {
			continue
		}
		hdrStart := pos
		hdrSize, ok := readSQLiteVarint(page, &pos)
		if !ok || hdrStart+int(hdrSize) > len(page) {
			continue
		}
		var colTypes []uint64
		for pos < hdrStart+int(hdrSize) {
			st, ok := readSQLiteVarint(page, &pos)
			if !ok {
				break
			}
			colTypes = append(colTypes, st)
		}
		if len(colTypes) < 5 {
			continue
		}
		dataPos := hdrStart + int(hdrSize)
		var cols [5]string
		for c := 0; c < 5; c++ {
			sz := sqliteSerialSize(colTypes[c])
			if dataPos+sz > len(page) {
				break
			}
			if colTypes[c] >= 13 && colTypes[c]%2 == 1 {
				cols[c] = string(page[dataPos : dataPos+sz])
			}
			dataPos += sz
		}
		if cols[0] != "" || cols[1] != "" {
			entries = append(entries, SchemaEntry{
				Type: cols[0],
				Name: cols[1],
				Tbl:  cols[2],
				SQL:  cols[4],
			})
		}
	}
	return entries
}

func readSQLiteVarint(buf []byte, pos *int) (uint64, bool) {
	var val uint64
	for i := 0; i < 9; i++ {
		if *pos >= len(buf) {
			return 0, false
		}
		b := buf[*pos]
		*pos++
		if i == 8 {
			return (val << 8) | uint64(b), true
		}
		val = (val << 7) | uint64(b&0x7F)
		if b&0x80 == 0 {
			return val, true
		}
	}
	return val, false
}

func sqliteSerialSize(st uint64) int {
	switch st {
	case 0, 8, 9:
		return 0
	case 1:
		return 1
	case 2:
		return 2
	case 3:
		return 3
	case 4:
		return 4
	case 5:
		return 6
	case 6, 7:
		return 8
	}
	if st >= 12 {
		return int((st - 12 - (st & 1)) / 2)
	}
	return 0
}
