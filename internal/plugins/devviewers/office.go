package devviewers

import (
	"archive/zip"
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/xml"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/icons/lucide"

	"nsgook/internal/officehtml"
	"nsgook/internal/plugins/pluginui"
	"nsgook/internal/viewer"
	"nsgook/locales"
)

var officeExts = map[string]bool{
	".docx": true, ".xlsx": true, ".pptx": true,
	".odt": true, ".ods": true, ".odp": true,
	".epub": true, ".rtf": true,
}

// OfficePlugin provides pure-Go previews for Word/Excel/PowerPoint/ODF/EPUB/RTF documents.
type OfficePlugin struct{}

func (OfficePlugin) ID() string              { return "officeviewer" }
func (OfficePlugin) Name() string            { return "OfficeViewer" }
func (OfficePlugin) Priority() int           { return 80 }
func (OfficePlugin) CanHandle(p string) bool { return hasExt(p, officeExts) }

func (OfficePlugin) Prepare(path string, ctx *viewer.Context) {
	ctx.PreferredSize = core.Size{Width: 840, Height: 640}
	ctx.Title.Set(viewer.DisplayName(path))
}

func (OfficePlugin) View(cx *core.Scope, path string, ctx *viewer.Context) core.Node {
	tr := locales.For(cx)
	fallback := func(cx *core.Scope) core.Node {
		return renderReportViewer(cx, path, ctx, lucide.FileText, func() (*Report, error) {
			return InspectOffice(path, tr.DevSummary(), tr.DevSheets(), tr.DevSlides())
		})
	}
	if !cx.Env().HasWebView() || !officehtml.Supported(path) {
		return fallback(cx)
	}

	// The text report shows at once; the rendered layout replaces it when
	// the document has been parsed.
	htmlDoc := core.NewSignal(cx, "")
	var stopped atomic.Bool
	cx.Cleanup(func() {
		stopped.Store(true)
		htmlDoc.Set("")
	})
	labels := officehtml.Labels{
		EmptySheet: tr.OfficeEmptySheet(),
		RowsLimit:  func(rows int) string { return tr.OfficeTruncatedRows(strconv.Itoa(rows)) },
		ColsLimit:  func(cols int) string { return tr.OfficeTruncatedCols(strconv.Itoa(cols)) },
	}
	go func() {
		rendered, err := officehtml.RenderWith(path, labels)
		if err != nil || stopped.Load() {
			return
		}
		cx.Post(func() {
			if !stopped.Load() {
				htmlDoc.Set(rendered)
			}
		})
	}()
	return core.Show{
		When: func() bool { return htmlDoc.Get() != "" },
		Then: func(cx *core.Scope) core.Node {
			return renderOfficeHTML(cx, path, htmlDoc.Get(), ctx)
		},
		Else: fallback,
	}
}

// renderOfficeHTML shows the document's generated HTML in the web engine.
func renderOfficeHTML(cx *core.Scope, originalPath, htmlDoc string, ctx *viewer.Context) core.Node {
	tr := locales.For(cx)
	ctx.MoreItems.Set([]viewer.MenuAction{
		{
			ID:    "office-external",
			Label: tr.CommonOpenExternal(),
			Icon:  lucide.ExternalLink,
			OnSelect: func() {
				if ctx.Sys != nil {
					_ = ctx.Sys.OpenDefault(originalPath)
				}
			},
		},
	})
	source := core.WebHTML(htmlDoc, "")
	// WebView2 refuses HTML larger than 2 MB through its string source, and
	// documents with images pass it; serve those over localhost instead.
	if len(htmlDoc) > 1<<20 {
		if urlStr, stop, err := serveOfficeHTML(htmlDoc); err == nil {
			cx.Cleanup(stop)
			source = core.WebSrc(urlStr)
		}
	}
	return pluginui.Fill(core.WebView{
		Source:       source,
		Incognito:    true,
		NoJavaScript: true,
		PinchZoom:    true,
		Style:        core.Style{FlexGrow: 1, FlexShrink: 1, MinWidth: core.Pt(0), MinHeight: core.Pt(0)},
	})
}

