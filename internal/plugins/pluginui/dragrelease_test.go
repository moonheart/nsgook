package pluginui

import (
	"testing"
	"time"
)

func TestDragGuardBlocksOnlyTheDragRelease(t *testing.T) {
	now := time.Unix(0, 0)
	g := &DragGuard{now: func() time.Time { return now }}
	if g.Blocked() {
		t.Fatal("expected clicks to pass before any drag")
	}

	g.DragEnded()
	if !g.Blocked() {
		t.Fatal("expected the release that ends a drag to be blocked")
	}

	now = now.Add(dragReleaseWindow)
	if g.Blocked() {
		t.Fatal("expected a click after the release to pass")
	}
}
