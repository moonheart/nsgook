package binaryviewer

import (
	"fmt"
	"io"
	"os"
	"strconv"
	"sync"
	"sync/atomic"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/icons/lucide"
	"github.com/nsgo-dev/nsgo/ui"

	"nsgook/internal/plugins/pluginui"
	"nsgook/internal/viewer"
	"nsgook/locales"
)

// Plugin provides a virtualized streaming hex viewer for arbitrary binary files.
// Priority is -10 so specialized viewers take precedence unless opened via More -> "View as Binary (Hex)".
type Plugin struct{}

func New() viewer.Plugin { return Plugin{} }

func (Plugin) ID() string   { return "binaryviewer" }
func (Plugin) Name() string { return "BinaryViewer" }
func (Plugin) Priority() int {
	return -10
}

func (Plugin) CanHandle(string) bool { return true }

func (Plugin) Prepare(path string, ctx *viewer.Context) {
	ctx.PreferredSize = core.Size{Width: 920, Height: 600}
	ctx.Title.Set(viewer.DisplayName(path))
}

func (Plugin) ExtendedMenuItems(cx *core.Scope, path string, ctx *viewer.Context) []viewer.MenuAction {
	if ctx.IsDir {
		return nil
	}
	return []viewer.MenuAction{
		{
			ID:    "ext-view-hex",
			Label: locales.For(cx).ViewAsHex(),
			Icon:  lucide.Binary,
			OnSelect: func() {
				if ctx.SwitchPlugin != nil {
					ctx.SwitchPlugin("binaryviewer")
				}
			},
		},
	}
}

