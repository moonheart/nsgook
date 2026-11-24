//go:build windows

package sys

import (
	"image"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"unsafe"
)

var (
	procGetForegroundWindow      = user32.NewProc("GetForegroundWindow")
	procGetClassNameW            = user32.NewProc("GetClassNameW")
	procGetWindowTextW           = user32.NewProc("GetWindowTextW")
	procGetWindowTextLengthW     = user32.NewProc("GetWindowTextLengthW")
	procFindWindowExW            = user32.NewProc("FindWindowExW")
	procEnumWindows              = user32.NewProc("EnumWindows")
	procEnumChildWindows         = user32.NewProc("EnumChildWindows")
	procIsWindowVisible          = user32.NewProc("IsWindowVisible")
	procGetWindowThreadProcessId = user32.NewProc("GetWindowThreadProcessId")
	procGetGUIThreadInfo         = user32.NewProc("GetGUIThreadInfo")
	procAttachThreadInput        = user32.NewProc("AttachThreadInput")
	procGetFocus                 = user32.NewProc("GetFocus")
	procRegisterClipboardFormatW = user32.NewProc("RegisterClipboardFormatW")

	procCreateCompatibleDC = gdi32.NewProc("CreateCompatibleDC")
	procDeleteDC           = gdi32.NewProc("DeleteDC")
	procDeleteObject       = gdi32.NewProc("DeleteObject")
	procGetObjectW         = gdi32.NewProc("GetObjectW")
	procGetDIBits          = gdi32.NewProc("GetDIBits")
)

const (
	processQueryLimitedInfo = 0x1000
	processQueryInfo        = 0x0400
	processVMRead           = 0x0010

	uiaControlTypePropertyID = 30003
	uiaEditControlTypeID     = 50004
	uiaComboBoxControlTypeID = 50003
)

type guiThreadInfo struct {
	cbSize        uint32
	flags         uint32
	hwndActive    uintptr
	hwndFocus     uintptr
	hwndCapture   uintptr
	hwndMenuOwner uintptr
	hwndMoveSize  uintptr
	hwndCaret     uintptr
	rcCaret       struct{ Left, Top, Right, Bottom int32 }
}

type bitmapHeader struct {
	bmType       int32
	bmWidth      int32
	bmHeight     int32
	bmWidthBytes int32
	bmPlanes     uint16
	bmBitsPixel  uint16
	bmBits       uintptr
}

type bitmapInfoHeader struct {
	biSize          uint32
	biWidth         int32
	biHeight        int32
	biPlanes        uint16
	biBitCount      uint16
	biCompression   uint32
	biSizeImage     uint32
	biXPelsPerMeter int32
	biYPelsPerMeter int32
	biClrUsed       uint32
	biClrImportant  uint32
}

type bitmapInfo struct {
	bmiHeader bitmapInfoHeader
	bmiColors [3]uint32
}

func lockOSThread() {
	runtime.LockOSThread()
}

var (
	enumMu     sync.Mutex
	enumNextID uintptr
	enumFuncs  = map[uintptr]func(hwnd uintptr) bool{}
	enumCB     = syscall.NewCallback(func(hwnd, lParam uintptr) uintptr {
		enumMu.Lock()
		fn := enumFuncs[lParam]
		enumMu.Unlock()
		if fn != nil && !fn(hwnd) {
			return 0
		}
		return 1
	})
)

func runEnum(fn func(hwnd uintptr) bool, invoke func(cb, id uintptr)) {
	enumMu.Lock()
	enumNextID++
	id := enumNextID
	enumFuncs[id] = fn
	enumMu.Unlock()
	defer func() {
		enumMu.Lock()
		delete(enumFuncs, id)
		enumMu.Unlock()
	}()
	invoke(enumCB, id)
}

func enumWindows(fn func(hwnd uintptr) bool) {
	runEnum(fn, func(cb, id uintptr) {
		procEnumWindows.Call(cb, id)
	})
}

func enumChildWindows(parent uintptr, fn func(hwnd uintptr) bool) {
	runEnum(fn, func(cb, id uintptr) {
		procEnumChildWindows.Call(parent, cb, id)
	})
}

var classBufPool = sync.Pool{
	New: func() any { return new([256]uint16) },
}

