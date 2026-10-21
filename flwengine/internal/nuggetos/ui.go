package nuggetos

import (
        "image"
        "image/color"
        "math"
        "math/rand"
        "time"

        "github.com/hajimehoshi/ebiten/v2"
        "github.com/hajimehoshi/ebiten/v2/text/v2"

        "flwengine/internal/buildinfo"
)

func currentFeatures() buildinfo.Features {
        return buildinfo.ParseFeatures()
}

var (
        hoverMX = -1
        hoverMY = -1
)

// hoverPointerMoved reports whether the pointer moved since the previous
// call. Call at most once per frame from the screen that owns pointer hover.
// Hover must NEVER steal keyboard selection with a stationary cursor: the
// arrows would fight the hover every frame, and Enter would activate the
// item under the parked cursor instead of the keyboard-selected one.
func hoverPointerMoved() bool {
        mx, my := ebiten.CursorPosition()
        if hoverMX < 0 {
                hoverMX, hoverMY = mx, my
                return false
        }
        moved := mx != hoverMX || my != hoverMY
        hoverMX, hoverMY = mx, my
        return moved
}

var rng = rand.New(rand.NewSource(time.Now().UnixNano()))

func randFloat() float64 {
        return rng.Float64()
}

func drawText(screen *ebiten.Image, s string, x, y float64, f *text.GoTextFace, c RGBA) {
        op := &text.DrawOptions{}
        op.GeoM.Translate(x, y)
        op.ColorScale.ScaleWithColor(c)
        text.Draw(screen, s, f, op)
}

func drawTextCenter(screen *ebiten.Image, s string, cx, y float64, f *text.GoTextFace, c RGBA) {
        w, _ := measure(s, f, f.Size*1.35)
        drawText(screen, s, cx-w/2, y, f, c)
}

func drawTextRight(screen *ebiten.Image, s string, rx, y float64, f *text.GoTextFace, c RGBA) {
        w, _ := measure(s, f, f.Size*1.35)
        drawText(screen, s, rx-w, y, f, c)
}

var whiteTex *ebiten.Image

// blendReplace copies the source over the destination without reading it.
// Blitting a full screen cache image with the default alpha blending costs
// a read-modify-write of every pixel, which is expensive on a software
// renderer (llvmpipe). The cached images are opaque, so the destination is
// never needed.
var blendReplace = ebiten.Blend{
        BlendFactorSourceRGB:        ebiten.BlendFactorOne,
        BlendFactorSourceAlpha:      ebiten.BlendFactorOne,
        BlendFactorDestinationRGB:   ebiten.BlendFactorZero,
        BlendFactorDestinationAlpha: ebiten.BlendFactorZero,
        BlendOperationRGB:           ebiten.BlendOperationAdd,
        BlendOperationAlpha:         ebiten.BlendOperationAdd,
}

// drawImageOpaque draws an opaque image without alpha blending.
func drawImageOpaque(screen *ebiten.Image, img *ebiten.Image) {
        op := &ebiten.DrawImageOptions{}
        op.Blend = blendReplace
        screen.DrawImage(img, op)
}

func white1x1() *ebiten.Image {
        if whiteTex == nil {
                whiteTex = ebiten.NewImage(1, 1)
                whiteTex.Fill(color.White)
        }
        return whiteTex
}

func fillRect(screen *ebiten.Image, x, y, w, h float64, c RGBA) {
        op := &ebiten.DrawImageOptions{}
        op.GeoM.Scale(w, h)
        op.GeoM.Translate(x, y)
        op.ColorScale.Scale(float32(c.R)/255, float32(c.G)/255, float32(c.B)/255, float32(c.A)/255)
        screen.DrawImage(white1x1(), op)
}

func maxInt(a, b int) int {
        if a > b {
                return a
        }
        return b
}

func drawOverlay(screen *ebiten.Image, alpha float64) {
        fillRect(screen, 0, 0, LogicalW, LogicalH, RGBA{0, 0, 0, uint8(alpha * 255)})
}

func clamp01(v float64) float64 {
        if v < 0 {
                return 0
        }
        if v > 1 {
                return 1
        }
        return v
}

func easeOutCubic(t float64) float64 {
        t = clamp01(t)
        f := 1 - t
        return 1 - f*f*f
}

func easeInCubic(t float64) float64 {
        t = clamp01(t)
        return t * t * t
}

func easeOutBack(t float64) float64 {
        t = clamp01(t)
        c1, c3 := 1.70158, 2.70158
        return 1 + c3*math.Pow(t-1, 3) + c1*math.Pow(t-1, 2)
}


func approach(a, b, rate, dt float64) float64 {
        if a < b {
                a += rate * dt
                if a > b {
                        return b
                }
                return a
        }
        a -= rate * dt
        if a < b {
                return b
        }
        return a
}



func damp(a, b, lambda, dt float64) float64 {
        if a == b {
                return b
        }
        k := 1 - math.Exp(-lambda*dt)
        v := a + (b-a)*k
        if math.Abs(v-b) < 0.0015 {
                return b
        }
        return v
}


func ease01(p, dt, dur float64) float64 {
        if dur <= 0 {
                return 1
        }
        p += dt / dur
        if p > 1 {
                return 1
        }
        return p
}

func lerpF(a, b, t float64) float64 {
        return a + (b-a)*t
}

var _ = image.Point{}
var _ = math.Pi
