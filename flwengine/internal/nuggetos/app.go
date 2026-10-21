package nuggetos

import (
        "net/http"
        _ "net/http/pprof"
        "net"
        "fmt"
        "os"
        "path/filepath"
        "strconv"
        "strings"
        "sync/atomic"
        "time"

        "github.com/hajimehoshi/ebiten/v2"

        "flwengine/internal/cryptx"
        "flwengine/internal/guard"
        "flwengine/internal/nklog"
        "flwengine/internal/supervisor"
)

const (
        LogicalW = 1280
        LogicalH = 720
)

type appState int

const (
        stBoot appState = iota
        stSplash
        stDashboard
        stSettings
        stDebug
        stConsole
        stDevConsole
        stRunning
)

var exitLogger *nklog.Writer

func Run() int {
        dataDir := "flwdata"
        if v := os.Getenv("FLW_DATA"); v != "" {
                dataDir = v
        }
        absData, _ := filepath.Abs(dataDir)
        _ = absData
        os.MkdirAll(filepath.Join(absData, "logs"), 0o755)
        cryptx.Init()
        logger, logErr := nklog.New(filepath.Join(absData, "logs", "supervisor.nklog"), currentFeatures().DevComponents)
        if logErr != nil || logger == nil {
                // A nil writer would panic on the first log call (and on
                // Close()). Fall back to a temporary log file.
                fmt.Fprintf(os.Stderr, "nuggetos: cannot open the log file (%v), using %s\n", logErr, os.TempDir())
                logger, _ = nklog.New(filepath.Join(os.TempDir(), "flwengine-fallback.nklog"), true)
        }
        if logger == nil {
                logger = nklog.Discard()
        }
        defer logger.Close()
        exitLogger = logger
        if currentFeatures().Guard {
                if err := guard.Enable(logger); err != nil {
                        logger.Crit("guard", "init failed: %v", err)
                        fmt.Fprintf(os.Stderr, "guard init failed: %v\n", err)
                        return 1
                }
        }
        if os.Getenv("FLW_PPROF") != "" {
                ln, perr := net.Listen("tcp", "127.0.0.1:6060")
                if perr == nil {
                        go http.Serve(ln, nil)
                }
        }
        supervisor.Init(absData, logger)
        mgr := supervisor.M()
        eventCh = make(chan gameEvent, 16)
        mgr.SetEventCallbacks(func(name, data string) {
                logger.Info("game", "event %s %s", name, data)
                select {
                case eventCh <- gameEvent{name: name, data: data}:
                default:
                }
        }, func() {
                select {
                case eventCh <- gameEvent{name: "game.exit"}:
                default:
                }
        }, func(note string) {
                logger.Info("hotreload", "%s", note)
                select {
                case eventCh <- gameEvent{name: "hotreload", data: note}:
                default:
                }
        })
        mgr.StartCtrl()
        st := mgr.Settings()
        initSound(st.Volume)
        logger.Info("audio", "sounds of the engine: %s", soundBackendName())
        if isHeadlessBuild() {
                
                return runHeadless(logger)
        }
        if currentFeatures().HotReload && st.HotReloadOn {
                if err := mgr.StartHotReload(); err != nil {
                        logger.Warn("hotreload", "auto start failed: %v", err)
                } else {
                        logger.Info("hotreload", "auto started on %s", mgr.HotReloadAddr())
                }
        }
        
        ebiten.SetCursorMode(ebiten.CursorModeHidden)
        if os.Getenv("FLW_NOVSYNC") != "" {
                vsyncDefault = false
                vsyncCur = false
                ebiten.SetVsyncEnabled(false)
        }
        startWatchdog(logger)
        a := newApp()
        a.start()
        ebiten.SetWindowSize(LogicalW, LogicalH)
        ebiten.SetWindowTitle("NuggetOS")
        ebiten.SetTPS(60)
        err := ebiten.RunGame(a)
        if err != nil && err != ebiten.Termination && isAudioError(err) {
                
                
                logger.Warn("audio", "audio device failed (%v), continuing without ui sounds", err)
                fmt.Fprintf(os.Stderr, "nuggetos: audio disabled: %v\n", err)
                snd = nil
                err = ebiten.RunGame(a)
        }
        if err != nil && err != ebiten.Termination {
                fmt.Fprintf(os.Stderr, "nuggetos run failed: %v\n", err)
                return 1
        }
        if a.exitReason != "" {
                logger.Info("supervisor", "engine exit: %s", a.exitReason)
        }
        return 0
}



