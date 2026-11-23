// Package pluginui provides shared themed UI primitives for NSGook viewer plugins.
package pluginui

import (
	"github.com/nsgo-dev/nsgo/base"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/icons/lucide"
	"github.com/nsgo-dev/nsgo/ui"
)

// IconButton renders an accessible icon-only ghost button without attaching a hoisted Tooltip overlay,
// preventing retained Overlay nodes across dynamic plugin switches in NSGo.
type IconButton struct {
	Icon      func() *core.SVG
	Label     string
	LabelFunc func() string
	Size      core.Points
	IconSize  core.Points
	Tint      func() core.Color
	Selected  func() bool
	Disabled  func() bool
	OnPress   func()
	TestID    string
}

// Render implements core.Node.
func (b IconButton) Render(cx *core.Scope) core.Node {
	colors := ui.ColorsOf(cx)
	size := b.Size
	if size == 0 {
		size = 28
	}
	iconSize := b.IconSize
	if iconSize == 0 {
		iconSize = size * 0.6
	}
	tint := b.Tint
	if tint == nil {
		tint = colors.Text
	}
	return base.Button{
		OnPress: func(core.PressEvent) {
			if b.OnPress != nil {
				b.OnPress()
			}
		},
		Label:     b.Label,
		LabelFunc: b.LabelFunc,
		Selected:  b.Selected,
		Disabled:  b.Disabled,
		TestID:    b.TestID,
		Desktop:   core.DesktopViewProps{WindowDrag: core.WindowDragOff},
		Style: core.Style{
			Width:          core.Pt(size),
			Height:         core.Pt(size),
			AlignItems:     core.AlignCenter,
			JustifyContent: core.JustifyCenter,
			CornerRadius:   8,
			Disabled:       ui.Dimmed(),
		},
		StyleFunc: func() core.Style {
			return core.Style{
				Hover:        &core.Style{Background: ui.Alpha(colors.Text(), 0.08)},
				Active:       &core.Style{Background: ui.Alpha(colors.Text(), 0.14)},
				FocusVisible: ui.FocusRing(cx, -2),
			}
		},
		Children: []core.Node{ui.IconOf(b.Icon, iconSize, tint)},
	}
}

// KV represents a key-value metadata row.
type KV struct {
	Key   string
	Value string
}

// Fill returns a flex container filling its parent without overflowing scroll regions.
func Fill(children ...core.Node) core.Node {
	return core.View{
		Style: core.Style{
			FlexGrow:   1,
			FlexShrink: 1,
			MinHeight:  core.Pt(0),
			MinWidth:   core.Pt(0),
		},
		Children: children,
	}
}

// Toolbar builds a horizontal plugin toolbar strip above content.
func Toolbar(cx *core.Scope, children ...core.Node) core.Node {
	colors := ui.ColorsOf(cx)
	return core.View{
		Style: core.Style{
			Direction:  core.Row,
			AlignItems: core.AlignCenter,
			Gap:        6,
			Padding:    core.Symmetric(6, 10),
			Borders:    core.Edges{Bottom: 1},
			FlexShrink: 0,
		},
		StyleFunc: func() core.Style {
			return core.Style{
				Background:  ui.Alpha(colors.Surface(), 0.72),
				BorderColor: colors.Line(),
			}
		},
		Children: children,
	}
}

// StatusBar builds a compact bottom status bar strip.
func StatusBar(cx *core.Scope, children ...core.Node) core.Node {
	colors := ui.ColorsOf(cx)
	return core.View{
		Style: core.Style{
			Direction:  core.Row,
			AlignItems: core.AlignCenter,
			Gap:        8,
			Padding:    core.Symmetric(5, 12),
			Borders:    core.Edges{Top: 1},
			FlexShrink: 0,
		},
		StyleFunc: func() core.Style {
			return core.Style{
				Background:  ui.Alpha(colors.Surface(), 0.72),
				BorderColor: colors.Line(),
			}
		},
		Children: children,
	}
}

