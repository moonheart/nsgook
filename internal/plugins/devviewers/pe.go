package devviewers

import (
	"bytes"
	"debug/pe"
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/icons/lucide"

	"nsgook/internal/plugins/pluginui"
	"nsgook/internal/viewer"
	"nsgook/locales"
)

var peExts = map[string]bool{
	".exe": true, ".dll": true, ".sys": true, ".ocx": true,
	".cpl": true, ".scr": true, ".drv": true, ".efi": true,
}

// peWindowsInfo holds the shell-grade metadata Windows shows for an executable.
type peWindowsInfo struct {
	FileType       string
	Description    string
	Version        string
	ProductName    string
	ProductVersion string
	Company        string
	Copyright      string
	OriginalName   string
	Language       string
	Created        string
	Modified       string
	Accessed       string
	SizeOnDisk     uint64
	Attributes     string
	Security       string
	Signature      string
}

// PEPlugin inspects Windows Portable Executable binaries (.exe, .dll, .sys, .ocx, .efi, ...).
type PEPlugin struct{}

func (PEPlugin) ID() string              { return "peviewer" }
func (PEPlugin) Name() string            { return "PEViewer" }
func (PEPlugin) Priority() int           { return 82 }
func (PEPlugin) CanHandle(p string) bool { return hasExt(p, peExts) }

func (PEPlugin) Prepare(path string, ctx *viewer.Context) {
	ctx.PreferredSize = core.Size{Width: 820, Height: 600}
	ctx.Title.Set(viewer.DisplayName(path))
}

func (PEPlugin) View(cx *core.Scope, path string, ctx *viewer.Context) core.Node {
	tr := locales.For(cx)
	return renderReportViewer(cx, path, ctx, lucide.Cpu, func() (*Report, error) {
		report, err := InspectPE(path, tr.DevSummary(), tr.DevSections(), tr.DevImports(), tr.DevSymbols(), tr.DevRawDetails())
		if err == nil && report != nil {
			// Show the icon as its own update before the signature and imports
			// finish: icon-only extraction is milliseconds, while hashing the
			// executable can take seconds.
			later := report.Later
			report.Later = func() *Report {
				icon := shellIconPNG(ctx, path, 64)
				next := *report
				if len(icon) > 0 {
					next.IconData = icon
				}
				next.Later = func() *Report {
					final := report
					if later != nil {
						final = later()
					}
					if final != nil && len(icon) > 0 {
						final.IconData = icon
					}
					return final
				}
				return &next
			}
		}
		return report, err
	})
}

func shellIconPNG(ctx *viewer.Context, path string, size int) []byte {
	if ctx == nil || ctx.Sys == nil {
		return nil
	}
	img, err := ctx.Sys.GetShellIcon(path, size)
	if err != nil || img == nil {
		img, err = ctx.Sys.GetShellThumbnail(path, size)
	}
	if err != nil || img == nil {
		return nil
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil
	}
	return buf.Bytes()
}