// serveOfficeHTML serves a rendered document over a localhost URL with a
// random path, for pages too large to hand to the web view as a string.
func serveOfficeHTML(doc string) (string, func(), error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", nil, err
	}
	var tok [16]byte
	_, _ = rand.Read(tok[:])
	secret := "/" + hex.EncodeToString(tok[:])
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != secret {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write([]byte(doc))
	})}
	go func() { _ = srv.Serve(ln) }()
	return "http://" + ln.Addr().String() + secret, func() {
		_ = srv.Close()
		_ = ln.Close()
	}, nil
}

// InspectOffice extracts readable text, spreadsheet sheets, presentation slides, or EPUB/RTF content.
func InspectOffice(path, titleSummary, titleSheets, titleSlides string) (*Report, error) {
	ext := strings.ToLower(filepath.Ext(path))
	st, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if ext == ".rtf" {
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		return &Report{
			TitleSuffix: "RTF",
			Badges:      []string{"RTF"},
			Sections:    []Section{{Title: titleSummary, Rows: []pluginui.KV{{Key: "Format", Value: "Rich Text Format (.rtf)"}, {Key: "Size", Value: viewer.FormatBytes(uint64(len(raw)))}}}},
			BodyText:    stripRTF(string(raw)),
		}, nil
	}

	zr, err := zip.OpenReader(path)
	if err != nil {
		return nil, err
	}
	defer zr.Close()

	files := make(map[string]*zip.File, len(zr.File))
	for _, f := range zr.File {
		files[strings.ToLower(f.Name)] = f
	}

	readEntry := func(key string) []byte {
		f := files[strings.ToLower(key)]
		if f == nil {
			return nil
		}
		b, _ := readZipLimit(f, 4<<20)
		return b
	}

	summary := []pluginui.KV{
		{Key: "Format", Value: strings.TrimPrefix(strings.ToUpper(ext), ".")},
		{Key: "Size", Value: viewer.FormatBytes(uint64(max(0, st.Size())))},
		{Key: "Parts", Value: strconv.Itoa(len(zr.File))},
	}

	switch ext {
	case ".docx":
		docXML := readEntry("word/document.xml")
		txt := extractXMLParagraphs(docXML, map[string]bool{"p": true, "tr": true, "br": true})
		return &Report{
			TitleSuffix: "DOCX",
			Badges:      []string{"Word", "DOCX"},
			Sections:    []Section{{Title: titleSummary, Rows: summary}},
			BodyText:    txt,
		}, nil

	case ".xlsx":
		shared := parseXLSXSharedStrings(readEntry("xl/sharedStrings.xml"))
		sheetNames := parseXLSXSheetNames(readEntry("xl/workbook.xml"))
		var sheetFiles []string
		for name := range files {
			if strings.HasPrefix(name, "xl/worksheets/sheet") && strings.HasSuffix(name, ".xml") {
				sheetFiles = append(sheetFiles, name)
			}
		}
		sort.Strings(sheetFiles)
		var tabs []ReportTab
		for i, sf := range sheetFiles {
			label := fmt.Sprintf("Sheet %d", i+1)
			if i < len(sheetNames) && sheetNames[i] != "" {
				label = sheetNames[i]
			}
			tabs = append(tabs, ReportTab{
				ID:    fmt.Sprintf("sheet-%d", i),
				Label: label,
				Text:  formatXLSXSheet(readEntry(sf), shared),
			})
		}
		summary = append(summary, pluginui.KV{Key: titleSheets, Value: strconv.Itoa(len(tabs))})
		return &Report{
			TitleSuffix: "XLSX",
			Badges:      []string{"Excel", "XLSX"},
			Sections:    []Section{{Title: titleSummary, Rows: summary}},
			Tabs:        tabs,
		}, nil

	case ".pptx":
		var slideFiles []string
		for name := range files {
			if strings.HasPrefix(name, "ppt/slides/slide") && strings.HasSuffix(name, ".xml") {
				slideFiles = append(slideFiles, name)
			}
		}
		sort.SliceStable(slideFiles, func(i, j int) bool {
			return len(slideFiles[i]) < len(slideFiles[j]) || (len(slideFiles[i]) == len(slideFiles[j]) && slideFiles[i] < slideFiles[j])
		})
		var tabs []ReportTab
		for i, sf := range slideFiles {
			txt := extractXMLParagraphs(readEntry(sf), map[string]bool{"p": true, "br": true})
			tabs = append(tabs, ReportTab{
				ID:    fmt.Sprintf("slide-%d", i),
				Label: fmt.Sprintf("Slide %d", i+1),
				Text:  txt,
			})
		}
		summary = append(summary, pluginui.KV{Key: titleSlides, Value: strconv.Itoa(len(tabs))})
		return &Report{
			TitleSuffix: "PPTX",
			Badges:      []string{"PowerPoint", "PPTX"},
			Sections:    []Section{{Title: titleSummary, Rows: summary}},
			Tabs:        tabs,
		}, nil

	case ".odt", ".ods", ".odp":
		txt := extractXMLParagraphs(readEntry("content.xml"), map[string]bool{"p": true, "h": true, "table-row": true})
		return &Report{
			TitleSuffix: strings.TrimPrefix(strings.ToUpper(ext), "."),
			Badges:      []string{"OpenDocument"},
			Sections:    []Section{{Title: titleSummary, Rows: summary}},
			BodyText:    txt,
		}, nil

	case ".epub":
		var tabs []ReportTab
		var cover []byte
		for _, f := range zr.File {
			low := strings.ToLower(f.Name)
			if cover == nil && strings.Contains(low, "cover") && (strings.HasSuffix(low, ".png") || strings.HasSuffix(low, ".jpg") || strings.HasSuffix(low, ".jpeg")) {
				cover, _ = readZipLimit(f, 2<<20)
			}
			if (strings.HasSuffix(low, ".xhtml") || strings.HasSuffix(low, ".html") || strings.HasSuffix(low, ".htm")) && len(tabs) < 30 {
				if b, err := readZipLimit(f, 512*1024); err == nil {
					txt := extractXMLParagraphs(b, map[string]bool{"p": true, "h1": true, "h2": true, "h3": true, "li": true, "br": true})
					if strings.TrimSpace(txt) != "" {
						tabs = append(tabs, ReportTab{
							ID:    f.Name,
							Label: filepath.Base(f.Name),
							Text:  txt,
						})
					}
				}
			}
		}
		return &Report{
			TitleSuffix: "EPUB",
			Badges:      []string{"EPUB"},
			ImageData:   cover,
			Sections:    []Section{{Title: titleSummary, Rows: summary}},
			Tabs:        tabs,
		}, nil
	}

	return &Report{
		TitleSuffix: "Office",
		Sections:    []Section{{Title: titleSummary, Rows: summary}},
	}, nil
}

