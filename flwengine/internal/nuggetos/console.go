package nuggetos

import (
        "fmt"
        "strconv"
        "strings"
        "sync"
        "unicode"

        "github.com/hajimehoshi/ebiten/v2"
        "github.com/hajimehoshi/ebiten/v2/inpututil"

        "flwengine/internal/nsh"
        "flwengine/internal/supervisor"
)

type consoleScreen struct {
        shell    *nsh.Shell
        sink     *termSink
        // mu guards lines/scroll: background commands (e.g. "game eval")
        // write their result from a goroutine while the UI thread draws.
        mu      sync.Mutex
        lines   []termLine
        scroll  int
        t       float64
        cursorOn bool
        exitReq bool
        // staticImg caches the background, the toolbar and the footer of the
        // terminal. Only the output lines are drawn live.
        staticImg *ebiten.Image
}

type termLine struct {
        text string
}

type termSink struct {
        cs *consoleScreen
}

func (s *termSink) Write(str string) {
        s.cs.appendRaw(str)
}

func (s *termSink) WriteLine(str string) {
        s.cs.appendLine(str)
}

func newConsoleScreen() *consoleScreen {
        cs := &consoleScreen{cursorOn: true}
        cs.sink = &termSink{cs: cs}
        cs.shell = nsh.New(cs.sink)
        clearFn := func() {
                cs.lines = nil
                cs.appendLine("nugget shell (nsh) 1.0")
        }
        exitFn := func() {
                cs.exitReq = true
        }
        registerEngineCommands(cs.shell, clearFn, exitFn)
        cs.appendLine("nugget shell (nsh) 1.0")
        cs.appendLine("type help for commands")
        return cs
}

func (cs *consoleScreen) enter() {
}

func (cs *consoleScreen) appendLine(s string) {
        cs.mu.Lock()
        defer cs.mu.Unlock()
        for _, part := range strings.Split(s, "\n") {
                cs.lines = append(cs.lines, termLine{text: part})
        }
        cs.trim()
        cs.scrollBottomLocked()
}

func (cs *consoleScreen) appendRaw(s string) {
        cs.mu.Lock()
        defer cs.mu.Unlock()
        if len(cs.lines) == 0 {
                cs.lines = append(cs.lines, termLine{})
        }
        last := &cs.lines[len(cs.lines)-1]
        last.text += s
        if strings.HasSuffix(s, "\n") {
                last.text = strings.TrimSuffix(last.text, "\n")
                cs.lines = append(cs.lines, termLine{})
                cs.trim()
                cs.scrollBottomLocked()
        }
}

func (cs *consoleScreen) trim() {
        max := 400
        if len(cs.lines) > max {
                cs.lines = cs.lines[len(cs.lines)-max:]
        }
}

// scrollBottomLocked assumes cs.mu is already held.
func (cs *consoleScreen) scrollBottomLocked() {
        cs.scroll = len(cs.lines)
}

func (cs *consoleScreen) scrollBottom() {
        cs.mu.Lock()
        defer cs.mu.Unlock()
        cs.scrollBottomLocked()
}

func (cs *consoleScreen) visibleLines() []termLine {
        cs.mu.Lock()
        defer cs.mu.Unlock()
        visible := 26
        if cs.scroll > len(cs.lines) {
                cs.scroll = len(cs.lines)
        }
        if cs.scroll < visible {
                cs.scroll = visible
        }
        start := cs.scroll - visible
        if start < 0 {
                start = 0
        }
        end := cs.scroll
        if end > len(cs.lines) {
                end = len(cs.lines)
        }
        out := make([]termLine, end-start)
        copy(out, cs.lines[start:end])
        return out
}

