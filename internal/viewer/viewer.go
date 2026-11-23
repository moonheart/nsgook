// Package viewer defines NSGook's viewer plugin contract, runtime context, and registry.
package viewer

import (
	"slices"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/ui"

	"nsgook/internal/config"
	"nsgook/internal/sys"
)

// MenuAction describes an entry contributed by a viewer plugin to the window's More menu.
type MenuAction struct {
	ID        string
	Label     string
	Icon      func() *core.SVG
	Shortcut  string
	Checked   func() bool
	Separator bool
	OnSelect  func()
}

// Context connects a viewer plugin instance to the hosting preview window and services.
type Context struct {
	Path          string
	IsDir         bool
	Title         *core.Signal[string]
	IsBusy        *core.Signal[bool]
	PreferredSize core.Size
	// DesktopSize is the usable desktop bounds in points, used by
	// SetPreferredSizeFit when a caller does not pass an explicit desktop size.
	DesktopSize        core.Size
	CanResize          *core.Signal[bool]
	FullWindowDragging *core.Signal[bool]
	TitlebarOverlap    *core.Signal[bool]
	TitlebarAutoHide   *core.Signal[bool]
	IsBlocked          bool
	MoreItems          *core.Signal[[]MenuAction]
	Releasing          *core.Signal[bool]
	mediaCount         int

	Config       *config.Store
	Sys          sys.System
	SwitchPlugin func(pluginID string)
	Toast        func(ui.ToastType, string)
	OpenPath     func(path string)
	CloseWindow  func()
}

// NewContext creates an initialized Context bound to cx's scope.
func NewContext(cx *core.Scope, cfg *config.Store, s sys.System) *Context {
	return &Context{
		Title:              core.NewSignal(cx, ""),
		IsBusy:             core.NewSignal(cx, false),
		PreferredSize:      core.Size{Width: 800, Height: 600},
		CanResize:          core.NewSignal(cx, true),
		FullWindowDragging: core.NewSignal(cx, false),
		TitlebarOverlap:    core.NewSignal(cx, false),
		TitlebarAutoHide:   core.NewSignal(cx, false),
		MoreItems:          core.NewSignalFunc(cx, []MenuAction(nil), func(_, _ []MenuAction) bool { return false }),
		Releasing:          core.NewSignal(cx, false),
		Config:             cfg,
		Sys:                s,
	}
}

// TrackMedia marks cx as holding a mounted media view (e.g. core.Image) that should
// commit an empty source for one tick before being unmounted or replaced.
func (c *Context) TrackMedia(cx *core.Scope) {
	if c == nil || cx == nil {
		return
	}
	c.mediaCount++
	cx.Cleanup(func() {
		if c.mediaCount > 0 {
			c.mediaCount--
		}
	})
}

// HasMedia reports whether any active plugin view under this Context registered media views.
func (c *Context) HasMedia() bool {
	return c != nil && c.mediaCount > 0
}

// IsReleasing reports reactively whether the viewer session is clearing active media
// sources prior to unmounting or replacing the current plugin view.
func (c *Context) IsReleasing() bool {
	return c != nil && c.Releasing != nil && c.Releasing.Get()
}

// Reset restores per-preview properties on ctx before preparing a new file.
func (c *Context) Reset(path string, isDir bool) {
	c.Path = path
	c.IsDir = isDir
	c.IsBlocked = false
	c.PreferredSize = core.Size{Width: 800, Height: 600}
	c.DesktopSize = core.Size{}
	if c.Title != nil {
		c.Title.Set("")
	}
	if c.IsBusy != nil {
		c.IsBusy.Set(false)
	}
	if c.CanResize != nil {
		c.CanResize.Set(true)
	}
	if c.FullWindowDragging != nil {
		c.FullWindowDragging.Set(false)
	}
	if c.TitlebarOverlap != nil {
		c.TitlebarOverlap.Set(false)
	}
	if c.TitlebarAutoHide != nil {
		c.TitlebarAutoHide.Set(false)
	}
	if c.MoreItems != nil {
		c.MoreItems.Set(nil)
	}
}

