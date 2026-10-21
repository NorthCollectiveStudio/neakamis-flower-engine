//go:build !flwnogui && !android




package gamehost

import (
	"sync/atomic"
        "encoding/base64"
        "encoding/json"
        "errors"
        "fmt"
        "strconv"
        "sync"
        "time"

        webview "github.com/webview/webview_go"

        "flwengine/internal/bridge"
)















const (
        shakeQuiet = iota
        // shakePending: the first move of a possible shake. The position we
        // are leaving is remembered as the base, but the move is not undone
        // yet: a single move (the apidemos window demo) must stay where it
        // was put.
        shakePending
        // shakeActive: a second move followed within shakeEnterWindow, so this
        // really is a shake. The window is snapped back to the base when the
        // moves stop.
        shakeActive
)

const (
        shakeEnterWindow = 250 * time.Millisecond
        shakeSnapAfter   = 180 * time.Millisecond
)

type webWindow struct {
        wv     webview.WebView
        mu     sync.Mutex
        host   *Host
        brg    *bridge.Bridge
        closed bool
        winX     int
        winY     int
        winKnown bool
        fsActive bool

        
        
        shakeMode      int
        shakeBaseX     int
        shakeBaseY     int
        shakeBaseKnown bool
        shakeNeedBase  bool 
        shakeLast      time.Time
        snapTimer      *time.Timer
        hungLast       time.Time
        syncPendingAt  int64
}

var activeWindow *webWindow
var winMu sync.Mutex

func (ww *webWindow) setPos(x, y int) {
        ww.mu.Lock()
        ww.winX, ww.winY, ww.winKnown = x, y, true
        ww.mu.Unlock()
}

func (ww *webWindow) getPos() (int, int, bool) {
        ww.mu.Lock()
        defer ww.mu.Unlock()
        return ww.winX, ww.winY, ww.winKnown
}



func (ww *webWindow) applyMove(x, y int) {
        ww.mu.Lock()
        needBase := ww.shakeNeedBase
        ww.shakeNeedBase = false
        ww.mu.Unlock()
        ww.setPos(x, y)
        ww.wv.Dispatch(func() {
                hwnd := ww.wv.Window()
                if needBase {
                        
                        
                        if rx, ry, ok := nativeWindowPos(hwnd); ok {
                                ww.mu.Lock()
                                ww.shakeBaseX, ww.shakeBaseY, ww.shakeBaseKnown = rx, ry, true
                                ww.mu.Unlock()
                        }
                }
                nativeMoveWindow(hwnd, x, y)
                if rx, ry, ok := nativeWindowPos(hwnd); ok {
                        ww.setPos(rx, ry)
                }
        })
}


func (ww *webWindow) armSnapLocked() {
        d := shakeSnapAfter
        if ww.shakeMode == shakePending {
                // No follow-up move yet: after shakeEnterWindow the move is
                // considered isolated and the base is dropped.
                d = shakeEnterWindow
        }
        if ww.snapTimer != nil {
                ww.snapTimer.Reset(d)
                return
        }
        ww.snapTimer = time.AfterFunc(d, func() {
                ww.mu.Lock()
                mode := ww.shakeMode
                if mode == shakePending {
                        // An isolated move: keep it, forget the base.
                        ww.shakeMode = shakeQuiet
                        ww.shakeBaseKnown = false
                        ww.mu.Unlock()
                        return
                }
                if mode != shakeActive {
                        ww.mu.Unlock()
                        return
                }
                ww.shakeMode = shakeQuiet
                bx, by, known := ww.shakeBaseX, ww.shakeBaseY, ww.shakeBaseKnown
                ww.shakeBaseKnown = false
                ww.mu.Unlock()
                if !known {
                        return
                }
                ww.applyMove(bx, by)
                ww.pushPosToPage(bx, by)
        })
}



func (ww *webWindow) pushPosToPage(x, y int) {
        js := "window.__flwSyncWinPos&&window.__flwSyncWinPos(" + strconv.Itoa(x) + "," + strconv.Itoa(y) + ")"
        ww.wv.Dispatch(func() {
                ww.wv.Eval(js)
        })
}

