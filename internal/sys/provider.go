package sys

import (
	"image"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// FocusedWindowType identifies the foreground file manager or desktop surface.
type FocusedWindowType uint8

const (
	FocusedInvalid FocusedWindowType = iota
	FocusedDesktop
	FocusedExplorer
	FocusedDialog
	FocusedEverything
	FocusedDOpus
	FocusedMultiCommander
	FocusedIDM
	FocusedFilePilot
	FocusedDeskBox
)

// System abstracts Windows/OS shell hooks, file selection inspection, thumbnails, and file launch actions.
type System interface {
	StartHook(onKey func(vk uint32, down bool, mods uint32), onForeground func()) (stop func())
	GetFocusedWindowType() FocusedWindowType
	GetCurrentSelection() string
	IsForegroundOwnedBySelf() bool
	OpenDefault(path string) error
	OpenWithDialog(path string) error
	GetAssocAppName(path string) string
	GetShellThumbnail(path string, size int) (*image.RGBA, error)
	GetDriveSpace(path string) (total, free uint64, ok bool)
}

// FocusMonitor polls the active file manager selection while a preview window is open.
type FocusMonitor struct {
	mu      sync.Mutex
	sys     System
	running bool
	stopCh  chan struct{}
}

// NewFocusMonitor creates a FocusMonitor backed by sys.
func NewFocusMonitor(s System) *FocusMonitor {
	return &FocusMonitor{sys: s}
}

// Start begins polling the active selection every 400ms until Stop is called.
func (m *FocusMonitor) Start(initialPath string, onSwitch func(path string)) {
	m.mu.Lock()
	if m.running {
		m.mu.Unlock()
		return
	}
	m.running = true
	stopCh := make(chan struct{})
	m.stopCh = stopCh
	sysRef := m.sys
	m.mu.Unlock()

	go func() {
		last := initialPath
		ticker := time.NewTicker(400 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-stopCh:
				return
			case <-ticker.C:
				if sysRef == nil {
					continue
				}
				if sysRef.GetFocusedWindowType() == FocusedInvalid {
					continue
				}
				path := sysRef.GetCurrentSelection()
				if path != "" && path != last {
					last = path
					if onSwitch != nil {
						onSwitch(path)
					}
				}
			}
		}
	}()
}

// Stop halts the background selection polling loop.
func (m *FocusMonitor) Stop() {
	m.mu.Lock()
	if !m.running {
		m.mu.Unlock()
		return
	}
	m.running = false
	close(m.stopCh)
	m.stopCh = nil
	m.mu.Unlock()
}

// WatchFile polls path's modification timestamp and size, invoking onChange when updated.
func WatchFile(path string, onChange func()) func() {
	if path == "" || onChange == nil {
		return func() {}
	}
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return func() {}
	}
	lastMod := info.ModTime()
	lastSize := info.Size()

	done := make(chan struct{})
	var once sync.Once
	go func() {
		ticker := time.NewTicker(500 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				cur, statErr := os.Stat(path)
				if statErr != nil || cur.IsDir() {
					continue
				}
				if !cur.ModTime().Equal(lastMod) || cur.Size() != lastSize {
					lastMod = cur.ModTime()
					lastSize = cur.Size()
					onChange()
				}
			}
		}
	}()
	return func() {
		once.Do(func() { close(done) })
	}
}

// FormatCopyPath quotes path for clipboard copying, adding the Windows long-path prefix when needed.
func FormatCopyPath(path string) string {
	if path == "" {
		return `""`
	}
	if len(path) >= 260 && filepath.IsAbs(path) && !strings.HasPrefix(path, `\\?\`) {
		path = `\\?\` + path
	}
	return `"` + path + `"`
}
