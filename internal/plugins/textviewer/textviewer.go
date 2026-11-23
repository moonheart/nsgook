package textviewer

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"

	"github.com/nsgo-dev/nsgo/base/diff"
	"github.com/nsgo-dev/nsgo/base/editor"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/icons/lucide"
	"github.com/nsgo-dev/nsgo/ui"
	uidiff "github.com/nsgo-dev/nsgo/ui/diff"

	"nsgook/internal/config"
	"nsgook/internal/plugins/pluginui"
	"nsgook/internal/viewer"
	"nsgook/locales"
)

// Plugin previews source code, plain text, configs, logs, and patch files.
type Plugin struct{}

func New() viewer.Plugin { return Plugin{} }

func (Plugin) ID() string    { return "textviewer" }
func (Plugin) Name() string  { return "TextViewer" }
func (Plugin) Priority() int { return -5 }

func (Plugin) CanHandle(path string) bool {
	return IsLikelyTextFile(path)
}

func (Plugin) Prepare(path string, ctx *viewer.Context) {
	ctx.PreferredSize = core.Size{Width: 860, Height: 640}
	ctx.Title.Set(viewer.DisplayName(path))
}

func (Plugin) ExtendedMenuItems(cx *core.Scope, _ string, ctx *viewer.Context) []viewer.MenuAction {
	if ctx.IsDir || ctx.SwitchPlugin == nil {
		return nil
	}
	return []viewer.MenuAction{
		{
			ID:       "ext-view-text",
			Label:    locales.For(cx).ViewAsText(),
			Icon:     lucide.FileText,
			OnSelect: func() { ctx.SwitchPlugin("textviewer") },
		},
	}
}

