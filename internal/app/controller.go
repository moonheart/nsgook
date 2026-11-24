package app

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"
	"slices"
	"sync"
	"time"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/desktop"

	"nsgook/assets"
	"nsgook/internal/config"
	"nsgook/internal/plugins"
	"nsgook/internal/sys"
	"nsgook/internal/viewer"
	"nsgook/locales"
)

// AppVersion is the version shown in the tray. Release builds set it from the
// tag with -ldflags "-X nsgook/internal/app.AppVersion=...".
var AppVersion = "0.1.1"

// AppOptions configures Controller startup for production or isolated headless tests.
type AppOptions struct {
	Sys            sys.System
	DisableHooks   bool
	InitialFiles   []string
	InitialTopmost bool
	InitialPin     bool
	ConfigPath     string
	OnReady        func(*Controller)
}

// PreviewOptions customizes how a Preview call opens or updates a viewer window.
type PreviewOptions struct {
	ForcePluginID string
	Topmost       bool
	Pinned        bool
	NewWindow     bool
}

// Controller coordinates configuration, tray icon, global keyboard hooks,
// selection focus monitoring, viewer windows, and the singleton settings window.
type Controller struct {
	app           *core.Scope
	cfg           *config.Store
	cfgSig        *core.Signal[config.Config]
	sys           sys.System
	registry      *viewer.Registry
	dispatcher    *sys.Dispatcher
	focusMon      *sys.FocusMonitor
	launchAtLogin *core.Signal[bool]

	active       *ViewerSession
	hidden       *ViewerSession
	pinned       []*ViewerSession
	settingsWin  *desktop.Window
	tray         *desktop.Tray
	shuttingDown bool

	freeMu    sync.Mutex
	freeTimer *time.Timer
}

// Start initializes NSGook on the NSGo engine root scope and returns its Controller.
func Start(app *core.Scope, opts AppOptions) *Controller {
	cfgPath := opts.ConfigPath
	if cfgPath == "" {
		cfgPath = config.DefaultConfigPath(app.DataDir())
	}
	store, _ := config.NewStore(cfgPath)

	s := opts.Sys
	if s == nil {
		s = sys.NewNativeSystem()
	}

	c := &Controller{
		app:           app,
		cfg:           store,
		cfgSig:        core.NewSignalFunc(app, store.Get(), func(_, _ config.Config) bool { return false }),
		sys:           s,
		registry:      plugins.NewDefaultRegistry(),
		focusMon:      sys.NewFocusMonitor(s),
		launchAtLogin: core.NewSignal(app, false),
	}
	c.dispatcher = &sys.Dispatcher{
		IsValidTarget: func() bool {
			return c.sys.GetFocusedWindowType() != sys.FocusedInvalid || c.sys.IsForegroundOwnedBySelf()
		},
		AutoCloseHolding: func() bool {
			return c.cfg.Get().AutoCloseHolding
		},
		OnAction: func(act sys.Action) {
			app.Post(func() { c.handleGlobalAction(act) })
		},
	}

	store.SetOnChange(func(next config.Config) {
		app.Post(func() {
			c.cfgSig.Set(next)
		})
	})

	desktop.SetShutdownMode(app, desktop.ShutdownOnExplicit)

	// Keep theme appearance and locale synchronized with config reactively.
	app.Effect(func() {
		cfg := c.cfgSig.Get()
		switch cfg.Theme {
		case config.ThemeLight:
			app.SetAppearance(core.AppearanceLight)
		case config.ThemeDark:
			app.SetAppearance(core.AppearanceDark)
		default:
			app.SetAppearance(core.AppearanceSystem)
		}
		app.SetLocale(cfg.Language)
	})

	// Query system autostart status.
	desktop.LaunchAtLogin(app, func(on bool) {
		c.launchAtLogin.Set(on)
	})

	// Handle OS file associations, drops on dock icon, or single-instance second invocations.
	desktop.OnOpenFiles(app, func(paths []string) {
		for i, p := range paths {
			if p == "" {
				continue
			}
			c.Preview(p, PreviewOptions{NewWindow: i > 0})
		}
	})
	desktop.OnReopen(app, func() {
		c.OpenSettings()
	})

	// Manage system tray dynamically when ShowTrayIcon changes.
	app.Effect(func() {
		showTray := c.cfgSig.Get().ShowTrayIcon
		if showTray && c.tray == nil {
			c.installTray()
		}
	})

	app.Cleanup(func() {
		c.shuttingDown = true
		c.cancelFreeOSMemory()
		c.focusMon.Stop()
	})

	// Install global Win32 low-level keyboard hook & active foreground monitor unless disabled in tests.
	if !opts.DisableHooks {
		stopHook := s.StartHook(
			func(vk uint32, down bool, mods uint32) {
				c.dispatcher.HandleKey(vk, down, mods)
			},
			func() {
				c.dispatcher.OnForegroundChanged()
			},
		)
		app.Cleanup(func() {
			stopHook()
		})
	}

	// Preview any files specified on command line launch.
	for i, p := range opts.InitialFiles {
		if p == "" {
			continue
		}
		c.Preview(p, PreviewOptions{
			Topmost:   opts.InitialTopmost,
			Pinned:    opts.InitialPin,
			NewWindow: i > 0,
		})
	}

	if opts.OnReady != nil {
		opts.OnReady(c)
	}
	return c
}

