//go:build !windows





package gamehost

import (
	"unsafe"

	"flwengine/internal/bridge"
)

type unsafeNative = unsafe.Pointer

func attachGDI(nativeWindow unsafe.Pointer, br *bridge.Bridge) {
}

func detachGDI() {
}
