package markdownviewer

import (
	"strings"
	"testing"
)

func TestExtractHeadingsSkipsCodeBlocks(t *testing.T) {
	md := "# Intro\n\n```python\n# not a heading\n```\n\n## Details\n### Sub ###\n"
	got := ExtractHeadings(md)
	if len(got) != 3 {
		t.Fatalf("expected 3 headings, got %d: %+v", len(got), got)
	}
	if got[0].Level != 1 || got[0].Title != "Intro" {
		t.Fatalf("unexpected heading 0: %+v", got[0])
	}
	if got[2].Level != 3 || got[2].Title != "Sub" {
		t.Fatalf("unexpected heading 2: %+v", got[2])
	}
}

func TestConvertIPYNBAndMermaid(t *testing.T) {
	ipynb := `{
	  "metadata": {"language_info": {"name": "python"}},
	  "cells": [
	    {"cell_type": "markdown", "source": ["# Title\n", "Summary"]},
	    {"cell_type": "code", "source": "print(42)", "outputs": [
	      {"output_type": "stream", "text": ["42\n"]}
	    ]}
	  ]
	}`
	md, err := ConvertIPYNB([]byte(ipynb))
	if err != nil {
		t.Fatalf("ConvertIPYNB failed: %v", err)
	}
	if !strings.Contains(md, "# Title") || !strings.Contains(md, "```python\nprint(42)\n```") || !strings.Contains(md, "42") {
		t.Fatalf("unexpected ipynb markdown:\n%s", md)
	}

	mmd, lang := PrepareMarkdown("diagram.mmd", "flowchart LR\nA-->B\n")
	if lang != "mermaid" || !strings.HasPrefix(mmd, "```mermaid\n") {
		t.Fatalf("unexpected mermaid preparation: lang=%q md=%q", lang, mmd)
	}
}
