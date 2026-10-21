package supervisor

import (
	"os"
	"path/filepath"
	"testing"
)

// A build that cannot be executed from its folder (a cloud drive with
// placeholders, a mark of the web) is started from a copy in the temporary
// directory.
func TestStageExecutable(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "flwengine.exe")
	if err := os.WriteFile(src, []byte("MZ pretend binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	staged, ok := stageExecutable(src)
	if !ok {
		t.Fatal("stageExecutable failed")
	}
	defer os.RemoveAll(filepath.Dir(staged))
	if filepath.Base(staged) != "flwengine.exe" {
		t.Errorf("unexpected staged name %q", filepath.Base(staged))
	}
	data, err := os.ReadFile(staged)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "MZ pretend binary" {
		t.Errorf("staged content mismatch: %q", string(data))
	}
	// A second call must not fail.
	if _, ok := stageExecutable(src); !ok {
		t.Error("the second stageExecutable failed")
	}
}

func TestStageExecutableOfAMissingFile(t *testing.T) {
	if _, ok := stageExecutable("/does/not/exist"); ok {
		t.Error("stageExecutable of a missing file must fail")
	}
	if _, ok := stageExecutable(""); ok {
		t.Error("stageExecutable of an empty path must fail")
	}
}
