//go:build !windows && (!linux || android)





package gamehost

import "unsafe"

func nativeMoveWindow(hwnd unsafe.Pointer, x, y int) {}

func nativeWindowPos(hwnd unsafe.Pointer) (int, int, bool) {
        return 0, 0, false
}

func nativeFullscreen(hwnd unsafe.Pointer, on bool, w, h int) {}

func nativeCenterWindow(hwnd unsafe.Pointer, pos string) {}

func nativeSetKeepAbove(hwnd unsafe.Pointer, on bool) {}

func nativeLowerWindow(hwnd unsafe.Pointer) {}

func nativeDialog(hwnd unsafe.Pointer, title, text string) bool {
        return false
}
