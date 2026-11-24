// Package pdfviewer implements NSGook's PDF previewer using metadata inspection and WebView2 rendering.
package pdfviewer

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/icons/lucide"
	"github.com/nsgo-dev/nsgo/ui"

	"nsgook/internal/plugins/pluginui"
	"nsgook/internal/viewer"
	"nsgook/locales"
)

// PDFMeta holds basic structural metadata parsed directly from a PDF file.
type PDFMeta struct {
	Version string
	Pages   int
	Title   string
	Author  string
	Size    uint64
}

var (
	rePageType = regexp.MustCompile(`/Type\s*/Page\b`)
	reTitle    = regexp.MustCompile(`/Title\s*\(([^)]{1,200})\)`)
	reAuthor   = regexp.MustCompile(`/Author\s*\(([^)]{1,200})\)`)
)

// pdfMetaCache keeps the last few inspections: Prepare and View both read the
// same metadata, and a large PDF otherwise gets read twice on the way to its
// first page.
var pdfMetaCache = struct {
	sync.Mutex
	entries map[string]PDFMeta
}{entries: make(map[string]PDFMeta)}

// InspectPDF extracts PDF version, approximate page count, title, and author in pure Go.
func InspectPDF(path string) PDFMeta {
	info, err := os.Stat(path)
	key := path
	if err == nil {
		key = fmt.Sprintf("%s\x00%d\x00%d", path, info.Size(), info.ModTime().UnixNano())
	}
	pdfMetaCache.Lock()
	meta, ok := pdfMetaCache.entries[key]
	pdfMetaCache.Unlock()
	if ok {
		return meta
	}

	meta = inspectPDF(path, info)
	pdfMetaCache.Lock()
	if len(pdfMetaCache.entries) >= 32 {
		clear(pdfMetaCache.entries)
	}
	pdfMetaCache.entries[key] = meta
	pdfMetaCache.Unlock()
	return meta
}

func inspectPDF(path string, info os.FileInfo) PDFMeta {
	var meta PDFMeta
	if info != nil {
		meta.Size = uint64(max(0, info.Size()))
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return meta
	}
	if idx := bytes.Index(data[:min(len(data), 64)], []byte("%PDF-")); idx >= 0 {
		line := data[idx+5 : min(len(data), idx+16)]
		if end := bytes.IndexAny(line, "\r\n "); end > 0 {
			meta.Version = string(line[:end])
		} else {
			meta.Version = strings.TrimSpace(string(line))
		}
	}
	meta.Pages = len(rePageType.FindAllIndex(data, -1))
	if meta.Pages == 0 {
		// Fallback to /Count N inside Pages tree.
		reCount := regexp.MustCompile(`/Count\s+(\d+)`)
		for _, m := range reCount.FindAllSubmatch(data, 8) {
			if n, err := strconv.Atoi(string(m[1])); err == nil && n > meta.Pages {
				meta.Pages = n
			}
		}
	}
	if m := reTitle.FindSubmatch(data); len(m) == 2 {
		meta.Title = cleanPDFString(m[1])
	}
	if m := reAuthor.FindSubmatch(data); len(m) == 2 {
		meta.Author = cleanPDFString(m[1])
	}
	return meta
}

func cleanPDFString(b []byte) string {
	s := strings.ReplaceAll(string(b), `\(`, "(")
	s = strings.ReplaceAll(s, `\)`, ")")
	return strings.TrimSpace(s)
}

// Plugin previews .pdf files via an ephemeral local HTTP endpoint in core.WebView.
type Plugin struct{}

func New() viewer.Plugin { return Plugin{} }

func (Plugin) ID() string    { return "pdfviewer" }
func (Plugin) Name() string  { return "PdfViewer" }
func (Plugin) Priority() int { return -1 }

func (Plugin) CanHandle(path string) bool {
	if path == "" || strings.HasPrefix(path, "::") {
		return false
	}
	return strings.EqualFold(filepath.Ext(path), ".pdf")
}

