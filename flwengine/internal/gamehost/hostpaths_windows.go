//go:build windows



package gamehost

import (
	"errors"
	"syscall"
	"unicode/utf16"
	"unsafe"
)

var errHostDenied = errors.New("host path not allowed")

var (
	shell32              = syscall.NewLazyDLL("shell32.dll")
	ole32                = syscall.NewLazyDLL("ole32.dll")
	procSHGetKnownFolder = shell32.NewProc("SHGetKnownFolderPath")
	procCoTaskMemFree    = ole32.NewProc("CoTaskMemFree")
	knownFolderDesktop   = syscall.GUID{Data1: 0xB4BFCC3A, Data2: 0xDB2C, Data3: 0x424C, Data4: [8]byte{0xB0, 0x29, 0x7F, 0xE9, 0x9A, 0x87, 0xC6, 0x41}}
)

func windowsDesktopDir() string {
	var pathPtr *uint16
	ret, _, _ := procSHGetKnownFolder.Call(
		uintptr(unsafe.Pointer(&knownFolderDesktop)),
		0,
		0,
		uintptr(unsafe.Pointer(&pathPtr)),
	)
	if ret != 0 || pathPtr == nil {
		return ""
	}
	defer procCoTaskMemFree.Call(uintptr(unsafe.Pointer(pathPtr)))
	raw := (*[1 << 16]uint16)(unsafe.Pointer(pathPtr))
	n := 0
	for n < 1<<16 && raw[n] != 0 {
		n++
	}
	if n == 0 || n >= 1<<16 {
		return ""
	}
	return string(utf16.Decode(raw[:n]))
}
