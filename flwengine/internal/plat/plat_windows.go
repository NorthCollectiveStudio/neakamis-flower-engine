//go:build windows



package plat

import (
	"golang.org/x/sys/windows/registry"
	"strings"
	"syscall"
	"unsafe"
)

func windowsVersion() string {
	k, err := registry.OpenKey(registry.LOCAL_MACHINE, `SOFTWARE\Microsoft\Windows NT\CurrentVersion`, registry.QUERY_VALUE)
	if err == nil {
		defer k.Close()
		if v, _, err := k.GetStringValue("DisplayVersion"); err == nil && v != "" {
			return v
		}
		if v, _, err := k.GetStringValue("CurrentBuild"); err == nil {
			maj, _, _ := k.GetIntegerValue("CurrentMajorVersionNumber")
			return itoa(int(maj)) + "." + v
		}
	}
	var major, minor, build uint32
	r, _, _ := syscall.NewLazyDLL("ntdll.dll").NewProc("RtlGetNtVersionNumbers").Call(
		uintptr(unsafe.Pointer(&major)), uintptr(unsafe.Pointer(&minor)), uintptr(unsafe.Pointer(&build)))
	_ = r
	return itoa(int(major)) + "." + itoa(int(minor)) + "." + strings.TrimSuffix(itoa(int(build)), "\x00")
}

func wineVersionWindows() (string, bool) {
	ntdll := syscall.NewLazyDLL("ntdll.dll")
	proc := ntdll.NewProc("wine_get_version")
	if proc.Find() != nil {
		return "", false
	}
	type wineVersion struct {
		major int32
		minor int32
		build int32
		str   [64]byte
	}
	var v wineVersion
	_, _, _ = proc.Call(uintptr(unsafe.Pointer(&v)))
	ver := itoa(int(v.major)) + "." + itoa(int(v.minor)) + "-" + itoa(int(v.build))
	return ver, true
}

func itoa(v int) string {
	if v == 0 {
		return "0"
	}
	neg := v < 0
	if neg {
		v = -v
	}
	var b [20]byte
	i := len(b)
	for v > 0 {
		i--
		b[i] = byte('0' + v%10)
		v /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}
