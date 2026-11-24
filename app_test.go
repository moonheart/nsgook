package main

import (
	"archive/zip"
	"bytes"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
	"weak"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"

	"nsgook/internal/app"
	"nsgook/internal/config"
	"nsgook/internal/sys"
)

func TestMain(m *testing.M) {
	nsgotest.Main(m)
}

type mockSystem struct {
	mu             sync.Mutex
	selection      string
	selectionCalls int
	focusedType    sys.FocusedWindowType
	selfOwned      bool
	opened         []string
	openedWith     []string
	assocName      string
	driveTotal     uint64
	driveFree      uint64
	onKey          func(vk uint32, down bool, mods uint32)
	onForeground   func()
}

func (m *mockSystem) StartHook(onKey func(vk uint32, down bool, mods uint32), onForeground func()) func() {
	m.mu.Lock()
	m.onKey = onKey
	m.onForeground = onForeground
	m.mu.Unlock()
	return func() {
		m.mu.Lock()
		m.onKey = nil
		m.onForeground = nil
		m.mu.Unlock()
	}
}

func (m *mockSystem) GetFocusedWindowType() sys.FocusedWindowType {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.focusedType
}

func (m *mockSystem) GetCurrentSelection() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.selectionCalls++
	return m.selection
}

func (m *mockSystem) SetSelection(path string) {
	m.mu.Lock()
	m.selection = path
	m.mu.Unlock()
}

func (m *mockSystem) SelectionCalls() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.selectionCalls
}

func (m *mockSystem) PressKey(vk uint32) {
	m.mu.Lock()
	onKey := m.onKey
	m.mu.Unlock()
	if onKey != nil {
		onKey(vk, true, 0)
		onKey(vk, false, 0)
	}
}

func (m *mockSystem) IsForegroundOwnedBySelf() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.selfOwned
}

func (m *mockSystem) OpenDefault(path string) error {
	m.mu.Lock()
	m.opened = append(m.opened, path)
	m.mu.Unlock()
	return nil
}

func (m *mockSystem) OpenWithDialog(path string) error {
	m.mu.Lock()
	m.openedWith = append(m.openedWith, path)
	m.mu.Unlock()
	return nil
}

func (m *mockSystem) GetAssocAppName(string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.assocName
}

func (m *mockSystem) GetShellThumbnail(_ string, size int) (*image.RGBA, error) {
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	img.SetRGBA(0, 0, color.RGBA{R: 60, G: 120, B: 220, A: 255})
	return img, nil
}

func (m *mockSystem) GetShellIcon(path string, size int) (*image.RGBA, error) {
	return m.GetShellThumbnail(path, size)
}

func (m *mockSystem) GetDriveSpace(string) (uint64, uint64, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.driveTotal > 0 {
		return m.driveTotal, m.driveFree, true
	}
	return 100 << 30, 40 << 30, true
}

func (m *mockSystem) SnapshotOpened() ([]string, []string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string(nil), m.opened...), append([]string(nil), m.openedWith...)
}

type fixtureSet struct {
	dir   string
	sub   string
	goF   string
	mdF   string
	csvF  string
	pngF  string
	zipF  string
	jsonF string
	htmlF string
	emlF  string
	objF  string
	docxF string
}

