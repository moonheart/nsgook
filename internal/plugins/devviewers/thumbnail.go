package devviewers

import (
	"archive/zip"
	"bytes"
	"image"
	"image/png"
	"io"
	"os"
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

var thumbExts = map[string]bool{
	".ai": true, ".eps": true, ".indd": true, ".cdr": true,
	".clip": true, ".kra": true, ".ora": true, ".afphoto": true,
	".afdesign": true, ".blend": true, ".skp": true, ".3ds": true,
	".max": true, ".c4d": true, ".fbx": true, ".dwg": true, ".dxf": true,
}

// ThumbnailPlugin previews creative/CAD project files via embedded ZIP thumbnails or OS shell thumbnails.
type ThumbnailPlugin struct{}

func (ThumbnailPlugin) ID() string              { return "thumbnailviewer" }
func (ThumbnailPlugin) Name() string            { return "ThumbnailViewer" }
func (ThumbnailPlugin) Priority() int           { return 65 }
func (ThumbnailPlugin) CanHandle(p string) bool { return hasExt(p, thumbExts) }

func (ThumbnailPlugin) Prepare(path string, ctx *viewer.Context) {
	ctx.PreferredSize = core.Size{Width: 740, Height: 580}
	ctx.Title.Set(viewer.DisplayName(path))
}

func (ThumbnailPlugin) View(cx *core.Scope, path string, ctx *viewer.Context) core.Node {
	colors := ui.ColorsOf(cx)
	tr := locales.For(cx)

	pngSig := core.NewSignalFunc(cx, []byte(nil), func(_, _ []byte) bool { return false })
	sizeSig := core.NewSignal(cx, "")
	var stopped atomic.Bool
	cx.Cleanup(func() {
		stopped.Store(true)
		pngSig.Set(nil)
	})
	ctx.TrackMedia(cx)
	cx.Effect(func() {
		if ctx.IsReleasing() {
			stopped.Store(true)
		}
	})

	ctx.IsBusy.Set(true)
	go func() {
		if st, err := os.Stat(path); err == nil {
			szStr := viewer.FormatBytes(uint64(max(0, st.Size())))
			cx.Post(func() {
				if !stopped.Load() {
					sizeSig.Set(szStr)
				}
			})
		}

		var outPNG []byte
		// 1. Check ZIP-packaged creative formats (.kra, .ora, etc.) for embedded PNG thumbnails.
		outPNG = extractZipThumbnail(path)
		// 2. Fallback to OS shell thumbnail provider (e.g. IShellItemImageFactory on Windows).
		if len(outPNG) == 0 && ctx.Sys != nil {
			if rgba, err := ctx.Sys.GetShellThumbnail(path, 512); err == nil && rgba != nil {
				outPNG = encodeRGBAPNG(rgba)
			}
		}

		if stopped.Load() {
			return
		}
		cx.Post(func() {
			if stopped.Load() {
				return
			}
			ctx.IsBusy.Set(false)
			pngSig.Set(outPNG)
		})
	}()

	extLabel := strings.TrimPrefix(strings.ToUpper(filepath.Ext(path)), ".")
	return pluginui.Fill(
		core.Dynamic(func(cx *core.Scope) core.Node {
			imgBytes := pngSig.Get()
			if len(imgBytes) == 0 {
				return pluginui.EmptyState(cx, lucide.Image, viewer.DisplayName(path), tr.CommonUnsupported())
			}
			return pluginui.Fill(
				core.View{
					Style: core.Style{
						FlexGrow:       1,
						FlexShrink:     1,
						Padding:        core.All(20),
						AlignItems:     core.AlignCenter,
						JustifyContent: core.JustifyCenter,
					},
					Children: []core.Node{
						core.Image{
							SourceFunc: func() core.ImageSource {
								if ctx.IsReleasing() {
									return core.ImageSource{}
								}
								return core.ImageSource{Data: pngSig.Get()}
							},
							ResizeMode: core.Contain,
							Style:      core.Style{Width: core.Pct(100), Height: core.Pct(100)},
						},
					},
				},
				pluginui.StatusBar(cx,
					ui.Text(extLabel, 11, ui.Medium, colors.Muted),
					ui.Spacer{},
					core.Text{
						ContentFunc: sizeSig.Get,
						Style:       core.Style{FontSize: 11},
						StyleFunc:   ui.Colored(colors.Muted),
					},
				),
			)
		}),
	)
}

func extractZipThumbnail(path string) []byte {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return nil
	}
	defer zr.Close()
	candidates := []string{"mergedimage.png", "thumbnails/thumbnail.png", "preview.png", "thumbnail.png"}
	for _, want := range candidates {
		for _, f := range zr.File {
			if strings.EqualFold(f.Name, want) {
				rc, err := f.Open()
				if err != nil {
					continue
				}
				b, _ := io.ReadAll(io.LimitReader(rc, 4<<20))
				_ = rc.Close()
				if len(b) > 0 {
					return b
				}
			}
		}
	}
	return nil
}

func encodeRGBAPNG(rgba *image.RGBA) []byte {
	var buf bytes.Buffer
	if err := png.Encode(&buf, rgba); err != nil {
		return nil
	}
	return buf.Bytes()
}
