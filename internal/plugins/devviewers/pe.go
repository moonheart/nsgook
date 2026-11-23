package devviewers

import (
	"debug/pe"
	"fmt"
	"os"
	"strconv"
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
		return InspectPE(path, tr.DevSummary(), tr.DevSections(), tr.DevImports(), tr.DevSymbols())
	})
}

// InspectPE parses PE headers, sections, imports, and COFF symbols using stdlib debug/pe.
func InspectPE(path, titleSummary, titleSections, titleImports, titleSymbols string) (*Report, error) {
	st, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	f, err := pe.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

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

	summary := []pluginui.KV{
		{Key: "Architecture", Value: arch + " (" + bitness + ")"},
		{Key: "Kind", Value: kind},
		{Key: "Subsystem", Value: subsys},
		{Key: "Timestamp", Value: ts},
		{Key: "File Size", Value: viewer.FormatBytes(uint64(max(0, st.Size())))},
		{Key: "Sections", Value: strconv.Itoa(len(f.Sections))},
	}

	secRows := make([]pluginui.KV, 0, len(f.Sections))
	for _, s := range f.Sections {
		secRows = append(secRows, pluginui.KV{
			Key:   s.Name,
			Value: fmt.Sprintf("VA 0x%X · Raw %s · Virt %s", s.VirtualAddress, viewer.FormatBytes(uint64(s.Size)), viewer.FormatBytes(uint64(s.VirtualSize))),
		})
	}

	var impRows []pluginui.KV
	if imps, err := f.ImportedSymbols(); err == nil {
		limit := min(len(imps), 250)
		for _, imp := range imps[:limit] {
			impRows = append(impRows, pluginui.KV{Key: "Import", Value: imp})
		}
		if len(imps) > limit {
			impRows = append(impRows, pluginui.KV{Key: "…", Value: fmt.Sprintf("+%d more", len(imps)-limit)})
		}
	}

	var symRows []pluginui.KV
	limit := min(len(f.Symbols), 200)
	for _, sym := range f.Symbols[:limit] {
		symRows = append(symRows, pluginui.KV{
			Key:   fmt.Sprintf("Sec %d", sym.SectionNumber),
			Value: sym.Name,
		})
	}

	tabs := []ReportTab{
		{ID: "sections", Label: fmt.Sprintf("%s (%d)", titleSections, len(secRows)), Sections: []Section{{Title: titleSections, Rows: secRows}}},
	}
	if len(impRows) > 0 {
		tabs = append(tabs, ReportTab{ID: "imports", Label: fmt.Sprintf("%s (%d)", titleImports, len(impRows)), Sections: []Section{{Title: titleImports, Rows: impRows}}})
	}
	if len(symRows) > 0 {
		tabs = append(tabs, ReportTab{ID: "symbols", Label: fmt.Sprintf("%s (%d)", titleSymbols, len(symRows)), Sections: []Section{{Title: titleSymbols, Rows: symRows}}})
	}

	return &Report{
		TitleSuffix: arch + " " + kind,
		Badges:      []string{arch, bitness, kind, subsys},
		Sections:    []Section{{Title: titleSummary, Rows: summary}},
		Tabs:        tabs,
	}, nil
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
