//go:build !flwnodash





package nuggetos

import (
        "fmt"
        "image"
        "math"
        "os"
        "strings"
        "sync"

        "github.com/hajimehoshi/ebiten/v2"
        "github.com/hajimehoshi/ebiten/v2/inpututil"
        _ "image/png"

        "flwengine/internal/buildinfo"
        "flwengine/internal/supervisor"
)














const (
        shellPanelH = 600.0
        shellAnimT  = 0.30
        tbIndAnimT  = 0.22 
        tipDelay     = 0.35 

        
        mzCardX  = 48.0
        mzCardY  = 130.0
        mzCardW  = 316.0
        mzCardH  = 408.0 
        mzLinkX  = 404.0
        mzLinkY0 = 162.0
        mzLinkW  = 404.0
        mzLinkH  = 52.0
        mzSysX   = 844.0
        mzSysY   = 130.0
        mzSysW   = 392.0

        apSearchY = 90.0
        apTileY   = 90.0
        apRowY0   = 190.0
        apRowH    = 84.0
        apRowW    = 560.0

        znColX  = 48.0
        znColY  = 130.0
        znColW  = 384.0
        znColH  = 320.0
        znHdrH  = 30.0  
        znPrevY = 172.0 
        znPrevH = 86.0
        znCardY = 270.0 
        znCardH = 156.0
)


const (
        zoneMy = iota
        zoneApps
        zoneZones
        zoneCount
)

var appCats = []string{"all", "games", "preferences", "system", "media", "web"}


var catAccent = map[string]RGBA{
        "all":         MgTextDim,
        "games":       MgCyan,
        "preferences": MgOrange,
        "system":      MgRed,
        "media":       MgGreen,
        "web":         RGBA{0x9b, 0x59, 0xd0, 0xff},
}

type shellApp struct {
        name, desc, cat string
        icon            string 
        run             func(a *app)
}

type hitTarget struct {
        x, y, w, h float64
        hand       bool
        run        func(a *app)
}

type meegoLink struct {
        label string
        icon  string
        sub   string
        run   func(a *app)
}

type ctxEntry struct {
        label string
        icon  func(screen *ebiten.Image, cx, cy, s float64, c RGBA)
        run   func(a *app)
}

type dashboard struct {
        zone    int 
        closing int 
        anim    float64
        closeAnim float64
        sel     [zoneCount]int
        t       float64

        
        selA      []float64     
        selPulse  float64       
        lastSel   [zoneCount]int
        tbIndFrom, tbIndTo float64 
        tbIndP    float64       
        tbIndA    float64       
        volAnim   float64       
        ctxAnim   float64       
        tipT      float64       
        tipShown  string        
        tipFade   float64       
        footerT   float64       
        volDrag   bool          
        pressT    float64       
        hovCat    int           
        hovTB     string        

        gameTitle string
        gameSub   string
        gameFiles int
        gameFound bool
        iconOnce  sync.Once
        iconTile  *ebiten.Image

        search  string
        cat     int

        ctxOpen bool
        ctxSel  int
        ctxEnts []ctxEntry

        volOpen bool
        volTTL  float64

        hits       []hitTarget
        hovTooltip string
        lastMX, lastMY float64

        bat    batteryInfo
        batTTL float64

        
        
        
        bgCache    *ebiten.Image
        tbCache    *ebiten.Image
        panelCache [zoneCount]*ebiten.Image
        // panelOld keeps the previous generation of a panel image so it can
        // be released one rebuild later (it may still be referenced by a
        // frame that is being presented).
        panelOld   [zoneCount]*ebiten.Image
        frameCache *ebiten.Image
        frameZone  int
        frameKey   string
        caching    bool
        cacheKey   [zoneCount]string
        tbKey      string
}

func newDashboard() *dashboard {
        d := &dashboard{zone: zoneMy, closing: -1, anim: 1, closeAnim: 0, bat: readBattery(), hovCat: -1}
        d.tbIndFrom = 112 + 26
        d.tbIndTo = 112 + 26
        d.tbIndP = 1
        d.tbIndA = 1
        d.rebuild()
        return d
}

func (d *dashboard) rebuild() {
        meta := supervisor.M().GameMeta()
        if meta.Found {
                d.gameTitle = meta.Title
                d.gameSub = meta.Version
                d.gameFiles = meta.Files
                d.gameFound = true
        } else {
                d.gameTitle = "no game package"
                d.gameSub = ""
                d.gameFound = false
        }
}

func (d *dashboard) enter() {
        d.rebuild()
        d.iconOnce = sync.Once{}
}

func (d *dashboard) loadIcon() {
        d.iconOnce.Do(func() {
                meta := supervisor.M().GameMeta()
                if !meta.Found || meta.Icon == "" {
                        return
                }
                f, err := os.Open(meta.Icon)
                if err != nil {
                        return
                }
                defer f.Close()
                img, _, err := image.Decode(f)
                if err != nil {
                        return
                }
                d.iconTile = roundedCrop(ebiten.NewImageFromImage(img), 200, 24)
        })
}





func (d *dashboard) update(dt float64, a *app) {
        d.layout()
        d.updateFrame(dt, a)
        if d.ctxOpen {
                d.updateCtx(a)
                return
        }
        if d.volOpen {
                d.updateVol(a)
        }
        d.keyNav(a)
        d.pointer(a)
}

func (d *dashboard) updateFrame(dt float64, a *app) {
        d.t += dt
        if d.anim < 1 {
                d.anim = math.Min(1, d.anim+dt/shellAnimT)
        }
        if d.closing >= 0 {
                d.closeAnim -= dt / shellAnimT
                if d.closeAnim <= 0 {
                        d.closing = -1
                }
        }
        
        for len(d.selA) < len(d.hits) {
                d.selA = append(d.selA, 0)
        }
        
        for i := range d.selA {
                target := 0.0
                if d.zone >= 0 && i == d.sel[d.zone] && i < len(d.hits) {
                        target = 1
                }
                d.selA[i] = approach(d.selA[i], target, 11, dt)
        }
        if d.selPulse > 0 {
                d.selPulse -= dt
        }
        for z := 0; z < zoneCount; z++ {
                if d.sel[z] != d.lastSel[z] {
                        d.selPulse = 0.26
                        d.lastSel[z] = d.sel[z]
                }
        }
        if d.pressT > 0 {
                d.pressT -= dt
        }
        
        
        indZone := d.zone
        if indZone < 0 {
                indZone = d.closing
                if indZone < 0 {
                        indZone = 0 
                }
        }
        indTarget := 112 + 26 + float64(indZone)*58
        if indTarget != d.tbIndTo {
                d.tbIndFrom = d.tbIndX()
                d.tbIndTo = indTarget
                d.tbIndP = 0
        }
        d.tbIndP = ease01(d.tbIndP, dt, tbIndAnimT)
        indA := 0.0
        if d.zone >= 0 || d.closing >= 0 {
                indA = 1
        }
        d.tbIndA = damp(d.tbIndA, indA, 14, dt)
        
        if d.zone >= 0 && d.anim >= 1 && d.closing < 0 {
                d.footerT = ease01(d.footerT, dt, 0.34)
        }
        
        vt := 0.0
        if d.volOpen {
                vt = 1
        }
        d.volAnim = damp(d.volAnim, vt, 12, dt)
        
        ct := 0.0
        if d.ctxOpen {
                ct = 1
        }
        d.ctxAnim = damp(d.ctxAnim, ct, 16, dt)
        
        if d.hovTooltip != "" && d.hovTooltip == d.tipShown {
                d.tipT += dt
        } else {
                d.tipShown = d.hovTooltip
                d.tipT = 0
        }
        tt := 0.0
        if d.hovTooltip != "" && d.tipT >= tipDelay && !d.volOpen && !d.ctxOpen {
                tt = 1
        }
        d.tipFade = damp(d.tipFade, tt, 15, dt)

        d.batTTL -= dt
        if d.batTTL <= 0 {
                d.bat = readBattery()
                d.batTTL = 30
        }
        if d.volOpen {
                d.volTTL -= dt
                if d.volTTL <= 0 {
                        d.volOpen = false
                }
        }
}




func (d *dashboard) interactive() bool {
        return d.zone >= 0 && d.anim >= 0.999 && d.closing < 0
}


func (d *dashboard) tbIndX() float64 {
        return lerpF(d.tbIndFrom, d.tbIndTo, easeOutCubic(d.tbIndP))
}

func (d *dashboard) layout() {
        d.hits = d.hits[:0]
        if d.zone < 0 {
                return
        }
        switch d.zone {
        case zoneMy:
                
                d.hits = append(d.hits, hitTarget{x: mzCardX, y: mzCardY, w: mzCardW, h: mzCardH, hand: true,
                        run: func(a *app) { d.launchOrWarn(a) }})
                links := d.quickLinks()
                for i := range links {
                        d.hits = append(d.hits, hitTarget{x: mzLinkX, y: mzLinkY0 + float64(i)*56, w: mzLinkW, h: mzLinkH, hand: true, run: links[i].run})
                }
                
                d.hits = append(d.hits, hitTarget{x: mzCardX + mzCardW - 48, y: mzCardY + 10, w: 36, h: 28, hand: true,
                        run: func(a *app) { d.openCtx(a) }})
        case zoneApps:
                d.hits = append(d.hits, hitTarget{x: LogicalW - 300, y: apSearchY, w: 260, h: 32, hand: false,
                        run: func(a *app) { d.sel[zoneApps] = 0 }})
                apps := d.filteredApps()
                for i := range apps {
                        col := i % 2
                        row := i / 2
                        d.hits = append(d.hits, hitTarget{x: 40 + float64(col)*600, y: apRowY0 + float64(row)*apRowH, w: apRowW, h: 76, hand: true, run: apps[i].run})
                }
        case zoneZones:
                d.hits = append(d.hits, hitTarget{x: znColX + 12, y: znCardY, w: znColW - 24, h: znCardH, hand: true,
                        run: func(a *app) {
                                if d.gameRunning() {
                                        a.gotoState(stRunning)
                                        hideWindow()
                                } else {
                                        d.launchOrWarn(a)
                                }
                        }})
                if d.gameRunning() {
                        d.hits = append(d.hits, hitTarget{x: znColX + 12, y: znColY + znColH + 12, w: 172, h: 40, hand: true,
                                run: func(a *app) {
                                        go supervisor.M().KillGame()
                                        a.showToast("application closed")
                                }})
                }
                d.hits = append(d.hits, hitTarget{x: znColX + 416, y: znCardY, w: znColW - 24, h: znCardH, hand: true,
                        run: func(a *app) {
                                next := 0
                                if supervisor.M().Settings().Workspace == 0 {
                                        next = 1
                                }
                                supervisor.M().UpdateSettings(func(x *supervisor.Settings) { x.Workspace = next })
                                a.showToast("switched to workspace" + fmt.Sprint(next))
                        }})
                d.hits = append(d.hits, hitTarget{x: znColX + 832, y: znCardY, w: 352 - 24, h: znCardH, hand: true,
                        run: func(a *app) { a.showToast("only two zones are available on this system") }})
        }
}

func (d *dashboard) gameRunning() bool {
        st := supervisor.M().State()
        return st == supervisor.StateRunning || st == supervisor.StateLaunching
}

