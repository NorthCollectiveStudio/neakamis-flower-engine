//go:build !windows





package plat

func windowsVersion() string { return "" }

func wineVersionWindows() (string, bool) { return "", false }
