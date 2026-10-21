package nuggetos

import (
        "math"
        "strings"
        "sync/atomic"

        "github.com/hajimehoshi/ebiten/v2"
        "github.com/hajimehoshi/ebiten/v2/inpututil"

        "flwengine/internal/supervisor"
)

type runningScreen struct {
        t         float64
        launchT   float64
        launched  bool
        note      string
        relTries  int
        reveal    float64
        crashed   bool
        stopPulse float64
        // pending is 1 while a launch is running in the background.
        pending int32
        // relaunch marks the in-flight launch as a hot reload relaunch.
        relaunch int32
}

// launchPending reports whether a launch is currently running.
func (r *runningScreen) launchPending() bool {
        return atomic.LoadInt32(&r.pending) == 1
}

// launchInBackground starts the game without blocking the ebiten UI thread.
// Starting the gamehost takes seconds (package load + webview init); doing
// it on the UI thread froze the whole shell and could even trip the
// watchdog, which then killed the healthy game.
func (r *runningScreen) launchInBackground(a *app, start func() error, isRelaunch bool) {
        atomic.StoreInt32(&r.pending, 1)
        if isRelaunch {
                atomic.StoreInt32(&r.relaunch, 1)
        }
        go func() {
                err := start()
                atomic.StoreInt32(&r.pending, 0)
                msg := ""
                if err != nil {
                        msg = err.Error()
                }
                kind := "launch.result"
                if isRelaunch {
                        kind = "relaunch.result"
                }
                select {
                case eventCh <- gameEvent{name: kind, data: msg}:
                default:
                }
        }()
}

func newRunningScreen() *runningScreen {
        return &runningScreen{}
}

func (r *runningScreen) update(dt float64, a *app) {
        r.t += dt
        if r.reveal < 1 {
                r.reveal = math.Min(1, r.reveal+dt/0.4)
        }
        if a.reloading && supervisor.M().State() == supervisor.StateIdle {
                r.launchInBackground(a, func() error {
                        err := supervisor.M().LaunchGame()
                        if err == nil || strings.Contains(err.Error(), "already active") {
                                a.reloading = false
                        }
                        return err
                }, true)
                if !r.launchPending() {
                        r.launched = true
                        r.launchT = 0
                        r.note = ""
                        r.relTries = 0
                }
        }
        if !r.launched && !r.launchPending() {
                r.launched = true
                r.launchT = 0
                r.launchInBackground(a, func() error {
                        return supervisor.M().LaunchGame()
                }, false)
        }
        r.launchT += dt
        if r.stopPulse > 0 {
                r.stopPulse -= dt * 4
                if r.stopPulse < 0 {
                        r.stopPulse = 0
                }
        }
        st := supervisor.M().State()
        
        
        
        if (inpututil.IsKeyJustPressed(ebiten.KeyK) || r.stopClicked()) &&
                (st == supervisor.StateRunning || st == supervisor.StateLaunching) {
                r.stopPulse = 1
                playSound(sndBack)
                go supervisor.M().KillGame()
        }
        if inpututil.IsKeyJustPressed(ebiten.KeyF10) {
                if currentFeatures().DebugMenu {
                        playSound(sndSelect)
                        showWindow()
                        a.gotoState(stDevConsole)
                }
        }
        if inpututil.IsKeyJustPressed(ebiten.KeyEscape) && supervisor.M().State() != supervisor.StateRunning && !a.reloading {
                if currentFeatures().Dashboard {
                        a.back()
                } else {
                        a.requestExit("user exit from stopped state")
                }
        }
}


func (r *runningScreen) stopButtonRect() (float64, float64, float64, float64) {
        cw := 460.0
        cx := float64(LogicalW)/2 - cw/2
        cy := 215.0
        bw, bh := 168.0, 34.0
        return cx + cw/2 - bw/2, cy + 196 - 6, bw, bh
}

func (r *runningScreen) stopClicked() bool {
        if !inpututil.IsMouseButtonJustPressed(ebiten.MouseButtonLeft) {
                return false
        }
        x, y := ebiten.CursorPosition()
        bx, by, bw, bh := r.stopButtonRect()
        return float64(x) >= bx && float64(x) <= bx+bw && float64(y) >= by && float64(y) <= by+bh
}

