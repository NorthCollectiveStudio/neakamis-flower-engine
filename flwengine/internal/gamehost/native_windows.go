//go:build windows

package gamehost

import (
        "strings"
        "syscall"
        "unsafe"
)

type winRect struct {
        Left, Top, Right, Bottom int32
}

type monitorInfo struct {
        CbSize    uint32
        RcMonitor winRect
        RcWork    winRect
        DwFlags   uint32
}

var (
        u32               = syscall.NewLazyDLL("user32.dll")
        pMoveWindow       = u32.NewProc("MoveWindow")
        pGetWindowRect    = u32.NewProc("GetWindowRect")
        pMonitorFromWindow = u32.NewProc("MonitorFromWindow")
        pGetMonitorInfoW  = u32.NewProc("GetMonitorInfoW")
        pSetWindowPos     = u32.NewProc("SetWindowPos")
        pGetWindowLongPtrW = u32.NewProc("GetWindowLongPtrW")
        pSetWindowLongPtrW = u32.NewProc("SetWindowLongPtrW")
        pMessageBoxW      = u32.NewProc("MessageBoxW")
        pGetSystemMetrics = u32.NewProc("GetSystemMetrics")
)

const (
        gwlStyle   = uintptr(^uint64(15)) 
        wsCaption  = uintptr(0x00C00000)
        wsThickFr  = uintptr(0x00040000)
        swpNoZ     = uintptr(0x0004)
        swpFrameCh = uintptr(0x0020)
        swpShowWin = uintptr(0x0040)
        smCxScreen = 0
        smCyScreen = 1
)

const (
        hwndTopmost    = ^uintptr(0)
        hwndNoTopmost  = ^uintptr(1)
        swpNoActivate  = uintptr(0x0010)
        swpNoMove      = uintptr(0x0002)
        swpNoSize      = uintptr(0x0001)
        // SWP_ASYNCWINDOWPOS: apply the change without sending synchronous
        // cross-thread window messages. Without it, a z-order/activation
        // change (topmost/lower/center) can block the webview UI thread
        // forever waiting for another process's frozen window thread to pump
        // messages — the "everything hangs" mutual deadlock.
        swpAsync       = uintptr(0x4000)
)

func nativeMoveWindow(hwnd unsafe.Pointer, x, y int) {
        if hwnd == nil {
                return
        }
        var rc winRect
        r, _, _ := pGetWindowRect.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&rc)))
        if r == 0 {
                return
        }
        w := int(rc.Right - rc.Left)
        h := int(rc.Bottom - rc.Top)
        pMoveWindow.Call(uintptr(hwnd), uintptr(int32(x)), uintptr(int32(y)), uintptr(w), uintptr(h), 1)
}

func nativeWindowPos(hwnd unsafe.Pointer) (int, int, bool) {
        if hwnd == nil {
                return 0, 0, false
        }
        var rc winRect
        r, _, _ := pGetWindowRect.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&rc)))
        if r == 0 {
                return 0, 0, false
        }
        return int(rc.Left), int(rc.Top), true
}



func nativeCenterWindow(hwnd unsafe.Pointer, pos string) {
        if hwnd == nil {
                return
        }
        var rc winRect
        r, _, _ := pGetWindowRect.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&rc)))
        if r == 0 {
                return
        }
        w := int(rc.Right - rc.Left)
        h := int(rc.Bottom - rc.Top)
        
        mi := monitorInfo{}
        mi.CbSize = uint32(unsafe.Sizeof(mi))
        mon, _, _ := pMonitorFromWindow.Call(uintptr(hwnd), 1)
        var ax, ay, aw, ah int
        gotInfo := false
        if mon != 0 {
            if r1, _, _ := pGetMonitorInfoW.Call(mon, uintptr(unsafe.Pointer(&mi))); r1 != 0 {
                ax, ay = int(mi.RcWork.Left), int(mi.RcWork.Top)
                aw, ah = int(mi.RcWork.Right-mi.RcWork.Left), int(mi.RcWork.Bottom-mi.RcWork.Top)
                gotInfo = true
            }
        }
        if !gotInfo {
                cx, _, _ := pGetSystemMetrics.Call(smCxScreen)
                cy, _, _ := pGetSystemMetrics.Call(smCyScreen)
                ax, ay, aw, ah = 0, 0, int(cx), int(cy)
        }
        x := ax + (aw-w)/2
        y := ay + (ah-h)/2
        pSetWindowPos.Call(uintptr(hwnd), 0, uintptr(int32(x)), uintptr(int32(y)), uintptr(w), uintptr(h), swpNoZ|swpNoActivate|swpAsync|swpFrameCh|swpShowWin)
}


