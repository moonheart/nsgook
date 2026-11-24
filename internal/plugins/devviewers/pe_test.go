package devviewers

import (
	"testing"

	"nsgook/internal/plugins/pluginui"
)

func TestReportWithDetails(t *testing.T) {
	base := &Report{
		TitleSuffix: "Orca 2.0",
		Badges:      []string{"x86_64", "64-bit", "Executable", "Windows GUI", "Signed"},
		Sections: []Section{
			{Title: "Summary", Rows: []pluginui.KV{{Key: "Type", Value: "Application"}}},
			{Title: "Details", Rows: []pluginui.KV{{Key: "Digital Signature", Value: "Checking…"}}},
		},
		Tabs: []ReportTab{
			{ID: "pe-details", Label: "PE Details"},
			{ID: "sections", Label: "Sections"},
		},
	}
	base.Later = func() *Report { return nil }

	next := reportWithDetails(base, "Valid", []pluginui.KV{{Key: "Import", Value: "A.dll!f"}}, nil, "Imports", "Symbols")
	if next.Later != nil {
		t.Fatal("patched report must not verify again")
	}
	if got := len(next.Badges); got != 5 || next.Badges[4] != "Signed" {
		t.Fatalf("badges = %v, want one Signed badge", next.Badges)
	}
	if got := next.Sections[1].Rows[0].Value; got != "Valid" {
		t.Fatalf("signature row = %q, want Valid", got)
	}
	if got := base.Sections[1].Rows[0].Value; got != "Checking…" {
		t.Fatalf("base report mutated: signature row = %q", got)
	}
	if got := len(next.Tabs); got != 3 || next.Tabs[2].ID != "imports" {
		t.Fatalf("tabs = %v, want an imports tab", next.Tabs)
	}
	if len(base.Tabs) != 2 {
		t.Fatalf("base tabs mutated: %v", base.Tabs)
	}

	unsigned := reportWithDetails(base, "Not signed", nil, nil, "Imports", "Symbols")
	if got := len(unsigned.Badges); got != 4 {
		t.Fatalf("unsigned badges = %v, want Signed removed", unsigned.Badges)
	}
	if got := unsigned.Sections[1].Rows[0].Value; got != "Not signed" {
		t.Fatalf("unsigned signature row = %q", got)
	}
	if got := len(unsigned.Tabs); got != 2 {
		t.Fatalf("unsigned tabs = %v, want no imports tab", unsigned.Tabs)
	}
}
