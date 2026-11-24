//go:build windows

package sys

import (
	"fmt"
	"image"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"unsafe"
)

const (
	swShowNormal            = 1
	assocFNone              = 0
	assocStrFriendlyAppName = 4
)

// NativeSystem implements System using pure-Go Win32/COM APIs.
type NativeSystem struct{}

// NewNativeSystem returns the Windows System implementation.
func NewNativeSystem() System {
	return NativeSystem{}
}

func (NativeSystem) StartHook(onKey func(vk uint32, down bool, mods uint32), onForeground func()) func() {
	return startHookWin(onKey, onForeground)
}

func (NativeSystem) GetFocusedWindowType() FocusedWindowType {
	return getFocusedWindowTypeWin()
}

func (NativeSystem) GetCurrentSelection() string {
	return getCurrentSelectionWin()
}

func (NativeSystem) IsForegroundOwnedBySelf() bool {
	fg, _, _ := procGetForegroundWindow.Call()
	if fg == 0 {
		return false
	}
	var pid uint32
	procGetWindowThreadProcessId.Call(fg, uintptr(unsafe.Pointer(&pid)))
	return int(pid) == os.Getpid()
}

func (NativeSystem) OpenDefault(path string) error {
	if path == "" {
		return nil
	}
	dir := filepath.Dir(path)
	r, _, err := procShellExecuteW.Call(
		0,
		uintptr(unsafe.Pointer(wide("open"))),
		uintptr(unsafe.Pointer(wide(path))),
		0,
		uintptr(unsafe.Pointer(wide(dir))),
		swShowNormal,
	)
	if r <= 32 {
		return fmt.Errorf("ShellExecuteW failed (%d): %v", r, err)
	}
	return nil
}

func (NativeSystem) OpenWithDialog(path string) error {
	if path == "" {
		return nil
	}
	cmd := exec.Command("rundll32.exe", "shell32.dll,OpenAs_RunDLL", path)
	return cmd.Start()
}

func (NativeSystem) GetAssocAppName(path string) string {
	ext := filepath.Ext(path)
	if ext == "" {
		return ""
	}
	var size uint32 = 256
	buf := make([]uint16, size)
	hr, _, _ := procAssocQueryStringW.Call(
		assocFNone,
		assocStrFriendlyAppName,
		uintptr(unsafe.Pointer(wide(ext))),
		0,
		uintptr(unsafe.Pointer(&buf[0])),
		uintptr(unsafe.Pointer(&size)),
	)
	if failed(hr) || size == 0 {
		return ""
	}
	return syscall.UTF16ToString(buf[:size])
}

func (NativeSystem) GetShellThumbnail(path string, size int) (*image.RGBA, error) {
	return getShellThumbnailWin(path, size)
}

func (NativeSystem) GetShellIcon(path string, size int) (*image.RGBA, error) {
	return getShellIconWin(path, size)
}

func (NativeSystem) GetDriveSpace(path string) (total, free uint64, ok bool) {
	var freeCaller, totalBytes, totalFree uint64
	r, _, _ := procGetDiskFreeSpaceExW.Call(
		uintptr(unsafe.Pointer(wide(path))),
		uintptr(unsafe.Pointer(&freeCaller)),
		uintptr(unsafe.Pointer(&totalBytes)),
		uintptr(unsafe.Pointer(&totalFree)),
	)
	if r == 0 || totalBytes == 0 {
		return 0, 0, false
	}
	return totalBytes, freeCaller, true
}