func createFixtures(t *testing.T) fixtureSet {
	t.Helper()
	dir := t.TempDir()
	sub := filepath.Join(dir, "folder")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(name string, data []byte) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, data, 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}

	// PNG
	img := image.NewRGBA(image.Rect(0, 0, 16, 16))
	for y := 0; y < 16; y++ {
		for x := 0; x < 16; x++ {
			img.SetRGBA(x, y, color.RGBA{R: uint8(x * 16), G: uint8(y * 16), B: 180, A: 255})
		}
	}
	var pngBuf bytes.Buffer
	_ = png.Encode(&pngBuf, img)

	// ZIP
	var zipBuf bytes.Buffer
	zw := zip.NewWriter(&zipBuf)
	zf, _ := zw.Create("notes.txt")
	_, _ = zf.Write([]byte("zipped note content"))
	_ = zw.Close()

	// DOCX
	var docxBuf bytes.Buffer
	dzw := zip.NewWriter(&docxBuf)
	dw, _ := dzw.Create("word/document.xml")
	_, _ = dw.Write([]byte(`<?xml version="1.0"?><w:document><w:body><w:p><w:r><w:t>NSGook E2E DOCX</w:t></w:r></w:p></w:body></w:document>`))
	_ = dzw.Close()

	return fixtureSet{
		dir:   dir,
		sub:   sub,
		goF:   write("main.go", []byte("package demo\n\nfunc Hello() string { return \"hi\" }\n")),
		mdF:   write("readme.md", []byte("# NSGook\n\n## Features\n\n- Fast\n- Native\n")),
		csvF:  write("table.csv", []byte("name,count\nalpha,10\nbeta,20\n")),
		pngF:  write("pic.png", pngBuf.Bytes()),
		zipF:  write("bundle.zip", zipBuf.Bytes()),
		jsonF: write("data.json", []byte(`{"app":"NSGook","ok":true}`)),
		htmlF: write("page.html", []byte(`<!doctype html><html><body><h1>NSGook HTML</h1></body></html>`)),
		emlF:  write("mail.eml", []byte("Subject: Test Mail\r\nFrom: user@example.com\r\n\r\nHello body.\r\n")),
		objF:  write("tri.obj", []byte("v 0 0 0\nv 1 0 0\nv 0 1 0\nf 1 2 3\n")),
		docxF: write("sample.docx", docxBuf.Bytes()),
	}
}

func TestControllerMultiWindowPinningAndSingletonSettings(t *testing.T) {
	fx := createFixtures(t)
	cfgPath := filepath.Join(t.TempDir(), "config.yaml")
	mock := &mockSystem{
		selection:   fx.goF,
		focusedType: sys.FocusedExplorer,
		assocName:   "Notepad",
	}

	var ctrl *app.Controller
	a := nsgotest.StartApp(t, func(cx *core.Scope) {
		ctrl = app.Start(cx, app.AppOptions{
			Sys:          mock,
			DisableHooks: true,
			ConfigPath:   cfgPath,
		})
	}, nsgotest.Environment(core.Environment{OS: core.OSWindows}))

	a.Post(func() {
		s1 := ctrl.ToggleFromSelection()
		if s1 == nil || s1.Path() != fx.goF || s1.PluginID() != "textviewer" {
			t.Fatalf("unexpected initial preview session: %+v", s1)
		}
		s1.BringToFront()
		if s1.Topmost() {
			t.Fatal("BringToFront should preserve Topmost=false when not pinned topmost")
		}
		ctrl.DetachToPinned(s1)
		s2 := ctrl.Preview(fx.pngF, app.PreviewOptions{})
		if s2 == nil || s2 == s1 {
			t.Fatalf("expected second unpinned session after pinning first")
		}
	})
	time.Sleep(60 * time.Millisecond)
	a.Settle()

	var count int
	a.Post(func() {
		count = len(ctrl.Sessions())
		// Calling OpenSettings twice must reuse singleton settings window without panicking.
		ctrl.OpenSettings()
		ctrl.OpenSettings()
		// Switch theme & language reactively across all windows.
		_ = ctrl.Config().Update(func(c *config.Config) {
			c.Theme = config.ThemeDark
			c.Language = "zh-Hans"
		})
	})
	a.Settle()

	if count != 2 {
		t.Fatalf("expected 2 open sessions (1 pinned + 1 active), got %d", count)
	}
}

