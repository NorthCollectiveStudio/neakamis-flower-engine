//go:build !windows && !linux





package guard

import "errors"

func debuggerPresent() string {
	return ""
}

func lockRegion(b []byte) error {
	return errors.New("unsupported platform")
}
