package markdownviewer

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"

	"github.com/nsgo-dev/nsgo/base/diagram"
	"github.com/nsgo-dev/nsgo/base/editor"
	"github.com/nsgo-dev/nsgo/base/formula"
	"github.com/nsgo-dev/nsgo/base/textview"
	"github.com/nsgo-dev/nsgo/base/textview/highlight"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/icons/lucide"
	"github.com/nsgo-dev/nsgo/ui"

	"nsgook/internal/config"
	"nsgook/internal/plugins/pluginui"
	"nsgook/internal/plugins/textviewer"
	"nsgook/internal/viewer"
	"nsgook/locales"
)

// Plugin previews Markdown, Mermaid diagrams, Jupyter Notebooks, AsciiDoc, and reStructuredText.
type Plugin struct{}

func New() viewer.Plugin { return Plugin{} }

func (Plugin) ID() string    { return "markdownviewer" }
func (Plugin) Name() string  { return "MarkdownViewer" }
func (Plugin) Priority() int { return 0 }

func (Plugin) CanHandle(path string) bool {
	if path == "" || strings.HasPrefix(path, "::") {
		return false
	}
	return isSupportedExt(filepath.Ext(path))
}

func (Plugin) Prepare(path string, ctx *viewer.Context) {
	ctx.PreferredSize = core.Size{Width: 900, Height: 680}
	ctx.Title.Set(viewer.DisplayName(path))
}

