package nuggetos

import (
	"strconv"
        "fmt"
        "math"
        "strings"

        "github.com/hajimehoshi/ebiten/v2"
        "github.com/hajimehoshi/ebiten/v2/inpututil"

        "flwengine/internal/buildinfo"
        "flwengine/internal/plat"
        "flwengine/internal/supervisor"
)

type settingsItem struct {
        label  string
        value  string
        action func(a *app, si *settingsItem)
}

type settingsCat struct {
        label string
        icon  func(screen *ebiten.Image, cx, cy, s float64, c RGBA)
        rows  []settingsItem
}

type settingsScreen struct {
        cats    []settingsCat
        catSel  int
        rowSel  int
        inPanel bool
        t       float64

        reveal    float64
        catFade   []float64
        rowFade   []float64
        pulse     float64
        lastCat   int
        lastRow   int
        catSwitch float64
        flashT    float64
        flashRow  int

        // staticImg caches the parts of the screen that never change: the
        // wallpaper, the panel chrome, the category tiles (with their icons
        // and labels) and the row labels. Only the selection plates, the
        // values, the flash and the footer are drawn live. Without the cache
        // the screen redrew about two million pixels per frame, which is why
        // it ran at 9 fps on a software renderer.
        staticImg *ebiten.Image
        staticKey string
}

var resolutions = []string{"816x624", "1024x768", "1280x720", "1600x900", "1920x1080"}

func newSettingsScreen() *settingsScreen {
        s := &settingsScreen{}
        s.build()
        return s
}

func (s *settingsScreen) build() {
        st := supervisor.M().Settings()
        osInfo, _ := plat.Detect()
        s.reveal = 0
        s.cats = []settingsCat{
                {
                        label: "display",
                        icon:  iconMonitor,
                        rows: []settingsItem{
                                {label: "display mode", value: st.DisplayMode, action: func(a *app, si *settingsItem) {
                                        if si.value == "windowed" {
                                                si.value = "fullscreen"
                                        } else {
                                                si.value = "windowed"
                                        }
                                        supervisor.M().UpdateSettings(func(x *supervisor.Settings) { x.DisplayMode = si.value })
                                        applyDisplayMode(si.value)
                                }},
                                {label: "resolution", value: st.Resolution, action: func(a *app, si *settingsItem) {
                                        idx := 0
                                        for i, r := range resolutions {
                                                if r == si.value {
                                                        idx = i
                                                }
                                        }
                                        idx = (idx + 1) % len(resolutions)
                                        si.value = resolutions[idx]
                                        supervisor.M().UpdateSettings(func(x *supervisor.Settings) { x.Resolution = si.value })
                                        if w, h, ok := parseRes(si.value); ok {
                                                ebiten.SetWindowSize(w, h)
                                        }
                                }},
                        },
                },
                {
                        label: "sound",
                        icon:  iconSpeaker,
                        rows: []settingsItem{
                                {label: "master volume", value: fmt.Sprintf("%.0f%%", st.Volume*100), action: func(a *app, si *settingsItem) {
                                        v := st.Volume + 0.1
                                        if v > 1.0 {
                                                v = 0
                                        }
                                        supervisor.M().UpdateSettings(func(x *supervisor.Settings) { x.Volume = v })
                                        st.Volume = v
                                        si.value = fmt.Sprintf("%.0f%%", v*100)
                                        SetSoundVolume(v)
                                }},
                                {label: "ui sounds", value: onOff(st.UISounds), action: func(a *app, si *settingsItem) {
                                        v := !st.UISounds
                                        supervisor.M().UpdateSettings(func(x *supervisor.Settings) { x.UISounds = v })
                                        st.UISounds = v
                                        si.value = onOff(v)
                                        if v {
                                                SetSoundVolume(st.Volume)
                                        } else {
                                                SetSoundVolume(0)
                                        }
                                }},
                        },
                },
                {
                        label: "system",
                        icon:  iconInfo,
                        rows: []settingsItem{
                                {label: "engine", value: "flower engine " + buildinfo.Version, action: nil},
                                {label: "build type", value: buildinfo.Type(), action: nil},
                                {label: "host", value: systemSummary(osInfo), action: nil},
                                {label: "workspace", value: fmt.Sprintf("workspace%d", st.Workspace), action: nil},
                                {label: "cursor theme", value: "meego (nuggetos)", action: nil},
                        },
                },
        }
        if s.catSel >= len(s.cats) {
                s.catSel = 0
        }
        if s.rowSel >= len(s.cats[s.catSel].rows) {
                s.rowSel = 0
        }
        s.catFade = make([]float64, len(s.cats))
        s.rowFade = make([]float64, len(s.cats[s.catSel].rows))
}

