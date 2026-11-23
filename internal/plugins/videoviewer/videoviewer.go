package videoviewer

import (
	"fmt"
	"path/filepath"
	"sync/atomic"
	"time"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/icons/lucide"
	"github.com/nsgo-dev/nsgo/ui"

	"nsgook/internal/plugins/pluginui"
	"nsgook/internal/viewer"
	"nsgook/locales"
)

// Plugin previews video and audio files via Media Foundation / AVFoundation / GStreamer,
// enriched with pure-Go tag/cover-art extraction and synced .lrc lyrics.
type Plugin struct{}

func New() viewer.Plugin { return Plugin{} }

func (Plugin) ID() string   { return "videoviewer" }
func (Plugin) Name() string { return "VideoViewer" }
func (Plugin) Priority() int {
	return 70
}

func (Plugin) CanHandle(path string) bool {
	ext := filepath.Ext(path)
	return IsVideoExt(ext) || IsAudioExt(ext)
}

func (Plugin) Prepare(path string, ctx *viewer.Context) {
	isAudio := IsAudioExt(filepath.Ext(path))
	if isAudio {
		ctx.PreferredSize = core.Size{Width: 600, Height: 480}
		ctx.TitlebarOverlap.Set(false)
		ctx.TitlebarAutoHide.Set(false)
	} else {
		ctx.PreferredSize = core.Size{Width: 900, Height: 560}
		ctx.TitlebarOverlap.Set(true)
		ctx.TitlebarAutoHide.Set(true)
	}
	ctx.Title.Set(viewer.DisplayName(path))
}

