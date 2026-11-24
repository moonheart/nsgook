// Package devviewers bundles NSGook's specialized pure-Go developer, binary-structure,
// certificate, document, mail, database, 3D mesh, and Windows-format viewers.
package devviewers

import (
	"fmt"
	"path/filepath"
	"strings"
	"sync/atomic"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/icons/lucide"
	"github.com/nsgo-dev/nsgo/ui"

	"nsgook/internal/plugins/pluginui"
	"nsgook/internal/viewer"
	"nsgook/locales"
)

// All returns every viewer plugin implemented in devviewers.
func All() []viewer.Plugin {
	return []viewer.Plugin{
		PEPlugin{},
		ELFPlugin{},
		CertPlugin{},
		MailPlugin{},
		AppPlugin{},
		OfficePlugin{},
		SqlitePlugin{},
		Model3DPlugin{},
		CLSIDPlugin{},
		PrefetchPlugin{},
		DumpPlugin{},
		ChmPlugin{},
		QLPlugin{},
		ThumbnailPlugin{},
		MediaInfoPlugin{},
	}
}

// Section represents one titled group of key-value pairs or list items inside a Report.
type Section struct {
	Title string
	Rows  []pluginui.KV
}

// Report is a generic structured inspection result rendered uniformly with theme-aware cards and tabs.
type Report struct {
	TitleSuffix string
	Badges      []string
	BadgeAccent string
	Sections    []Section
	Tabs        []ReportTab
	IconData    []byte
	ImageData   []byte
	BodyTitle   string
	BodyText    string
	// Later, when set, runs in the background after the report is shown and
	// replaces it with the report it returns. It keeps slow metadata, such as
	// verifying a large executable's signature, off the first paint. The
	// replacement may set Later too, chaining another update.
	Later func() *Report
}

// ReportTab represents one named view/table/slide/sheet inside a tabbed Report.
type ReportTab struct {
	ID       string
	Label    string
	Sections []Section
	Text     string
}

