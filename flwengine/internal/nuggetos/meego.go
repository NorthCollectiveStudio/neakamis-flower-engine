package nuggetos

import (
        "bytes"
        "embed"
        "fmt"
        "image"
        "math"
        "os"
        "strings"
        "sync"
        "time"

        "github.com/hajimehoshi/ebiten/v2"
        "github.com/hajimehoshi/ebiten/v2/text/v2"
        _ "image/png"

        "github.com/hajimehoshi/ebiten/v2/inpututil"
        "github.com/hajimehoshi/ebiten/v2/vector"
)







//go:embed assets/meego/icons
var meegoIconFS embed.FS



//go:embed assets/meego/wallpaper.png
var meegoWallpaperPng []byte

var (
        iconCache = map[string]*ebiten.Image{}
        iconMu    sync.Mutex
)



func meegoIcon(path string) *ebiten.Image {
        iconMu.Lock()
        defer iconMu.Unlock()
        if img, ok := iconCache[path]; ok {
                return img
        }
        data, err := meegoIconFS.ReadFile("assets/meego/icons/" + path)
        if err != nil {
                return nil
        }
        img, _, err := image.Decode(bytes.NewReader(data))
        if err != nil {
                return nil
        }
        ei := ebiten.NewImageFromImage(img)
        iconCache[path] = ei
        return ei
}


func drawMeegoIcon(screen *ebiten.Image, path string, x, y, size float64) {
        img := meegoIcon(path)
        if img == nil {
                return
        }
        op := &ebiten.DrawImageOptions{}
        b := img.Bounds()
        op.GeoM.Scale(size/float64(b.Dx()), size/float64(b.Dy()))
        op.GeoM.Translate(x, y)
        screen.DrawImage(img, op)
}


func drawMeegoIconC(screen *ebiten.Image, path string, cx, cy, size float64) {
        drawMeegoIcon(screen, path, cx-size/2, cy-size/2, size)
}

const toolbarH = 34.0 





var wallOnce sync.Once
var wallImg *ebiten.Image
var wallScaleOnce sync.Once
var wallScaled *ebiten.Image

func getWallpaper() *ebiten.Image {
        wallOnce.Do(func() {
                img, _, err := image.Decode(bytes.NewReader(meegoWallpaperPng))
                if err != nil {
                        wallImg = nil
                        return
                }
                wallImg = ebiten.NewImageFromImage(img)
        })
        return wallImg
}



// wallpaperScaled returns the wallpaper scaled to the screen size, rendered
// once. Scaling a texture on every frame is expensive on a software
// rasterizer: the settings screen drew the 1024x600 wallpaper scaled up to
// 1280x720 every frame and ran at 8 fps.
func wallpaperScaled() *ebiten.Image {
        wallScaleOnce.Do(func() {
                w := getWallpaper()
                if w == nil {
                        return
                }
                img := ebiten.NewImage(LogicalW, LogicalH)
                op := &ebiten.DrawImageOptions{}
                op.GeoM.Scale(float64(LogicalW)/float64(w.Bounds().Dx()), float64(LogicalH)/float64(w.Bounds().Dy()))
                img.DrawImage(w, op)
                wallScaled = img
        })
        return wallScaled
}

func drawWallpaper(screen *ebiten.Image) {
        if w := wallpaperScaled(); w != nil {
                screen.DrawImage(w, &ebiten.DrawImageOptions{})
                return
        }
        fillRect(screen, 0, 0, LogicalW, LogicalH, RGBA{0xf0, 0xf4, 0xf8, 0xff})
}

func drawWallDim(screen *ebiten.Image, a float64) {
        fillRect(screen, 0, 0, LogicalW, LogicalH, RGBA{0x0a, 0x0c, 0x0e, uint8(a * 255)})
}














