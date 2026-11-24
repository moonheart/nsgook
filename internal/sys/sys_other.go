//go:build !windows

package sys

import (
	"image"
	"os/exec"
	"runtime"
)

// NativeSystem provides portable fallback behavior off Windows.
type NativeSystem struct{}

// NewNativeSystem returns the portable fallback System implementation.
func NewNativeSystem() System {
	return NativeSystem{}
}

func (NativeSystem) StartHook(func(vk uint32, down bool, mods uint32), func()) func() {
	return func() {}
}

func (NativeSystem) GetFocusedWindowType() FocusedWindowType {
	return FocusedInvalid
}

func (NativeSystem) GetCurrentSelection() string {
	return ""
}

func (NativeSystem) IsForegroundOwnedBySelf() bool {
	return false
}

func (NativeSystem) OpenDefault(path string) error {
	if path == "" {
		return nil
	}
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", path).Start()
	case "linux":
		return exec.Command("xdg-open", path).Start()
	default:
		return nil
	}
}

func (NativeSystem) OpenWithDialog(path string) error {
	return NativeSystem{}.OpenDefault(path)
}

func (NativeSystem) GetAssocAppName(string) string {
	return ""
}

func (NativeSystem) GetShellThumbnail(string, int) (*image.RGBA, error) {
	return nil, nil
}

func (NativeSystem) GetShellIcon(string, int) (*image.RGBA, error) {
	return nil, nil
}

func (NativeSystem) GetDriveSpace(string) (total, free uint64, ok bool) {
	return 0, 0, false
}