func renderReportViewer(
	cx *core.Scope,
	path string,
	ctx *viewer.Context,
	icon func() *core.SVG,
	load func() (*Report, error),
) core.Node {
	colors := ui.ColorsOf(cx)
	tr := locales.For(cx)

	repSig := core.NewSignal[*Report](cx, nil)
	loadErr := core.NewSignal(cx, "")
	activeTab := core.NewSignal(cx, "")

	var stopped atomic.Bool
	cx.Cleanup(func() {
		stopped.Store(true)
		repSig.Set(nil)
	})
	ctx.TrackMedia(cx)
	cx.Effect(func() {
		if ctx.IsReleasing() {
			stopped.Store(true)
		}
	})

	ctx.IsBusy.Set(true)
	go func() {
		rep, err := load()
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
			repSig.Set(rep)
			if rep != nil {
				if rep.TitleSuffix != "" {
					ctx.Title.Set(fmt.Sprintf("%s · %s", viewer.DisplayName(path), rep.TitleSuffix))
				}
				if len(rep.Tabs) > 0 {
					activeTab.Set(rep.Tabs[0].ID)
				}
				if rep.Later != nil {
					go func() {
						for current := rep; current != nil && current.Later != nil; {
							next := current.Later()
							if next == nil || stopped.Load() {
								return
							}
							cx.Post(func() {
								if !stopped.Load() {
									repSig.Set(next)
								}
							})
							current = next
						}
					}()
				}
			}
		})
	}()

	onCopy := func(val string) {
		cx.SetClipboard(val)
		if ctx.Toast != nil {
			ctx.Toast(ui.ToastSuccess, tr.CommonCopied())
		}
	}

	return pluginui.Fill(
		core.Show{
			When: func() bool { return loadErr.Get() != "" },
			Then: func(cx *core.Scope) core.Node {
				return pluginui.EmptyState(cx, lucide.FileExclamationPoint, locales.For(cx).CommonError(), loadErr.Get())
			},
			Else: func(cx *core.Scope) core.Node {
				return core.Dynamic(func(cx *core.Scope) core.Node {
					r := repSig.Get()
					if r == nil {
						return core.View{}
					}

					var children []core.Node

					// Header badges & thumbnail preview if present.
					if len(r.Badges) > 0 || r.BadgeAccent != "" || len(r.IconData) > 0 {
						var badgeNodes []core.Node
						if len(r.IconData) > 0 {
							badgeNodes = append(badgeNodes, core.Image{
								Source:     core.ImageSource{Data: r.IconData},
								ResizeMode: core.Contain,
								Style:      core.Style{Width: core.Pt(28), Height: core.Pt(28), FlexShrink: 0},
							})
						} else {
							badgeNodes = append(badgeNodes, ui.IconOf(icon, 16, colors.Accent))
						}
						if r.BadgeAccent != "" {
							badgeNodes = append(badgeNodes, ui.Badge{Text: r.BadgeAccent, Variant: ui.BadgeAccent})
						}
						for _, b := range r.Badges {
							badgeNodes = append(badgeNodes, ui.Badge{Text: b, Variant: ui.BadgeNeutral})
						}
						children = append(children, core.View{
							Style:    core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Wrap: core.Wrap, Gap: 6},
							Children: badgeNodes,
						})
					}

					if len(r.ImageData) > 0 {
						children = append(children, core.View{
							Style: core.Style{
								Height:         core.Pt(180),
								CornerRadius:   10,
								Borders:        core.All(1),
								AlignItems:     core.AlignCenter,
								JustifyContent: core.JustifyCenter,
								Overflow:       core.Hidden,
							},
							StyleFunc: func() core.Style {
								return core.Style{Background: ui.Alpha(colors.Inset(), 0.6), BorderColor: colors.Line()}
							},
							Children: []core.Node{
								core.Image{
									SourceFunc: func() core.ImageSource {
										if ctx.IsReleasing() {
											return core.ImageSource{}
										}
										if cur := repSig.Get(); cur != nil {
											return core.ImageSource{Data: cur.ImageData}
										}
										return core.ImageSource{}
									},
									ResizeMode: core.Contain,
									Style:      core.Style{Width: core.Pct(100), Height: core.Pct(100)},
								},
							},
						})
					}

					for _, sec := range r.Sections {
						if len(sec.Rows) > 0 {
							children = append(children, pluginui.SectionCard(cx, sec.Title, sec.Rows, tr.CommonCopy(), onCopy))
						}
					}

					if len(r.Tabs) > 0 {
						opts := make([]ui.Option[string], len(r.Tabs))
						var chosen *ReportTab
						curID := activeTab.Get()
						for i := range r.Tabs {
							opts[i] = ui.Option[string]{Value: r.Tabs[i].ID, Label: r.Tabs[i].Label}
							if r.Tabs[i].ID == curID {
								chosen = &r.Tabs[i]
							}
						}
						if chosen == nil {
							chosen = &r.Tabs[0]
						}
						children = append(children, ui.Tabs[string]{
							Options:  opts,
							Value:    activeTab.Get,
							OnChange: activeTab.Set,
						})
						for _, sec := range chosen.Sections {
							if len(sec.Rows) > 0 {
								children = append(children, pluginui.SectionCard(cx, sec.Title, sec.Rows, tr.CommonCopy(), onCopy))
							}
						}
						if chosen.Text != "" {
							children = append(children, renderCodeCard(cx, "", chosen.Text))
						}
					}

					if r.BodyText != "" {
						children = append(children, renderCodeCard(cx, r.BodyTitle, r.BodyText))
					}

					return core.Scroll{
						Style: core.Style{FlexGrow: 1, FlexShrink: 1, Width: core.Pct(100), Height: core.Pct(100)},
						Children: []core.Node{
							core.View{
								Style:    core.Style{Padding: core.All(16), Gap: 12},
								Children: children,
							},
						},
					}
				})
			},
		},
	)
}

func renderCodeCard(cx *core.Scope, title, body string) core.Node {
	colors := ui.ColorsOf(cx)
	var kids []core.Node
	if title != "" {
		kids = append(kids, ui.Text(title, 13, ui.Semibold, colors.Text))
	}
	kids = append(kids, core.Text{
		Content:    body,
		Selectable: core.SelectableOn,
		Style:      core.Style{FontSize: 12, FontFamily: ui.Mono, LineHeight: 18},
		StyleFunc:  ui.Colored(colors.Text),
	})
	return core.View{
		Style: core.Style{
			Padding:      core.All(14),
			Gap:          8,
			CornerRadius: ui.RadiusCard,
			BorderWidth:  1,
		},
		StyleFunc: func() core.Style {
			return core.Style{
				Background:  ui.Alpha(colors.Surface(), 0.78),
				BorderColor: colors.Border(),
			}
		},
		Children: kids,
	}
}

func hasExt(path string, exts map[string]bool) bool {
	return exts[strings.ToLower(filepath.Ext(path))]
}
