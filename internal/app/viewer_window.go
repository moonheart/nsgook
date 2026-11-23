package app

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/desktop"
	"github.com/nsgo-dev/nsgo/icons/lucide"
	"github.com/nsgo-dev/nsgo/ui"

	"nsgook/internal/plugins/infopanel"
	"nsgook/internal/plugins/pluginui"
	"nsgook/internal/sys"
	"nsgook/internal/viewer"
	"nsgook/locales"
)

// ViewerSession owns the reactive state, plugin context, file watcher, and optional desktop window
// for a single preview window instance.
type ViewerSession struct {
	ctrl   *Controller
	scope  *core.Scope
	win    *desktop.Window
	ctx    *viewer.Context
	closed atomic.Bool

	pathSig       *core.Signal[string]
	isDirSig      *core.Signal[bool]
	pluginSig     *core.Signal[string]
	viewPathSig   *core.Signal[string]
	viewPluginSig *core.Signal[string]
	viewBlocked   *core.Signal[bool]
	viewSeq       *core.Signal[uint64]
	mountGen      uint64
	pinnedSig     *core.Signal[bool]
	topmostSig    *core.Signal[bool]
	reloadSeq     *core.Signal[uint64]
	dropHover     *core.Signal[bool]

	watchStop      func()
	resizeFn       func()
	fullscreenFn   func()
	bringToFrontFn func()
}

// NewViewerSession creates a ViewerSession bound to an owned child scope of cx and loads initialPath.
func NewViewerSession(ctrl *Controller, cx *core.Scope, initialPath string, opts PreviewOptions) *ViewerSession {
	cfg := ctrl.cfg.Get()
	sessScope := cx.Child()

	var s *ViewerSession
	sessScope.Run(func() {
		ctx := viewer.NewContext(sessScope, ctrl.cfg, ctrl.sys)
		s = &ViewerSession{
			ctrl:          ctrl,
			scope:         sessScope,
			ctx:           ctx,
			pathSig:       core.NewSignal(sessScope, ""),
			isDirSig:      core.NewSignal(sessScope, false),
			pluginSig:     core.NewSignal(sessScope, ""),
			viewPathSig:   core.NewSignal(sessScope, ""),
			viewPluginSig: core.NewSignal(sessScope, ""),
			viewBlocked:   core.NewSignal(sessScope, false),
			viewSeq:       core.NewSignal(sessScope, uint64(0)),
			pinnedSig:     core.NewSignal(sessScope, opts.Pinned),
			topmostSig:    core.NewSignal(sessScope, cfg.Topmost || opts.Topmost),
			reloadSeq:     core.NewSignal(sessScope, uint64(0)),
			dropHover:     core.NewSignal(sessScope, false),
		}

		ctx.SwitchPlugin = func(pluginID string) {
			p := s.pathSig.GetUntracked()
			if p == "" || s.IsClosed() {
				return
			}
			s.Load(p, pluginID)
		}
		ctx.OpenPath = func(p string) {
			if p != "" {
				s.Load(p, "")
			}
		}
		ctx.CloseWindow = s.Close

		sessScope.Cleanup(s.stopWatcher)
		if initialPath != "" {
			s.Load(initialPath, opts.ForcePluginID)
		}
	})
	return s
}

func (s *ViewerSession) Path() string     { return s.pathSig.GetUntracked() }
func (s *ViewerSession) PluginID() string { return s.pluginSig.GetUntracked() }
func (s *ViewerSession) Pinned() bool     { return s.pinnedSig.GetUntracked() }
func (s *ViewerSession) Topmost() bool    { return s.topmostSig.GetUntracked() }
func (s *ViewerSession) IsClosed() bool   { return s.closed.Load() || (s.win != nil && s.win.Closed()) }
func (s *ViewerSession) markClosed()      { s.closed.Store(true); s.stopWatcher(); s.detachWindow() }

func (s *ViewerSession) detachWindow() {
	s.resizeFn = nil
	s.fullscreenFn = nil
	s.bringToFrontFn = nil
	s.win = nil
	if s.ctx != nil {
		s.ctx.Toast = nil
		if s.ctx.MoreItems != nil && s.scope != nil && !s.scope.Disposed() {
			s.ctx.MoreItems.Set(nil)
		}
	}
}