func (d *dashboard) keyNav(a *app) {
        
        
        if d.zone == zoneApps {
                d.typeSearch()
        }
        if inpututil.IsKeyJustPressed(ebiten.KeyTab) {
                dbgPrintf("keyNav: TAB pressed (zone=%d)", d.zone)
                next := zoneMy
                if d.zone >= 0 {
                        next = (d.zone + 1) % zoneCount
                }
                d.switchZone(next, a)
                return
        }
        if inpututil.IsKeyJustPressed(ebiten.KeyEscape) {
                dbgPrintf("keyNav: ESC pressed (zone=%d)", d.zone)
                if d.volOpen {
                        d.volOpen = false
                        playSound(sndBack)
                        return
                }
                
                if d.zone == zoneApps && d.search != "" {
                        d.search = ""
                        d.sel[zoneApps] = 0
                        playSound(sndBack)
                        return
                }
                if d.zone >= 0 {
                        playSound(sndBack)
                        d.closing = d.zone
                        d.closeAnim = d.anim
                        d.zone = -1
                }
                return
        }
        if d.zone < 0 {
                return
        }
        
        
        if d.zone == zoneApps && d.search == "" && d.sel[zoneApps] == 0 {
                if inpututil.IsKeyJustPressed(ebiten.KeyRight) {
                        d.cat = (d.cat + 1) % len(appCats)
                        playSound(sndMove)
                        return
                }
                if inpututil.IsKeyJustPressed(ebiten.KeyLeft) {
                        d.cat = (d.cat - 1 + len(appCats)) % len(appCats)
                        playSound(sndMove)
                        return
                }
        }
        if inpututil.IsKeyJustPressed(ebiten.KeyRight) && d.zone < zoneZones {
                d.switchZone(d.zone+1, a)
                return
        }
        if inpututil.IsKeyJustPressed(ebiten.KeyLeft) && d.zone > zoneMy {
                d.switchZone(d.zone-1, a)
                return
        }
        
        if d.volOpen {
                return
        }
        n := len(d.hits)
        if n == 0 {
                return
        }
        if d.sel[d.zone] >= n {
                d.sel[d.zone] = n - 1
        }
        if inpututil.IsKeyJustPressed(ebiten.KeyDown) {
                d.sel[d.zone] = (d.sel[d.zone] + 1) % n
                dbgPrintf("keyNav: DOWN -> sel=%d", d.sel[d.zone])
                playSound(sndMove)
        }
        if inpututil.IsKeyJustPressed(ebiten.KeyUp) {
                d.sel[d.zone] = (d.sel[d.zone] - 1 + n) % n
                playSound(sndMove)
        }
        if inpututil.IsKeyJustPressed(ebiten.KeyEnter) || inpututil.IsKeyJustPressed(ebiten.KeySpace) {
                dbgPrintf("keyNav: ENTER zone=%d sel=%d hits=%d", d.zone, d.sel[d.zone], len(d.hits))
                
                if d.zone == zoneApps && d.sel[zoneApps] == 0 {
                        if apps := d.filteredApps(); len(apps) > 0 {
                                playSound(sndSelect)
                                d.pressT = 0.16
                                d.sel[zoneApps] = 1
                                apps[0].run(a)
                                return
                        }
                        return
                }
                if h := d.hits[d.sel[d.zone]]; h.run != nil {
                        playSound(sndSelect)
                        d.pressT = 0.16
                        h.run(a)
                }
        }
}

func (d *dashboard) typeSearch() {
        typed := false
        for _, r := range ebiten.InputChars() {
                if r >= 32 && r < 127 && len(d.search) < 24 {
                        d.search += string(r)
                        typed = true
                }
        }
        if typed {
                d.sel[zoneApps] = 0
                playSound(sndType)
        }
        if inpututil.IsKeyJustPressed(ebiten.KeyBackspace) && len(d.search) > 0 {
                d.search = d.search[:len(d.search)-1]
                d.sel[zoneApps] = 0
        }
}

func (d *dashboard) switchZone(z int, a *app) {
        dbgPrintf("switchZone: cur=%d -> %d (closing=%d)", d.zone, z, d.closing)
        if z == d.zone {
                return
        }
        playSound(sndMove)
        if d.zone >= 0 {
                d.closing = d.zone
                d.closeAnim = d.anim
        }
        d.zone = z
        d.anim = 0
        d.footerT = 0
        d.selA = d.selA[:0]
}


func (d *dashboard) pickHit(px, py float64) (int, *hitTarget) {
        for i := len(d.hits) - 1; i >= 0; i-- {
                h := &d.hits[i]
                if inRect(px, py, h.x, h.y, h.w, h.h) {
                        return i, h
                }
        }
        return -1, nil
}


func (d *dashboard) tooltipForHit(i int) string {
        if i < 0 || i >= len(d.hits) {
                return ""
        }
        switch d.zone {
        case zoneMy:
                links := d.quickLinks()
                if i == 0 {
                        return "open " + d.gameTitle
                }
                if i == len(d.hits)-1 {
                        return "more actions"
                }
                if i-1 < len(links) {
                        return links[i-1].label + " — " + links[i-1].sub
                }
        case zoneApps:
                if i == 0 {
                        return "type to search"
                }
                if apps := d.filteredApps(); i-1 < len(apps) {
                        return apps[i-1].name + " — " + apps[i-1].desc
                }
        case zoneZones:
                switch {
                case i == 0:
                        return "zone 1 — " + d.gameTitle
                case i == 1 && d.gameRunning():
                        return "close the running application"
                case i == 1 && !d.gameRunning():
                        return "activate workspace 2"
                default:
                        return "add zone"
                }
        }
        return ""
}

func (d *dashboard) pointer(a *app) {
        mx, my := ebiten.CursorPosition()
        px, py := float64(mx), float64(my)
        clicked := false
        rightClick := false
        if inpututil.IsMouseButtonJustPressed(ebiten.MouseButtonLeft) {
                clicked = true
        }
        if inpututil.IsMouseButtonJustPressed(ebiten.MouseButtonRight) {
                rightClick = true
        }
        if tx, ty, ok := tapPoint(); ok {
                px, py = tx, ty
                clicked = true
                dbgPrintf("pointer: TOUCH at %v,%v", px, py)
        }
        if inpututil.IsMouseButtonJustPressed(ebiten.MouseButtonLeft) {
                dbgPrintf("pointer: LEFT CLICK at %v,%v", px, py)
        }
        if inpututil.IsMouseButtonJustPressed(ebiten.MouseButtonRight) {
                dbgPrintf("pointer: RIGHT CLICK at %v,%v", px, py)
        }
        
        
        
        
        d.hovTooltip = ""
        d.hovCat = -1
        mouseMoved := px != d.lastMX || py != d.lastMY
        d.lastMX, d.lastMY = px, py
        if d.interactive() {
                if i, h := d.pickHit(px, py); h != nil {
                        if h.hand {
                                uiCursor = curHand
                        }
                        d.hovTooltip = d.tooltipForHit(i)
                        if mouseMoved && d.sel[d.zone] != i {
                                d.sel[d.zone] = i
                        }
                } else if d.zone == zoneApps && py >= apTileY-10 && py < apTileY+70 {
                        for i := range appCats {
                                tx := 44 + float64(i)*84 - 8
                                if inRect(px, py, tx, apTileY-10, 68, 80) {
                                        d.hovCat = i
                                        d.hovTooltip = "category: " + appCats[i]
                                        uiCursor = curHand
                                }
                        }
                }
        }
        d.toolbarHover(px, py)
        if !clicked && !rightClick {
                return
        }
        if py < toolbarH+2 {
                d.toolbarClick(px, py, a)
                return
        }
        
        if d.volOpen && px > LogicalW-260 && py < toolbarH+120 {
                if bx, by, bw, bh := d.volTrackRect(); inRect(px, py, bx-8, by-10, bw+16, bh+20) {
                        d.setVolume(clamp01((px - bx) / bw))
                        d.volDrag = true
                        return
                }
                d.volTTL = 3.5
                return
        }
        if d.zone < 0 {
                
                if clicked && d.closing < 0 {
                        d.switchZone(zoneMy, a)
                }
                return
        }
        if !d.interactive() {
                return 
        }
        if i, h := d.pickHit(px, py); h != nil {
                d.sel[d.zone] = i
                if rightClick && d.zone == zoneMy && i == 0 {
                        d.openCtx(a)
                        return
                }
                if h.run != nil {
                        playSound(sndSelect)
                        d.pressT = 0.16
                        h.run(a)
                }
                return
        }
        
        if d.zone == zoneApps && clicked {
                for i := range appCats {
                        tx := 44 + float64(i)*84 - 8
                        if inRect(px, py, tx, apTileY-8, 68, 76) {
                                d.cat = i
                                d.sel[zoneApps] = 0
                                playSound(sndMove)
                                return
                        }
                }
        }
        
        if clicked {
                playSound(sndBack)
                d.closing = d.zone
                d.closeAnim = d.anim
                d.zone = -1
        }
}


func toolbarVolX() float64  { return LogicalW - 234 }
func toolbarNetX() float64  { return LogicalW - 210 }
func toolbarBtX() float64   { return LogicalW - 188 }


func (d *dashboard) toolbarHover(px, py float64) {
        if py > toolbarH {
                return
        }
        if px >= 112 && px < 112+58*3 {
                uiCursor = curHand
                z := int((px - 112 + 6) / 58)
                if z < 0 {
                        z = 0
                }
                if z > 2 {
                        z = 2
                }
                d.hovTooltip = []string{"myzone", "applications", "zones"}[z]
                return
        }
        if px >= 306 && px < 306+34*float64(len(d.quickApps())) {
                uiCursor = curHand
                idx := int((px - 306) / 34)
                apps := d.quickApps()
                if idx >= 0 && idx < len(apps) {
                        d.hovTooltip = "quick launch: " + apps[idx].name
                }
                return
        }
        if px >= toolbarBtX()-4 && px < toolbarBtX()+22 {
                d.hovTooltip = "bluetooth: off"
                return
        }
        if px >= toolbarNetX()-4 && px < toolbarNetX()+28 {
                d.hovTooltip = "network: wired"
                return
        }
        if px >= toolbarVolX()-4 && px < toolbarVolX()+28 {
                uiCursor = curHand
                d.hovTooltip = fmt.Sprintf("volume: %d%% — click to open", int(supervisor.M().Settings().Volume*100))
                return
        }
        if px >= LogicalW-42 && px <= LogicalW-4 {
                if d.bat.Present {
                        d.hovTooltip = fmt.Sprintf("battery: %d%%", d.bat.Pct)
                        if d.bat.Chg {
                                d.hovTooltip += " (charging)"
                        }
                } else {
                        d.hovTooltip = "battery: not present"
                }
                return
        }
}

func (d *dashboard) toolbarClick(px, py float64, a *app) {
        if px < 100 {
                d.switchZone(zoneMy, a)
                return
        }
        if px >= 112 && px < 112+58*3 {
                z := int((px - 112 + 6) / 58)
                if z < 0 {
                        z = 0
                }
                if z > 2 {
                        z = 2
                }
                if z == d.zone {
                        playSound(sndBack)
                        d.closing = d.zone
                        d.closeAnim = d.anim
                        d.zone = -1
                        return
                }
                d.switchZone(z, a)
                return
        }
        if px >= toolbarVolX() {
                if px >= toolbarVolX() && px < toolbarVolX()+24 {
                        d.volOpen = !d.volOpen
                        d.volTTL = 3.5
                        playSound(sndToggle)
                        return
                }
                return
        }
        if px >= 306 {
                idx := int((px - 306) / 34)
                apps := d.quickApps()
                if idx >= 0 && idx < len(apps) {
                        playSound(sndSelect)
                        apps[idx].run(a)
                }
        }
}

