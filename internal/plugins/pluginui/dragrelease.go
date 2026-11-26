package pluginui

import "time"

// dragReleaseWindow is how long a drag's release may still arrive as a
// click after the drag ends. The platform reports both in the same
// message, so this only covers that instant.
const dragReleaseWindow = 150 * time.Millisecond

// DragGuard keeps the release that ends a slider drag from pressing a
// control it lands on. NSGo's Windows backend reports the release as a
// press on the view under the pointer rather than the view the press
// began on, so dragging a volume slider over a button and letting go
// would click that button.
//
// A guard is used on one thread, by the controls of one preview.
type DragGuard struct {
	now   func() time.Time
	until time.Time
}

// NewDragGuard makes a guard for the controls around a slider.
func NewDragGuard() *DragGuard {
	return &DragGuard{now: time.Now}
}

// DragEnded notes that a slider drag just ended, so the click its release
// makes is not the controls'.
func (g *DragGuard) DragEnded() {
	g.until = g.now().Add(dragReleaseWindow)
}

// Blocked reports whether a click now is the release of a drag that began
// elsewhere.
func (g *DragGuard) Blocked() bool {
	return g.now().Before(g.until)
}