func TestViewerSurfaceFormatsAndTitlebarActions(t *testing.T) {
	fx := createFixtures(t)
	cfgPath := filepath.Join(t.TempDir(), "config.yaml")
	mock := &mockSystem{assocName: "Editor"}

	var ctrl *app.Controller
	var sess *app.ViewerSession
	closedCount := 0

	a := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		ctrl = app.Start(cx, app.AppOptions{
			Sys:          mock,
			DisableHooks: true,
			ConfigPath:   cfgPath,
		})
		_ = ctrl.Config().Update(func(c *config.Config) {
			c.ShowReload = true
		})
		sess = app.NewViewerSession(ctrl, cx, fx.goF, app.PreviewOptions{})
		return app.RenderViewerSurface(cx, sess, func() {
			closedCount++
		})
	}, nsgotest.Environment(core.Environment{OS: core.OSWindows}))

	a.WaitFor("id:viewer-titlebar", 2*time.Second)

	// Switch across representative formats and confirm each loads and renders cleanly.
	cases := []struct {
		path   string
		wantID string
	}{
		{fx.mdF, "markdownviewer"},
		{fx.csvF, "csvviewer"},
		{fx.pngF, "imageviewer"},
		{fx.zipF, "archiveviewer"},
		{fx.jsonF, "textviewer"},
		{fx.htmlF, "htmlviewer"},
		{fx.emlF, "mailviewer"},
		{fx.objF, "model3dviewer"},
		{fx.docxF, "officeviewer"},
		{fx.sub, "infopanel"},
		{fx.goF, "textviewer"},
	}
	for _, tc := range cases {
		p := tc.path
		a.Post(func() { sess.Load(p, "") })
		time.Sleep(35 * time.Millisecond)
		a.Settle()
		if got := sess.PluginID(); got != tc.wantID {
			t.Fatalf("for %s expected plugin %q, got %q", filepath.Base(p), tc.wantID, got)
		}
	}

	// Titlebar: Stay on Top toggle
	if sess.Topmost() {
		t.Fatal("expected initial topmost=false")
	}
	a.Press("id:btn-topmost")
	if !sess.Topmost() {
		t.Fatal("expected topmost=true after btn-topmost press")
	}

	// Titlebar: Pin toggle
	a.Press("id:btn-pin")
	if !sess.Pinned() {
		t.Fatal("expected pinned=true after btn-pin press")
	}

	// More menu -> Copy Path -> verify clipboard
	a.Press("id:btn-more")
	a.WaitFor("id:menu-copy-path", 2*time.Second)
	a.Press("id:menu-copy-path")
	wantClip := sys.FormatCopyPath(fx.goF)
	if a.Clipboard != wantClip {
		t.Fatalf("clipboard = %q, want %q", a.Clipboard, wantClip)
	}

	// More menu -> View as Binary (Hex)
	a.Press("id:btn-more")
	a.WaitFor("id:ext-view-hex", 2*time.Second)
	a.Press("id:ext-view-hex")
	time.Sleep(25 * time.Millisecond)
	a.Settle()
	if sess.PluginID() != "binaryviewer" {
		t.Fatalf("expected binaryviewer after View as Binary (Hex), got %q", sess.PluginID())
	}

	// Open & Open With actions
	a.Press("id:btn-open")
	a.Press("id:btn-open-with")
	opened, openedWith := mock.SnapshotOpened()
	if len(opened) != 1 || opened[0] != fx.goF {
		t.Fatalf("unexpected OpenDefault calls: %v", opened)
	}
	if len(openedWith) != 1 || openedWith[0] != fx.goF {
		t.Fatalf("unexpected OpenWithDialog calls: %v", openedWith)
	}

	// Reload & Close button
	a.Press("id:btn-reload")
	a.Settle()
	a.Press("id:btn-close")
	if closedCount != 1 {
		t.Fatalf("expected onClose callback once, got %d", closedCount)
	}
}

func TestSettingsSurfaceToggleSearchAndPersistence(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "settings.yaml")
	mock := &mockSystem{}

	var ctrl *app.Controller
	a := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		ctrl = app.Start(cx, app.AppOptions{
			Sys:          mock,
			DisableHooks: true,
			ConfigPath:   cfgPath,
		})
		return app.RenderSettingsSurface(cx, ctrl, func() {})
	}, nsgotest.Environment(core.Environment{OS: core.OSWindows}))

	a.WaitFor("id:set-close-lost-focus", 2*time.Second)
	a.Press("id:set-close-lost-focus")
	a.Press("id:set-show-reload")
	a.Settle()

	if !ctrl.Config().Get().CloseOnLostFocus || !ctrl.Config().Get().ShowReload {
		t.Fatal("expected settings store switches to update in memory")
	}

	data, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if !strings.Contains(text, "close_on_lost_focus: true") || !strings.Contains(text, "show_reload: true") {
		t.Fatalf("expected updated YAML on disk, got:\n%s", text)
	}

	// Switch theme + language and settle cleanly.
	a.Post(func() {
		_ = ctrl.Config().Update(func(c *config.Config) {
			c.Theme = config.ThemeLight
			c.Language = "en"
		})
	})
	a.Settle()
	a.Post(func() {
		_ = ctrl.Config().Update(func(c *config.Config) {
			c.Theme = config.ThemeDark
			c.Language = "zh-Hans"
		})
	})
	a.Settle()
}