func roundRectPoints(x, y, w, h, r float64) [][2]float64 {
        if w <= 0 || h <= 0 {
                return nil
        }
        if r < 0 {
                r = 0
        }
        if r*2 > w {
                r = w / 2
        }
        if r*2 > h {
                r = h / 2
        }
        pts := make([][2]float64, 0, 40)
        pts = append(pts, [2]float64{x + r, y})
        pts = append(pts, [2]float64{x + w - r, y})
        
        pts = append(pts, arcPoints(x+w-r, y+r, r, -math.Pi/2, 0)...)
        pts = append(pts, [2]float64{x + w, y + h - r})
        
        pts = append(pts, arcPoints(x+w-r, y+h-r, r, 0, math.Pi/2)...)
        pts = append(pts, [2]float64{x + r, y + h})
        
        pts = append(pts, arcPoints(x+r, y+h-r, r, math.Pi/2, math.Pi)...)
        pts = append(pts, [2]float64{x, y + r})
        
        pts = append(pts, arcPoints(x+r, y+r, r, math.Pi, math.Pi*1.5)...)
        return pts
}



func arcPoints(cx, cy, r, a0, a1 float64) [][2]float64 {
        span := a1 - a0
        if span <= 0 {
                return nil
        }
        n := int(math.Ceil(span * r / 2.2))
        if n < 2 {
                n = 2
        }
        if n > 64 {
                n = 64
        }
        pts := make([][2]float64, 0, n)
        for i := 1; i <= n; i++ {
                a := a0 + span*float64(i)/float64(n)
                pts = append(pts, [2]float64{cx + math.Cos(a)*r, cy + math.Sin(a)*r})
        }
        return pts
}


func strokePolyline(screen *ebiten.Image, pts [][2]float64, width float64, c RGBA, closed bool) {
        if len(pts) < 2 {
                return
        }
        var p vector.Path
        p.MoveTo(float32(pts[0][0]), float32(pts[0][1]))
        for _, pt := range pts[1:] {
                p.LineTo(float32(pt[0]), float32(pt[1]))
        }
        if closed {
                p.Close()
        }
        vs, is := p.AppendVerticesAndIndicesForStroke(nil, nil, &vector.StrokeOptions{Width: float32(width)})
        tintVertices(vs, c)
        op := &ebiten.DrawTrianglesOptions{}
        op.AntiAlias = aaEnabled()
        op.FillRule = ebiten.FillRuleNonZero
        screen.DrawTriangles(vs, is, white1x1(), op)
}






func fillRoundRect(screen *ebiten.Image, x, y, w, h, r float64, c RGBA) {
        fillPoly(screen, roundRectPoints(x, y, w, h, r), c)
}



// Anti-aliasing of the vector shapes.
//
// Every shape is drawn with ebiten.DrawTriangles and AntiAlias=true, which
// makes the whole framebuffer multisampled. On a software rasterizer
// (llvmpipe - a machine without hardware GL, a VM, a headless CI box) that
// costs about seven times the frame time: the dashboard ran at 8 fps instead
// of 60, and the dev console at 4 fps.
//
// The flag is therefore decided automatically from the measured frame rate:
// it stays on while the renderer can afford it and is switched off once the
// shell is clearly too slow. FLW_AA=1 / FLW_AA=0 override the decision.
var (
        aaOn        = true
        aaDecided   bool
        aaLowSamples int
        aaTuneOnce  sync.Once
)

// aaEnabled reports whether the shapes are drawn anti-aliased.
func aaEnabled() bool {
        if v := os.Getenv("FLW_AA"); v != "" {
                return v != "0"
        }
        return aaOn
}

// SetAA decides whether the anti-aliasing is used. It is called by the
// frame rate monitor of the shell.
func SetAA(on bool) {
        if aaDecided {
                return
        }
        if on == aaOn {
                return
        }
        aaOn = on
        aaDecided = true
        if on {
                fmt.Fprintf(os.Stderr, "nuggetos: anti-aliasing on (the renderer can afford it)\n")
        } else {
                fmt.Fprintf(os.Stderr, "nuggetos: anti-aliasing off (the renderer is too slow)\n")
        }
}

// aaAutoTune switches the anti-aliasing off when the frame rate stays low.
// It is called from the frame loop with the measured frames per second.
func aaAutoTune(fps float64) {
        if v := os.Getenv("FLW_AA"); v != "" {
                return
        }
        if aaDecided || !aaOn {
                return
        }
        if fps > 0 && fps < 24 {
                aaLowSamples++
                if aaLowSamples >= 2 {
                        SetAA(false)
                }
                return
        }
        aaLowSamples = 0
}