func (c *Controller) handleGlobalAction(act sys.Action) {
	switch act {
	case sys.ActionToggle:
		if c.active != nil && !c.active.IsClosed() {
			c.CloseActive()
			return
		}
		c.ToggleFromSelection()
	case sys.ActionClose:
		c.CloseActive()
	case sys.ActionSwitch:
		if c.active == nil || c.active.IsClosed() {
			return
		}
		if sel := c.sys.GetCurrentSelection(); sel != "" && sel != c.active.Path() {
			c.active.Load(sel, "")
		}
	case sys.ActionRunAndClose:
		if c.active != nil && !c.active.IsClosed() {
			p := c.active.Path()
			c.CloseActive()
			if p != "" {
				_ = c.sys.OpenDefault(p)
			}
		}
	case sys.ActionReload:
		if c.active != nil && !c.active.IsClosed() {
			c.active.Reload()
		}
	case sys.ActionFullscreen:
		if c.active != nil && !c.active.IsClosed() && c.active.win != nil {
			c.active.RequestFullscreenToggle()
		}
	}
}

func (c *Controller) installTray() {
	c.app.Run(func() {
		c.tray = desktop.NewTray(c.app, desktop.TrayOptions{
			Icon:    assets.AppIconPNG,
			Tooltip: locales.For(c.app).TrayTooltip(AppVersion),
			OnClick: func() {
				if c.active != nil && !c.active.IsClosed() {
					c.active.Focus()
					return
				}
				c.OpenSettings()
			},
			Menu: func() []desktop.MenuItem {
				tr := locales.For(c.app)
				cfg := c.cfg.Get()
				return []desktop.MenuItem{
					{
						Label: tr.TrayPreviewFile(),
						OnSelect: func() {
							c.app.PickFiles(core.PickFilesOptions{Multiple: false}, func(files []core.File, _ error) {
								if len(files) > 0 && files[0].Path != "" {
									c.Preview(files[0].Path, PreviewOptions{})
								}
							})
						},
					},
					{
						Label:    tr.TrayOpenSettings(),
						OnSelect: func() { c.OpenSettings() },
					},
					{
						Label: tr.TrayOpenDataFolder(),
						OnSelect: func() {
							dir := c.cfg.Dir()
							_ = os.MkdirAll(dir, 0o755)
							_ = c.sys.OpenDefault(dir)
						},
					},
					{Separator: true},
					{
						Label:     tr.TrayRunAtStartup(),
						Checkable: true,
						Checked:   c.launchAtLogin.GetUntracked(),
						OnSelect: func() {
							next := !c.launchAtLogin.GetUntracked()
							c.launchAtLogin.Set(next)
							desktop.SetLaunchAtLogin(c.app, next)
						},
					},
					{
						Label:     tr.TrayCloseOnLostFocus(),
						Checkable: true,
						Checked:   cfg.CloseOnLostFocus,
						OnSelect: func() {
							_ = c.cfg.Update(func(c2 *config.Config) {
								c2.CloseOnLostFocus = !c2.CloseOnLostFocus
							})
						},
					},
					{Separator: true},
					{
						Label:    tr.TrayRestart(),
						OnSelect: func() { desktop.Restart(c.app) },
					},
					{
						Label:    tr.TrayQuit(),
						OnSelect: func() { c.app.Quit() },
					},
				}
			},
		})
	})
}