func TestOpenSettingsFromMoreMenuSurvivesPopupClose(t *testing.T) {
	fx := createFixtures(t)
	cfgPath := filepath.Join(t.TempDir(), "config.yaml")
	mock := &mockSystem{assocName: "Editor"}

	var ctrl *app.Controller
	a := nsgotest.StartApp(t, func(appCx *core.Scope) {
		ctrl = app.Start(appCx, app.AppOptions{
			Sys:          mock,
			DisableHooks: true,
			ConfigPath:   cfgPath,
		})
		appCx.SetMainView(func(cx *core.Scope) core.Node {
			sess := app.NewViewerSession(ctrl, cx, fx.goF, app.PreviewOptions{})
			return app.RenderViewerSurface(cx, sess, func() {})
		})
	}, nsgotest.Environment(core.Environment{OS: core.OSWindows}))

	a.WaitFor("id:btn-more", 2*time.Second)
	a.Press("id:btn-more")
	a.WaitFor("id:menu-settings", 2*time.Second)
	a.Press("id:menu-settings")
	time.Sleep(40 * time.Millisecond)
	a.Settle()

	var open bool
	a.Post(func() { open = ctrl.IsSettingsOpen() })
	a.Settle()
	if !open {
		t.Fatal("expected Settings window opened from More menu to remain open after popup menu disposes")
	}
}

func TestCloseActiveReusesHiddenWindowAndPinnedSessionsCollect(t *testing.T) {
	fx := createFixtures(t)
	cfgPath := filepath.Join(t.TempDir(), "config.yaml")
	mock := &mockSystem{assocName: "Editor"}

	var ctrl *app.Controller
	a := nsgotest.StartApp(t, func(cx *core.Scope) {
		ctrl = app.Start(cx, app.AppOptions{
			Sys:          mock,
			DisableHooks: true,
			ConfigPath:   cfgPath,
		})
	}, nsgotest.Environment(core.Environment{OS: core.OSWindows}))

	var s1, s2 *app.ViewerSession
	a.Post(func() {
		s1 = ctrl.Preview(fx.goF, app.PreviewOptions{})
	})
	time.Sleep(30 * time.Millisecond)
	a.Settle()

	var activeAfterClose *app.ViewerSession
	var sessionsAfterClose int
	var pathAfterClose string
	a.Post(func() {
		ctrl.CloseActive()
		activeAfterClose = ctrl.ActiveSession()
		sessionsAfterClose = len(ctrl.Sessions())
		pathAfterClose = s1.Path()
	})
	a.Settle()

	if activeAfterClose != nil || sessionsAfterClose != 0 || pathAfterClose != "" {
		t.Fatalf("after CloseActive: active=%v sessions=%d path=%q", activeAfterClose, sessionsAfterClose, pathAfterClose)
	}

	a.Post(func() {
		s2 = ctrl.Preview(fx.pngF, app.PreviewOptions{})
	})
	time.Sleep(30 * time.Millisecond)
	a.Settle()

	if s2 != s1 || s2.PluginID() != "imageviewer" {
		t.Fatalf("expected hidden window reuse (same session + imageviewer), got s1=%p s2=%p plugin=%q", s1, s2, s2.PluginID())
	}

	// Verify closed pinned sessions have no immortal scope retention and are collected by GC.
	var ptrs []weak.Pointer[app.ViewerSession]
	for range 10 {
		a.Post(func() {
			p := ctrl.Preview(fx.goF, app.PreviewOptions{Pinned: true})
			ptrs = append(ptrs, weak.Make(p))
		})
		a.Settle()
		a.Post(func() {
			if pinned := ctrl.Sessions(); len(pinned) > 0 {
				pinned[len(pinned)-1].Close()
			}
		})
		a.Settle()
	}

	// Ensure delayed 40ms timers/watchers unwind cleanly before GC.
	time.Sleep(60 * time.Millisecond)
	a.Settle()
	for range 5 {
		runtime.GC()
		time.Sleep(10 * time.Millisecond)
	}
	for i, wp := range ptrs {
		if wp.Value() != nil {
			t.Fatalf("closed pinned ViewerSession #%d still retained after GC", i)
		}
	}
}