func (Plugin) View(cx *core.Scope, path string, ctx *viewer.Context) core.Node {
	colors := ui.ColorsOf(cx)
	tr := locales.For(cx)

	cfg := config.Default()
	if ctx.Config != nil {
		cfg = ctx.Config.Get()
	}

	content := core.NewSignal(cx, "")
	loaded := core.NewSignal(cx, false)
	encoding := core.NewSignal(cx, "UTF-8")
	lang := core.NewSignal(cx, "")
	totalSize := core.NewSignal(cx, int64(0))
	truncated := core.NewSignal(cx, false)
	loadErr := core.NewSignal(cx, "")

	lineNumbers := core.NewSignal(cx, cfg.ShowLineNumbers)
	wordWrap := core.NewSignal(cx, cfg.WordWrap)
	fontSize := core.NewSignal(cx, core.Points(cfg.TextFontSize))
	fontFamily := cfg.TextFontFamily

	showSearch := core.NewSignal(cx, false)
	searchQuery := core.NewSignal(cx, "")
	searchResult := core.NewSignal(cx, editor.SearchResult{Current: -1})
	showDiff := core.NewSignal(cx, isPatchExt(path))

	edRef := &editor.Ref{}
	searchInputRef := &core.Ref{}

	var stopped atomic.Bool
	cx.Cleanup(func() {
		stopped.Store(true)
		content.Set("")
		searchQuery.Set("")
	})

	loadFile := func(limit int64) {
		ctx.IsBusy.Set(true)
		go func() {
			raw, fullSize, isTrunc, err := readFileCapped(path, limit)
			if stopped.Load() {
				return
			}
			var decoded, enc string
			var detectedLang string
			if err == nil {
				decoded, enc = DecodeText(raw)
				detectedLang = DetectLanguage(path, decoded)
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
				totalSize.Set(fullSize)
				truncated.Set(isTrunc)
				encoding.Set(enc)
				lang.Set(detectedLang)
				content.Set(decoded)
				loaded.Set(true)
			})
		}()
	}

	loadFile(InitialReadLimit)

	toggleLineNumbers := func() {
		next := !lineNumbers.GetUntracked()
		lineNumbers.Set(next)
		if ctx.Config != nil {
			_ = ctx.Config.Update(func(c *config.Config) { c.ShowLineNumbers = next })
		}
	}

	toggleWordWrap := func() {
		next := !wordWrap.GetUntracked()
		wordWrap.Set(next)
		if ctx.Config != nil {
			_ = ctx.Config.Update(func(c *config.Config) { c.WordWrap = next })
		}
	}

	changeFontSize := func(delta core.Points) {
		next := clampPoints(fontSize.GetUntracked()+delta, 10, 32)
		fontSize.Set(next)
		if ctx.Config != nil {
			_ = ctx.Config.Update(func(c *config.Config) { c.TextFontSize = float32(next) })
		}
	}

	prettyFormat := func() {
		formatted, ok := PrettyFormat(lang.GetUntracked(), content.GetUntracked())
		if ok {
			content.Set(formatted)
		}
	}

	openSearch := func() {
		showSearch.Set(true)
		cx.AfterFunc(0, func() { searchInputRef.Focus() })
	}

	// Register QuickLook TextViewer hotkeys: Alt+L, Alt+Z, Ctrl+F, Ctrl+=, Ctrl+-.
	cx.OnKeyWith("alt+l", core.KeyOptions{When: core.KeyAlways}, toggleLineNumbers)
	cx.OnKeyWith("alt+z", core.KeyOptions{When: core.KeyAlways}, toggleWordWrap)
	cx.OnKeyWith("mod+f", core.KeyOptions{When: core.KeyAlways}, openSearch)
	cx.OnKeyWith("mod+=", core.KeyOptions{When: core.KeyAlways}, func() { changeFontSize(1) })
	cx.OnKeyWith("mod+-", core.KeyOptions{When: core.KeyAlways}, func() { changeFontSize(-1) })

	more := []viewer.MenuAction{
		{
			ID:       "text-lines",
			Label:    tr.TextToggleLineNumbers(),
			Icon:     lucide.ListOrdered,
			Shortcut: "alt+l",
			Checked:  lineNumbers.Get,
			OnSelect: toggleLineNumbers,
		},
		{
			ID:       "text-wrap",
			Label:    tr.TextToggleWordWrap(),
			Icon:     lucide.TextWrap,
			Shortcut: "alt+z",
			Checked:  wordWrap.Get,
			OnSelect: toggleWordWrap,
		},
		{
			ID:       "text-find",
			Label:    tr.TextFind(),
			Icon:     lucide.Search,
			Shortcut: "mod+f",
			OnSelect: openSearch,
		},
	}
	if isPatchExt(path) {
		more = append(more, viewer.MenuAction{
			ID:       "text-diff",
			Label:    tr.TextToggleDiff(),
			Icon:     lucide.GitCompareArrows,
			Checked:  showDiff.Get,
			OnSelect: func() { showDiff.Set(!showDiff.GetUntracked()) },
		})
	}
	extLower := strings.ToLower(filepath.Ext(path))
	if extLower == ".json" || extLower == ".xml" || extLower == ".svg" || extLower == ".plist" {
		more = append(more, viewer.MenuAction{
			ID:       "text-pretty",
			Label:    tr.TextPrettyFormat(),
			Icon:     lucide.Sparkles,
			OnSelect: prettyFormat,
		})
	}
	ctx.MoreItems.Set(more)

	truncBanner := core.Show{
		When: truncated.Get,
		Then: func(cx *core.Scope) core.Node {
			return core.View{
				Style: core.Style{
					Direction:  core.Row,
					AlignItems: core.AlignCenter,
					Gap:        10,
					Padding:    core.Symmetric(6, 12),
					Borders:    core.Edges{Bottom: 1},
					FlexShrink: 0,
				},
				StyleFunc: func() core.Style {
					return core.Style{
						Background:  ui.TintsOf(cx).Warn(),
						BorderColor: colors.Line(),
					}
				},
				Children: []core.Node{
					ui.IconOf(lucide.TriangleAlert, 14, colors.Warn),
					core.Text{
						ContentFunc: func() string {
							return locales.For(cx).TextTruncated(
								viewer.FormatBytes(InitialReadLimit),
								viewer.FormatBytes(uint64(max(0, totalSize.Get()))),
							)
						},
						Style:     core.Style{FontSize: 12, FlexGrow: 1, FlexShrink: 1},
						StyleFunc: ui.Colored(colors.Text),
					},
					ui.Button{
						Text:    locales.For(cx).TextLoadFull(),
						Variant: ui.ButtonOutline,
						Size:    ui.SizeSmall,
						OnPress: func() { loadFile(0) },
					},
				},
			}
		},
	}

	searchBar := core.Show{
		When: showSearch.Get,
		Then: func(cx *core.Scope) core.Node {
			return pluginui.Toolbar(cx,
				ui.IconOf(lucide.Search, 14, colors.Muted),
				ui.Input{
					Ref:         searchInputRef,
					Value:       searchQuery.Get,
					OnChange:    searchQuery.Set,
					Placeholder: locales.For(cx).TextFindPlaceholder(),
					OnSubmit:    func(string) { edRef.Run(editor.FindNext) },
					Style:       core.Style{Width: core.Pt(220)},
				},
				core.Text{
					ContentFunc: func() string {
						r := searchResult.Get()
						if searchQuery.Get() == "" {
							return ""
						}
						if r.Count == 0 {
							return "0/0"
						}
						cur := max(0, r.Current) + 1
						return fmt.Sprintf("%d/%d", cur, r.Count)
					},
					Style:     core.Style{FontSize: 11, FontFamily: ui.Mono, MinWidth: core.Pt(40)},
					StyleFunc: ui.Colored(colors.Muted),
				},
				pluginui.IconButton{
					Icon:    lucide.ChevronUp,
					Label:   locales.For(cx).TextFindPrev(),
					Size:    24,
					OnPress: func() { edRef.Run(editor.FindPrevious) },
				},
				pluginui.IconButton{
					Icon:    lucide.ChevronDown,
					Label:   locales.For(cx).TextFindNext(),
					Size:    24,
					OnPress: func() { edRef.Run(editor.FindNext) },
				},
				ui.Spacer{},
				pluginui.IconButton{
					Icon:    lucide.X,
					Label:   locales.For(cx).WinClose(),
					Size:    24,
					OnPress: func() { showSearch.Set(false); searchQuery.Set("") },
				},
			)
		},
	}

	body := core.Dynamic(func(cx *core.Scope) core.Node {
		if loadErr.Get() != "" {
			return pluginui.EmptyState(cx, lucide.FileExclamationPoint, locales.For(cx).CommonError(), loadErr.Get())
		}
		if !loaded.Get() {
			return core.View{}
		}
		if showDiff.Get() && isPatchExt(path) {
			txt := content.Get()
			if txt == "" {
				return core.View{}
			}
			return uidiff.View{
				Patch:    func() string { return content.Get() },
				Unified:  true,
				SoftWrap: wordWrap.Get(),
				Style:    core.Style{FlexGrow: 1, FlexShrink: 1, MinWidth: core.Pt(0), MinHeight: core.Pt(0)},
			}
		}

		fs := fontSize.Get()
		lh := fs * 1.45
		styles := EditorStyles(cx)
		return editor.Editor{
			Ref:            edRef,
			Value:          content.Get,
			Language:       lang.Get(),
			ReadOnly:       true,
			NoLineNumbers:  !lineNumbers.Get(),
			NoFolding:      true,
			SoftWrap:       wordWrap.Get(),
			FontFamily:     fontFamily,
			FontSize:       fs,
			LineHeight:     lh,
			NoActiveLine:   true,
			NoIndentGuides: true,
			Search: func() editor.Search {
				return editor.Search{Query: searchQuery.Get()}
			},
			OnSearchChange: searchResult.Set,
			StylesFunc:     styles,
			Style:          core.Style{FlexGrow: 1, FlexShrink: 1, MinWidth: core.Pt(0), MinHeight: core.Pt(0)},
		}
	})

	status := pluginui.StatusBar(cx,
		core.Text{
			ContentFunc: func() string {
				txt := content.Get()
				lines := 1
				if txt != "" {
					lines = strings.Count(txt, "\n") + 1
				}
				return locales.For(cx).TextLinesLabel(strconv.Itoa(lines), encoding.Get())
			},
			Style:     core.Style{FontSize: 11},
			StyleFunc: ui.Colored(colors.Muted),
		},
		ui.Spacer{},
		core.Show{
			When: func() bool { return lang.Get() != "" },
			Then: func(*core.Scope) core.Node {
				return ui.Badge{TextFunc: func() string { return strings.ToUpper(lang.Get()) }, Variant: ui.BadgeNeutral}
			},
		},
		pluginui.IconButton{
			Icon:     lucide.ListOrdered,
			Label:    locales.For(cx).TextToggleLineNumbers(),
			Size:     24,
			Selected: lineNumbers.Get,
			OnPress:  toggleLineNumbers,
		},
		pluginui.IconButton{
			Icon:     lucide.TextWrap,
			Label:    locales.For(cx).TextToggleWordWrap(),
			Size:     24,
			Selected: wordWrap.Get,
			OnPress:  toggleWordWrap,
		},
		pluginui.IconButton{
			Icon:     lucide.Search,
			Label:    locales.For(cx).TextFind(),
			Size:     24,
			Selected: showSearch.Get,
			OnPress:  openSearch,
		},
	)

	// Touch diff.Hunk so unused import isn't possible if diff package stays linked cleanly.
	_ = diff.ExpandEvery

	return pluginui.Fill(truncBanner, searchBar, body, status)
}