func fillRoundRectGradV(screen *ebiten.Image, x, y, w, h, r float64, cTop, cBot RGBA) {
        pts := roundRectPoints(x, y, w, h, r)
        if len(pts) < 3 {
                return
        }
        var p vector.Path
        p.MoveTo(float32(pts[0][0]), float32(pts[0][1]))
        for _, pt := range pts[1:] {
                p.LineTo(float32(pt[0]), float32(pt[1]))
        }
        p.Close()
        vs, is := p.AppendVerticesAndIndicesForFilling(nil, nil)
        tr, tg, tb, ta := float32(cTop.R)/255, float32(cTop.G)/255, float32(cTop.B)/255, float32(cTop.A)/255
        br, bg, bb, ba := float32(cBot.R)/255, float32(cBot.G)/255, float32(cBot.B)/255, float32(cBot.A)/255
        for i := range vs {
                t := clamp01((float64(vs[i].DstY) - y) / h)
                vs[i].ColorR = tr + (br-tr)*float32(t)
                vs[i].ColorG = tg + (bg-tg)*float32(t)
                vs[i].ColorB = tb + (bb-tb)*float32(t)
                vs[i].ColorA = ta + (ba-ta)*float32(t)
        }
        op := &ebiten.DrawTrianglesOptions{}
        op.AntiAlias = aaEnabled()
        op.FillRule = ebiten.FillRuleNonZero
        screen.DrawTriangles(vs, is, white1x1(), op)
}



func strokeRoundRect(screen *ebiten.Image, x, y, w, h, r, t float64, c RGBA) {
        strokePolyline(screen, roundRectPoints(x, y, w, h, r), t, c, true)
}



func strokeRoundRectDashed(screen *ebiten.Image, x, y, w, h, r, t float64, c RGBA, dash, gap float64) {
        pts := roundRectPoints(x, y, w, h, r)
        if len(pts) < 2 {
                return
        }
        
        const step = 1.6
        var samples [][2]float64
        for i := 0; i < len(pts); i++ {
                a := pts[i]
                b := pts[(i+1)%len(pts)]
                dx, dy := b[0]-a[0], b[1]-a[1]
                l := math.Hypot(dx, dy)
                if l < 1e-6 {
                        continue
                }
                n := int(math.Ceil(l / step))
                for s := 0; s < n; s++ {
                        f := float64(s) / float64(n)
                        samples = append(samples, [2]float64{a[0] + dx*f, a[1] + dy*f})
                }
        }
        
        pen := true
        rem := dash
        var cur [][2]float64
        flush := func() {
                if len(cur) >= 2 {
                        strokePolyline(screen, cur, t, c, false)
                }
                cur = cur[:0]
        }
        for i := 0; i < len(samples); i++ {
                p := samples[i]
                if pen {
                        cur = append(cur, p)
                }
                if i+1 < len(samples) {
                        q := samples[i+1]
                        d := math.Hypot(q[0]-p[0], q[1]-p[1])
                        for rem -= d; rem <= 0; {
                                if pen {
                                        flush()
                                        pen, rem = false, gap+rem
                                } else {
                                        pen, rem = true, dash+rem
                                }
                        }
                }
        }
        flush()
}

func tintVertices(vs []ebiten.Vertex, c RGBA) {
        r := float32(c.R) / 255
        g := float32(c.G) / 255
        b := float32(c.B) / 255
        a := float32(c.A) / 255
        for i := range vs {
                vs[i].ColorR *= r
                vs[i].ColorG *= g
                vs[i].ColorB *= b
                vs[i].ColorA *= a
        }
}



func strokeArc(screen *ebiten.Image, cx, cy, r, a0, a1, width float64, c RGBA) {
        if a1 < a0 {
                a1 += math.Pi * 2
        }
        pts := [][2]float64{{cx + math.Cos(a0)*r, cy + math.Sin(a0)*r}}
        pts = append(pts, arcPoints(cx, cy, r, a0, a1)...)
        strokePolyline(screen, pts, width, c, false)
}