func isAudioError(err error) bool {
        if err == nil {
                return false
        }
        s := err.Error()
        return strings.Contains(s, "oto:") || strings.Contains(s, "ALSA") ||
                strings.Contains(s, "audio") || strings.Contains(s, "snd_pcm") ||
                strings.Contains(s, "wasapi") || strings.Contains(s, "coreaudio") ||
                strings.Contains(s, "OpenAL") || strings.Contains(s, "oss")
}

func newApp() *app {
        a := &app{lastT: time.Now(),
                state:      stBoot,
                bootScr:    newBootScreen(),
                dashScr:    newDashboard(),
                setScr:     newSettingsScreen(),
                dbgScr:     newDebugScreen(),
                conScr:     newConsoleScreen(),
                devScr:     newDevConsole(),
                running:    newRunningScreen(),
                splashScr:  newSplashScreen(),
        }
        return a
}

type gameEvent struct {
        name string
        data string
}

var eventCh chan gameEvent


var windowHidden bool
var vsyncDefault = true
var vsyncCur = true

// gameWindowExists reports whether a game/gamehost window may be on screen.
// While one is, the shell must NEVER render vsync'ed: on Windows/DWM a
// SwapBuffers on an occluded window waits for a vblank that never comes and
// the UI loop dies ("виснет намертво"); worse, the gamehost's synchronous
// window ops then block against the frozen shell thread -> mutual deadlock,
// both windows hang. vsync is restored only when no game is running.
func gameWindowExists() bool {
        st := supervisor.M().State()
        return st == supervisor.StateRunning || st == supervisor.StateLaunching || st == supervisor.StateStopping
}

func applyVsync(v bool) {
        if v && !vsyncDefault {
                v = false
        }
        if v == vsyncCur {
                return
        }
        vsyncCur = v
        ebiten.SetVsyncEnabled(v)
}

func hideWindow() {
        if !windowHidden {
                windowHidden = true
                // vsync must be off while offscreen: a swap on a hidden window
                // waits for a vblank that never arrives (session-10 lesson)
                applyVsync(false)
                ebiten.SetWindowPosition(-8000, -8000)
        }
}

func showWindow() {
        showWindowVsync(!gameWindowExists())
}

// ---- ui-loop watchdog ----------------------------------------------------
// Escape hatch for any residual freeze we cannot reproduce headless: if the
// render loop stalls for a long time while a game window exists (occluded
// swap block, driver quirks, ...), kill the game - its window disappears,
// the blocked swap returns, the shell heals and tells the user what happened.
// The stall must persist across several checks so a slow machine or a
// suspend/resume (wall-clock jump + healthy beat right after) never trips it.
// FLW_WATCHDOG_MS overrides the stall threshold (0 disables).
//
// The beat uses a MONOTONIC clock: a wall-clock jump (NTP step, manual
// change, suspend/resume) must not be able to fake a stall.
var uiBeat int64
var uiEpoch = time.Now()
var watchdogKilled int32

func watchdogThreshold() time.Duration {
        if v := os.Getenv("FLW_WATCHDOG_MS"); v != "" {
                if n, err := strconv.Atoi(v); err == nil {
                        if n <= 0 {
                                return 0
                        }
                        return time.Duration(n) * time.Millisecond
                }
        }
        // 15s: a loaded software renderer can spend seconds in a frame, and
        // a healthy game must never be killed because of that.
        return 15 * time.Second
}

