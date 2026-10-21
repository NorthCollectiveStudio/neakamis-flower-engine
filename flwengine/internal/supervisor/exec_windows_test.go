//go:build windows

package supervisor

import (
	"os"
	"path/filepath"
	"testing"
)

func TestUnblockExecutableIsSafe(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "flwengine.exe")
	if err := os.WriteFile(p, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	// No mark of the web: must not fail and must not touch the file.
	unblockExecutable(p)
	data, err := os.ReadFile(p)
	if err != nil || string(data) != "x" {
		t.Errorf("the file was modified: %v %q", err, string(data))
	}
}