func (d *dashboard) quickApps() []shellApp {
        apps := []shellApp{
                {name: "game", icon: "", run: func(a *app) { d.launchOrWarn(a) }},
                {name: "settings", icon: "apps/settings.png", run: func(a *app) { a.gotoState(stSettings) }},
                {name: "dev console", icon: "apps/terminal.png", run: func(a *app) { a.gotoState(stConsole) }},
        }
        if currentFeatures().DebugMenu {
                apps = append(apps, shellApp{name: "debug", icon: "apps/control.png", run: func(a *app) { a.gotoState(stDebug) }})
        }
        return apps
}

func (d *dashboard) launchOrWarn(a *app) {
        if !d.gameFound {
                a.showToast("no game package installed")
                return
        }
        a.launchGame()
}





func (d *dashboard) updateVol(a *app) {
        
        if d.volDrag {
                if !ebiten.IsMouseButtonPressed(ebiten.MouseButtonLeft) {
                        d.volDrag = false
                } else {
                        mx, _ := ebiten.CursorPosition()
                        bx, _, bw, _ := d.volTrackRect()
                        d.setVolumeQuiet(clamp01((float64(mx) - bx) / bw))
                        return
                }
        }
        _, dy := ebiten.Wheel()
        if inpututil.IsKeyJustPressed(ebiten.KeyUp) || dy > 0 {
                d.setVolume(supervisor.M().Settings().Volume + 0.1)
        }
        if inpututil.IsKeyJustPressed(ebiten.KeyDown) || dy < 0 {
                d.setVolume(supervisor.M().Settings().Volume - 0.1)
        }
}


func (d *dashboard) volTrackRect() (float64, float64, float64, float64) {
        x := float64(LogicalW) - 244
        y := toolbarH + 8
        return x + 16, y + 48, 220 - 32, 8
}

func (d *dashboard) setVolume(v float64) {
        d.setVolumeQuiet(v)
        playSound(sndToggle)
}

func (d *dashboard) setVolumeQuiet(v float64) {
        if v < 0 {
                v = 0
        }
        if v > 1 {
                v = 1
        }
        supervisor.M().UpdateSettings(func(x *supervisor.Settings) { x.Volume = v })
        SetSoundVolume(v)
        d.volTTL = 3.5
}





func (d *dashboard) openCtx(a *app) {
        d.volOpen = false 
        entries := []ctxEntry{
                {label: "start", icon: iconPlayTriangle, run: func(a *app) { d.launchOrWarn(a) }},
        }
        m := supervisor.M()
        if inc := m.ScanIncoming(); len(inc) > 0 {
                entries = append(entries, ctxEntry{label: fmt.Sprintf("install incoming (%d)", len(inc)), icon: iconDownload, run: func(a *app) {
                        okN := 0
                        var lastErr string
                        for _, f := range m.ScanIncoming() {
                                if err := m.InstallPackage(f); err != nil {
                                        lastErr = err.Error()
                                        continue
                                }
                                okN++
                        }
                        if okN > 0 {
                                a.showToast(fmt.Sprintf("installed %d package(s)", okN))
                                d.rebuild()
                        } else {
                                a.showWarn("install failed: " + lastErr)
                        }
                }})
        }
        if d.gameRunning() {
                entries = append(entries, ctxEntry{label: "close application", icon: iconCloseApp, run: func(a *app) {
                        go m.KillGame()
                        a.showToast("application closed")
                }})
        }
        if meta := m.GameMeta(); meta.Found {
                entries = append(entries, ctxEntry{label: "properties", icon: iconInfo, run: func(a *app) {
                        a.showToast(fmt.Sprintf("%s v%s · %d files", meta.Title, meta.Version, meta.Files))
                }})
        }
        if currentFeatures().DebugMenu {
                entries = append(entries, ctxEntry{label: "debug settings", icon: iconStar, run: func(a *app) {
                        a.gotoState(stDebug)
                }})
        }
        d.ctxEnts = entries
        d.ctxSel = 0
        d.ctxOpen = true
        playSound(sndSelect)
}

func (d *dashboard) ctxRect() (float64, float64, float64) {
        x := mzCardX + mzCardW - 40
        y := mzCardY + 44
        w := 296.0
        if x+w > LogicalW-16 {
                x = LogicalW - 16 - w
        }
        return x, y, w
}

func (d *dashboard) updateCtx(a *app) {
        if inpututil.IsKeyJustPressed(ebiten.KeyDown) {
                if d.ctxSel < len(d.ctxEnts)-1 {
                        d.ctxSel++
                        playSound(sndMove)
                }
        }
        if inpututil.IsKeyJustPressed(ebiten.KeyUp) {
                if d.ctxSel > 0 {
                        d.ctxSel--
                        playSound(sndMove)
                }
        }
        if inpututil.IsKeyJustPressed(ebiten.KeyEnter) || inpututil.IsKeyJustPressed(ebiten.KeySpace) {
                if e := d.ctxEnts[d.ctxSel]; e.run != nil {
                        d.ctxOpen = false
                        playSound(sndSelect)
                        e.run(a)
                }
        }
        if inpututil.IsKeyJustPressed(ebiten.KeyEscape) {
                d.ctxOpen = false
                playSound(sndBack)
        }
        
        px, py, ok := tapOrClick()
        x, y, w := d.ctxRect()
        if !ok {
                mv := hoverPointerMoved()
                mx, my := ebiten.CursorPosition()
                for i := range d.ctxEnts {
                        ry := y + 8 + float64(i)*48
                        if inRect(float64(mx), float64(my), x+4, ry, w-8, 44) {
                                uiCursor = curHand
                                if mv && d.ctxSel != i {
                                        d.ctxSel = i
                                }
                        }
                }
                return
        }
        for i := range d.ctxEnts {
                ry := y + 8 + float64(i)*48
                if inRect(px, py, x+4, ry, w-8, 44) {
                        if d.ctxSel == i {
                                if e := d.ctxEnts[i]; e.run != nil {
                                        d.ctxOpen = false
                                        playSound(sndSelect)
                                        e.run(a)
                                }
                        } else {
                                d.ctxSel = i
                                playSound(sndMove)
                        }
                        return
                }
        }
        if !inRect(px, py, x, y, w, float64(len(d.ctxEnts))*48+16) {
                d.ctxOpen = false
                playSound(sndBack)
        }
}

func (d *dashboard) quickLinks() []meegoLink {
        links := []meegoLink{
                {label: "settings", icon: "apps/settings.png", sub: "system configuration", run: func(a *app) { a.gotoState(stSettings) }},
                {label: "dev console", icon: "apps/terminal.png", sub: "nugget shell (nsh)", run: func(a *app) { a.gotoState(stConsole) }},
                {label: "applications", icon: "apps/myzone.png", sub: "browse installed software", run: func(a *app) { d.switchZone(zoneApps, a) }},
                {label: "zones", icon: "apps/tasks.png", sub: "running applications", run: func(a *app) { d.switchZone(zoneZones, a) }},
        }
        if currentFeatures().DebugMenu {
                links = append(links, meegoLink{label: "debug settings", icon: "apps/control.png", sub: "engine devkit", run: func(a *app) { a.gotoState(stDebug) }})
        }
        return links
}

func (d *dashboard) appEntries() []shellApp {
        apps := []shellApp{
                {name: d.gameTitle, desc: "version " + d.gameSub, cat: "games", icon: "", run: func(a *app) { d.launchOrWarn(a) }},
                {name: "settings", desc: "system configuration", cat: "preferences", icon: "apps/settings.png", run: func(a *app) { a.gotoState(stSettings) }},
                {name: "dev console", desc: "nugget shell (nsh)", cat: "system", icon: "apps/terminal.png", run: func(a *app) { a.gotoState(stConsole) }},
        }
        if currentFeatures().DevComponents {
                apps = append(apps, shellApp{name: "api demos", desc: "bridge api playground", cat: "system", icon: "apps/apidemos.png", run: func(a *app) { a.launchAPIDemos() }})
        }
        if currentFeatures().DebugMenu {
                apps = append(apps, shellApp{name: "debug settings", desc: "engine devkit", cat: "system", icon: "apps/control.png", run: func(a *app) { a.gotoState(stDebug) }})
        }
        return apps
}

func (d *dashboard) filteredApps() []shellApp {
        var out []shellApp
        q := strings.ToLower(d.search)
        for _, app := range d.appEntries() {
                if d.cat > 0 && appCats[d.cat] != app.cat {
                        continue
                }
                if q != "" && !strings.Contains(strings.ToLower(app.name), q) && !strings.Contains(strings.ToLower(app.desc), q) {
                        continue
                }
                out = append(out, app)
        }
        return out
}

func (d *dashboard) catCount(cat string) int {
        n := 0
        for _, app := range d.appEntries() {
                if cat == "all" || app.cat == cat {
                        n++
                }
        }
        return n
}

func (d *dashboard) systemRows() [][2]string {
        m := supervisor.M()
        st := m.Settings()
        hr := "off"
        if m.HotReloadRunning() {
                hr = "on"
        }
        return [][2]string{
                {"engine", buildinfo.Version},
                {"build type", buildinfo.Type()},
                {"workspace", fmt.Sprintf("workspace%d", st.Workspace)},
                {"hot reload", hr},
                {"sound", fmt.Sprintf("%d%%", int(st.Volume*100))},
        }
}





func (d *dashboard) draw(screen *ebiten.Image, a *app) {
        d.ensureCaches(a)
        settled := d.zone >= 0 && d.anim >= 1 && d.closing < 0
        if settled {
                
                
                for z := 0; z < zoneCount; z++ {
                        if d.panelCache[z] == nil {
                                d.panelImg(z, a)
                                break
                        }
                }
                
                img := d.frameImg(a)
                if img != nil {
                        // The frame cache is opaque: blit it without alpha
                        // blending (a full screen read-modify-write per
                        // frame is expensive on a software renderer).
                        drawImageOpaque(screen, img)
                        d.drawSelOverlays(screen, 1, 0)
                        d.drawSearchOverlay(screen, 0)
                        d.drawClockOverlay(screen, 0)
                        d.drawPanelFooterDyn(screen, 0)
                        d.drawToolbarDynamic(screen)
                        if d.volOpen || d.volAnim > 0.01 {
                                d.drawVolOSD(screen)
                        }
                        if d.ctxOpen || d.ctxAnim > 0.01 {
                                d.drawCtx(screen)
                        }
                        if d.tipFade > 0.01 && d.hovTooltip != "" {
                                d.drawTooltip(screen)
                        }
                        return
                }
        }
        
        if d.bgCache != nil {
                screen.DrawImage(d.bgCache, &ebiten.DrawImageOptions{})
        } else {
                drawWallpaper(screen)
        }
        vis := 0.0
        if d.zone >= 0 {
                vis = easeOutCubic(d.anim)
        }
        if d.closing >= 0 {
                if v := clamp01(d.closeAnim); v > vis {
                        vis = v
                }
        }
        if vis > 0.003 {
                drawWallDim(screen, 0.32*vis)
        }

        
        if d.closing >= 0 {
                p := clamp01(d.closeAnim)
                if img := d.panelImg(d.closing, a); img != nil {
                        op := &ebiten.DrawImageOptions{}
                        op.GeoM.Translate(0, -easeInCubic(1-p)*shellPanelH)
                        screen.DrawImage(img, op)
                }
        }
        if d.zone >= 0 {
                off := (1 - easeOutCubic(d.anim)) * shellPanelH
                if img := d.panelImg(d.zone, a); img != nil {
                        op := &ebiten.DrawImageOptions{}
                        op.GeoM.Translate(0, -off)
                        screen.DrawImage(img, op)
                }
                
                
                d.drawSelOverlays(screen, easeOutCubic(d.anim), -off)
        }
        d.drawToolbar(screen, a)
        if d.volOpen || d.volAnim > 0.01 {
                d.drawVolOSD(screen)
        }
        if d.ctxOpen || d.ctxAnim > 0.01 {
                d.drawCtx(screen)
        }
        if d.tipFade > 0.01 && d.hovTooltip != "" {
                d.drawTooltip(screen)
        }
}


