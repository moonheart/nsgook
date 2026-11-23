package fontviewer

import (
	"fmt"
	"path/filepath"
	"strconv"
	"sync/atomic"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/icons/lucide"
	"github.com/nsgo-dev/nsgo/ui"

	"nsgook/internal/config"
	"nsgook/internal/plugins/pluginui"
	"nsgook/internal/viewer"
	"nsgook/locales"
)

const defaultSpecimen = "The quick brown fox jumps over the lazy dog. 0123456789\nABCDEFGHIJKLMNOPQRSTUVWXYZ abcdefghijklmnopqrstuvwxyz !?@#$%&*\n天地玄黄 宇宙洪荒 永和九年 岁在癸丑 1234567890"

// Plugin previews TTF, OTF, TTC, WOFF, and WOFF2 fonts with specimen waterfall and glyph grid views.
type Plugin struct{}

func New() viewer.Plugin { return Plugin{} }

func (Plugin) ID() string   { return "fontviewer" }
func (Plugin) Name() string { return "FontViewer" }
func (Plugin) Priority() int {
	return 80
}

func (Plugin) CanHandle(path string) bool {
	return IsFontExt(filepath.Ext(path))
}

func (Plugin) Prepare(path string, ctx *viewer.Context) {
	ctx.PreferredSize = core.Size{Width: 820, Height: 620}
	ctx.Title.Set(viewer.DisplayName(path))
}

