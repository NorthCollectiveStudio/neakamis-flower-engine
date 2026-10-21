package gamehost

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
)

var desktopOnce sync.Once
var desktopPath string

func homeDir() string {
	if h, err := os.UserHomeDir(); err == nil && h != "" {
		return h
	}
	return os.Getenv("HOME")
}

func firstExistingDir(candidates []string) string {
	for _, c := range candidates {
		if c == "" {
			continue
		}
		if st, err := os.Stat(c); err == nil && st.IsDir() {
			return c
		}
	}
	return ""
}

func xdgDesktopFromUserDirs() string {
	home := homeDir()
	if home == "" {
		return ""
	}
	raw, err := os.ReadFile(filepath.Join(home, ".config", "user-dirs.dirs"))
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "XDG_DESKTOP_DIR=") {
			continue
		}
		v := strings.Trim(strings.TrimPrefix(line, "XDG_DESKTOP_DIR="), "\"'")
		v = strings.ReplaceAll(v, "$HOME", home)
		if v != "" && v != home {
			return v
		}
	}
	return ""
}

func detectDesktopDir() string {
	home := homeDir()
	candidates := []string{}
	if v := os.Getenv("XDG_DESKTOP_DIR"); v != "" {
		candidates = append(candidates, v)
	}
	candidates = append(candidates, xdgDesktopFromUserDirs())
	if home != "" {
		candidates = append(candidates,
			filepath.Join(home, "Desktop"),
			filepath.Join(home, "Рабочий стол"),
			filepath.Join(home, "desktop"),
		)
	}
	if p := windowsDesktopDir(); p != "" {
		candidates = append([]string{p}, candidates...)
	}
	return firstExistingDir(candidates)
}

func DesktopDir() string {
	desktopOnce.Do(func() {
		desktopPath = detectDesktopDir()
	})
	return desktopPath
}

var hostWriteExts = map[string]bool{
	".png": true, ".jpg": true, ".jpeg": true, ".webp": true, ".txt": true,
}

func isHostPathAbs(p string) bool {
	if p == "" {
		return false
	}
	if strings.HasPrefix(p, "/") || strings.HasPrefix(p, "\\") || strings.HasPrefix(p, "~") {
		return true
	}
	if len(p) > 2 && p[1] == ':' && (p[2] == '\\' || p[2] == '/') {
		return true
	}
	return false
}

func resolveHostPath(p string) string {
	if strings.HasPrefix(p, "~") {
		if h := homeDir(); h != "" {
			p = filepath.Join(h, strings.TrimPrefix(p, "~"))
		}
	}
	if !isHostPathAbs(p) {
		return ""
	}
	abs, err := filepath.Abs(filepath.Clean(strings.ReplaceAll(p, "\\", string(filepath.Separator))))
	if err != nil {
		return ""
	}
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		return resolved
	}
	return abs
}

func hostAllowedContainer() []string {
	out := []string{}
	if d := DesktopDir(); d != "" {
		out = append(out, d)
	}
	return out
}

func pathInAllowedDir(p string) bool {
	for _, d := range hostAllowedContainer() {
		if d == "" {
			continue
		}
		rel, err := filepath.Rel(d, p)
		if err != nil {
			continue
		}
		if rel == "." || strings.HasPrefix(rel, "..") || filepath.IsAbs(rel) {
			if rel == "." {
				return true
			}
			continue
		}
		if strings.ContainsRune(rel, filepath.Separator) {
			return false
		}
		return true
	}
	return false
}

func HostExistsAllowed(path string) bool {
	if !isHostPathAbs(path) {
		return false
	}
	full := resolveHostPath(path)
	if full == "" {
		return false
	}
	if !pathInAllowedDir(full) {
		return false
	}
	_, err := os.Stat(full)
	return err == nil
}

func HostDirAllowed(path string) bool {
	full := resolveHostPath(path)
	if full == "" {
		return false
	}
	for _, d := range hostAllowedContainer() {
		if d != "" && full == d {
			return true
		}
	}
	return false
}

func HostWriteAllowed(path string, data []byte) error {
	if !isHostPathAbs(path) {
		return errHostDenied
	}
	full := resolveHostPath(path)
	if full == "" || !pathInAllowedDir(full) {
		return errHostDenied
	}
	ext := strings.ToLower(filepath.Ext(full))
	if !hostWriteExts[ext] {
		return errHostDenied
	}
	if len(data) > 16<<20 {
		return errHostDenied
	}
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return err
	}
	return os.WriteFile(full, data, 0o644)
}
