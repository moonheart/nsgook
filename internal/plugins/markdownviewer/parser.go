// Package markdownviewer implements NSGook's Markdown, Mermaid, AsciiDoc, RST, and Jupyter Notebook viewer.
package markdownviewer

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
)

// Heading represents a Markdown ATX heading extracted for the table of contents.
type Heading struct {
	Level int
	Title string
	Line  int
}

var supportedExts = map[string]bool{
	".md": true, ".markdown": true, ".mdx": true, ".mmd": true, ".mermaid": true,
	".mkd": true, ".mdwn": true, ".mdown": true, ".mdc": true, ".qmd": true,
	".rmd": true, ".rmarkdown": true, ".apib": true, ".mdtxt": true, ".mdtext": true,
	".ipynb": true, ".adoc": true, ".asciidoc": true, ".rst": true, ".restructuredtext": true,
}

func isSupportedExt(ext string) bool {
	return supportedExts[strings.ToLower(ext)]
}

// ExtractHeadings extracts ATX headings (# .. ######) outside fenced code blocks.
func ExtractHeadings(md string) []Heading {
	var out []Heading
	lines := strings.Split(strings.ReplaceAll(md, "\r\n", "\n"), "\n")
	inFence := false
	fenceChar := byte(0)

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			ch := trimmed[0]
			if !inFence {
				inFence = true
				fenceChar = ch
			} else if ch == fenceChar {
				inFence = false
			}
			continue
		}
		if inFence || !strings.HasPrefix(trimmed, "#") {
			continue
		}
		lvl := 0
		for lvl < len(trimmed) && lvl < 6 && trimmed[lvl] == '#' {
			lvl++
		}
		if lvl == 0 || lvl >= len(trimmed) || trimmed[lvl] != ' ' {
			continue
		}
		title := strings.TrimSpace(strings.TrimRight(trimmed[lvl+1:], "# "))
		if title != "" {
			out = append(out, Heading{Level: lvl, Title: title, Line: i + 1})
		}
	}
	return out
}

// PrepareMarkdown turns raw document text into Markdown suitable for ui.TextView plus raw source language.
func PrepareMarkdown(path, decoded string) (markdown string, rawLang string) {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".mermaid", ".mmd":
		if looksLikeMermaid(decoded) {
			return "```mermaid\n" + strings.TrimSpace(decoded) + "\n```\n", "mermaid"
		}
		return decoded, "markdown"
	case ".ipynb":
		if md, err := ConvertIPYNB([]byte(decoded)); err == nil {
			return md, "json"
		}
		return decoded, "json"
	case ".adoc", ".asciidoc":
		return convertAsciiDoc(decoded), "asciidoc"
	case ".rst", ".restructuredtext":
		return decoded, "rst"
	default:
		return decoded, "markdown"
	}
}

func looksLikeMermaid(s string) bool {
	trimmed := strings.TrimSpace(s)
	for _, line := range strings.Split(trimmed, "\n") {
		l := strings.TrimSpace(line)
		if l == "" || strings.HasPrefix(l, "%%") {
			continue
		}
		for _, kw := range []string{
			"graph ", "flowchart ", "sequenceDiagram", "classDiagram",
			"stateDiagram", "erDiagram", "gantt", "pie", "gitGraph",
			"mindmap", "timeline", "journey", "quadrantChart",
		} {
			if strings.HasPrefix(l, kw) {
				return true
			}
		}
		return false
	}
	return false
}