// InspectPE parses PE headers, version resources, shell metadata, signatures, sections, imports, and symbols.
func InspectPE(path, titleSummary, titleSections, titleImports, titleSymbols, titleDetails string) (*Report, error) {
	st, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	f, err := pe.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	// Verifying Authenticode hashes the whole file, which takes seconds for a
	// large installer. Keep it off the first paint and fill it in afterwards.
	win := readPEFileInfo(path, st)
	win.Signature = "Checking…"

	arch := peMachineName(f.Machine)
	subsys := "Unknown"
	is64 := false
	switch oh := f.OptionalHeader.(type) {
	case *pe.OptionalHeader64:
		is64 = true
		subsys = peSubsystemName(oh.Subsystem)
	case *pe.OptionalHeader32:
		subsys = peSubsystemName(oh.Subsystem)
	}
	kind := "Executable"
	if f.Characteristics&pe.IMAGE_FILE_DLL != 0 {
		kind = "DLL"
	}

	ts := time.Unix(int64(f.TimeDateStamp), 0).UTC().Format("2006-01-02 15:04:05 UTC")
	bitness := "32-bit"
	if is64 {
		bitness = "64-bit"
	}

	techRows := []pluginui.KV{
		{Key: "Architecture", Value: arch + " (" + bitness + ")"},
		{Key: "Kind", Value: kind},
		{Key: "Subsystem", Value: subsys},
		{Key: "Timestamp", Value: ts},
		{Key: "Sections", Value: strconv.Itoa(len(f.Sections))},
	}

	summary := make([]pluginui.KV, 0, 9)
	addSummary := func(key, value string) {
		if value != "" {
			summary = append(summary, pluginui.KV{Key: key, Value: value})
		}
	}
	addSummary("File Description", win.Description)
	fileType := win.FileType
	if fileType == "" {
		fileType = kind
	}
	addSummary("Type", fileType)
	addSummary("File Version", win.Version)
	addSummary("Product Name", win.ProductName)
	addSummary("Product Version", win.ProductVersion)
	addSummary("Company", win.Company)
	addSummary("Copyright", win.Copyright)
	addSummary("Original Filename", win.OriginalName)
	addSummary("Language", win.Language)
	if len(summary) == 0 {
		summary = techRows
	}

	details := []pluginui.KV{
		{Key: "Location", Value: filepath.Dir(path)},
		{Key: "Size", Value: fmt.Sprintf("%s (%d B)", viewer.FormatBytes(uint64(max(0, st.Size()))), st.Size())},
	}
	if win.SizeOnDisk > 0 {
		details = append(details, pluginui.KV{Key: "Size on Disk", Value: fmt.Sprintf("%s (%d B)", viewer.FormatBytes(win.SizeOnDisk), win.SizeOnDisk)})
	}
	for _, row := range []pluginui.KV{
		{Key: "Created", Value: win.Created},
		{Key: "Modified", Value: win.Modified},
		{Key: "Accessed", Value: win.Accessed},
		{Key: "Attributes", Value: win.Attributes},
		{Key: "Security", Value: win.Security},
		{Key: "Digital Signature", Value: win.Signature},
	} {
		if row.Value != "" {
			details = append(details, row)
		}
	}

	secRows := make([]pluginui.KV, 0, len(f.Sections))
	for _, s := range f.Sections {
		secRows = append(secRows, pluginui.KV{
			Key:   s.Name,
			Value: fmt.Sprintf("VA 0x%X · Raw %s · Virt %s", s.VirtualAddress, viewer.FormatBytes(uint64(s.Size)), viewer.FormatBytes(uint64(s.VirtualSize))),
		})
	}

	// Imports and symbols, like the signature, can take seconds on large
	// binaries; readPEImports fills them in after the summary is shown.
	tabs := []ReportTab{
		{ID: "pe-details", Label: fmt.Sprintf("PE %s", titleDetails), Sections: []Section{{Title: fmt.Sprintf("PE %s", titleDetails), Rows: techRows}}},
		{ID: "sections", Label: fmt.Sprintf("%s (%d)", titleSections, len(secRows)), Sections: []Section{{Title: titleSections, Rows: secRows}}},
	}

	titleSuffix := arch + " " + kind
	if win.Description != "" {
		titleSuffix = win.Description
	}
	badges := []string{arch, bitness, kind, subsys}
	if win.Signature == "Valid" {
		badges = append(badges, "Signed")
	}

	report := &Report{
		TitleSuffix: titleSuffix,
		Badges:      badges,
		Sections: []Section{
			{Title: titleSummary, Rows: summary},
			{Title: titleDetails, Rows: details},
		},
		Tabs: tabs,
	}
	report.Later = func() *Report {
		signature, imports, symbols := readPEDetails(path)
		return reportWithDetails(report, signature, imports, symbols, titleImports, titleSymbols)
	}
	return report, nil
}

