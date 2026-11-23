package devviewers

import (
	"archive/zip"
	"encoding/binary"
	"encoding/xml"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/icons/lucide"

	"nsgook/internal/plugins/pluginui"
	"nsgook/internal/viewer"
	"nsgook/locales"
)

// CLSIDPlugin previews Explorer CLSID virtual folders (::{...}) and Windows shortcuts (.lnk).
type CLSIDPlugin struct{}

func (CLSIDPlugin) ID() string    { return "clsidviewer" }
func (CLSIDPlugin) Name() string  { return "CLSIDViewer" }
func (CLSIDPlugin) Priority() int { return 78 }
func (CLSIDPlugin) CanHandle(p string) bool {
	return strings.HasPrefix(p, "::") || strings.EqualFold(filepath.Ext(p), ".lnk")
}
func (CLSIDPlugin) Prepare(path string, ctx *viewer.Context) {
	ctx.PreferredSize = core.Size{Width: 620, Height: 420}
	ctx.Title.Set(viewer.DisplayName(path))
}
func (CLSIDPlugin) View(cx *core.Scope, path string, ctx *viewer.Context) core.Node {
	tr := locales.For(cx)
	return renderReportViewer(cx, path, ctx, lucide.FolderDot, func() (*Report, error) {
		if strings.HasPrefix(path, "::") {
			return &Report{
				TitleSuffix: "Shell CLSID",
				Badges:      []string{"Windows Shell Namespace"},
				Sections: []Section{{Title: tr.DevSummary(), Rows: []pluginui.KV{
					{Key: "CLSID Path", Value: path},
					{Key: "Name", Value: knownCLSIDName(path)},
				}}},
			}, nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		return InspectLNKBytes(raw, tr.DevSummary()), nil
	})
}

func knownCLSIDName(p string) string {
	upper := strings.ToUpper(p)
	known := map[string]string{
		"::{20D04FE0-3AEA-1069-A2D8-08002B30309D}": "This PC",
		"::{645FF040-5081-101B-9F08-00AA002F954E}": "Recycle Bin",
		"::{F02C1A0D-BE21-4350-88B0-7367FC96EF3C}": "Network",
		"::{5399E694-6CE5-4D6C-8FCE-1D8870FDCBA0}": "Control Panel",
		"::{031E4825-7B94-4DC3-B131-E946B44C8DD5}": "Libraries",
	}
	if n, ok := known[upper]; ok {
		return n
	}
	return p
}

// InspectLNKBytes parses the Microsoft Shell Link (.lnk) binary header & link info strings.
func InspectLNKBytes(raw []byte, titleSummary string) *Report {
	var rows []pluginui.KV
	if len(raw) < 76 || binary.LittleEndian.Uint32(raw[0:4]) != 0x0000004C {
		rows = append(rows, pluginui.KV{Key: "Format", Value: "Invalid or non-standard .lnk file"})
		return &Report{TitleSuffix: "LNK", Sections: []Section{{Title: titleSummary, Rows: rows}}}
	}
	flags := binary.LittleEndian.Uint32(raw[20:24])
	fileSize := binary.LittleEndian.Uint32(raw[32:36])
	pos := 76
	// HasLinkTargetIDList (bit 0)
	if flags&0x01 != 0 && pos+2 <= len(raw) {
		idLen := int(binary.LittleEndian.Uint16(raw[pos : pos+2]))
		pos += 2 + idLen
	}
	var targetPath, relPath, workDir, args string
	// HasLinkInfo (bit 1)
	if flags&0x02 != 0 && pos+28 <= len(raw) {
		infoSize := int(binary.LittleEndian.Uint32(raw[pos : pos+4]))
		localOff := int(binary.LittleEndian.Uint32(raw[pos+16 : pos+20]))
		if localOff > 0 && pos+localOff < len(raw) {
			end := bytesIndexZero(raw[pos+localOff : min(len(raw), pos+infoSize)])
			targetPath = string(raw[pos+localOff : pos+localOff+end])
		}
		pos += infoSize
	}
	isUnicode := flags&0x80 != 0
	readStrData := func() string {
		if pos+2 > len(raw) {
			return ""
		}
		cnt := int(binary.LittleEndian.Uint16(raw[pos : pos+2]))
		pos += 2
		if cnt == 0 {
			return ""
		}
		if isUnicode {
			if pos+cnt*2 > len(raw) {
				return ""
			}
			b := raw[pos : pos+cnt*2]
			pos += cnt * 2
			return decodeUTF16LE(b)
		}
		if pos+cnt > len(raw) {
			return ""
		}
		s := string(raw[pos : pos+cnt])
		pos += cnt
		return s
	}
	if flags&0x04 != 0 { // HasName
		_ = readStrData()
	}
	if flags&0x08 != 0 { // HasRelativePath
		relPath = readStrData()
	}
	if flags&0x10 != 0 { // HasWorkingDir
		workDir = readStrData()
	}
	if flags&0x20 != 0 { // HasArguments
		args = readStrData()
	}

	rows = append(rows, pluginui.KV{Key: "Format", Value: "Windows Shortcut (.lnk)"})
	if targetPath != "" {
		rows = append(rows, pluginui.KV{Key: "Target", Value: targetPath})
	}
	if relPath != "" {
		rows = append(rows, pluginui.KV{Key: "Relative Path", Value: relPath})
	}
	if workDir != "" {
		rows = append(rows, pluginui.KV{Key: "Working Dir", Value: workDir})
	}
	if args != "" {
		rows = append(rows, pluginui.KV{Key: "Arguments", Value: args})
	}
	rows = append(rows, pluginui.KV{Key: "Target Size", Value: viewer.FormatBytes(uint64(fileSize))})

	return &Report{
		TitleSuffix: "Shortcut",
		Badges:      []string{"LNK"},
		Sections:    []Section{{Title: titleSummary, Rows: rows}},
	}
}

func bytesIndexZero(b []byte) int {
	for i, c := range b {
		if c == 0 {
			return i
		}
	}
	return len(b)
}

func decodeUTF16LE(b []byte) string {
	var runes []rune
	for i := 0; i+1 < len(b); i += 2 {
		v := binary.LittleEndian.Uint16(b[i : i+2])
		if v == 0 {
			break
		}
		runes = append(runes, rune(v))
	}
	return string(runes)
}

// PrefetchPlugin inspects Windows Prefetch (.pf) headers.
type PrefetchPlugin struct{}

func (PrefetchPlugin) ID() string              { return "prefetchviewer" }
func (PrefetchPlugin) Name() string            { return "PrefetchViewer" }
func (PrefetchPlugin) Priority() int           { return 78 }
func (PrefetchPlugin) CanHandle(p string) bool { return strings.EqualFold(filepath.Ext(p), ".pf") }
func (PrefetchPlugin) Prepare(path string, ctx *viewer.Context) {
	ctx.PreferredSize = core.Size{Width: 660, Height: 440}
	ctx.Title.Set(viewer.DisplayName(path))
}
func (PrefetchPlugin) View(cx *core.Scope, path string, ctx *viewer.Context) core.Node {
	tr := locales.For(cx)
	return renderReportViewer(cx, path, ctx, lucide.Cpu, func() (*Report, error) {
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		return InspectPrefetchBytes(raw, tr.DevSummary()), nil
	})
}

// InspectPrefetchBytes parses uncompressed SCCA and compressed MAM Windows Prefetch files.
func InspectPrefetchBytes(raw []byte, titleSummary string) *Report {
	var rows []pluginui.KV
	if len(raw) >= 8 && string(raw[:3]) == "MAM" {
		uncompSize := binary.LittleEndian.Uint32(raw[4:8])
		rows = []pluginui.KV{
			{Key: "Format", Value: "Windows 10/11 Prefetch (MAM Compressed)"},
			{Key: "Uncompressed Size", Value: viewer.FormatBytes(uint64(uncompSize))},
			{Key: "File Size", Value: viewer.FormatBytes(uint64(len(raw)))},
		}
		return &Report{TitleSuffix: "Prefetch MAM", Badges: []string{"PF", "MAM"}, Sections: []Section{{Title: titleSummary, Rows: rows}}}
	}
	if len(raw) >= 84 && string(raw[4:8]) == "SCCA" {
		ver := binary.LittleEndian.Uint32(raw[0:4])
		exeName := decodeUTF16LE(raw[16:76])
		hash := binary.LittleEndian.Uint32(raw[76:80])
		rows = []pluginui.KV{
			{Key: "Format", Value: fmt.Sprintf("Windows Prefetch (SCCA v%d)", ver)},
			{Key: "Executable", Value: exeName},
			{Key: "Prefetch Hash", Value: fmt.Sprintf("0x%08X", hash)},
			{Key: "File Size", Value: viewer.FormatBytes(uint64(len(raw)))},
		}
		return &Report{TitleSuffix: exeName, Badges: []string{"PF", "SCCA"}, Sections: []Section{{Title: titleSummary, Rows: rows}}}
	}
	return &Report{
		TitleSuffix: "Prefetch",
		Sections:    []Section{{Title: titleSummary, Rows: []pluginui.KV{{Key: "Size", Value: viewer.FormatBytes(uint64(len(raw)))}}}},
	}
}

// DumpPlugin inspects Windows Minidump (.dmp, .mdmp) headers and stream directories.
type DumpPlugin struct{}

func (DumpPlugin) ID() string    { return "dumpviewer" }
func (DumpPlugin) Name() string  { return "DumpViewer" }
func (DumpPlugin) Priority() int { return 78 }
func (DumpPlugin) CanHandle(p string) bool {
	ext := strings.ToLower(filepath.Ext(p))
	return ext == ".dmp" || ext == ".mdmp"
}
func (DumpPlugin) Prepare(path string, ctx *viewer.Context) {
	ctx.PreferredSize = core.Size{Width: 740, Height: 520}
	ctx.Title.Set(viewer.DisplayName(path))
}
func (DumpPlugin) View(cx *core.Scope, path string, ctx *viewer.Context) core.Node {
	tr := locales.For(cx)
	return renderReportViewer(cx, path, ctx, lucide.CircleAlert, func() (*Report, error) {
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		return InspectDumpBytes(raw, tr.DevSummary(), tr.DevStreams())
	})
}

// InspectDumpBytes parses a Windows MDMP minidump header and stream directory.
func InspectDumpBytes(raw []byte, titleSummary, titleStreams string) (*Report, error) {
	if len(raw) < 32 || string(raw[:4]) != "MDMP" {
		return nil, fmt.Errorf("not a valid MDMP crash dump header")
	}
	numStreams := int(binary.LittleEndian.Uint32(raw[8:12]))
	dirRVA := int(binary.LittleEndian.Uint32(raw[12:16]))
	ts := time.Unix(int64(binary.LittleEndian.Uint32(raw[20:24])), 0).UTC().Format("2006-01-02 15:04:05 UTC")
	flags := binary.LittleEndian.Uint64(raw[24:32])

	summary := []pluginui.KV{
		{Key: "Signature", Value: "MDMP"},
		{Key: "Streams", Value: fmt.Sprintf("%d", numStreams)},
		{Key: "Timestamp", Value: ts},
		{Key: "Flags", Value: fmt.Sprintf("0x%08X", flags)},
		{Key: "Size", Value: viewer.FormatBytes(uint64(len(raw)))},
	}

	var streamRows []pluginui.KV
	for i := 0; i < numStreams && dirRVA+i*12+12 <= len(raw); i++ {
		base := dirRVA + i*12
		stType := binary.LittleEndian.Uint32(raw[base : base+4])
		dataLen := binary.LittleEndian.Uint32(raw[base+4 : base+8])
		rva := binary.LittleEndian.Uint32(raw[base+8 : base+12])
		streamRows = append(streamRows, pluginui.KV{
			Key:   mdmpStreamName(stType),
			Value: fmt.Sprintf("%s @ RVA 0x%X", viewer.FormatBytes(uint64(dataLen)), rva),
		})
	}

	return &Report{
		TitleSuffix: "Minidump",
		Badges:      []string{"MDMP", fmt.Sprintf("%d streams", numStreams)},
		Sections: []Section{
			{Title: titleSummary, Rows: summary},
			{Title: titleStreams, Rows: streamRows},
		},
	}, nil
}

func mdmpStreamName(t uint32) string {
	switch t {
	case 3:
		return "ThreadListStream"
	case 4:
		return "ModuleListStream"
	case 5:
		return "MemoryListStream"
	case 6:
		return "ExceptionStream"
	case 7:
		return "SystemInfoStream"
	case 9:
		return "Memory64ListStream"
	case 15:
		return "MiscInfoStream"
	default:
		return fmt.Sprintf("Stream(%d)", t)
	}
}

// ChmPlugin inspects Microsoft Compiled HTML Help (.chm / ITSF) files.
type ChmPlugin struct{}

func (ChmPlugin) ID() string              { return "chmviewer" }
func (ChmPlugin) Name() string            { return "ChmViewer" }
func (ChmPlugin) Priority() int           { return 78 }
func (ChmPlugin) CanHandle(p string) bool { return strings.EqualFold(filepath.Ext(p), ".chm") }
func (ChmPlugin) Prepare(path string, ctx *viewer.Context) {
	ctx.PreferredSize = core.Size{Width: 700, Height: 480}
	ctx.Title.Set(viewer.DisplayName(path))
}
func (ChmPlugin) View(cx *core.Scope, path string, ctx *viewer.Context) core.Node {
	tr := locales.For(cx)
	return renderReportViewer(cx, path, ctx, lucide.BookOpen, func() (*Report, error) {
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		return InspectCHMBytes(raw, tr.DevSummary())
	})
}

// InspectCHMBytes parses the ITSF CHM header and scans internal HTML topic paths.
func InspectCHMBytes(raw []byte, titleSummary string) (*Report, error) {
	if len(raw) < 32 || string(raw[:4]) != "ITSF" {
		return nil, fmt.Errorf("not a valid ITSF (.chm) file")
	}
	ver := binary.LittleEndian.Uint32(raw[4:8])
	langID := binary.LittleEndian.Uint32(raw[20:24])
	rows := []pluginui.KV{
		{Key: "Format", Value: fmt.Sprintf("Compiled HTML Help (ITSF v%d)", ver)},
		{Key: "Language LCID", Value: fmt.Sprintf("0x%04X", langID)},
		{Key: "Size", Value: viewer.FormatBytes(uint64(len(raw)))},
	}
	return &Report{
		TitleSuffix: "CHM",
		Badges:      []string{"CHM", fmt.Sprintf("ITSF v%d", ver)},
		Sections:    []Section{{Title: titleSummary, Rows: rows}},
	}, nil
}

// QLPlugin inspects QuickLook plugin bundle files (.qlplugin).
type QLPlugin struct{}

func (QLPlugin) ID() string              { return "plugininstaller" }
func (QLPlugin) Name() string            { return "PluginInstaller" }
func (QLPlugin) Priority() int           { return 78 }
func (QLPlugin) CanHandle(p string) bool { return strings.EqualFold(filepath.Ext(p), ".qlplugin") }
func (QLPlugin) Prepare(path string, ctx *viewer.Context) {
	ctx.PreferredSize = core.Size{Width: 680, Height: 460}
	ctx.Title.Set(viewer.DisplayName(path))
}
func (QLPlugin) View(cx *core.Scope, path string, ctx *viewer.Context) core.Node {
	tr := locales.For(cx)
	return renderReportViewer(cx, path, ctx, lucide.Puzzle, func() (*Report, error) {
		zr, err := zip.OpenReader(path)
		if err != nil {
			return nil, err
		}
		defer zr.Close()
		var rows []pluginui.KV
		rows = append(rows, pluginui.KV{Key: "Note", Value: "NSGook ships built-in pure-Go viewers for QuickLook plugin formats."})
		for _, f := range zr.File {
			if strings.EqualFold(filepath.Base(f.Name), "QuickLook.Plugin.Metadata.config") {
				if b, err := readZipLimit(f, 64*1024); err == nil {
					rows = append(rows, pluginui.KV{Key: "Metadata XML", Value: extractMetadataSummary(b)})
				}
			}
			rows = append(rows, pluginui.KV{Key: f.Name, Value: viewer.FormatBytes(f.UncompressedSize64)})
			if len(rows) > 40 {
				break
			}
		}
		return &Report{
			TitleSuffix: "QuickLook Plugin Bundle",
			Badges:      []string{"QLPLUGIN"},
			Sections:    []Section{{Title: tr.DevSummary(), Rows: rows}},
		}, nil
	})
}

func extractMetadataSummary(b []byte) string {
	dec := xml.NewDecoder(strings.NewReader(string(b)))
	var parts []string
	var cur string
	for {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			cur = t.Name.Local
		case xml.CharData:
			val := strings.TrimSpace(string(t))
			if val != "" && (cur == "Namespace" || cur == "ApiVersion" || cur == "Description") {
				parts = append(parts, cur+"="+val)
			}
		}
	}
	return strings.Join(parts, " · ")
}