func (Plugin) View(cx *core.Scope, path string, ctx *viewer.Context) core.Node {
	colors := ui.ColorsOf(cx)
	tr := locales.For(cx)
	isAudio := IsAudioExt(filepath.Ext(path))

	defLoop := false
	defVol := float32(1)
	if ctx.Config != nil {
		c := ctx.Config.Get()
		defLoop = c.VideoLoop
		if c.VideoVolume >= 0 && c.VideoVolume <= 100 {
			defVol = float32(c.VideoVolume) / 100.0
		}
	}

	paused := core.NewSignal(cx, false)
	muted := core.NewSignal(cx, defVol <= 0.001)
	volume := core.NewSignal(cx, max(defVol, 0.05))
	rate := core.NewSignal(cx, float32(1.0))
	loop := core.NewSignal(cx, defLoop)
	posMs := core.NewSignal(cx, int64(0))
	durMs := core.NewSignal(cx, int64(0))
	mediaErr := core.NewSignal(cx, "")
	meta := core.NewSignalFunc(cx, TrackMetadata{}, func(_, _ TrackMetadata) bool { return false })
	lyrics := core.NewSignalFunc(cx, []LyricLine(nil), func(_, _ []LyricLine) bool { return false })

	var stopped atomic.Bool
	cx.Cleanup(func() {
		stopped.Store(true)
		meta.Set(TrackMetadata{})
		lyrics.Set(nil)
	})
	ctx.TrackMedia(cx)
	cx.Effect(func() {
		if ctx.IsReleasing() {
			stopped.Store(true)
		}
	})

	go func() {
		m := InspectMedia(path)
		var lrc []LyricLine
		if isAudio {
			lrc = LoadSidecarLRC(path)
		}
		if stopped.Load() {
			return
		}
		cx.Post(func() {
			if stopped.Load() {
				return
			}
			meta.Set(m)
			lyrics.Set(lrc)
			if m.Duration > 0 && durMs.GetUntracked() <= 0 {
				durMs.Set(m.Duration.Milliseconds())
			}
			if m.Title != "" && m.Artist != "" {
				ctx.Title.Set(m.Artist + " - " + m.Title)
			} else if m.Title != "" {
				ctx.Title.Set(m.Title)
			}
		})
	}()

	videoRef := &core.Ref{}
	speeds := []float32{0.5, 1.0, 1.25, 1.5, 2.0}
	cycleSpeed := func() {
		cur := rate.GetUntracked()
		next := speeds[0]
		for i, sp := range speeds {
			if sp > cur+0.01 {
				next = speeds[i]
				break
			}
		}
		rate.Set(next)
	}
	togglePlay := func() {
		if posMs.GetUntracked() >= durMs.GetUntracked() && durMs.GetUntracked() > 0 && paused.GetUntracked() {
			videoRef.Seek(0)
			posMs.Set(0)
		}
		paused.Set(!paused.GetUntracked())
	}
	toggleMute := func() {
		muted.Set(!muted.GetUntracked())
	}
	toggleLoop := func() {
		loop.Set(!loop.GetUntracked())
	}

	ctx.MoreItems.Set([]viewer.MenuAction{
		{
			ID:       "media-play",
			Label:    tr.MediaPlay() + " / " + tr.MediaPause(),
			Icon:     lucide.Play,
			OnSelect: togglePlay,
		},
		{
			ID:       "media-mute",
			Label:    tr.MediaMute(),
			Icon:     lucide.VolumeX,
			Checked:  muted.Get,
			OnSelect: toggleMute,
		},
		{
			ID:       "media-loop",
			Label:    tr.MediaLoop(),
			Icon:     lucide.Repeat,
			Checked:  loop.Get,
			OnSelect: toggleLoop,
		},
	})

	playerNode := core.Video{
		Ref:              videoRef,
		Source:           path,
		PausedFunc:       paused.Get,
		MutedFunc:        func() bool { return muted.Get() || volume.Get() <= 0.001 },
		VolumeFunc:       func() float32 { return max(volume.Get(), 0.01) },
		RateFunc:         rate.Get,
		ResizeMode:       core.Contain,
		ProgressInterval: 200 * time.Millisecond,
		OnLoad: func(info core.VideoInfo) {
			if info.Duration > 0 {
				durMs.Set(info.Duration.Milliseconds())
			}
			if !isAudio && info.Size.Width > 0 && info.Size.Height > 0 {
				base := viewer.DisplayName(path)
				ctx.Title.Set(fmt.Sprintf("%s (%d×%d)", base, int(info.Size.Width), int(info.Size.Height)))
			}
		},
		OnProgress: func(pos, dur time.Duration) {
			posMs.Set(pos.Milliseconds())
			if dur > 0 {
				durMs.Set(dur.Milliseconds())
			}
		},
		OnPlay:  func() { paused.Set(false) },
		OnPause: func(core.VideoPauseReason) { paused.Set(true) },
		OnEnd: func() {
			if loop.GetUntracked() {
				videoRef.Seek(0)
				posMs.Set(0)
				paused.Set(false)
			} else {
				paused.Set(true)
			}
		},
		OnError: func(err error) {
			if err != nil {
				mediaErr.Set(err.Error())
			}
		},
		Style: func() core.Style {
			if isAudio {
				return core.Style{Width: core.Pt(1), Height: core.Pt(1), Opacity: core.Alpha(0)}
			}
			return core.Style{FlexGrow: 1, FlexShrink: 1, Width: core.Pct(100), Height: core.Pct(100)}
		}(),
	}

	controlsBar := core.View{
		Style: core.Style{
			Direction:    core.Row,
			AlignItems:   core.AlignCenter,
			Gap:          8,
			Padding:      core.Symmetric(8, 12),
			Borders:      core.All(1),
			CornerRadius: 12,
		},
		StyleFunc: func() core.Style {
			bg := ui.Alpha(colors.Surface(), 0.88)
			if cx.Dark() {
				bg = ui.Alpha(colors.Raised(), 0.90)
			}
			return core.Style{
				Background:  bg,
				BorderColor: colors.Line(),
			}
		},
		Children: []core.Node{
			pluginui.IconButton{
				Icon: func() *core.SVG {
					if paused.Get() {
						return lucide.Play()
					}
					return lucide.Pause()
				},
				Label:   locales.For(cx).MediaPlay(),
				Size:    28,
				OnPress: togglePlay,
			},
			core.Text{
				ContentFunc: func() string {
					return FormatClock(time.Duration(posMs.Get()) * time.Millisecond)
				},
				Style:     core.Style{FontSize: 11, FontFamily: ui.Mono, MinWidth: core.Pt(42)},
				StyleFunc: ui.Colored(colors.Muted),
			},
			core.View{
				Style: core.Style{FlexGrow: 1, FlexShrink: 1, MinWidth: core.Pt(80)},
				Children: []core.Node{
					ui.Slider{
						Min: 0,
						MaxFunc: func() float64 {
							if d := durMs.Get(); d > 0 {
								return float64(d)
							}
							return 1000
						},
						Value: func() float64 {
							return float64(posMs.Get())
						},
						OnChange: func(v float64) {
							d := time.Duration(v) * time.Millisecond
							posMs.Set(int64(v))
							videoRef.Seek(d)
						},
					},
				},
			},
			core.Text{
				ContentFunc: func() string {
					return FormatClock(time.Duration(durMs.Get()) * time.Millisecond)
				},
				Style:     core.Style{FontSize: 11, FontFamily: ui.Mono, MinWidth: core.Pt(42), TextAlign: core.TextAlignEnd},
				StyleFunc: ui.Colored(colors.Muted),
			},
			pluginui.IconButton{
				Icon: func() *core.SVG {
					if muted.Get() {
						return lucide.VolumeX()
					}
					return lucide.Volume2()
				},
				Label:   locales.For(cx).MediaMute(),
				Size:    26,
				OnPress: toggleMute,
			},
			core.View{
				Style: core.Style{Width: core.Pt(70)},
				Children: []core.Node{
					ui.Slider{
						Min:  0,
						Max:  100,
						Step: 5,
						Value: func() float64 {
							if muted.Get() {
								return 0
							}
							return float64(volume.Get() * 100)
						},
						OnChange: func(v float64) {
							if v <= 0.5 {
								muted.Set(true)
								volume.Set(0.05)
							} else {
								muted.Set(false)
								volume.Set(float32(v / 100.0))
							}
						},
					},
				},
			},
			ui.Press{
				Label:   locales.For(cx).MediaSpeed(),
				OnPress: cycleSpeed,
				Children: []core.Node{
					core.View{
						Style: core.Style{
							Height:         core.Pt(24),
							Padding:        core.Symmetric(0, 8),
							CornerRadius:   6,
							AlignItems:     core.AlignCenter,
							JustifyContent: core.JustifyCenter,
						},
						StyleFunc: func() core.Style {
							return core.Style{Hover: &core.Style{Background: colors.Hover()}}
						},
						Children: []core.Node{
							core.Text{
								ContentFunc: func() string { return fmt.Sprintf("%.2g×", rate.Get()) },
								Style:       core.Style{FontSize: 12, FontWeight: ui.Medium},
								StyleFunc:   ui.Colored(colors.Text),
							},
						},
					},
				},
			},
			pluginui.IconButton{
				Icon:     lucide.Repeat,
				Label:    locales.For(cx).MediaLoop(),
				Size:     26,
				Selected: loop.Get,
				OnPress:  toggleLoop,
			},
		},
	}

	if isAudio {
		audioBody := core.View{
			Style: core.Style{
				FlexGrow:   1,
				FlexShrink: 1,
				MinWidth:   core.Pt(0),
				MinHeight:  core.Pt(0),
				Padding:    core.All(16),
				Gap:        14,
			},
			Children: []core.Node{
				core.View{
					Style: core.Style{
						Direction:  core.Row,
						AlignItems: core.AlignCenter,
						Gap:        16,
					},
					Children: []core.Node{
						core.View{
							Style: core.Style{
								Width:          core.Pt(112),
								Height:         core.Pt(112),
								CornerRadius:   12,
								Borders:        core.All(1),
								AlignItems:     core.AlignCenter,
								JustifyContent: core.JustifyCenter,
								Overflow:       core.Hidden,
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
									When: func() bool { return len(meta.Get().CoverArt) > 0 },
									Then: func(*core.Scope) core.Node {
										return core.Image{
											SourceFunc: func() core.ImageSource {
												if ctx.IsReleasing() {
													return core.ImageSource{}
												}
												return core.ImageSource{Data: meta.Get().CoverArt}
											},
											ResizeMode: core.Contain,
											Style:      core.Style{Width: core.Pct(100), Height: core.Pct(100)},
										}
									},
									Else: func(*core.Scope) core.Node {
										return ui.IconOf(lucide.Music, 44, colors.Accent)
									},
								},
							},
						},
						core.View{
							Style: core.Style{FlexGrow: 1, FlexShrink: 1, MinWidth: core.Pt(0), Gap: 4},
							Children: []core.Node{
								core.Text{
									ContentFunc: func() string {
										if t := meta.Get().Title; t != "" {
											return t
										}
										return viewer.DisplayName(path)
									},
									MaxLines:  2,
									Style:     core.Style{FontSize: 18, FontWeight: ui.Bold},
									StyleFunc: ui.Colored(colors.Text),
								},
								core.Show{
									When: func() bool { return meta.Get().Artist != "" },
									Then: func(*core.Scope) core.Node {
										return core.Text{
											ContentFunc: func() string { return meta.Get().Artist },
											MaxLines:    1,
											Style:       core.Style{FontSize: 13},
											StyleFunc:   ui.Colored(colors.Muted),
										}
									},
								},
								core.Show{
									When: func() bool { return meta.Get().Album != "" },
									Then: func(*core.Scope) core.Node {
										return core.Text{
											ContentFunc: func() string { return meta.Get().Album },
											MaxLines:    1,
											Style:       core.Style{FontSize: 12},
											StyleFunc:   ui.Colored(colors.Muted),
										}
									},
								},
								core.View{
									Style: core.Style{Direction: core.Row, Wrap: core.Wrap, Gap: 6, Margin: core.Edges{Top: 4}},
									Children: []core.Node{
										ui.Badge{
											TextFunc: func() string { return meta.Get().Format },
											Variant:  ui.BadgeNeutral,
										},
										core.Show{
											When: func() bool { return meta.Get().SampleRate > 0 },
											Then: func(*core.Scope) core.Node {
												return ui.Badge{
													TextFunc: func() string {
														m := meta.Get()
														return fmt.Sprintf("%.1f kHz · %d ch", float64(m.SampleRate)/1000, m.Channels)
													},
													Variant: ui.BadgeNeutral,
												}
											},
										},
									},
								},
							},
						},
					},
				},
				core.View{
					Style: core.Style{
						FlexGrow:     1,
						FlexShrink:   1,
						MinHeight:    core.Pt(0),
						Borders:      core.All(1),
						CornerRadius: 10,
						Overflow:     core.Hidden,
					},
					StyleFunc: func() core.Style {
						return core.Style{
							Background:  ui.Alpha(colors.Inset(), 0.45),
							BorderColor: colors.Line(),
						}
					},
					Children: []core.Node{
						core.Show{
							When: func() bool { return len(lyrics.Get()) > 0 },
							Then: func(cx *core.Scope) core.Node {
								return core.Scroll{
									Style: core.Style{FlexGrow: 1, FlexShrink: 1, Width: core.Pct(100), Height: core.Pct(100)},
									Children: []core.Node{
										core.View{
											Style: core.Style{Padding: core.All(12), Gap: 6},
											Children: []core.Node{
												core.Each(lyrics.Get, func(l LyricLine) string {
													return fmt.Sprintf("%d:%s", l.Time, l.Text)
												}, func(_ *core.Scope, item func() LyricLine, idx func() int) core.Node {
													return core.Text{
														ContentFunc: func() string {
															txt := item().Text
															if txt == "" {
																return "♪"
															}
															return txt
														},
														Style: core.Style{FontSize: 13, LineHeight: 20},
														StyleFunc: func() core.Style {
															active := ActiveLyricIndex(lyrics.Get(), time.Duration(posMs.Get())*time.Millisecond)
															if active == idx() {
																return core.Style{Color: colors.Accent(), FontWeight: ui.Bold}
															}
															return core.Style{Color: colors.Muted()}
														},
													}
												}),
											},
										},
									},
								}
							},
							Else: func(cx *core.Scope) core.Node {
								return pluginui.EmptyState(cx, lucide.Music2, viewer.DisplayName(path), locales.For(cx).MediaNoLyrics())
							},
						},
					},
				},
				core.Show{
					When: func() bool { return mediaErr.Get() != "" },
					Then: func(*core.Scope) core.Node {
						return core.Text{
							ContentFunc: mediaErr.Get,
							Style:       core.Style{FontSize: 12},
							StyleFunc:   ui.Colored(colors.Danger),
						}
					},
				},
				playerNode,
				controlsBar,
			},
		}
		return audioBody
	}

	// Video layout with floating glass controls bar at the bottom.
	return core.View{
		Style: core.Style{
			FlexGrow:   1,
			FlexShrink: 1,
			MinWidth:   core.Pt(0),
			MinHeight:  core.Pt(0),
		},
		StyleFunc: func() core.Style {
			return core.Style{Background: core.RGB(0x0D0F14)}
		},
		Children: []core.Node{
			core.Show{
				When: func() bool { return mediaErr.Get() != "" },
				Then: func(cx *core.Scope) core.Node {
					return pluginui.EmptyState(cx, lucide.VideoOff, locales.For(cx).CommonError(), mediaErr.Get())
				},
				Else: func(*core.Scope) core.Node { return playerNode },
			},
			core.View{
				Style: core.Style{
					Position: core.Absolute,
					Left:     core.Pt(14),
					Right:    core.Pt(14),
					Bottom:   core.Pt(14),
				},
				Children: []core.Node{controlsBar},
			},
		},
	}
}