func onOff(v bool) string {
        if v {
                return "on"
        }
        return "off"
}

func systemSummary(osInfo plat.OSInfo) string {
        platName := osInfo.Platform
        if osInfo.Platform == "linux" {
                platName = osInfo.Platform + " " + osInfo.PlatformVer
        }
        if osInfo.Wine {
                platName += " (wine " + osInfo.WineVersion + ")"
        }
        return platName + " " + osInfo.Arch
}

func parseRes(s string) (int, int, bool) {
        parts := strings.SplitN(s, "x", 2)
        if len(parts) != 2 {
                return 0, 0, false
        }
        var w, h int
        if _, err := fmt.Sscanf(parts[0], "%d", &w); err != nil {
                return 0, 0, false
        }
        if _, err := fmt.Sscanf(parts[1], "%d", &h); err != nil {
                return 0, 0, false
        }
        return w, h, true
}

func applyDisplayMode(mode string) {
        if mode == "fullscreen" {
                ebiten.SetFullscreen(true)
        } else {
                ebiten.SetFullscreen(false)
        }
}


func (s *settingsScreen) flashChanged(row int) {
        s.flashT = 0.7
        s.flashRow = row
}

func (s *settingsScreen) update(dt float64, a *app) {
        s.t += dt
        if s.reveal < 1 {
                s.reveal = math.Min(1, s.reveal+dt/0.32)
        }
        s.catSwitch = ease01(s.catSwitch, dt, 0.20)
        if s.flashT > 0 {
                s.flashT -= dt
        }
        if len(s.catFade) != len(s.cats) {
                s.catFade = make([]float64, len(s.cats))
        }
        if len(s.rowFade) != len(s.cats[s.catSel].rows) {
                s.rowFade = make([]float64, len(s.cats[s.catSel].rows))
        }
        for i := range s.catFade {
                target := 0.0
                if s.catSel == i && !s.inPanel {
                        target = 1
                }
                s.catFade[i] = damp(s.catFade[i], target, 12, dt)
        }
        for i := range s.rowFade {
                target := 0.0
                if s.inPanel && s.rowSel == i {
                        target = 1
                }
                s.rowFade[i] = damp(s.rowFade[i], target, 12, dt)
        }
        if s.pulse > 0 {
                s.pulse -= dt
        }
        if s.catSel != s.lastCat {
                s.pulse = 0.24
                s.catSwitch = 0 
                s.lastCat = s.catSel
                if s.rowSel >= len(s.cats[s.catSel].rows) {
                        s.rowSel = 0
                }
        }
        if s.rowSel != s.lastRow {
                s.pulse = 0.24
                s.lastRow = s.rowSel
        }
        if inpututil.IsKeyJustPressed(ebiten.KeyEscape) {
                if s.inPanel {
                        s.inPanel = false
                        playSound(sndBack)
                        return
                }
                a.back()
                return
        }
        if s.inPanel {
                rows := s.cats[s.catSel].rows
                if inpututil.IsKeyJustPressed(ebiten.KeyUp) {
                        if s.rowSel > 0 {
                                s.rowSel--
                                playSound(sndMove)
                        }
                }
                if inpututil.IsKeyJustPressed(ebiten.KeyDown) {
                        if s.rowSel < len(rows)-1 {
                                s.rowSel++
                                playSound(sndMove)
                        }
                }
                if inpututil.IsKeyJustPressed(ebiten.KeyLeft) {
                        s.inPanel = false
                        playSound(sndBack)
                        return
                }
                if inpututil.IsKeyJustPressed(ebiten.KeyEnter) || inpututil.IsKeyJustPressed(ebiten.KeySpace) || inpututil.IsKeyJustPressed(ebiten.KeyRight) {
                        item := rows[s.rowSel]
                        if item.action != nil {
                                playSound(sndToggle)
                                item.action(a, &item)
                                s.cats[s.catSel].rows[s.rowSel] = item
                                s.flashChanged(s.rowSel)
                        }
                }
                s.pointerInput(a)
                return
        }
        if inpututil.IsKeyJustPressed(ebiten.KeyUp) {
                if s.catSel > 0 {
                        s.catSel--
                        playSound(sndMove)
                }
        }
        if inpututil.IsKeyJustPressed(ebiten.KeyDown) {
                if s.catSel < len(s.cats)-1 {
                        s.catSel++
                        playSound(sndMove)
                }
        }
        if inpututil.IsKeyJustPressed(ebiten.KeyRight) || inpututil.IsKeyJustPressed(ebiten.KeyEnter) || inpututil.IsKeyJustPressed(ebiten.KeySpace) {
                s.inPanel = true
                s.rowSel = 0
                playSound(sndSelect)
        }
        s.pointerInput(a)
}