func (d *dashboard) selAof(i int) float64 {
        if d.caching || i < 0 || i >= len(d.selA) {
                return 0
        }
        return d.selA[i]
}






func (d *dashboard) invalidate() {
        for i := range d.panelCache {
                d.panelCache[i] = nil
                d.cacheKey[i] = ""
                if d.panelOld[i] != nil {
                        d.panelOld[i].Deallocate()
                        d.panelOld[i] = nil
                }
        }
        d.tbKey = ""
}


func (d *dashboard) panelKey(z int) string {
        m := supervisor.M()
        st := m.Settings()
        running := 0
        if d.gameRunning() {
                running = 1
        }
        icon := "0"
        if d.iconTile != nil {
                icon = "1"
        }
        
        
        
        
        
        // NOTE: the clock is deliberately NOT part of the key. It has minute
        // resolution, so including it rebuilt the whole panel (and its
        // 3.5 MB cache image) every minute. The clock and the date are
        // drawn as a dynamic overlay instead (drawClockOverlay).
        return fmt.Sprintf("z%d|v%.2f|ws%d|hr%d|run%d|ic%s|c%d|b%d|%d|s%s", z, st.Volume, st.Workspace,
                b2i(m.HotReloadRunning()), running, icon, d.cat, d.bat.Pct, d.gameFiles, d.search)
}

func b2i(b bool) int {
        if b {
                return 1
        }
        return 0
}



func (d *dashboard) panelImg(z int, a *app) *ebiten.Image {
        key := d.panelKey(z)
        if d.panelCache[z] != nil && d.cacheKey[z] == key {
                return d.panelCache[z]
        }
        const px = 16.0
        img := ebiten.NewImage(LogicalW, int(shellPanelH)+40)
        d.caching = true
        d.drawZonePanel(img, z, 0, a, 1)
        d.caching = false
        // The old cache image is ~3.5 MB of VRAM. It is kept for one more
        // generation (it may still be referenced by a frame that is being
        // presented) and released after that.
        if old := d.panelCache[z]; old != nil {
                d.releasePanel(z, old)
        }
        d.panelCache[z] = img
        d.cacheKey[z] = key
        return img
}

// releasePanel keeps the previous panel image around for one generation and
// releases the one before it, so the GPU memory of the panel cache does not
// grow with every rebuild.
func (d *dashboard) releasePanel(z int, old *ebiten.Image) {
        if prev := d.panelOld[z]; prev != nil {
                prev.Deallocate()
        }
        d.panelOld[z] = old
}


func (d *dashboard) ensureCaches(a *app) {
        if d.bgCache == nil {
                img := ebiten.NewImage(LogicalW, LogicalH)
                drawWallpaper(img)
                d.bgCache = img
        }
        d.loadIcon()
}



func (d *dashboard) frameImg(a *app) *ebiten.Image {
        tbKey := fmt.Sprintf("tb|v%.2f|%d|ic%d", supervisor.M().Settings().Volume, len(d.quickApps()), b2i(d.iconTile != nil))
        key := d.panelKey(d.zone) + "|" + tbKey
        if d.frameCache != nil && d.frameKey == key {
                return d.frameCache
        }
        img := ebiten.NewImage(LogicalW, LogicalH)
        if d.bgCache != nil {
                img.DrawImage(d.bgCache, &ebiten.DrawImageOptions{})
        }
        drawWallDim(img, 0.32)
        if p := d.panelImg(d.zone, a); p != nil {
                img.DrawImage(p, &ebiten.DrawImageOptions{})
        }

        if d.tbCache == nil || d.tbKey != tbKey {
                tb := ebiten.NewImage(LogicalW, int(toolbarH)+1)
                d.caching = true
                d.drawToolbarChrome(tb, a)
                d.caching = false
                // The old toolbar image is released: it is about 180 kB of
                // video memory and the cache is rebuilt on every change of
                // the volume, the workspace or the quick apps.
                if d.tbCache != nil {
                        d.tbCache.Deallocate()
                }
                d.tbCache = tb
                d.tbKey = tbKey
        }
        img.DrawImage(d.tbCache, &ebiten.DrawImageOptions{})
        // The frame image is 3.7 MB of video memory. Without releasing the
        // previous one every rebuild leaked that amount - the dashboard was
        // rebuilt whenever the volume, the workspace, the search text or the
        // battery level changed.
        if d.frameCache != nil {
                d.frameCache.Deallocate()
        }
        d.frameCache = img
        d.frameKey = key
        return img
}





func (d *dashboard) drawSelOverlays(screen *ebiten.Image, vis float64, offY float64) {
        if vis < 0.65 || d.zone < 0 || len(d.hits) == 0 {
                return
        }
        al := clamp01((vis - 0.65) / 0.35)
        sel := d.sel[d.zone]
        if sel >= len(d.hits) {
                return
        }
        for i, h := range d.hits {
                if i != sel {
                        continue
                }
                y := h.y + offY
                plate := withAlphaF(MgTileHover, 0.70*al)
                border := withAlphaF(MgTileHoverBd, 0.95*al)
                switch d.zone {
                case zoneMy:
                        if i == 0 {
                                
                                glow := 0.0
                                if d.selPulse > 0 {
                                        glow = d.selPulse / 0.26
                                }
                                strokeRoundRect(screen, h.x-0.5, y-0.5, h.w+1, h.h+1, 8.5, 2, withAlphaF(MgTileHoverBd, (0.75+0.25*glow)*al))
                                continue
                        }
                        if i == len(d.hits)-1 {
                                
                                strokeRoundRect(screen, h.x-0.5, y-0.5, h.w+1, h.h+1, 6.5, 1.5, border)
                                continue
                        }
                        fillRoundRectGradV(screen, h.x, y, h.w, h.h, 8, withAlphaF(MgTileHoverTop, 0.74*al), plate)
                        strokeRoundRect(screen, h.x+0.5, y+0.5, h.w-1, h.h-1, 7.5, 1, border)
                        fillRoundRect(screen, h.x-3, y+h.h/2-9, 3, 18, 1.5, withAlphaF(MgCyan, al))
                        
                        if links := d.quickLinks(); i-1 < len(links) {
                                ln := links[i-1]
                                drawText(screen, clampText(ln.label, medium(14), h.w-58-70), h.x+58, y+7, medium(14), withAlphaF(MgText, al))
                                drawText(screen, ln.sub, h.x+58, y+29, regular(11), withAlphaF(MgTextDim, al))
                                iconChevron(screen, h.x+h.w-18, y+h.h/2, 5, withAlphaF(MgTextFaint, 0.55+0.45*al))
                        }
                case zoneApps:
                        acc := MgAppHover
                        accBd := MgAppHoverBd
                        if i == 0 {
                                
                                pulse := 0.55 + 0.45*math.Sin(d.t*4)
                                strokeRoundRect(screen, h.x-1.5, y-1.5, h.w+3, h.h+3, 17.5, 2, withAlphaF(MgCyan, (0.45+0.55*pulse)*al))
                                continue
                        }
                        fillRoundRectGradV(screen, h.x, y, h.w, h.h, 10, withAlphaF(MgAppHoverTop, 0.78*al), withAlphaF(acc, 0.70*al))
                        strokeRoundRect(screen, h.x+0.5, y+0.5, h.w-1, h.h-1, 9.5, 1.5, withAlphaF(accBd, al))
                        fillRoundRect(screen, h.x-3, y+h.h/2-9, 3, 18, 1.5, withAlphaF(catAccent["games"], al))
                        
                        if apps := d.filteredApps(); i-1 < len(apps) {
                                app := apps[i-1]
                                acc2 := catAccent[app.cat]
                                drawText(screen, clampText(app.name, medium(15), h.w-96-chipWidthOf(app.cat)-40), h.x+80, y+15, medium(15), withAlphaF(MgText, al))
                                drawText(screen, clampText(app.desc, regular(11), h.w-96-40), h.x+80, y+42, regular(11), withAlphaF(MgTextDim, al))
                                drawChip(screen, h.x+h.w-16-chipWidthOf(app.cat), y+14, app.cat, withAlphaF(acc2, al), false)
                                iconChevron(screen, h.x+h.w-22, y+h.h/2, 5, withAlphaF(MgTextFaint, 0.55+0.45*al))
                        }
                case zoneZones:
                        fillRoundRectGradV(screen, h.x, y, h.w, h.h, 10, withAlphaF(MgTileHoverTop, 0.74*al), plate)
                        strokeRoundRect(screen, h.x+0.5, y+0.5, h.w-1, h.h-1, 9.5, 1.5, border)
                        fillRoundRect(screen, h.x-3, y+h.h/2-9, 3, 18, 1.5, withAlphaF(MgOrange, al))
                }
        }
}

func (d *dashboard) drawTooltip(screen *ebiten.Image) {
        mx, my := ebiten.CursorPosition()
        f := regular(12)
        w, _ := measure(d.hovTooltip, f, 16)
        bw := w + 22
        x := float64(mx) - bw/2
        if x < 8 {
                x = 8
        }
        if x+bw > float64(LogicalW)-8 {
                x = float64(LogicalW) - 8 - bw
        }
        al := d.tipFade
        e := easeOutCubic(al)
        
        
        var y float64
        if float64(my) <= toolbarH {
                y = toolbarH + 6 - (1 - e) * 6
        } else {
                y = float64(my) + 18 + (1-e)*(-6)
                if y+24 > float64(LogicalH)-6 {
                        y = float64(my) - 26 - (1-e)*(-6)
                }
        }
        fillRoundRectGradV(screen, x, y, bw, 24, 6,
                withAlphaF(RGBA{0x2a, 0x2e, 0x32, 1}, al), withAlphaF(RGBA{0x10, 0x12, 0x14, 1}, al))
        strokeRoundRect(screen, x+0.5, y+0.5, bw-1, 23, 5.5, 1, withAlphaF(RGBA{0x4a, 0x52, 0x58, 1}, al))
        drawGloss(screen, x, y, bw, 24, 6, 0.5, 0.10*al)
        drawText(screen, d.hovTooltip, x+11, y+5, f, withAlphaF(MgLight, al))
}

