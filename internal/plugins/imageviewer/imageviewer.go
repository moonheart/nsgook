package imageviewer

import (
	"bytes"
	"fmt"
	"image"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/nsgo-dev/nsgo/base/camera"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/icons/lucide"
	"github.com/nsgo-dev/nsgo/ui"

	"nsgook/internal/plugins/pluginui"
	"nsgook/internal/viewer"
	"nsgook/locales"
)

// Plugin previews raster images, animated GIFs, SVGs, and raw/bitmap formats with pan, zoom, rotate, and flip.
type Plugin struct{}

func New() viewer.Plugin { return Plugin{} }

func (Plugin) ID() string    { return "imageviewer" }
func (Plugin) Name() string  { return "ImageViewer" }
func (Plugin) Priority() int { return 0 }

func (Plugin) CanHandle(path string) bool {
	if path == "" || strings.HasPrefix(path, "::") {
		return false
	}
	ext := filepath.Ext(path)
	if ext != "" {
		return isSupportedExt(ext)
	}
	return hasImageMagic(path)
}

func (Plugin) Prepare(path string, ctx *viewer.Context) {
	meta := ReadMeta(path)
	if meta.Width > 0 && meta.Height > 0 {
		ctx.SetPreferredSizeFit(core.Size{Width: core.Points(meta.Width), Height: core.Points(meta.Height)}, 0.8, ctx.DesktopSize)
		ctx.Title.Set(fmt.Sprintf("%d×%d: %s", meta.Width, meta.Height, viewer.DisplayName(path)))
	} else {
		ctx.PreferredSize = core.Size{Width: 800, Height: 600}
		ctx.Title.Set(viewer.DisplayName(path))
	}
}

var (
	freeMemMu    sync.Mutex
	freeMemTimer *time.Timer
)

func scheduleMemoryRelease() {
	freeMemMu.Lock()
	if freeMemTimer != nil {
		freeMemTimer.Stop()
	}
	freeMemTimer = time.AfterFunc(350*time.Millisecond, func() {
		debug.FreeOSMemory()
	})
	freeMemMu.Unlock()
}