func getWindowClass(hwnd uintptr) string {
	if hwnd == 0 {
		return ""
	}
	buf := classBufPool.Get().(*[256]uint16)
	defer classBufPool.Put(buf)
	n, _, _ := procGetClassNameW.Call(hwnd, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	if n == 0 {
		return ""
	}
	return syscall.UTF16ToString(buf[:n])
}

func getWindowText(hwnd uintptr) string {
	if hwnd == 0 {
		return ""
	}
	n, _, _ := procGetWindowTextLengthW.Call(hwnd)
	if n == 0 {
		return ""
	}
	buf := make([]uint16, n+1)
	read, _, _ := procGetWindowTextW.Call(hwnd, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	return syscall.UTF16ToString(buf[:read])
}

func getWindowProcessName(hwnd uintptr) string {
	if hwnd == 0 {
		return ""
	}
	var pid uint32
	procGetWindowThreadProcessId.Call(hwnd, uintptr(unsafe.Pointer(&pid)))
	if pid == 0 {
		return ""
	}
	h, _, _ := procOpenProcess.Call(processQueryLimitedInfo, 0, uintptr(pid))
	if h == 0 {
		h, _, _ = procOpenProcess.Call(processQueryInfo|processVMRead, 0, uintptr(pid))
	}
	if h == 0 {
		return ""
	}
	defer procCloseHandle.Call(h)
	var buf [1024]uint16
	size := uint32(len(buf))
	ok, _, _ := procQueryFullProcessImageNameW.Call(h, 0, uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&size)))
	if ok == 0 {
		return ""
	}
	full := syscall.UTF16ToString(buf[:size])
	for i := len(full) - 1; i >= 0; i-- {
		if full[i] == '\\' || full[i] == '/' {
			return full[i+1:]
		}
	}
	return full
}

func isListaryToolbarVisible() bool {
	found := false
	enumWindows(func(hwnd uintptr) bool {
		cls := getWindowClass(hwnd)
		if strings.HasPrefix(cls, "Listary_WidgetWin_") {
			vis, _, _ := procIsWindowVisible.Call(hwnd)
			if vis != 0 {
				found = true
				return false
			}
		}
		return true
	})
	return found
}

func isCursorActivated(hwnd uintptr) bool {
	var tid uintptr
	if hwnd != 0 {
		tid, _, _ = procGetWindowThreadProcessId.Call(hwnd, 0)
	} else {
		fg, _, _ := procGetForegroundWindow.Call()
		if fg != 0 {
			tid, _, _ = procGetWindowThreadProcessId.Call(fg, 0)
		}
	}
	var gti guiThreadInfo
	gti.cbSize = uint32(unsafe.Sizeof(gti))
	procGetGUIThreadInfo.Call(tid, uintptr(unsafe.Pointer(&gti)))
	return gti.flags != 0 || gti.hwndCaret != 0 || isListaryToolbarVisible()
}

func getFocusedControl(fg uintptr) uintptr {
	if fg == 0 {
		return 0
	}
	tid, _, _ := procGetWindowThreadProcessId.Call(fg, 0)
	curTid, _, _ := procGetCurrentThreadId.Call()
	if tid == 0 {
		return 0
	}
	attached := false
	if tid != curTid {
		ok, _, _ := procAttachThreadInput.Call(curTid, tid, 1)
		attached = ok != 0
	}
	focused, _, _ := procGetFocus.Call()
	if attached {
		procAttachThreadInput.Call(curTid, tid, 0)
	}
	return focused
}

func isExplorerSearchBoxFocused(fg uintptr) bool {
	return getWindowClass(getFocusedControl(fg)) == "Windows.UI.Core.CoreWindow"
}

func isTextInputClass(cls string) bool {
	return strings.HasPrefix(cls, "Edit") ||
		strings.HasPrefix(cls, "RichEdit") ||
		strings.HasPrefix(cls, "WindowsForms10.EDIT") ||
		strings.HasPrefix(cls, "Scintilla")
}