// syncPosFromWM reads the real window position and, when the window
// manager moved it, updates the cache and tells the page.
//
// At most one sync is in flight: the dispatch queue of the web view must
// not grow without a limit when the UI thread is busy.
func (ww *webWindow) syncPosFromWM() {
        // At most one sync is in flight so that the dispatch queue of the
        // web view cannot grow without a limit. A dispatch that never runs
        // (the UI thread is stuck) must not stop the sync for good, so an
        // in flight sync is given up after a second.
        now := time.Now().UnixNano()
        since := atomic.LoadInt64(&ww.syncPendingAt)
        if since != 0 && now-since < int64(time.Second) {
                return
        }
        atomic.StoreInt64(&ww.syncPendingAt, now)
        ww.wv.Dispatch(func() {
                defer atomic.StoreInt64(&ww.syncPendingAt, 0)
                hwnd := ww.wv.Window()
                rx, ry, ok := nativeWindowPos(hwnd)
                if !ok {
                        return
                }
                cx, cy, known := ww.getPos()
                if known && cx == rx && cy == ry {
                        return
                }
                ww.setPos(rx, ry)
                ww.pushPosToPage(rx, ry)
        })
}

func (h *Host) MoveGameWindow(x, y int) {
        winMu.Lock()
        ww := activeWindow
        winMu.Unlock()
        if ww == nil {
                return
        }
        now := time.Now()
        ww.mu.Lock()
        if ww.fsActive {
                ww.mu.Unlock()
                return
        }
        switch ww.shakeMode {
        case shakeActive:
                // The window is being shaken: keep it around the base and
                // (re)start the timer that snaps it back.
                ww.shakeLast = now
                ww.armSnapLocked()
                ww.mu.Unlock()
                ww.applyMove(x, y)
                return
        case shakePending:
                // The second move of a shake: now it is certain that this
                // is a shake and not a single move.
                ww.shakeMode = shakeActive
                ww.shakeLast = now
                ww.armSnapLocked()
                ww.mu.Unlock()
                ww.applyMove(x, y)
                return
        }
        // The first move of a possible shake. The position we are leaving
        // is remembered as the base, but the move is not undone yet: a
        // single move (the apidemos window demo) has to stay where it was
        // put. If a second move follows within shakeEnterWindow the move
        // becomes a shake and the window is snapped back to the base.
        ww.shakeMode = shakePending
        ww.shakeLast = now
        ww.shakeNeedBase = true
        ww.armSnapLocked()
        ww.mu.Unlock()
        ww.applyMove(x, y)
}

func (h *Host) GameWindowPos() (int, int, bool) {
        winMu.Lock()
        ww := activeWindow
        winMu.Unlock()
        if ww == nil {
                return 0, 0, false
        }
        return ww.getPos()
}


func (h *Host) CenterGameWindow(pos string) {
        winMu.Lock()
        ww := activeWindow
        winMu.Unlock()
        if ww == nil {
                return
        }
        ww.wv.Dispatch(func() {
                hwnd := ww.wv.Window()
                nativeCenterWindow(hwnd, pos)
                if rx, ry, ok := nativeWindowPos(hwnd); ok {
                        ww.setPos(rx, ry)
                        ww.pushPosToPage(rx, ry)
                }
        })
}


func (h *Host) SetGameWindowAlwaysOnTop(on bool) {
        winMu.Lock()
        ww := activeWindow
        winMu.Unlock()
        if ww == nil {
                return
        }
        ww.wv.Dispatch(func() {
                nativeSetKeepAbove(ww.wv.Window(), on)
        })
}




func (h *Host) LowerGameWindow() {
        winMu.Lock()
        ww := activeWindow
        winMu.Unlock()
        if ww == nil {
                return
        }
        ww.wv.Dispatch(func() {
                nativeLowerWindow(ww.wv.Window())
        })
}