// ConvertIPYNB renders a Jupyter Notebook (.ipynb) JSON document into Markdown.
func ConvertIPYNB(raw []byte) (string, error) {
	var nb struct {
		Metadata struct {
			KernelSpec struct {
				Language string `json:"language"`
			} `json:"kernelspec"`
			LanguageInfo struct {
				Name string `json:"name"`
			} `json:"language_info"`
		} `json:"metadata"`
		Cells []struct {
			CellType string          `json:"cell_type"`
			Source   json.RawMessage `json:"source"`
			Outputs  []struct {
				OutputType string                     `json:"output_type"`
				Text       json.RawMessage            `json:"text"`
				Data       map[string]json.RawMessage `json:"data"`
				EName      string                     `json:"ename"`
				EValue     string                     `json:"evalue"`
			} `json:"outputs"`
		} `json:"cells"`
	}
	if err := json.Unmarshal(raw, &nb); err != nil {
		return "", err
	}

	lang := nb.Metadata.LanguageInfo.Name
	if lang == "" {
		lang = nb.Metadata.KernelSpec.Language
	}
	if lang == "" {
		lang = "python"
	}

	var b strings.Builder
	for i, cell := range nb.Cells {
		src := decodeStringOrLines(cell.Source)
		if strings.TrimSpace(src) == "" && len(cell.Outputs) == 0 {
			continue
		}
		switch cell.CellType {
		case "markdown":
			b.WriteString(src)
			b.WriteString("\n\n")
		case "code":
			fmt.Fprintf(&b, "```%s\n%s\n```\n\n", lang, strings.TrimRight(src, "\n"))
			for _, out := range cell.Outputs {
				switch out.OutputType {
				case "stream":
					if txt := decodeStringOrLines(out.Text); txt != "" {
						fmt.Fprintf(&b, "> Output:\n> ```text\n%s\n> ```\n\n", prefixLines(strings.TrimRight(txt, "\n"), "> "))
					}
				case "execute_result", "display_data":
					if mdRaw, ok := out.Data["text/markdown"]; ok {
						b.WriteString(decodeStringOrLines(mdRaw))
						b.WriteString("\n\n")
					} else if txtRaw, ok := out.Data["text/plain"]; ok {
						txt := decodeStringOrLines(txtRaw)
						fmt.Fprintf(&b, "```text\n%s\n```\n\n", strings.TrimRight(txt, "\n"))
					}
				case "error":
					fmt.Fprintf(&b, "> **%s**: %s\n\n", out.EName, out.EValue)
				}
			}
		default:
			if i > 0 {
				b.WriteString("\n")
			}
			fmt.Fprintf(&b, "```text\n%s\n```\n\n", strings.TrimRight(src, "\n"))
		}
	}
	return b.String(), nil
}

func decodeStringOrLines(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var single string
	if err := json.Unmarshal(raw, &single); err == nil {
		return single
	}
	var lines []string
	if err := json.Unmarshal(raw, &lines); err == nil {
		return strings.Join(lines, "")
	}
	return ""
}

func prefixLines(s, prefix string) string {
	lines := strings.Split(s, "\n")
	for i := range lines {
		lines[i] = prefix + lines[i]
	}
	return strings.Join(lines, "\n")
}

func convertAsciiDoc(s string) string {
	lines := strings.Split(strings.ReplaceAll(s, "\r\n", "\n"), "\n")
	out := make([]string, 0, len(lines))
	inSource := false
	pendingLang := ""
	for _, line := range lines {
		trim := strings.TrimSpace(line)
		if strings.HasPrefix(trim, "[source") {
			inner := strings.TrimSuffix(strings.TrimPrefix(trim, "[source"), "]")
			inner = strings.TrimPrefix(inner, ",")
			pendingLang = strings.TrimSpace(inner)
			continue
		}
		if trim == "----" {
			if !inSource {
				inSource = true
				out = append(out, "```"+pendingLang)
				pendingLang = ""
			} else {
				inSource = false
				out = append(out, "```")
			}
			continue
		}
		if !inSource && strings.HasPrefix(trim, "=") {
			lvl := 0
			for lvl < len(trim) && lvl < 6 && trim[lvl] == '=' {
				lvl++
			}
			if lvl > 0 && lvl < len(trim) && trim[lvl] == ' ' {
				out = append(out, strings.Repeat("#", lvl)+" "+strings.TrimSpace(trim[lvl+1:]))
				continue
			}
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}