func isUIATextInputFocused(fg uintptr) bool {
	if fg == 0 {
		return false
	}
	var fgPid uint32
	procGetWindowThreadProcessId.Call(fg, uintptr(unsafe.Pointer(&fgPid)))
	if fgPid == 0 {
		return false
	}
	return runSTA(func() bool {
		uia := createUIAutomation()
		if uia == nil {
			return false
		}
		defer uia.release()
		var focused *comObj
		if failed(uia.call(uiaGetFocusedElement, uintptr(unsafe.Pointer(&focused)))) || focused == nil {
			return false
		}
		defer focused.release()
		var pid int32
		if failed(focused.call(uiaElGetCurrentPid, uintptr(unsafe.Pointer(&pid)))) || uint32(pid) != fgPid {
			return false
		}
		var ctrlType int32
		if !failed(focused.call(uiaElGetCurrentCtrlType, uintptr(unsafe.Pointer(&ctrlType)))) {
			if ctrlType == uiaEditControlTypeID || ctrlType == uiaComboBoxControlTypeID {
				return true
			}
		}
		var clsBstr uintptr
		if !failed(focused.call(uiaElGetCurrentClass, uintptr(unsafe.Pointer(&clsBstr)))) && clsBstr != 0 {
			cls := bstrToString(clsBstr)
			procSysFreeString.Call(clsBstr)
			return isTextInputClass(cls)
		}
		return false
	})
}

func createUIAutomation() *comObj {
	var uia *comObj
	hr, _, _ := procCoCreateInstance.Call(
		uintptr(unsafe.Pointer(&clsidCUIAutomation8)),
		0,
		clsctxInprocServer,
		uintptr(unsafe.Pointer(&iidIUIAutomation)),
		uintptr(unsafe.Pointer(&uia)),
	)
	if !failed(hr) && uia != nil {
		return uia
	}
	hr, _, _ = procCoCreateInstance.Call(
		uintptr(unsafe.Pointer(&clsidCUIAutomation)),
		0,
		clsctxInprocServer,
		uintptr(unsafe.Pointer(&iidIUIAutomation)),
		uintptr(unsafe.Pointer(&uia)),
	)
	if !failed(hr) && uia != nil {
		return uia
	}
	return nil
}

func getFocusedWindowTypeWin() FocusedWindowType {
	fg, _, _ := procGetForegroundWindow.Call()
	if fg == 0 || isCursorActivated(fg) {
		return FocusedInvalid
	}
	cls := getWindowClass(fg)
	switch {
	case cls == "MultiCommander":
		return FocusedMultiCommander
	case cls == "dopus.lister":
		return FocusedDOpus
	case strings.HasPrefix(cls, "EVERYTHING"):
		return FocusedEverything
	case strings.EqualFold(getWindowProcessName(fg), "FPilot.exe") || strings.EqualFold(getWindowProcessName(fg), "FilePilot.exe"):
		if !isTextInputClass(getWindowClass(getFocusedControl(fg))) && !isUIATextInputFocused(fg) {
			return FocusedFilePilot
		}
	case cls == "WorkerW" || cls == "Progman":
		defView, _, _ := procFindWindowExW.Call(fg, 0, uintptr(unsafe.Pointer(wide("SHELLDLL_DefView"))), 0)
		if defView != 0 {
			return FocusedDesktop
		}
	case cls == "ExploreWClass" || cls == "CabinetWClass":
		if !isExplorerSearchBoxFocused(fg) {
			return FocusedExplorer
		}
	case cls == "#32770":
		title := getWindowText(fg)
		if strings.HasPrefix(title, "Internet Download Manager") {
			return FocusedIDM
		}
		dui, _, _ := procFindWindowExW.Call(fg, 0, uintptr(unsafe.Pointer(wide("DUIViewWndClassName"))), 0)
		if dui != 0 && !isExplorerSearchBoxFocused(fg) {
			return FocusedDialog
		}
	case cls == "WinUIDesktopWin32WindowClass" && strings.EqualFold(getWindowProcessName(fg), "DeskBox.exe"):
		return FocusedDeskBox
	}
	return FocusedInvalid
}

func getCurrentSelectionWin() string {
	fg, _, _ := procGetForegroundWindow.Call()
	switch getFocusedWindowTypeWin() {
	case FocusedDesktop:
		return runSTA(getSelectedFromDesktopSTA)
	case FocusedExplorer:
		return runSTA(func() string { return getSelectedFromExplorerSTA(fg) })
	case FocusedEverything:
		return getSelectedFromEverything(fg)
	case FocusedDialog:
		return getSelectedFromDialog(fg)
	default:
		return ""
	}
}

