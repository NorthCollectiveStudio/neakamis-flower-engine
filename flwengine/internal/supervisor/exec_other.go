//go:build !windows

package supervisor

// unblockExecutables is a no-op outside Windows (the Mark-of-the-Web only
// exists there).
func unblockExecutables(paths ...string) {}

// unblockExecutable is a no-op outside Windows.
func unblockExecutable(path string) {}
