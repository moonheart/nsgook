//go:build windows

package sys

import (
	"strconv"
	"strings"
	"sync"
	"syscall"
	"unsafe"
)

var (
	ole32    = syscall.NewLazyDLL("ole32.dll")
	oleaut32 = syscall.NewLazyDLL("oleaut32.dll")
	shell32  = syscall.NewLazyDLL("shell32.dll")
	shlwapi  = syscall.NewLazyDLL("shlwapi.dll")
	user32   = syscall.NewLazyDLL("user32.dll")
	kernel32 = syscall.NewLazyDLL("kernel32.dll")
	gdi32    = syscall.NewLazyDLL("gdi32.dll")

	procCoInitializeEx              = ole32.NewProc("CoInitializeEx")
	procCoCreateInstance            = ole32.NewProc("CoCreateInstance")
	procCoTaskMemFree               = ole32.NewProc("CoTaskMemFree")
	procReleaseStgMedium            = ole32.NewProc("ReleaseStgMedium")
	procSysStringLen                = oleaut32.NewProc("SysStringLen")
	procSysFreeString               = oleaut32.NewProc("SysFreeString")
	procVariantInit                 = oleaut32.NewProc("VariantInit")
	procDragQueryFileW              = shell32.NewProc("DragQueryFileW")
	procILCombine                   = shell32.NewProc("ILCombine")
	procILFree                      = shell32.NewProc("ILFree")
	procSHCreateItemFromIDList      = shell32.NewProc("SHCreateItemFromIDList")
	procSHCreateItemFromParsingName = shell32.NewProc("SHCreateItemFromParsingName")
	procShellExecuteW               = shell32.NewProc("ShellExecuteW")
	procAssocQueryStringW           = shlwapi.NewProc("AssocQueryStringW")
	procGlobalLock                  = kernel32.NewProc("GlobalLock")
	procGlobalUnlock                = kernel32.NewProc("GlobalUnlock")
	procGetLongPathNameW            = kernel32.NewProc("GetLongPathNameW")
	procGetDiskFreeSpaceExW         = kernel32.NewProc("GetDiskFreeSpaceExW")
	procOpenProcess                 = kernel32.NewProc("OpenProcess")
	procCloseHandle                 = kernel32.NewProc("CloseHandle")
	procQueryFullProcessImageNameW  = kernel32.NewProc("QueryFullProcessImageNameW")
	procGetCurrentThreadId          = kernel32.NewProc("GetCurrentThreadId")
)

const (
	coinitApartmentThreaded = 0x2
	clsctxInprocServer      = 0x1
	clsctxLocalServer       = 0x4
	clsctxServer            = clsctxInprocServer | clsctxLocalServer

	vtEmpty = 0
	vtI4    = 3

	cfHdrop         = 15
	dvaspectContent = 1
	tymedHglobal    = 1

	swcDesktop       = 8
	swfoNeedDispatch = 1

	svgioSelection         = 1
	sigdnDesktopAbsParsing = 0x80028000
	siigbfResizeToFit      = 0x0
	siigbfIconOnly         = 0x4
)

type guid struct {
	a    uint32
	b, c uint16
	d    [8]byte
}

func parseGUID(s string) guid {
	h := strings.ReplaceAll(s, "-", "")
	word := func(from, to int) uint64 {
		n, err := strconv.ParseUint(h[from:to], 16, 64)
		if err != nil {
			panic("bad GUID " + s)
		}
		return n
	}
	g := guid{a: uint32(word(0, 8)), b: uint16(word(8, 12)), c: uint16(word(12, 16))}
	for i := range g.d {
		g.d[i] = byte(word(16+2*i, 18+2*i))
	}
	return g
}

var (
	clsidShellWindows     = parseGUID("9BA05972-F6A8-11CF-A442-00A0C90A8F39")
	iidShellWindows       = parseGUID("85CB6900-4D95-11CF-960C-0080C7F4EE85")
	iidServiceProvider    = parseGUID("6d5140c1-7436-11ce-8034-00aa006009fa")
	sidTopLevelBrowser    = parseGUID("4C96BE40-915C-11CF-99D3-00AA004AE837")
	iidShellBrowser       = parseGUID("000214E2-0000-0000-C000-000000000046")
	iidShellView          = parseGUID("000214E3-0000-0000-C000-000000000046")
	iidDataObject         = parseGUID("0000010e-0000-0000-C000-000000000046")
	iidShellItem          = parseGUID("43826d1e-e718-42ee-bc55-a1e261c37bfe")
	iidShellItemImageFact = parseGUID("bcc18b79-ba16-442f-80c4-8a59c30c463b")

	clsidCUIAutomation8  = parseGUID("e22ad333-b25f-460c-83d0-0581107395c9")
	clsidCUIAutomation   = parseGUID("ff48dba4-60ef-4201-aa87-54103eef594e")
	iidIUIAutomation     = parseGUID("30cbe57d-d9d0-452a-ab13-7ac5ac4825ee")
	iidIUIASelectionItem = parseGUID("a8efa66a-0fda-421a-9194-38021f3578ea")
	iidIUIAValuePattern  = parseGUID("a94cd8b1-0844-4cd6-9d2d-640537ab39e9")
)