func (Plugin) Prepare(path string, ctx *viewer.Context) {
	ctx.PreferredSize = core.Size{Width: 920, Height: 760}
	meta := InspectPDF(path)
	name := viewer.DisplayName(path)
	if meta.Pages > 0 {
		ctx.Title.Set(fmt.Sprintf("%s (%d p.)", name, meta.Pages))
	} else {
		ctx.Title.Set(name)
	}
}

func (Plugin) View(cx *core.Scope, path string, ctx *viewer.Context) core.Node {
	colors := ui.ColorsOf(cx)
	tr := locales.For(cx)
	meta := InspectPDF(path)

	if !cx.Env().HasWebView() {
		return renderMetadataFallback(cx, path, meta, colors, tr)
	}

	pdfURL, stopServer, err := serveLocalPDF(path)
	if err != nil {
		return renderMetadataFallback(cx, path, meta, colors, tr)
	}
	cx.Cleanup(stopServer)

	webRef := &core.Ref{}
	ctx.MoreItems.Set([]viewer.MenuAction{
		{
			ID:       "pdf-reload",
			Label:    tr.WebReload(),
			Icon:     lucide.RotateCw,
			OnSelect: webRef.Reload,
		},
		{
			ID:    "pdf-external",
			Label: tr.CommonOpenExternal(),
			Icon:  lucide.ExternalLink,
			OnSelect: func() {
				if ctx.Sys != nil {
					_ = ctx.Sys.OpenDefault(path)
				}
			},
		},
	})

	return pluginui.Fill(
		core.WebView{
			Ref:       webRef,
			Source:    core.WebSrc(pdfURL),
			Incognito: true,
			PinchZoom: true,
			Style:     core.Style{FlexGrow: 1, FlexShrink: 1, MinWidth: core.Pt(0), MinHeight: core.Pt(0)},
		},
	)
}

func renderMetadataFallback(cx *core.Scope, path string, meta PDFMeta, colors ui.Colors, tr locales.Messages) core.Node {
	rows := []pluginui.KV{
		{Key: tr.InfoKind(), Value: tr.PdfSummary()},
		{Key: tr.PdfVersion(), Value: meta.Version},
		{Key: tr.PdfPages(), Value: strconv.Itoa(max(0, meta.Pages))},
		{Key: tr.InfoSize(viewer.FormatBytes(meta.Size)), Value: viewer.FormatBytes(meta.Size)},
	}
	if meta.Title != "" {
		rows = append(rows, pluginui.KV{Key: tr.PdfTitle(), Value: meta.Title})
	}
	if meta.Author != "" {
		rows = append(rows, pluginui.KV{Key: tr.PdfAuthor(), Value: meta.Author})
	}
	return core.Scroll{
		Style: core.Style{FlexGrow: 1, FlexShrink: 1, MinHeight: core.Pt(0)},
		Children: []core.Node{
			core.View{
				Style: core.Style{Padding: core.All(24), Gap: 16},
				Children: []core.Node{
					core.View{
						Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 12},
						Children: []core.Node{
							ui.IconOf(lucide.FileText, 36, colors.Accent),
							ui.Text(viewer.DisplayName(path), 18, ui.Semibold, colors.Text),
						},
					},
					pluginui.SectionCard(cx, tr.PdfSummary(), rows, tr.CommonCopy(), cx.SetClipboard),
					ui.Text(tr.WebUnavailable(), 12, ui.Regular, colors.Muted),
				},
			},
		},
	}
}

func serveLocalPDF(path string) (urlStr string, stop func(), err error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", nil, err
	}
	var tok [16]byte
	_, _ = rand.Read(tok[:])
	// Keep the URL to the token only: ServeMux patterns cannot contain spaces,
	// so embedding the file name would panic for names such as "paper (2).pdf".
	secretPath := "/" + hex.EncodeToString(tok[:])

	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != secretPath {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/pdf")
		w.Header().Set("Content-Disposition", "inline")
		w.Header().Set("Cache-Control", "no-store")
		http.ServeFile(w, r, path)
	})}
	go func() { _ = srv.Serve(ln) }()

	return "http://" + ln.Addr().String() + secretPath, func() {
		_ = srv.Close()
		_ = ln.Close()
	}, nil
}
