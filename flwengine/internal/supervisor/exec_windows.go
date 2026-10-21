//go:build windows

package supervisor

import (
	"os"
)

// unblockExecutable removes the Mark-of-the-Web (the Zone.Identifier
// alternate data stream) from a file.
//
// A build that was sent as an archive (Telegram, e-mail, a cloud drive) keeps
// the "downloaded from the internet" mark, and Windows then refuses to run
// it. Depending on the system policy the failure looks like "The request is
// not supported" (ERROR_NOT_SUPPORTED) or "Access is denied" - the game
// simply does not start and the shell shows a red launch error.
//
// Removing the stream is the standard remedy and is safe: it only affects
// the copy of the file next to the engine.
func unblockExecutable(path string) {
	if path == "" {
		return
	}
	ads := path + ":Zone.Identifier"
	if _, err := os.Stat(ads); err != nil {
		return
	}
	_ = os.Remove(ads)
}

// unblockExecutables removes the mark from the engine and the game host.
func unblockExecutables(paths ...string) {
	for _, p := range paths {
		if p == "" {
			continue
		}
		unblockExecutable(p)
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
			unblockExecutable(p + ".exe")
		}
	}
}
