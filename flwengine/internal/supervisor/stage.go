package supervisor

import (
	"os"
	"path/filepath"
)

// stageExecutable copies an executable to a temporary directory and returns
// the copy.
//
// Some places refuse to execute files: a cloud drive with placeholders that
// were never downloaded, a folder with a "mark of the web", a network share.
// When the direct start fails with such an error, running a copy from the
// temporary directory usually works.
func stageExecutable(path string) (string, bool) {
	if path == "" {
		return "", false
	}
	src, err := os.Open(path)
	if err != nil {
		return "", false
	}
	defer src.Close()
	dir, err := os.MkdirTemp("", "flwexe-")
	if err != nil {
		return "", false
	}
	dst := filepath.Join(dir, filepath.Base(path))
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		os.RemoveAll(dir)
		return "", false
	}
	if _, err := out.ReadFrom(src); err != nil {
		out.Close()
		os.RemoveAll(dir)
		return "", false
	}
	out.Close()
	// The copy is a local temporary file: it must not carry the mark of the
	// web either (windows only, a no-op elsewhere).
	unblockExecutable(dst)
	return dst, true
}
