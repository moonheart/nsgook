//go:build windows

package devviewers

import (
	"fmt"
	"os"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

var (
	versionDLL  = syscall.NewLazyDLL("version.dll")
	shell32DLL  = syscall.NewLazyDLL("shell32.dll")
	kernelDLL   = syscall.NewLazyDLL("kernel32.dll")
	wintrustDLL = syscall.NewLazyDLL("wintrust.dll")

	procGetFileVersionInfoSizeW      = versionDLL.NewProc("GetFileVersionInfoSizeW")
	procGetFileVersionInfoW          = versionDLL.NewProc("GetFileVersionInfoW")
	procVerQueryValueW               = versionDLL.NewProc("VerQueryValueW")
	procSHGetFileInfoW               = shell32DLL.NewProc("SHGetFileInfoW")
	procGetFileInformationByHandleEx = kernelDLL.NewProc("GetFileInformationByHandleEx")
	procWinVerifyTrust               = wintrustDLL.NewProc("WinVerifyTrust")
)

// readPEFileInfo reads the same shell-facing metadata Windows shows in a
// file's General and Details tabs, except its signature: verifying that hashes
// the whole file, so InspectPE fills it in later.
func readPEFileInfo(path string, st os.FileInfo) peWindowsInfo {
	var info peWindowsInfo
	info.FileType = shellTypeName(path)
	readFileVersionInfo(path, &info)

	if data, ok := st.Sys().(*syscall.Win32FileAttributeData); ok {
		info.Created = filetimeString(data.CreationTime)
		info.Modified = filetimeString(data.LastWriteTime)
		info.Accessed = filetimeString(data.LastAccessTime)
		info.Attributes = attributesString(data.FileAttributes)
	} else if !st.ModTime().IsZero() {
		info.Modified = st.ModTime().Format("2006-01-02 15:04:05")
	}

	info.SizeOnDisk = allocatedFileSize(path, st.Size())
	info.Security = zoneSecurity(path)
	return info
}

func filetimeString(ft syscall.Filetime) string {
	if ft.Nanoseconds() == 0 {
		return ""
	}
	return time.Unix(0, ft.Nanoseconds()).Local().Format("2006-01-02 15:04:05")
}

func attributesString(attr uint32) string {
	const (
		attrReadonly  = 0x0001
		attrHidden    = 0x0002
		attrSystem    = 0x0004
		attrCompress  = 0x0800
		attrEncrypted = 0x4000
	)
	var parts []string
	for _, f := range []struct {
		flag uint32
		name string
	}{
		{attrReadonly, "Read-only"},
		{attrHidden, "Hidden"},
		{attrSystem, "System"},
		{attrCompress, "Compressed"},
		{attrEncrypted, "Encrypted"},
	} {
		if attr&f.flag != 0 {
			parts = append(parts, f.name)
		}
	}
	if len(parts) == 0 {
		return "Normal"
	}
	return strings.Join(parts, ", ")
}

type shFileInfo struct {
	hIcon         uintptr
	iIcon         int32
	dwAttributes  uint32
	szDisplayName [260]uint16
	szTypeName    [80]uint16
}

func shellTypeName(path string) string {
	p, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return ""
	}
	var info shFileInfo
	const shgfiTypeName = 0x400
	r, _, _ := procSHGetFileInfoW.Call(
		uintptr(unsafe.Pointer(p)),
		0,
		uintptr(unsafe.Pointer(&info)),
		unsafe.Sizeof(info),
		shgfiTypeName,
	)
	if r == 0 {
		return ""
	}
	return syscall.UTF16ToString(info.szTypeName[:])
}

type fileStandardInfo struct {
	AllocationSize int64
	EndOfFile      int64
	NumberOfLinks  uint32
	DeletePending  uint8
	Directory      uint8
	_              [2]byte
}