func (h *Host) SetGameWindowSize(w, hh int, fullscreen bool) {
        winMu.Lock()
        ww := activeWindow
        winMu.Unlock()
        if ww == nil {
                return
        }
        ww.mu.Lock()
        wasFs := ww.fsActive
        ww.fsActive = fullscreen
        if fullscreen {
                
                ww.shakeMode = shakeQuiet
                if ww.snapTimer != nil {
                        ww.snapTimer.Stop()
                        ww.snapTimer = nil
                }
        }
        ww.mu.Unlock()
        ww.wv.Dispatch(func() {
                if fullscreen {
                        nativeFullscreen(ww.wv.Window(), true, 0, 0)
                        return
                }
                if wasFs {
                        nativeFullscreen(ww.wv.Window(), false, w, hh)
                }
                if w > 0 && hh > 0 {
                        ww.wv.SetSize(w, hh, webview.HintNone)
                }
        })
}

func (h *Host) ShowGameDialog(title, text string) bool {
        winMu.Lock()
        ww := activeWindow
        winMu.Unlock()
        if ww == nil {
                return false
        }
        if title == "" {
                title = "System"
        }
        ww.wv.Dispatch(func() {
                nativeDialog(ww.wv.Window(), title, text)
        })
        return true
}

func (h *Host) BridgeJS() string {
        return bridge.JS()
}

func (h *Host) EvalJS(js string) string {
        winMu.Lock()
        ww := activeWindow
        winMu.Unlock()
        if ww == nil {
                return ""
        }
        ww.wv.Dispatch(func() {
                ww.wv.Eval(js)
        })
        return "dispatched"
}

func (h *Host) EvalResult(js string) (string, error) {
        winMu.Lock()
        ww := activeWindow
        winMu.Unlock()
        if ww == nil {
                return "", errors.New("no game window")
        }
        id := randToken(10)
        ch := make(chan map[string]string, 1)
        h.mu.Lock()
        if h.evalPending == nil {
                h.evalPending = map[string]chan map[string]string{}
        }
        h.evalPending[id] = ch
        h.mu.Unlock()
        defer func() {
                h.mu.Lock()
                delete(h.evalPending, id)
                h.mu.Unlock()
        }()
        // The evaluated snippet may return a Promise (StorageManager.loadObject
        // and every other async API does). Awaiting it here makes the RPC
        // channel - and therefore the dev console - report the real value
        // instead of the string "[object Promise]".
        wrapper := "(function(){var id='" + id + "';function send(o){try{window.flwNativeCall('event',JSON.stringify({name:'eval.result',data:JSON.stringify(o)}))}catch(e){}}"
        wrapper += "try{var r=eval(window.atob(" + jsB64(js) + "));"
        wrapper += "if(r&&typeof r.then==='function'){r.then(function(v){send({id:id,result:String(v)})},function(e){send({id:id,error:String(e)})})}"
        wrapper += "else{send({id:id,result:String(r)})}}catch(e){send({id:id,error:String(e)})}})()"
        h.EvalJS(wrapper)
        select {
        case res := <-ch:
                if e, ok := res["error"]; ok {
                        return "", errors.New(e)
                }
                return res["result"], nil
        case <-time.After(8 * time.Second):
                
                
                
                h.notifyUnresponsive(ww)
                return "", errors.New("eval timeout")
        }
}


func (h *Host) notifyUnresponsive(ww *webWindow) {
        ww.mu.Lock()
        if time.Since(ww.hungLast) < 30*time.Second {
                ww.mu.Unlock()
                return
        }
        ww.hungLast = time.Now()
        ww.mu.Unlock()
        h.log.Warn("gamehost", "eval timeout — game not responding to JS")
        h.mu.Lock()
        cb := h.onHung
        h.mu.Unlock()
        if cb != nil {
            go cb()
        }
}

func (h *Host) OnHung(f func()) {
        h.mu.Lock()
        h.onHung = f
        h.mu.Unlock()
}

func jsB64(js string) string {
        return strconv.Quote(base64.StdEncoding.EncodeToString([]byte(js)))
}

