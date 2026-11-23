package app

import (
	"fmt"
	"strconv"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/desktop"
	"github.com/nsgo-dev/nsgo/icons/lucide"
	"github.com/nsgo-dev/nsgo/ui"

	"nsgook/internal/config"
	"nsgook/locales"
)

// IsSettingsOpen reports whether the singleton Settings window is currently open.
func (c *Controller) IsSettingsOpen() bool {
	return c.settingsWin != nil && !c.settingsWin.Closed() && c.settingsWin.Visible()
}

// OpenSettings brings up NSGook's singleton Settings window (keyed by "settings").
func (c *Controller) OpenSettings() {
	if c.settingsWin != nil && !c.settingsWin.Closed() {
		c.settingsWin.Focus()
		return
	}
	// Detach ownership from any transient caller scope (e.g. More popup menu or preview window)
	// so closing that popup never disposes and closes the Settings window.
	c.app.Run(func() {
		tr := locales.For(c.app)
		c.settingsWin = desktop.OpenWindow(c.app, desktop.WindowOptions{
			Key:    "settings",
			Title:  fmt.Sprintf("%s - %s", tr.AppName(), tr.SettingsTitle()),
			Width:  780,
			Height: 540,
			Constraints: desktop.WindowConstraints{
				MinWidth:  560,
				MinHeight: 400,
			},
		}, func(cx *core.Scope) core.Node {
			win := desktop.CurrentWindow(cx)
			if win != nil {
				win.OnClosing(cx, func(ev *desktop.WindowClosing) {
					if !c.shuttingDown {
						ev.Cancel()
						win.Hide()
					}
				})
			}
			return RenderSettingsSurface(cx, c, func() {
				if win != nil {
					win.Hide()
				}
			})
		})
		if c.settingsWin != nil {
			c.settingsWin.Focus()
		}
	})
}

