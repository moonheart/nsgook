//go:build windows

package sys

import (
	"runtime"
	"sync"
	"syscall"
	"unsafe"
)

var (
	procSetWindowsHookExW   = user32.NewProc("SetWindowsHookExW")
	procUnhookWindowsHookEx = user32.NewProc("UnhookWindowsHookEx")
	procCallNextHookEx      = user32.NewProc("CallNextHookEx")
	procSetWinEventHook     = user32.NewProc("SetWinEventHook")
	procUnhookWinEvent      = user32.NewProc("UnhookWinEvent")
	procGetAsyncKeyState    = user32.NewProc("GetAsyncKeyState")
	procGetMessageW         = user32.NewProc("GetMessageW")
	procTranslateMessage    = user32.NewProc("TranslateMessage")
	procDispatchMessageW    = user32.NewProc("DispatchMessageW")
	procPostThreadMessageW  = user32.NewProc("PostThreadMessageW")
	procGetModuleHandleW    = kernel32.NewProc("GetModuleHandleW")
)

const (
	whKeyboardLL          = 13
	wmKeyDown             = 0x0100
	wmKeyUp               = 0x0101
	wmSysKeyDown          = 0x0104
	wmSysKeyUp            = 0x0105
	wmQuit                = 0x0012
	eventSystemForeground = 0x0003
	wineventOutOfContext  = 0x0000

	vkLShift   = 0xA0
	vkRShift   = 0xA1
	vkLControl = 0xA2
	vkRControl = 0xA3
	vkLMenu    = 0xA4
	vkRMenu    = 0xA5
	vkLWin     = 0x5B
	vkRWin     = 0x5C

	thirdPartyReplayMarker = 0x514C5452 // 'QLTR'
)

type kbdLLHookStruct struct {
	vkCode      uint32
	scanCode    uint32
	flags       uint32
	time        uint32
	dwExtraInfo uintptr
}

type winMsg struct {
	hwnd    uintptr
	message uint32
	wParam  uintptr
	lParam  uintptr
	time    uint32
	pt      struct{ x, y int32 }
}

var (
	hookMu      sync.Mutex
	activeKbd   func(vk uint32, down bool, mods uint32)
	activeFG    func()
	currentKbdH uintptr

	kbdHookProc = syscall.NewCallback(func(code int32, wParam uintptr, lParam uintptr) uintptr {
		if code >= 0 && lParam != 0 {
			kb := (*kbdLLHookStruct)(foreignPtr(lParam))
			if kb.dwExtraInfo != thirdPartyReplayMarker && !isKeyDown(vkLWin) && !isKeyDown(vkRWin) {
				var down, known bool
				switch wParam {
				case wmKeyDown, wmSysKeyDown:
					down, known = true, true
				case wmKeyUp, wmSysKeyUp:
					down, known = false, true
				}
				if known {
					mods := currentModifiers()
					hookMu.Lock()
					fn := activeKbd
					hookMu.Unlock()
					if fn != nil {
						fn(kb.vkCode, down, mods)
					}
				}
			}
		}
		ret, _, _ := procCallNextHookEx.Call(currentKbdH, uintptr(code), wParam, lParam)
		return ret
	})

	winEventProc = syscall.NewCallback(func(hHook, event, hwnd, idObject, idChild, idThread, time uintptr) uintptr {
		hookMu.Lock()
		fn := activeFG
		hookMu.Unlock()
		if fn != nil {
			fn()
		}
		return 0
	})
)

func isKeyDown(vk int) bool {
	state, _, _ := procGetAsyncKeyState.Call(uintptr(vk))
	return int16(state) < 0
}

func currentModifiers() uint32 {
	var mods uint32
	if isKeyDown(vkLShift) || isKeyDown(vkRShift) {
		mods |= ModShift
	}
	if isKeyDown(vkLControl) || isKeyDown(vkRControl) {
		mods |= ModControl
	}
	if isKeyDown(vkLMenu) || isKeyDown(vkRMenu) {
		mods |= ModAlt
	}
	return mods
}

func startHookWin(onKey func(vk uint32, down bool, mods uint32), onForeground func()) func() {
	hookMu.Lock()
	activeKbd = onKey
	activeFG = onForeground
	hookMu.Unlock()

	var tid uint32
	ready := make(chan struct{})
	done := make(chan struct{})

	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		defer close(done)

		t, _, _ := procGetCurrentThreadId.Call()
		tid = uint32(t)

		hMod, _, _ := procGetModuleHandleW.Call(0)
		hKbd, _, _ := procSetWindowsHookExW.Call(whKeyboardLL, kbdHookProc, hMod, 0)
		currentKbdH = hKbd
		hEvt, _, _ := procSetWinEventHook.Call(eventSystemForeground, eventSystemForeground, 0, winEventProc, 0, 0, wineventOutOfContext)

		close(ready)

		var msg winMsg
		for {
			r, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0)
			if int32(r) <= 0 {
				break
			}
			procTranslateMessage.Call(uintptr(unsafe.Pointer(&msg)))
			procDispatchMessageW.Call(uintptr(unsafe.Pointer(&msg)))
		}

		if hKbd != 0 {
			procUnhookWindowsHookEx.Call(hKbd)
			currentKbdH = 0
		}
		if hEvt != 0 {
			procUnhookWinEvent.Call(hEvt)
		}
	}()

	<-ready

	var once sync.Once
	return func() {
		once.Do(func() {
			hookMu.Lock()
			activeKbd = nil
			activeFG = nil
			hookMu.Unlock()
			if tid != 0 {
				procPostThreadMessageW.Call(uintptr(tid), wmQuit, 0, 0)
			}
			<-done
		})
	}
}