func allocatedFileSize(path string, _ int64) uint64 {
	f, err := os.Open(path)
	if err != nil {
		return 0
	}
	defer f.Close()
	var info fileStandardInfo
	const fileStandardInfoClass = 1
	r, _, _ := procGetFileInformationByHandleEx.Call(
		f.Fd(),
		fileStandardInfoClass,
		uintptr(unsafe.Pointer(&info)),
		unsafe.Sizeof(info),
	)
	if r == 0 || info.AllocationSize < 0 {
		return 0
	}
	return uint64(info.AllocationSize)
}

func zoneSecurity(path string) string {
	data, err := os.ReadFile(path + ":Zone.Identifier")
	if err != nil {
		return ""
	}
	zone := -1
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "ZoneId=") {
			_, _ = fmt.Sscanf(line, "ZoneId=%d", &zone)
		}
	}
	switch zone {
	case 0:
		return "Local machine"
	case 1:
		return "Local intranet"
	case 2:
		return "Trusted intranet"
	case 3:
		return "Downloaded from the Internet (blocked)"
	case 4:
		return "Restricted site (blocked)"
	default:
		return "Marked from another computer"
	}
}

func verQueryValue(block []byte, sub string, out *unsafe.Pointer, size *uint32) bool {
	if len(block) == 0 {
		return false
	}
	subPtr, err := syscall.UTF16PtrFromString(sub)
	if err != nil {
		return false
	}
	r, _, _ := procVerQueryValueW.Call(
		uintptr(unsafe.Pointer(&block[0])),
		uintptr(unsafe.Pointer(subPtr)),
		uintptr(unsafe.Pointer(out)),
		uintptr(unsafe.Pointer(size)),
	)
	return r != 0
}

func versionString(block []byte, lang, codepage uint16, name string) string {
	var p unsafe.Pointer
	var n uint32
	sub := fmt.Sprintf(`\StringFileInfo\%04x%04x\%s`, lang, codepage, name)
	if !verQueryValue(block, sub, &p, &n) || p == nil {
		return ""
	}
	offset := int(uintptr(p) - uintptr(unsafe.Pointer(&block[0])))
	if offset < 0 || offset >= len(block) {
		return ""
	}
	count := (len(block) - offset) / 2
	if count > 1<<15 {
		count = 1 << 15
	}
	if count <= 0 {
		return ""
	}
	return syscall.UTF16ToString(unsafe.Slice((*uint16)(p), count))
}

func readFileVersionInfo(path string, info *peWindowsInfo) {
	p, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return
	}
	size, _, _ := procGetFileVersionInfoSizeW.Call(uintptr(unsafe.Pointer(p)), 0)
	if size == 0 {
		return
	}
	block := make([]byte, size)
	ok, _, _ := procGetFileVersionInfoW.Call(
		uintptr(unsafe.Pointer(p)),
		0,
		size,
		uintptr(unsafe.Pointer(&block[0])),
	)
	if ok == 0 {
		return
	}

	var pairs [][2]uint16
	var trans unsafe.Pointer
	var transLen uint32
	if verQueryValue(block, `\VarFileInfo\Translation`, &trans, &transLen) && trans != nil && transLen >= 4 {
		for _, d := range unsafe.Slice((*uint32)(trans), int(transLen/4)) {
			pairs = append(pairs, [2]uint16{uint16(d & 0xFFFF), uint16(d >> 16)})
		}
	}
	// Fall back to common translations when the table is missing or unusual.
	pairs = append(pairs,
		[2]uint16{0x0409, 0x04B0},
		[2]uint16{0x0000, 0x04B0},
		[2]uint16{0x0804, 0x04B0},
		[2]uint16{0x0409, 0x04E4},
		[2]uint16{0x0804, 0x04E4},
	)

	for _, pair := range pairs {
		desc := versionString(block, pair[0], pair[1], "FileDescription")
		if desc == "" {
			continue
		}
		info.Description = strings.TrimSpace(desc)
		info.Version = strings.TrimSpace(versionString(block, pair[0], pair[1], "FileVersion"))
		info.ProductName = strings.TrimSpace(versionString(block, pair[0], pair[1], "ProductName"))
		info.ProductVersion = strings.TrimSpace(versionString(block, pair[0], pair[1], "ProductVersion"))
		info.Company = strings.TrimSpace(versionString(block, pair[0], pair[1], "CompanyName"))
		info.Copyright = strings.TrimSpace(versionString(block, pair[0], pair[1], "LegalCopyright"))
		info.OriginalName = strings.TrimSpace(versionString(block, pair[0], pair[1], "OriginalFilename"))
		info.Language = languageName(pair[0])
		return
	}
}