// readPEDetails reads a PE's slow metadata in parallel: its Authenticode
// signature, imports and COFF symbols. Any of them can take seconds on a large
// binary, so none belongs on the path to the first paint.
func readPEDetails(path string) (signature string, imports, symbols []pluginui.KV) {
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		signature = verifyAuthenticode(path)
	}()
	go func() {
		defer wg.Done()
		imports, symbols = readPEImports(path)
	}()
	wg.Wait()
	return signature, imports, symbols
}

// readPEImports reads the imports and COFF symbols a PE report lists.
func readPEImports(path string) (imports, symbols []pluginui.KV) {
	f, err := pe.Open(path)
	if err != nil {
		return nil, nil
	}
	defer f.Close()

	if imps, err := f.ImportedSymbols(); err == nil {
		limit := min(len(imps), 250)
		for _, imp := range imps[:limit] {
			imports = append(imports, pluginui.KV{Key: "Import", Value: imp})
		}
		if len(imps) > limit {
			imports = append(imports, pluginui.KV{Key: "…", Value: fmt.Sprintf("+%d more", len(imps)-limit)})
		}
	}
	limit := min(len(f.Symbols), 200)
	for _, sym := range f.Symbols[:limit] {
		symbols = append(symbols, pluginui.KV{
			Key:   fmt.Sprintf("Sec %d", sym.SectionNumber),
			Value: sym.Name,
		})
	}
	return imports, symbols
}

// reportWithDetails returns a copy of a PE report with the signature row and
// Signed badge set, and the imports and symbols tabs appended.
func reportWithDetails(base *Report, signature string, imports, symbols []pluginui.KV, titleImports, titleSymbols string) *Report {
	if base == nil {
		return nil
	}
	next := *base
	next.Later = nil
	next.Badges = make([]string, 0, len(base.Badges)+1)
	for _, badge := range base.Badges {
		if badge != "Signed" {
			next.Badges = append(next.Badges, badge)
		}
	}
	if signature == "Valid" {
		next.Badges = append(next.Badges, "Signed")
	}
	next.Sections = make([]Section, len(base.Sections))
	for i, section := range base.Sections {
		rows := make([]pluginui.KV, len(section.Rows))
		copy(rows, section.Rows)
		for j := range rows {
			if rows[j].Key == "Digital Signature" {
				rows[j].Value = signature
			}
		}
		next.Sections[i] = Section{Title: section.Title, Rows: rows}
	}
	next.Tabs = append([]ReportTab(nil), base.Tabs...)
	if len(imports) > 0 {
		next.Tabs = append(next.Tabs, ReportTab{ID: "imports", Label: fmt.Sprintf("%s (%d)", titleImports, len(imports)), Sections: []Section{{Title: titleImports, Rows: imports}}})
	}
	if len(symbols) > 0 {
		next.Tabs = append(next.Tabs, ReportTab{ID: "symbols", Label: fmt.Sprintf("%s (%d)", titleSymbols, len(symbols)), Sections: []Section{{Title: titleSymbols, Rows: symbols}}})
	}
	return &next
}

func peMachineName(m uint16) string {
	switch m {
	case pe.IMAGE_FILE_MACHINE_AMD64:
		return "x86_64"
	case pe.IMAGE_FILE_MACHINE_I386:
		return "x86"
	case pe.IMAGE_FILE_MACHINE_ARM64:
		return "ARM64"
	case pe.IMAGE_FILE_MACHINE_ARMNT:
		return "ARMv7"
	default:
		return fmt.Sprintf("0x%04X", m)
	}
}

func peSubsystemName(s uint16) string {
	switch s {
	case pe.IMAGE_SUBSYSTEM_WINDOWS_GUI:
		return "Windows GUI"
	case pe.IMAGE_SUBSYSTEM_WINDOWS_CUI:
		return "Windows Console"
	case pe.IMAGE_SUBSYSTEM_NATIVE:
		return "Native Driver"
	case pe.IMAGE_SUBSYSTEM_EFI_APPLICATION:
		return "EFI Application"
	default:
		return fmt.Sprintf("Subsystem %d", s)
	}
}