func (s *settingsScreen) catY(i int) float64 { return 168.0 + float64(i)*62 }

func (s *settingsScreen) rowY(i int) float64 { return 150.0 + float64(i)*62 }



func (s *settingsScreen) pointerInput(a *app) {
        px, py, ok := tapOrClick()
        if !ok {
                mv := hoverPointerMoved()
                mx, my := ebiten.CursorPosition()
                hx, hy := float64(mx), float64(my)
                
                for i := range s.cats {
                        if inRect(hx, hy, 96, s.catY(i), 330, 56) {
                                uiCursor = curHand
                                if mv && s.catSel != i {
                                        s.catSel = i
                                        s.inPanel = false
                                        playSound(sndMove)
                                }
                                return
                        }
                }
                
                rows := s.cats[s.catSel].rows
                for i := range rows {
                        if inRect(hx, hy, 470, s.rowY(i), 714, 56) {
                                uiCursor = curHand
                                if !s.inPanel {
                                        s.inPanel = true
                                }
                                if mv && s.rowSel != i {
                                        s.rowSel = i
                                        playSound(sndMove)
                                }
                                return
                        }
                }
                return
        }
        
        for i := range s.cats {
                if inRect(px, py, 96, s.catY(i), 330, 56) {
                        if s.catSel != i || s.inPanel {
                                s.catSel = i
                                s.inPanel = false
                                playSound(sndMove)
                        } else {
                                s.inPanel = true
                                s.rowSel = 0
                                playSound(sndSelect)
                        }
                        return
                }
        }
        rows := s.cats[s.catSel].rows
        for i := range rows {
                if inRect(px, py, 470, s.rowY(i), 714, 56) {
                        s.inPanel = true
                        if s.rowSel != i {
                                s.rowSel = i
                                playSound(sndMove)
                                return
                        }
                        item := rows[s.rowSel]
                        if item.action != nil {
                                playSound(sndToggle)
                                item.action(a, &item)
                                s.cats[s.catSel].rows[s.rowSel] = item
                                s.flashChanged(s.rowSel)
                        }
                        return
                }
        }
        if px < 440 && s.inPanel {
                s.inPanel = false
                playSound(sndBack)
        }
}

// The settings screen drew the wallpaper (a full screen texture), the panel
// chrome (1152x620), the tile chrome and every icon on every frame. That is
// about two million pixels per frame, which costs ~100 ms on a software
// rasterizer: the screen ran at 9 fps.
//
// Everything that never changes is therefore rendered into a texture once
// (wallpaper, dim, panel chrome, header, category tiles with their icons and
// labels, the tile chrome, the row labels and separators). Only the parts
// that really change are drawn live: the selection plates, the row values,
// the value flash and the footer hints.
func (s *settingsScreen) draw(screen *ebiten.Image) {
        vis := easeOutCubic(s.reveal)
        sw := easeOutCubic(s.catSwitch)
        // While the panel animates everything is drawn live.
        animating := vis < 0.999 || sw < 0.999
        if animating {
                s.drawStatic(screen, vis, sw)
                s.drawDynamic(screen, vis, sw)
        } else {
                if img := s.staticImage(); img != nil {
                        drawImageOpaque(screen, img)
                } else {
                        s.drawStatic(screen, vis, sw)
                }
                s.drawDynamic(screen, vis, sw)
        }
}

// staticSignature describes everything the cached layer depends on.
func (s *settingsScreen) staticSignature() string {
        var b strings.Builder
        b.WriteString("v1")
        b.WriteString(strconv.Itoa(s.catSel))
        b.WriteString(strconv.Itoa(s.rowSel))
        if s.inPanel {
                b.WriteString("p")
        }
        cat := s.cats[s.catSel]
        for i := range cat.rows {
                b.WriteString(";")
                b.WriteString(cat.rows[i].label)
                b.WriteString("=")
                b.WriteString(cat.rows[i].value)
        }
        return b.String()
}