func (Plugin) View(cx *core.Scope, path string, ctx *viewer.Context) core.Node {
	colors := ui.ColorsOf(cx)
	tr := locales.For(cx)

	showTocDefault := false
	if ctx.Config != nil {
		showTocDefault = ctx.Config.Get().MarkdownShowToc
	}

	rendered := core.NewSignal(cx, "")
	rawText := core.NewSignal(cx, "")
	rawLang := core.NewSignal(cx, "markdown")
	headings := core.NewSignalFunc(cx, []Heading(nil), func(_, _ []Heading) bool { return false })
	showToc := core.NewSignal(cx, showTocDefault)
	showRaw := core.NewSignal(cx, false)
	loadErr := core.NewSignal(cx, "")

	var stopped atomic.Bool
	cx.Cleanup(func() {
		stopped.Store(true)
		rendered.Set("")
		rawText.Set("")
		headings.Set(nil)
	})

	ctx.IsBusy.Set(true)
	go func() {
		data, err := os.ReadFile(path)
		if stopped.Load() {
			return
		}
		var dec, md, lang string
		var toc []Heading
		if err == nil {
			dec, _ = textviewer.DecodeText(data)
			md, lang = PrepareMarkdown(path, dec)
			toc = ExtractHeadings(md)
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
			rawText.Set(dec)
			rawLang.Set(lang)
			rendered.Set(md)
			headings.Set(toc)
		})
	}()

	toggleToc := func() {
		next := !showToc.GetUntracked()
		showToc.Set(next)
		if ctx.Config != nil {
			_ = ctx.Config.Update(func(c *config.Config) { c.MarkdownShowToc = next })
		}
	}
	toggleRaw := func() {
		showRaw.Set(!showRaw.GetUntracked())
	}

	cx.OnKeyWith("mod+shift+l", core.KeyOptions{When: core.KeyAlways}, toggleToc)

	ctx.MoreItems.Set([]viewer.MenuAction{
		{
			ID:       "md-toc",
			Label:    tr.MdToggleToc(),
			Icon:     lucide.ListTree,
			Shortcut: "mod+shift+l",
			Checked:  showToc.Get,
			OnSelect: toggleToc,
		},
		{
			ID:       "md-raw",
			Label:    tr.MdToggleRaw(),
			Icon:     lucide.Code,
			Checked:  showRaw.Get,
			OnSelect: toggleRaw,
		},
	})

	hl := highlight.New(cx)

	tocSidebar := core.Show{
		When: func() bool { return showToc.Get() && !showRaw.Get() && len(headings.Get()) > 0 },
		Then: func(cx *core.Scope) core.Node {
			return core.View{
				Style: core.Style{
					Width:      core.Pt(220),
					Borders:    core.Edges{Right: 1},
					FlexShrink: 0,
				},
				StyleFunc: func() core.Style {
					return core.Style{
						Background:  ui.Alpha(colors.Inset(), 0.5),
						BorderColor: colors.Line(),
					}
				},
				Children: []core.Node{
					core.View{
						Style: core.Style{Padding: core.Symmetric(10, 12), Borders: core.Edges{Bottom: 1}},
						StyleFunc: func() core.Style {
							return core.Style{BorderColor: colors.Line()}
						},
						Children: []core.Node{
							ui.Text(locales.For(cx).MdTocTitle(), 12, ui.Semibold, colors.Muted),
						},
					},
					core.Scroll{
						Style: core.Style{FlexGrow: 1, FlexShrink: 1, MinHeight: core.Pt(0)},
						Children: []core.Node{
							core.View{
								Style: core.Style{Padding: core.All(8), Gap: 2},
								Children: []core.Node{
									core.Each(headings.Get, func(h Heading) int { return h.Line },
										func(cx *core.Scope, item func() Heading, _ func() int) core.Node {
											return core.View{
												StyleFunc: func() core.Style {
													h := item()
													indent := core.Points(max(0, h.Level-1) * 10)
													return core.Style{
														Padding:      core.Edges{Top: 4, Bottom: 4, Left: 8 + indent, Right: 8},
														CornerRadius: ui.RadiusSmall,
													}
												},
												Children: []core.Node{
													core.Text{
														ContentFunc: func() string { return item().Title },
														MaxLines:    1,
														Style:       core.Style{FontSize: 12},
														StyleFunc: func() core.Style {
															if item().Level <= 2 {
																return core.Style{Color: colors.Text(), FontWeight: ui.Medium}
															}
															return core.Style{Color: colors.Muted()}
														},
													},
												},
											}
										},
									),
								},
							},
						},
					},
				},
			}
		},
	}

	renderedView := ui.ScrollView{
		Style:        core.Style{FlexGrow: 1, FlexShrink: 1, MinHeight: core.Pt(0), MinWidth: core.Pt(0)},
		ContentStyle: core.Style{Padding: core.Symmetric(20, 28)},
		Children: []core.Node{
			ui.TextView{
				MarkdownFunc:     rendered.Get,
				Highlighter:      hl,
				RenderMath:       formula.RenderMath,
				RenderDiagram:    diagram.RenderDiagram,
				CodeBlockActions: []textview.CodeAction{{ID: textview.CopyAction}},
				Selectable:       true,
				OpenLinks:        true,
			},
		},
	}

	rawEditor := core.Dynamic(func(cx *core.Scope) core.Node {
		return editor.Editor{
			Value:          rawText.Get,
			Language:       rawLang.Get(),
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

	toolbar := pluginui.Toolbar(cx,
		pluginui.IconButton{
			Icon:     lucide.ListTree,
			Label:    locales.For(cx).MdToggleToc(),
			Size:     26,
			Selected: showToc.Get,
			OnPress:  toggleToc,
		},
		ui.Button{
			Text:    locales.For(cx).MdToggleRaw(),
			Icon:    lucide.Code,
			Variant: ui.ButtonGhost,
			Size:    ui.SizeSmall,
			OnPress: toggleRaw,
		},
		ui.Spacer{},
		core.Text{
			ContentFunc: func() string {
				if cnt := len(headings.Get()); cnt > 0 {
					return fmt.Sprintf("%d headings", cnt)
				}
				return strings.ToUpper(strings.TrimPrefix(filepath.Ext(path), "."))
			},
			Style:     core.Style{FontSize: 11},
			StyleFunc: ui.Colored(colors.Muted),
		},
	)

	mainBody := core.Show{
		When: func() bool { return loadErr.Get() != "" },
		Then: func(cx *core.Scope) core.Node {
			return pluginui.EmptyState(cx, lucide.FileExclamationPoint, locales.For(cx).CommonError(), loadErr.Get())
		},
		Else: func(*core.Scope) core.Node {
			return core.View{
				Style: core.Style{
					Direction:  core.Row,
					FlexGrow:   1,
					FlexShrink: 1,
					MinWidth:   core.Pt(0),
					MinHeight:  core.Pt(0),
				},
				Children: []core.Node{
					tocSidebar,
					core.Show{
						When: showRaw.Get,
						Then: func(*core.Scope) core.Node { return rawEditor },
						Else: func(*core.Scope) core.Node { return renderedView },
					},
				},
			}
		},
	}

	return pluginui.Fill(toolbar, mainBody)
}