func (s *ViewerSession) disposeScope() {
	s.stopWatcher()
	s.detachWindow()
	if s.scope != nil && !s.scope.Disposed() {
		s.scope.Dispose()
	}
}

// Unload clears active preview state and unmounts heavy plugin views while keeping the hidden window reusable.
func (s *ViewerSession) Unload() {
	s.stopWatcher()
	if s.scope != nil && !s.scope.Disposed() {
		s.scope.Batch(func() {
			if s.ctx != nil {
				s.ctx.Reset("", false)
			}
			s.isDirSig.Set(false)
			s.pathSig.Set("")
			s.pluginSig.Set("")
			s.scheduleContentMount("", "", false)
		})
	}
}

func (s *ViewerSession) scheduleContentMount(path, pluginID string, blocked bool) {
	s.mountGen++
	gen := s.mountGen
	if s.viewPathSig.GetUntracked() == "" || !s.ctx.HasMedia() || s.scope == nil || s.scope.Disposed() {
		if s.ctx != nil && s.ctx.Releasing != nil {
			s.ctx.Releasing.Set(false)
		}
		s.viewBlocked.Set(blocked)
		s.viewPathSig.Set(path)
		s.viewPluginSig.Set(pluginID)
		s.viewSeq.Update(func(v uint64) uint64 { return v + 1 })
		return
	}
	if s.ctx != nil && s.ctx.Releasing != nil {
		s.ctx.Releasing.Set(true)
	}
	s.scope.Post(func() {
		if s.scope == nil || s.scope.Disposed() || s.IsClosed() || s.mountGen != gen {
			return
		}
		s.scope.Batch(func() {
			if s.ctx != nil && s.ctx.Releasing != nil {
				s.ctx.Releasing.Set(false)
			}
			s.viewBlocked.Set(blocked)
			s.viewPathSig.Set(path)
			s.viewPluginSig.Set(pluginID)
			s.viewSeq.Update(func(v uint64) uint64 { return v + 1 })
		})
	})
}

// Focus brings the preview window to the front and activates it.
func (s *ViewerSession) Focus() {
	if s.win != nil && !s.IsClosed() {
		s.win.Focus()
	}
}

// Close closes the preview window, stops any file watcher, and disposes the session scope.
func (s *ViewerSession) Close() {
	if s.closed.Swap(true) {
		return
	}
	s.stopWatcher()
	if s.ctrl != nil {
		s.ctrl.RemoveSession(s)
	}
	win := s.win
	s.detachWindow()
	if s.scope != nil && !s.scope.Disposed() {
		s.scope.Dispose()
	}
	if win != nil && !win.Closed() {
		win.Close()
	}
}

// Reload forces the current file and plugin view to reload cleanly.
func (s *ViewerSession) Reload() {
	p := s.pathSig.GetUntracked()
	pid := s.pluginSig.GetUntracked()
	if p == "" || s.IsClosed() {
		return
	}
	s.reloadSeq.Update(func(v uint64) uint64 { return v + 1 })
	s.Load(p, pid)
}

// RequestFullscreenToggle toggles fullscreen state on the underlying window if mounted.
func (s *ViewerSession) RequestFullscreenToggle() {
	if s.win != nil && !s.IsClosed() && s.fullscreenFn != nil {
		s.fullscreenFn()
	}
}

// BringToFront raises the preview window in Z-order above File Explorer without stealing focus
// (pulsing Floating -> Normal unless pinned always-on-top, matching QuickLook's BringToFront).
func (s *ViewerSession) BringToFront() {
	if !s.IsClosed() && s.bringToFrontFn != nil {
		s.bringToFrontFn()
	}
}

