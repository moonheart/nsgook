// Package htmlviewer implements NSGook's HTML, MHTML, and .url Internet Shortcut viewer.
package htmlviewer

import (
	"bytes"
	"encoding/base64"
	"io"
	"mime"
	"mime/multipart"
	"net/mail"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"

	"github.com/nsgo-dev/nsgo/base/editor"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/icons/lucide"
	"github.com/nsgo-dev/nsgo/ui"

	"nsgook/internal/plugins/pluginui"
	"nsgook/internal/plugins/textviewer"
	"nsgook/internal/viewer"
	"nsgook/locales"
)

// Plugin previews .html, .htm, .mht, .mhtml, and .url files.
type Plugin struct{}

func New() viewer.Plugin { return Plugin{} }

func (Plugin) ID() string    { return "htmlviewer" }
func (Plugin) Name() string  { return "HtmlViewer" }
func (Plugin) Priority() int { return 0 }

func (Plugin) CanHandle(path string) bool {
	if path == "" || strings.HasPrefix(path, "::") {
		return false
	}
	ext := strings.ToLower(filepath.Ext(path))
	return ext == ".html" || ext == ".htm" || ext == ".mht" || ext == ".mhtml" || ext == ".url"
}

func (Plugin) Prepare(path string, ctx *viewer.Context) {
	ctx.PreferredSize = core.Size{Width: 960, Height: 680}
	ctx.Title.Set(viewer.DisplayName(path))
}

func (Plugin) View(cx *core.Scope, path string, ctx *viewer.Context) core.Node {
	colors := ui.ColorsOf(cx)
	tr := locales.For(cx)

	ext := strings.ToLower(filepath.Ext(path))
	webSrc := core.NewSignal(cx, core.WebSource{})
	rawSource := core.NewSignal(cx, "")
	targetURL := core.NewSignal(cx, "")
	canBack := core.NewSignal(cx, false)
	canForward := core.NewSignal(cx, false)
	currentURL := core.NewSignal(cx, "")
	showSource := core.NewSignal(cx, false)
	loadErr := core.NewSignal(cx, "")
	webRef := &core.Ref{}

	var stopped atomic.Bool
	cx.Cleanup(func() {
		stopped.Store(true)
		rawSource.Set("")
		webSrc.Set(core.WebSource{})
	})

	ctx.IsBusy.Set(true)
	go func() {
		data, err := os.ReadFile(path)
		if stopped.Load() {
			return
		}
		var src core.WebSource
		var rawStr, urlStr string
		if err == nil {
			rawStr, _ = textviewer.DecodeText(data)
			switch ext {
			case ".url":
				urlStr = parseURLShortcut(rawStr)
				if urlStr != "" {
					src = core.WebSrc(urlStr)
				}
			case ".mht", ".mhtml":
				htmlBody := extractMHTML(data)
				src = core.WebHTML(htmlBody, "")
			default:
				baseURL := "file:///" + filepath.ToSlash(filepath.Dir(path)) + "/"
				src = core.WebHTML(rawStr, baseURL)
			}
		}
		cx.Post(func() {
			if stopped.Load() {
				return
			}
			ctx.IsBusy.Set(false)
			if err != nil {
				loadErr.Set(err.Error())
				return
			}
			rawSource.Set(rawStr)
			targetURL.Set(urlStr)
			currentURL.Set(urlStr)
			webSrc.Set(src)
		})
	}()

	toggleSource := func() {
		showSource.Set(!showSource.GetUntracked())
	}

	openExternal := func() {
		u := currentURL.GetUntracked()
		if u != "" && (strings.HasPrefix(u, "http://") || strings.HasPrefix(u, "https://")) {
			cx.OpenURL(u, nil)
			return
		}
		if ctx.Sys != nil {
			_ = ctx.Sys.OpenDefault(path)
		}
	}

	ctx.MoreItems.Set([]viewer.MenuAction{
		{
			ID:       "html-source",
			Label:    tr.WebToggleSource(),
			Icon:     lucide.Code,
			Checked:  showSource.Get,
			OnSelect: toggleSource,
		},
		{
			ID:       "html-open-browser",
			Label:    tr.WebOpenBrowser(),
			Icon:     lucide.ExternalLink,
			OnSelect: openExternal,
		},
	})

	toolbar := pluginui.Toolbar(cx,
		pluginui.IconButton{
			Icon:     lucide.ArrowLeft,
			Label:    locales.For(cx).WebBack(),
			Size:     26,
			Disabled: func() bool { return !canBack.Get() || showSource.Get() },
			OnPress:  webRef.GoBack,
		},
		pluginui.IconButton{
			Icon:     lucide.ArrowRight,
			Label:    locales.For(cx).WebForward(),
			Size:     26,
			Disabled: func() bool { return !canForward.Get() || showSource.Get() },
			OnPress:  webRef.GoForward,
		},
		pluginui.IconButton{
			Icon:     lucide.RotateCw,
			Label:    locales.For(cx).WebReload(),
			Size:     26,
			Disabled: showSource.Get,
			OnPress:  webRef.Reload,
		},
		core.View{
			Style: core.Style{
				FlexGrow:     1,
				FlexShrink:   1,
				MinWidth:     core.Pt(0),
				Padding:      core.Symmetric(4, 10),
				CornerRadius: ui.RadiusControl,
				BorderWidth:  1,
			},
			StyleFunc: func() core.Style {
				return core.Style{
					Background:  colors.Field(),
					BorderColor: colors.Line(),
				}
			},
			Children: []core.Node{
				core.Text{
					ContentFunc: func() string {
						if u := currentURL.Get(); u != "" {
							return u
						}
						return path
					},
					MaxLines:  1,
					Style:     core.Style{FontSize: 11, FontFamily: ui.Mono},
					StyleFunc: ui.Colored(colors.Muted),
				},
			},
		},
		ui.Button{
			Text:    locales.For(cx).WebToggleSource(),
			Icon:    lucide.Code,
			Variant: ui.ButtonGhost,
			Size:    ui.SizeSmall,
			OnPress: toggleSource,
		},
		pluginui.IconButton{
			Icon:    lucide.ExternalLink,
			Label:   locales.For(cx).WebOpenBrowser(),
			Size:    26,
			OnPress: openExternal,
		},
	)

	updateNav := func(nav core.WebNavigation) {
		canBack.Set(nav.CanGoBack)
		canForward.Set(nav.CanGoForward)
		if nav.URL != "" && nav.URL != "about:blank" {
			currentURL.Set(nav.URL)
		}
		if nav.Title != "" && nav.Title != "about:blank" {
			ctx.Title.Set(nav.Title + " — " + viewer.DisplayName(path))
		}
	}

	sourceView := core.Dynamic(func(cx *core.Scope) core.Node {
		lang := "html"
		if ext == ".url" {
			lang = "ini"
		}
		return editor.Editor{
			Value:          rawSource.Get,
			Language:       lang,
			ReadOnly:       true,
			NoFolding:      true,
			SoftWrap:       true,
			FontSize:       13,
			LineHeight:     20,
			NoActiveLine:   true,
			NoIndentGuides: true,
			StylesFunc:     textviewer.EditorStyles(cx),
			Style:          core.Style{FlexGrow: 1, FlexShrink: 1, MinWidth: core.Pt(0), MinHeight: core.Pt(0)},
		}
	})

	webContent := core.Show{
		When: func() bool { return cx.Env().HasWebView() && !showSource.Get() },
		Then: func(*core.Scope) core.Node {
			return core.WebView{
				Ref:                     webRef,
				SourceFunc:              webSrc.Get,
				Incognito:               true,
				PinchZoom:               true,
				OnLoadStart:             updateNav,
				OnLoad:                  updateNav,
				OnNavigationStateChange: updateNav,
				Style:                   core.Style{FlexGrow: 1, FlexShrink: 1, MinWidth: core.Pt(0), MinHeight: core.Pt(0)},
			}
		},
		Else: func(cx *core.Scope) core.Node {
			if showSource.Get() {
				return sourceView
			}
			return pluginui.Fill(
				core.View{
					Style: core.Style{Padding: core.Symmetric(6, 12), FlexShrink: 0},
					Children: []core.Node{
						ui.Text(locales.For(cx).WebUnavailable(), 12, ui.Regular, colors.Muted),
					},
				},
				sourceView,
			)
		},
	}

	return pluginui.Fill(
		core.Show{
			When: func() bool { return loadErr.Get() != "" },
			Then: func(cx *core.Scope) core.Node {
				return pluginui.EmptyState(cx, lucide.Globe, locales.For(cx).CommonError(), loadErr.Get())
			},
			Else: func(*core.Scope) core.Node {
				return pluginui.Fill(toolbar, webContent)
			},
		},
	)
}

