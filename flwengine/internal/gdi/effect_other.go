//go:build !windows





package gdi

import (
	"errors"
)

var (
	errWine          = errors.New("gdi effects disabled under wine")
	errNoWindow      = errors.New("gdi window not ready")
	errUnknownEffect = errors.New("unknown gdi effect")
)

func (m *Manager) Start(targetHwnd uintptr) bool {
	return false
}

func (m *Manager) Stop() {
}
