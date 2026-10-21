//go:build windows



package guard

import (
        "fmt"
        "unsafe"

        "golang.org/x/sys/windows"
)

var (
        procIsDebuggerPresent     *windows.LazyProc
        procCheckRemote           *windows.LazyProc
        procNtQueryInformationProc *windows.LazyProc
        debuggerProcsOnce         bool
)

func initProcs() {
        if debuggerProcsOnce {
                return
        }
        debuggerProcsOnce = true
        k32 := windows.NewLazySystemDLL("kernel32.dll")
        ntdll := windows.NewLazySystemDLL("ntdll.dll")
        procIsDebuggerPresent = k32.NewProc("IsDebuggerPresent")
        procCheckRemote = k32.NewProc("CheckRemoteDebuggerPresent")
        procNtQueryInformationProc = ntdll.NewProc("NtQueryInformationProcess")
}

func debuggerPresent() string {
        initProcs()
        if procIsDebuggerPresent.Find() == nil {
                if r, _, _ := procIsDebuggerPresent.Call(); r != 0 {
                        return "isdebuggerpresent"
                }
        }
        if procCheckRemote.Find() == nil {
                var flag int32
                h := windows.CurrentProcess()
                if r, _, _ := procCheckRemote.Call(uintptr(h), uintptr(unsafe.Pointer(&flag))); r == 0 && flag != 0 {
                        return "checkremotedebuggerpresent"
                }
        }
        if procNtQueryInformationProc.Find() == nil {
                h := windows.CurrentProcess()
                if r := debugPort(h); r != "" {
                        return r
                }
                if r := debugFlags(h); r != "" {
                        return r
                }
                if r := debugObject(h); r != "" {
                        return r
                }
        }
        return ""
}

func debugPort(h windows.Handle) string {
        var val uintptr
        r, _, _ := procNtQueryInformationProc.Call(uintptr(h), 7, uintptr(unsafe.Pointer(&val)), unsafe.Sizeof(val), 0)
        if r == 0 && val != 0 {
                return "ntdebugport"
        }
        return ""
}

func debugFlags(h windows.Handle) string {
        var val uint32
        r, _, _ := procNtQueryInformationProc.Call(uintptr(h), 0x1f, uintptr(unsafe.Pointer(&val)), unsafe.Sizeof(val), 0)
        if r == 0 && val == 0 {
                return "ntdebugflags"
        }
        return ""
}

func debugObject(h windows.Handle) string {
        var val uintptr
        r, _, _ := procNtQueryInformationProc.Call(uintptr(h), 0x1e, uintptr(unsafe.Pointer(&val)), unsafe.Sizeof(val), 0)
        if r == 0 && val != 0 {
                return "ntdebugobject"
        }
        return ""
}

func lockRegion(b []byte) error {
        if len(b) == 0 {
                return nil
        }
        allocBase := uintptr(unsafe.Pointer(unsafe.SliceData(b)))
        page := uintptr(4096)
        base := allocBase & ^(page - 1)
        size := (allocBase + uintptr(len(b)) - base + page - 1) & ^(page - 1)
        k32 := windows.NewLazySystemDLL("kernel32.dll")
        procVL := k32.NewProc("VirtualLock")
        r, _, err := procVL.Call(base, size)
        if r == 0 {
                return fmt.Errorf("virtuallock: %v", err)
        }
        return nil
}
