package nuggetos

import (
        "fmt"
        "path/filepath"
        "math"

        "github.com/hajimehoshi/ebiten/v2"
        "github.com/hajimehoshi/ebiten/v2/inpututil"

        "flwengine/internal/buildinfo"
        "flwengine/internal/plat"
        "flwengine/internal/supervisor"
)

type dbgItem struct {
        label string
        value string
        run   func(a *app)
}

type debugScreen struct {
        items    []dbgItem
        sel      int
        t        float64
        result   string
        resultTTL float64

        reveal   float64   
        rowFade  []float64 
        pulse    float64   
        lastSel  int
        scroll   float64 
}

var hrPorts = []int{7777, 7800, 8080, 9090}

const (
        dbgRowH  = 46.0
        dbgListX = 96.0
        dbgListW = 580.0
        dbgListY = 150.0
)

func newDebugScreen() *debugScreen {
        return &debugScreen{}
}

func (d *debugScreen) enter() {
        d.build()
        d.reveal = 0
        d.scroll = 0
}

func (d *debugScreen) build() {
        m := supervisor.M()
        st := m.Settings()
        osInfo, _ := plat.Detect()
        h := m.Handle()
        gameState := "idle"
        if h != nil {
                gameState = fmt.Sprintf("running pid=%d rpc=%d", h.PID, h.RPC)
        }
        pkg, _ := m.FindGamePackage()
        plainState := "n/a"
        if ok, err := m.PlainStatus(); buildinfo.ParseFeatures().Signing {
                if ok {
                        plainState = "verified"
                } else {
                        plainState = "failed: " + err.Error()
                }
        }
        incoming := len(m.ScanIncoming())
        meta := m.GameMeta()
        d.items = []dbgItem{
                {label: "engine", value: fmt.Sprintf("%s type=%s rev=%s", buildinfo.Version, buildinfo.Type(), buildinfo.GitRev)},
                {label: "host", value: fmt.Sprintf("%s %s (%s)", osInfo.Platform, osInfo.PlatformVer, osInfo.Arch)},
                {label: "wine", value: fmt.Sprintf("%v", osInfo.Wine)},
                {label: "game state", value: gameState},
                {label: "game package", value: pkg},
                {label: "game meta", value: fmt.Sprintf("%s v%s (%d files)", meta.Title, meta.Version, meta.Files)},
                {label: "runtime integrity", value: plainState},
                {label: "hot reload", value: fmt.Sprintf("%v %s:%d", st.HotReloadOn, st.HotReloadIP, st.HotReloadPort), run: func(a *app) {
                        if m.HotReloadRunning() {
                                m.StopHotReload()
                                supervisor.M().UpdateSettings(func(x *supervisor.Settings) { x.HotReloadOn = false })
                                a.showToast("hot reload disabled")
                        } else {
                                if err := m.StartHotReload(); err != nil {
                                        a.showWarn("hot reload failed: " + err.Error())
                                        return
                                }
                                supervisor.M().UpdateSettings(func(x *supervisor.Settings) { x.HotReloadOn = true })
                                a.showToast(fmt.Sprintf("hot reload on %s", m.HotReloadAddr()))
                        }
                        d.build()
                }},
                {label: "hot reload ip", value: st.HotReloadIP, run: func(a *app) {
                        next := "127.0.0.1"
                        if st.HotReloadIP == "127.0.0.1" {
                                next = "0.0.0.0"
                        }
                        wasRunning := m.HotReloadRunning()
                        if wasRunning {
                                m.StopHotReload()
                        }
                        supervisor.M().UpdateSettings(func(x *supervisor.Settings) { x.HotReloadIP = next })
                        if wasRunning {
                                if err := m.StartHotReload(); err != nil {
                                        a.showWarn("hot reload failed: " + err.Error())
                                }
                        }
                        a.showToast("hot reload ip: " + next)
                        d.build()
                }},
                {label: "hot reload port", value: fmt.Sprintf("%d", st.HotReloadPort), run: func(a *app) {
                        idx := 0
                        for i, p := range hrPorts {
                                if p == st.HotReloadPort {
                                        idx = i
                                }
                        }
                        next := hrPorts[(idx+1)%len(hrPorts)]
                        wasRunning := m.HotReloadRunning()
                        if wasRunning {
                                m.StopHotReload()
                        }
                        supervisor.M().UpdateSettings(func(x *supervisor.Settings) { x.HotReloadPort = next })
                        if wasRunning {
                                if err := m.StartHotReload(); err != nil {
                                        a.showWarn("hot reload failed: " + err.Error())
                                }
                        }
                        a.showToast(fmt.Sprintf("hot reload port: %d", next))
                        d.build()
                }},
                {label: "install package", value: fmt.Sprintf("incoming: %d nkpkg", incoming), run: func(a *app) {
                        files := m.ScanIncoming()
                        if len(files) == 0 {
                                a.showWarn("no nkpkg in flwdata/incoming")
                                return
                        }
                        okN := 0
                        var lastErr string
                        for _, f := range files {
                                if err := m.InstallPackage(f); err != nil {
                                        lastErr = filepath.Base(f) + ": " + err.Error()
                                        continue
                                }
                                okN++
                        }
                        if okN > 0 {
                                a.showToast(fmt.Sprintf("installed %d package(s)", okN))
                        } else {
                                a.showWarn("install failed: " + lastErr)
                        }
                        d.build()
                }},
                {label: "workspace", value: fmt.Sprintf("workspace%d", st.Workspace), run: func(a *app) {
                        next := 0
                        if st.Workspace == 0 {
                                next = 1
                        }
                        supervisor.M().UpdateSettings(func(x *supervisor.Settings) { x.Workspace = next })
                        a.showToast("switched to workspace" + fmt.Sprint(next))
                        d.build()
                }},
                {label: "launch game", value: "spawn gamehost", run: func(a *app) {
                        // Launching blocks until the gamehost reports
                        // readiness (up to 20s). Doing that on the ebiten UI
                        // thread freezes the whole shell.
                        go func() {
                                if err := supervisor.M().LaunchGame(); err != nil {
                                        a.showWarn("launch failed: " + err.Error())
                                        return
                                }
                                hideWindow()
                                a.gotoState(stRunning)
                        }()
                }},
                {label: "kill game", value: "terminate", run: func(a *app) {
                        go supervisor.M().KillGame()
                        a.showToast("game terminated")
                }},
                {label: "eval ping", value: "test rpc to game", run: func(a *app) {
                        go func() {
                                res, err := supervisor.M().RPCCall("eval", "1+1")
                                if err != nil {
                                        d.result = "err: " + err.Error()
                                } else {
                                        d.result = "result: " + res
                                }
                                d.resultTTL = 5
                        }()
                }},
                {label: "game dev console", value: "f10 overlay in game", run: func(a *app) {
                        a.gotoState(stDevConsole)
                }},
                {label: "nsh console", value: "nugget shell", run: func(a *app) {
                        a.gotoState(stConsole)
                }},
        }
        if d.sel >= len(d.items) {
                d.sel = len(d.items) - 1
        }
        if d.sel < 0 {
                d.sel = 0
        }
}