func startWatchdog(logger *nklog.Writer) {
        go func() {
                var misses int
                for {
                        time.Sleep(3 * time.Second)
                        if !gameWindowExists() {
                                misses = 0
                                continue
                        }
                        thr := watchdogThreshold()
                        if thr <= 0 {
                                continue
                        }
                        last := atomic.LoadInt64(&uiBeat)
                        if last == 0 {
                                misses = 0
                                continue
                        }
                        age := time.Since(uiEpoch.Add(time.Duration(last)))
                        if age < thr {
                                misses = 0
                                continue
                        }
                        misses++
                        if misses < 4 {
                                continue
                        }
                        logger.Crit("watchdog", "ui loop stalled %.1fs with a game running - force-stopping the game to unfreeze the shell", age.Seconds())
                        atomic.StoreInt32(&watchdogKilled, 1)
                        supervisor.M().KillGame()
                        misses = 0
                }
        }()
}









func showWindowVsync(vsync bool) {
        if windowHidden {
                windowHidden = false
                ebiten.SetWindowPosition(80, 80)
        }
        applyVsync(vsync)
}

type app struct {
        lastT      time.Time
        state      appState
        prevState  appState
        bootScr    *bootScreen
        dashScr    *dashboard
        setScr     *settingsScreen
        dbgScr     *debugScreen
        conScr     *consoleScreen
        devScr     *devConsole
        running    *runningScreen
        splashScr  *splashScreen
        toast      string
        toastTTL   float64
        toastDanger bool
        fadeIn     float64
        reloading  bool
        exitReq    bool
        exitReason string
        t         float64
}

func (a *app) pumpEvents() {
        if eventCh == nil {
                return
        }
        for {
                select {
                case ev := <-eventCh:
                        a.handleGameEvent(ev)
                default:
                        return
                }
        }
}

// ---- fps meter (development aid) ----------------------------------------
// FLW_FPS_LOG=1 logs the measured frame rate of the shell every two
// seconds. The dev console draws a lot of text and gradients, so it is the
// screen where a low frame rate becomes visible.
var (
        fpsFrames  int
        fpsSince   time.Time
        fpsEnabled = os.Getenv("FLW_FPS_LOG") != ""
)

func (a *app) tickFps() {
        fpsFrames++
        if fpsSince.IsZero() {
                fpsSince = time.Now()
                return
        }
        el := time.Since(fpsSince)
        if el < 2*time.Second {
                return
        }
        fps := float64(fpsFrames) / el.Seconds()
        fpsFrames = 0
        fpsSince = time.Now()
        // The anti-aliasing of the vector shapes is far too expensive on a
        // software rasterizer: it made the whole shell run at 8 fps. It is
        // switched off automatically when the frame rate says so.
        switch a.state {
        case stDashboard, stSettings, stDebug, stConsole, stDevConsole:
                aaAutoTune(fps)
        }
        if !fpsEnabled {
                return
        }
        name := "?"
        switch a.state {
        case stBoot:
                name = "boot"
        case stSplash:
                name = "splash"
        case stDashboard:
                name = "dashboard"
        case stSettings:
                name = "settings"
        case stDebug:
                name = "debug"
        case stConsole:
                name = "console"
        case stDevConsole:
                name = "devconsole"
        case stRunning:
                name = "running"
        }
        fmt.Fprintf(os.Stderr, "[fps] %-10s %5.1f fps (ebiten tps %.1f)\n",
                name, fps, ebiten.ActualTPS())
}

func (a *app) requestExit(reason string) {
        a.exitReason = reason
        a.exitReq = true
}