func fillPoly(screen *ebiten.Image, pts [][2]float64, c RGBA) {
        if len(pts) < 3 {
                return
        }
        var p vector.Path
        p.MoveTo(float32(pts[0][0]), float32(pts[0][1]))
        for _, pt := range pts[1:] {
                p.LineTo(float32(pt[0]), float32(pt[1]))
        }
        p.Close()
        vs, is := p.AppendVerticesAndIndicesForFilling(nil, nil)
        tintVertices(vs, c)
        op := &ebiten.DrawTrianglesOptions{}
        op.AntiAlias = aaEnabled()
        op.FillRule = ebiten.FillRuleNonZero
        screen.DrawTriangles(vs, is, white1x1(), op)
}

func strokeLine(screen *ebiten.Image, x1, y1, x2, y2, width float64, c RGBA) {
        vector.StrokeLine(screen, float32(x1), float32(y1), float32(x2), float32(y2), float32(width), c, true)
}

func strokeCircle(screen *ebiten.Image, cx, cy, r, width float64, c RGBA) {
        vector.StrokeCircle(screen, float32(cx), float32(cy), float32(r), float32(width), c, true)
}

func fillCircle(screen *ebiten.Image, cx, cy, r float64, c RGBA) {
        vector.DrawFilledCircle(screen, float32(cx), float32(cy), float32(r), c, true)
}



func dropShadow(screen *ebiten.Image, x, y, w, h float64) {
        fillRoundRectGradV(screen, x-3, y-1, w+6, h+7, 14, RGBA{0x00, 0x00, 0x00, 0x2c}, RGBA{0x00, 0x00, 0x00, 0x08})
        fillRoundRectGradV(screen, x-7, y+2, w+14, h+14, 18, RGBA{0x00, 0x00, 0x00, 0x16}, RGBA{0x00, 0x00, 0x00, 0x00})
}



func drawGloss(screen *ebiten.Image, x, y, w, h, r float64, frac, strength float64) {
        gh := h * frac
        if gh < 2 {
                return
        }
        gr := r * 0.7
        if gr > gh {
                gr = gh
        }
        c0 := RGBA{0xff, 0xff, 0xff, uint8(0xff * strength)}
        c1 := RGBA{0xff, 0xff, 0xff, uint8(0xff * strength * 0.25)}
        fillRoundRectGradV(screen, x+1.5, y+1.5, w-3, gh, gr, c0, c1)
}



func drawInnerTopLine(screen *ebiten.Image, x, y, w, r float64, c RGBA) {
        inset := r * 0.55
        if inset > 6 {
                inset = 6
        }
        strokeLine(screen, x+inset, y+1.4, x+w-inset, y+1.4, 1, c)
}



func drawPanelChrome(screen *ebiten.Image, x, y, w, h, r float64) {
        dropShadow(screen, x, y, w, h)
        fillRoundRectGradV(screen, x, y, w, h, r, MgPanelGradTop, MgPanelGradBot)
        strokeRoundRect(screen, x, y, w, h, r, 1, MgPanelBorder)
        drawInnerTopLine(screen, x, y, w, r, RGBA{0xff, 0xff, 0xff, 0x9e})
}


func drawTileChrome(screen *ebiten.Image, x, y, w, h, r float64) {
        fillRoundRectGradV(screen, x, y, w, h, r, RGBA{0xff, 0xff, 0xff, 0xff}, MgTileGradBot)
        strokeRoundRect(screen, x+0.5, y+0.5, w-1, h-1, r-0.5, 1, MgTileBorder)
        drawInnerTopLine(screen, x, y, w, r, RGBA{0xff, 0xff, 0xff, 0xc8})
}



func drawButtonPill(screen *ebiten.Image, x, y, w, h float64, accent RGBA, press, glow float64) {
        cx, cy := x+w/2, y+h/2
        if press > 0 {
                w -= w * 0.04 * press
                h -= h * 0.06 * press
        }
        x, y = cx-w/2, cy-h/2
        if glow > 0 {
                fillRoundRect(screen, x-3, y-2, w+6, h+5, h/2+3, RGBA{accent.R, accent.G, accent.B, uint8(0x38 * glow)})
        }
        fillRoundRectGradV(screen, x, y, w, h, h/2,
                mixRGBA(accent, RGBA{0xff, 0xff, 0xff, 0xff}, 0.22), mixRGBA(accent, RGBA{0x00, 0x00, 0x00, 0xff}, 0.14))
        drawGloss(screen, x, y, w, h, h/2, 0.52, 0.34)
        strokeRoundRect(screen, x+0.5, y+0.5, w-1, h-1, h/2-0.5, 1, mixRGBA(accent, RGBA{0x00, 0x00, 0x00, 0xff}, 0.35))
}