func getSelectedFromExplorerSTA(fg uintptr) string {
	var psw *comObj
	hr, _, _ := procCoCreateInstance.Call(
		uintptr(unsafe.Pointer(&clsidShellWindows)),
		0,
		clsctxServer,
		uintptr(unsafe.Pointer(&iidShellWindows)),
		uintptr(unsafe.Pointer(&psw)),
	)
	if failed(hr) || psw == nil {
		return ""
	}
	defer psw.release()

	fgt, _, _ := procFindWindowExW.Call(fg, 0, uintptr(unsafe.Pointer(wide("ShellTabWindowClass"))), 0)

	var count int32
	if failed(psw.call(swGetCount, uintptr(unsafe.Pointer(&count)))) {
		return ""
	}

	for i := int32(0); i < count; i++ {
		var vi variant
		vi.vt = vtI4
		vi.val = uintptr(i)

		var disp *comObj
		itemHR := psw.call(swItem, uintptr(unsafe.Pointer(&vi)), uintptr(unsafe.Pointer(&disp)))
		if itemHR != 0 || disp == nil {
			continue
		}
		path := extractSelectionFromShellDispatch(disp, fg, fgt)
		disp.release()
		if path != "" {
			return path
		}
	}
	return ""
}

func extractSelectionFromShellDispatch(disp *comObj, fg, fgt uintptr) string {
	var psp *comObj
	if failed(disp.call(comQueryInterface, uintptr(unsafe.Pointer(&iidServiceProvider)), uintptr(unsafe.Pointer(&psp)))) || psp == nil {
		return ""
	}
	defer psp.release()

	var psb *comObj
	if failed(psp.call(spQueryService, uintptr(unsafe.Pointer(&iidShellBrowser)), uintptr(unsafe.Pointer(&iidShellBrowser)), uintptr(unsafe.Pointer(&psb)))) || psb == nil {
		return ""
	}
	defer psb.release()

	var phwnd uintptr
	if failed(psb.call(sbGetWindow, uintptr(unsafe.Pointer(&phwnd)))) {
		return ""
	}
	if fg != phwnd && (fgt == 0 || fgt != phwnd) {
		return ""
	}
	if isCursorActivated(0) {
		return ""
	}
	return getSelectedFromShellBrowser(psb)
}

func getSelectedFromDesktopSTA() string {
	var psw *comObj
	hr, _, _ := procCoCreateInstance.Call(
		uintptr(unsafe.Pointer(&clsidShellWindows)),
		0,
		clsctxServer,
		uintptr(unsafe.Pointer(&iidShellWindows)),
		uintptr(unsafe.Pointer(&psw)),
	)
	if failed(hr) || psw == nil {
		return ""
	}
	defer psw.release()

	var vLoc variant
	var vRoot variant
	var phwnd int32
	var pwba *comObj
	hr = psw.call(
		swFindWindowSW,
		uintptr(unsafe.Pointer(&vLoc)),
		uintptr(unsafe.Pointer(&vRoot)),
		swcDesktop,
		uintptr(unsafe.Pointer(&phwnd)),
		swfoNeedDispatch,
		uintptr(unsafe.Pointer(&pwba)),
	)
	if failed(hr) || pwba == nil {
		return ""
	}
	defer pwba.release()

	if isCursorActivated(uintptr(uint32(phwnd))) {
		return ""
	}

	var psp *comObj
	if failed(pwba.call(comQueryInterface, uintptr(unsafe.Pointer(&iidServiceProvider)), uintptr(unsafe.Pointer(&psp)))) || psp == nil {
		return ""
	}
	defer psp.release()

	var psb *comObj
	if failed(psp.call(spQueryService, uintptr(unsafe.Pointer(&iidShellBrowser)), uintptr(unsafe.Pointer(&iidShellBrowser)), uintptr(unsafe.Pointer(&psb)))) || psb == nil {
		return ""
	}
	defer psb.release()

	return getSelectedFromShellBrowser(psb)
}

func getSelectedFromShellBrowser(psb *comObj) string {
	var psv *comObj
	if failed(psb.call(sbQueryActiveShellView, uintptr(unsafe.Pointer(&psv)))) || psv == nil {
		return ""
	}
	defer psv.release()

	var dao *comObj
	if failed(psv.call(svGetItemObject, svgioSelection, uintptr(unsafe.Pointer(&iidDataObject)), uintptr(unsafe.Pointer(&dao)))) || dao == nil {
		return ""
	}
	defer dao.release()

	return obtainFirstItemFromDataObject(dao)
}

var cfShellIDList = func() uint16 {
	id, _, _ := procRegisterClipboardFormatW.Call(uintptr(unsafe.Pointer(wide("Shell IDList Array"))))
	return uint16(id)
}()