// RenderSettingsSurface builds the multi-category searchable Settings UI backed directly by ctrl.Config().
func RenderSettingsSurface(cx *core.Scope, ctrl *Controller, onClose func()) core.Node {
	colors := ui.ColorsOf(cx)
	cfgSig := ctrl.cfgSig
	updateCfg := func(fn func(*config.Config)) {
		_ = ctrl.cfg.Update(fn)
	}

	cx.Effect(func() {
		cx.SetLocale(cfgSig.Get().Language)
	})

	catSig := core.NewSignal(cx, "general")
	querySig := core.NewSignal(cx, "")
	allowText := core.NewSignal(cx, config.FormatExtensionList(cfgSig.Get().ExtensionAllowlist))
	blockText := core.NewSignal(cx, config.FormatExtensionList(cfgSig.Get().ExtensionBlocklist))

	return core.Dynamic(func(cx *core.Scope) core.Node {
		tr := locales.For(cx)

		var pluginRows []ui.SettingsItem
		for _, p := range ctrl.registry.All() {
			plug := p
			pluginRows = append(pluginRows, ui.SettingsItem{
				ID:          "plug-" + plug.ID(),
				Title:       plug.Name(),
				Description: fmt.Sprintf("ID: %s", plug.ID()),
				Keywords:    []string{plug.ID(), plug.Name()},
				Control:     ui.Badge{Text: fmt.Sprintf("P%d", plug.Priority()), Variant: ui.BadgeNeutral},
			})
		}
		if def := ctrl.registry.Default; def != nil {
			pluginRows = append(pluginRows, ui.SettingsItem{
				ID:          "plug-" + def.ID(),
				Title:       def.Name(),
				Description: fmt.Sprintf("ID: %s (Fallback)", def.ID()),
				Keywords:    []string{def.ID(), def.Name(), "default"},
				Control:     ui.Badge{Text: fmt.Sprintf("P%d", def.Priority()), Variant: ui.BadgeAccent},
			})
		}

		categories := []ui.SettingsCategory{
			{
				ID:    "general",
				Title: tr.SettingsCatGeneral(),
				Groups: []ui.SettingsGroup{
					{
						ID:    "startup",
						Title: tr.SettingsGrpStartup(),
						Items: []ui.SettingsItem{
							{
								ID:          "show-tray",
								Title:       tr.SettingsShowTrayIcon(),
								Description: tr.SettingsShowTrayIconDesc(),
								Control: ui.Switch{
									TestID:   "set-show-tray",
									Checked:  func() bool { return cfgSig.Get().ShowTrayIcon },
									OnChange: func(v bool) { updateCfg(func(c *config.Config) { c.ShowTrayIcon = v }) },
								},
							},
							{
								ID:          "run-at-startup",
								Title:       tr.SettingsRunAtStartup(),
								Description: tr.SettingsRunAtStartupDesc(),
								Control: ui.Switch{
									TestID:  "set-startup",
									Checked: ctrl.launchAtLogin.Get,
									OnChange: func(v bool) {
										ctrl.launchAtLogin.Set(v)
										desktop.SetLaunchAtLogin(ctrl.app, v)
									},
								},
							},
						},
					},
					{
						ID:    "behavior",
						Title: tr.SettingsGrpBehavior(),
						Items: []ui.SettingsItem{
							{
								ID:          "close-lost-focus",
								Title:       tr.SettingsCloseOnLostFocus(),
								Description: tr.SettingsCloseOnLostFocusDesc(),
								Control: ui.Switch{
									TestID:   "set-close-lost-focus",
									Checked:  func() bool { return cfgSig.Get().CloseOnLostFocus },
									OnChange: func(v bool) { updateCfg(func(c *config.Config) { c.CloseOnLostFocus = v }) },
								},
							},
							{
								ID:          "auto-close-hold",
								Title:       tr.SettingsAutoCloseHolding(),
								Description: tr.SettingsAutoCloseHoldingDesc(),
								Control: ui.Switch{
									TestID:   "set-auto-close-hold",
									Checked:  func() bool { return cfgSig.Get().AutoCloseHolding },
									OnChange: func(v bool) { updateCfg(func(c *config.Config) { c.AutoCloseHolding = v }) },
								},
							},
							{
								ID:          "focus-on-open",
								Title:       tr.SettingsFocusOnOpen(),
								Description: tr.SettingsFocusOnOpenDesc(),
								Control: ui.Switch{
									TestID:   "set-focus-on-open",
									Checked:  func() bool { return cfgSig.Get().FocusWindowOnOpen },
									OnChange: func(v bool) { updateCfg(func(c *config.Config) { c.FocusWindowOnOpen = v }) },
								},
							},
							{
								ID:          "default-topmost",
								Title:       tr.SettingsTopmost(),
								Description: tr.SettingsTopmostDesc(),
								Control: ui.Switch{
									TestID:   "set-topmost",
									Checked:  func() bool { return cfgSig.Get().Topmost },
									OnChange: func(v bool) { updateCfg(func(c *config.Config) { c.Topmost = v }) },
								},
							},
							{
								ID:          "show-in-taskbar",
								Title:       tr.SettingsShowInTaskbar(),
								Description: tr.SettingsShowInTaskbarDesc(),
								Control: ui.Switch{
									TestID:   "set-taskbar",
									Checked:  func() bool { return cfgSig.Get().ShowInTaskbar },
									OnChange: func(v bool) { updateCfg(func(c *config.Config) { c.ShowInTaskbar = v }) },
								},
							},
							{
								ID:          "show-reload",
								Title:       tr.SettingsShowReload(),
								Description: tr.SettingsShowReloadDesc(),
								Control: ui.Switch{
									TestID:   "set-show-reload",
									Checked:  func() bool { return cfgSig.Get().ShowReload },
									OnChange: func(v bool) { updateCfg(func(c *config.Config) { c.ShowReload = v }) },
								},
							},
							{
								ID:          "auto-reload",
								Title:       tr.SettingsAutoReload(),
								Description: tr.SettingsAutoReloadDesc(),
								Control: ui.Switch{
									TestID:   "set-auto-reload",
									Checked:  func() bool { return cfgSig.Get().AutoReload },
									OnChange: func(v bool) { updateCfg(func(c *config.Config) { c.AutoReload = v }) },
								},
							},
						},
					},
				},
			},
			{
				ID:    "appearance",
				Title: tr.SettingsCatAppearance(),
				Groups: []ui.SettingsGroup{
					{
						ID:    "window-mat",
						Title: tr.SettingsGrpWindow(),
						Items: []ui.SettingsItem{
							{
								ID:          "use-transparency",
								Title:       tr.SettingsUseTransparency(),
								Description: tr.SettingsUseTransparencyDesc(),
								Control: ui.Switch{
									TestID:   "set-transparency",
									Checked:  func() bool { return cfgSig.Get().UseTransparency },
									OnChange: func(v bool) { updateCfg(func(c *config.Config) { c.UseTransparency = v }) },
								},
							},
							{
								ID:          "backdrop-style",
								Title:       tr.SettingsBackdrop(),
								Description: tr.SettingsBackdropDesc(),
								Control: ui.Select[config.BackdropStyle]{
									TestID: "set-backdrop",
									Value:  func() config.BackdropStyle { return cfgSig.Get().WindowBackdrop },
									OnChange: func(v config.BackdropStyle) {
										updateCfg(func(c *config.Config) { c.WindowBackdrop = v })
									},
									Options: []ui.Option[config.BackdropStyle]{
										{Value: config.BackdropAuto, Label: tr.BackdropAuto()},
										{Value: config.BackdropAcrylic, Label: tr.BackdropAcrylic()},
										{Value: config.BackdropMica, Label: tr.BackdropMica()},
										{Value: config.BackdropTabbed, Label: tr.BackdropTabbed()},
										{Value: config.BackdropNone, Label: tr.BackdropNone()},
									},
								},
							},
						},
					},
					{
						ID:    "locale-theme",
						Title: tr.SettingsGrpLocale(),
						Items: []ui.SettingsItem{
							{
								ID:          "theme-mode",
								Title:       tr.SettingsTheme(),
								Description: tr.SettingsThemeDesc(),
								Control: ui.Select[config.ThemeMode]{
									TestID: "set-theme",
									Value:  func() config.ThemeMode { return cfgSig.Get().Theme },
									OnChange: func(v config.ThemeMode) {
										updateCfg(func(c *config.Config) { c.Theme = v })
									},
									Options: []ui.Option[config.ThemeMode]{
										{Value: config.ThemeSystem, Label: tr.ThemeSystem()},
										{Value: config.ThemeLight, Label: tr.ThemeLight()},
										{Value: config.ThemeDark, Label: tr.ThemeDark()},
									},
								},
							},
							{
								ID:          "language",
								Title:       tr.SettingsLanguage(),
								Description: tr.SettingsLanguageDesc(),
								Control: ui.Select[string]{
									TestID: "set-language",
									Value:  func() string { return cfgSig.Get().Language },
									OnChange: func(v string) {
										updateCfg(func(c *config.Config) { c.Language = v })
									},
									Options: []ui.Option[string]{
										{Value: "", Label: tr.LangSystem()},
										{Value: "en", Label: tr.LangEnglish()},
										{Value: "zh-Hans", Label: tr.LangChinese()},
									},
								},
							},
						},
					},
				},
			},
			{
				ID:    "viewers",
				Title: tr.SettingsCatViewers(),
				Groups: []ui.SettingsGroup{
					{
						ID:    "text-grp",
						Title: tr.SettingsGrpText(),
						Items: []ui.SettingsItem{
							{
								ID:          "line-numbers",
								Title:       tr.SettingsShowLineNumbers(),
								Description: tr.SettingsShowLineNumbersDesc(),
								Control: ui.Switch{
									TestID:   "set-line-numbers",
									Checked:  func() bool { return cfgSig.Get().ShowLineNumbers },
									OnChange: func(v bool) { updateCfg(func(c *config.Config) { c.ShowLineNumbers = v }) },
								},
							},
							{
								ID:          "word-wrap",
								Title:       tr.SettingsWordWrap(),
								Description: tr.SettingsWordWrapDesc(),
								Control: ui.Switch{
									TestID:   "set-word-wrap",
									Checked:  func() bool { return cfgSig.Get().WordWrap },
									OnChange: func(v bool) { updateCfg(func(c *config.Config) { c.WordWrap = v }) },
								},
							},
							{
								ID:          "text-font-size",
								Title:       tr.SettingsTextFontSize(),
								Description: tr.SettingsTextFontSizeDesc(),
								Control: core.View{
									Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 8, Width: core.Pt(180)},
									Children: []core.Node{
										core.View{
											Style: core.Style{FlexGrow: 1},
											Children: []core.Node{
												ui.Slider{
													Min:      10,
													Max:      28,
													Step:     1,
													Value:    func() float64 { return float64(cfgSig.Get().TextFontSize) },
													OnChange: func(v float64) { updateCfg(func(c *config.Config) { c.TextFontSize = float32(v) }) },
												},
											},
										},
										core.Text{
											ContentFunc: func() string { return strconv.Itoa(int(cfgSig.Get().TextFontSize)) + " pt" },
											Style:       core.Style{FontSize: 12, Width: core.Pt(38), TextAlign: core.TextAlignEnd},
											StyleFunc:   ui.Colored(colors.Muted),
										},
									},
								},
							},
						},
					},
					{
						ID:    "md-grp",
						Title: tr.SettingsGrpMarkdown(),
						Items: []ui.SettingsItem{
							{
								ID:          "md-toc",
								Title:       tr.SettingsMarkdownToc(),
								Description: tr.SettingsMarkdownTocDesc(),
								Control: ui.Switch{
									Checked:  func() bool { return cfgSig.Get().MarkdownShowToc },
									OnChange: func(v bool) { updateCfg(func(c *config.Config) { c.MarkdownShowToc = v }) },
								},
							},
						},
					},
					{
						ID:    "img-grp",
						Title: tr.SettingsGrpImage(),
						Items: []ui.SettingsItem{
							{
								ID:          "img-checkered",
								Title:       tr.SettingsImageCheckered(),
								Description: tr.SettingsImageCheckeredDesc(),
								Control: ui.Switch{
									Checked:  func() bool { return cfgSig.Get().ImageCheckeredBg },
									OnChange: func(v bool) { updateCfg(func(c *config.Config) { c.ImageCheckeredBg = v }) },
								},
							},
							{
								ID:          "img-svg-web",
								Title:       tr.SettingsRenderSvgWeb(),
								Description: tr.SettingsRenderSvgWebDesc(),
								Control: ui.Switch{
									Checked:  func() bool { return cfgSig.Get().RenderSvgWeb },
									OnChange: func(v bool) { updateCfg(func(c *config.Config) { c.RenderSvgWeb = v }) },
								},
							},
						},
					},
					{
						ID:    "media-grp",
						Title: tr.SettingsGrpMedia(),
						Items: []ui.SettingsItem{
							{
								ID:          "video-loop",
								Title:       tr.SettingsVideoLoop(),
								Description: tr.SettingsVideoLoopDesc(),
								Control: ui.Switch{
									Checked:  func() bool { return cfgSig.Get().VideoLoop },
									OnChange: func(v bool) { updateCfg(func(c *config.Config) { c.VideoLoop = v }) },
								},
							},
							{
								ID:          "video-volume",
								Title:       tr.SettingsVideoVolume(),
								Description: tr.SettingsVideoVolumeDesc(),
								Control: core.View{
									Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 8, Width: core.Pt(180)},
									Children: []core.Node{
										core.View{
											Style: core.Style{FlexGrow: 1},
											Children: []core.Node{
												ui.Slider{
													Min:      0,
													Max:      100,
													Step:     5,
													Value:    func() float64 { return float64(cfgSig.Get().VideoVolume * 100) },
													OnChange: func(v float64) { updateCfg(func(c *config.Config) { c.VideoVolume = float32(v / 100.0) }) },
												},
											},
										},
										core.Text{
											ContentFunc: func() string { return fmt.Sprintf("%.0f%%", cfgSig.Get().VideoVolume*100) },
											Style:       core.Style{FontSize: 12, Width: core.Pt(38), TextAlign: core.TextAlignEnd},
											StyleFunc:   ui.Colored(colors.Muted),
										},
									},
								},
							},
						},
					},
					{
						ID:    "font-grp",
						Title: tr.SettingsGrpFont(),
						Items: []ui.SettingsItem{
							{
								ID:          "font-mode",
								Title:       tr.SettingsFontMode(),
								Description: tr.SettingsFontModeDesc(),
								Control: ui.Select[config.FontPreviewMode]{
									Value: func() config.FontPreviewMode { return cfgSig.Get().FontPreviewMode },
									OnChange: func(v config.FontPreviewMode) {
										updateCfg(func(c *config.Config) { c.FontPreviewMode = v })
									},
									Options: []ui.Option[config.FontPreviewMode]{
										{Value: config.FontModePangram, Label: tr.FontModePangram()},
										{Value: config.FontModeGlyphs, Label: tr.FontModeGlyphs()},
									},
								},
							},
						},
					},
				},
			},
			{
				ID:    "extensions",
				Title: tr.SettingsCatExtensions(),
				Groups: []ui.SettingsGroup{
					{
						ID:    "ext-mode",
						Title: tr.SettingsGrpFilterMode(),
						Items: []ui.SettingsItem{
							{
								ID:          "use-allowlist",
								Title:       tr.SettingsUseAllowlist(),
								Description: tr.SettingsUseAllowlistDesc(),
								Control: ui.Switch{
									TestID:   "set-use-allowlist",
									Checked:  func() bool { return cfgSig.Get().UseExtensionAllowlist },
									OnChange: func(v bool) { updateCfg(func(c *config.Config) { c.UseExtensionAllowlist = v }) },
								},
							},
						},
					},
					{
						ID:    "ext-lists",
						Title: tr.SettingsGrpFilterLists(),
						Actions: []core.Node{
							ui.Button{
								TestID:  "btn-apply-ext",
								Text:    tr.SettingsSaveLists(),
								Icon:    lucide.Check,
								Variant: ui.ButtonPrimary,
								Size:    ui.SizeSmall,
								OnPress: func() {
									updateCfg(func(c *config.Config) {
										c.ExtensionAllowlist = config.ParseExtensionList(allowText.GetUntracked())
										c.ExtensionBlocklist = config.ParseExtensionList(blockText.GetUntracked())
									})
								},
							},
						},
						Items: []ui.SettingsItem{
							{
								ID:          "allowlist",
								Title:       tr.SettingsAllowlist(),
								Description: tr.SettingsAllowlistDesc(),
								Stacked:     true,
								Control: ui.Input{
									TestID:      "in-allowlist",
									Value:       allowText.Get,
									OnChange:    allowText.Set,
									Placeholder: ".txt, .md, .png",
									Style:       core.Style{Width: core.Pct(100)},
								},
							},
							{
								ID:          "blocklist",
								Title:       tr.SettingsBlocklist(),
								Description: tr.SettingsBlocklistDesc(),
								Stacked:     true,
								Control: ui.Input{
									TestID:      "in-blocklist",
									Value:       blockText.Get,
									OnChange:    blockText.Set,
									Placeholder: ".insv",
									Style:       core.Style{Width: core.Pct(100)},
								},
							},
						},
					},
				},
			},
			{
				ID:    "plugins",
				Title: tr.SettingsCatPlugins(),
				Groups: []ui.SettingsGroup{
					{
						ID:    "reg-plugins",
						Title: tr.SettingsGrpRegisteredPlugins(),
						Items: pluginRows,
					},
				},
			},
		}

		return core.View{
			Style: core.Style{FlexGrow: 1, FlexShrink: 1, MinWidth: core.Pt(0), MinHeight: core.Pt(0)},
			StyleFunc: func() core.Style {
				return core.Style{Background: colors.Background()}
			},
			Children: []core.Node{
				ui.Settings{
					Categories:        categories,
					Category:          catSig,
					Query:             querySig,
					Title:             tr.SettingsTitle(),
					SearchLabel:       tr.SettingsSearch(),
					SearchPlaceholder: tr.SettingsSearchPlaceholder(),
					ResultsTitle:      tr.SettingsResults(),
					EmptyText:         tr.SettingsEmpty(),
					CloseLabel:        tr.SettingsClose(),
					OnClose:           onClose,
				},
			},
		}
	})
}