func nativeSetKeepAbove(hwnd unsafe.Pointer, on bool) {
        if hwnd == nil {
                return
        }
        var rc winRect
        r, _, _ := pGetWindowRect.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&rc)))
        if r == 0 {
                return
        }
        w := int(rc.Right - rc.Left)
        h := int(rc.Bottom - rc.Top)
        after := hwndNoTopmost
        if on {
                after = hwndTopmost
        }
        pSetWindowPos.Call(uintptr(hwnd), after, uintptr(int32(rc.Left)), uintptr(int32(rc.Top)), uintptr(w), uintptr(h), swpNoActivate|swpAsync|swpShowWin)
}



func nativeLowerWindow(hwnd unsafe.Pointer) {
        if hwnd == nil {
                return
        }
        
        pSetWindowPos.Call(uintptr(hwnd), 1, 0, 0, 0, 0, swpNoActivate|swpNoMove|swpNoSize|swpAsync)
}

func nativeFullscreen(hwnd unsafe.Pointer, on bool, w, h int) {
        if hwnd == nil {
                return
        }
        if on {
                style, _, _ := pGetWindowLongPtrW.Call(uintptr(hwnd), gwlStyle)
                pSetWindowLongPtrW.Call(uintptr(hwnd), gwlStyle, style&^(wsCaption|wsThickFr))
                mi := monitorInfo{}
                mi.CbSize = uint32(unsafe.Sizeof(mi))
                mon, _, _ := pMonitorFromWindow.Call(uintptr(hwnd), 1) 
                if mon != 0 {
                        pGetMonitorInfoW.Call(mon, uintptr(unsafe.Pointer(&mi)))
                        pSetWindowPos.Call(uintptr(hwnd), 0,
                                uintptr(mi.RcMonitor.Left), uintptr(mi.RcMonitor.Top),
                                uintptr(mi.RcMonitor.Right-mi.RcMonitor.Left),
                                uintptr(mi.RcMonitor.Bottom-mi.RcMonitor.Top),
                                swpNoZ|swpNoActivate|swpAsync|swpFrameCh|swpShowWin)
                } else {
                        cx, _, _ := pGetSystemMetrics.Call(smCxScreen)
                        cy, _, _ := pGetSystemMetrics.Call(smCyScreen)
                        pSetWindowPos.Call(uintptr(hwnd), 0, 0, 0, cx, cy, swpNoZ|swpNoActivate|swpAsync|swpFrameCh|swpShowWin)
                }
        } else {
                style, _, _ := pGetWindowLongPtrW.Call(uintptr(hwnd), gwlStyle)
                pSetWindowLongPtrW.Call(uintptr(hwnd), gwlStyle, style|wsCaption|wsThickFr)
                if w <= 0 {
                        w = 816
                }
                if h <= 0 {
                        h = 624
                }
                var rc winRect
                if r, _, _ := pGetWindowRect.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&rc))); r != 0 {
                        cx := (int(rc.Right-rc.Left) - w) / 2
                        cy := (int(rc.Bottom-rc.Top) - h) / 2
                        pSetWindowPos.Call(uintptr(hwnd), 0, uintptr(int32(int(rc.Left)+cx)), uintptr(int32(int(rc.Top)+cy)),
                                uintptr(w), uintptr(h), swpNoZ|swpNoActivate|swpAsync|swpFrameCh|swpShowWin)
                } else {
                        pSetWindowPos.Call(uintptr(hwnd), 0, 80, 80, uintptr(w), uintptr(h), swpNoZ|swpNoActivate|swpAsync|swpFrameCh|swpShowWin)
                }
        }
}

func nativeDialog(hwnd unsafe.Pointer, title, text string) bool {
        var ph uintptr
        if hwnd != nil {
                ph = uintptr(hwnd)
        }
        t, _ := syscall.UTF16PtrFromString(title)
        x, _ := syscall.UTF16PtrFromString(strings.ReplaceAll(text, "\x00", ""))
        
        go func() {
                defer func() { recover() }()
                pMessageBoxW.Call(ph, uintptr(unsafe.Pointer(x)), uintptr(unsafe.Pointer(t)), 0x40) 
        }()
        return true
}