func languageName(lang uint16) string {
	switch lang {
	case 0x0000:
		return "Language Neutral"
	case 0x0409:
		return "English (United States)"
	case 0x0804:
		return "Chinese (Simplified)"
	case 0x0404:
		return "Chinese (Traditional)"
	case 0x0411:
		return "Japanese"
	case 0x0412:
		return "Korean"
	case 0x0407:
		return "German"
	case 0x040C:
		return "French"
	case 0x0410:
		return "Italian"
	case 0x0419:
		return "Russian"
	default:
		return fmt.Sprintf("Language 0x%04X", lang)
	}
}

type winTrustGUID struct {
	Data1 uint32
	Data2 uint16
	Data3 uint16
	Data4 [8]byte
}

var winTrustActionGenericVerifyV2 = winTrustGUID{
	Data1: 0x00AAC56B,
	Data2: 0xCD44,
	Data3: 0x11D0,
	Data4: [8]byte{0x8C, 0xC2, 0x00, 0xC0, 0x4F, 0xC2, 0x95, 0xEE},
}

type winTrustFileInfo struct {
	cbStruct       uint32
	pcwszFilePath  *uint16
	hFile          uintptr
	pgKnownSubject *winTrustGUID
}

type winTrustData struct {
	cbStruct            uint32
	pPolicyCallbackData uintptr
	pSIPClientData      uintptr
	dwUIChoice          uint32
	fdwRevocationChecks uint32
	dwUnionChoice       uint32
	pFile               uintptr
	dwStateAction       uint32
	hWVTStateData       uintptr
	pwszURLReference    *uint16
	dwProvFlags         uint32
	dwUIContext         uint32
	pSignatureSettings  uintptr
}

func verifyAuthenticode(path string) string {
	p, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return ""
	}
	fileInfo := winTrustFileInfo{
		cbStruct:      uint32(unsafe.Sizeof(winTrustFileInfo{})),
		pcwszFilePath: p,
	}
	data := winTrustData{
		cbStruct:            uint32(unsafe.Sizeof(winTrustData{})),
		dwUIChoice:          2, // WTD_UI_NONE
		fdwRevocationChecks: 0, // WTD_REVOKE_NONE
		dwUnionChoice:       1, // WTD_CHOICE_FILE
		pFile:               uintptr(unsafe.Pointer(&fileInfo)),
		dwStateAction:       1,      // WTD_STATEACTION_VERIFY
		dwProvFlags:         0x1000, // WTD_CACHE_ONLY_URL_RETRIEVAL
	}
	hr, _, _ := procWinVerifyTrust.Call(
		0,
		uintptr(unsafe.Pointer(&winTrustActionGenericVerifyV2)),
		uintptr(unsafe.Pointer(&data)),
	)
	data.dwStateAction = 2 // WTD_STATEACTION_CLOSE
	procWinVerifyTrust.Call(
		0,
		uintptr(unsafe.Pointer(&winTrustActionGenericVerifyV2)),
		uintptr(unsafe.Pointer(&data)),
	)

	switch uint32(hr) {
	case 0:
		return "Valid"
	case 0x800B0100, 0x800B0003, 0x800B0004:
		return "Not signed"
	case 0x800B0109:
		return "Untrusted root"
	case 0x800B010A:
		return "Untrusted chain"
	case 0x800B0101:
		return "Expired certificate"
	case 0x80096010:
		return "Invalid signature"
	case 0x800B0110:
		return "Wrong usage"
	default:
		return fmt.Sprintf("Error 0x%08X", uint32(hr))
	}
}