// COM vtable method slots verified against Windows SDK 10.0.26100.0 headers.
const (
	comQueryInterface = 0
	comAddRef         = 1
	comRelease        = 2

	swGetCount     = 7
	swItem         = 8
	swFindWindowSW = 15

	spQueryService = 3

	sbGetWindow            = 3
	sbQueryActiveShellView = 15

	svGetItemObject = 15

	doGetData = 3

	siGetDisplayName = 5

	siifGetImage = 3

	uiaElementFromHandle   = 6
	uiaGetFocusedElement   = 8
	uiaCreateTrueCondition = 21
	uiaCreatePropCondition = 23
	uiaCreateOrCondition   = 28

	uiaElFindFirst           = 5
	uiaElFindAll             = 6
	uiaElGetCurrentPatternAs = 14
	uiaElGetCurrentPid       = 20
	uiaElGetCurrentCtrlType  = 21
	uiaElGetCurrentName      = 23
	uiaElGetCurrentClass     = 30

	uiaArrGetLength  = 3
	uiaArrGetElement = 4

	uiaSelGetIsSelected = 6
	uiaValGetValue      = 4
)

type comObj struct {
	vtbl *[96]uintptr
}

//go:uintptrescapes
func (o *comObj) call(method int, args ...uintptr) uintptr {
	var buf [12]uintptr
	all := append(buf[:0], uintptr(unsafe.Pointer(o)))
	r, _, _ := syscall.SyscallN(o.vtbl[method], append(all, args...)...)
	return r
}

func (o *comObj) release() {
	if o != nil {
		o.call(comRelease)
	}
}

func failed(hr uintptr) bool { return int32(hr) < 0 }

type variant struct {
	vt         uint16
	wReserved1 uint16
	wReserved2 uint16
	wReserved3 uint16
	val        uintptr
	pad        uintptr
}

type formatEtc struct {
	cfFormat uint16
	_        uint16
	ptd      uintptr
	dwAspect uint32
	lindex   int32
	tymed    uint32
}

type stgMedium struct {
	tymed          uint32
	hGlobal        uintptr
	pUnkForRelease uintptr
}

// staWorker runs COM calls on a single locked STA thread.
var (
	staOnce sync.Once
	staCh   chan func()
)

func runSTA[T any](fn func() T) T {
	staOnce.Do(func() {
		staCh = make(chan func(), 16)
		ready := make(chan struct{})
		go func() {
			// Lock this goroutine to one OS thread initialized as COM STA.
			// Shell COM objects (IShellWindows, IShellItemImageFactory) require an STA thread.
			lockOSThread()
			procCoInitializeEx.Call(0, coinitApartmentThreaded)
			close(ready)
			for task := range staCh {
				task()
			}
		}()
		<-ready
	})
	done := make(chan T, 1)
	staCh <- func() {
		done <- fn()
	}
	return <-done
}

func wide(s string) *uint16 {
	p, err := syscall.UTF16PtrFromString(s)
	if err != nil {
		var zero uint16
		return &zero
	}
	return p
}

// foreignPtr turns a foreign Win32 address held in a uintptr into an unsafe.Pointer
// using the indirection pattern accepted by go vet.
func foreignPtr(addr uintptr) unsafe.Pointer {
	return *(*unsafe.Pointer)(unsafe.Pointer(&addr))
}

func utf16PtrToString(p uintptr) string {
	if p == 0 {
		return ""
	}
	return syscall.UTF16ToString((*[1 << 15]uint16)(foreignPtr(p))[:])
}

func bstrToString(bstr uintptr) string {
	if bstr == 0 {
		return ""
	}
	n, _, _ := procSysStringLen.Call(bstr)
	if n == 0 {
		return ""
	}
	slice := unsafe.Slice((*uint16)(foreignPtr(bstr)), int(n))
	return syscall.UTF16ToString(slice)
}