func obtainFirstItemFromDataObject(dao *comObj) string {
	fe := formatEtc{
		cfFormat: cfHdrop,
		dwAspect: dvaspectContent,
		lindex:   -1,
		tymed:    tymedHglobal,
	}
	var med stgMedium
	if !failed(dao.call(doGetData, uintptr(unsafe.Pointer(&fe)), uintptr(unsafe.Pointer(&med)))) {
		defer procReleaseStgMedium.Call(uintptr(unsafe.Pointer(&med)))
		cnt, _, _ := procDragQueryFileW.Call(med.hGlobal, 0xFFFFFFFF, 0, 0)
		if cnt >= 1 {
			var buf [1024]uint16
			n, _, _ := procDragQueryFileW.Call(med.hGlobal, 0, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
			if n > 0 {
				return toLongPath(syscall.UTF16ToString(buf[:n]))
			}
		}
	}

	if cfShellIDList == 0 {
		return ""
	}
	fe.cfFormat = cfShellIDList
	med = stgMedium{}
	if failed(dao.call(doGetData, uintptr(unsafe.Pointer(&fe)), uintptr(unsafe.Pointer(&med)))) {
		return ""
	}
	defer procReleaseStgMedium.Call(uintptr(unsafe.Pointer(&med)))

	ptr, _, _ := procGlobalLock.Call(med.hGlobal)
	if ptr == 0 {
		return ""
	}
	basePtr := foreignPtr(ptr)
	cidl := *(*uint32)(basePtr)
	if cidl < 1 {
		procGlobalUnlock.Call(med.hGlobal)
		return ""
	}
	offsets := unsafe.Slice((*uint32)(unsafe.Add(basePtr, 4)), int(cidl)+1)
	pidlFolder := unsafe.Add(basePtr, offsets[0])
	pidlItem := unsafe.Add(basePtr, offsets[1])
	pidlFull, _, _ := procILCombine.Call(uintptr(pidlFolder), uintptr(pidlItem))
	procGlobalUnlock.Call(med.hGlobal)
	if pidlFull == 0 {
		return ""
	}
	defer procILFree.Call(pidlFull)

	var item *comObj
	hr, _, _ := procSHCreateItemFromIDList.Call(pidlFull, uintptr(unsafe.Pointer(&iidShellItem)), uintptr(unsafe.Pointer(&item)))
	if failed(hr) || item == nil {
		return ""
	}
	defer item.release()

	var pszName uintptr
	if failed(item.call(siGetDisplayName, sigdnDesktopAbsParsing, uintptr(unsafe.Pointer(&pszName)))) || pszName == 0 {
		return ""
	}
	defer procCoTaskMemFree.Call(pszName)
	return utf16PtrToString(pszName)
}

func toLongPath(path string) string {
	if path == "" {
		return ""
	}
	var buf [2048]uint16
	n, _, _ := procGetLongPathNameW.Call(uintptr(unsafe.Pointer(wide(path))), uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	if n > 0 && int(n) < len(buf) {
		return syscall.UTF16ToString(buf[:n])
	}
	return path
}

// getSelectedFromDialog reads the current folder from the dialog's Address toolbar
// and combines it with the focused item's name via UI Automation (no DLL injection).
func getSelectedFromDialog(fg uintptr) string {
	var folder string
	enumChildWindows(fg, func(child uintptr) bool {
		if getWindowClass(child) == "ToolbarWindow32" {
			txt := getWindowText(child)
			for _, prefix := range []string{"Address: ", "地址: "} {
				if strings.HasPrefix(txt, prefix) {
					cand := strings.TrimSpace(strings.TrimPrefix(txt, prefix))
					if info, err := os.Stat(cand); err == nil && info.IsDir() {
						folder = cand
						return false
					}
				}
			}
		}
		return true
	})
	if folder == "" {
		return ""
	}

	itemName := runSTA(func() string {
		uia := createUIAutomation()
		if uia == nil {
			return ""
		}
		defer uia.release()
		var focused *comObj
		if failed(uia.call(uiaGetFocusedElement, uintptr(unsafe.Pointer(&focused)))) || focused == nil {
			return ""
		}
		defer focused.release()
		var nameBstr uintptr
		if failed(focused.call(uiaElGetCurrentName, uintptr(unsafe.Pointer(&nameBstr)))) || nameBstr == 0 {
			return ""
		}
		defer procSysFreeString.Call(nameBstr)
		return bstrToString(nameBstr)
	})
	if itemName == "" {
		return ""
	}
	full := filepath.Join(folder, itemName)
	if _, err := os.Stat(full); err == nil {
		return full
	}
	return ""
}

func getShellThumbnailWin(path string, size int) (*image.RGBA, error) {
	return getShellImageWin(path, size, siigbfResizeToFit)
}

// getShellIconWin asks only for the file's icon, skipping thumbnail handlers
// that can spend seconds loading an app's own preview.
func getShellIconWin(path string, size int) (*image.RGBA, error) {
	return getShellImageWin(path, size, siigbfIconOnly)
}

func getShellImageWin(path string, size int, flags uintptr) (*image.RGBA, error) {
	if path == "" || size <= 0 {
		return nil, nil
	}
	type thumbResult struct {
		img *image.RGBA
		err error
	}
	res := runSTA(func() thumbResult {
		var item *comObj
		hr, _, _ := procSHCreateItemFromParsingName.Call(
			uintptr(unsafe.Pointer(wide(path))),
			0,
			uintptr(unsafe.Pointer(&iidShellItem)),
			uintptr(unsafe.Pointer(&item)),
		)
		if failed(hr) || item == nil {
			return thumbResult{}
		}
		defer item.release()

		var factory *comObj
		if failed(item.call(comQueryInterface, uintptr(unsafe.Pointer(&iidShellItemImageFact)), uintptr(unsafe.Pointer(&factory)))) || factory == nil {
			return thumbResult{}
		}
		defer factory.release()

		packedSize := uintptr(uint32(size)) | (uintptr(uint32(size)) << 32)
		var hbm uintptr
		if failed(factory.call(siifGetImage, packedSize, flags, uintptr(unsafe.Pointer(&hbm)))) || hbm == 0 {
			return thumbResult{}
		}
		defer procDeleteObject.Call(hbm)

		return thumbResult{img: hbitmapToRGBA(hbm)}
	})
	return res.img, res.err
}

func hbitmapToRGBA(hbm uintptr) *image.RGBA {
	var bm bitmapHeader
	if n, _, _ := procGetObjectW.Call(hbm, unsafe.Sizeof(bm), uintptr(unsafe.Pointer(&bm))); n == 0 || bm.bmWidth <= 0 || bm.bmHeight <= 0 {
		return nil
	}
	w, h := int(bm.bmWidth), int(bm.bmHeight)
	dc, _, _ := procCreateCompatibleDC.Call(0)
	if dc == 0 {
		return nil
	}
	defer procDeleteDC.Call(dc)

	var bi bitmapInfo
	bi.bmiHeader.biSize = uint32(unsafe.Sizeof(bi.bmiHeader))
	bi.bmiHeader.biWidth = int32(w)
	bi.bmiHeader.biHeight = -int32(h) // top-down
	bi.bmiHeader.biPlanes = 1
	bi.bmiHeader.biBitCount = 32

	bgra := make([]byte, w*h*4)
	lines, _, _ := procGetDIBits.Call(dc, hbm, 0, uintptr(h), uintptr(unsafe.Pointer(&bgra[0])), uintptr(unsafe.Pointer(&bi)), 0)
	if lines == 0 {
		return nil
	}

	hasAlpha := false
	straight := false
	for i := 0; i < len(bgra); i += 4 {
		b, g, r, a := bgra[i], bgra[i+1], bgra[i+2], bgra[i+3]
		if a != 0 {
			hasAlpha = true
			if r > a || g > a || b > a {
				straight = true
			}
		}
	}

	out := image.NewRGBA(image.Rect(0, 0, w, h))
	for i := 0; i < len(bgra); i += 4 {
		b, g, r, a := bgra[i], bgra[i+1], bgra[i+2], bgra[i+3]
		if !hasAlpha {
			a = 255
		} else if straight && a < 255 {
			r = uint8((uint16(r)*uint16(a) + 127) / 255)
			g = uint8((uint16(g)*uint16(a) + 127) / 255)
			b = uint8((uint16(b)*uint16(a) + 127) / 255)
		}
		out.Pix[i] = r
		out.Pix[i+1] = g
		out.Pix[i+2] = b
		out.Pix[i+3] = a
	}
	return out
}