func (d *dashboard) drawZonePanel(screen *ebiten.Image, z int, offY float64, a *app, vis float64) {
        const px = 16.0
        x := px
        y := shellPanelY - offY 
        w := float64(LogicalW) - px*2
        h := shellPanelH
        drawPanelChrome(screen, x, y, w, h, 12)
        dy := y - toolbarH 
        switch z {
        case zoneMy:
                d.drawMyZone(screen, dy, a, vis)
        case zoneApps:
                d.drawApps(screen, dy, a, vis)
        case zoneZones:
                d.drawZones(screen, dy, a, vis)
        }
        
        
}

const shellPanelY = toolbarH


func itemY(alpha float64) float64 {
        return (1 - easeOutCubic(alpha)) * 18
}



func (d *dashboard) drawPanelFooterDyn(screen *ebiten.Image, offY float64) {
        if d.zone < 0 || d.footerT <= 0.01 {
                return
        }
        z := d.zone
        y := offY + shellPanelH - 44
        kcBase := withAlphaF(RGBA{0xd8, 0xda, 0xd6, 0xff}, 1)
        txtBase := MgTextFaint
        f := regular(11)
        x := 44.0
        const kh = 20.0
        step := 0 
        next := func() float64 {
                a := itemAlpha(d.footerT, float64(step)*0.10)
                step++
                return a
        }
        key := func(label, label2 string, al float64) {
                drawKeycap(screen, x, y+8+(1-easeOutCubic(al))*8, kh, label, withAlphaF(kcBase, al))
                x += keycapWidth(label, kh) + 7
                if label2 != "" {
                        drawText(screen, label2, x, y+13+(1-easeOutCubic(al))*6, f, withAlphaF(txtBase, al))
                        x += measureTextW(label2, f) + 14
                }
        }
        switch z {
        case zoneMy:
                key("Tab", "switch zone", next())
                key("Esc", "close", next())
                key("Enter", "activate", next())
        case zoneApps:
                key("Tab", "switch zone", next())
                key("←→", "category", next())
                key("Esc", "close", next())
                drawText(screen, "type to search", x, y+13, f, withAlphaF(txtBase, itemAlpha(d.footerT, 0.3)))
        case zoneZones:
                key("Tab", "switch zone", next())
                key("Enter", "activate zone", next())
                key("Esc", "close", next())
        }
}

func (d *dashboard) drawToolbar(screen *ebiten.Image, a *app) {
        d.drawToolbarChrome(screen, a)
        d.drawToolbarDynamic(screen)
}


func (d *dashboard) drawToolbarChrome(screen *ebiten.Image, a *app) {
        
        fillRoundRectGradV(screen, 0, 0, float64(LogicalW), toolbarH, 0, MgBarGradTop, MgBarGradBot)
        fillRect(screen, 0, toolbarH-1, float64(LogicalW), 1, MgBarBorder)
        strokeLine(screen, 0, 0.5, float64(LogicalW), 0.5, 1, RGBA{0xff, 0xff, 0xff, 0x1c})
        drawText(screen, "NuggetOS", 14, 9, boldFace(14), MgLight)

        glyphs := []func(*ebiten.Image, float64, float64, float64, RGBA){iconHouse, iconAppGrid, iconZones}
        for z := 0; z < zoneCount; z++ {
                bx := 112 + float64(z)*58
                glyphs[z](screen, bx+26, toolbarH/2, 9, MgLightDim)
        }
        
        fillRect(screen, 294, 8, 1, toolbarH-16, RGBA{0xff, 0xff, 0xff, 0x22})

        
        apps := d.quickApps()
        for i, app := range apps {
                ax := 306 + float64(i)*34
                if app.icon != "" {
                        drawMeegoIconC(screen, app.icon, ax+11, toolbarH/2, 20)
                } else {
                        d.loadIcon()
                        if d.iconTile != nil {
                                op := &ebiten.DrawImageOptions{}
                                op.GeoM.Scale(20.0/200, 20.0/200)
                                op.GeoM.Translate(ax+1, toolbarH/2-10)
                                screen.DrawImage(d.iconTile, op)
                        } else {
                                drawMeegoIconC(screen, "tiles/games.png", ax+11, toolbarH/2, 20)
                        }
                }
        }

        
        drawMeegoIconC(screen, "status/bluetooth.png", toolbarBtX()+8, toolbarH/2, 15)
        drawMeegoIconC(screen, "status/network.png", toolbarNetX()+12, toolbarH/2, 16)
        drawMeegoIconC(screen, volIconPath(supervisor.M().Settings().Volume), toolbarVolX()+12, toolbarH/2, 16)
}


func (d *dashboard) drawToolbarDynamic(screen *ebiten.Image) {
        
        if d.tbIndA > 0.01 {
                ix := d.tbIndX()
                fillRoundRectGradV(screen, ix-26, 4, 52, toolbarH-8, 7, withAlphaF(MgBarPillTop, d.tbIndA), withAlphaF(MgBarPillBot, d.tbIndA))
                drawGloss(screen, ix-26, 4, 52, toolbarH-8, 7, 0.5, 0.10*d.tbIndA)
                strokeRoundRect(screen, ix-25.5, 4.5, 51, toolbarH-9, 6.5, 1, withAlphaF(RGBA{0x55, 0x5c, 0x62, 1}, d.tbIndA))
        }
        if z := d.zone; z >= 0 && z < zoneCount && d.tbIndA > 0.01 {
                acc := []RGBA{MgCyan, MgRed, MgOrange}[z]
                gl := []func(*ebiten.Image, float64, float64, float64, RGBA){iconHouse, iconAppGrid, iconZones}[z]
                gl(screen, 112+float64(z)*58+26, toolbarH/2, 9, acc)
        }
        
        
        mx, my := ebiten.CursorPosition()
        nApps := len(d.quickApps())
        if float64(my) <= toolbarH+2 {
                for i := 0; i < nApps; i++ {
                        ax := 306 + float64(i)*34
                        if float64(mx) >= ax-4 && float64(mx) < ax+30 {
                                fillRoundRectGradV(screen, ax-5, 5, 30, toolbarH-10, 6, RGBA{0x3a, 0x40, 0x46, 0xff}, RGBA{0x1c, 0x20, 0x24, 0xff})
                                drawGloss(screen, ax-5, 5, 30, toolbarH-10, 6, 0.5, 0.12)
                        }
                }
        }
        
        vol := supervisor.M().Settings().Volume
        for i := 0; i < 3; i++ {
                bh := 3.0 + float64(i)*2
                on := vol > float64(i+1)/3.0-0.04
                c := RGBA{0x5a, 0x62, 0x68, 0xff}
                if on {
                        c = MgCyan
                }
                fillRoundRect(screen, toolbarVolX()+30, toolbarH/2+4-float64(i)*3-bh/2, 3, bh, 1, c)
        }
        drawTextRight(screen, clockText(), LogicalW-46, 9, medium(14), MgLight)
        drawMeegoIconC(screen, batteryIconPath(d.bat), LogicalW-21, toolbarH/2, 18)
}

func volIconPath(v float64) string {
        if v <= 0 {
                return "status/volume-muted.png"
        }
        return "status/volume-high.png"
}





func (d *dashboard) drawMyZone(screen *ebiten.Image, dy float64, a *app, vis float64) {
        sel := d.sel[zoneMy]
        nLinks := len(d.quickLinks())
        ta := itemAlpha(vis, 0)
        if ta > 0 {
                
                
                drawText(screen, "My Zone", 44, dy+toolbarH+24+itemY(ta), boldFace(22), withAlphaF(MgCyan, ta))
        }
        ca := itemAlpha(vis, 0.06)
        if ca > 0 && !d.caching {
                // While caching, the clock is skipped: it is drawn live as
                // an overlay (see drawClockOverlay) so the panel cache does
                // not have to be rebuilt every minute.
                drawTextRight(screen, clockText(), float64(LogicalW)-44, dy+toolbarH+20+itemY(ca), boldFace(34), withAlphaF(MgPanelHead, ca))
                drawTextRight(screen, dateText(), float64(LogicalW)-44, dy+toolbarH+78+itemY(ca), regular(13), withAlphaF(MgTextDim, ca))
        }

        
        ga := itemAlpha(vis, 0.14)
        if ga > 0 {
                gx, gy := mzCardX, mzCardY+dy+itemY(ga)
                hovA := 0.0
                if sel == 0 && sel < len(d.selA) {
                        hovA = d.selAof(0)
                }
                d.drawGameCard(screen, gx, gy, hovA, ga, a)
        }

        
        la := itemAlpha(vis, 0.24)
        if la > 0 {
                lx, ly := mzLinkX, mzSysY+dy
                d.drawPaneHeader(screen, lx, ly, mzLinkW, "QUICK LINKS", la)
                links := d.quickLinks()
                for i := 0; i < nLinks; i++ {
                        ln := links[i]
                        ia := clamp01((vis*1.3 - (0.28 + float64(i)*0.05)) / 0.3)
                        if ia <= 0 {
                                continue
                        }
                        ry := mzLinkY0 + dy + float64(i)*56 + itemY(ia)
                        hovA := 0.0
                        if sel == i+1 && i+1 < len(d.selA) {
                                hovA = d.selAof(i + 1)
                        }
                        d.drawLinkRow(screen, lx, ry, mzLinkW, mzLinkH, hovA, ln, ia)
                }
        }

        
        sa := itemAlpha(vis, 0.34)
        if sa > 0 {
                sx, sy := mzSysX, mzSysY+dy
                d.drawPaneHeader(screen, sx, sy, mzSysW, "SYSTEM", sa)
                rows := d.systemRows()
                for i, r := range rows {
                        ry := sy + 42 + float64(i)*44 + itemY(sa)
                        rowAl := withAlphaF(MgTextDim, sa)
                        
                        iconX := sx + 26.0
                        labelX := sx + 46.0
                        drawText(screen, clampText(r[0], regular(12), 120), labelX, ry+12, regular(12), rowAl)
                        drawTextRight(screen, clampText(r[1], medium(12), 190), sx+mzSysW-16, ry+12, medium(12), withAlphaF(MgText, sa))
                        
                        switch r[0] {
                        case "hot reload":
                                on := r[1] == "on"
                                d.drawToggleDot(screen, sx+mzSysW-16-measureTextW(r[1], medium(12))-22, ry+15, on, sa)
                                drawStatusDot(screen, iconX, ry+17, 2.6, MgTextFaint, d.t, on)
                        case "sound":
                                
                                v := supervisor.M().Settings().Volume
                                for b := 0; b < 5; b++ {
                                        on := v >= float64(b+1)/5.0-0.04
                                        c := RGBA{0xc8, 0xcd, 0xd2, 0xff}
                                        if on {
                                                c = MgCyan
                                        }
                                        fillRoundRect(screen, labelX-18+float64(b)*6, ry+22-float64(b)*1.6, 4, 4+float64(b)*2.4, 1.2, withAlphaF(c, sa))
                                }
                                drawMeegoIconC(screen, "status/volume-high.png", iconX, ry+17, 13)
                        case "engine":
                                iconGear(screen, iconX, ry+17, 5.5, withAlphaF(MgTextFaint, sa))
                        case "build type":
                                iconInfo(screen, iconX, ry+17, 5.5, withAlphaF(MgTextFaint, sa))
                        case "workspace":
                                iconZones(screen, iconX, ry+17, 6, withAlphaF(MgTextFaint, sa))
                        }
                        if i < len(rows)-1 {
                                fillRect(screen, sx+16, ry+42, mzSysW-32, 1, withAlphaF(MgDivider, sa*1.8))
                        }
                }
                drawText(screen, "nuggetos "+buildinfo.Type()+" build", sx+16, sy+42+float64(len(rows))*44+16, regular(10), withAlphaF(MgTextFaint, sa))
        }
}


