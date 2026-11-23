package sys

import (
	"slices"
	"testing"
	"time"
)

func TestDispatcherSpaceTapAndHold(t *testing.T) {
	now := time.Unix(1000, 0)
	valid := true
	autoClose := true
	var got []Action

	d := &Dispatcher{
		Now:              func() time.Time { return now },
		IsValidTarget:    func() bool { return valid },
		AutoCloseHolding: func() bool { return autoClose },
		OnAction:         func(a Action) { got = append(got, a) },
	}

	// Quick tap Space: down toggles, auto-repeat ignored, quick up does not toggle again.
	d.HandleKey(VKSpace, true, 0)
	now = now.Add(100 * time.Millisecond)
	d.HandleKey(VKSpace, true, 0) // repeat
	now = now.Add(100 * time.Millisecond)
	d.HandleKey(VKSpace, false, 0)
	if !slices.Equal(got, []Action{ActionToggle}) {
		t.Fatalf("quick tap expected [Toggle], got %v", got)
	}

	// Long hold Space (>=750ms): down toggles, up toggles again.
	got = nil
	d.HandleKey(VKSpace, true, 0)
	now = now.Add(800 * time.Millisecond)
	d.HandleKey(VKSpace, false, 0)
	if !slices.Equal(got, []Action{ActionToggle, ActionToggle}) {
		t.Fatalf("long hold expected [Toggle, Toggle], got %v", got)
	}

	// Long hold with AutoCloseHolding=false: only down toggles.
	got = nil
	autoClose = false
	d.HandleKey(VKSpace, true, 0)
	now = now.Add(900 * time.Millisecond)
	d.HandleKey(VKSpace, false, 0)
	if !slices.Equal(got, []Action{ActionToggle}) {
		t.Fatalf("long hold with AutoCloseHolding=false expected [Toggle], got %v", got)
	}
}

func TestDispatcherInvalidKeySuppressionAndForegroundReset(t *testing.T) {
	now := time.Unix(2000, 0)
	var got []Action
	d := &Dispatcher{
		Now:           func() time.Time { return now },
		IsValidTarget: func() bool { return true },
		OnAction:      func(a Action) { got = append(got, a) },
	}

	// Typing 'A' (0x41) suppresses Space for 1 second.
	d.HandleKey('A', true, 0)
	now = now.Add(300 * time.Millisecond)
	d.HandleKey(VKSpace, true, 0)
	d.HandleKey(VKSpace, false, 0)
	if len(got) != 0 {
		t.Fatalf("expected Space to be suppressed within 1s of typing, got %v", got)
	}

	// Changing foreground window resets suppression immediately (Alt+Tab fix).
	d.HandleKey('A', true, 0)
	now = now.Add(50 * time.Millisecond)
	d.OnForegroundChanged()
	d.HandleKey(VKSpace, true, 0)
	d.HandleKey(VKSpace, false, 0)
	if !slices.Equal(got, []Action{ActionToggle}) {
		t.Fatalf("expected foreground change to reset suppression, got %v", got)
	}

	// After 1s expires naturally, Space works again.
	got = nil
	d.HandleKey('A', true, 0)
	now = now.Add(1100 * time.Millisecond)
	d.HandleKey(VKSpace, true, 0)
	d.HandleKey(VKSpace, false, 0)
	if !slices.Equal(got, []Action{ActionToggle}) {
		t.Fatalf("expected Space to work after 1s, got %v", got)
	}
}

func TestDispatcherOtherKeysAndModifiers(t *testing.T) {
	now := time.Unix(3000, 0)
	var got []Action
	d := &Dispatcher{
		Now:           func() time.Time { return now },
		IsValidTarget: func() bool { return true },
		OnAction:      func(a Action) { got = append(got, a) },
	}

	// Modified Space on key down is ignored.
	d.HandleKey(VKSpace, true, ModControl)
	d.HandleKey(VKSpace, false, ModControl)
	if len(got) != 0 {
		t.Fatalf("expected modified Space to be ignored, got %v", got)
	}

	d.HandleKey(VKReturn, true, 0)
	d.HandleKey(VKReturn, false, 0)
	d.HandleKey(VKF5, true, 0)
	d.HandleKey(VKF5, false, 0)
	d.HandleKey(VKF11, true, 0)
	d.HandleKey(VKF11, false, 0)
	d.HandleKey(VKDown, true, 0)
	d.HandleKey(VKDown, false, 0)
	d.HandleKey(VKEscape, true, 0)
	d.HandleKey(VKEscape, false, 0)

	want := []Action{
		ActionRunAndClose,
		ActionReload,
		ActionFullscreen,
		ActionSwitch,
		ActionClose,
	}
	if !slices.Equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}