func (Plugin) View(cx *core.Scope, path string, ctx *viewer.Context) core.Node {
	colors := ui.ColorsOf(cx)
	tr := locales.For(cx)

	fileSize := core.NewSignal(cx, int64(0))
	loadErr := core.NewSignal(cx, "")
	selectedOffset := core.NewSignal(cx, int64(0))
	gotoText := core.NewSignal(cx, "")
	inspectorSlice := core.NewSignalFunc(cx, []byte(nil), func(_, _ []byte) bool { return false })

	var mu sync.Mutex
	var handle *os.File
	var stopped atomic.Bool

	cx.Cleanup(func() {
		stopped.Store(true)
		inspectorSlice.Set(nil)
		mu.Lock()
		if handle != nil {
			_ = handle.Close()
			handle = nil
		}
		mu.Unlock()
	})

	readWindow := func(off int64, length int) []byte {
		mu.Lock()
		f := handle
		mu.Unlock()
		if f == nil || off < 0 {
			return nil
		}
		buf := make([]byte, length)
		n, err := f.ReadAt(buf, off)
		if err != nil && err != io.EOF {
			return nil
		}
		return buf[:n]
	}

	selectOffset := func(off int64) {
		if off < 0 {
			off = 0
		}
		selectedOffset.Set(off)
		inspectorSlice.Set(readWindow(off, 16))
	}

	ctx.IsBusy.Set(true)
	go func() {
		f, err := os.Open(path)
		var size int64
		if err == nil {
			if st, serr := f.Stat(); serr == nil {
				size = st.Size()
			} else {
				err = serr
			}
		}
		if err != nil && f != nil {
			_ = f.Close()
			f = nil
		}
		if stopped.Load() {
			if f != nil {
				_ = f.Close()
			}
			return
		}
		var firstBytes []byte
		if f != nil {
			buf := make([]byte, 16)
			n, _ := f.ReadAt(buf, 0)
			firstBytes = buf[:n]
		}
		cx.Post(func() {
			if stopped.Load() {
				if f != nil {
					_ = f.Close()
				}
				return
			}
			ctx.IsBusy.Set(false)
			if err != nil {
				loadErr.Set(err.Error())
				return
			}
			mu.Lock()
			handle = f
			mu.Unlock()
			fileSize.Set(size)
			inspectorSlice.Set(firstBytes)
		})
	}()

	listRef := &core.Ref{}
	jumpTo := func() {
		off, err := ParseOffset(gotoText.GetUntracked(), fileSize.GetUntracked())
		if err != nil {
			if ctx.Toast != nil {
				ctx.Toast(ui.ToastWarning, err.Error())
			}
			return
		}
		selectOffset(off)
		listRef.ScrollToIndex(int(off/16), false)
	}

	toolbar := pluginui.Toolbar(cx,
		ui.Text(tr.HexOffset()+":", 12, ui.Medium, colors.Muted),
		ui.Input{
			Value:       gotoText.Get,
			OnChange:    gotoText.Set,
			OnSubmit:    func(string) { jumpTo() },
			Placeholder: locales.For(cx).HexGoToPlaceholder(),
			Style:       core.Style{Width: core.Pt(180)},
		},
		ui.Button{
			Text:    locales.For(cx).HexGoTo(),
			Variant: ui.ButtonSecondary,
			Size:    ui.SizeSmall,
			OnPress: jumpTo,
		},
		ui.Spacer{},
		core.Text{
			ContentFunc: func() string {
				sz := uint64(max(0, fileSize.Get()))
				return locales.For(cx).HexSize(viewer.FormatBytes(sz), strconv.FormatInt(fileSize.Get(), 10))
			},
			Style:     core.Style{FontSize: 11, FontFamily: ui.Mono},
			StyleFunc: ui.Colored(colors.Muted),
		},
	)

	hexList := core.List{
		Ref: listRef,
		Count: func() int {
			sz := fileSize.Get()
			if sz <= 0 {
				return 0
			}
			return int((sz + 15) / 16)
		},
		Key:           func(i int) any { return i },
		EstimatedSize: 22,
		Style:         core.Style{FlexGrow: 1, FlexShrink: 1, MinWidth: core.Pt(0), MinHeight: core.Pt(0)},
		Row: func(cx *core.Scope, row core.ListRow) core.Node {
			return ui.Press{
				OnPress: func() {
					off := int64(row.Index()) * 16
					selectOffset(off)
				},
				Children: []core.Node{
					core.View{
						Style: core.Style{
							Direction:  core.Row,
							AlignItems: core.AlignCenter,
							Gap:        14,
							Height:     core.Pt(22),
							Padding:    core.Symmetric(0, 12),
						},
						StyleFunc: func() core.Style {
							rowStart := int64(row.Index()) * 16
							sel := selectedOffset.Get()
							if sel >= rowStart && sel < rowStart+16 {
								return core.Style{Background: ui.Alpha(colors.Accent(), 0.16)}
							}
							if row.Index()%2 == 1 {
								return core.Style{Background: ui.Alpha(colors.Inset(), 0.35)}
							}
							return core.Style{Hover: &core.Style{Background: colors.Hover()}}
						},
						Children: []core.Node{
							core.Text{
								ContentFunc: func() string {
									return fmt.Sprintf("%08X", int64(row.Index())*16)
								},
								Style:     core.Style{FontSize: 12, FontFamily: ui.Mono, Width: core.Pt(76)},
								StyleFunc: ui.Colored(colors.Accent),
							},
							core.Text{
								ContentFunc: func() string {
									off := int64(row.Index()) * 16
									hexStr, _ := FormatHexRow(readWindow(off, 16))
									return hexStr
								},
								Style:     core.Style{FontSize: 12, FontFamily: ui.Mono, FlexGrow: 1},
								StyleFunc: ui.Colored(colors.Text),
							},
							core.Text{
								ContentFunc: func() string {
									off := int64(row.Index()) * 16
									_, asciiStr := FormatHexRow(readWindow(off, 16))
									return asciiStr
								},
								Style:     core.Style{FontSize: 12, FontFamily: ui.Mono, Width: core.Pt(140)},
								StyleFunc: ui.Colored(colors.Muted),
							},
						},
					},
				},
			}
		},
	}

	inspectorSidebar := core.View{
		Style: core.Style{
			Width:      core.Pt(250),
			MinHeight:  core.Pt(0),
			Borders:    core.Edges{Left: 1},
			FlexShrink: 0,
		},
		StyleFunc: func() core.Style {
			return core.Style{
				Background:  ui.Alpha(colors.Inset(), 0.45),
				BorderColor: colors.Line(),
			}
		},
		Children: []core.Node{
			core.Scroll{
				Style: core.Style{FlexGrow: 1, FlexShrink: 1, Width: core.Pct(100), Height: core.Pct(100)},
				Children: []core.Node{
					core.View{
						Style: core.Style{Padding: core.All(12), Gap: 8},
						Children: []core.Node{
							core.Text{
								ContentFunc: func() string {
									return fmt.Sprintf("%s · 0x%X", locales.For(cx).HexInspector(), selectedOffset.Get())
								},
								Style:     core.Style{FontSize: 12, FontWeight: ui.Semibold},
								StyleFunc: ui.Colored(colors.Text),
							},
							core.Dynamic(func(cx *core.Scope) core.Node {
								fields := DecodeInspector(inspectorSlice.Get())
								rows := make([]pluginui.KV, 0, len(fields))
								for _, f := range fields {
									rows = append(rows, pluginui.KV{Key: f.Label, Value: f.Value})
								}
								return pluginui.SectionCard(cx, "", rows, tr.CommonCopy(), func(val string) {
									cx.SetClipboard(val)
									if ctx.Toast != nil {
										ctx.Toast(ui.ToastSuccess, tr.CommonCopied())
									}
								})
							}),
						},
					},
				},
			},
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
						Children: []core.Node{hexList, inspectorSidebar},
					},
				)
			},
		},
	)
}