// SetPreferredSizeFit scales size down so it fits within maxRatio of desktop without upscaling.
func (c *Context) SetPreferredSizeFit(size core.Size, maxRatio float64, desktop core.Size) float64 {
	if maxRatio > 1 {
		maxRatio = 1
	}
	if maxRatio <= 0 {
		maxRatio = 0.8
	}
	if desktop.Width <= 0 || desktop.Height <= 0 {
		desktop = c.DesktopSize
	}
	if desktop.Width <= 0 || desktop.Height <= 0 {
		desktop = core.Size{Width: 1280, Height: 800}
	}
	if size.Width <= 0 || size.Height <= 0 {
		c.PreferredSize = core.Size{Width: 640, Height: 480}
		return 1
	}

	wRatio := float64(desktop.Width) * maxRatio / float64(size.Width)
	hRatio := float64(desktop.Height) * maxRatio / float64(size.Height)
	ratio := min(wRatio, hRatio)
	if ratio > 1 {
		ratio = 1
	}
	c.PreferredSize = core.Size{
		Width:  size.Width * core.Points(ratio),
		Height: size.Height * core.Points(ratio),
	}
	return ratio
}

// Plugin is a file-format viewer analogous to QuickLook's IViewer.
type Plugin interface {
	ID() string
	Name() string
	Priority() int
	CanHandle(path string) bool
	Prepare(path string, ctx *Context)
	View(cx *core.Scope, path string, ctx *Context) core.Node
}

// ExtendedMenuProvider is an optional interface for plugins that contribute actions
// to the More menu even when another plugin is active (analogous to IMoreMenuExtended).
type ExtendedMenuProvider interface {
	ExtendedMenuItems(cx *core.Scope, path string, ctx *Context) []MenuAction
}

// Registry maintains the priority-ordered set of viewer plugins and the fallback Default plugin.
type Registry struct {
	Default Plugin
	plugins []Plugin
}

// NewRegistry builds a Registry sorted by descending Priority().
func NewRegistry(defaultPlugin Plugin, plugins ...Plugin) *Registry {
	ordered := slices.Clone(plugins)
	slices.SortStableFunc(ordered, func(a, b Plugin) int {
		if a.Priority() > b.Priority() {
			return -1
		}
		if a.Priority() < b.Priority() {
			return 1
		}
		return 0
	})
	return &Registry{
		Default: defaultPlugin,
		plugins: ordered,
	}
}

// All returns all registered plugins in descending priority order.
func (r *Registry) All() []Plugin {
	return slices.Clone(r.plugins)
}

// FindByID returns the plugin matching id (including Default), or nil if none exists.
func (r *Registry) FindByID(id string) Plugin {
	if r.Default != nil && r.Default.ID() == id {
		return r.Default
	}
	for _, p := range r.plugins {
		if p.ID() == id {
			return p
		}
	}
	return nil
}

// FindMatch picks the highest-priority plugin whose CanHandle returns true, falling back to Default.
func (r *Registry) FindMatch(path string) Plugin {
	if path == "" {
		return r.Default
	}
	for _, p := range r.plugins {
		if safeCanHandle(p, path) {
			return p
		}
	}
	return r.Default
}

// CollectExtendedMenuItems gathers More menu actions from non-active ExtendedMenuProvider plugins
// in ascending priority order (matching QuickLook's ViewerWindow.Actions.cs).
func (r *Registry) CollectExtendedMenuItems(cx *core.Scope, active Plugin, path string, ctx *Context) []MenuAction {
	var out []MenuAction
	ordered := slices.Clone(r.plugins)
	slices.SortStableFunc(ordered, func(a, b Plugin) int {
		if a.Priority() < b.Priority() {
			return -1
		}
		if a.Priority() > b.Priority() {
			return 1
		}
		return 0
	})
	for _, p := range ordered {
		if active != nil && p.ID() == active.ID() {
			continue
		}
		if ext, ok := p.(ExtendedMenuProvider); ok {
			out = append(out, ext.ExtendedMenuItems(cx, path, ctx)...)
		}
	}
	return out
}

func safeCanHandle(p Plugin, path string) (ok bool) {
	defer func() {
		if recover() != nil {
			ok = false
		}
	}()
	return p.CanHandle(path)
}