func parseURLShortcut(raw string) string {
	for _, line := range strings.Split(strings.ReplaceAll(raw, "\r\n", "\n"), "\n") {
		trim := strings.TrimSpace(line)
		if len(trim) > 4 && strings.EqualFold(trim[:4], "URL=") {
			return strings.TrimSpace(trim[4:])
		}
	}
	return ""
}

func extractMHTML(data []byte) string {
	msg, err := mail.ReadMessage(bytes.NewReader(data))
	if err != nil {
		s, _ := textviewer.DecodeText(data)
		return s
	}
	ct := msg.Header.Get("Content-Type")
	mediaType, params, err := mime.ParseMediaType(ct)
	if err != nil || !strings.HasPrefix(mediaType, "multipart/") {
		body, _ := io.ReadAll(msg.Body)
		s, _ := textviewer.DecodeText(body)
		return s
	}
	mr := multipart.NewReader(msg.Body, params["boundary"])
	var htmlBody string
	cids := map[string]string{}
	for {
		part, err := mr.NextPart()
		if err != nil {
			break
		}
		partBytes, _ := io.ReadAll(part)
		if strings.EqualFold(part.Header.Get("Content-Transfer-Encoding"), "base64") {
			if dec, decErr := base64.StdEncoding.DecodeString(strings.TrimSpace(string(partBytes))); decErr == nil {
				partBytes = dec
			}
		}
		pct := part.Header.Get("Content-Type")
		pmt, _, _ := mime.ParseMediaType(pct)
		if strings.EqualFold(pmt, "text/html") && htmlBody == "" {
			htmlBody, _ = textviewer.DecodeText(partBytes)
		} else if cid := strings.Trim(part.Header.Get("Content-ID"), "<>"); cid != "" && pmt != "" {
			cids["cid:"+cid] = "data:" + pmt + ";base64," + base64.StdEncoding.EncodeToString(partBytes)
		}
	}
	for k, v := range cids {
		htmlBody = strings.ReplaceAll(htmlBody, k, v)
	}
	if htmlBody == "" {
		s, _ := textviewer.DecodeText(data)
		return s
	}
	return htmlBody
}
