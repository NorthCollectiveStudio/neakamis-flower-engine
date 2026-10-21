//go:build !windows





package gamehost

import "errors"

var errHostDenied = errors.New("host path not allowed")

func windowsDesktopDir() string {
	return ""
}
