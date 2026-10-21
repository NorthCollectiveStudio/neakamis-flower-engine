package supervisor

import (
	"os"
	"strings"
	"sync"
)

// stderrTail keeps the last lines that the game host wrote to stderr.
//
// The stderr of the game host was written to the console of the shell,
// where nobody sees it: if the game host dies (on windows for example when
// the WebView2 runtime is missing) the shell only showed "game stopped"
// without a reason. The tail is shown in the shell and written into the log.
type stderrTail struct {
	mu   sync.Mutex
	buf  []byte
	max  int
	echo bool
}

func newStderrTail(max int) *stderrTail {
	if max <= 0 {
		max = 8192
	}
	return &stderrTail{max: max, echo: true}
}

func (t *stderrTail) Write(p []byte) (int, error) {
	t.mu.Lock()
	t.buf = append(t.buf, p...)
	if len(t.buf) > t.max {
		// Keep the last bytes: the error is at the end of the output.
		t.buf = append([]byte(nil), t.buf[len(t.buf)-t.max:]...)
	}
	t.mu.Unlock()
	if t.echo {
		os.Stderr.Write(p)
	}
	return len(p), nil
}

// lastError returns the line of the output that looks like the reason of a
// failure. The output of the web view is full of noise ("XGB:", "MESA:"),
// so the last line that mentions an error is preferred.
func (t *stderrTail) lastError() string {
	t.mu.Lock()
	text := string(t.buf)
	t.mu.Unlock()
	lines := strings.Split(text, "\n")
	noise := []string{"XGB:", "MESA-", "dbus", "Gtk-WARNING", "GLib-GObject", "libEGL", "gdk_window"}
	best := ""
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			continue
		}
		skip := false
		for _, n := range noise {
			if strings.HasPrefix(line, n) {
				skip = true
				break
			}
		}
		if skip {
			continue
		}
		low := strings.ToLower(line)
		if strings.Contains(low, "error") || strings.Contains(low, "failed") ||
			strings.Contains(low, "cannot") || strings.Contains(low, "missing") ||
			strings.Contains(low, "not supported") || strings.Contains(low, "fatal") {
			return line
		}
		if best == "" {
			best = line
		}
	}
	return best
}

// stderrSuffix returns the reason of the failure of the game host as a
// suffix for an error message.
func stderrSuffix(t *stderrTail) string {
	if t == nil {
		return ""
	}
	line := t.lastError()
	if line == "" {
		return ""
	}
	return ": " + line
}

// LastGameError returns the last error line of the game host. The shell
// shows it when the game stops.
func (m *Manager) LastGameError() string {
	m.mu.Lock()
	t := m.stderrTail
	m.mu.Unlock()
	if t == nil {
		return ""
	}
	return t.lastError()
}
