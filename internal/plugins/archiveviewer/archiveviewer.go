package archiveviewer

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"

	"github.com/nsgo-dev/nsgo/base/datatable"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/icons/lucide"
	"github.com/nsgo-dev/nsgo/ui"

	"nsgook/internal/plugins/pluginui"
	"nsgook/internal/plugins/textviewer"
	"nsgook/internal/viewer"
	"nsgook/locales"
)

// Plugin previews compressed and container archives (ZIP, TAR, GZ, BZ2, ASAR, PAK, .DS_Store).
type Plugin struct{}

func New() viewer.Plugin { return Plugin{} }

func (Plugin) ID() string   { return "archiveviewer" }
func (Plugin) Name() string { return "ArchiveViewer" }
func (Plugin) Priority() int {
	return 75
}

func (Plugin) CanHandle(path string) bool {
	return IsSupportedPath(path)
}

func (Plugin) Prepare(path string, ctx *viewer.Context) {
	ctx.PreferredSize = core.Size{Width: 920, Height: 620}
	ctx.Title.Set(viewer.DisplayName(path))
}

func (Plugin) View(cx *core.Scope, path string, ctx *viewer.Context) core.Node {
	colors := ui.ColorsOf(cx)
	tr := locales.For(cx)

	archSig := core.NewSignal[*Archive](cx, nil)
	loadErr := core.NewSignal(cx, "")
	treeMode := core.NewSignal(cx, true)
	searchQuery := core.NewSignal(cx, "")
	selectedPath := core.NewSignal(cx, "")
	sortState := core.NewSignal(cx, datatable.Sort{})

	expanded := core.NewSignalFunc(cx, map[string]bool{}, func(_, _ map[string]bool) bool { return false })
	selection := ui.NewSelection(cx, false)

	previewKind := core.NewSignal(cx, "empty") // "empty", "text", "image", "binary", "error"
	previewText := core.NewSignal(cx, "")
	previewImg := core.NewSignalFunc(cx, []byte(nil), func(_, _ []byte) bool { return false })
	previewMsg := core.NewSignal(cx, "")

	var stopped atomic.Bool
	var previewGen atomic.Uint64
	cx.Cleanup(func() {
		stopped.Store(true)
		archSig.Set(nil)
		previewText.Set("")
		previewImg.Set(nil)
		expanded.Set(nil)
	})
	ctx.TrackMedia(cx)
	cx.Effect(func() {
		if ctx.IsReleasing() {
			stopped.Store(true)
		}
	})

	ctx.IsBusy.Set(true)
	go func() {
		emitted := false
		a, err := OpenArchiveStreaming(path, ScanLimits{
			BatchEntries: 200,
			MaxEntries:   20000,
			Stop:         func() bool { return stopped.Load() },
		}, func(partial *Archive) bool {
			if stopped.Load() {
				return false
			}
			emitted = true
			cx.Post(func() {
				if stopped.Load() {
					return
				}
				archSig.Set(partial)
				ctx.IsBusy.Set(false)
				ctx.Title.Set(fmt.Sprintf("%s (%s)", viewer.DisplayName(path), partial.Format))
			})
			return true
		})
		if stopped.Load() {
			return
		}
		cx.Post(func() {
			if stopped.Load() {
				return
			}
			ctx.IsBusy.Set(false)
			if err != nil {
				if !emitted {
					loadErr.Set(err.Error())
				}
				return
			}
			if a != nil {
				archSig.Set(a)
				ctx.Title.Set(fmt.Sprintf("%s (%s)", viewer.DisplayName(path), a.Format))
			}
		})
	}()

	loadEntryPreview := func(entryPath string) {
		selectedPath.Set(entryPath)
		a := archSig.GetUntracked()
		if a == nil || entryPath == "" {
			previewKind.Set("empty")
			return
		}
		entry := a.ByPath[entryPath]
		if entry == nil || entry.Dir || entry.ReadFunc == nil {
			previewKind.Set("empty")
			return
		}
		if entry.Size > 8*MaxInlinePreviewBytes {
			previewKind.Set("binary")
			previewMsg.Set(tr.ArchEntryTooLarge())
			return
		}

		gen := previewGen.Add(1)
		go func() {
			raw, err := entry.ReadFunc(MaxInlinePreviewBytes)
			if stopped.Load() || previewGen.Load() != gen {
				return
			}
			if err != nil {
				cx.Post(func() {
					if stopped.Load() || previewGen.Load() != gen {
						return
					}
					previewKind.Set("error")
					previewMsg.Set(err.Error())
				})
				return
			}

			ct := http.DetectContentType(raw)
			isImg := strings.HasPrefix(ct, "image/") && !strings.Contains(ct, "svg")
			var txt string
			isText := false
			if !isImg && textviewer.IsTextData(raw) {
				txt, _ = textviewer.DecodeText(raw)
				isText = true
			}

			cx.Post(func() {
				if stopped.Load() || previewGen.Load() != gen {
					return
				}
				switch {
				case isImg:
					previewImg.Set(raw)
					previewKind.Set("image")
				case isText:
					previewText.Set(txt)
					previewKind.Set("text")
				default:
					previewKind.Set("binary")
					previewMsg.Set(locales.For(cx).ArchBinaryEntry(viewer.FormatBytes(uint64(max(0, entry.Size)))))
				}
			})
		}()
	}

	cx.Effect(func() {
		keys := selection.Keys()
		if len(keys) == 0 {
			return
		}
		if k, ok := keys[0].(string); ok && k != selectedPath.GetUntracked() {
			loadEntryPreview(k)
		}
	})

	ctx.MoreItems.Set([]viewer.MenuAction{
		{
			ID:       "arch-tree",
			Label:    tr.ArchTreeMode(),
			Icon:     lucide.FolderTree,
			Checked:  treeMode.Get,
			OnSelect: func() { treeMode.Set(true) },
		},
		{
			ID:       "arch-flat",
			Label:    tr.ArchFlatMode(),
			Icon:     lucide.List,
			Checked:  func() bool { return !treeMode.Get() },
			OnSelect: func() { treeMode.Set(false) },
		},
	})

	filteredEntries := core.NewMemoFunc(cx, func() []*Entry {
		a := archSig.Get()
		if a == nil {
			return nil
		}
		q := strings.ToLower(strings.TrimSpace(searchQuery.Get()))
		var out []*Entry
		for _, e := range a.Entries {
			if e.Dir {
				continue
			}
			if q == "" || strings.Contains(strings.ToLower(e.Path), q) {
				out = append(out, e)
			}
		}
		return out
	}, func(_, _ []*Entry) bool { return false })

	tableCols := []datatable.Column{
		{Key: "name", Title: tr.ArchColName(), Width: 230, Sortable: false},
		{Key: "size", Title: tr.ArchColSize(), Width: 82, Sortable: false},
		{Key: "packed", Title: tr.ArchColPacked(), Width: 82, Sortable: false},
		{Key: "mod", Title: tr.ArchColModified(), Width: 120, Sortable: false},
	}

	toolbar := pluginui.Toolbar(cx,
		pluginui.IconButton{
			Icon:     lucide.FolderTree,
			Label:    tr.ArchTreeMode(),
			Size:     26,
			Selected: treeMode.Get,
			OnPress:  func() { treeMode.Set(true) },
		},
		pluginui.IconButton{
			Icon:     lucide.List,
			Label:    tr.ArchFlatMode(),
			Size:     26,
			Selected: func() bool { return !treeMode.Get() },
			OnPress:  func() { treeMode.Set(false) },
		},
		ui.Input{
			Value:       searchQuery.Get,
			OnChange:    searchQuery.Set,
			Placeholder: locales.For(cx).ArchSearchPlaceholder(),
			Style:       core.Style{Width: core.Pt(220)},
		},
		ui.Spacer{},
		core.Text{
			ContentFunc: func() string {
				a := archSig.Get()
				if a == nil {
					return ""
				}
				files := strconv.Itoa(a.FileCount)
				if a.Scanning || a.Truncated {
					files += "+"
				}
				return locales.For(cx).ArchSummary(
					files,
					viewer.FormatBytes(uint64(max(0, a.TotalSize))),
					viewer.FormatBytes(uint64(max(0, a.PackedTotal))),
				)
			},
			Style:     core.Style{FontSize: 11},
			StyleFunc: ui.Colored(colors.Muted),
		},
	)

	leftPane := core.View{
		Style: core.Style{
			Width:      core.Pct(48),
			MinWidth:   core.Pt(240),
			MinHeight:  core.Pt(0),
			Borders:    core.Edges{Right: 1},
			FlexShrink: 0,
		},
		StyleFunc: func() core.Style { return core.Style{BorderColor: colors.Line()} },
		Children: []core.Node{
			core.Dynamic(func(cx *core.Scope) core.Node {
				a := archSig.Get()
				if a == nil {
					return core.View{}
				}
				if !treeMode.Get() || strings.TrimSpace(searchQuery.Get()) != "" {
					rows := filteredEntries.Get()
					return datatable.DataTable{
						Columns:  func() []datatable.Column { return tableCols },
						RowCount: func() int { return len(rows) },
						Text: func(r, c int) string {
							if r < 0 || r >= len(rows) {
								return ""
							}
							e := rows[r]
							switch c {
							case 0:
								return e.Path
							case 1:
								return viewer.FormatBytes(uint64(max(0, e.Size)))
							case 2:
								return viewer.FormatBytes(uint64(max(0, e.PackedSize)))
							case 3:
								if e.ModTime.IsZero() {
									return "-"
								}
								return e.ModTime.Format("2006-01-02 15:04")
							}
							return ""
						},
						Sort:         sortState.Get,
						OnSortChange: sortState.Set,
						RowHeight:    28,
						HeaderHeight: 32,
						StylesFunc:   pluginui.DataTableStyles(cx),
						OnSelectionChange: func(sel datatable.Selection) {
							if sel.Row >= 0 && sel.Row < len(rows) {
								loadEntryPreview(rows[sel.Row].Path)
							}
						},
						Style: core.Style{FlexGrow: 1, FlexShrink: 1, MinWidth: core.Pt(0), MinHeight: core.Pt(0)},
					}
				}

				return ui.Tree{
					Roots: func() []any { return a.Roots },
					Children: func(key any) []any {
						if s, ok := key.(string); ok {
							return a.Children[s]
						}
						return nil
					},
					Expanded: func(key any) bool {
						if s, ok := key.(string); ok {
							return expanded.Get()[s]
						}
						return false
					},
					OnExpand: func(key any, exp bool) {
						s, ok := key.(string)
						if !ok {
							return
						}
						m := make(map[string]bool, len(expanded.GetUntracked())+1)
						for k, v := range expanded.GetUntracked() {
							m[k] = v
						}
						m[s] = exp
						expanded.Set(m)
					},
					Selection:     selection,
					EstimatedSize: 28,
					Style:         core.Style{FlexGrow: 1, FlexShrink: 1, MinWidth: core.Pt(0), MinHeight: core.Pt(0), Padding: core.All(6)},
					Row: func(cx *core.Scope, item ui.TreeItem) core.Node {
						key, _ := item.Key().(string)
						e := a.ByPath[key]
						isDir := e != nil && e.Dir
						icon := lucide.FileText
						if isDir {
							icon = lucide.Folder
						}
						label := key
						if e != nil && e.Name != "" {
							label = e.Name
						}
						sizeStr := ""
						if e != nil && !e.Dir {
							sizeStr = viewer.FormatBytes(uint64(max(0, e.Size)))
						}
						return core.View{
							Style: core.Style{
								Direction:  core.Row,
								AlignItems: core.AlignCenter,
								Gap:        6,
								FlexGrow:   1,
								MinWidth:   core.Pt(0),
							},
							Children: []core.Node{
								ui.IconOf(icon, 14, func() core.Color {
									if isDir {
										return colors.Accent()
									}
									return colors.Muted()
								}),
								core.Text{
									Content:   label,
									MaxLines:  1,
									Style:     core.Style{FontSize: 12, FlexGrow: 1, FlexShrink: 1},
									StyleFunc: ui.Colored(colors.Text),
								},
								core.Text{
									Content:   sizeStr,
									Style:     core.Style{FontSize: 11, FlexShrink: 0},
									StyleFunc: ui.Colored(colors.Muted),
								},
							},
						}
					},
				}
			}),
		},
	}

	rightPane := core.View{
		Style: core.Style{
			FlexGrow:   1,
			FlexShrink: 1,
			MinWidth:   core.Pt(0),
			MinHeight:  core.Pt(0),
		},
		Children: []core.Node{
			core.Show{
				When: func() bool { return selectedPath.Get() != "" },
				Then: func(cx *core.Scope) core.Node {
					return pluginui.StatusBar(cx,
						core.Text{
							ContentFunc: selectedPath.Get,
							MaxLines:    1,
							Style:       core.Style{FontSize: 12, FontWeight: ui.Medium, FlexGrow: 1, FlexShrink: 1},
							StyleFunc:   ui.Colored(colors.Text),
						},
					)
				},
			},
			core.Dynamic(func(cx *core.Scope) core.Node {
				switch previewKind.Get() {
				case "text":
					return core.Scroll{
						Style: core.Style{FlexGrow: 1, FlexShrink: 1, Width: core.Pct(100), Height: core.Pct(100)},
						Children: []core.Node{
							core.View{
								Style: core.Style{Padding: core.All(12)},
								Children: []core.Node{
									core.Text{
										ContentFunc: previewText.Get,
										Selectable:  core.SelectableOn,
										Style:       core.Style{FontSize: 12, FontFamily: ui.Mono, LineHeight: 18},
										StyleFunc:   ui.Colored(colors.Text),
									},
								},
							},
						},
					}
				case "image":
					return core.View{
						Style: core.Style{
							FlexGrow:       1,
							FlexShrink:     1,
							Padding:        core.All(14),
							AlignItems:     core.AlignCenter,
							JustifyContent: core.JustifyCenter,
						},
						Children: []core.Node{
							core.Image{
								SourceFunc: func() core.ImageSource {
									if ctx.IsReleasing() {
										return core.ImageSource{}
									}
									return core.ImageSource{Data: previewImg.Get()}
								},
								ResizeMode: core.Contain,
								Style:      core.Style{Width: core.Pct(100), Height: core.Pct(100)},
							},
						},
					}
				case "binary", "error":
					return pluginui.EmptyState(cx, lucide.File, selectedPath.Get(), previewMsg.Get())
				default:
					return pluginui.EmptyState(cx, lucide.Archive, viewer.DisplayName(path), locales.For(cx).ArchSelectHint())
				}
			}),
		},
	}

	return pluginui.Fill(
		core.Show{
			When: func() bool { return loadErr.Get() != "" },
			Then: func(cx *core.Scope) core.Node {
				return pluginui.EmptyState(cx, lucide.FileExclamationPoint, locales.For(cx).CommonError(), loadErr.Get())
			},
			Else: func(*core.Scope) core.Node {
				return pluginui.Fill(
					toolbar,
					core.View{
						Style: core.Style{
							Direction:  core.Row,
							FlexGrow:   1,
							FlexShrink: 1,
							MinWidth:   core.Pt(0),
							MinHeight:  core.Pt(0),
						},
						Children: []core.Node{leftPane, rightPane},
					},
				)
			},
		},
	)
}