func (Plugin) View(cx *core.Scope, path string, ctx *viewer.Context) core.Node {
	colors := ui.ColorsOf(cx)
	tr := locales.For(cx)

	cfgCheckered := true
	cfgRenderSvgWeb := true
	if ctx.Config != nil {
		c := ctx.Config.Get()
		cfgCheckered = c.ImageCheckeredBg
		cfgRenderSvgWeb = c.RenderSvgWeb
	}

	meta := ReadMeta(path)
	useNativeData := CanNativeURI(meta)

	rawData := core.NewSignalFunc[[]byte](cx, nil, func(a, b []byte) bool {
		return len(a) == len(b) && (len(a) == 0 || &a[0] == &b[0])
	})
	decoded := core.NewSignal[*DecodedImage](cx, nil)
	nativeFailed := core.NewSignal(cx, false)
	loadErr := core.NewSignal(cx, "")
	turns := core.NewSignal(cx, 0)
	flipH := core.NewSignal(cx, false)
	flipV := core.NewSignal(cx, false)
	showChecker := core.NewSignal(cx, cfgCheckered)
	showMeta := core.NewSignal(cx, false)
	pixVersion := core.NewSignal(cx, uint64(1))
	viewSize := core.NewSignal(cx, core.Size{})
	cam := &camera.State{}

	var stopped atomic.Bool
	var decodeStarted atomic.Bool
	cx.Cleanup(func() {
		stopped.Store(true)
		hadDecoded := decoded.GetUntracked() != nil
		hadRaw := len(rawData.GetUntracked()) > 0
		rawData.Set(nil)
		decoded.Set(nil)
		if hadDecoded || hadRaw || int64(meta.Width)*int64(meta.Height) >= 4_000_000 {
			scheduleMemoryRelease()
		}
	})
	ctx.TrackMedia(cx)
	controlsHide := pluginui.NewAutoHide(cx, 2500*time.Millisecond)
	controlsHide.Track(cx, ctx.PointerInside, ctx.PointerActivity)

	// Extremely tall screenshots are unreadable when the whole image is fitted
	// into a narrow window. Start those at fit-width so the content is legible;
	// the "fit window" control still shows the whole image.
	initialFitDone := false
	cx.Effect(func() {
		if initialFitDone || stopped.Load() {
			return
		}
		bounds, ok := tallImageFitBounds(viewSize.Get(), meta.Width, meta.Height)
		if !ok {
			return
		}
		initialFitDone = true
		cam.FitBounds(bounds)
	})

	ensureDecoded := func() {
		if !decodeStarted.CompareAndSwap(false, true) {
			return
		}
		ctx.IsBusy.Set(true)
		go func() {
			img, err := DecodeFile(path)
			if stopped.Load() {
				return
			}
			cx.Post(func() {
				if stopped.Load() {
					return
				}
				ctx.IsBusy.Set(false)
				if err != nil {
					if !useNativeData || nativeFailed.GetUntracked() {
						loadErr.Set(err.Error())
					}
					return
				}
				if useNativeData && !nativeFailed.GetUntracked() {
					t := ((turns.GetUntracked() % 4) + 4) % 4
					if t == 0 && !flipH.GetUntracked() && !flipV.GetUntracked() {
						decodeStarted.Store(false)
						return
					}
				}
				decoded.Set(img)
				if img.Meta.Width > 0 && img.Meta.Height > 0 {
					ctx.Title.Set(fmt.Sprintf("%d×%d: %s", img.Meta.Width, img.Meta.Height, viewer.DisplayName(path)))
				}
			})
		}()
	}

	syncDecodedForTransform := func() {
		if !useNativeData || nativeFailed.GetUntracked() {
			return
		}
		t := ((turns.GetUntracked() % 4) + 4) % 4
		if t == 0 && !flipH.GetUntracked() && !flipV.GetUntracked() {
			if decoded.GetUntracked() != nil {
				decoded.Set(nil)
				decodeStarted.Store(false)
				scheduleMemoryRelease()
			}
			return
		}
		ensureDecoded()
	}

	if useNativeData {
		ctx.IsBusy.Set(true)
		go func() {
			data, err := os.ReadFile(path)
			if stopped.Load() {
				return
			}
			cx.Post(func() {
				if stopped.Load() {
					return
				}
				if err != nil {
					ctx.IsBusy.Set(false)
					loadErr.Set(err.Error())
					return
				}
				if len(data) == 0 {
					nativeFailed.Set(true)
					ensureDecoded()
					return
				}
				rawData.Set(data)
			})
		}()
	} else {
		ensureDecoded()
	}

	// Transformed RGBA bitmap computed from decoded image + rotation/flip.
	transformed := core.NewMemoFunc(cx, func() *image.RGBA {
		if ctx.IsReleasing() {
			return nil
		}
		d := decoded.Get()
		if d == nil || d.RGBA == nil {
			return nil
		}
		t := ((turns.Get() % 4) + 4) % 4
		fh, fv := flipH.Get(), flipV.Get()
		if t == 0 && !fh && !fv {
			return d.RGBA
		}
		res := Rotate90(d.RGBA, t)
		if fh || fv {
			res = Flip(res, fh, fv)
		}
		return res
	}, func(_, _ *image.RGBA) bool { return false })

	cx.Effect(func() {
		if ctx.IsReleasing() {
			stopped.Store(true)
			_ = transformed.GetUntracked()
		}
	})

	bumpVersion := func() {
		pixVersion.Update(func(v uint64) uint64 { return v + 1 })
	}

	copyImage := func() {
		d := decoded.GetUntracked()
		if d == nil {
			if !useNativeData {
				return
			}
			if data := rawData.GetUntracked(); len(data) > 0 {
				cx.SetClipboardImage(data)
				if ctx.Toast != nil {
					ctx.Toast(ui.ToastSuccess, locales.For(cx).ImgCopied())
				}
				return
			}
			go func() {
				data, err := os.ReadFile(path)
				if err != nil || len(data) == 0 || stopped.Load() {
					return
				}
				cx.Post(func() {
					if stopped.Load() {
						return
					}
					cx.SetClipboardImage(data)
					if ctx.Toast != nil {
						ctx.Toast(ui.ToastSuccess, locales.For(cx).ImgCopied())
					}
				})
			}()
			return
		}
		pix := core.Untracked(cx, transformed.Get)
		if pix == nil && len(d.RawData) > 0 {
			cx.SetClipboardImage(d.RawData)
			if ctx.Toast != nil {
				ctx.Toast(ui.ToastSuccess, locales.For(cx).ImgCopied())
			}
			return
		}
		if pix == nil {
			return
		}
		go func() {
			var buf bytes.Buffer
			if err := png.Encode(&buf, pix); err == nil {
				data := buf.Bytes()
				if stopped.Load() {
					return
				}
				cx.Post(func() {
					if stopped.Load() {
						return
					}
					cx.SetClipboardImage(data)
					if ctx.Toast != nil {
						ctx.Toast(ui.ToastSuccess, locales.For(cx).ImgCopied())
					}
				})
			}
		}()
	}

	zoomActual := func() {
		w, h := float64(meta.Width), float64(meta.Height)
		t := ((turns.GetUntracked() % 4) + 4) % 4
		if t%2 == 1 {
			w, h = h, w
		}
		if d := decoded.GetUntracked(); d != nil {
			w, h = float64(d.Meta.Width), float64(d.Meta.Height)
			if pix := core.Untracked(cx, transformed.Get); pix != nil {
				w, h = float64(pix.Bounds().Dx()), float64(pix.Bounds().Dy())
			}
		} else if !useNativeData || nativeFailed.GetUntracked() {
			return
		}
		vs := viewSize.GetUntracked()
		if w <= 0 || h <= 0 || vs.Width <= 0 || vs.Height <= 0 {
			return
		}
		// FitBounds fits into (viewport - 2*margin), capped at zoom=1.0, so giving viewport-2*margin centered on image yields 1:1.
		margin := core.Points(32)
		bw := max(core.Points(1), vs.Width-margin*2)
		bh := max(core.Points(1), vs.Height-margin*2)
		cam.FitBounds(core.Bounds{
			Origin: core.Point{X: core.Points(w/2) - bw/2, Y: core.Points(h/2) - bh/2},
			Size:   core.Size{Width: bw, Height: bh},
		})
	}

	ctx.MoreItems.Set([]viewer.MenuAction{
		{
			ID:       "img-copy",
			Label:    tr.ImgCopyImage(),
			Icon:     lucide.Copy,
			OnSelect: copyImage,
		},
		{
			ID:       "img-checker",
			Label:    tr.ImgCheckerToggle(),
			Icon:     lucide.Grid2x2,
			Checked:  showChecker.Get,
			OnSelect: func() { showChecker.Set(!showChecker.GetUntracked()) },
		},
		{
			ID:       "img-meta",
			Label:    tr.ImgMetadata(),
			Icon:     lucide.Info,
			Checked:  showMeta.Get,
			OnSelect: func() { showMeta.Set(!showMeta.GetUntracked()) },
		},
	})

	checkerCanvas := core.Canvas{
		StyleFunc: func() core.Style {
			if !showChecker.Get() {
				return core.Style{Display: core.DisplayNone}
			}
			return core.Style{
				Position: core.Absolute,
				Top:      core.Pt(0),
				Left:     core.Pt(0),
				Right:    core.Pt(0),
				Bottom:   core.Pt(0),
			}
		},
		Draw: func(d *core.Drawing, size core.Size) {
			w, h := float32(size.Width), float32(size.Height)
			if w <= 0 || h <= 0 {
				return
			}
			c1 := core.RGB(0xE8E8EC)
			c2 := core.RGB(0xD4D4DC)
			if cx.Dark() {
				c1 = core.RGB(0x26262B)
				c2 = core.RGB(0x1E1E22)
			}
			d.SetFillColor(c1)
			d.FillRect(0, 0, w, h)

			tile := float32(14)
			if longest := max(w, h); longest > tile*128 {
				tile = longest / 128
			}
			cols := int(w/tile) + 1
			rows := int(h/tile) + 1
			d.SetFillColor(c2)
			d.BeginPath()
			for c := 1; c < cols; c += 2 {
				d.Rect(float32(c)*tile, 0, tile, h)
			}
			for r := 1; r < rows; r += 2 {
				d.Rect(0, float32(r)*tile, w, tile)
			}
			d.FillEvenOdd()
		},
	}

	imageSurface := core.Dynamic(func(*core.Scope) core.Node {
		useNative := useNativeData && !nativeFailed.Get()
		if !useNative {
			d := decoded.Get()
			if d == nil {
				return core.View{}
			}
			if d.IsSVG && cfgRenderSvgWeb && cx.Env().HasWebView() {
				html := buildSVGHTML(string(d.RawData), showChecker.Get(), cx.Dark())
				return core.WebView{
					Source:    core.WebHTML(html, ""),
					Incognito: true,
					PinchZoom: true,
					Style:     core.Style{FlexGrow: 1, FlexShrink: 1, Width: core.Pct(100), Height: core.Pct(100)},
				}
			}
		}

		displaySize := func() core.Size {
			if d := decoded.Get(); d != nil {
				if d.IsSVG {
					return core.Size{Width: core.Points(d.Meta.Width), Height: core.Points(d.Meta.Height)}
				}
				pix := transformed.Get()
				if pix != nil {
					return core.Size{Width: core.Points(pix.Bounds().Dx()), Height: core.Points(pix.Bounds().Dy())}
				}
				return core.Size{Width: core.Points(d.Meta.Width), Height: core.Points(d.Meta.Height)}
			}
			w, h := meta.Width, meta.Height
			if ((turns.Get()%4)+4)%4%2 == 1 {
				w, h = h, w
			}
			return core.Size{Width: core.Points(w), Height: core.Points(h)}
		}

		content := core.View{
			StyleFunc: func() core.Style {
				sz := displaySize()
				return core.Style{Width: core.Pt(sz.Width), Height: core.Pt(sz.Height), Overflow: core.Hidden}
			},
			Children: []core.Node{
				checkerCanvas,
				core.Image{
					ResizeMode: core.Contain,
					Style:      core.Style{Width: core.Pct(100), Height: core.Pct(100)},
					OnLoadStart: func() {
						if !stopped.Load() {
							ctx.IsBusy.Set(true)
						}
					},
					OnLoad: func(core.Size) {
						if !stopped.Load() {
							ctx.IsBusy.Set(false)
						}
					},
					OnLoadEnd: func() {
						if !stopped.Load() && !decodeStarted.Load() {
							ctx.IsBusy.Set(false)
						}
					},
					OnError: func(error) {
						if stopped.Load() {
							return
						}
						if useNativeData && !nativeFailed.GetUntracked() {
							rawData.Set(nil)
							nativeFailed.Set(true)
							ensureDecoded()
							return
						}
						ctx.IsBusy.Set(false)
					},
					SourceFunc: func() core.ImageSource {
						if ctx.IsReleasing() {
							return core.ImageSource{}
						}
						t := ((turns.Get() % 4) + 4) % 4
						fh, fv := flipH.Get(), flipV.Get()
						if useNativeData && !nativeFailed.Get() && t == 0 && !fh && !fv {
							data := rawData.Get()
							if len(data) == 0 {
								return core.ImageSource{}
							}
							return core.ImageSource{
								Data: data,
								Size: core.Size{Width: core.Points(meta.Width), Height: core.Points(meta.Height)},
							}
						}
						d := decoded.Get()
						if d == nil {
							return core.ImageSource{}
						}
						if d.IsSVG {
							return core.ImageSource{Data: d.RawData}
						}
						if d.Animated && t == 0 && !fh && !fv && len(d.RawData) > 0 {
							return core.ImageSource{Data: d.RawData}
						}
						pix := transformed.Get()
						if pix == nil {
							return core.ImageSource{}
						}
						return core.ImageSource{Pixels: pix, Version: pixVersion.Get()}
					},
				},
			},
		}

		controls := pluginui.FloatingPill(cx,
			pluginui.IconButton{Icon: lucide.ZoomOut, Label: locales.For(cx).ImgZoomOut(), Size: 26, OnPress: cam.ZoomOut},
			core.Text{
				ContentFunc: func() string { return fmt.Sprintf("%.0f%%", cam.Zoom()*100) },
				Style:       core.Style{FontSize: 11, FontFamily: ui.Mono, MinWidth: core.Pt(42), TextAlign: core.TextAlignCenter},
				StyleFunc:   ui.Colored(colors.Text),
			},
			pluginui.IconButton{Icon: lucide.ZoomIn, Label: locales.For(cx).ImgZoomIn(), Size: 26, OnPress: cam.ZoomIn},
			ui.Button{Text: locales.For(cx).ImgZoomFit(), Variant: ui.ButtonGhost, Size: ui.SizeSmall, OnPress: cam.Fit},
			ui.Button{Text: locales.For(cx).ImgZoomActual(), Variant: ui.ButtonGhost, Size: ui.SizeSmall, OnPress: zoomActual},
			core.View{Style: core.Style{Width: core.Pt(1), Height: core.Pt(16), Margin: core.Symmetric(0, 2)}, StyleFunc: ui.Filled(colors.Line)},
			pluginui.IconButton{Icon: lucide.RotateCcw, Label: locales.For(cx).ImgRotateLeft(), Size: 26, OnPress: func() {
				turns.Update(func(n int) int { return n + 3 })
				bumpVersion()
				syncDecodedForTransform()
			}},
			pluginui.IconButton{Icon: lucide.RotateCw, Label: locales.For(cx).ImgRotateRight(), Size: 26, OnPress: func() {
				turns.Update(func(n int) int { return n + 1 })
				bumpVersion()
				syncDecodedForTransform()
			}},
			pluginui.IconButton{Icon: lucide.MirrorRectangular, Label: locales.For(cx).ImgFlipH(), Size: 26, Selected: flipH.Get, OnPress: func() {
				flipH.Set(!flipH.GetUntracked())
				bumpVersion()
				syncDecodedForTransform()
			}},
			pluginui.IconButton{Icon: lucide.Grid2x2, Label: locales.For(cx).ImgCheckerToggle(), Size: 26, Selected: showChecker.Get, OnPress: func() {
				showChecker.Set(!showChecker.GetUntracked())
			}},
			pluginui.IconButton{Icon: lucide.Copy, Label: locales.For(cx).ImgCopyImage(), Size: 26, OnPress: copyImage},
		)
		controls.OnMouseEnter = func(core.MouseEvent) { controlsHide.Hover(true) }
		controls.OnMouseLeave = func(core.MouseEvent) { controlsHide.Hover(false) }

		return core.View{
			OnLayout: func(b core.Bounds) { viewSize.Set(b.Size) },
			Style:    core.Style{FlexGrow: 1, FlexShrink: 1, MinWidth: core.Pt(0), MinHeight: core.Pt(0)},
			Children: []core.Node{
				camera.View{
					State:     cam,
					MinZoom:   0.05,
					MaxZoom:   16,
					FitMargin: 16,
					Content:   content,
					OnHover: func(_ core.Point, over bool) {
						if over {
							controlsHide.Show()
						} else {
							controlsHide.Hover(false)
						}
					},
					Children: []core.Node{controlsHide.Wrap(controls)},
				},
			},
		}
	})

	metaOverlay := core.Show{
		When: func() bool {
			return showMeta.Get() && ((useNativeData && !nativeFailed.Get()) || decoded.Get() != nil)
		},
		Then: func(cx *core.Scope) core.Node {
			m := meta
			if d := decoded.Get(); d != nil {
				m = d.Meta
			}
			rows := []pluginui.KV{
				{Key: locales.For(cx).InfoKind(), Value: m.Format},
				{Key: "Dimensions", Value: fmt.Sprintf("%d × %d px", m.Width, m.Height)},
			}
			if m.CameraMake != "" || m.CameraModel != "" {
				rows = append(rows, pluginui.KV{Key: "Camera", Value: strings.TrimSpace(m.CameraMake + " " + m.CameraModel)})
			}
			if m.DateTime != "" {
				rows = append(rows, pluginui.KV{Key: "Date", Value: m.DateTime})
			}
			if m.Orientation > 0 {
				rows = append(rows, pluginui.KV{Key: "EXIF Orientation", Value: fmt.Sprintf("%d", m.Orientation)})
			}
			return core.View{
				Style: core.Style{Position: core.Absolute, Top: core.Pt(12), Right: core.Pt(12), Width: core.Pt(290)},
				Children: []core.Node{
					pluginui.SectionCard(cx, locales.For(cx).ImgMetadata(), rows, locales.For(cx).CommonCopy(), func(val string) {
						cx.SetClipboard(val)
					}),
				},
			}
		},
	}

	return pluginui.Fill(
		core.Show{
			When: func() bool { return loadErr.Get() != "" },
			Then: func(cx *core.Scope) core.Node {
				return pluginui.EmptyState(cx, lucide.ImageOff, locales.For(cx).CommonError(), loadErr.Get())
			},
			Else: func(*core.Scope) core.Node {
				return core.View{
					Style:    core.Style{FlexGrow: 1, FlexShrink: 1, MinWidth: core.Pt(0), MinHeight: core.Pt(0)},
					Children: []core.Node{imageSurface, metaOverlay},
				}
			},
		},
	)
}