// ToggleFromSelection inspects the active file manager selection and opens a preview if present.
func (c *Controller) ToggleFromSelection() *ViewerSession {
	p := c.sys.GetCurrentSelection()
	if p == "" {
		return nil
	}
	return c.Preview(p, PreviewOptions{})
}

// Preview displays path in the active unpinned ViewerSession (or creates a new window when pinned/requested).
func (c *Controller) Preview(path string, opts PreviewOptions) *ViewerSession {
	if path == "" {
		return nil
	}
	c.cancelFreeOSMemory()
	clean := filepath.Clean(path)
	if !opts.NewWindow && !opts.Pinned && c.active != nil && !c.active.IsClosed() {
		if opts.Topmost {
			c.active.topmostSig.Set(true)
		}
		c.active.Load(clean, opts.ForcePluginID)
		c.showWindow(c.active)
		return c.active
	}
	if !opts.NewWindow && !opts.Pinned && c.hidden != nil && !c.hidden.IsClosed() {
		s := c.hidden
		c.hidden = nil
		c.active = s
		s.topmostSig.Set(c.cfg.Get().Topmost || opts.Topmost)
		s.Load(clean, opts.ForcePluginID)
		c.showWindow(s)
		c.focusMon.Start(clean, func(next string) {
			c.app.Post(func() {
				if c.active == s && !s.IsClosed() && next != s.Path() {
					s.Load(next, "")
				}
			})
		})
		return s
	}

	s := c.openSessionWindow(clean, opts)
	if opts.Pinned {
		c.pinned = append(c.pinned, s)
	} else {
		c.active = s
		c.focusMon.Start(clean, func(next string) {
			c.app.Post(func() {
				if c.active == s && !s.IsClosed() && next != s.Path() {
					s.Load(next, "")
				}
			})
		})
	}
	return s
}

func (c *Controller) showWindow(s *ViewerSession) {
	if s == nil || s.win == nil || s.IsClosed() {
		return
	}
	focus := c.cfg.Get().FocusWindowOnOpen
	if focus {
		s.win.Focus()
	} else {
		s.win.Show()
	}
	s.BringToFront()
	if s.scope != nil && !s.scope.Disposed() {
		s.scope.Run(func() {
			s.scope.AfterFunc(40*time.Millisecond, func() {
				if !s.IsClosed() && (c.active == s || s.Pinned()) {
					s.BringToFront()
				}
			})
		})
	}
}

// CloseActive closes or hides the currently unpinned active preview window if open.
func (c *Controller) CloseActive() {
	if c.active == nil {
		return
	}
	s := c.active
	c.active = nil
	c.focusMon.Stop()
	if !c.shuttingDown && s.win != nil && !s.IsClosed() && !s.Pinned() {
		s.Unload()
		s.win.Hide()
		if c.hidden != nil && c.hidden != s {
			c.hidden.Close()
		}
		c.hidden = s
		c.scheduleFreeOSMemory()
		return
	}
	s.Close()
	c.scheduleFreeOSMemory()
}

func (c *Controller) cancelFreeOSMemory() {
	c.freeMu.Lock()
	if c.freeTimer != nil {
		c.freeTimer.Stop()
		c.freeTimer = nil
	}
	c.freeMu.Unlock()
}

func (c *Controller) scheduleFreeOSMemory() {
	if c.shuttingDown {
		return
	}
	c.freeMu.Lock()
	if c.freeTimer != nil {
		c.freeTimer.Stop()
	}
	var t *time.Timer
	t = time.AfterFunc(500*time.Millisecond, func() {
		c.freeMu.Lock()
		active := c.freeTimer == t
		if active {
			c.freeTimer = nil
		}
		c.freeMu.Unlock()
		if active {
			debug.FreeOSMemory()
		}
	})
	c.freeTimer = t
	c.freeMu.Unlock()
}

// DetachToPinned promotes s from the active unpinned slot into the pinned window list.
func (c *Controller) DetachToPinned(s *ViewerSession) {
	if s == nil {
		return
	}
	s.pinnedSig.Set(true)
	if c.active == s {
		c.active = nil
		c.focusMon.Stop()
	}
	if !slices.Contains(c.pinned, s) {
		c.pinned = append(c.pinned, s)
	}
}