func (s *settingsScreen) staticImage() *ebiten.Image {
        key := s.staticSignature()
        if s.staticImg != nil && s.staticKey == key {
                return s.staticImg
        }
        if s.staticImg != nil {
                s.staticImg.Deallocate()
        }
        img := ebiten.NewImage(LogicalW, LogicalH)
        s.drawStatic(img, 1.0, 1.0)
        s.staticImg = img
        s.staticKey = key
        return img
}

// drawCatContent draws the icon tile, the icon and the label of a category.
// It is used both for the cached layer and to redraw the selected category
// on top of its selection plate.
func (s *settingsScreen) drawCatContent(screen *ebiten.Image, i int, y, ia float64) {
        cat := s.cats[i]
        ic := MgTextDim
        lc := MgText
        if s.catSel == i {
                ic = MgCyan
        }
        if cat.icon != nil {
                fillRoundRectGradV(screen, 118, y+13, 30, 30, 8, withAlphaF(RGBA{0xff, 0xff, 0xff, 0xff}, ia), withAlphaF(MgTileGradBot, ia))
                strokeRoundRect(screen, 118.5, y+13.5, 29, 29, 7.5, 1, withAlphaF(MgTileBorder, ia))
                cat.icon(screen, 133, y+28, 9, ic)
        }
        drawText(screen, cat.label, 162, y+17, medium(17), withAlphaF(lc, ia))
        if s.catSel == i && !s.inPanel {
                iconChevron(screen, 402, y+28, 6, withAlphaF(MgTextDim, ia))
        }
}

// drawStatic renders the parts of the screen that do not change.
func (s *settingsScreen) drawStatic(screen *ebiten.Image, vis, sw float64) {
        drawWallpaper(screen)
        drawWallDim(screen, 0.38)

        px, py, pw, ph := 64.0, 44.0, float64(LogicalW)-128, float64(LogicalH)-100
        pyy := py - (1-vis)*22
        drawPanelChrome(screen, px, pyy, pw, ph, 12)

        hal := vis
        fillRoundRectGradV(screen, px+22, pyy+16, 40, 40, 10, MgTileHoverTop, MgTileHover)
        strokeRoundRect(screen, px+22.5, pyy+16.5, 39, 39, 9.5, 1, MgTileHoverBd)
        drawMeegoIconC(screen, "apps/settings.png", px+42, pyy+36, 28)
        drawText(screen, "Settings", px+72, pyy+20, boldFace(22), withAlphaF(MgText, hal))
        drawTextRight(screen, "nuggetos preferences", px+pw-24, pyy+26, regular(12), withAlphaF(MgTextDim, hal))
        fillRect(screen, px, pyy+58, pw, 1, withAlphaF(MgPanelBorder, hal))

        for i := range s.cats {
                ia := itemAlpha(vis, 0.08+float64(i)*0.06)
                if ia <= 0 {
                        continue
                }
                y := s.catY(i) + (1-ia)*14
                s.drawCatContent(screen, i, y, ia)
        }

        cat := s.cats[s.catSel]
        pa := itemAlpha(vis, 0.2)
        if pa <= 0 {
                return
        }
        panelH := float64(len(cat.rows))*62 + 28
        rpx, rpy := 470.0+(1-sw)*24, 138.0+(1-pa)*16
        drawTileChrome(screen, rpx, rpy, 714, panelH, 12)
        for i, item := range cat.rows {
                y := s.rowY(i) + (1-pa)*16
                lc := MgText
                if item.action == nil {
                        lc = MgTextDim
                }
                drawText(screen, clampText(item.label, regular(17), 300), 508, y+17, regular(17), withAlphaF(lc, pa*sw))
                if item.action != nil {
                        iconChevron(screen, 1130, y+28, 6, withAlphaF(MgTextFaint, pa*sw))
                }
                if i < len(cat.rows)-1 {
                        fillRect(screen, 508, y+61, 640, 1, withAlphaF(MgDivider, pa*1.8))
                }
        }
}