func tallImageFitBounds(view core.Size, width, height int) (core.Bounds, bool) {
	if view.Width <= 0 || view.Height <= 0 || width <= 0 || height <= 0 {
		return core.Bounds{}, false
	}
	if float64(height) <= float64(width)*3 {
		return core.Bounds{}, false
	}
	scale := math.Min(1, float64(view.Width)/float64(width))
	if scale <= 0 {
		return core.Bounds{}, false
	}
	visible := float64(view.Height) / scale
	if visible > float64(height) {
		visible = float64(height)
	}
	return core.Bounds{
		Size: core.Size{Width: core.Points(width), Height: core.Points(visible)},
	}, true
}

func buildSVGHTML(svg string, checkered, dark bool) string {
	bg := "#ffffff"
	c1, c2 := "#ececf0", "#dcdce2"
	if dark {
		bg = "#18181b"
		c1, c2 = "#27272a", "#1e1e22"
	}
	bgCSS := bg
	if checkered {
		bgCSS = fmt.Sprintf("repeating-conic-gradient(%s 0%% 25%%, %s 0%% 50%%) 50%% / 24px 24px", c1, c2)
	}
	return fmt.Sprintf(`<!doctype html><html><head><meta charset="utf-8"><style>
html,body{margin:0;padding:0;width:100%%;height:100%%;display:flex;align-items:center;justify-content:center;background:%s;overflow:auto;}
svg{max-width:94vw;max-height:94vh;width:auto;height:auto;}
</style></head><body>%s</body></html>`, bgCSS, svg)
}