// Load resolves the viewer plugin for path (or forcePluginID), prepares context, updates watchers,
// and triggers responsive window sizing via 9-zone placement.
func (s *ViewerSession) Load(path string, forcePluginID string) {
	if path == "" || s.IsClosed() || (s.scope != nil && s.scope.Disposed()) {
		return
	}
	clean := filepath.Clean(path)

	isDir := false
	if strings.HasPrefix(clean, "::") {
		isDir = false
	} else if st, err := os.Stat(clean); err == nil && st.IsDir() {
		isDir = true
	}

	batch := func(fn func()) {
		if s.scope != nil {
			s.scope.Batch(fn)
		} else {
			fn()
		}
	}

	blocked := false
	batch(func() {
		s.ctx.Reset(clean, isDir)
		s.isDirSig.Set(isDir)

		allowed := s.ctrl.cfg.IsExtensionAllowed(clean)
		if !allowed && forcePluginID == "" {
			blocked = true
			s.ctx.IsBlocked = true
			s.ctx.PreferredSize = core.Size{Width: 520, Height: 260}
			s.ctx.Title.Set(viewer.DisplayName(clean))
			s.pathSig.Set(clean)
			s.pluginSig.Set("blocked")
			s.reloadSeq.Update(func(v uint64) uint64 { return v + 1 })
			s.scheduleContentMount(clean, "blocked", true)
			return
		}

		var plug viewer.Plugin
		if forcePluginID != "" {
			plug = s.ctrl.registry.FindByID(forcePluginID)
		}
		if plug == nil {
			if isDir {
				plug = s.ctrl.registry.Default
			} else {
				plug = s.ctrl.registry.FindMatch(clean)
			}
		}
		if plug == nil {
			plug = infopanel.New()
		}

		// Guard against third-party or unexpected panics in Prepare().
		func() {
			defer func() {
				if recover() != nil {
					plug = s.ctrl.registry.Default
					if plug == nil {
						plug = infopanel.New()
					}
					s.ctx.Reset(clean, isDir)
					s.isDirSig.Set(isDir)
					plug.Prepare(clean, s.ctx)
				}
			}()
			plug.Prepare(clean, s.ctx)
		}()

		s.pathSig.Set(clean)
		s.pluginSig.Set(plug.ID())
		s.reloadSeq.Update(func(v uint64) uint64 { return v + 1 })
		s.scheduleContentMount(clean, plug.ID(), false)
	})

	s.updateWatcher(clean, isDir)
	if s.resizeFn != nil {
		s.resizeFn()
	}
	if !blocked && s.win != nil && !s.IsClosed() {
		s.BringToFront()
	}
}

func (s *ViewerSession) updateWatcher(path string, isDir bool) {
	s.stopWatcher()
	if isDir || strings.HasPrefix(path, "::") || s.ctrl == nil || !s.ctrl.cfg.Get().AutoReload {
		return
	}
	s.watchStop = sys.WatchFile(path, func() {
		s.ctrl.app.Post(func() {
			if !s.IsClosed() && s.pathSig.GetUntracked() == path {
				s.Reload()
			}
		})
	})
}

func (s *ViewerSession) stopWatcher() {
	if s.watchStop != nil {
		s.watchStop()
		s.watchStop = nil
	}
}