func TestSwitchFilesRepeatedlyBoundedHeap(t *testing.T) {
	fx := createFixtures(t)
	cfgPath := filepath.Join(t.TempDir(), "config.yaml")
	mock := &mockSystem{assocName: "Editor"}

	var ctrl *app.Controller
	var sess *app.ViewerSession
	a := nsgotest.StartApp(t, func(cx *core.Scope) {
		ctrl = app.Start(cx, app.AppOptions{
			Sys:          mock,
			DisableHooks: true,
			ConfigPath:   cfgPath,
		})
		sess = app.NewViewerSession(ctrl, cx, fx.goF, app.PreviewOptions{})
		cx.SetMainView(func(vcx *core.Scope) core.Node {
			return app.RenderViewerSurface(vcx, sess, func() {})
		})
	}, nsgotest.Environment(core.Environment{OS: core.OSWindows}))

	a.WaitFor("id:viewer-titlebar", 2*time.Second)

	files := []string{fx.goF, fx.pngF, fx.zipF, fx.mdF, fx.jsonF, fx.sub}
	var allocAt8, allocAt40 uint64
	for i := 1; i <= 40; i++ {
		p := files[i%len(files)]
		a.Post(func() {
			sess.Load(p, "")
		})
		a.Settle()
		if i == 8 || i == 40 {
			time.Sleep(40 * time.Millisecond)
			a.Settle()
			for range 4 {
				runtime.GC()
				time.Sleep(5 * time.Millisecond)
			}
			var ms runtime.MemStats
			runtime.ReadMemStats(&ms)
			if i == 8 {
				allocAt8 = ms.HeapAlloc
			} else {
				allocAt40 = ms.HeapAlloc
			}
		}
	}

	if allocAt40 > allocAt8 && allocAt40-allocAt8 >= 15<<20 {
		t.Fatalf("heap grew unexpectedly across repeated file switches: iter8=%.2fMB iter40=%.2fMB",
			float64(allocAt8)/(1<<20), float64(allocAt40)/(1<<20))
	}
}

func TestArrowKeyDoesNotReopenClosedPreview(t *testing.T) {
	fx := createFixtures(t)
	cfgPath := filepath.Join(t.TempDir(), "config.yaml")
	mock := &mockSystem{
		selection:   fx.goF,
		focusedType: sys.FocusedExplorer,
		assocName:   "Editor",
	}

	var ctrl *app.Controller
	a := nsgotest.StartApp(t, func(cx *core.Scope) {
		ctrl = app.Start(cx, app.AppOptions{
			Sys:          mock,
			DisableHooks: false,
			ConfigPath:   cfgPath,
		})
	}, nsgotest.Environment(core.Environment{OS: core.OSWindows}))

	readState := func() (*app.ViewerSession, int, string) {
		var active *app.ViewerSession
		var count int
		var path string
		a.Post(func() {
			active = ctrl.ActiveSession()
			count = len(ctrl.Sessions())
			if active != nil {
				path = active.Path()
			}
		})
		a.Settle()
		return active, count, path
	}

	// 1. Arrow key before ever opening preview must remain a no-op without querying selection.
	mock.PressKey(sys.VKDown)
	a.Settle()
	if active, count, _ := readState(); active != nil || count != 0 || mock.SelectionCalls() != 0 {
		t.Fatalf("arrow before open: active=%v count=%d selectionCalls=%d", active, count, mock.SelectionCalls())
	}

	// 2. Press Space to open initial preview.
	mock.PressKey(sys.VKSpace)
	time.Sleep(30 * time.Millisecond)
	a.Settle()
	s1, count, path := readState()
	if s1 == nil || count != 1 || path != fx.goF {
		t.Fatalf("Space open: s1=%v count=%d path=%q, want %q", s1, count, path, fx.goF)
	}

	// 3. Arrow key while preview is open switches file in the active session.
	mock.SetSelection(fx.pngF)
	mock.PressKey(sys.VKDown)
	time.Sleep(30 * time.Millisecond)
	a.Settle()
	active, count, path := readState()
	if active != s1 || count != 1 || path != fx.pngF {
		t.Fatalf("arrow while open: active=%p (want %p) count=%d path=%q (want %q)", active, s1, count, path, fx.pngF)
	}

	// 4. Press Space again to close preview.
	mock.PressKey(sys.VKSpace)
	a.Settle()
	if active, count, _ := readState(); active != nil || count != 0 {
		t.Fatalf("after Space close: active=%v count=%d, want nil and 0", active, count)
	}

	// 5. Arrow keys after closing must NOT reopen preview or query Explorer selection.
	callsBefore := mock.SelectionCalls()
	mock.SetSelection(fx.mdF)
	for _, vk := range []uint32{sys.VKDown, sys.VKUp, sys.VKLeft, sys.VKRight} {
		mock.PressKey(vk)
		a.Settle()
	}
	if active, count, _ := readState(); active != nil || count != 0 || mock.SelectionCalls() != callsBefore {
		t.Fatalf("arrow after close reopened preview: active=%v count=%d calls=%d (before=%d)",
			active, count, mock.SelectionCalls(), callsBefore)
	}

	// 6. Press Space to reopen preview on newly selected file, reusing hidden session s1.
	mock.PressKey(sys.VKSpace)
	time.Sleep(30 * time.Millisecond)
	a.Settle()
	active, count, path = readState()
	if active != s1 || count != 1 || path != fx.mdF {
		t.Fatalf("Space reopen: active=%p (want %p) count=%d path=%q (want %q)", active, s1, count, path, fx.mdF)
	}

	// 7. Arrow key while reopened switches file cleanly.
	mock.SetSelection(fx.csvF)
	mock.PressKey(sys.VKRight)
	time.Sleep(30 * time.Millisecond)
	a.Settle()
	active, count, path = readState()
	if active != s1 || count != 1 || path != fx.csvF {
		t.Fatalf("arrow after reopen: active=%p (want %p) count=%d path=%q (want %q)", active, s1, count, path, fx.csvF)
	}
}