func drawKeycap(screen *ebiten.Image, x, y, h float64, label string, c RGBA) {
        f := regular(float64(int(h * 0.52)))
        w, _ := measure(label, f, h*0.7)
        w += h * 0.72
        fillRoundRectGradV(screen, x, y+1.2, w, h+1.4, 4, RGBA{0x00, 0x00, 0x00, 0x26}, RGBA{0x00, 0x00, 0x00, 0x0d})
        fillRoundRectGradV(screen, x, y, w, h, 4, mixRGBA(c, RGBA{0xff, 0xff, 0xff, 0xff}, 0.55), c)
        strokeRoundRect(screen, x+0.5, y+0.5, w-1, h-1, 3.5, 1, mixRGBA(c, RGBA{0x00, 0x00, 0x00, 0xff}, 0.38))
        drawTextCenter(screen, label, x+w/2, y+h/2-f.Size*0.62, f, mixRGBA(c, RGBA{0x00, 0x00, 0x00, 0xff}, 0.66))
}


func keycapWidth(label string, h float64) float64 {
        f := regular(float64(int(h * 0.52)))
        w, _ := measure(label, f, h*0.7)
        return w + h*0.72
}


func drawChip(screen *ebiten.Image, x, y float64, label string, c RGBA, filled bool) {
        f := regular(10)
        w, _ := measure(label, f, 13)
        h := 18.0
        if filled {
                fillRoundRectGradV(screen, x, y, w+14, h, h/2, mixRGBA(c, RGBA{0xff, 0xff, 0xff, 0xff}, 0.18), mixRGBA(c, RGBA{0x00, 0x00, 0x00, 0xff}, 0.10))
                drawText(screen, label, x+7, y+4, f, MgLight)
        } else {
                fillRoundRectGradV(screen, x, y, w+14, h, h/2, RGBA{0xff, 0xff, 0xff, 0xff}, MgTileGradBot)
                strokeRoundRect(screen, x+0.5, y+0.5, w+13, h-1, h/2-0.5, 1, c)
                drawText(screen, label, x+7, y+4, f, c)
        }
}


func drawStatusDot(screen *ebiten.Image, cx, cy, r float64, c RGBA, t float64, pulse bool) {
        if pulse {
                p := 0.5 + 0.5*math.Sin(t*3.4)
                fillCircle(screen, cx, cy, r*(1.5+p*0.9), RGBA{c.R, c.G, c.B, uint8(0x30 + 0x28*p)})
        }
        fillCircle(screen, cx, cy, r, c)
        fillCircle(screen, cx-r*0.28, cy-r*0.28, r*0.34, RGBA{0xff, 0xff, 0xff, 0x99})
}


func drawSpinner(screen *ebiten.Image, cx, cy, r, t float64, c RGBA) {
        a := t * 4.2
        strokeArc(screen, cx, cy, r, a, a+math.Pi*1.25, 3, c)
}

func mixRGBA(a, b RGBA, t float64) RGBA {
        t = clamp01(t)
        return RGBA{
                uint8(float64(a.R) + (float64(b.R)-float64(a.R))*t),
                uint8(float64(a.G) + (float64(b.G)-float64(a.G))*t),
                uint8(float64(a.B) + (float64(b.B)-float64(a.B))*t),
                uint8(float64(a.A) + (float64(b.A)-float64(a.A))*t),
        }
}






func iconHouse(screen *ebiten.Image, cx, cy, s float64, c RGBA) {
        roof := [][2]float64{{cx - s, cy - s * 0.15}, {cx, cy - s * 0.95}, {cx + s, cy - s * 0.15}}
        strokePolyline(screen, roof, 2, c, false)
        strokeRoundRect(screen, cx-s*0.62, cy-s*0.15, s*1.24, s*0.95, 1.5, 2, c)
        fillRect(screen, cx-s*0.12, cy+s*0.28, s*0.24, s*0.52, c)
}