func (cs *consoleScreen) update(dt float64, a *app) {
        cs.t += dt
        if int(cs.t*2)%2 == 0 {
                cs.cursorOn = true
        } else {
                cs.cursorOn = false
        }
        if cs.exitReq {
                cs.exitReq = false
                a.back()
                return
        }
        if inpututil.IsKeyJustPressed(ebiten.KeyEscape) {
                a.back()
                return
        }
        if inpututil.IsKeyJustPressed(ebiten.KeyUp) {
                cs.shell.FeedKey(nsh.KeyUp)
        }
        if inpututil.IsKeyJustPressed(ebiten.KeyDown) {
                cs.shell.FeedKey(nsh.KeyDown)
        }
        if inpututil.IsKeyJustPressed(ebiten.KeyLeft) {
                cs.shell.FeedKey(nsh.KeyLeft)
        }
        if inpututil.IsKeyJustPressed(ebiten.KeyRight) {
                cs.shell.FeedKey(nsh.KeyRight)
        }
        if inpututil.IsKeyJustPressed(ebiten.KeyHome) {
                cs.shell.FeedKey(nsh.KeyHome)
        }
        if inpututil.IsKeyJustPressed(ebiten.KeyEnd) {
                cs.shell.FeedKey(nsh.KeyEnd)
        }
        if inpututil.IsKeyJustPressed(ebiten.KeyDelete) {
                cs.shell.FeedKey(nsh.KeyDelete)
        }
        if inpututil.IsKeyJustPressed(ebiten.KeyBackspace) {
                cs.shell.FeedKey(nsh.KeyBackspace)
        }
        if inpututil.IsKeyJustPressed(ebiten.KeyTab) {
                cs.shell.FeedControl([]byte{'\t'})
        }
        if inpututil.IsKeyJustPressed(ebiten.KeyEnter) {
                cs.sink.WriteLine("")
                cs.shell.Submit()
                return
        }
        if inpututil.IsKeyJustPressed(ebiten.KeyPageUp) {
                cs.mu.Lock()
                cs.scroll -= 10
                if cs.scroll < 26 {
                        cs.scroll = 26
                }
                cs.mu.Unlock()
                return
        }
        if inpututil.IsKeyJustPressed(ebiten.KeyPageDown) {
                cs.scrollBottom()
                return
        }
        _, ywheel := ebiten.Wheel()
        if ywheel != 0 {
                cs.mu.Lock()
                cs.scroll -= int(ywheel * 3)
                if cs.scroll < 26 {
                        cs.scroll = 26
                }
                if cs.scroll > len(cs.lines) {
                        cs.scroll = len(cs.lines)
                }
                cs.mu.Unlock()
        }
        cs.typeRunes()
}

func (cs *consoleScreen) typeRunes() {
        runes := ebiten.AppendInputChars(nil)
        ctrl := ebiten.IsKeyPressed(ebiten.KeyControl)
        for _, r := range runes {
                if ctrl {
                        continue
                }
                if r == '\t' || r == '\n' || r == '\r' {
                        continue
                }
                if !unicode.IsPrint(r) {
                        continue
                }
                cs.shell.FeedRune(r)
        }
}

// drawStatic renders the parts of the terminal that never change: the
// background, the toolbar and the footer. The terminal itself redrew all of
// it every frame, which cost about two thirds of the frame time.
// drawStatic renders the parts of the terminal that never change: the
// background, the toolbar and the footer. Redrawing them every frame cost
// about two thirds of the frame time of the console.
func (cs *consoleScreen) drawStatic(screen *ebiten.Image) {
        fillRoundRectGradV(screen, 0, 0, float64(LogicalW), float64(LogicalH), 0, RGBA{0x13, 0x15, 0x17, 0xff}, MgTermBg)

        fillRoundRectGradV(screen, 0, 0, float64(LogicalW), 44, 0, RGBA{0x2a, 0x2e, 0x32, 0xff}, RGBA{0x11, 0x13, 0x15, 0xff})
        drawGloss(screen, 0, 0, float64(LogicalW), 44, 0, 0.5, 0.07)
        fillCircle(screen, 24, 22, 8, mixRGBA(MgCyan, RGBA{0, 0, 0, 255}, 0.35))
        drawGloss(screen, 16, 14, 16, 16, 8, 0.5, 0.4)
        iconTerminal(screen, 42, 22, 8, MgCyan)
        drawText(screen, "Dev Console", 64, 13, boldFace(16), MgLight)
        drawTextRight(screen, "nugget shell (nsh) 1.0", LogicalW-24, 15, monoFace(12), MgLightFaint)
        fillRect(screen, 0, 44, float64(LogicalW), 1, MgBarBorder)

        fillRoundRectGradV(screen, 0, float64(LogicalH-44), float64(LogicalW), 44, 0, RGBA{0x24, 0x27, 0x2a, 0xff}, RGBA{0x0c, 0x0e, 0x10, 0xff})
        fillRect(screen, 0, float64(LogicalH-44), float64(LogicalW), 1, MgBarBorder)
        x := 24.0
        const kh = 20.0
        kc := RGBA{0x3a, 0x40, 0x46, 0xff}
        drawKeycap(screen, x, float64(LogicalH-32), kh, "enter", kc)
        x += keycapWidth("enter", kh) + 8
        drawText(screen, "run", x, float64(LogicalH-27), regular(11), MgLightFaint)
        x += measureTextW("run", regular(11)) + 16
        drawKeycap(screen, x, float64(LogicalH-32), kh, "tab", kc)
        x += keycapWidth("tab", kh) + 8
        drawText(screen, "complete", x, float64(LogicalH-27), regular(11), MgLightFaint)
        x += measureTextW("complete", regular(11)) + 16
        drawKeycap(screen, x, float64(LogicalH-32), kh, "pgup/pgdn", kc)
        x += keycapWidth("pgup/pgdn", kh) + 8
        drawText(screen, "scroll", x, float64(LogicalH-27), regular(11), MgLightFaint)
        drawTextRight(screen, "esc back", LogicalW-24, float64(LogicalH-27), regular(12), MgLightFaint)
}