func TestLargeImagePreviewBoundedHeap(t *testing.T) {
	dir := t.TempDir()
	writeLargePNG := func(name string, fill color.RGBA) string {
		img := image.NewRGBA(image.Rect(0, 0, 2048, 1536))
		for i := 0; i < len(img.Pix); i += 4 {
			img.Pix[i] = fill.R
			img.Pix[i+1] = fill.G
			img.Pix[i+2] = fill.B
			img.Pix[i+3] = fill.A
		}
		p := filepath.Join(dir, name)
		var buf bytes.Buffer
		enc := png.Encoder{CompressionLevel: png.NoCompression}
		if err := enc.Encode(&buf, img); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, buf.Bytes(), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	largeImgs := []string{
		writeLargePNG("large1.png", color.RGBA{R: 180, G: 80, B: 60, A: 255}),
		writeLargePNG("large2.png", color.RGBA{R: 40, G: 140, B: 210, A: 255}),
		writeLargePNG("large3.png", color.RGBA{R: 60, G: 190, B: 90, A: 255}),
		writeLargePNG("large4.png", color.RGBA{R: 210, G: 170, B: 40, A: 255}),
	}
	notePath := filepath.Join(dir, "note.txt")
	if err := os.WriteFile(notePath, []byte("hello nsgook\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfgPath := filepath.Join(t.TempDir(), "config.yaml")
	mock := &mockSystem{assocName: "Photos"}

	var ctrl *app.Controller
	a := nsgotest.StartApp(t, func(cx *core.Scope) {
		ctrl = app.Start(cx, app.AppOptions{
			Sys:          mock,
			DisableHooks: true,
			ConfigPath:   cfgPath,
		})
	}, nsgotest.Environment(core.Environment{OS: core.OSWindows}))

	a.Settle()
	for range 4 {
		runtime.GC()
		time.Sleep(5 * time.Millisecond)
	}
	var baseMS runtime.MemStats
	runtime.ReadMemStats(&baseMS)

	var sess *app.ViewerSession
	a.Post(func() {
		sess = ctrl.Preview(largeImgs[0], app.PreviewOptions{})
	})
	a.Settle()
	time.Sleep(50 * time.Millisecond)
	a.Settle()

	for i := 0; i < 6; i++ {
		target := largeImgs[(i+1)%len(largeImgs)]
		a.Post(func() {
			sess.Load(target, "")
		})
		a.Settle()
		time.Sleep(40 * time.Millisecond)
		a.Settle()
		var gotPlugin string
		a.Post(func() {
			gotPlugin = sess.PluginID()
		})
		a.Settle()
		if gotPlugin != "imageviewer" {
			t.Fatalf("iteration %d: expected imageviewer plugin, got %q", i, gotPlugin)
		}
	}

	// Switching from a large image to a non-image plugin must release the image buffer immediately.
	a.Post(func() {
		sess.Load(notePath, "")
	})
	a.Settle()
	time.Sleep(30 * time.Millisecond)
	a.Settle()
	var notePlugin string
	a.Post(func() {
		notePlugin = sess.PluginID()
	})
	a.Settle()
	if notePlugin != "textviewer" {
		t.Fatalf("after switch to note.txt: expected textviewer plugin, got %q", notePlugin)
	}
	for range 5 {
		runtime.GC()
		time.Sleep(10 * time.Millisecond)
	}
	var switchMS runtime.MemStats
	runtime.ReadMemStats(&switchMS)
	if switchMS.HeapAlloc > baseMS.HeapAlloc && switchMS.HeapAlloc-baseMS.HeapAlloc >= 8<<20 {
		t.Fatalf("large image preview retained unexpected heap after switching to note.txt: before=%.2fMB after=%.2fMB",
			float64(baseMS.HeapAlloc)/(1<<20), float64(switchMS.HeapAlloc)/(1<<20))
	}

	// Reload a large image, then close via CloseActive and verify heap is released immediately.
	a.Post(func() {
		sess.Load(largeImgs[3], "")
	})
	a.Settle()
	time.Sleep(40 * time.Millisecond)
	a.Settle()
	var reloadedPlugin string
	a.Post(func() {
		reloadedPlugin = sess.PluginID()
	})
	a.Settle()
	if reloadedPlugin != "imageviewer" {
		t.Fatalf("after reloading large4.png: expected imageviewer plugin, got %q", reloadedPlugin)
	}

	var active *app.ViewerSession
	var count int
	a.Post(func() {
		ctrl.CloseActive()
		active = ctrl.ActiveSession()
		count = len(ctrl.Sessions())
	})
	a.Settle()
	if active != nil || count != 0 {
		t.Fatalf("after CloseActive: active=%v sessions=%d, want nil and 0", active, count)
	}

	time.Sleep(60 * time.Millisecond)
	a.Settle()
	for range 5 {
		runtime.GC()
		time.Sleep(10 * time.Millisecond)
	}
	var afterMS runtime.MemStats
	runtime.ReadMemStats(&afterMS)

	if afterMS.HeapAlloc > baseMS.HeapAlloc && afterMS.HeapAlloc-baseMS.HeapAlloc >= 8<<20 {
		t.Fatalf("large image preview retained unexpected heap after close: before=%.2fMB after=%.2fMB",
			float64(baseMS.HeapAlloc)/(1<<20), float64(afterMS.HeapAlloc)/(1<<20))
	}
}

func findViewByKind(root *nsgotest.View, kind nsgotest.ViewKind) *nsgotest.View {
	if root == nil {
		return nil
	}
	if root.Kind == kind {
		return root
	}
	for _, ch := range root.Children {
		if hit := findViewByKind(ch, kind); hit != nil {
			return hit
		}
	}
	return nil
}

func TestImageViewerUsesDataNotURI(t *testing.T) {
	fx := createFixtures(t)
	cfgPath := filepath.Join(t.TempDir(), "config.yaml")
	mock := &mockSystem{assocName: "Photos"}

	var ctrl *app.Controller
	var sess *app.ViewerSession

	a := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		ctrl = app.Start(cx, app.AppOptions{
			Sys:          mock,
			DisableHooks: true,
			ConfigPath:   cfgPath,
		})
		sess = app.NewViewerSession(ctrl, cx, fx.pngF, app.PreviewOptions{})
		return app.RenderViewerSurface(cx, sess, func() {})
	}, nsgotest.Environment(core.Environment{OS: core.OSWindows}))

	a.WaitFor("id:viewer-root", 2*time.Second)

	var imgView *nsgotest.View
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		a.Settle()
		root := a.Find("id:viewer-root")
		if v := findViewByKind(root, nsgotest.KindImage); v != nil && len(v.Image.Source.Data) > 0 {
			imgView = v
			break
		}
		time.Sleep(15 * time.Millisecond)
	}
	if imgView == nil {
		t.Fatal("expected imageviewer to mount a core.Image using non-empty Source.Data")
	}
	if imgView.Image.Source.URI != "" {
		t.Fatalf("expected ImageSource.URI to be empty (bypassing imageload.cache), got %q", imgView.Image.Source.URI)
	}
	if imgView.Image.Source.Pixels != nil {
		t.Fatal("expected native ImageSource.Data fast path without RGBA Pixels copy")
	}
}