func iconAppGrid(screen *ebiten.Image, cx, cy, s float64, c RGBA) {
        g := s * 0.52
        strokeRoundRect(screen, cx-g*1.12, cy-g*1.12, g, g, 2, 2, c)
        strokeRoundRect(screen, cx+g*0.12, cy-g*1.12, g, g, 2, 2, c)
        strokeRoundRect(screen, cx-g*1.12, cy+g*0.12, g, g, 2, 2, c)
        strokeRoundRect(screen, cx+g*0.12, cy+g*0.12, g, g, 2, 2, c)
}


func iconZones(screen *ebiten.Image, cx, cy, s float64, c RGBA) {
        strokeRoundRect(screen, cx-s, cy-s*0.78, s*2, s*1.56, 2, 2, c)
        strokeLine(screen, cx, cy-s*0.78, cx, cy+s*0.78, 1.6, c)
        strokeLine(screen, cx-s, cy, cx+s, cy, 1.6, c)
        fillRoundRect(screen, cx+s*0.08, cy-s*0.7, s*0.84, s*0.62, 1.5, c)
}


func iconSearch(screen *ebiten.Image, cx, cy, s float64, c RGBA) {
        strokeCircle(screen, cx-s*0.2, cy-s*0.2, s*0.62, 2, c)
        strokeLine(screen, cx+s*0.28, cy+s*0.28, cx+s*0.85, cy+s*0.85, 2, c)
}

func iconPlusGlyph(screen *ebiten.Image, cx, cy, s float64, c RGBA) {
        strokeLine(screen, cx-s*0.7, cy, cx+s*0.7, cy, 2.2, c)
        strokeLine(screen, cx, cy-s*0.7, cx, cy+s*0.7, 2.2, c)
}

func iconXGlyph(screen *ebiten.Image, cx, cy, s float64, c RGBA) {
        strokeLine(screen, cx-s*0.6, cy-s*0.6, cx+s*0.6, cy+s*0.6, 2.2, c)
        strokeLine(screen, cx+s*0.6, cy-s*0.6, cx-s*0.6, cy+s*0.6, 2.2, c)
}

func iconChevron(screen *ebiten.Image, cx, cy, s float64, c RGBA) {
        fillPoly(screen, [][2]float64{{cx - s * 0.4, cy - s}, {cx + s * 0.6, cy}, {cx - s * 0.4, cy + s}}, c)
}

func iconPlayTriangle(screen *ebiten.Image, cx, cy, s float64, c RGBA) {
        fillPoly(screen, [][2]float64{{cx - s * 0.5, cy - s}, {cx + s * 0.8, cy}, {cx - s * 0.5, cy + s}}, c)
}


func powerSymbol(screen *ebiten.Image, cx, cy, s float64, c RGBA, width float64) {
        strokeLine(screen, cx, cy-s*0.85, cx, cy-s*0.1, width, c)
        strokeArc(screen, cx, cy+0.08*s, s*0.62, -math.Pi/2.6, math.Pi/2.6+math.Pi, width, c)
}






func iconGear(screen *ebiten.Image, cx, cy, s float64, c RGBA) {
        for i := 0; i < 8; i++ {
                a := float64(i) * math.Pi / 4
                x1 := cx + math.Cos(a)*s*0.78
                y1 := cy + math.Sin(a)*s*0.78
                x2 := cx + math.Cos(a)*s
                y2 := cy + math.Sin(a)*s
                strokeLine(screen, x1, y1, x2, y2, 3.2, c)
        }
        strokeCircle(screen, cx, cy, s*0.78, 3.2, c)
        fillCircle(screen, cx, cy, s*0.3, c)
}

func iconStar(screen *ebiten.Image, cx, cy, s float64, c RGBA) {
        pts := make([][2]float64, 10)
        for i := 0; i < 10; i++ {
                r := s
                if i%2 == 1 {
                        r = s * 0.44
                }
                a := float64(i)*math.Pi/5 - math.Pi/2
                pts[i] = [2]float64{cx + math.Cos(a)*r, cy + math.Sin(a)*r}
        }
        fillPoly(screen, pts, c)
}

