// Package app implements NSGook's application controller, multi-window preview lifecycle,
// 9-zone QuickLook window positioning, and Settings surface.
package app

import (
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/desktop"
)

const (
	MinWindowWidth  core.Points = 360
	MinWindowHeight core.Points = 240
	DefaultMaxRatio             = 0.85
)

// PickDisplay selects the display containing current's center, or Main, or the first display.
func PickDisplay(displays []core.DisplayInfo, current desktop.WindowFrame) core.DisplayInfo {
	if len(displays) == 0 {
		return core.DisplayInfo{
			ID:           "fallback",
			Frame:        core.Bounds{Size: core.Size{Width: 1920, Height: 1080}},
			VisibleFrame: core.Bounds{Size: core.Size{Width: 1920, Height: 1040}},
			Scale:        1,
			Main:         true,
		}
	}
	if current.Width > 0 && current.Height > 0 {
		center := core.Point{
			X: current.X + current.Width/2,
			Y: current.Y + current.Height/2,
		}
		for _, d := range displays {
			if d.Frame.Contains(center) {
				return d
			}
		}
	}
	for _, d := range displays {
		if d.Main {
			return d
		}
	}
	return displays[0]
}

// clampPreferredSize clamps preferred to [min, 0.85 * visibleFrame] respecting aspect ratio where shrunk.
func clampPreferredSize(preferred core.Size, vis core.Bounds) (core.Points, core.Points) {
	maxW := max(MinWindowWidth, vis.Size.Width*DefaultMaxRatio)
	maxH := max(MinWindowHeight, vis.Size.Height*DefaultMaxRatio)
	w := preferred.Width
	h := preferred.Height
	if w <= 0 {
		w = 800
	}
	if h <= 0 {
		h = 600
	}
	if w > maxW || h > maxH {
		r := min(maxW/w, maxH/h)
		w *= r
		h *= r
	}
	w = max(MinWindowWidth, min(maxW, w))
	h = max(MinWindowHeight, min(maxH, h))
	return w, h
}

// ResizeAndCentreNewWindow computes a centered initial WindowFrame within the main/active display's VisibleFrame.
func ResizeAndCentreNewWindow(preferred core.Size, displays []core.DisplayInfo) desktop.WindowFrame {
	d := PickDisplay(displays, desktop.WindowFrame{})
	vis := usableBounds(d)
	w, h := clampPreferredSize(preferred, vis)
	frame := desktop.WindowFrame{
		X:      vis.Origin.X + (vis.Size.Width-w)/2,
		Y:      vis.Origin.Y + (vis.Size.Height-h)/2,
		Move:   true,
		Width:  w,
		Height: h,
	}
	return frame.OnScreen(visibleScreenList(displays))
}

// ResizeAndCentreExistingWindow implements QuickLook's 9-zone resize heuristic:
// depending on which 3x3 region of the screen the window center occupies, the nearest edges/corners stay anchored
// while adapting size to preferred, keeping the window on screen.
func ResizeAndCentreExistingWindow(current desktop.WindowFrame, preferred core.Size, displays []core.DisplayInfo) desktop.WindowFrame {
	if current.Width <= 0 || current.Height <= 0 {
		return ResizeAndCentreNewWindow(preferred, displays)
	}
	d := PickDisplay(displays, current)
	vis := usableBounds(d)
	newW, newH := clampPreferredSize(preferred, vis)

	cx := current.X + current.Width/2
	cy := current.Y + current.Height/2
	relX := (cx - vis.Origin.X) / max(1, vis.Size.Width)
	relY := (cy - vis.Origin.Y) / max(1, vis.Size.Height)

	var newX, newY core.Points
	switch {
	case relX < 1.0/3.0:
		// Left third: anchor left edge.
		newX = current.X
	case relX > 2.0/3.0:
		// Right third: anchor right edge.
		newX = current.X + current.Width - newW
	default:
		// Horizontal middle third: preserve center.
		newX = cx - newW/2
	}

	switch {
	case relY < 1.0/3.0:
		// Top third: anchor top edge.
		newY = current.Y
	case relY > 2.0/3.0:
		// Bottom third: anchor bottom edge.
		newY = current.Y + current.Height - newH
	default:
		// Vertical middle third: preserve center.
		newY = cy - newH/2
	}

	// Clamp cleanly inside vis while keeping reachable titlebar.
	if newX < vis.Origin.X {
		newX = vis.Origin.X
	}
	if newX+newW > vis.Origin.X+vis.Size.Width {
		newX = vis.Origin.X + vis.Size.Width - newW
	}
	if newY < vis.Origin.Y {
		newY = vis.Origin.Y
	}
	if newY+newH > vis.Origin.Y+vis.Size.Height {
		newY = vis.Origin.Y + vis.Size.Height - newH
	}

	frame := desktop.WindowFrame{
		X:      newX,
		Y:      newY,
		Move:   true,
		Width:  newW,
		Height: newH,
	}
	return frame.OnScreen(visibleScreenList(displays))
}

func usableBounds(d core.DisplayInfo) core.Bounds {
	if d.VisibleFrame.Size.Width > 0 && d.VisibleFrame.Size.Height > 0 {
		return d.VisibleFrame
	}
	return d.Frame
}

func visibleScreenList(displays []core.DisplayInfo) []core.Bounds {
	if len(displays) == 0 {
		return nil
	}
	out := make([]core.Bounds, len(displays))
	for i, d := range displays {
		out[i] = usableBounds(d)
	}
	return out
}