func (a *app) handleGameEvent(ev gameEvent) {
        switch ev.name {
        case "launch.result":
                // The game was launched in the background (the launch blocks
                // for seconds, which would freeze the UI thread).
                if ev.data != "" {
                        a.running.note = "launch failed: " + ev.data
                        playSound(sndError)
                        showWindow()
                } else {
                        a.running.note = ""
                        hideWindow()
                }
        case "relaunch.result":
                if ev.data != "" {
                        a.running.relTries++
                        if a.running.relTries > 5 {
                                a.reloading = false
                                a.running.note = "relaunch failed: " + ev.data
                                playSound(sndError)
                        }
                } else {
                        a.reloading = false
                        a.running.note = ""
                }
        case "nugget.input":
                if a.state == stDevConsole {
                        a.devScr.pushKey(ev.data, a)
                }
        case "nugget.button":
                if currentFeatures().DebugMenu {
                        if a.state == stRunning {
                                playSound(sndSelect)
                                
                                
                                showWindowVsync(false)
                                a.gotoState(stDevConsole)
                        } else if a.state == stDevConsole {
                                a.closeDevConsole()
                        }
                }
        case "game.error":
                
                
                
                msg := ev.data
                if len(msg) > 160 {
                        msg = msg[:160]
                }
                a.showWarn("game error: " + msg)
                if a.state == stRunning {
                        showWindowVsync(false)
                        a.running.crashed = true
                }
        case "game.hung":
                
                
                
                
                a.showWarn("game not responding — use force stop (K)")
                if a.state == stRunning {
                        showWindowVsync(false)
                        a.running.crashed = true
                }
        case "game.exit":
                if a.reloading {
                        return
                }
                st := supervisor.M().State()
                if st == supervisor.StateRunning || st == supervisor.StateLaunching || st == supervisor.StateStopping {
                        return
                }
                if currentFeatures().Dashboard {
                        if a.state == stRunning || a.state == stDebug || a.state == stConsole || a.state == stDevConsole {
                                a.devScr.forceUnfreeze()
                                showWindow()
                                a.running.note = ""
                                // The reason of the crash of the game host
                                // (on windows for example a missing
                                // WebView2 runtime) is shown in the shell.
                                if reason := supervisor.M().LastGameError(); reason != "" {
                                        a.running.note = "the game stopped: " + reason
                                        a.showWarn("the game stopped: " + reason)
                                }
                                a.running.launched = false
                                a.gotoState(stDashboard)
                        }
                        // The watchdog kill must always be reported, no
                        // matter which screen the shell was on.
                        if atomic.LoadInt32(&watchdogKilled) == 1 {
                                atomic.StoreInt32(&watchdogKilled, 0)
                                a.showWarn("watchdog: shell was frozen - game force-stopped")
                        }
                        return
                }
                if exitLogger != nil {
                        exitLogger.Info("supervisor", "game exited, engine closing")
                }
                a.requestExit("game exited")
        case "hotreload":
                if currentFeatures().Dashboard {
                        a.showToast(ev.data)
                        if a.state == stRunning {
                                showWindow()
                                a.running.launched = false
                                a.gotoState(stDashboard)
                        }
                        return
                }
                a.reloading = true
                a.running.note = "hot reload applied, restarting"
        }
}

func (a *app) launchGame() {
        // Launching blocks for seconds; running it on the ebiten UI thread
        // froze the shell (and could trip the watchdog).
        a.running.crashed = false
        a.running.note = ""
        a.running.launchInBackground(a, func() error {
                return supervisor.M().LaunchGame()
        }, false)
        a.gotoState(stRunning)
        hideWindow()
}



func (a *app) launchAPIDemos() {
        if st := supervisor.M().State(); st == supervisor.StateRunning || st == supervisor.StateLaunching {
                a.gotoState(stRunning)
                hideWindow()
                return
        }
        if !supervisor.M().PackageExists("apidemos.nkpkg") {
                a.showWarn("apidemos.nkpkg not installed (dev builds only)")
                return
        }
        a.running.crashed = false
        a.running.note = ""
        a.running.launchInBackground(a, func() error {
                return supervisor.M().LaunchPackage("apidemos.nkpkg", "API Demos")
        }, false)
        a.gotoState(stRunning)
        hideWindow()
}


func (a *app) closeDevConsole() {
        a.devScr.forceUnfreeze()
        a.gotoState(stRunning)
        hideWindow()
}

func (a *app) start() {
        features := currentFeatures()
        if features.BootLog {
                a.state = stBoot
        } else {
                a.state = stSplash
        }
        a.fadeIn = 1
}

