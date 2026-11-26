package pluginui

import (
	"sync"
	"time"

	"github.com/nsgo-dev/nsgo/core"
)

// AutoHide hides a floating control overlay by default and shows it for
// Delay after the pointer moves inside the preview, then hides it again when
// the pointer rests. The controls themselves keep it shown while hovered, so
// a slider is not taken away under the pointer.
type AutoHide struct {
	Visible *core.Signal[bool]

	cx    *core.Scope
	delay time.Duration

	mu      sync.Mutex
	timer   func() bool
	hovered bool
	stopped bool
}

// NewAutoHide makes controls hidden until the pointer moves, hiding them
// again after delay without movement. A non-positive delay uses 2.5 seconds.
func NewAutoHide(cx *core.Scope, delay time.Duration) *AutoHide {
	if delay <= 0 {
		delay = 2500 * time.Millisecond
	}
	h := &AutoHide{
		Visible: core.NewSignal(cx, false),
		cx:      cx,
		delay:   delay,
	}
	cx.Cleanup(h.Stop)
	return h
}

// Show shows the controls and restarts the hide timer.
func (h *AutoHide) Show() {
	h.Visible.Set(true)
	h.schedule()
}

// Hide hides the controls and stops the timer.
func (h *AutoHide) Hide() {
	h.stopTimer()
	h.Visible.Set(false)
}

// Hover keeps the controls up while the pointer is over them. When it leaves,
// they hide after the usual delay unless the pointer moves in the content.
func (h *AutoHide) Hover(over bool) {
	h.mu.Lock()
	h.hovered = over
	h.mu.Unlock()
	if over {
		h.stopTimer()
		h.Visible.Set(true)
		return
	}
	h.schedule()
}

// Track shows the controls on pointer movement inside the preview and hides
// them when the pointer leaves. It reads inside and activity so the caller's
// effect reruns for both.
func (h *AutoHide) Track(cx *core.Scope, inside *core.Signal[bool], activity *core.Signal[uint64]) {
	shown := false
	cx.Effect(func() {
		if !inside.Get() {
			if shown {
				shown = false
				h.Hide()
			}
			return
		}
		shown = true
		h.Show()
		_ = activity.Get()
	})
}

// Wrap returns node as the overlay behind Visible.
func (h *AutoHide) Wrap(node core.Node) core.Node {
	return core.Show{When: h.Visible.Get, Then: func(*core.Scope) core.Node { return node }}
}

// Stop ends the timer when the view goes.
func (h *AutoHide) Stop() {
	h.mu.Lock()
	h.stopped = true
	h.stopTimerLocked()
	h.mu.Unlock()
}

func (h *AutoHide) schedule() {
	h.mu.Lock()
	if h.stopped || h.hovered {
		h.mu.Unlock()
		return
	}
	h.stopTimerLocked()
	h.timer = h.cx.AfterFunc(h.delay, func() {
		h.mu.Lock()
		if h.stopped || h.hovered {
			h.mu.Unlock()
			return
		}
		h.timer = nil
		h.mu.Unlock()
		h.Visible.Set(false)
	})
	h.mu.Unlock()
}

func (h *AutoHide) stopTimer() {
	h.mu.Lock()
	h.stopTimerLocked()
	h.mu.Unlock()
}

func (h *AutoHide) stopTimerLocked() {
	if h.timer != nil {
		h.timer()
		h.timer = nil
	}
}
