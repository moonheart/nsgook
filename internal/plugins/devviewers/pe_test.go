package devviewers

import (
	"testing"

	"nsgook/internal/plugins/pluginui"
)

func TestReportWithSignature(t *testing.T) {
	base := &Report{
		TitleSuffix: "Orca 2.0",
		Badges:      []string{"x86_64", "64-bit", "Executable", "Windows GUI", "Signed"},
		Sections: []Section{
			{Title: "Summary", Rows: []pluginui.KV{{Key: "Type", Value: "Application"}}},
			{Title: "Details", Rows: []pluginui.KV{{Key: "Digital Signature", Value: "Checking…"}}},
		},
	}
	base.Later = func() *Report { return nil }

	next := reportWithSignature(base, "Valid")
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

	unsigned := reportWithSignature(base, "Not signed")
	if got := len(unsigned.Badges); got != 4 {
		t.Fatalf("unsigned badges = %v, want Signed removed", unsigned.Badges)
	}
	if got := unsigned.Sections[1].Rows[0].Value; got != "Not signed" {
		t.Fatalf("unsigned signature row = %q", got)
	}
}