func (d *debugScreen) visibleCount() int {
        maxY := float64(LogicalH) - 110
        n := int((maxY - dbgListY) / dbgRowH)
        if n < 5 {
                n = 5
        }
        return n
}


func (d *debugScreen) scrollStart() int {
        vis := d.visibleCount()
        if d.sel < vis {
                return 0
        }
        return d.sel - vis + 1
}

func (d *debugScreen) update(dt float64, a *app) {
        d.t += dt
        if d.reveal < 1 {
                d.reveal = math.Min(1, d.reveal+dt/0.3)
        }
        if d.resultTTL > 0 {
                d.resultTTL -= dt
        }
        
        if len(d.rowFade) != len(d.items) {
                d.rowFade = make([]float64, len(d.items))
        }
        for i := range d.rowFade {
                target := 0.0
                if d.sel == i {
                        target = 1
                }
                d.rowFade[i] = damp(d.rowFade[i], target, 12, dt)
        }
        if d.pulse > 0 {
                d.pulse -= dt
        }
        if d.sel != d.lastSel {
                d.pulse = 0.22
                d.lastSel = d.sel
        }
        
        targetScroll := float64(d.scrollStart())
        d.scroll = damp(d.scroll, targetScroll, 14, dt)
        
        if _, wy := ebiten.Wheel(); wy != 0 {
                maxStart := len(d.items) - d.visibleCount()
                if maxStart < 0 {
                        maxStart = 0
                }
                ns := int(d.scroll) - int(wy*2)
                if ns < 0 {
                        ns = 0
                }
                if ns > maxStart {
                        ns = maxStart
                }
                d.scroll = float64(ns)
                if d.sel < ns {
                        d.sel = ns
                }
                if d.sel > ns+d.visibleCount()-1 {
                        d.sel = ns + d.visibleCount() - 1
                }
        }
        if inpututil.IsKeyJustPressed(ebiten.KeyDown) {
                if d.sel < len(d.items)-1 {
                        d.sel++
                        playSound(sndMove)
                }
        }
        if inpututil.IsKeyJustPressed(ebiten.KeyUp) {
                if d.sel > 0 {
                        d.sel--
                        playSound(sndMove)
                }
        }
        if inpututil.IsKeyJustPressed(ebiten.KeyEnter) || inpututil.IsKeyJustPressed(ebiten.KeySpace) {
                if item := d.items[d.sel]; item.run != nil {
                        playSound(sndSelect)
                        item.run(a)
                }
        }
        if inpututil.IsKeyJustPressed(ebiten.KeyEscape) {
                a.back()
        }
        d.tapRows(a)
}