func extractXMLParagraphs(data []byte, lineBreaks map[string]bool) string {
	if len(data) == 0 {
		return ""
	}
	dec := xml.NewDecoder(bytes.NewReader(data))
	var out strings.Builder
	var cur strings.Builder
	for {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.CharData:
			cur.Write(bytes.Trim(t, "\r\n"))
		case xml.EndElement:
			if lineBreaks[t.Name.Local] {
				line := strings.TrimSpace(cur.String())
				if line != "" {
					out.WriteString(line)
					out.WriteByte('\n')
				}
				cur.Reset()
			}
		}
		if out.Len() > 256*1024 {
			break
		}
	}
	if rem := strings.TrimSpace(cur.String()); rem != "" {
		out.WriteString(rem)
	}
	return strings.TrimSpace(out.String())
}

func parseXLSXSharedStrings(data []byte) []string {
	if len(data) == 0 {
		return nil
	}
	dec := xml.NewDecoder(bytes.NewReader(data))
	var out []string
	var cur strings.Builder
	inSI := false
	for {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if t.Name.Local == "si" {
				inSI = true
				cur.Reset()
			}
		case xml.CharData:
			if inSI {
				cur.Write(t)
			}
		case xml.EndElement:
			if t.Name.Local == "si" {
				out = append(out, cur.String())
				inSI = false
			}
		}
	}
	return out
}