func (d *dashboard) drawToggleDot(screen *ebiten.Image, cx, cy float64, on bool, al float64) {
        w, h := 26.0, 12.0
        c := mixRGBA(RGBA{0xb9, 0xc0, 0xc6, 0xff}, MgCyan, 0)
        if on {
                c = MgCyan
        }
        fillRoundRectGradV(screen, cx-w/2, cy-h/2, w, h, h/2, withAlphaF(mixRGBA(c, RGBA{0xff, 0xff, 0xff, 0xff}, 0.25), al), withAlphaF(c, al))
        kx := cx - w/2 + 3
        if on {
                kx = cx + w/2 - 9
        }
        fillRoundRectGradV(screen, kx, cy-h/2+1.5, 9, h-3, (h-3)/2, withAlphaF(RGBA{0xff, 0xff, 0xff, 0xff}, al), withAlphaF(RGBA{0xe8, 0xea, 0xec, 0xff}, al))
}

func (d *dashboard) drawGameCard(screen *ebiten.Image, gx, gy, hovA, vis float64, a *app) {
        
        glow := 0.0
        if d.selPulse > 0 {
                glow = d.selPulse / 0.26
        }
        if hovA > 0.01 || glow > 0 {
                fillRoundRect(screen, gx-3, gy-2, mzCardW+6, mzCardH+5, 11, withAlphaF(MgTileHoverBd, 0x30*hovA+0x28*glow))
        }
        drawTileChrome(screen, gx, gy, mzCardW, mzCardH, 8)
        if hovA > 0.01 {
                strokeRoundRect(screen, gx-0.5, gy-0.5, mzCardW+1, mzCardH+1, 8.5, 2, withAlphaF(MgTileHoverBd, 0.9*hovA+0.1*glow))
        }
        
        d.loadIcon()
        if d.iconTile != nil {
                dropShadow(screen, gx+mzCardW/2-100, gy+16, 200, 200)
                op := &ebiten.DrawImageOptions{}
                op.GeoM.Translate(gx+mzCardW/2-100, gy+16)
                screen.DrawImage(d.iconTile, op)
        } else {
                drawMeegoIconC(screen, "tiles/games.png", gx+mzCardW/2, gy+116, 96)
        }
        drawTextCenter(screen, clampText(d.gameTitle, boldFace(19), mzCardW-24), gx+mzCardW/2, gy+232, boldFace(19), withAlphaF(MgText, vis))
        if d.gameFound {
                drawTextCenter(screen, clampText(fmt.Sprintf("version %s · %d files", d.gameSub, d.gameFiles), regular(12), mzCardW-24), gx+mzCardW/2, gy+264, regular(12), withAlphaF(MgTextDim, vis))
        } else {
                drawTextCenter(screen, "place game.nkpkg in flwdata/packages", gx+mzCardW/2, gy+264, regular(12), withAlphaF(MgTextDim, vis))
        }
        
        label := "open"
        if d.gameRunning() {
                label = "resume"
        }
        bx, by := gx+mzCardW/2-70, gy+300
        press := 0.0
        if !d.caching && d.pressT > 0 && d.sel[zoneMy] == 0 {
                press = d.pressT / 0.16
        }
        drawButtonPill(screen, bx, by, 140, 40, MgCyan, press, 0)
        iconPlayTriangle(screen, bx+24, by+20, 7, MgLight)
        w, _ := measure(label, medium(14), 18)
        drawText(screen, label, bx+140-16-w, by+13, medium(14), MgLight)
        
        
        st := supervisor.M().State()
        statusLine, statusCol := "not running", MgTextDim
        if st == supervisor.StateRunning {
                statusLine, statusCol = "running", MgGreen
        } else if st == supervisor.StateLaunching {
                statusLine, statusCol = "starting...", MgOrange
        }
        drawStatusDot(screen, gx+mzCardW/2-measureTextW(statusLine, regular(12))/2-12, gy+371, 3.2, statusCol, d.t, st == supervisor.StateRunning && !d.caching)
        drawTextCenter(screen, statusLine, gx+mzCardW/2, gy+364, regular(12), withAlphaF(statusCol, vis))
        
        mx := gx + mzCardW - 48
        fillRoundRectGradV(screen, mx, gy+10, 36, 28, 6, RGBA{0xff, 0xff, 0xff, 0xff}, MgTileGradBot)
        strokeRoundRect(screen, mx+0.5, gy+10.5, 35, 27, 5.5, 1, MgTileBorder)
        for i := 0; i < 3; i++ {
                fillCircle(screen, mx+18, gy+19+float64(i)*5, 1.6, MgTextDim)
        }
}

func (d *dashboard) drawLinkRow(screen *ebiten.Image, lx, ry, w, h, hovA float64, ln meegoLink, al float64) {
        
        if hovA > 0.01 {
                fillRoundRectGradV(screen, lx, ry, w, h, 8, withAlphaF(MgTileHoverTop, hovA), withAlphaF(MgTileHover, hovA))
                strokeRoundRect(screen, lx+0.5, ry+0.5, w-1, h-1, 7.5, 1, withAlphaF(MgTileHoverBd, hovA))
                
                fillRoundRect(screen, lx-3, ry+h/2-9, 3, 18, 1.5, withAlphaF(MgCyan, hovA))
        }
        
        fillRoundRectGradV(screen, lx+8, ry+6, 40, 40, 8, RGBA{0xff, 0xff, 0xff, 0xff}, MgTileGradBot)
        strokeRoundRect(screen, lx+8.5, ry+6.5, 39, 39, 7.5, 1, withAlphaF(MgTileBorder, al))
        drawMeegoIconC(screen, ln.icon, lx+28, ry+26, 26)
        drawText(screen, clampText(ln.label, medium(14), w-58-70), lx+58, ry+7, medium(14), withAlphaF(MgText, al))
        drawText(screen, clampText(ln.sub, regular(11), w-58-60), lx+58, ry+29, regular(11), withAlphaF(MgTextDim, al))
        iconChevron(screen, lx+w-18, ry+26, 5, withAlphaF(MgTextFaint, 0.35+0.65*hovA))
}

func (d *dashboard) drawPaneHeader(screen *ebiten.Image, x, y, w float64, label string, al float64) {
        
        fillRoundRectGradV(screen, x, y, w, 26, 5, withAlphaF(RGBA{0xef, 0xf0, 0xee, 0xff}, al), withAlphaF(RGBA{0xdf, 0xe1, 0xdc, 0xff}, al))
        strokeRoundRect(screen, x+0.5, y+0.5, w-1, 25, 4.5, 1, withAlphaF(RGBA{0xcc, 0xcf, 0xc9, 0xff}, al))
        fillRect(screen, x+16, y+9, 3, 9, withAlphaF(MgCyan, al))
        drawText(screen, label, x+27, y+7, boldFace(11), withAlphaF(MgTextDim, al))
}





func (d *dashboard) drawApps(screen *ebiten.Image, dy float64, a *app, vis float64) {
        sel := d.sel[zoneApps]
        ta := itemAlpha(vis, 0)
        if ta > 0 {
                drawText(screen, "Applications", 44, dy+toolbarH+26+itemY(ta), boldFace(22), withAlphaF(MgRed, ta))
                drawTextRight(screen, fmt.Sprintf("%d applications", len(d.appEntries())), float64(LogicalW)-44, dy+toolbarH+30+itemY(ta), regular(12), withAlphaF(MgTextDim, ta))
        }

        
        
        sa := itemAlpha(vis, 0.08)
        if sa > 0 {
                sx := float64(LogicalW) - 300
                sy := apSearchY + dy + itemY(sa)
                fillRoundRectGradV(screen, sx, sy, 260, 32, 16, withAlphaF(RGBA{0xe9, 0xea, 0xe7, 0xff}, sa), withAlphaF(RGBA{0xf7, 0xf8, 0xf6, 0xff}, sa))
                strokeRoundRect(screen, sx+0.5, sy+0.5, 259, 31, 15.5, 1, withAlphaF(MgTileBorder, sa))
                
                strokeLine(screen, sx+14, sy+2.2, sx+246, sy+2.2, 1, withAlphaF(RGBA{0x00, 0x00, 0x00, 0x1a}, sa))
                iconSearch(screen, sx+16, sy+16, 6, withAlphaF(MgTextDim, sa))
        }

        
        for i, cat := range appCats {
                ia := clamp01((vis*1.3 - (0.12 + float64(i)*0.045)) / 0.3)
                if ia <= 0 {
                        continue
                }
                tx := 44 + float64(i)*84
                ty := apTileY + dy + itemY(ia)
                selected := d.cat == i
                hov := d.hovCat == i
                acc := catAccent[cat]
                if selected || hov {
                        fillRoundRectGradV(screen, tx-8, ty-8, 68, 76, 10, MgTileHoverTop, MgTileHover)
                        strokeRoundRect(screen, tx-7.5, ty-7.5, 67, 75, 9.5, 1.5, MgTileHoverBd)
                }
                if selected {
                        
                        fillRoundRect(screen, tx+10, ty+58, 32, 3, 1.5, acc)
                }
                drawMeegoIconC(screen, "tiles/"+cat+".png", tx+26, ty+22, 44)
                drawTextCenter(screen, cat, tx+26, ty+44, regular(11), withAlphaF(mixRGBA(MgTextDim, acc, selectedB(selected)), ia))
                
                n := d.catCount(cat)
                drawChip(screen, tx+42, ty-6, fmt.Sprint(n), withAlphaF(acc, ia), false)
        }

        
        apps := d.filteredApps()
        if len(apps) == 0 {
                ea := itemAlpha(vis, 0.3)
                iconAppGrid(screen, 340, apRowY0+dy+50, 26, withAlphaF(MgTextFaint, ea*0.8))
                drawText(screen, "no applications match", 400, apRowY0+dy+34, medium(15), withAlphaF(MgTextDim, ea))
                drawText(screen, "clear the search field or pick another category", 400, apRowY0+dy+58, regular(12), withAlphaF(MgTextFaint, ea))
                return
        }
        for i, app := range apps {
                col := i % 2
                row := i / 2
                ia := clamp01((vis*1.3 - (0.22 + float64(i)*0.05)) / 0.3)
                if ia <= 0 {
                        continue
                }
                ax := 40 + float64(col)*600
                ay := apRowY0 + dy + float64(row)*apRowH + itemY(ia)
                s := sel == i+1
                hovA := 0.0
                if s && i+1 < len(d.selA) {
                        hovA = d.selAof(i + 1)
                }
                d.drawAppRow(screen, ax, ay, apRowW, 76, hovA, app, ia)
        }
}