func (d *debugScreen) rowScreenY(i int) float64 {
        return dbgListY + (float64(i)-d.scroll)*dbgRowH
}

func (d *debugScreen) tapRows(a *app) {
        px, py, ok := tapOrClick()
        if !ok {
                mv := hoverPointerMoved()
                mx, my := ebiten.CursorPosition()
                for i := range d.items {
                        y := d.rowScreenY(i)
                        if inRect(float64(mx), float64(my), dbgListX, y, dbgListW, dbgRowH) {
                                uiCursor = curHand
                                if mv && d.sel != i {
                                        d.sel = i
                                }
                                return
                        }
                }
                return
        }
        for i := range d.items {
                y := d.rowScreenY(i)
                if inRect(px, py, dbgListX, y, dbgListW, dbgRowH) {
                        if d.sel != i {
                                d.sel = i
                                playSound(sndMove)
                        } else {
                                if item := d.items[d.sel]; item.run != nil {
                                        playSound(sndSelect)
                                        item.run(a)
                                }
                        }
                        return
                }
        }
}

func (d *debugScreen) draw(screen *ebiten.Image) {
        drawWallpaper(screen)
        drawWallDim(screen, 0.38)
        vis := easeOutCubic(d.reveal)
        
        px, py, pw, ph := 64.0, 44.0-(1-vis)*18, float64(LogicalW)-128, float64(LogicalH)-100
        drawPanelChrome(screen, px, py, pw, ph, 12)
        
        hal := vis
        fillRoundRectGradV(screen, px+22, py+16, 40, 40, 10, MgTileHoverTop, MgTileHover)
        strokeRoundRect(screen, px+22.5, py+16.5, 39, 39, 9.5, 1, MgTileHoverBd)
        drawMeegoIconC(screen, "apps/control.png", px+42, py+36, 28)
        drawText(screen, "Debug Settings", px+72, py+20, boldFace(22), withAlphaF(MgText, hal))
        drawTextRight(screen, fmt.Sprintf("nuggetos %s · workspace%d", buildinfo.Type(), supervisor.M().Settings().Workspace), px+pw-24, py+26, regular(12), withAlphaF(MgTextDim, hal))
        fillRect(screen, px, py+58, pw, 1, withAlphaF(MgPanelBorder, hal))
        
        n := len(d.items)
        for i := 0; i < n; i++ {
                item := d.items[i]
                y := d.rowScreenY(i)
                if y < dbgListY-dbgRowH || y > float64(LogicalH)-110 {
                        continue
                }
                ia := itemAlpha(vis, 0.05+float64(i-d.scrollStart())*0.04)
                if ia <= 0 {
                        continue
                }
                selected := d.sel == i
                if selected {
                        fade := 1.0
                        if i < len(d.rowFade) {
                                fade = d.rowFade[i]
                        }
                        glow := 0.0
                        if d.pulse > 0 {
                                glow = d.pulse / 0.22
                        }
                        fillRoundRectGradV(screen, dbgListX, y+3, dbgListW, dbgRowH-8, 10,
                                withAlphaF(mixRGBA(MgTileHoverTop, MgOrange, 0.12*glow), fade*ia),
                                withAlphaF(mixRGBA(MgTileHover, MgOrange, 0.12*glow), fade*ia))
                        strokeRoundRect(screen, dbgListX+0.5, y+3.5, dbgListW-1, dbgRowH-9, 9.5, 1.5, withAlphaF(MgTileHoverBd, fade*ia))
                        fillRoundRect(screen, dbgListX-3, y+dbgRowH/2-9, 3, 18, 1.5, withAlphaF(MgOrange, fade*ia))
                }
                lc := MgText
                vc := MgTextDim
                if item.run == nil {
                        lc = MgTextDim
                        vc = MgTextFaint
                }
                drawText(screen, clampText(item.label, regular(16), 200), dbgListX+22, y+14, regular(16), withAlphaF(lc, ia))
                drawTextRight(screen, clampText(item.value, medium(14), 320), dbgListX+dbgListW-18, y+17, medium(14), withAlphaF(vc, ia))
                if item.run != nil {
                        iconChevron(screen, dbgListX+dbgListW-6, y+dbgRowH/2, 4, withAlphaF(MgTextFaint, ia))
                }
                if i < n-1 {
                        fillRect(screen, dbgListX+22, y+dbgRowH-4, dbgListW-44, 1, withAlphaF(MgDivider, ia*1.8))
                }
        }
        
        visN := d.visibleCount()
        if n > visN {
                sbx := px + pw - 14
                trackH := float64(visN)*dbgRowH - 10
                fillRoundRect(screen, sbx, dbgListY, 5, trackH, 2.5, MgDivider)
                thumbH := float64(visN) * trackH / float64(n)
                maxScroll := float64(n - visN)
                thumbY := dbgListY + clamp01(d.scroll/maxScroll)*trackH
                fillRoundRectGradV(screen, sbx, thumbY, 5, thumbH, 2.5, MgOrange, mixRGBA(MgOrange, RGBA{0, 0, 0, 255}, 0.2))
        }
        
        fy := float64(LogicalH) - 46
        fillRect(screen, px+24, fy-10, pw-48, 1, MgDivider)
        x := px + 28.0
        const kh = 20.0
        kc := RGBA{0xd8, 0xda, 0xd6, 0xff}
        drawKeycap(screen, x, fy, kh, "up/down", kc)
        x += keycapWidth("up/down", kh) + 8
        drawText(screen, "navigate", x, fy+5, regular(11), MgTextFaint)
        x += measureTextW("navigate", regular(11)) + 16
        drawKeycap(screen, x, fy, kh, "enter", kc)
        x += keycapWidth("enter", kh) + 8
        drawText(screen, "run", x, fy+5, regular(11), MgTextFaint)
        x += measureTextW("run", regular(11)) + 16
        drawKeycap(screen, x, fy, kh, "esc", kc)
        x += keycapWidth("esc", kh) + 8
        drawText(screen, "back", x, fy+5, regular(11), MgTextFaint)
        if d.resultTTL > 0 && d.result != "" {
                ral := clamp01(d.resultTTL / 0.5)
                drawTextRight(screen, clampText(d.result, monoFace(13), 460), px+pw-28, fy+5, monoFace(13), withAlphaF(MgOrange, ral))
        }
}
