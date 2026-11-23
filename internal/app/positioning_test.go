package app

import (
	"math"
	"testing"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/desktop"
)

func TestResizeNewAndNineZones(t *testing.T) {
	displays := []core.DisplayInfo{
		{
			ID:           "main",
			Main:         true,
			Frame:        core.Bounds{Size: core.Size{Width: 1800, Height: 1200}},
			VisibleFrame: core.Bounds{Size: core.Size{Width: 1800, Height: 1140}},
		},
	}

	// New centered window
	f := ResizeAndCentreNewWindow(core.Size{Width: 800, Height: 600}, displays)
	if !f.Move || f.Width != 800 || f.Height != 600 {
		t.Fatalf("unexpected new size: %+v", f)
	}
	if math.Abs(float64(f.X-500)) > 1 || math.Abs(float64(f.Y-270)) > 1 {
		t.Fatalf("expected centered at (500, 270), got %+v", f)
	}

	// Huge preferred size clamps inside 0.85 * VisibleFrame.
	huge := ResizeAndCentreNewWindow(core.Size{Width: 4000, Height: 2000}, displays)
	if huge.Width > 1800*0.85+1 || huge.Height > 1140*0.85+1 {
		t.Fatalf("expected clamped frame, got %+v", huge)
	}

	// Zone tests (Top-Left, Center, Bottom-Right)
	tl := ResizeAndCentreExistingWindow(desktop.WindowFrame{X: 40, Y: 50, Width: 360, Height: 260}, core.Size{Width: 600, Height: 400}, displays)
	if math.Abs(float64(tl.X-40)) > 1 || math.Abs(float64(tl.Y-50)) > 1 {
		t.Fatalf("top-left zone should preserve top-left corner: %+v", tl)
	}

	centerCur := desktop.WindowFrame{X: 700, Y: 420, Width: 400, Height: 300}
	mid := ResizeAndCentreExistingWindow(centerCur, core.Size{Width: 600, Height: 400}, displays)
	if math.Abs(float64((mid.X+mid.Width/2)-900)) > 1 || math.Abs(float64((mid.Y+mid.Height/2)-570)) > 1 {
		t.Fatalf("center zone should preserve center: %+v", mid)
	}

	brCur := desktop.WindowFrame{X: 1300, Y: 800, Width: 400, Height: 300}
	br := ResizeAndCentreExistingWindow(brCur, core.Size{Width: 600, Height: 400}, displays)
	if math.Abs(float64((br.X+br.Width)-1700)) > 1 || math.Abs(float64((br.Y+br.Height)-1100)) > 1 {
		t.Fatalf("bottom-right zone should preserve bottom-right corner: %+v", br)
	}
}
