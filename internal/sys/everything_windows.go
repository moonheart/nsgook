//go:build windows

package sys

import (
	"strings"
	"syscall"
	"time"
	"unsafe"
)

// Everything IPC constants, mirroring QuickLook's QuickLook.Native32/Everything.cpp.
// Everything 1.5 exposes the focused result path as the window text of a hidden
// EVERYTHING_RESULT_LIST_FOCUS child. Everything 1.4 lacks that window, so the
// documented WM_COMMAND copy-to-clipboard command is used as a fallback.
const (
	everythingFocusWindowClass   = "EVERYTHING_RESULT_LIST_FOCUS"
	everythingIpcCopyToClipboard = 41007

	wmCommand      = 0x0111
	cfUnicodeText  = 13
	clipboardTries = 20
	clipboardDelay = 10 * time.Millisecond
	everythingWait = 300 * time.Millisecond
)

var (
	procOpenClipboard              = user32.NewProc("OpenClipboard")
	procCloseClipboard             = user32.NewProc("CloseClipboard")
	procGetClipboardData           = user32.NewProc("GetClipboardData")
	procGetClipboardSequenceNumber = user32.NewProc("GetClipboardSequenceNumber")
	procSendMessageW               = user32.NewProc("SendMessageW")
	procGlobalSize                 = kernel32.NewProc("GlobalSize")
)

// getSelectedFromEverything resolves the focused result in an Everything window.
func getSelectedFromEverything(fg uintptr) string {
	if path := everythingFocusPath(fg); path != "" {
		return path
	}
	return everythingClipboardPath(fg)
}

// everythingFocusPath reads the hidden focus window used by Everything 1.5.
func everythingFocusPath(fg uintptr) string {
	if fg == 0 {
		return ""
	}
	hidden, _, _ := procFindWindowExW.Call(
		fg,
		0,
		uintptr(unsafe.Pointer(wide(everythingFocusWindowClass))),
		0,
	)
	if hidden == 0 {
		return ""
	}
	return firstPathLine(getWindowText(hidden))
}

// everythingClipboardPath asks Everything 1.4 to copy the selection to the
// clipboard and reads the first path back. The clipboard sequence number avoids
// returning stale contents copied by a previous invocation.
func everythingClipboardPath(fg uintptr) string {
	if fg == 0 {
		return ""
	}
	before, _, _ := procGetClipboardSequenceNumber.Call()
	procSendMessageW.Call(fg, wmCommand, everythingIpcCopyToClipboard, 0)

	deadline := time.Now().Add(everythingWait)
	for {
		seq, _, _ := procGetClipboardSequenceNumber.Call()
		if seq != before {
			if text := readClipboardUnicodeText(); text != "" {
				return firstPathLine(text)
			}
		}
		if time.Now().After(deadline) {
			return ""
		}
		time.Sleep(clipboardDelay)
	}
}

func readClipboardUnicodeText() string {
	for i := 0; i < clipboardTries; i++ {
		opened, _, _ := procOpenClipboard.Call(0)
		if opened != 0 {
			h, _, _ := procGetClipboardData.Call(cfUnicodeText)
			text := clipboardText(h)
			procCloseClipboard.Call()
			return text
		}
		time.Sleep(clipboardDelay)
	}
	return ""
}

func clipboardText(h uintptr) string {
	if h == 0 {
		return ""
	}
	ptr, _, _ := procGlobalLock.Call(h)
	if ptr == 0 {
		return ""
	}
	defer procGlobalUnlock.Call(h)

	if size, _, _ := procGlobalSize.Call(h); size >= 2 {
		units := int(size / 2)
		if units > 1<<20 {
			units = 1 << 20
		}
		buf := unsafe.Slice((*uint16)(foreignPtr(ptr)), units)
		for i, c := range buf {
			if c == 0 {
				return syscall.UTF16ToString(buf[:i])
			}
		}
		return syscall.UTF16ToString(buf)
	}
	return utf16PtrToString(ptr)
}

// firstPathLine keeps the first non-empty line, tolerating quoted clipboard text.
func firstPathLine(text string) string {
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		return strings.TrimSpace(strings.Trim(line, `"`))
	}
	return ""
}
