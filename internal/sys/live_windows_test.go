//go:build windows

package sys

import (
	"os"
	"strings"
	"testing"
	"unsafe"
)

// TestLiveEverythingSelection exercises the real Everything IPC path.
// It is opt-in because it needs a running Everything window with a selection and
// because Everything 1.4 publishes its selection through the clipboard.
func TestLiveEverythingSelection(t *testing.T) {
	if os.Getenv("NSGOOK_LIVE_EVERYTHING") == "" {
		t.Skip("set NSGOOK_LIVE_EVERYTHING=1 to run")
	}

	var hwnd uintptr
	enumWindows(func(h uintptr) bool {
		if !strings.HasPrefix(getWindowClass(h), "EVERYTHING") {
			return true
		}
		if vis, _, _ := procIsWindowVisible.Call(h); vis == 0 {
			return true
		}
		hwnd = h
		return false
	})
	if hwnd == 0 {
		t.Skip("no visible Everything window")
	}

	fg, _, _ := procGetForegroundWindow.Call()
	if fg != hwnd {
		t.Skipf("Everything is not the foreground window (fg=%q); focus it and rerun", getWindowClass(fg))
	}

	var tid uintptr
	tid, _, _ = procGetWindowThreadProcessId.Call(fg, 0)
	var gti guiThreadInfo
	gti.cbSize = uint32(unsafe.Sizeof(gti))
	procGetGUIThreadInfo.Call(tid, uintptr(unsafe.Pointer(&gti)))
	t.Logf("fg=0x%X class=%q cursorActivated=%v gti.flags=%d focus=0x%X caret=0x%X",
		fg, getWindowClass(fg), isCursorActivated(fg), gti.flags, gti.hwndFocus, gti.hwndCaret)

	if got := getFocusedWindowTypeWin(); got != FocusedEverything {
		t.Fatalf("foreground type = %v, want FocusedEverything (click the result list first)", got)
	}

	focusPath := everythingFocusPath(hwnd)
	t.Logf("Everything hwnd=0x%X class=%q hidden-focus=%q", hwnd, getWindowClass(hwnd), focusPath)

	path := getSelectedFromEverything(hwnd)
	t.Logf("selection=%q", path)
	if path == "" {
		t.Fatal("Everything selection path is empty")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("Everything selection %q does not exist: %v", path, err)
	}
}
