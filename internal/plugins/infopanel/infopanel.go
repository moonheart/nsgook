// Package infopanel implements NSGook's fallback file, folder, and drive info viewer.
package infopanel

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"image"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"sync/atomic"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/icons/lucide"
	"github.com/nsgo-dev/nsgo/ui"

	"nsgook/internal/plugins/pluginui"
	"nsgook/internal/viewer"
	"nsgook/locales"
)

// Plugin is the default fallback viewer showing shell icon, metadata, drive space, folder counts, and file hashes.
type Plugin struct{}

func New() viewer.Plugin { return Plugin{} }

func (Plugin) ID() string   { return "infopanel" }
func (Plugin) Name() string { return "InfoPanel" }
func (Plugin) Priority() int {
	return 0
}
func (Plugin) CanHandle(string) bool { return true }

func (Plugin) Prepare(path string, ctx *viewer.Context) {
	ctx.PreferredSize = core.Size{Width: 520, Height: 290}
	ctx.CanResize.Set(false)
	ctx.FullWindowDragging.Set(true)
	ctx.Title.Set(viewer.DisplayName(path))
}

func (Plugin) View(cx *core.Scope, path string, ctx *viewer.Context) core.Node {
	colors := ui.ColorsOf(cx)
	tr := locales.For(cx)

	thumb := core.NewSignal[*image.RGBA](cx, nil)
	sizeText := core.NewSignal(cx, "")
	countsText := core.NewSignal(cx, "")
	modText := core.NewSignal(cx, "")
	modeText := core.NewSignal(cx, "")
	drivePercent := core.NewSignal(cx, float32(-1))
	hashState := core.NewSignal(cx, "") // "", "computing", or hex string

	var stopped atomic.Bool
	cx.Cleanup(func() {
		stopped.Store(true)
		thumb.Set(nil)
	})
	ctx.TrackMedia(cx)
	cx.Effect(func() {
		if ctx.IsReleasing() {
			stopped.Store(true)
		}
	})

	isDrive := isDriveRoot(path)
	isDir := ctx.IsDir || isDrive

	// Load shell thumbnail asynchronously.
	if ctx.Sys != nil {
		go func() {
			img, err := ctx.Sys.GetShellThumbnail(path, 128)
			if err != nil || img == nil || stopped.Load() {
				return
			}
			cx.Post(func() {
				if !stopped.Load() {
					thumb.Set(img)
				}
			})
		}()
	}

	// Load file/folder/drive stats asynchronously.
	go func() {
		info, err := os.Stat(path)
		if err == nil && !stopped.Load() {
			modStr := info.ModTime().Format("2006-01-02 15:04:05")
			modeStr := info.Mode().String()
			cx.Post(func() {
				if stopped.Load() {
					return
				}
				modText.Set(modStr)
				modeText.Set(modeStr)
			})
		}

		if isDrive && ctx.Sys != nil {
			if total, free, ok := ctx.Sys.GetDriveSpace(path); ok && total > 0 {
				usedPct := float32(total-free) / float32(total) * 100
				cx.Post(func() {
					if stopped.Load() {
						return
					}
					drivePercent.Set(usedPct)
					sizeText.Set(tr.InfoDriveSpace(viewer.FormatBytes(free), viewer.FormatBytes(total)))
				})
				return
			}
		}

		if info != nil && info.IsDir() {
			cx.Post(func() {
				if !stopped.Load() {
					countsText.Set(tr.InfoCounting())
				}
			})
			dirs, files, totalBytes := countFolder(path, &stopped)
			if stopped.Load() {
				return
			}
			cx.Post(func() {
				if stopped.Load() {
					return
				}
				sizeText.Set(viewer.FormatBytes(totalBytes))
				countsText.Set(tr.InfoFolderCounts(strconv.Itoa(dirs), strconv.Itoa(files)))
			})
			return
		}

		if info != nil {
			sz := uint64(max(0, info.Size()))
			cx.Post(func() {
				if !stopped.Load() {
					sizeText.Set(fmt.Sprintf("%s (%d B)", viewer.FormatBytes(sz), sz))
				}
			})
		}
	}()

	computeHash := func() {
		if isDir || hashState.GetUntracked() == "computing" {
			return
		}
		hashState.Set("computing")
		go func() {
			f, err := os.Open(path)
			if err != nil {
				cx.Post(func() { hashState.Set("") })
				return
			}
			defer f.Close()
			h := sha256.New()
			buf := make([]byte, 64*1024)
			for {
				if stopped.Load() {
					return
				}
				n, readErr := f.Read(buf)
				if n > 0 {
					_, _ = h.Write(buf[:n])
				}
				if readErr == io.EOF {
					break
				}
				if readErr != nil {
					cx.Post(func() { hashState.Set("") })
					return
				}
			}
			sum := hex.EncodeToString(h.Sum(nil))
			cx.Post(func() {
				if !stopped.Load() {
					hashState.Set(sum)
				}
			})
		}()
	}

	kindLabel := func() string {
		if isDrive {
			return locales.For(cx).InfoDrive()
		}
		if isDir {
			return locales.For(cx).InfoFolder()
		}
		return locales.For(cx).InfoFile()
	}

	fallbackIcon := func() *core.SVG {
		if isDrive {
			return lucide.HardDrive()
		}
		if isDir {
			return lucide.Folder()
		}
		return lucide.File()
	}

	iconBox := core.View{
		Style: core.Style{
			Width:          core.Pt(112),
			Height:         core.Pt(112),
			AlignItems:     core.AlignCenter,
			JustifyContent: core.JustifyCenter,
			CornerRadius:   ui.RadiusCard,
			BorderWidth:    1,
			FlexShrink:     0,
		},
		StyleFunc: func() core.Style {
			return core.Style{
				Background:  ui.Alpha(colors.Inset(), 0.75),
				BorderColor: colors.Line(),
			}
		},
		Children: []core.Node{
			core.Show{
				When: func() bool { return thumb.Get() != nil },
				Then: func(*core.Scope) core.Node {
					return core.Image{
						SourceFunc: func() core.ImageSource {
							if ctx.IsReleasing() {
								return core.ImageSource{}
							}
							return core.ImageSource{Pixels: thumb.Get(), Version: 1}
						},
						ResizeMode: core.Contain,
						Style:      core.Style{Width: core.Pt(92), Height: core.Pt(92)},
					}
				},
				Else: func(*core.Scope) core.Node {
					return ui.IconOf(fallbackIcon, 56, colors.Accent)
				},
			},
		},
	}

	details := core.View{
		Style: core.Style{
			FlexGrow:   1,
			FlexShrink: 1,
			MinWidth:   core.Pt(0),
			Gap:        6,
		},
		Children: []core.Node{
			core.View{
				Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 8},
				Children: []core.Node{
					core.Text{
						Content:    viewer.DisplayName(path),
						MaxLines:   2,
						Selectable: core.SelectableOn,
						Style:      core.Style{FontSize: 17, FontWeight: ui.Semibold, FlexShrink: 1},
						StyleFunc:  ui.Colored(colors.Text),
					},
					ui.Badge{TextFunc: kindLabel, Variant: ui.BadgeInfo},
				},
			},
			core.Text{
				Content:    path,
				MaxLines:   2,
				Selectable: core.SelectableOn,
				Style:      core.Style{FontSize: 11},
				StyleFunc:  ui.Colored(colors.Muted),
			},
			core.Show{
				When: func() bool { return sizeText.Get() != "" },
				Then: func(*core.Scope) core.Node {
					return core.Text{
						ContentFunc: func() string { return locales.For(cx).InfoSize(sizeText.Get()) },
						Selectable:  core.SelectableOn,
						Style:       core.Style{FontSize: 12},
						StyleFunc:   ui.Colored(colors.Text),
					}
				},
			},
			core.Show{
				When: func() bool { return drivePercent.Get() >= 0 },
				Then: func(*core.Scope) core.Node {
					return ui.Progress{
						Value: drivePercent.Get,
						Label: locales.For(cx).InfoDrive(),
						Style: core.Style{Margin: core.Symmetric(2, 0)},
					}
				},
			},
			core.Show{
				When: func() bool { return countsText.Get() != "" },
				Then: func(*core.Scope) core.Node {
					return core.Text{
						ContentFunc: countsText.Get,
						Style:       core.Style{FontSize: 12},
						StyleFunc:   ui.Colored(colors.Muted),
					}
				},
			},
			core.Show{
				When: func() bool { return modText.Get() != "" },
				Then: func(*core.Scope) core.Node {
					return core.Text{
						ContentFunc: func() string { return locales.For(cx).InfoLastModified(modText.Get()) },
						Style:       core.Style{FontSize: 12},
						StyleFunc:   ui.Colored(colors.Muted),
					}
				},
			},
			core.Show{
				When: func() bool { return modeText.Get() != "" },
				Then: func(*core.Scope) core.Node {
					return core.Text{
						ContentFunc: func() string { return locales.For(cx).InfoPermissions() + ": " + modeText.Get() },
						Style:       core.Style{FontSize: 11, FontFamily: ui.Mono},
						StyleFunc:   ui.Colored(colors.Faint),
					}
				},
			},
			core.Show{
				When: func() bool { return !isDir },
				Then: func(cx *core.Scope) core.Node {
					return core.View{
						Desktop: core.DesktopViewProps{WindowDrag: core.WindowDragOff},
						Style:   core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 8, Margin: core.Edges{Top: 6}},
						Children: []core.Node{
							core.Show{
								When: func() bool { return hashState.Get() == "" },
								Then: func(cx *core.Scope) core.Node {
									return ui.Button{
										Text:    locales.For(cx).InfoComputeHash(),
										Icon:    lucide.FingerprintPattern,
										Variant: ui.ButtonSecondary,
										Size:    ui.SizeSmall,
										OnPress: computeHash,
									}
								},
							},
							core.Show{
								When: func() bool { return hashState.Get() == "computing" },
								Then: func(cx *core.Scope) core.Node {
									return core.View{
										Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 6},
										Children: []core.Node{
											ui.Spinner{Size: 14},
											ui.Text(locales.For(cx).InfoComputingHash(), 12, ui.Regular, colors.Muted),
										},
									}
								},
							},
							core.Show{
								When: func() bool {
									s := hashState.Get()
									return s != "" && s != "computing"
								},
								Then: func(cx *core.Scope) core.Node {
									return core.View{
										Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 6, FlexGrow: 1, FlexShrink: 1, MinWidth: core.Pt(0)},
										Children: []core.Node{
											core.Text{
												ContentFunc: func() string { return "SHA-256: " + hashState.Get() },
												MaxLines:    1,
												Selectable:  core.SelectableOn,
												Style:       core.Style{FontSize: 11, FontFamily: ui.Mono, FlexShrink: 1},
												StyleFunc:   ui.Colored(colors.Text),
											},
											pluginui.IconButton{
												Icon:     lucide.Copy,
												Label:    locales.For(cx).InfoCopyHash(),
												Size:     24,
												IconSize: 13,
												OnPress: func() {
													cx.SetClipboard(hashState.GetUntracked())
													if ctx.Toast != nil {
														ctx.Toast(ui.ToastSuccess, locales.For(cx).InfoHashCopied())
													}
												},
											},
										},
									}
								},
							},
						},
					}
				},
			},
		},
	}

	return core.View{
		Style: core.Style{
			FlexGrow:   1,
			FlexShrink: 1,
			Direction:  core.Row,
			AlignItems: core.AlignCenter,
			Padding:    core.All(22),
			Gap:        20,
			MinWidth:   core.Pt(0),
			MinHeight:  core.Pt(0),
		},
		Children: []core.Node{iconBox, details},
	}
}

func isDriveRoot(path string) bool {
	if len(path) >= 2 && path[1] == ':' {
		clean := filepath.Clean(path)
		return len(clean) <= 3 && (len(clean) == 2 || clean[2] == '\\' || clean[2] == '/')
	}
	return path == "/"
}

func countFolder(root string, stopped *atomic.Bool) (dirs, files int, total uint64) {
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if stopped.Load() {
			return fs.SkipAll
		}
		if err != nil {
			return nil
		}
		if p == root {
			return nil
		}
		if d.IsDir() {
			dirs++
			return nil
		}
		files++
		if info, infoErr := d.Info(); infoErr == nil {
			total += uint64(max(0, info.Size()))
		}
		return nil
	})
	return dirs, files, total
}