// drawDynamic renders the parts of the screen that change.
func (s *settingsScreen) drawDynamic(screen *ebiten.Image, vis, sw float64) {
        px, py, pw, ph := 64.0, 44.0, float64(LogicalW)-128, float64(LogicalH)-100
        pyy := py - (1-vis)*22

        // The selection plates of the categories. The plate is drawn over the
        // cached layer, so the icon and the label of the selected category
        // are drawn again on top of it.
        for i := range s.cats {
                ia := itemAlpha(vis, 0.08+float64(i)*0.06)
                if ia <= 0 {
                        continue
                }
                y := s.catY(i) + (1-ia)*14
                fade := 0.0
                if i < len(s.catFade) {
                        fade = s.catFade[i]
                }
                if fade > 0.01 {
                        fillRoundRectGradV(screen, 96, y, 330, 56, 10, withAlphaF(MgTileHoverTop, fade), withAlphaF(MgTileHover, fade))
                        strokeRoundRect(screen, 96.5, y+0.5, 329, 55, 9.5, 1.5, withAlphaF(MgTileHoverBd, fade))
                        fillRoundRect(screen, 93, y+19, 3, 18, 1.5, withAlphaF(MgCyan, fade))
                        s.drawCatContent(screen, i, y, ia)
                }
        }

        cat := s.cats[s.catSel]
        pa := itemAlpha(vis, 0.2)
        if pa <= 0 {
                return
        }
        for i, item := range cat.rows {
                y := s.rowY(i) + (1-pa)*16
                fade := 0.0
                if i < len(s.rowFade) {
                        fade = s.rowFade[i]
                }
                if fade > 0.01 {
                        fillRoundRectGradV(screen, 482, y, 690, 56, 10, withAlphaF(MgTileHoverTop, fade), withAlphaF(MgTileHover, fade))
                        strokeRoundRect(screen, 482.5, y+0.5, 689, 55, 9.5, 1.5, withAlphaF(MgTileHoverBd, fade))
                        fillRoundRect(screen, 479, y+19, 3, 18, 1.5, withAlphaF(MgCyan, fade))
                        // The label is drawn again on top of the plate.
                        lc := MgText
                        if item.action == nil {
                                lc = MgTextDim
                        }
                        drawText(screen, clampText(item.label, regular(17), 300), 508, y+17, regular(17), withAlphaF(lc, pa*sw))
                }
                vc := MgTextDim
                if item.action == nil {
                        vc = MgTextFaint
                }
                if s.flashT > 0 && s.flashRow == i {
                        fl := clamp01(s.flashT / 0.7)
                        vc = mixRGBA(vc, MgGreen, fl*0.8)
                        fillRoundRectGradV(screen, 482, y, 690, 56, 10,
                                withAlphaF(mixRGBA(MgTileHoverTop, MgGreen, 0.25), 0.4*fl),
                                withAlphaF(mixRGBA(MgTileHover, MgGreen, 0.25), 0.4*fl))
                }
                if item.action != nil {
                        drawTextRight(screen, clampText(item.value, medium(15), 380), 1110, y+19, medium(15), withAlphaF(vc, pa*sw))
                } else {
                        drawTextRight(screen, clampText(item.value, medium(15), 420), 1136, y+19, medium(15), withAlphaF(vc, pa*sw))
                }
        }

        fal := itemAlpha(vis, 0.4)
        if fal <= 0 {
                return
        }
        y := pyy + ph - 34
        fillRect(screen, px+24, y-10, pw-48, 1, withAlphaF(MgDivider, fal*1.8))
        x := px + 28.0
        const kh = 20.0
        kc := withAlphaF(RGBA{0xd8, 0xda, 0xd6, 0xff}, fal)
        txt := withAlphaF(MgTextFaint, fal)
        f := regular(11)
        if s.inPanel {
                drawKeycap(screen, x, y, kh, "up/down", kc)
                x += keycapWidth("up/down", kh) + 8
                drawText(screen, "navigate", x, y+5, f, txt)
                x += measureTextW("navigate", f) + 16
                drawKeycap(screen, x, y, kh, "enter", kc)
                x += keycapWidth("enter", kh) + 8
                drawText(screen, "change", x, y+5, f, txt)
                x += measureTextW("change", f) + 16
                drawKeycap(screen, x, y, kh, "left", kc)
                x += keycapWidth("left", kh) + 8
                drawText(screen, "back", x, y+5, f, txt)
        } else {
                drawKeycap(screen, x, y, kh, "up/down", kc)
                x += keycapWidth("up/down", kh) + 8
                drawText(screen, "navigate", x, y+5, f, txt)
                x += measureTextW("navigate", f) + 16
                drawKeycap(screen, x, y, kh, "enter", kc)
                x += keycapWidth("enter", kh) + 8
                drawText(screen, "open category", x, y+5, f, txt)
                x += measureTextW("open category", f) + 16
                drawKeycap(screen, x, y, kh, "esc", kc)
                x += keycapWidth("esc", kh) + 8
                drawText(screen, "back", x, y+5, f, txt)
        }
}
