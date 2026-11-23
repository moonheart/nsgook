// Package sys provides OS integration: global keyboard/foreground hooks, file manager selection detection, shell actions, and file watching.
package sys

import (
	"sync"
	"time"
)

// Action represents a preview control command dispatched by global keystrokes.
type Action uint8

const (
	ActionToggle Action = iota
	ActionSwitch
	ActionClose
	ActionRunAndClose
	ActionReload
	ActionFullscreen
)

// Windows virtual-key codes handled by Dispatcher.
const (
	VKReturn uint32 = 0x0D
	VKEscape uint32 = 0x1B
	VKSpace  uint32 = 0x20
	VKLeft   uint32 = 0x25
	VKUp     uint32 = 0x26
	VKRight  uint32 = 0x27
	VKDown   uint32 = 0x28
	VKF5     uint32 = 0x74
	VKF11    uint32 = 0x7A
)

// Modifier bitmasks passed to Dispatcher.HandleKey.
const (
	ModShift   uint32 = 1 << 0
	ModControl uint32 = 1 << 1
	ModAlt     uint32 = 1 << 2
	ModWin     uint32 = 1 << 3
)

const (
	// HoldToPreviewDuration is how long Space must be held so releasing it auto-closes the preview.
	HoldToPreviewDuration = 750 * time.Millisecond
	// ValidKeyPressDelay suppresses valid trigger keys immediately following an invalid key (e.g., while typing).
	ValidKeyPressDelay = 1 * time.Second
)

// Dispatcher mirrors QuickLook's KeystrokeDispatcher state machine.
type Dispatcher struct {
	mu               sync.Mutex
	Now              func() time.Time
	IsValidTarget    func() bool
	AutoCloseHolding func() bool
	OnAction         func(Action)

	isPreviewRequest bool
	spaceIsDown      bool
	spaceHoldStart   time.Time
	lastInvalidAt    time.Time
}

// HandleKey processes a raw keyboard event and triggers OnAction when appropriate.
func (d *Dispatcher) HandleKey(vk uint32, down bool, mods uint32) {
	now := time.Now()
	if d.Now != nil {
		now = d.Now()
	}

	var actions []Action

	d.mu.Lock()
	if !isValidKey(vk) {
		d.lastInvalidAt = now
		d.mu.Unlock()
		return
	}

	if down && mods != 0 {
		d.mu.Unlock()
		return
	}

	if !d.lastInvalidAt.IsZero() && now.Sub(d.lastInvalidAt) < ValidKeyPressDelay {
		d.mu.Unlock()
		return
	}
	d.lastInvalidAt = time.Time{}

	if down && vk == VKSpace {
		if d.spaceIsDown {
			d.mu.Unlock()
			return
		}
		d.spaceHoldStart = now
	}

	if down {
		d.isPreviewRequest = d.IsValidTarget != nil && d.IsValidTarget()
	}

	if d.isPreviewRequest {
		if down || vk != VKSpace || (!d.spaceHoldStart.IsZero() && now.Sub(d.spaceHoldStart) >= HoldToPreviewDuration) {
			if act, ok := actionForKey(vk, down, d.AutoCloseHolding); ok {
				actions = append(actions, act)
			}
			if down && vk == VKSpace {
				d.spaceIsDown = true
			}
		}
	}

	if !down {
		d.isPreviewRequest = false
		if vk == VKSpace {
			d.spaceIsDown = false
			d.spaceHoldStart = time.Time{}
		}
	}
	onAction := d.OnAction
	d.mu.Unlock()

	if onAction != nil {
		for _, act := range actions {
			onAction(act)
		}
	}
}

// OnForegroundChanged clears the invalid-key suppression window after switching windows (e.g. Alt+Tab).
func (d *Dispatcher) OnForegroundChanged() {
	d.mu.Lock()
	d.lastInvalidAt = time.Time{}
	d.mu.Unlock()
}

func isValidKey(vk uint32) bool {
	switch vk {
	case VKUp, VKDown, VKLeft, VKRight, VKReturn, VKSpace, VKEscape, VKF5, VKF11:
		return true
	default:
		return false
	}
}

func actionForKey(vk uint32, down bool, autoClose func() bool) (Action, bool) {
	if down {
		switch vk {
		case VKReturn:
			return ActionRunAndClose, true
		case VKSpace:
			return ActionToggle, true
		case VKF5:
			return ActionReload, true
		case VKF11:
			return ActionFullscreen, true
		}
		return 0, false
	}
	switch vk {
	case VKUp, VKDown, VKLeft, VKRight:
		return ActionSwitch, true
	case VKEscape:
		return ActionClose, true
	case VKSpace:
		if autoClose == nil || autoClose() {
			return ActionToggle, true
		}
	}
	return 0, false
}