func parseXLSXSheetNames(data []byte) []string {
	if len(data) == 0 {
		return nil
	}
	dec := xml.NewDecoder(bytes.NewReader(data))
	var names []string
	for {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		if se, ok := tok.(xml.StartElement); ok && se.Name.Local == "sheet" {
			for _, a := range se.Attr {
				if a.Name.Local == "name" {
					names = append(names, a.Value)
				}
			}
		}
	}
	return names
}

func formatXLSXSheet(data []byte, shared []string) string {
	if len(data) == 0 {
		return ""
	}
	dec := xml.NewDecoder(bytes.NewReader(data))
	var rows strings.Builder
	var rowCells []string
	var cellType string
	var valBuf strings.Builder
	inVal := false
	for {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "row":
				rowCells = rowCells[:0]
			case "c":
				cellType = ""
				for _, a := range t.Attr {
					if a.Name.Local == "t" {
						cellType = a.Value
					}
				}
			case "v", "t":
				inVal = true
				valBuf.Reset()
			}
		case xml.CharData:
			if inVal {
				valBuf.Write(t)
			}
		case xml.EndElement:
			switch t.Name.Local {
			case "v", "t":
				inVal = false
				v := strings.TrimSpace(valBuf.String())
				if cellType == "s" {
					if idx, err := strconv.Atoi(v); err == nil && idx >= 0 && idx < len(shared) {
						v = shared[idx]
					}
				}
				rowCells = append(rowCells, v)
			case "row":
				if len(rowCells) > 0 {
					rows.WriteString(strings.Join(rowCells, "\t"))
					rows.WriteByte('\n')
				}
			}
		}
		if rows.Len() > 128*1024 {
			break
		}
	}
	return strings.TrimSpace(rows.String())
}

func stripRTF(src string) string {
	var out strings.Builder
	depth := 0
	skipGroup := false
	skipDepth := 0
	for i := 0; i < len(src); i++ {
		ch := src[i]
		switch ch {
		case '{':
			depth++
			if !skipGroup && (strings.HasPrefix(src[i:], `{\fonttbl`) || strings.HasPrefix(src[i:], `{\colortbl`) || strings.HasPrefix(src[i:], `{\stylesheet`)) {
				skipGroup = true
				skipDepth = depth
			}
		case '}':
			if skipGroup && depth == skipDepth {
				skipGroup = false
			}
			if depth > 0 {
				depth--
			}
		case '\\':
			if i+1 >= len(src) {
				break
			}
			i++
			next := src[i]
			if next == '\\' || next == '{' || next == '}' {
				if !skipGroup {
					out.WriteByte(next)
				}
				continue
			}
			if next == '\'' && i+2 < len(src) {
				if b, err := strconv.ParseUint(src[i+1:i+3], 16, 8); err == nil && !skipGroup {
					out.WriteByte(byte(b))
				}
				i += 2
				continue
			}
			j := i
			for j < len(src) && ((src[j] >= 'a' && src[j] <= 'z') || (src[j] >= 'A' && src[j] <= 'Z')) {
				j++
			}
			word := src[i:j]
			for j < len(src) && ((src[j] >= '0' && src[j] <= '9') || src[j] == '-') {
				j++
			}
			if j < len(src) && src[j] == ' ' {
				j++
			}
			i = j - 1
			if !skipGroup {
				switch word {
				case "par", "line":
					out.WriteByte('\n')
				case "tab":
					out.WriteByte('\t')
				}
			}
		case '\r', '\n':
		default:
			if !skipGroup {
				out.WriteByte(ch)
			}
		}
	}
	return strings.TrimSpace(out.String())
}