func iconTerminal(screen *ebiten.Image, cx, cy, s float64, c RGBA) {
        strokeRoundRect(screen, cx-s, cy-s*0.75, s*2, s*1.5, 3, 2, c)
        strokeLine(screen, cx-s*0.6, cy-s*0.25, cx-s*0.1, cy+s*0.1, 2, c)
        strokeLine(screen, cx-s*0.6, cy+s*0.25, cx-s*0.35, cy+s*0.05, 2, c)
        strokeLine(screen, cx-s*0.1, cy+s*0.25, cx+s*0.55, cy+s*0.25, 2, c)
}


func iconMonitor(screen *ebiten.Image, cx, cy, s float64, c RGBA) {
        strokeRoundRect(screen, cx-s, cy-s*0.72, s*2, s*1.16, 3, 2, c)
        strokeLine(screen, cx, cy+s*0.44, cx, cy+s*0.78, 2, c)
        strokeLine(screen, cx-s*0.4, cy+s*0.78, cx+s*0.4, cy+s*0.78, 2, c)
}

func iconSpeaker(screen *ebiten.Image, cx, cy, s float64, c RGBA) {
        fillPoly(screen, [][2]float64{{cx - s, cy - s * 0.4}, {cx - s * 0.45, cy - s * 0.4}, {cx + s * 0.15, cy - s}, {cx + s * 0.15, cy + s}, {cx - s * 0.45, cy + s * 0.4}, {cx - s, cy + s * 0.4}}, c)
        strokeArc(screen, cx+s*0.4, cy, s*0.42, -math.Pi/3, math.Pi/3, 2, c)
        strokeArc(screen, cx+s*0.4, cy, s*0.8, -math.Pi/3, math.Pi/3, 2, c)
}

func iconInfo(screen *ebiten.Image, cx, cy, s float64, c RGBA) {
        strokeCircle(screen, cx, cy, s, 2, c)
        fillCircle(screen, cx, cy-s*0.5, s*0.1, c)
        strokeLine(screen, cx, cy-s*0.18, cx, cy+s*0.5, 2.2, c)
}

func iconCloseApp(screen *ebiten.Image, cx, cy, s float64, c RGBA) {
        strokeLine(screen, cx-s*0.6, cy-s*0.6, cx+s*0.6, cy+s*0.6, 2.4, c)
        strokeLine(screen, cx+s*0.6, cy-s*0.6, cx-s*0.6, cy+s*0.6, 2.4, c)
}

func iconDownload(screen *ebiten.Image, cx, cy, s float64, c RGBA) {
        strokeLine(screen, cx, cy-s, cx, cy+s*0.3, 2.4, c)
        strokeLine(screen, cx-s*0.45, cy-s*0.05, cx, cy+s*0.42, 2.4, c)
        strokeLine(screen, cx+s*0.45, cy-s*0.05, cx, cy+s*0.42, 2.4, c)
        strokeLine(screen, cx-s*0.8, cy+s*0.8, cx+s*0.8, cy+s*0.8, 2.4, c)
}