// drawClockOverlay draws the clock and the date of the My Zone panel on top
// of the cached panel image. The clock has minute resolution; baking it into
// the cache would rebuild the whole panel every minute.
func (d *dashboard) drawClockOverlay(screen *ebiten.Image, offY float64) {
        if d.zone != zoneMy || d.anim < 0.999 || d.closing >= 0 {
                return
        }
        ca := itemAlpha(1, 0.06)
        if ca <= 0 {
                return
        }
        dy := offY
        drawTextRight(screen, clockText(), float64(LogicalW)-44, dy+toolbarH+20+itemY(ca), boldFace(34), withAlphaF(MgPanelHead, ca))
        drawTextRight(screen, dateText(), float64(LogicalW)-44, dy+toolbarH+78+itemY(ca), regular(13), withAlphaF(MgTextDim, ca))
}

func (d *dashboard) drawSearchOverlay(screen *ebiten.Image, offY float64) {
        if d.zone != zoneApps {
                return
        }
        sx := float64(LogicalW) - 300
        sy := apSearchY + offY
        focused := d.sel[zoneApps] == 0
        if focused {
                
                uiCursor = curText
        }
        if d.search == "" {
                drawText(screen, "search", sx+32, sy+8, regular(13), MgTextFaint)
        } else {
                drawText(screen, clampText(d.search, regular(13), 210), sx+32, sy+8, regular(13), MgText)
        }
        if focused && int(d.t*2)%2 == 0 {
                txt := d.search
                if measureTextW(txt, regular(13)) > 210 {
                        txt = clampText(txt, regular(13), 210)
                }
                w := measureTextW(txt, regular(13))
                fillRect(screen, sx+34+w, sy+8, 1.5, 16, MgText)
        }
}

func (d *dashboard) drawAppRow(screen *ebiten.Image, ax, ay, w, h float64, hovA float64, app shellApp, al float64) {
        acc := catAccent[app.cat]
        if hovA > 0.01 {
                fillRoundRectGradV(screen, ax, ay, w, h, 10, withAlphaF(MgAppHoverTop, hovA), withAlphaF(MgAppHover, hovA))
                strokeRoundRect(screen, ax+0.5, ay+0.5, w-1, h-1, 9.5, 1.5, withAlphaF(MgAppHoverBd, hovA))
                fillRoundRect(screen, ax-3, ay+h/2-9, 3, 18, 1.5, withAlphaF(acc, hovA))
        } else {
                drawTileChrome(screen, ax, ay, w, h, 10)
        }
        
        fillCircle(screen, ax+42, ay+h/2, 27, withAlphaF(RGBA{0xf3, 0xf4, 0xf2, 0xff}, al))
        strokeCircle(screen, ax+42, ay+h/2, 27, 1, withAlphaF(MgTileBorder, al))
        if app.icon != "" {
                drawMeegoIconC(screen, app.icon, ax+42, ay+h/2, 44)
        } else {
                d.loadIcon()
                if d.iconTile != nil {
                        op := &ebiten.DrawImageOptions{}
                        op.GeoM.Scale(44.0/200, 44.0/200)
                        op.GeoM.Translate(ax+20, ay+16)
                        screen.DrawImage(d.iconTile, op)
                } else {
                        drawMeegoIconC(screen, "tiles/games.png", ax+42, ay+h/2, 44)
                }
        }
        drawText(screen, clampText(app.name, medium(15), w-96-chipWidthOf(app.cat)-40), ax+80, ay+15, medium(15), withAlphaF(MgText, al))
        drawText(screen, clampText(app.desc, regular(11), w-96-40), ax+80, ay+42, regular(11), withAlphaF(MgTextDim, al))
        drawChip(screen, ax+w-16-chipWidthOf(app.cat), ay+14, app.cat, withAlphaF(acc, al), false)
        if hovA > 0.01 {
                iconChevron(screen, ax+w-22, ay+h/2, 5, withAlphaF(MgTextFaint, 0.35+0.65*hovA))
        }
}

func chipWidthOf(label string) float64 {
        f := regular(10)
        w, _ := measure(label, f, 13)
        return w + 14
}





var (
        zoneThumbOnce sync.Once
        zoneThumbImg  *ebiten.Image
)


func zonePreviewImg() *ebiten.Image {
        zoneThumbOnce.Do(func() {
                const tw, th = 360, 86
                dst := ebiten.NewImage(tw, th)
                if w := getWallpaper(); w != nil {
                        b := w.Bounds()
                        sw, sh := float64(b.Dx()), float64(b.Dy())
                        sc := math.Max(tw/sw, th/sh)
                        dw, dh := sw*sc, sh*sc
                        op := &ebiten.DrawImageOptions{}
                        op.GeoM.Scale(sc, sc)
                        op.GeoM.Translate((float64(tw)-dw)/2, (float64(th)-dh)/2)
                        dst.DrawImage(w, op)
                } else {
                        dst.Fill(RGBA{0x78, 0x76, 0xa8, 0xff})
                }
                zoneThumbImg = dst
        })
        return zoneThumbImg
}

func (d *dashboard) drawZones(screen *ebiten.Image, dy float64, a *app, vis float64) {
        sel := d.sel[zoneZones]
        ws := supervisor.M().Settings().Workspace
        ta := itemAlpha(vis, 0)
        if ta > 0 {
                drawText(screen, "Zones", 44, dy+toolbarH+26+itemY(ta), boldFace(22), withAlphaF(MgOrange, ta))
                drawTextRight(screen, fmt.Sprintf("active: workspace%d", ws), float64(LogicalW)-44, dy+toolbarH+30+itemY(ta), regular(12), withAlphaF(MgTextDim, ta))
        }

        xs := []float64{znColX, znColX + 416, znColX + 832}
        ws3 := []float64{znColW, znColW, 352.0}
        for i := 0; i < 3; i++ {
                ia := clamp01((vis*1.3 - (0.10 + float64(i)*0.08)) / 0.3)
                if ia <= 0 {
                        continue
                }
                x := xs[i]
                w := ws3[i]
                yy := znColY + dy + itemY(ia)
                active := (i == 0 && ws == 0) || (i == 1 && ws == 1)
                
                drawTileChrome(screen, x, yy, w, znColH, 8)
                
                tc := mixRGBA(RGBA{0xd8, 0xd8, 0xd4, 0xff}, MgOrange, selectedB(active))
                if i == 0 {
                        tc = MgOrange
                }
                fillRoundRectGradV(screen, x, yy, w, znHdrH, 8, mixRGBA(tc, RGBA{0xff, 0xff, 0xff, 0xff}, 0.18), mixRGBA(tc, RGBA{0x00, 0x00, 0x00, 0xff}, 0.12))
                fillRect(screen, x+1, yy+znHdrH-8, w-2, 8, mixRGBA(tc, RGBA{0x00, 0x00, 0x00, 0xff}, 0.12))
                tl := fmt.Sprintf("Zone %d", i+1)
                if i == 2 {
                        tl = "New Zone"
                }
                drawText(screen, tl, x+14, yy+7, boldFace(13), MgLight)
                if i < 2 {
                        
                        pw := 84.0
                        px := x + w - pw - 10
                        fillRoundRectGradV(screen, px, yy+7, pw, 16, 8, RGBA{0xff, 0xff, 0xff, 0x3c}, RGBA{0x00, 0x00, 0x00, 0x2e})
                        drawTextRight(screen, fmt.Sprintf("workspace%d", i), px+pw-10, yy+11, regular(10), MgLight)
                        if active {
                                drawStatusDot(screen, px+10, yy+15, 2.6, RGBA{0xff, 0xff, 0xff, 0xff}, d.t, false)
                        }
                }
        }

        
        z1a := itemAlpha(vis, 0.22)
        if z1a > 0 {
                x1 := xs[0]
                py := znPrevY + dy + itemY(z1a)
                d.drawZonePreview(screen, x1+12, py, znColW-24, znPrevH, z1a, a)
                cardY := znCardY + dy + itemY(clamp01(z1a+0.12))
                hovA := 0.0
                if sel == 0 && 0 < len(d.selA) {
                        hovA = d.selAof(0)
                }
                d.drawZoneGameCard(screen, x1+12, cardY, znColW-24, znCardH, hovA, z1a)
                
                if d.gameRunning() {
                        by := znColY + dy + znColH + 12
                        hov2 := 0.0
                        if sel == 1 && 1 < len(d.selA) {
                                hov2 = d.selAof(1)
                        }
                        d.drawZoneCloseRow(screen, x1+12, by, 172, 40, hov2, z1a)
                }
        }

        
        z2a := itemAlpha(vis, 0.3)
        if z2a > 0 {
                x2 := xs[1]
                cardY := znCardY + dy + itemY(z2a)
                z2sel := 1
                if d.gameRunning() {
                        z2sel = 2
                }
                hovA := 0.0
                if sel == z2sel && z2sel < len(d.selA) {
                        hovA = d.selAof(z2sel)
                }
                yy := cardY
                w := znColW - 24
                if hovA > 0.01 {
                        fillRoundRectGradV(screen, x2+12, yy, w, znCardH, 10, withAlphaF(MgTileHoverTop, hovA), withAlphaF(MgTileHover, hovA))
                        strokeRoundRect(screen, x2+12.5, yy+0.5, w-1, znCardH-1, 9.5, 1.5, withAlphaF(MgTileHoverBd, hovA))
                        fillRoundRect(screen, x2+9, yy+znCardH/2-9, 3, 18, 1.5, withAlphaF(MgOrange, hovA))
                }
                iconZones(screen, x2+12+w/2, yy+46, 15, withAlphaF(MgTextFaint, z2a))
                drawTextCenter(screen, "no applications in this zone", x2+12+w/2, yy+74, regular(13), withAlphaF(MgTextFaint, z2a))
                
                bx := x2 + 12 + w/2 - 56
                drawButtonPill(screen, bx, yy+98, 112, 30, RGBA{0x8e, 0x96, 0x9c, 0xff}, 0, 0)
                drawTextCenter(screen, "activate", x2+12+w/2, yy+106, medium(11), MgLight)
        }

        
        z3a := itemAlpha(vis, 0.38)
        if z3a > 0 {
                x3 := xs[2]
                cardY := znCardY + dy + itemY(z3a)
                hovA := 0.0
                if sel == 3 && 3 < len(d.selA) {
                        hovA = d.selAof(3)
                }
                yy, w, h := cardY, 352.0-24.0, znCardH
                strokeRoundRectDashed(screen, x3+12, yy, w, h, 10, 2, withAlphaF(MgTextFaint, 0.55+0.45*hovA), 7, 6)
                
                cx, cy := x3+12+w/2, yy+58
                fillCircle(screen, cx, cy+1.5, 20, withAlphaF(RGBA{0x00, 0x00, 0x00, 0x1c}, z3a))
                fillCircle(screen, cx, cy, 20, withAlphaF(MgOrange, z3a))
                drawGloss(screen, cx-19, cy-19, 38, 38, 19, 0.5, 0.30)
                strokeCircle(screen, cx, cy, 19.5, 1, withAlphaF(mixRGBA(MgOrange, RGBA{0x00, 0x00, 0x00, 0xff}, 0.4), z3a))
                iconPlusGlyph(screen, cx, cy, 11, MgLight)
                drawTextCenter(screen, "add zone", x3+12+w/2, yy+108, regular(11), withAlphaF(MgTextFaint, z3a))
                drawTextCenter(screen, "creating zones is disabled in this build", x3+12+w/2, yy+128, regular(10), withAlphaF(MgTextFaint, 0.6*z3a))
        }
}