func (r *runningScreen) draw(screen *ebiten.Image, a *app) {
        drawWallpaper(screen)
        st := supervisor.M().State()
        if st == supervisor.StateLaunching {
                forceBusyCursor = true
        }
        drawWallDim(screen, 0.42)

        
        cw, ch := 460.0, 250.0
        cx := float64(LogicalW)/2 - cw/2
        cy := 215.0 + (1-easeOutCubic(r.reveal))*20
        al := easeOutCubic(r.reveal)
        drawPanelChrome(screen, cx, cy, cw, ch, 14)
        drawGloss(screen, cx, cy, cw, ch, 14, 0.4, 0.10)

        
        if a.dashScr != nil {
                a.dashScr.loadIcon()
        }
        iconChip := func() {
                fillCircle(screen, cx+cw/2, cy+58, 34, RGBA{0xf3, 0xf4, 0xf2, 0xff})
                strokeCircle(screen, cx+cw/2, cy+58, 34, 1, MgTileBorder)
                if a.dashScr != nil && a.dashScr.iconTile != nil {
                        op := &ebiten.DrawImageOptions{}
                        op.GeoM.Scale(52.0/200, 52.0/200)
                        op.GeoM.Translate(cx+cw/2-26, cy+32)
                        screen.DrawImage(a.dashScr.iconTile, op)
                } else {
                        drawMeegoIconC(screen, "tiles/games.png", cx+cw/2, cy+58, 52)
                }
        }
        iconChip()

        if a.reloading {
                drawSpinner(screen, cx+cw/2, cy+124, 12, r.t, MgCyan)
                drawTextCenter(screen, "hot reload", cx+cw/2, cy+150, boldFace(24), withAlphaF(MgPanelHead, al))
                dots := ""
                for i := 0; i < int(r.t*2)%4; i++ {
                        dots += "."
                }
                drawTextCenter(screen, "applying"+dots, cx+cw/2, cy+184, regular(15), withAlphaF(MgCyan, al))
                forceBusyCursor = true
        } else {
                title := supervisor.M().CurrentTitle()
                if title == "" {
                        title = "the rosemary field"
                }
                switch st {
                case supervisor.StateLaunching, supervisor.StateRunning:
                        drawTextCenter(screen, title, cx+cw/2, cy+120, boldFace(24), withAlphaF(MgPanelHead, al))
                        if st == supervisor.StateRunning {
                                drawStatusDot(screen, cx+cw/2-measureTextW("running", regular(15))/2-14, cy+163, 3.6, MgGreen, r.t, true)
                                drawTextCenter(screen, "running", cx+cw/2, cy+156, regular(15), withAlphaF(MgGreen, al))
                        } else {
                                drawSpinner(screen, cx+cw/2-measureTextW("starting...", regular(15))/2-20, cy+163, 9, r.t, MgTextDim)
                                drawTextCenter(screen, "starting...", cx+cw/2, cy+156, regular(15), withAlphaF(MgTextDim, al))
                                forceBusyCursor = true
                        }
                default:
                        drawTextCenter(screen, "game stopped", cx+cw/2, cy+120, boldFace(22), withAlphaF(MgTextDim, al))
                        if currentFeatures().Dashboard {
                                drawTextCenter(screen, "esc: back to nuggetos", cx+cw/2, cy+156, regular(15), withAlphaF(MgTextDim, al))
                        } else {
                                drawTextCenter(screen, "closing", cx+cw/2, cy+156, regular(15), withAlphaF(MgTextDim, al))
                        }
                }
                if r.crashed && st == supervisor.StateRunning {
                        drawTextCenter(screen, "game error reported — the window may be stuck", cx+cw/2, cy+184, monoFace(12), withAlphaF(MgRed, al*0.9))
                }
                if st == supervisor.StateRunning || st == supervisor.StateLaunching {
                        bx, by, bw, bh := r.stopButtonRect()
                        press := 1 - r.stopPulse*0.25
                        danger := r.crashed || r.note != ""
                        acc := MgCyan
                        if danger {
                                acc = MgRed
                        }
                        fillRoundRectGradV(screen, bx, by+(1-press)*bh*0.08, bw, bh*press, bh/2,
                                withAlphaF(RGBA{0x33, 0x38, 0x3e, 1}, al), withAlphaF(RGBA{0x16, 0x18, 0x1b, 1}, al))
                        strokeRoundRect(screen, bx+0.5, by+0.5, bw-1, bh-1, bh/2-0.5, 1, withAlphaF(acc, al))
                        drawTextCenter(screen, "force stop", bx+bw/2, by+bh/2-6, semibold(14), withAlphaF(MgLight, al))
                }
        }
        if r.note != "" {
                drawTextCenter(screen, r.note, cx+cw/2, cy+196, monoFace(13), MgRed)
        }
        
        if currentFeatures().DebugMenu {
                kx := cx + cw/2 - 60
                ky := cy + ch - 44
                drawKeycap(screen, kx, ky, 20, "F10", withAlphaF(RGBA{0xd8, 0xda, 0xd6, 0xff}, al))
                drawText(screen, "dev console", kx+keycapWidth("F10", 20)+8, ky+5, regular(11), withAlphaF(MgTextFaint, al))
                kx2 := kx - 92
                drawKeycap(screen, kx2, ky, 20, "K", withAlphaF(RGBA{0xd8, 0xda, 0xd6, 0xff}, al))
                drawText(screen, "force stop", kx2+keycapWidth("K", 20)+8, ky+5, regular(11), withAlphaF(MgTextFaint, al))
        }
}
