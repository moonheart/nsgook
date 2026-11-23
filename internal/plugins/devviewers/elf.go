package devviewers

import (
	"debug/elf"
	"debug/macho"
	"fmt"
	"strconv"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/icons/lucide"

	"nsgook/internal/plugins/pluginui"
	"nsgook/internal/viewer"
	"nsgook/locales"
)

var elfExts = map[string]bool{
	".elf": true, ".so": true, ".o": true, ".ko": true,
	".out": true, ".dylib": true, ".macho": true,
}

// ELFPlugin inspects ELF and Mach-O executables/shared libraries using stdlib debug/elf and debug/macho.
type ELFPlugin struct{}

func (ELFPlugin) ID() string              { return "elfviewer" }
func (ELFPlugin) Name() string            { return "ELFViewer" }
func (ELFPlugin) Priority() int           { return 82 }
func (ELFPlugin) CanHandle(p string) bool { return hasExt(p, elfExts) }

func (ELFPlugin) Prepare(path string, ctx *viewer.Context) {
	ctx.PreferredSize = core.Size{Width: 820, Height: 600}
	ctx.Title.Set(viewer.DisplayName(path))
}

func (ELFPlugin) View(cx *core.Scope, path string, ctx *viewer.Context) core.Node {
	tr := locales.For(cx)
	return renderReportViewer(cx, path, ctx, lucide.Cpu, func() (*Report, error) {
		return InspectBinaryObj(path, tr.DevSummary(), tr.DevSections(), tr.DevImports(), tr.DevSymbols())
	})
}

// InspectBinaryObj parses either ELF or Mach-O headers and sections.
func InspectBinaryObj(path, titleSummary, titleSections, titleImports, titleSymbols string) (*Report, error) {
	if ef, err := elf.Open(path); err == nil {
		defer ef.Close()
		summary := []pluginui.KV{
			{Key: "Format", Value: fmt.Sprintf("ELF (%s, %s)", ef.Class, ef.Data)},
			{Key: "Machine", Value: ef.Machine.String()},
			{Key: "Type", Value: ef.Type.String()},
			{Key: "Entry Point", Value: fmt.Sprintf("0x%X", ef.Entry)},
			{Key: "Sections", Value: strconv.Itoa(len(ef.Sections))},
		}
		var secRows []pluginui.KV
		for _, s := range ef.Sections {
			secRows = append(secRows, pluginui.KV{
				Key:   s.Name,
				Value: fmt.Sprintf("%s · Addr 0x%X · %s", s.Type.String(), s.Addr, viewer.FormatBytes(s.Size)),
			})
		}
		var libRows []pluginui.KV
		if libs, err := ef.ImportedLibraries(); err == nil {
			for _, l := range libs {
				libRows = append(libRows, pluginui.KV{Key: "Needed", Value: l})
			}
		}
		var symRows []pluginui.KV
		if syms, err := ef.DynamicSymbols(); err == nil {
			limit := min(len(syms), 250)
			for _, sy := range syms[:limit] {
				symRows = append(symRows, pluginui.KV{Key: fmt.Sprintf("0x%X", sy.Value), Value: sy.Name})
			}
		}
		tabs := []ReportTab{
			{ID: "sections", Label: fmt.Sprintf("%s (%d)", titleSections, len(secRows)), Sections: []Section{{Title: titleSections, Rows: secRows}}},
		}
		if len(libRows) > 0 {
			tabs = append(tabs, ReportTab{ID: "libs", Label: fmt.Sprintf("%s (%d)", titleImports, len(libRows)), Sections: []Section{{Title: titleImports, Rows: libRows}}})
		}
		if len(symRows) > 0 {
			tabs = append(tabs, ReportTab{ID: "syms", Label: fmt.Sprintf("%s (%d)", titleSymbols, len(symRows)), Sections: []Section{{Title: titleSymbols, Rows: symRows}}})
		}
		return &Report{
			TitleSuffix: "ELF " + ef.Machine.String(),
			Badges:      []string{"ELF", ef.Class.String(), ef.Machine.String(), ef.Type.String()},
			Sections:    []Section{{Title: titleSummary, Rows: summary}},
			Tabs:        tabs,
		}, nil
	}

	mf, err := macho.Open(path)
	if err != nil {
		return nil, err
	}
	defer mf.Close()
	summary := []pluginui.KV{
		{Key: "Format", Value: "Mach-O"},
		{Key: "CPU", Value: mf.Cpu.String()},
		{Key: "Type", Value: mf.Type.String()},
		{Key: "Sections", Value: strconv.Itoa(len(mf.Sections))},
	}
	var secRows []pluginui.KV
	for _, s := range mf.Sections {
		secRows = append(secRows, pluginui.KV{
			Key:   s.Seg + "," + s.Name,
			Value: fmt.Sprintf("Addr 0x%X · %s", s.Addr, viewer.FormatBytes(s.Size)),
		})
	}
	var libRows []pluginui.KV
	if libs, err := mf.ImportedLibraries(); err == nil {
		for _, l := range libs {
			libRows = append(libRows, pluginui.KV{Key: "Load Dylib", Value: l})
		}
	}
	return &Report{
		TitleSuffix: "Mach-O " + mf.Cpu.String(),
		Badges:      []string{"Mach-O", mf.Cpu.String(), mf.Type.String()},
		Sections:    []Section{{Title: titleSummary, Rows: summary}},
		Tabs: []ReportTab{
			{ID: "sections", Label: titleSections, Sections: []Section{{Title: titleSections, Rows: secRows}}},
			{ID: "libs", Label: titleImports, Sections: []Section{{Title: titleImports, Rows: libRows}}},
		},
	}, nil
}