// RenderViewerSurface builds the complete NSGook preview window UI (caption bar, content container,
// error fallback boundary, loading spinner, drag-drop zone, and toasts).
func RenderViewerSurface(cx *core.Scope, s *ViewerSession, onClose func()) core.Node {
	colors := ui.ColorsOf(cx)
	toaster := ui.NewToaster(cx)
	s.ctx.Toast = func(kind ui.ToastType, msg string) {
		if msg == "" {
			return
		}
		toaster.Show(ui.ToastContent{Type: kind, Title: msg})
	}
	cx.Cleanup(func() { s.detachWindow() })

	if onClose == nil {
		onClose = s.Close
	}

	s.fullscreenFn = func() {
		if desktop.CurrentWindow(cx) != nil {
			desktop.ToggleFullScreen(cx)
		}
	}
	s.bringToFrontFn = func() {
		if s.IsClosed() || desktop.CurrentWindow(cx) == nil {
			return
		}
		desktop.SetWindowLevel(cx, desktop.WindowLevelFloating)
		if !s.topmostSig.GetUntracked() {
			desktop.SetWindowLevel(cx, desktop.WindowLevelNormal)
		}
	}

	// Bind 9-zone resize callback on this window's scope.
	s.resizeFn = func() {
		win := desktop.CurrentWindow(cx)
		if win == nil {
			return
		}
		cur := desktop.Frame(cx)
		displays := cx.Env().Displays()
		var target desktop.WindowFrame
		if cur.Width > 0 && cur.Height > 0 {
			target = ResizeAndCentreExistingWindow(cur, s.ctx.PreferredSize, displays)
		} else {
			target = ResizeAndCentreNewWindow(s.ctx.PreferredSize, displays)
		}
		desktop.SetWindowFrame(cx, target)
	}

	// Keep window titlebar hidden and draw custom QuickLook chrome cleanly.
	desktop.SetTitleBar(cx, desktop.TitleBarHidden, 34)
	desktop.SetWindowControls(cx, desktop.WindowControls{Hidden: true})

	// Synchronize locale, material, topmost level, and resize constraints reactively.
	cx.Effect(func() {
		cfg := s.ctrl.cfgSig.Get()
		cx.SetLocale(cfg.Language)
		if desktop.ShowsMaterial(cx) {
			desktop.SetWindowMaterial(cx, windowMaterialForConfig(cfg))
		}
	})

	cx.Effect(func() {
		if s.topmostSig.Get() {
			desktop.SetWindowLevel(cx, desktop.WindowLevelFloating)
		} else {
			desktop.SetWindowLevel(cx, desktop.WindowLevelNormal)
		}
	})

	cx.Effect(func() {
		canResize := s.ctx.CanResize.Get()
		desktop.SetWindowConstraints(cx, desktop.WindowConstraints{
			MinWidth:  MinWindowWidth,
			MinHeight: MinWindowHeight,
			NoResize:  !canResize,
		})
	})

	// Close on lost focus when configured and unpinned (following window active->inactive transition).
	prevActive := false
	cx.Effect(func() {
		lc := cx.Env().Lifecycle()
		cfg := s.ctrl.cfgSig.Get()
		if lc == core.LifecycleActive {
			prevActive = true
			return
		}
		if prevActive && cfg.CloseOnLostFocus && !s.pinnedSig.GetUntracked() {
			onClose()
		}
	})

	windowHover := core.NewSignal(cx, false)
	titleHover := core.NewSignal(cx, false)

	openDefault := func() {
		p := s.pathSig.GetUntracked()
		if p == "" {
			return
		}
		_ = s.ctrl.sys.OpenDefault(p)
		if !s.pinnedSig.GetUntracked() {
			onClose()
		}
	}

	copyPath := func() {
		p := s.pathSig.GetUntracked()
		if p == "" {
			return
		}
		cx.SetClipboard(sys.FormatCopyPath(p))
		s.ctx.Toast(ui.ToastSuccess, locales.For(cx).WinMoreCopySuccess())
	}

	// Window keyboard shortcuts inside the preview window itself.
	cx.OnKey("escape", onClose)
	cx.OnKey("f5", s.Reload)
	cx.OnKey("mod+r", s.Reload)
	cx.OnKey("f11", func() { desktop.ToggleFullScreen(cx) })
	cx.OnKey("mod+shift+c", copyPath)

	// Custom 34pt caption bar matching QuickLook's action order.
	noDrag := func(child core.Node) core.Node {
		return core.View{
			Desktop:  core.DesktopViewProps{WindowDrag: core.WindowDragOff},
			Children: []core.Node{child},
		}
	}

	topmostBtn := ui.IconButton{
		TestID: "btn-topmost",
		Icon:   lucide.Pin,
		LabelFunc: func() string {
			return locales.For(cx).WinStayTop()
		},
		Size:     26,
		Selected: s.topmostSig.Get,
		OnPress:  func() { s.topmostSig.Set(!s.topmostSig.GetUntracked()) },
	}

	pinBtn := ui.IconButton{
		TestID: "btn-pin",
		Icon: func() *core.SVG {
			if s.pinnedSig.Get() {
				return lucide.Lock()
			}
			return lucide.LockOpen()
		},
		LabelFunc: func() string {
			return locales.For(cx).WinPreventClosing()
		},
		Size:     26,
		Selected: s.pinnedSig.Get,
		OnPress: func() {
			if !s.pinnedSig.GetUntracked() {
				s.ctrl.DetachToPinned(s)
				return
			}
			if s.ctrl.cfg.Get().CloseOnLostFocus {
				s.ctx.Toast(ui.ToastInfo, locales.For(cx).WinCantPreventClosing())
				onClose()
				return
			}
			s.ctrl.UnpinSession(s)
		},
	}

	moreMenu := noDrag(ui.Menu{
		TestID: "btn-more",
		Label:  locales.For(cx).WinMore(),
		Trigger: ui.Button{
			Variant: ui.ButtonGhost,
			Size:    ui.SizeSmall,
			Label:   locales.For(cx).WinMore(),
			Style:   core.Style{Width: core.Pt(26), Height: core.Pt(26), Padding: core.All(0)},
		},
		Face: core.View{
			Style: core.Style{
				Width:          core.Pt(26),
				Height:         core.Pt(26),
				CornerRadius:   ui.RadiusControl,
				AlignItems:     core.AlignCenter,
				JustifyContent: core.JustifyCenter,
			},
			Children: []core.Node{ui.IconOf(lucide.Ellipsis, 15, colors.Text)},
		},
		Children: []core.Node{
			core.Dynamic(func(cx *core.Scope) core.Node {
				tr := locales.For(cx)
				curPath := s.pathSig.Get()
				curPluginID := s.pluginSig.Get()
				pluginItems := s.ctx.MoreItems.Get()

				var menuNodes []core.Node
				for _, act := range pluginItems {
					a := act
					if a.Separator {
						menuNodes = append(menuNodes, ui.MenuSeparator{})
						continue
					}
					menuNodes = append(menuNodes, ui.MenuItem{
						TestID:   a.ID,
						Text:     a.Label,
						Icon:     a.Icon,
						Shortcut: a.Shortcut,
						Checked:  a.Checked,
						OnSelect: a.OnSelect,
					})
				}

				// Collect extended items from plugins that implement ExtendedMenuProvider.
				activePlug := s.ctrl.registry.FindByID(curPluginID)
				extActions := s.ctrl.registry.CollectExtendedMenuItems(cx, activePlug, curPath, s.ctx)
				if len(extActions) > 0 {
					if len(menuNodes) > 0 {
						menuNodes = append(menuNodes, ui.MenuSeparator{})
					}
					for _, act := range extActions {
						a := act
						menuNodes = append(menuNodes, ui.MenuItem{
							TestID:   a.ID,
							Text:     a.Label,
							Icon:     a.Icon,
							Shortcut: a.Shortcut,
							Checked:  a.Checked,
							OnSelect: a.OnSelect,
						})
					}
				}

				if len(menuNodes) > 0 {
					menuNodes = append(menuNodes, ui.MenuSeparator{})
				}
				menuNodes = append(menuNodes,
					ui.MenuItem{
						TestID:   "menu-copy-path",
						Text:     tr.WinMoreCopyPath(),
						Icon:     lucide.Copy,
						Shortcut: "mod+shift+c",
						OnSelect: copyPath,
					},
					ui.MenuItem{
						TestID:   "menu-settings",
						Text:     tr.WinMoreSettings(),
						Icon:     lucide.Settings,
						OnSelect: func() { s.ctrl.OpenSettings() },
					},
				)
				return core.Group(menuNodes)
			}),
		},
	})

	reloadBtn := core.Show{
		When: func() bool { return s.ctrl.cfgSig.Get().ShowReload },
		Then: func(cx *core.Scope) core.Node {
			return noDrag(pluginui.IconButton{
				TestID:    "btn-reload",
				Icon:      lucide.RotateCw,
				LabelFunc: func() string { return locales.For(cx).WinReload() },
				Size:      26,
				OnPress:   s.Reload,
			})
		},
	}

	openBtn := noDrag(ui.IconButton{
		TestID: "btn-open",
		Icon:   lucide.ExternalLink,
		LabelFunc: func() string {
			tr := locales.For(cx)
			p := s.pathSig.Get()
			isDir := s.isDirSig.Get()
			name := viewer.DisplayName(p)
			if isDir {
				return tr.WinBrowseFolder(name)
			}
			ext := strings.ToLower(filepath.Ext(p))
			if ext == ".exe" || ext == ".bat" || ext == ".cmd" || ext == ".msi" {
				return tr.WinRun(name)
			}
			if appName := s.ctrl.sys.GetAssocAppName(p); appName != "" {
				return tr.WinOpenWithApp(appName)
			}
			return tr.WinOpen(name)
		},
		Size:    26,
		OnPress: openDefault,
	})

	openWithBtn := core.Show{
		When: func() bool {
			p := s.pathSig.Get()
			return !s.isDirSig.Get() && !strings.HasPrefix(p, "::")
		},
		Then: func(cx *core.Scope) core.Node {
			return noDrag(pluginui.IconButton{
				TestID: "btn-open-with",
				Icon:   lucide.FolderOpenDot,
				LabelFunc: func() string {
					return locales.For(cx).WinOpenWith()
				},
				Size: 26,
				OnPress: func() {
					p := s.pathSig.GetUntracked()
					if p != "" {
						_ = s.ctrl.sys.OpenWithDialog(p)
					}
				},
			})
		},
	}

	revealBtn := noDrag(ui.IconButton{
		TestID: "btn-reveal",
		Icon:   lucide.FolderSearch,
		LabelFunc: func() string {
			return locales.For(cx).WinRevealInExplorer()
		},
		Size: 26,
		OnPress: func() {
			p := s.pathSig.GetUntracked()
			if p != "" {
				desktop.RevealFile(cx, p, nil)
			}
		},
	})

	maxBtn := core.Show{
		When: func() bool { return s.ctx.CanResize.Get() },
		Then: func(cx *core.Scope) core.Node {
			return noDrag(pluginui.IconButton{
				TestID: "btn-maximize",
				Icon: func() *core.SVG {
					if desktop.Maximized(cx) {
						return lucide.Minimize2()
					}
					return lucide.Maximize2()
				},
				LabelFunc: func() string {
					tr := locales.For(cx)
					if desktop.Maximized(cx) {
						return tr.WinRestore()
					}
					return tr.WinMaximize()
				},
				Size:    26,
				OnPress: func() { desktop.ToggleMaximize(cx) },
			})
		},
	}

	closeBtn := noDrag(ui.IconButton{
		TestID: "btn-close",
		Icon:   lucide.X,
		LabelFunc: func() string {
			return locales.For(cx).WinClose()
		},
		Size:    26,
		OnPress: onClose,
	})

	dragTitle := core.View{
		Desktop: core.DesktopViewProps{WindowDrag: core.WindowDragOn},
		Style: core.Style{
			FlexGrow:       1,
			FlexShrink:     1,
			MinWidth:       core.Pt(0),
			Height:         core.Pct(100),
			AlignItems:     core.AlignCenter,
			JustifyContent: core.JustifyCenter,
			Padding:        core.Symmetric(0, 8),
		},
		Children: []core.Node{
			core.Text{
				TestID: "viewer-title",
				ContentFunc: func() string {
					if t := s.ctx.Title.Get(); t != "" {
						return t
					}
					return viewer.DisplayName(s.pathSig.Get())
				},
				MaxLines:  1,
				Style:     core.Style{FontSize: 12, FontWeight: ui.Semibold, TextAlign: core.TextAlignCenter},
				StyleFunc: ui.Colored(colors.Text),
			},
		},
	}

	titleBar := core.View{
		TestID:       "viewer-titlebar",
		OnMouseEnter: func(core.MouseEvent) { titleHover.Set(true) },
		OnMouseLeave: func(core.MouseEvent) { titleHover.Set(false) },
		StyleFunc: func() core.Style {
			overlap := s.ctx.TitlebarOverlap.Get()
			autoHide := s.ctx.TitlebarAutoHide.Get()
			showBar := !autoHide || titleHover.Get() || windowHover.Get()

			st := core.Style{
				Direction:   core.Row,
				AlignItems:  core.AlignCenter,
				Gap:         2,
				Height:      core.Pt(34),
				Padding:     core.Symmetric(0, 8),
				FlexShrink:  0,
				Borders:     core.Edges{Bottom: 1},
				Background:  ui.Alpha(colors.Surface(), 0.78),
				BorderColor: colors.Line(),
			}
			if overlap {
				st.Position = core.Absolute
				st.Top = core.Pt(0)
				st.Left = core.Pt(0)
				st.Right = core.Pt(0)
				st.ZIndex = 10
				st.Background = ui.Alpha(colors.Surface(), 0.85)
			}
			if !showBar {
				st.Opacity = core.Alpha(0)
			} else {
				st.Opacity = core.Alpha(1)
			}
			return st
		},
		Children: []core.Node{
			noDrag(topmostBtn),
			noDrag(pinBtn),
			dragTitle,
			moreMenu,
			reloadBtn,
			openBtn,
			openWithBtn,
			revealBtn,
			maxBtn,
			closeBtn,
		},
	}

	contentBody := core.Dynamic(func(cx *core.Scope) core.Node {
		curPath := s.viewPathSig.Get()
		pluginID := s.viewPluginSig.Get()
		blocked := s.viewBlocked.Get()
		_ = s.viewSeq.Get()

		if curPath == "" {
			return withLayoutSlotFlush(pluginui.EmptyState(cx, lucide.Search, locales.For(cx).AppName(), locales.For(cx).WinDropHint()))
		}
		if blocked || pluginID == "blocked" {
			return withLayoutSlotFlush(pluginui.EmptyState(cx, lucide.ShieldBan, viewer.DisplayName(curPath), locales.For(cx).WinFileBlocked()))
		}

		plug := s.ctrl.registry.FindByID(pluginID)
		if plug == nil {
			plug = s.ctrl.registry.Default
		}
		if plug == nil {
			plug = infopanel.New()
		}

		return withLayoutSlotFlush(core.Catch{
			Try: func(cx *core.Scope) core.Node {
				if s.ctx.FullWindowDragging.GetUntracked() {
					return core.View{
						Desktop: core.DesktopViewProps{WindowDrag: core.WindowDragOn},
						Style:   core.Style{FlexGrow: 1, FlexShrink: 1, MinWidth: core.Pt(0), MinHeight: core.Pt(0)},
						Children: []core.Node{
							plug.View(cx, curPath, s.ctx),
						},
					}
				}
				return plug.View(cx, curPath, s.ctx)
			},
			Fallback: func(cx *core.Scope, err error, _ func()) core.Node {
				msg := locales.For(cx).WinPreviewFailed(viewer.DisplayName(curPath))
				if err != nil {
					msg += ": " + err.Error()
				}
				return pluginui.Fill(
					core.View{
						Style:     core.Style{Padding: core.Symmetric(8, 12), Borders: core.Edges{Bottom: 1}, FlexShrink: 0},
						StyleFunc: func() core.Style { return core.Style{Background: ui.TintsOf(cx).Danger(), BorderColor: colors.Line()} },
						Children:  []core.Node{ui.Text(msg, 12, ui.Medium, colors.Danger)},
					},
					infopanel.New().View(cx, curPath, s.ctx),
				)
			},
		})
	})

	busyOverlay := core.Show{
		When: s.ctx.IsBusy.Get,
		Then: func(cx *core.Scope) core.Node {
			return core.View{
				PointerEvents: core.PointerEventsBoxNone,
				Style: core.Style{
					Position:       core.Absolute,
					Top:            core.Pt(0),
					Left:           core.Pt(0),
					Right:          core.Pt(0),
					Bottom:         core.Pt(0),
					AlignItems:     core.AlignCenter,
					JustifyContent: core.JustifyCenter,
				},
				Children: []core.Node{
					core.View{
						Style: core.Style{
							Padding:      core.All(14),
							CornerRadius: 12,
							Borders:      core.All(1),
							AlignItems:   core.AlignCenter,
							Gap:          8,
						},
						StyleFunc: func() core.Style {
							return core.Style{
								Background:  ui.Alpha(colors.Surface(), 0.90),
								BorderColor: colors.Line(),
							}
						},
						Children: []core.Node{
							ui.Spinner{Size: 22},
							ui.Text(locales.For(cx).WinLoading(), 12, ui.Medium, colors.Muted),
						},
					},
				},
			}
		},
	}

	dropOverlay := core.Show{
		When: s.dropHover.Get,
		Then: func(cx *core.Scope) core.Node {
			return core.View{
				PointerEvents: core.PointerEventsNone,
				Style: core.Style{
					Position:       core.Absolute,
					Top:            core.Pt(10),
					Left:           core.Pt(10),
					Right:          core.Pt(10),
					Bottom:         core.Pt(10),
					CornerRadius:   12,
					Borders:        core.All(2),
					AlignItems:     core.AlignCenter,
					JustifyContent: core.JustifyCenter,
				},
				StyleFunc: func() core.Style {
					return core.Style{
						Background:  ui.Alpha(colors.Accent(), 0.12),
						BorderColor: colors.Accent(),
					}
				},
				Children: []core.Node{
					ui.Text(locales.For(cx).WinDropHint(), 14, ui.Semibold, colors.Accent),
				},
			}
		},
	}

	return core.View{
		TestID:          "viewer-root",
		OnMouseEnter:    func(core.MouseEvent) { windowHover.Set(true) },
		OnMouseLeave:    func(core.MouseEvent) { windowHover.Set(false) },
		OnFileDragEnter: func() { s.dropHover.Set(true) },
		OnFileDragLeave: func() { s.dropHover.Set(false) },
		OnFileDrop: func(uris []string) {
			s.dropHover.Set(false)
			for _, raw := range uris {
				if p := parseDroppedPath(raw); p != "" {
					s.Load(p, "")
					return
				}
			}
		},
		Style: core.Style{
			FlexGrow:   1,
			FlexShrink: 1,
			MinWidth:   core.Pt(0),
			MinHeight:  core.Pt(0),
			Overflow:   core.Hidden,
		},
		StyleFunc: func() core.Style {
			cfg := s.ctrl.cfgSig.Get()
			if !cfg.UseTransparency || !desktop.ShowsMaterial(cx) {
				return core.Style{Background: colors.Background()}
			}
			// Keep a subtle tint so contrast remains crisp over Acrylic/Mica.
			if cx.Dark() {
				return core.Style{Background: ui.Alpha(colors.Background(), 0.65)}
			}
			return core.Style{Background: ui.Alpha(colors.Background(), 0.72)}
		},
		Children: []core.Node{
			titleBar,
			pluginui.Fill(contentBody),
			busyOverlay,
			dropOverlay,
			ui.Toasts{Toaster: toaster, Placement: ui.ToastBottom},
		},
	}
}