// FloatingPill builds a centered bottom floating capsule bar over media/canvas/image viewers.
func FloatingPill(cx *core.Scope, children ...core.Node) core.Node {
	colors := ui.ColorsOf(cx)
	fin := ui.FinOf(cx)
	return core.View{
		PointerEvents: core.PointerEventsBoxNone,
		Style: core.Style{
			Position:       core.Absolute,
			Left:           core.Pt(12),
			Right:          core.Pt(12),
			Bottom:         core.Pt(14),
			AlignItems:     core.AlignCenter,
			JustifyContent: core.JustifyCenter,
		},
		Children: []core.Node{
			core.View{
				Desktop: core.DesktopViewProps{WindowDrag: core.WindowDragOff},
				Style: core.Style{
					Direction:    core.Row,
					AlignItems:   core.AlignCenter,
					Gap:          4,
					Padding:      core.Symmetric(4, 10),
					CornerRadius: ui.RadiusFull,
					BorderWidth:  1,
				},
				StyleFunc: func() core.Style {
					return fin.Menu().Over(core.Style{
						Background:  ui.Alpha(colors.Surface(), 0.90),
						BorderColor: colors.Border(),
					})
				},
				Children: children,
			},
		},
	}
}

// SectionCard renders a titled metadata card containing key-value pairs.
func SectionCard(cx *core.Scope, title string, rows []KV, copyLabel string, onCopy func(string)) core.Node {
	colors := ui.ColorsOf(cx)
	var items []core.Node
	if title != "" {
		items = append(items, ui.Text(title, 13, ui.Semibold, colors.Text))
	}
	for i, row := range rows {
		r := row
		rowNode := core.View{
			Style: core.Style{
				Direction:  core.Row,
				AlignItems: core.AlignCenter,
				Gap:        10,
				Padding:    core.Symmetric(6, 0),
			},
			StyleFunc: func() core.Style {
				if i == 0 {
					return core.Style{}
				}
				return core.Style{Borders: core.Edges{Top: 1}, BorderColor: colors.Line()}
			},
			Children: []core.Node{
				core.Text{
					Content:   r.Key,
					Style:     core.Style{Width: core.Pt(130), FlexShrink: 0, FontSize: 12, FontWeight: ui.Medium},
					StyleFunc: ui.Colored(colors.Muted),
				},
				core.Text{
					Content:    r.Value,
					Selectable: core.SelectableOn,
					Style:      core.Style{FlexGrow: 1, FlexShrink: 1, FontSize: 12},
					StyleFunc:  ui.Colored(colors.Text),
				},
			},
		}
		if onCopy != nil && r.Value != "" {
			rowNode.Children = append(rowNode.Children, IconButton{
				Icon:     lucide.Copy,
				Label:    copyLabel,
				Size:     24,
				IconSize: 13,
				OnPress:  func() { onCopy(r.Value) },
			})
		}
		items = append(items, rowNode)
	}
	return core.View{
		Style: core.Style{
			Padding:      core.All(14),
			Gap:          6,
			CornerRadius: ui.RadiusCard,
			BorderWidth:  1,
		},
		StyleFunc: func() core.Style {
			return core.Style{
				Background:  ui.Alpha(colors.Surface(), 0.78),
				BorderColor: colors.Border(),
			}
		},
		Children: items,
	}
}

// EmptyState renders a centered icon and title/subtitle message.
func EmptyState(cx *core.Scope, icon func() *core.SVG, title, subtitle string) core.Node {
	colors := ui.ColorsOf(cx)
	var children []core.Node
	if icon != nil {
		children = append(children, ui.IconOf(icon, 44, colors.Faint))
	}
	if title != "" {
		children = append(children, core.Text{
			Content:   title,
			Style:     core.Style{FontSize: 15, FontWeight: ui.Semibold, TextAlign: core.TextAlignCenter},
			StyleFunc: ui.Colored(colors.Text),
		})
	}
	if subtitle != "" {
		children = append(children, core.Text{
			Content:   subtitle,
			Style:     core.Style{FontSize: 12, TextAlign: core.TextAlignCenter},
			StyleFunc: ui.Colored(colors.Muted),
		})
	}
	return core.View{
		Style: core.Style{
			FlexGrow:       1,
			FlexShrink:     1,
			AlignItems:     core.AlignCenter,
			JustifyContent: core.JustifyCenter,
			Padding:        core.All(24),
			Gap:            10,
		},
		Children: children,
	}
}
