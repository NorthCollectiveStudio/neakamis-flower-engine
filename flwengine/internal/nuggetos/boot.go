package nuggetos

import (
        "fmt"
        "math"
        "strings"
        "time"

        "github.com/hajimehoshi/ebiten/v2"

        "flwengine/internal/buildinfo"
        "flwengine/internal/supervisor"
)





type bootStep struct {
        text  string
        delay float64
}

type bootScreen struct {
        steps   []bootStep
        shown   []string
        timer   float64
        idx     int
        acc     float64
        doneTTL float64
        ready   bool
}

func newBootScreen() *bootScreen {
        b := &bootScreen{}
        info, _ := detectOSLite()
        ver := buildinfo.Version
        steps := []bootStep{
                {text: fmt.Sprintf("flower engine %s (type=%s platform=%s)", ver, buildinfo.Type(), buildinfo.CurrentPlatform()), delay: 0.25},
                {text: fmt.Sprintf("host: %s %s", info.Host, info.Kernel), delay: 0.15},
        }
        b.steps = steps
        return b
}

func (b *bootScreen) update(dt float64) {
        if b.ready {
                return
        }
        b.timer += dt
        if b.idx >= len(b.steps) {
                b.doneTTL += dt
                if b.doneTTL > 0.5 {
                        b.ready = true
                }
                return
        }
        b.acc += dt
        step := b.steps[b.idx]
        if b.acc >= step.delay {
                b.acc = 0
                b.shown = append(b.shown, step.text)
                b.idx++
        }
}

func (b *bootScreen) done() bool {
        return b.ready
}

func (b *bootScreen) draw(screen *ebiten.Image) {
        fillRect(screen, 0, 0, LogicalW, LogicalH, RGBA{0x00, 0x00, 0x00, 0xff})
        f := monoFace(15)
        y := 40.0
        x := 48.0
        for i, line := range b.shown {
                c := MgLightFaint
                if strings.Contains(line, "ok") || strings.Contains(line, "ready") || strings.Contains(line, "complete") {
                        c = MgGreen
                }
                if strings.Contains(line, "flower engine") || strings.Contains(line, "nuggetos") {
                        c = MgLight
                }
                drawText(screen, "> "+line, x, y+float64(i)*22, f, c)
        }
        if b.idx < len(b.steps) || !b.ready {
                caret := " "
                if int(b.timer*3)%2 == 0 {
                        caret = "_"
                }
                drawText(screen, caret, x, y+float64(len(b.shown))*22, f, MgCyan)
        }
        drawTextRight(screen, "nuggetos dev boot", LogicalW-48, LogicalH-60, monoFace(13), MgLightFaint)
        drawTextRight(screen, time.Now().Format("15:04:05"), LogicalW-48, 40, monoFace(14), MgLightDim)
}

func detectOSLite() (struct{ Host, Kernel string }, error) {
        var out struct{ Host, Kernel string }
        out.Host = "nugget-host"
        out.Kernel = ""
        if h, err := hostname(); err == nil {
                out.Host = h
        }
        out.Kernel = kernelVer()
        return out, nil
}






type splashScreen struct {
        t      float64
        ready  bool
        booted bool
        sndOn  bool
}

const splashDur = 4.0

func newSplashScreen() *splashScreen {
        return &splashScreen{}
}

func (s *splashScreen) update(dt float64) {
        s.t += dt
        if s.t > 0.15 && !s.booted {
                s.booted = true
                supervisor.NotifyBootProgress("init")
        }
        if s.t > 0.4 && !s.sndOn {
                s.sndOn = true
                playSound(sndBoot) 
        }
        if s.t > splashDur {
                s.ready = true
        }
}

func (s *splashScreen) done() bool { return s.ready }

func (s *splashScreen) draw(screen *ebiten.Image) {
        fillRect(screen, 0, 0, LogicalW, LogicalH, RGBA{0x00, 0x00, 0x00, 0xff})
        t := clamp01(s.t / splashDur)
        fade := clamp01(s.t * 2.2)
        
        drawTextCenter(screen, "NuggetOS", float64(LogicalW/2), float64(LogicalH/2)-34, boldFace(42), withAlphaF(MgLight, fade))
        drawTextCenter(screen, "flower engine", float64(LogicalW/2), float64(LogicalH/2)+16, regular(15), withAlphaF(MgLightFaint, clamp01(s.t*1.4)))
        
        const nDots = 5
        const dotR = 3.4
        const gap = 22.0
        x0 := float64(LogicalW/2) - float64(nDots-1)*gap/2
        dy := float64(LogicalH/2) + 74
        for i := 0; i < nDots; i++ {
                x := x0 + float64(i)*gap
                phase := s.t*2.4 - float64(i)*0.55
                sv := math.Sin(phase) * 0.5
                pulse := 0.28 + 0.72*maxF(0, sv+0.5)
                pulse = maxF(0.28, pulse)
                fillCircle(screen, x, dy, dotR, withAlphaF(MgLight, fade*pulse))
        }
        
        barW := 240.0
        bx := float64(LogicalW/2) - barW/2
        by := dy + 34
        fillRect(screen, bx, by, barW, 2, RGBA{0x2a, 0x2a, 0x2a, 0xff})
        fillRect(screen, bx, by, barW*easeOutCubic(t), 2, MgLight)
}

func withAlphaF(c RGBA, f float64) RGBA {
        return RGBA{c.R, c.G, c.B, uint8(255 * clamp01(f))}
}

func maxF(a, b float64) float64 {
        if a > b {
                return a
        }
        return b
}