func (Plugin) View(cx *core.Scope, path string, ctx *viewer.Context) core.Node {
	colors := ui.ColorsOf(cx)
	tr := locales.For(cx)

	defaultGlyphs := false
	if ctx.Config != nil && ctx.Config.Get().FontPreviewMode == config.FontModeGlyphs {
		defaultGlyphs = true
	}

	infoSig := core.NewSignal[*FontInfo](cx, nil)
	loadErr := core.NewSignal(cx, "")
	glyphMode := core.NewSignal(cx, defaultGlyphs)
	sampleText := core.NewSignal(cx, defaultSpecimen)
	activeFamily := core.NewSignal(cx, "")

	var stopped atomic.Bool
	cx.Cleanup(func() {
		stopped.Store(true)
		infoSig.Set(nil)
	})

	ctx.IsBusy.Set(true)
	go func() {
		info, err := ParseFontFile(path)
		if stopped.Load() {
			return
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
			infoSig.Set(info)
			if info.Family != "" {
				ctx.Title.Set(fmt.Sprintf("%s · %s", info.Family, info.Style))
			}
			if len(info.RegisterableBytes) > 0 && info.Family != "" {
				if regErr := cx.RegisterFont(info.RegisterableBytes); regErr == nil {
					activeFamily.Set(info.Family)
				}
			}
		})
	}()

	ctx.MoreItems.Set([]viewer.MenuAction{
		{
			ID:       "font-specimen",
			Label:    tr.FontSpecimen(),
			Icon:     lucide.Type,
			Checked:  func() bool { return !glyphMode.Get() },
			OnSelect: func() { glyphMode.Set(false) },
		},
		{
			ID:       "font-glyphs",
			Label:    tr.FontGlyphs(),
			Icon:     lucide.Grid3x3,
			Checked:  glyphMode.Get,
			OnSelect: func() { glyphMode.Set(true) },
		},
	})

	toolbar := pluginui.Toolbar(cx,
		ui.Button{
			Text:    locales.For(cx).FontSpecimen(),
			Icon:    lucide.Type,
			Variant: ui.ButtonGhost,
			Size:    ui.SizeSmall,
			OnPress: func() { glyphMode.Set(false) },
		},
		ui.Button{
			Text:    locales.For(cx).FontGlyphs(),
			Icon:    lucide.Grid3x3,
			Variant: ui.ButtonGhost,
			Size:    ui.SizeSmall,
			OnPress: func() { glyphMode.Set(true) },
		},
		ui.Spacer{},
		core.Show{
			When: func() bool { return infoSig.Get() != nil },
			Then: func(*core.Scope) core.Node {
				return ui.Badge{
					TextFunc: func() string {
						fi := infoSig.Get()
						if fi == nil {
							return ""
						}
						if fi.GlyphCount > 0 {
							return fmt.Sprintf("%s · %d", fi.Format, fi.GlyphCount)
						}
						return fi.Format
					},
					Variant: ui.BadgeNeutral,
				}
			},
		},
	)

	waterfallSizes := []core.Points{12, 16, 20, 28, 36, 48, 64}

	specimenView := core.Scroll{
		Style: core.Style{FlexGrow: 1, FlexShrink: 1, Width: core.Pct(100), Height: core.Pct(100)},
		Children: []core.Node{
			core.View{
				Style: core.Style{Padding: core.All(16), Gap: 14},
				Children: []core.Node{
					ui.Input{
						Value:       sampleText.Get,
						OnChange:    sampleText.Set,
						Placeholder: locales.For(cx).FontSamplePlaceholder(),
						Style:       core.Style{Width: core.Pct(100)},
					},
					core.Dynamic(func(cx *core.Scope) core.Node {
						fi := infoSig.Get()
						fam := activeFamily.Get()
						txt := sampleText.Get()
						if txt == "" {
							txt = defaultSpecimen
						}
						var cards []core.Node
						for _, sz := range waterfallSizes {
							ptSize := sz
							cards = append(cards, core.View{
								Style: core.Style{
									Direction:  core.Row,
									AlignItems: core.AlignBaseline,
									Gap:        12,
									Padding:    core.Edges{Bottom: 10},
									Borders:    core.Edges{Bottom: 1},
								},
								StyleFunc: func() core.Style { return core.Style{BorderColor: colors.Line()} },
								Children: []core.Node{
									ui.Text(fmt.Sprintf("%.0f pt", ptSize), 11, ui.Medium, colors.Muted),
									core.Text{
										Content:    txt,
										Selectable: core.SelectableOn,
										Style: core.Style{
											FontSize:   ptSize,
											LineHeight: ptSize * 1.35,
											FontFamily: fam,
											FlexGrow:   1,
											FlexShrink: 1,
										},
										StyleFunc: ui.Colored(colors.Text),
									},
								},
							})
						}
						if fi != nil {
							var rows []pluginui.KV
							if fi.Family != "" {
								rows = append(rows, pluginui.KV{Key: tr.FontFamily(), Value: fi.Family})
							}
							if fi.Style != "" {
								rows = append(rows, pluginui.KV{Key: tr.FontStyle(), Value: fi.Style})
							}
							if fi.Version != "" {
								rows = append(rows, pluginui.KV{Key: tr.FontVersion(), Value: fi.Version})
							}
							if fi.GlyphCount > 0 {
								rows = append(rows, pluginui.KV{Key: tr.FontGlyphCount(), Value: strconv.Itoa(fi.GlyphCount)})
							}
							if fi.Designer != "" {
								rows = append(rows, pluginui.KV{Key: "Designer", Value: fi.Designer})
							}
							if fi.Copyright != "" {
								rows = append(rows, pluginui.KV{Key: "Copyright", Value: fi.Copyright})
							}
							if len(rows) > 0 {
								cards = append(cards, pluginui.SectionCard(cx, "", rows, tr.CommonCopy(), func(val string) {
									cx.SetClipboard(val)
									if ctx.Toast != nil {
										ctx.Toast(ui.ToastSuccess, tr.CommonCopied())
									}
								}))
							}
						}
						return core.View{Style: core.Style{Gap: 10}, Children: cards}
					}),
				},
			},
		},
	}

	glyphGridView := core.Dynamic(func(cx *core.Scope) core.Node {
		fi := infoSig.Get()
		if fi == nil || len(fi.Runes) == 0 {
			return pluginui.EmptyState(cx, lucide.Grid3x3, viewer.DisplayName(path), locales.For(cx).CommonEmpty())
		}
		fam := activeFamily.Get()
		runes := fi.Runes
		return core.List{
			Count:         func() int { return len(runes) },
			Key:           func(i int) any { return runes[i] },
			NumColumns:    8,
			Gap:           6,
			ColumnGap:     6,
			EstimatedSize: 64,
			Style:         core.Style{FlexGrow: 1, FlexShrink: 1, MinWidth: core.Pt(0), MinHeight: core.Pt(0), Padding: core.All(12)},
			Row: func(cx *core.Scope, row core.ListRow) core.Node {
				r := runes[row.Index()]
				glyph := string(r)
				code := fmt.Sprintf("U+%04X", r)
				return ui.Press{
					Label: glyph + " " + code,
					OnPress: func() {
						cx.SetClipboard(glyph)
						if ctx.Toast != nil {
							ctx.Toast(ui.ToastSuccess, tr.FontCopiedGlyph(glyph, code))
						}
					},
					Children: []core.Node{
						core.View{
							Style: core.Style{
								Height:         core.Pt(64),
								CornerRadius:   8,
								Borders:        core.All(1),
								AlignItems:     core.AlignCenter,
								JustifyContent: core.JustifyCenter,
								Gap:            2,
							},
							StyleFunc: func() core.Style {
								return core.Style{
									Background:  ui.Alpha(colors.Inset(), 0.55),
									BorderColor: colors.Line(),
									Hover:       &core.Style{Background: colors.Hover()},
								}
							},
							Children: []core.Node{
								core.Text{
									Content:   glyph,
									Style:     core.Style{FontSize: 22, FontFamily: fam},
									StyleFunc: ui.Colored(colors.Text),
								},
								core.Text{
									Content:   code,
									Style:     core.Style{FontSize: 9, FontFamily: ui.Mono},
									StyleFunc: ui.Colored(colors.Muted),
								},
							},
						},
					},
				}
			},
		}
	})

	return pluginui.Fill(
		core.Show{
			When: func() bool { return loadErr.Get() != "" },
			Then: func(cx *core.Scope) core.Node {
				return pluginui.EmptyState(cx, lucide.FileExclamationPoint, locales.For(cx).CommonError(), loadErr.Get())
			},
			Else: func(*core.Scope) core.Node {
				return pluginui.Fill(
					toolbar,
					core.Show{
						When: glyphMode.Get,
						Then: func(*core.Scope) core.Node { return glyphGridView },
						Else: func(*core.Scope) core.Node { return specimenView },
					},
				)
			},
		},
	)
}
