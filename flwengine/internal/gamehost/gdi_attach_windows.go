//go:build windows



package gamehost

import (
	"unsafe"

	"flwengine/internal/bridge"
	"flwengine/internal/gdi"
	"flwengine/internal/plat"
)

type unsafeNative = unsafe.Pointer

var gdiStop func()

func attachGDI(nativeWindow unsafeNative, br *bridge.Bridge) {
	info, _ := plat.Detect()
	mgr := gdi.NewManager(info.Wine)
	if mgr.Start(uintptr(nativeWindow)) {
		bridge.SetGDIBackend(mgr.Handle)
		gdiStop = mgr.Stop
	}
}

func detachGDI() {
	if gdiStop != nil {
		gdiStop()
		gdiStop = nil
	}
}