// staticImage returns the cached background of the terminal.
func (cs *consoleScreen) staticImage() *ebiten.Image {
        if cs.staticImg != nil {
                return cs.staticImg
        }
        img := ebiten.NewImage(LogicalW, LogicalH)
        cs.drawStatic(img)
        cs.staticImg = img
        return img
}

func (cs *consoleScreen) draw(screen *ebiten.Image) {
        if img := cs.staticImage(); img != nil {
                drawImageOpaque(screen, img)
        } else {
                cs.drawStatic(screen)
        }
        cs.drawOutput(screen)
}

// drawOutput draws the terminal output, the prompt and the cursor.
func (cs *consoleScreen) drawOutput(screen *ebiten.Image) {
        uiCursor = curText
        f := monoFace(15)
        vis := cs.visibleLines()
        for i, line := range vis {
                clean := stripAnsi(line.text)
                drawText(screen, clean, 24, 58+float64(i)*22, f, termColor(line.text))
        }
        prompt := "nugget:~$ "
        cur := cs.shell.CurrentLine()
        drawText(screen, prompt, 24, 58+float64(len(vis))*22, f, MgCyan)
        cw, _ := measure(prompt, f, 22)
        drawText(screen, cur, 24+cw, 58+float64(len(vis))*22, f, MgLight)
        if cs.cursorOn {
                w, _ := measure(cur, f, 22)
                fillRect(screen, 24+cw+w+1, 59+float64(len(vis))*22, 8, 16, MgCyan)
        }
}

func stripAnsi(s string) string {
        var b strings.Builder
        inEsc := false
        for _, r := range s {
                if r == 0x1b {
                        inEsc = true
                        continue
                }
                if inEsc {
                        if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
                                inEsc = false
                        }
                        continue
                }
                b.WriteRune(r)
        }
        return b.String()
}

func termColor(s string) RGBA {
        switch {
        case strings.Contains(s, "error"), strings.Contains(s, "denied"):
                return MgRed
        case strings.HasPrefix(s, "nugget"), strings.Contains(s, "ok"):
                return MgGreen
        default:
                return RGBA{0xc8, 0xcc, 0xcf, 0xff}
        }
}