func (d *dashboard) drawZonePreview(screen *ebiten.Image, x, y, w, h, al float64, a *app) {
        if img := zonePreviewImg(); img != nil {
                op := &ebiten.DrawImageOptions{}
                op.GeoM.Scale(w/360, h/86)
                op.GeoM.Translate(x, y)
                screen.DrawImage(img, op)
        }
        
        fillRoundRectGradV(screen, x, y+h-26, w, 26, 0, RGBA{0x00, 0x00, 0x00, 0x00}, withAlphaF(RGBA{0x00, 0x00, 0x00, 0xb8}, al))
        drawText(screen, clampText(d.gameTitle, medium(11), w-24), x+10, y+h-19, medium(11), withAlphaF(MgLight, al))
        
        strokeRoundRect(screen, x+0.5, y+0.5, w-1, h-1, 2, 1, withAlphaF(RGBA{0x00, 0x00, 0x00, 0x61}, al))
        
        cx, cy := x+w/2, y+h/2
        if d.gameRunning() {
                fillCircle(screen, cx, cy+1.5, 16, withAlphaF(RGBA{0x00, 0x00, 0x00, 0x30}, al))
                fillCircle(screen, cx, cy, 16, withAlphaF(RGBA{0xff, 0xff, 0xff, 0xb9}, al))
                drawGloss(screen, cx-15, cy-15, 30, 30, 15, 0.5, 0.35)
                iconPlayTriangle(screen, cx+1.5, cy, 7, MgText)
                drawStatusDot(screen, x+w-14, y+13, 3.4, MgGreen, d.t, true)
        } else {
                fillCircle(screen, cx, cy+1.5, 16, withAlphaF(RGBA{0x00, 0x00, 0x00, 0x30}, al))
                fillCircle(screen, cx, cy, 16, withAlphaF(RGBA{0xff, 0xff, 0xff, 0x66}, al))
                iconPlayTriangle(screen, cx+1.5, cy, 7, withAlphaF(MgLight, al))
        }
}

func (d *dashboard) drawZoneGameCard(screen *ebiten.Image, x, y, w, h, hovA, al float64) {
        if hovA > 0.01 {
                fillRoundRect(screen, x-3, y-2, w+6, h+5, 12, withAlphaF(MgTileHoverBd, 0x30*hovA))
                fillRoundRectGradV(screen, x, y, w, h, 10, withAlphaF(MgTileHoverTop, hovA), withAlphaF(MgTileHover, hovA))
                strokeRoundRect(screen, x+0.5, y+0.5, w-1, h-1, 9.5, 1.5, withAlphaF(MgTileHoverBd, hovA))
        } else {
                drawTileChrome(screen, x, y, w, h, 10)
        }
        d.loadIcon()
        if d.iconTile != nil {
                op := &ebiten.DrawImageOptions{}
                op.GeoM.Scale(84.0/200, 84.0/200)
                op.GeoM.Translate(x+18, y+18)
                screen.DrawImage(d.iconTile, op)
        } else {
                fillCircle(screen, x+60, y+60, 42, withAlphaF(RGBA{0xf3, 0xf4, 0xf2, 0xff}, al))
                drawMeegoIconC(screen, "tiles/games.png", x+60, y+60, 64)
        }
        tx := x + 118
        drawText(screen, clampText(d.gameTitle, boldFace(15), w-118-140), tx, y+18, boldFace(15), withAlphaF(MgText, al))
        st := supervisor.M().State()
        stateTxt, stateCol := "idle", MgTextDim
        if st == supervisor.StateRunning {
                stateTxt, stateCol = "running", MgGreen
        } else if st == supervisor.StateLaunching {
                stateTxt, stateCol = "starting...", MgOrange
        }
        if !d.caching {
                drawStatusDot(screen, tx+measureTextW(stateTxt, regular(11))/2+5, y+47, 2.8, stateCol, d.t, st == supervisor.StateRunning)
                drawTextCenter(screen, stateTxt, tx+measureTextW(stateTxt, regular(11))/2+16, y+41, regular(11), withAlphaF(stateCol, al))
        }
        drawText(screen, "the installed game package", tx, y+62, regular(11), withAlphaF(MgTextDim, al))
        
        press := 0.0
        if !d.caching && d.pressT > 0 && d.sel[zoneZones] == 0 {
                press = d.pressT / 0.16
        }
        label := "switch to"
        if d.gameRunning() {
                label = "resume"
        }
        bw := 118.0
        drawButtonPill(screen, tx, y+92, bw, 30, MgCyan, press, 0)
        drawTextCenter(screen, label, tx+bw/2, y+100, medium(11), MgLight)
}

func (d *dashboard) drawZoneCloseRow(screen *ebiten.Image, x, y, w, h, hovA, al float64) {
        if hovA > 0.01 {
                fillRoundRectGradV(screen, x, y, w, h, 8, withAlphaF(MgAppHoverTop, hovA), withAlphaF(MgAppHover, hovA))
                strokeRoundRect(screen, x+0.5, y+0.5, w-1, h-1, 7.5, 1.5, withAlphaF(MgAppHoverBd, hovA))
        } else {
                drawTileChrome(screen, x, y, w, h, 8)
        }
        iconCloseApp(screen, x+24, y+h/2, 7, MgRed)
        drawText(screen, "close application", x+40, y+13, medium(12), withAlphaF(MgText, al))
}





func (d *dashboard) drawVolOSD(screen *ebiten.Image) {
        st := supervisor.M().Settings()
        al := d.volAnim
        if al <= 0.01 {
                return
        }
        x := float64(LogicalW) - 244
        y := toolbarH + 8 - (1 - easeOutCubic(al)) * 14
        w, h := 220.0, 100.0
        fillRoundRectGradV(screen, x, y, w, h, 10, withAlphaF(MgDarkGradTop, al*0xf7/255), withAlphaF(MgDarkGradBot, al*0xf7/255))
        strokeRoundRect(screen, x+0.5, y+0.5, w-1, h-1, 9.5, 1, withAlphaF(RGBA{0x4a, 0x52, 0x58, 1}, al))
        drawGloss(screen, x, y, w, h, 10, 0.42, 0.09*al)
        drawText(screen, "volume", x+16, y+10, medium(12), withAlphaF(MgLightDim, al))
        drawTextRight(screen, fmt.Sprintf("%d%%", int(st.Volume*100)), x+w-16, y+10, medium(12), withAlphaF(MgLight, al))
        
        bx, _, bw, bh := d.volTrackRect()
        by := y + 48 
        fillRoundRectGradV(screen, bx, by, bw, bh, 4, withAlphaF(RGBA{0x00, 0x00, 0x00, 0x46}, al), withAlphaF(RGBA{0x00, 0x00, 0x00, 0x2a}, al))
        fillRoundRectGradV(screen, bx, by, bw*st.Volume, bh, 4, mixRGBA(MgCyan, RGBA{0xff, 0xff, 0xff, 0xff}, 0.25), MgCyan)
        kx := bx + bw*st.Volume
        
        mx, my := ebiten.CursorPosition()
        if inRect(float64(mx), float64(my), bx-12, by-12, bw+24, bh+24) {
                uiCursor = curHand
        }
        fillCircle(screen, kx, by+bh/2+1.2, 9, withAlphaF(RGBA{0x00, 0x00, 0x00, 0x40}, al))
        fillCircle(screen, kx, by+bh/2, 9, withAlphaF(MgLight, al))
        drawGloss(screen, kx-8, by+bh/2-8, 16, 16, 8, 0.5, 0.5)
        drawText(screen, "up/down or wheel · drag the knob · esc close", x+16, y+76, regular(10), withAlphaF(MgLightFaint, al))
}





func (d *dashboard) drawCtx(screen *ebiten.Image) {
        al := d.ctxAnim
        if al <= 0.01 {
                return
        }
        x, y, w := d.ctxRect()
        h := float64(len(d.ctxEnts))*48 + 16
        pop := easeOutBack(al)
        scale := 0.92 + 0.08*pop
        cx, cy := x+w/2, y+24
        nw := w * scale
        nh := h * scale
        nx, ny := cx-nw/2, cy-nh/2 + (1-al)*8
        dropShadow(screen, nx, ny, nw, nh)
        fillRoundRectGradV(screen, nx, ny, nw, nh, 10, withAlphaF(RGBA{0xfb, 0xfb, 0xfa, 1}, al), withAlphaF(RGBA{0xec, 0xee, 0xea, 1}, al))
        strokeRoundRect(screen, nx+0.5, ny+0.5, nw-1, nh-1, 9.5, 1, withAlphaF(MgPanelBorder, al))
        drawInnerTopLine(screen, nx, ny, nw, 10, withAlphaF(RGBA{0xff, 0xff, 0xff, 0xb4}, al))
        for i, e := range d.ctxEnts {
                ry := ny + 8 + float64(i)*48
                rowAl := clamp01(al*1.6 - float64(i)*0.08)
                if i == d.ctxSel {
                        fillRoundRectGradV(screen, nx+4, ry, nw-8, 44, 8, withAlphaF(MgTileHoverTop, rowAl), withAlphaF(MgTileHover, rowAl))
                        strokeRoundRect(screen, nx+4.5, ry+0.5, nw-9, 43, 7.5, 1, withAlphaF(MgTileHoverBd, rowAl))
                        fillRoundRect(screen, nx+1, ry+12, 2.5, 20, 1.2, withAlphaF(MgCyan, rowAl))
                }
                if e.icon != nil {
                        e.icon(screen, nx+28, ry+22, 8, MgText)
                }
                drawText(screen, e.label, nx+48, ry+14, medium(13), withAlphaF(MgText, rowAl))
                iconChevron(screen, nx+nw-18, ry+22, 4, withAlphaF(MgTextFaint, rowAl*0.6))
        }
}





func roundedCrop(src *ebiten.Image, outSize int, r float64) *ebiten.Image {
        out := ebiten.NewImage(outSize, outSize)
        b := src.Bounds()
        sw, sh := float64(b.Dx()), float64(b.Dy())
        sc := math.Max(float64(outSize)/sw, float64(outSize)/sh)
        dw, dh := sw*sc, sh*sc
        op := &ebiten.DrawImageOptions{}
        op.GeoM.Scale(sc, sc)
        op.GeoM.Translate((float64(outSize)-dw)/2, (float64(outSize)-dh)/2)
        out.DrawImage(src, op)
        pix := make([]byte, outSize*outSize*4)
        out.ReadPixels(pix)
        s := float64(outSize)
        for yy := 0; yy < outSize; yy++ {
                for xx := 0; xx < outSize; xx++ {
                        if !pointInRounded(float64(xx)+0.5, float64(yy)+0.5, s, r) {
                                i := (yy*outSize + xx) * 4
                                pix[i] = 0
                                pix[i+1] = 0
                                pix[i+2] = 0
                                pix[i+3] = 0
                        }
                }
        }
        out.ReplacePixels(pix)
        return out
}

func pointInRounded(px, py, s, r float64) bool {
        corners := [4][2]float64{{r, r}, {s - r, r}, {r, s - r}, {s - r, s - r}}
        for _, c := range corners {
                inX := (c[0] == r && px < r) || (c[0] == s-r && px > s-r)
                inY := (c[1] == r && py < r) || (c[1] == s-r && py > s-r)
                if inX && inY {
                        dx, dy := px-c[0], py-c[1]
                        return dx*dx+dy*dy <= r*r
                }
        }
        return true
}
