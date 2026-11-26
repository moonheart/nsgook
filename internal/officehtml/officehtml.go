// Package officehtml renders Office documents as self-contained HTML for
// the web view, without needing Office or another converter installed.
package officehtml

import (
	"fmt"
	"html"
	"path/filepath"
	"strings"
)

// Supported reports whether path is an Office document this package renders.
func Supported(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".docx", ".xlsx", ".pptx":
		return true
	}
	return false
}

// Labels are the words the renderers put in their own UI, localized by the
// app; empty or nil fields use English defaults.
type Labels struct {
	EmptySheet string
	RowsLimit  func(rows int) string
	ColsLimit  func(cols int) string
}

// Render returns a self-contained HTML page showing path with English
// labels.
func Render(path string) (string, error) {
	return RenderWith(path, Labels{})
}

// RenderWith returns a self-contained HTML page showing path.
func RenderWith(path string, labels Labels) (string, error) {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".docx":
		return renderDOCX(path)
	case ".xlsx":
		return renderXLSX(path, labels)
	case ".pptx":
		return renderPPTX(path)
	}
	return "", fmt.Errorf("officehtml: unsupported file %q", path)
}

const pageCSS = `
*{box-sizing:border-box}
html,body{margin:0;padding:0}
body{background:#e8e8ec;color:#1f2328;font-family:"Segoe UI","Microsoft YaHei",system-ui,sans-serif;-webkit-font-smoothing:antialiased}
a{color:inherit}
`

// document wraps body in the page shell shared by the renderers. style is
// CSS for the format and is trusted: it never holds document text.
func document(title, style, body string) string {
	var b strings.Builder
	b.WriteString("<!doctype html><html><head><meta charset=\"utf-8\">")
	b.WriteString("<meta http-equiv=\"Content-Security-Policy\" content=\"default-src 'none'; img-src data:; style-src 'unsafe-inline'\">")
	b.WriteString("<meta name=\"viewport\" content=\"width=device-width, initial-scale=1\">")
	b.WriteString("<title>")
	b.WriteString(html.EscapeString(title))
	b.WriteString("</title><style>")
	b.WriteString(pageCSS)
	b.WriteString(style)
	b.WriteString("</style></head><body>")
	b.WriteString(body)
	b.WriteString("</body></html>")
	return b.String()
}