func drawMeegoBanner(screen *ebiten.Image, msg string, ttl float64, danger bool) {
        elapsed := 3.4 - ttl
        
        f := 0.0
        if elapsed < 0.26 {
                f = 1 - easeOutCubic(elapsed/0.26)
        }
        if ttl < 0.3 {
                f = easeInCubic(1 - ttl/0.3) 
        }
        alphaF := 1.0
        if ttl < 0.3 {
                alphaF = ttl / 0.3
        }
        mf := medium(15)
        lines := wrapLines(msg, mf, 430)
        boxH := 36.0 + float64(len(lines)-1)*20
        maxW := 0.0
        for _, ln := range lines {
                w, _ := measure(ln, mf, 20)
                if w > maxW {
                        maxW = w
                }
        }
        boxW := maxW + 84
        x := float64(LogicalW)/2 - boxW/2
        y := toolbarH + 12 - f*(boxH + toolbarH + 16)
        
        accent := MgCyan
        icPath := "apps/myzone.png"
        if danger {
                accent = MgRed
                icPath = "status/battery-caution.png"
        }
        fillRoundRectGradV(screen, x, y, boxW, boxH, boxH/2,
                withAlphaF(RGBA{0x22, 0x26, 0x2a, 1}, alphaF), withAlphaF(RGBA{0x0c, 0x0e, 0x10, 1}, alphaF))
        strokeRoundRect(screen, x+0.5, y+0.5, boxW-1, boxH-1, boxH/2-0.5, 1, withAlphaF(RGBA{0x4a, 0x52, 0x58, 1}, alphaF))
        drawGloss(screen, x, y, boxW, boxH, boxH/2, 0.42, 0.10*alphaF)
        
        fillRoundRect(screen, x+14, y+boxH/2-9, 3, 18, 1.5, withAlphaF(accent, alphaF))
        drawMeegoIconC(screen, icPath, x+34, y+boxH/2, 20)
        fillCircle(screen, x+boxW-20, y+boxH/2, 3, withAlphaF(accent, 0.9*alphaF))
        for i, ln := range lines {
                drawText(screen, ln, x+52, y+boxH/2-9+float64(i)*20, mf, withAlphaF(MgLight, alphaF))
        }
}





func tapPoint() (float64, float64, bool) {
        tids := inpututil.AppendJustPressedTouchIDs(nil)
        if len(tids) == 0 {
                return 0, 0, false
        }
        x, y := ebiten.TouchPosition(tids[0])
        return float64(x), float64(y), true
}

func inRect(px, py, x, y, w, h float64) bool {
        return px >= x && px <= x+w && py >= y && py <= y+h
}

func wrapLines(s string, f *text.GoTextFace, maxW float64) []string {
        words := strings.Fields(s)
        if len(words) == 0 {
                return []string{""}
        }
        var out []string
        cur := words[0]
        for _, w := range words[1:] {
                try := cur + " " + w
                width, _ := measure(try, f, f.Size*1.35)
                if width > maxW && cur != "" {
                        out = append(out, cur)
                        cur = w
                } else {
                        cur = try
                }
        }
        out = append(out, cur)
        if len(out) > 2 {
                out = out[:2]
        }
        return out
}





type batteryInfo struct {
        Pct     int
        Chg     bool
        Present bool
}

func readBattery() batteryInfo {
        info := batteryInfo{Pct: 100, Present: false}
        if data, err := os.ReadFile("/sys/class/power_supply/BAT0/capacity"); err == nil {
                var p int
                if _, err := fmt.Sscanf(strings.TrimSpace(string(data)), "%d", &p); err == nil && p >= 0 && p <= 100 {
                        info.Pct = p
                        info.Present = true
                }
        }
        if data, err := os.ReadFile("/sys/class/power_supply/BAT0/status"); err == nil {
                info.Chg = strings.Contains(string(data), "Charging")
        }
        return info
}

func batteryIconPath(b batteryInfo) string {
        if !b.Present {
                return "status/battery-full.png"
        }
        switch {
        case b.Pct > 80:
                return "status/battery-full.png"
        case b.Pct > 45:
                return "status/battery-good.png"
        default:
                return "status/battery-caution.png"
        }
}

func clockText() string {
        return time.Now().Format("15:04")
}

func dateText() string {
        return time.Now().Format("Mon Jan 2")
}






func measureTextW(s string, f *text.GoTextFace) float64 {
        w, _ := measure(s, f, f.Size*1.35)
        return w
}



func clampText(s string, f *text.GoTextFace, maxW float64) string {
        if s == "" || maxW <= 8 {
                return s
        }
        if measureTextW(s, f) <= maxW {
                return s
        }
        runes := []rune(s)
        lo, hi := 0, len(runes) 
        ell := measureTextW("…", f)
        for lo < hi {
                mid := (lo + hi + 1) / 2
                if measureTextW(string(runes[:mid]), f)+ell <= maxW {
                        lo = mid
                } else {
                        hi = mid - 1
                }
        }
        if lo == 0 {
                return "…"
        }
        return string(runes[:lo]) + "…"
}


func itemAlpha(vis, delay float64) float64 {
        return clamp01((vis*1.3 - delay) / 0.3)
}


func selectedB(b bool) float64 {
        if b {
                return 0.7
        }
        return 0
}