// parseDroppedPath resolves dropped file:// URIs or local paths for both files and directories.
func parseDroppedPath(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if strings.HasPrefix(strings.ToLower(raw), "file:") {
		u, err := url.Parse(raw)
		if err != nil {
			return ""
		}
		p, err := url.PathUnescape(u.Path)
		if err != nil {
			p = u.Path
		}
		if u.Host != "" && !strings.EqualFold(u.Host, "localhost") {
			return fmt.Sprintf(`\\%s%s`, u.Host, strings.ReplaceAll(p, "/", `\`))
		}
		if runtime.GOOS == "windows" && len(p) >= 3 && p[0] == '/' && p[2] == ':' {
			p = p[1:]
		}
		return filepath.FromSlash(p)
	}
	return filepath.Clean(raw)
}

// laidOutFlushSlots overwrites stale pointers left in NSGo's Tree.laidOut[:0] backing array
// when switching away from plugins that register OnLayout handlers (e.g. imageviewer + camera.View = 3).
const laidOutFlushSlots = 6

func noopLayout(core.Bounds) {}

func withLayoutSlotFlush(main core.Node) core.Node {
	nodes := make(core.Group, 1+laidOutFlushSlots)
	nodes[0] = main
	for i := 1; i < len(nodes); i++ {
		nodes[i] = core.View{
			PointerEvents: core.PointerEventsNone,
			Style: core.Style{
				Position: core.Absolute,
				Width:    core.Pt(0),
				Height:   core.Pt(0),
			},
			OnLayout: noopLayout,
		}
	}
	return nodes
}