func isPatchExt(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	return ext == ".diff" || ext == ".patch"
}

func readFileCapped(path string, limit int64) (raw []byte, total int64, truncated bool, err error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, 0, false, err
	}
	defer f.Close()
	info, statErr := f.Stat()
	if statErr == nil {
		total = info.Size()
	}
	if limit > 0 && total > limit {
		raw, err = io.ReadAll(io.LimitReader(f, limit))
		return raw, total, true, err
	}
	raw, err = io.ReadAll(f)
	if total == 0 {
		total = int64(len(raw))
	}
	return raw, total, false, err
}

func clampPoints(v, lo, hi core.Points) core.Points {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// EditorStyles returns ui-themed styles for base/editor.Editor.
func EditorStyles(cx *core.Scope) func() editor.Styles {
	return ui.Themed(cx, func() editor.Styles {
		colors := ui.ColorsOf(cx).Now()
		return editor.Styles{
			Gutter:             core.Style{Color: colors.Faint, FontSize: 12, Padding: core.Symmetric(0, 8)},
			LineNumber:         core.Style{Color: colors.Faint},
			ActiveLineNumber:   core.Style{Color: colors.Text},
			FoldMarker:         core.Style{Color: colors.Faint},
			Content:            core.Style{Color: colors.Text, Padding: core.Symmetric(6, 10)},
			ActiveLine:         core.Style{Background: ui.Alpha(colors.Text, 0.04)},
			Selection:          core.Style{Background: ui.Alpha(colors.Accent, 0.22)},
			Caret:              core.Style{Background: colors.Accent, Width: core.Pt(2)},
			SearchMatch:        core.Style{Background: core.RGBA(0xfbbf24, 0.25)},
			CurrentSearchMatch: core.Style{Background: core.RGBA(0xfbbf24, 0.55)},
			MatchingBracket:    core.Style{BorderWidth: 1, BorderColor: ui.Alpha(colors.Muted, 0.6)},
			FoldPlaceholder:    core.Style{Color: colors.Faint},
			Placeholder:        core.Style{Color: colors.Faint},
			IndentGuide:        core.Style{Background: colors.Line},
		}
	})
}