// UnpinSession closes a pinned session or converts it if no active session exists.
func (c *Controller) UnpinSession(s *ViewerSession) {
	if s == nil {
		return
	}
	s.pinnedSig.Set(false)
	c.pinned = slices.DeleteFunc(c.pinned, func(item *ViewerSession) bool { return item == s })
	if c.active == nil || c.active.IsClosed() {
		c.active = s
	} else {
		s.Close()
	}
}

// RemoveSession removes s cleanly when its window closes.
func (c *Controller) RemoveSession(s *ViewerSession) {
	if c.active == s {
		c.active = nil
		c.focusMon.Stop()
	}
	if c.hidden == s {
		c.hidden = nil
	}
	c.pinned = slices.DeleteFunc(c.pinned, func(item *ViewerSession) bool { return item == s })
}

// Sessions returns all open viewer sessions (active followed by pinned).
func (c *Controller) Sessions() []*ViewerSession {
	var out []*ViewerSession
	if c.active != nil && !c.active.IsClosed() {
		out = append(out, c.active)
	}
	for _, p := range c.pinned {
		if p != nil && !p.IsClosed() {
			out = append(out, p)
		}
	}
	return out
}

func (c *Controller) ActiveSession() *ViewerSession             { return c.active }
func (c *Controller) Config() *config.Store                     { return c.cfg }
func (c *Controller) ConfigSignal() *core.Signal[config.Config] { return c.cfgSig }
func (c *Controller) Registry() *viewer.Registry                { return c.registry }
func (c *Controller) Sys() sys.System                           { return c.sys }
func (c *Controller) LaunchAtLoginSignal() *core.Signal[bool]   { return c.launchAtLogin }

// openSessionWindow creates the NSGo desktop.Window hosting a ViewerSession.
func (c *Controller) openSessionWindow(initialPath string, opts PreviewOptions) *ViewerSession {
	var sess *ViewerSession
	// Own viewer windows directly under the persistent root app scope rather than any transient menu/event scope.
	c.app.Run(func() {
		cfg := c.cfg.Get()
		mat := windowMaterialForConfig(cfg)

		sess = NewViewerSession(c, c.app, initialPath, opts)
		displays := c.app.Env().Displays()
		initFrame := ResizeAndCentreNewWindow(sess.ctx.PreferredSize, displays)

		title := sess.ctx.Title.GetUntracked()
		if title == "" {
			title = fmt.Sprintf("NSGook - %s", viewer.DisplayName(initialPath))
		}

		sess.scope.Run(func() {
			onClose := func() {
				if !c.shuttingDown && c.active == sess && !sess.Pinned() {
					c.CloseActive()
					return
				}
				sess.Close()
			}
			w := desktop.OpenWindow(sess.scope, desktop.WindowOptions{
				Title:       title,
				Width:       initFrame.Width,
				Height:      initFrame.Height,
				Frame:       initFrame,
				Material:    mat,
				SkipTaskbar: !cfg.ShowInTaskbar,
				Constraints: desktop.WindowConstraints{
					MinWidth:  MinWindowWidth,
					MinHeight: MinWindowHeight,
					NoResize:  !sess.ctx.CanResize.GetUntracked(),
				},
				Hidden: true,
			}, func(cx *core.Scope) core.Node {
				win := desktop.CurrentWindow(cx)
				if win != nil {
					win.OnClosing(cx, func(ev *desktop.WindowClosing) {
						if !c.shuttingDown && c.active == sess && !sess.Pinned() {
							ev.Cancel()
							c.CloseActive()
							return
						}
						sess.markClosed()
						c.RemoveSession(sess)
						c.app.Post(func() { sess.disposeScope() })
					})
				}
				return RenderViewerSurface(cx, sess, onClose)
			})
			sess.win = w
		})
		c.showWindow(sess)
	})
	return sess
}

func windowMaterialForConfig(cfg config.Config) desktop.WindowMaterial {
	if !cfg.UseTransparency {
		return desktop.WindowMaterialNone
	}
	switch cfg.WindowBackdrop {
	case config.BackdropNone:
		return desktop.WindowMaterialNone
	case config.BackdropMica:
		return desktop.WindowMaterialMica
	case config.BackdropTabbed:
		return desktop.WindowMaterialTabbed
	case config.BackdropAuto, config.BackdropAcrylic:
		return desktop.WindowMaterialAcrylic
	default:
		return desktop.WindowMaterialAcrylic
	}
}