func (h *Host) HandleEvent(name string, data string) {
        if name != "eval.result" {
                return
        }
        var ev struct {
                ID     string `json:"id"`
                Result string `json:"result"`
                Error  string `json:"error"`
        }
        if err := json.Unmarshal([]byte(data), &ev); err != nil {
                return
        }
        h.mu.Lock()
        ch := h.evalPending[ev.ID]
        h.mu.Unlock()
        if ch != nil {
                m := map[string]string{}
                if ev.Error != "" {
                        m["error"] = ev.Error
                } else {
                        m["result"] = ev.Result
                }
                ch <- m
        }
}

func (h *Host) RequestExit() {
        winMu.Lock()
        ww := activeWindow
        winMu.Unlock()
        h.mu.Lock()
        already := h.exitFlag
        h.exitFlag = true
        h.mu.Unlock()
        if ww != nil && !already {
                ww.wv.Dispatch(func() {
                        ww.wv.Terminate()
                })
        }
}

func (h *Host) SetGameWindowTitle(title string) {
        winMu.Lock()
        ww := activeWindow
        winMu.Unlock()
        if ww != nil {
                ww.wv.Dispatch(func() { ww.wv.SetTitle(title) })
        }
}

func (h *Host) RunWindow(title string, w int, hh int, br *bridge.Bridge) error {
        wv := webview.New(false)
        defer wv.Destroy()
        if wv.Window() == nil {
                // On windows the web view needs the runtime of Microsoft
                // (WebView2). If it is not installed, no window is created:
                // the game would stay invisible without any message.
                h.log.Crit("gamehost", "the web view could not be created - on windows the "+
                        "Microsoft Edge WebView2 runtime is needed "+
                        "(https://developer.microsoft.com/microsoft-edge/webview2/)")
                return fmt.Errorf("the web view could not be created (the WebView2 runtime is missing?)")
        }
        wv.SetTitle(title)
        if w <= 0 {
                w = 816
        }
        if hh <= 0 {
                hh = 624
        }
        wv.SetSize(w, hh, webview.HintNone)
        wv.Bind("flwNativeCall", func(name string, args string) (string, error) {
                res := br.Handle(name, json.RawMessage(args))
                out, err := json.Marshal(res)
                if err != nil {
                        return `{"ok":false,"e":"marshal"}`, nil
                }
                return string(out), nil
        })
        ww := &webWindow{wv: wv, host: h, brg: br}
        winMu.Lock()
        activeWindow = ww
        winMu.Unlock()
        if x, y, ok := nativeWindowPos(wv.Window()); ok {
                ww.setPos(x, y)
        }
        attachGDI(wv.Window(), br)
        defer detachGDI()
        // The window manager can move the window on its own (the user drags
        // it, the WM re-cascades it when another window appears). The page
        // caches the position, and the game reads that cache once to compute
        // the base of its screen shake - a stale cache made the window jump
        // to a completely different position. The real position is therefore
        // tracked continuously and pushed to the page.
        stopSync := make(chan struct{})
        go func() {
                // 75 ms: the page caches the window position and the game reads that
                // cache once to compute the base of its screen shake. A stale
                // cache would make the window jump to an old position, so
                // the position is kept fresh.
                ticker := time.NewTicker(75 * time.Millisecond)
                defer ticker.Stop()
                for {
                        select {
                        case <-stopSync:
                                return
                        case <-ticker.C:
                                ww.syncPosFromWM()
                        }
                }
        }()
        defer close(stopSync)
        wv.Navigate(h.BootURL())
        h.log.Info("gamehost", "window opened: %s", h.BootURL())
        wv.Run()
        winMu.Lock()
        activeWindow = nil
        winMu.Unlock()
        ww.mu.Lock()
        if ww.snapTimer != nil {
                ww.snapTimer.Stop()
                ww.snapTimer = nil
        }
        ww.mu.Unlock()
        h.mu.Lock()
        exitCb := h.onExit
        h.mu.Unlock()
        if exitCb != nil {
                exitCb(0)
        }
        return nil
}

func (h *Host) OnExit(f func(code int)) {
        h.mu.Lock()
        h.onExit = f
        h.mu.Unlock()
}
