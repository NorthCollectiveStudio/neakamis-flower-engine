//go:build flwnogui && !android





package gamehost

import (
        "errors"

        "flwengine/internal/bridge"
)

func (h *Host) BridgeJS() string {
        return bridge.JS()
}

func (h *Host) EvalJS(js string) string {
        return ""
}

func (h *Host) HandleEvent(name string, data string) {
}

func (h *Host) EvalResult(js string) (string, error) {
        return "", errors.New("eval unavailable in nogui mode")
}

func (h *Host) RequestExit() {
        h.mu.Lock()
        h.exitFlag = true
        h.mu.Unlock()
}

func (h *Host) SetGameWindowTitle(title string) {}

func (h *Host) MoveGameWindow(x, y int) {}

func (h *Host) GameWindowPos() (int, int, bool) { return 0, 0, false }

func (h *Host) CenterGameWindow(pos string) {}

func (h *Host) SetGameWindowAlwaysOnTop(on bool) {}

func (h *Host) LowerGameWindow() {}

func (h *Host) SetGameWindowSize(w, hh int, fullscreen bool) {}

func (h *Host) ShowGameDialog(title, text string) bool { return false }

func (h *Host) RunWindow(title string, w int, hh int, br *bridge.Bridge) error {
        return errors.New("window mode not supported in nogui mode")
}

func (h *Host) OnExit(f func(code int)) {
        h.mu.Lock()
        h.onExit = f
        h.mu.Unlock()
}

func (h *Host) OnHung(f func()) {
        h.mu.Lock()
        h.onHung = f
        h.mu.Unlock()
}