func (a *app) Update() error {
        // Monotonic beat: the watchdog must not be fooled by wall-clock jumps.
        atomic.StoreInt64(&uiBeat, int64(time.Since(uiEpoch)))
        a.tickFps()
        now := time.Now()
        dt := now.Sub(a.lastT).Seconds()
        if dt > 0.25 {
                dt = 0.25
        }
        if dt < 0 {
                dt = 0
        }
        a.lastT = now
        a.t += dt
        a.pumpEvents()
        if a.exitReq {
                return ebiten.Termination
        }
        // vsync reconcile (single source of truth): never vsync while a game
        // window exists — every overlay path (gotoState etc.) used to re-enable
        // it and the occluded swap froze the whole shell on real Windows.
        if !windowHidden {
                applyVsync(!gameWindowExists())
        }
        
        uiCursor = curArrow
        forceBusyCursor = false
        if a.fadeIn < 1 {
                a.fadeIn += dt * 2.4
        }
        if a.toastTTL > 0 {
                a.toastTTL -= dt
        }
        switch a.state {
        case stBoot:
                if a.bootScr.done() {
                        if currentFeatures().Dashboard {
                                a.gotoState(stDashboard)
                        } else {
                                a.gotoState(stRunning)
                        }
                }
                a.bootScr.update(dt)
        case stSplash:
                if a.splashScr.done() {
                        if currentFeatures().Dashboard {
                                a.gotoState(stDashboard)
                        } else {
                                a.gotoState(stRunning)
                        }
                }
                a.splashScr.update(dt)
        case stDashboard:
                a.dashScr.update(dt, a)
        case stSettings:
                a.setScr.update(dt, a)
        case stDebug:
                a.dbgScr.update(dt, a)
        case stConsole:
                a.conScr.update(dt, a)
        case stDevConsole:
                a.devScr.update(dt, a)
        case stRunning:
                a.running.update(dt, a)
        }
        return nil
}

func (a *app) gotoState(s appState) {
        a.prevState = a.state
        a.state = s
        a.fadeIn = 0
        switch s {
        case stDashboard:
                showWindow()
                a.dashScr.enter()
        case stDebug, stConsole:
                showWindow()
                a.dbgScr.enter()
        case stDevConsole:
                showWindow()
                a.devScr.enter()
        case stSettings:
                showWindow()
                a.setScr.build()
        }
}

func (a *app) back() {
        switch a.state {
        case stSettings, stDebug, stConsole:
                playSound(sndBack)
                if a.prevState == stRunning && supervisor.M().State() == supervisor.StateRunning {
                        a.gotoState(stRunning)
                        hideWindow()
                        return
                }
                a.gotoState(stDashboard)
        case stDevConsole:
                playSound(sndBack)
                a.closeDevConsole()
        case stRunning:
                if currentFeatures().Dashboard {
                        a.gotoState(stDashboard)
                }
        }
}

func (a *app) showToast(msg string) {
        a.toast = msg
        a.toastTTL = 3.4
        a.toastDanger = false
        playSound(sndNotif)
}

func (a *app) showWarn(msg string) {
        a.toast = msg
        a.toastTTL = 3.4
        a.toastDanger = true
        playSound(sndError)
}

func (a *app) Draw(screen *ebiten.Image) {
        switch a.state {
        case stBoot:
                a.bootScr.draw(screen)
        case stSplash:
                a.splashScr.draw(screen)
        case stDashboard:
                a.dashScr.draw(screen, a)
        case stSettings:
                a.setScr.draw(screen)
        case stDebug:
                a.dbgScr.draw(screen)
        case stConsole:
                a.conScr.draw(screen)
        case stDevConsole:
                a.devScr.draw(screen)
        case stRunning:
                a.running.draw(screen, a)
        }
        if a.fadeIn < 1 {
                f := 1 - a.fadeIn
                drawOverlay(screen, f)
        }
        if a.toastTTL > 0 && a.toast != "" {
                drawMeegoBanner(screen, a.toast, a.toastTTL, a.toastDanger)
        }
        
        if !isTouchDevice() {
                drawCursor(screen, a.t)
        }
}

func (a *app) Layout(outsideWidth, outsideHeight int) (int, int) {
        return LogicalW, LogicalH
}


func isTouchDevice() bool {
        return touchBuild
}