func registerEngineCommands(sh *nsh.Shell, clearFn func(), exitFn func()) {
        sh.Register(&nsh.Command{
                Name:   "help",
                Help:   "show help",
                Groups: []string{"dev", "user"},
                Run: func(ctx *nsh.Context, args []string, opts map[string]nsh.ParsedOption) error {
                        sh.Output().WriteLine("commands: help, info, game, pkg, hotreload, clear, exit")
                        return nil
                },
        })
        sh.Register(&nsh.Command{
                Name:   "info",
                Help:   "engine info",
                Groups: []string{"dev", "user"},
                Run: func(ctx *nsh.Context, args []string, opts map[string]nsh.ParsedOption) error {
                        m := supervisor.M()
                        h := m.Handle()
                        st := m.State()
                        stStr := fmt.Sprintf("%d", st)
                        gameStr := "idle"
                        if h != nil {
                                gameStr = fmt.Sprintf("pid=%d rpc=%d", h.PID, h.RPC)
                        }
                        meta := m.GameMeta()
                        sh.Output().WriteLine("engine: flower")
                        sh.Output().WriteLine("game: " + gameStr + " (state " + stStr + ")")
                        sh.Output().WriteLine("meta: " + meta.Title + " v" + meta.Version + " files=" + fmt.Sprint(meta.Files))
                        if ok, err := m.PlainStatus(); ok {
                                sh.Output().WriteLine("plain: verified")
                        } else if err != nil {
                                sh.Output().WriteLine("plain: " + err.Error())
                        }
                        return nil
                },
        })
        sh.Register(&nsh.Command{
                Name:   "game",
                Help:   "game control",
                Groups: []string{"dev"},
                Sub: []*nsh.Command{
                        {Name: "launch", Help: "launch game", Run: func(ctx *nsh.Context, args []string, opts map[string]nsh.ParsedOption) error {
                                // Launching blocks until the gamehost is ready
                                // (up to 20s). Running it on the ebiten UI
                                // thread would freeze the whole shell.
                                go func() {
                                        if err := supervisor.M().LaunchGame(); err != nil {
                                                sh.Output().WriteLine("launch failed: " + err.Error())
                                                return
                                        }
                                        sh.Output().WriteLine("game launched")
                                }()
                                sh.Output().WriteLine("launching game...")
                                return nil
                        }},
                        {Name: "kill", Help: "kill game", Run: func(ctx *nsh.Context, args []string, opts map[string]nsh.ParsedOption) error {
                                go supervisor.M().KillGame()
                                sh.Output().WriteLine("game killed")
                                return nil
                        }},
                        {Name: "eval", Help: "eval js in game", Options: []*nsh.Option{{Long: "js", Short: 'j', Type: nsh.OptString, Required: true, Help: "javascript"}}, Run: func(ctx *nsh.Context, args []string, opts map[string]nsh.ParsedOption) error {
                                js := opts["js"].Value
                                go func() {
                                        res, err := supervisor.M().RPCCall("eval", js)
                                        if err != nil {
                                                sh.Output().WriteLine("eval error: " + err.Error())
                                                return
                                        }
                                        sh.Output().WriteLine(res)
                                }()
                                sh.Output().WriteLine("evaluating...")
                                return nil
                        }},
                },
        })
        sh.Register(&nsh.Command{
                Name:   "pkg",
                Help:   "package tools",
                Groups: []string{"dev"},
                Sub: []*nsh.Command{
                        {Name: "list", Help: "list packages", Run: func(ctx *nsh.Context, args []string, opts map[string]nsh.ParsedOption) error {
                                m := supervisor.M()
                                p, _ := m.FindGamePackage()
                                r, _ := m.FindRuntimePackage()
                                if p == "" {
                                        sh.Output().WriteLine("game: none")
                                } else {
                                        sh.Output().WriteLine("game: " + p)
                                }
                                if r == "" {
                                        sh.Output().WriteLine("runtime: none")
                                } else {
                                        sh.Output().WriteLine("runtime: " + r)
                                }
                                incoming := m.ScanIncoming()
                                for _, f := range incoming {
                                        sh.Output().WriteLine("incoming: " + f)
                                }
                                return nil
                        }},
                        {Name: "info", Help: "package info", Run: func(ctx *nsh.Context, args []string, opts map[string]nsh.ParsedOption) error {
                                m := supervisor.M()
                                meta := m.GameMeta()
                                if !meta.Found {
                                        sh.Output().WriteLine("no game package")
                                        return nil
                                }
                                sh.Output().WriteLine("title: " + meta.Title)
                                sh.Output().WriteLine("version: " + meta.Version)
                                sh.Output().WriteLine("files: " + fmt.Sprint(meta.Files))
                                sh.Output().WriteLine("encrypted: " + fmt.Sprint(meta.Enc))
                                return nil
                        }},
                        {Name: "install", Help: "install plain nkpkg from file", Run: func(ctx *nsh.Context, args []string, opts map[string]nsh.ParsedOption) error {
                                if len(args) < 1 {
                                        return fmt.Errorf("usage: pkg install <path>")
                                }
                                if err := supervisor.M().InstallPackage(args[0]); err != nil {
                                        return err
                                }
                                sh.Output().WriteLine("installed: " + args[0])
                                return nil
                        }},
                        {Name: "verify", Help: "verify package signature", Run: func(ctx *nsh.Context, args []string, opts map[string]nsh.ParsedOption) error {
                                m := supervisor.M()
                                p, _ := m.FindGamePackage()
                                if p == "" {
                                        return fmt.Errorf("no game package")
                                }
                                if err := m.VerifyPackageSignature(p); err != nil {
                                        return err
                                }
                                sh.Output().WriteLine("signature ok")
                                return nil
                        }},
                },
        })
        sh.Register(&nsh.Command{
                Name:   "hotreload",
                Help:   "hot reload control",
                Groups: []string{"dev"},
                Sub: []*nsh.Command{
                        {Name: "start", Help: "start server", Run: func(ctx *nsh.Context, args []string, opts map[string]nsh.ParsedOption) error {
                                if err := supervisor.M().StartHotReload(); err != nil {
                                        return err
                                }
                                sh.Output().WriteLine("hot reload on " + supervisor.M().HotReloadAddr())
                                return nil
                        }},
                        {Name: "stop", Help: "stop server", Run: func(ctx *nsh.Context, args []string, opts map[string]nsh.ParsedOption) error {
                                supervisor.M().StopHotReload()
                                sh.Output().WriteLine("hot reload off")
                                return nil
                        }},
                        {Name: "status", Help: "show status", Run: func(ctx *nsh.Context, args []string, opts map[string]nsh.ParsedOption) error {
                                m := supervisor.M()
                                st := m.Settings()
                                if m.HotReloadRunning() {
                                        sh.Output().WriteLine("running on " + m.HotReloadAddr())
                                } else {
                                        sh.Output().WriteLine("stopped")
                                }
                                sh.Output().WriteLine(fmt.Sprintf("config: %s:%d enabled=%v", st.HotReloadIP, st.HotReloadPort, st.HotReloadOn))
                                return nil
                        }},
                        {Name: "set", Help: "set ip and port", Options: []*nsh.Option{
                                {Long: "ip", Short: 'i', Type: nsh.OptString, Help: "listen ip"},
                                {Long: "port", Short: 'p', Type: nsh.OptInt, Help: "listen port"},
                        }, Run: func(ctx *nsh.Context, args []string, opts map[string]nsh.ParsedOption) error {
                                m := supervisor.M()
                                wasRunning := m.HotReloadRunning()
                                if wasRunning {
                                        m.StopHotReload()
                                }
                                m.UpdateSettings(func(x *supervisor.Settings) {
                                        if v, ok := opts["ip"]; ok && v.Value != "" {
                                                x.HotReloadIP = v.Value
                                        }
                                        if v, ok := opts["port"]; ok && v.Value != "" {
                                                if p, err := strconv.Atoi(v.Value); err == nil && p > 0 && p < 65536 {
                                                        x.HotReloadPort = p
                                                }
                                        }
                                })
                                st := m.Settings()
                                if wasRunning {
                                        if err := m.StartHotReload(); err != nil {
                                                return err
                                        }
                                }
                                sh.Output().WriteLine(fmt.Sprintf("hot reload config: %s:%d", st.HotReloadIP, st.HotReloadPort))
                                return nil
                        }},
                },
        })
        sh.Register(&nsh.Command{
                Name:   "clear",
                Help:   "clear screen",
                Groups: []string{"dev", "user"},
                Run: func(ctx *nsh.Context, args []string, opts map[string]nsh.ParsedOption) error {
                        clearFn()
                        return nil
                },
        })
        sh.Register(&nsh.Command{
                Name:   "exit",
                Help:   "close console",
                Groups: []string{"dev", "user"},
                Run: func(ctx *nsh.Context, args []string, opts map[string]nsh.ParsedOption) error {
                        exitFn()
                        return nil
                },
        })
}
