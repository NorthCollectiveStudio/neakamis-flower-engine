package nuggetos









import (
        "fmt"
        "math"
        "strconv"
        "strings"
        "sync"
        "sync/atomic"

        "github.com/hajimehoshi/ebiten/v2"
        "github.com/hajimehoshi/ebiten/v2/inpututil"

        "flwengine/internal/supervisor"
)

const (
        dcColW   = 232.0
        dcColGap = 8.0
        dcItemH  = 28.0
        dcColX   = 40.0
        dcColY   = 150.0
        dcHdrH   = 26.0
)

type devItemKind int

const (
        devAction devItemKind = iota
        devSubmenu
        devExit
)

type devItem struct {
        name   string
        kind   devItemKind
        action string
        target string 
        value  int    
}

type devMenu struct {
        id    string
        title string
        items []devItem
}

type devLayer struct {
        menu *devMenu
        sel  int
}

type devModalKind int

const (
        devModalNone devModalKind = iota
        devModalInput
        devModalChoice
        devModalConfirm
)

type devModal struct {
        kind    devModalKind
        title   string
        value   string
        choices []string
        sel     int
        onOK    func(val string)
}

type devInfo struct {
        ok       bool
        mapID    string
        x, y     string
        gold     string
        playtime string
        noclip   bool
        noenc    bool
}

type devConsole struct {
        mu        sync.Mutex
        layers    []*devLayer
        modal     devModal
        info      devInfo
        infoTTL   float64
        models    []string
        t         float64
        pushT     float64 
        selPulse  float64 
        lastSel   int
        colOff    float64 
        modalT    float64 
        resultA   float64 
        opened    bool
        noData    bool
        freezeSet int32
        busy      int32 
        result    string
        resultTTL float64
        resultErr bool
        // chromeImg caches the static part of the console (background, info
        // panel frame, columns, labels) so it is not re-rasterized every
        // frame. chromeKey is the signature it was rendered for.
        chromeImg *ebiten.Image
        chromeKey string
}

func newDevConsole() *devConsole {
        return &devConsole{}
}





func dcMenus() map[string]*devMenu {
        return map[string]*devMenu{
                "main": {id: "main", title: "DEBUG", items: []devItem{
                        {name: "GAME", kind: devSubmenu, target: "game"},
                        {name: "PARTY", kind: devSubmenu, target: "party"},
                        {name: "INVENTORY", kind: devSubmenu, target: "inventory"},
                        {name: "MODEL", kind: devSubmenu, target: "model"},
                        {name: "FIELD & CHEATS", kind: devSubmenu, target: "field"},
                        {name: "EVENT & VARS", kind: devSubmenu, target: "event"},
                        {name: "TESTING TOOLS", kind: devSubmenu, target: "tools"},
                        {name: "EXIT", kind: devExit, action: "exit"},
                }},
                "game": {id: "game", title: "GAME", items: []devItem{
                        {name: "RECOVER PARTY", kind: devAction, action: "recover"},
                        {name: "LEVEL UP PARTY", kind: devAction, action: "levelup"},
                        {name: "GET MAX MONEY", kind: devAction, action: "money"},
                        {name: "SAVE GAME", kind: devAction, action: "call_save"},
                }},
                "party": {id: "party", title: "PARTY MGMT", items: []devItem{
                        {name: "ADD MEMBER", kind: devAction, action: "add_member"},
                        {name: "REMOVE MEMBER", kind: devAction, action: "remove_member"},
                }},
                "inventory": {id: "inventory", title: "INVENTORY", items: []devItem{
                        {name: "GET ALL ITEMS", kind: devAction, action: "all_items"},
                        {name: "GET ALL WEAPONS", kind: devAction, action: "all_weapons"},
                        {name: "GET ALL ARMORS", kind: devAction, action: "all_armors"},
                        {name: "CLEAR INVENTORY", kind: devAction, action: "clear_inv"},
                }},
                "field": {id: "field", title: "FIELD & CHEATS", items: []devItem{
                        {name: "MAP JUMP", kind: devAction, action: "map_jump"},
                        {name: "RELOAD MAP", kind: devAction, action: "reload_map"},
                        {name: "TOGGLE NOCLIP", kind: devAction, action: "noclip"},
                        {name: "TOGGLE ENCOUNTERS", kind: devAction, action: "encounters"},
                        {name: "PLAYER SPEED UP", kind: devAction, action: "speed_up"},
                        {name: "CHANGE WEATHER", kind: devAction, action: "weather"},
                }},
                "event": {id: "event", title: "EVENT & VARS", items: []devItem{
                        {name: "EDIT SWITCH", kind: devAction, action: "switch_edit"},
                        {name: "EDIT VARIABLE", kind: devAction, action: "var_edit"},
                        {name: "RUN COMMON EV", kind: devAction, action: "common_event"},
                }},
                "tools": {id: "tools", title: "TESTING TOOLS", items: []devItem{
                        {name: "RM DEBUG MENU", kind: devAction, action: "rm_debug"},
                        {name: "TRIGGER ERROR", kind: devAction, action: "force_error"},
                        {name: "JS CONSOLE EVAL", kind: devAction, action: "eval_js"},
                }},
        }
}





func (dc *devConsole) enter() {
        dc.mu.Lock()
        dc.layers = []*devLayer{{menu: dcMenus()["main"], sel: 0}}
        dc.modal = devModal{}
        dc.noData = false
        dc.result = ""
        dc.resultTTL = 0
        dc.pushT = 0 
        dc.colOff = 0
        dc.modalT = 0
        dc.resultA = 0
        dc.t = 0
        dc.mu.Unlock()
        dc.freeze(true)
        go dc.fetchInfo()
        go dc.fetchModels()
}



func (dc *devConsole) freeze(on bool) {
        if supervisor.M().State() != supervisor.StateRunning {
                return
        }
        js := "window.__NUGGET_FREEZE=false; window.__NUGGET_KEYHOOK=false; 'ok'"
        if on {
                
                
                
                js = "window.__NUGGET_FREEZE=true; window.__NUGGET_KEYHOOK=true; try{window.flwCall('window.lower',{})}catch(e){}; 'ok'"
        }
        go func() {
                _, _ = supervisor.M().RPCCall("eval", js)
        }()
        if on {
                atomic.StoreInt32(&dc.freezeSet, 1)
        } else {
                atomic.StoreInt32(&dc.freezeSet, 0)
        }
}

func (dc *devConsole) forceUnfreeze() {
        if atomic.LoadInt32(&dc.freezeSet) == 1 {
                dc.freeze(false)
        }
}

func (dc *devConsole) fetchInfo() {
        js := `(function(){
                if (typeof $gameMap === "undefined" || !$gameMap || !$gamePlayer) return "NOGAME";
                return $gameMap.mapId()+"|"+$gamePlayer.x+"|"+$gamePlayer.y+"|"+$gameParty.gold()+"|"+$gameSystem.playtimeText()+"|"+($gamePlayer._through?1:0)+"|"+($gameSystem.isEncounterEnabled()?0:1);
        })()`
        res, err := supervisor.M().RPCCall("eval", js)
        dc.mu.Lock()
        defer dc.mu.Unlock()
        if err != nil || res == "NOGAME" {
                dc.info = devInfo{}
                dc.noData = err == nil
                return
        }
        p := strings.Split(res, "|")
        if len(p) < 7 {
                dc.info = devInfo{}
                return
        }
        dc.info = devInfo{ok: true, mapID: p[0], x: p[1], y: p[2], gold: p[3], playtime: p[4],
                noclip: p[5] == "1", noenc: p[6] == "1"}
        dc.noData = false
}

func (dc *devConsole) fetchModels() {
        js := `(typeof $dataActors !== "undefined" && $dataActors) ? $dataActors.map(function(a, i){return a ? i + ":" + a.name : null}).filter(Boolean).join("\n") : ""`
        res, err := supervisor.M().RPCCall("eval", js)
        dc.mu.Lock()
        defer dc.mu.Unlock()
        dc.models = nil
        if err != nil || res == "" {
                return
        }
        dc.models = strings.Split(res, "\n")
}

func (dc *devConsole) modelItems() []devItem {
        var items []devItem
        for _, m := range dc.models {
                idx := 0
                name := m
                if i := strings.Index(m, ":"); i > 0 {
                        if v, err := strconv.Atoi(m[:i]); err == nil {
                                idx = v
                                name = m[i+1:]
                        }
                }
                items = append(items, devItem{name: name, kind: devAction, action: "change_model", value: idx})
        }
        return items
}








func (dc *devConsole) runAsync(label string, js string) {
        if atomic.LoadInt32(&dc.busy) == 1 {
                dc.mu.Lock()
                dc.result = "busy: " + label + " still running"
                dc.resultErr = true
                dc.resultTTL = 2
                dc.mu.Unlock()
                playSound(sndError)
                return
        }
        atomic.StoreInt32(&dc.busy, 1)
        dc.mu.Lock()
        dc.result = label + "..."
        dc.resultErr = false
        dc.resultTTL = 8
        dc.mu.Unlock()
        go func() {
                res, err := supervisor.M().RPCCall("eval", js)
                dc.mu.Lock()
                atomic.StoreInt32(&dc.busy, 0)
                if err != nil {
                        dc.result = "err: " + err.Error()
                        dc.resultErr = true
                        playSound(sndError)
                } else if res != "" {
                        dc.result = res
                        dc.resultErr = false
                }
                dc.resultTTL = 4
                dc.mu.Unlock()
                go dc.fetchInfo()
        }()
}





func (dc *devConsole) update(dt float64, a *app) {
        dc.mu.Lock()
        defer dc.mu.Unlock()
        dc.t += dt
        if dc.pushT < 1 {
                dc.pushT = math.Min(1, dc.pushT+dt/0.14)
        }
        if dc.selPulse > 0 {
                dc.selPulse -= dt
        }
        
        if l := dc.topLayer(); l != nil {
                vis := dcColVis()
                tgt := 0.0
                if n := len(l.menu.items); n > vis {
                        top := l.sel - vis + 1
                        if top < 0 {
                                top = 0
                        }
                        if top > n-vis {
                                top = n - vis
                        }
                        tgt = float64(top)
                }
                dc.colOff = damp(dc.colOff, tgt, 16, dt)
        } else {
                dc.colOff = damp(dc.colOff, 0, 16, dt)
        }
        
        mt := 0.0
        if dc.modal.kind != devModalNone {
                mt = 1
        }
        dc.modalT = damp(dc.modalT, mt, 15, dt)
        rt := 0.0
        if dc.resultTTL > 0 || atomic.LoadInt32(&dc.busy) == 1 {
                rt = 1
        }
        dc.resultA = damp(dc.resultA, rt, 14, dt)
        if dc.infoTTL > 0 {
                dc.infoTTL -= dt
                if dc.infoTTL <= 0 {
                        go dc.fetchInfo()
                        dc.infoTTL = 2.0
                }
        } else {
                dc.infoTTL = 2.0
        }
        if dc.resultTTL > 0 {
                dc.resultTTL -= dt
        }
        
        if inpututil.IsKeyJustPressed(ebiten.KeyF10) {
                playSound(sndBack)
                dc.mu.Unlock()
                a.closeDevConsole()
                dc.mu.Lock()
                return
        }
        
        if dc.modal.kind != devModalNone {
                dc.updateModal(a)
                return
        }
        dc.updateColumns(a)
}

func (dc *devConsole) updateColumns(a *app) {
        
        layer := dc.layers[len(dc.layers)-1]
        n := len(layer.menu.items)
        if n == 0 {
                if inpututil.IsKeyJustPressed(ebiten.KeyEscape) || inpututil.IsKeyJustPressed(ebiten.KeyLeft) {
                        dc.popLayer(a)
                }
                return
        }
        if layer.sel != dc.lastSel {
                dc.selPulse = 0.22
                dc.lastSel = layer.sel
        }
        if inpututil.IsKeyJustPressed(ebiten.KeyDown) {
                layer.sel = (layer.sel + 1) % n
                dc.lastSel = layer.sel
                dc.selPulse = 0.22
                playSound(sndMove)
        }
        if inpututil.IsKeyJustPressed(ebiten.KeyUp) {
                layer.sel = (layer.sel - 1 + n) % n
                dc.lastSel = layer.sel
                dc.selPulse = 0.22
                playSound(sndMove)
        }
        if inpututil.IsKeyJustPressed(ebiten.KeyRight) || inpututil.IsKeyJustPressed(ebiten.KeyEnter) || inpututil.IsKeyJustPressed(ebiten.KeySpace) {
                // activate() runs actions that lock dc.mu again (runAsync),
                // so the lock must be released around the call - a plain
                // sync.Mutex is not reentrant and the shell would freeze
                // forever on the UI thread.
                dc.mu.Unlock()
                dc.activate(layer.menu.items[layer.sel], a)
                dc.mu.Lock()
        }
        if inpututil.IsKeyJustPressed(ebiten.KeyLeft) || inpututil.IsKeyJustPressed(ebiten.KeyEscape) {
                dc.mu.Unlock()
                dc.popLayer(a)
                dc.mu.Lock()
        }
        dc.tapColumns(a)
}




func (dc *devConsole) pushKey(key string, a *app) {
        dc.mu.Lock()
        defer dc.mu.Unlock()
        switch key {
        case "ArrowDown":
                if dc.modal.kind != devModalNone {
                        if dc.modal.kind != devModalInput && dc.modal.sel < len(dc.modal.choices)-1 {
                                dc.modal.sel++
                                playSound(sndMove)
                        }
                        return
                }
                if l := dc.topLayer(); l != nil && len(l.menu.items) > 0 {
                        l.sel = (l.sel + 1) % len(l.menu.items)
                        dc.lastSel = l.sel
                        dc.selPulse = 0.22
                        playSound(sndMove)
                }
        case "ArrowUp":
                if dc.modal.kind != devModalNone {
                        if dc.modal.kind != devModalInput && dc.modal.sel > 0 {
                                dc.modal.sel--
                                playSound(sndMove)
                        }
                        return
                }
                if l := dc.topLayer(); l != nil && len(l.menu.items) > 0 {
                        l.sel = (l.sel - 1 + len(l.menu.items)) % len(l.menu.items)
                        dc.lastSel = l.sel
                        dc.selPulse = 0.22
                        playSound(sndMove)
                }
        case "ArrowRight", "Enter":
                if dc.modal.kind == devModalInput {
                        val := dc.modal.value
                        onOK := dc.modal.onOK
                        dc.modal = devModal{}
                        playSound(sndSelect)
                        dc.mu.Unlock()
                        onOK(val)
                        dc.mu.Lock()
                        return
                }
                if dc.modal.kind != devModalNone {
                        val := dc.modal.choices[dc.modal.sel]
                        onOK := dc.modal.onOK
                        dc.modal = devModal{}
                        playSound(sndSelect)
                        dc.mu.Unlock()
                        onOK(val)
                        dc.mu.Lock()
                        return
                }
                if l := dc.topLayer(); l != nil && len(l.menu.items) > 0 {
                        item := l.menu.items[l.sel]
                        dc.mu.Unlock()
                        dc.activate(item, a)
                        dc.mu.Lock()
                }
        case "ArrowLeft", "Escape":
                if dc.modal.kind != devModalNone {
                        dc.modal = devModal{}
                        playSound(sndBack)
                        return
                }
                dc.mu.Unlock()
                dc.popLayer(a)
                dc.mu.Lock()
        case "Backspace":
                if dc.modal.kind == devModalInput && len(dc.modal.value) > 0 {
                        dc.modal.value = dc.modal.value[:len(dc.modal.value)-1]
                }
        default:
                if len(key) == 1 && key[0] >= 32 && key[0] < 127 {
                        if dc.modal.kind == devModalInput && len(dc.modal.value) < 60 {
                                dc.modal.value += key
                                playSound(sndType)
                        }
                }
        }
}

func (dc *devConsole) topLayer() *devLayer {
        if len(dc.layers) == 0 {
                return nil
        }
        return dc.layers[len(dc.layers)-1]
}

func (dc *devConsole) popLayer(a *app) {
        if len(dc.layers) > 1 {
                dc.layers = dc.layers[:len(dc.layers)-1]
                dc.pushT = 0
                dc.lastSel = -1
                playSound(sndBack)
                return
        }
        playSound(sndBack)
        a.closeDevConsole()
}

func (dc *devConsole) activate(item devItem, a *app) {
        switch item.kind {
        case devExit:
                playSound(sndSelect)
                a.closeDevConsole()
        case devSubmenu:
                menu := dcMenus()[item.target]
                if item.target == "model" {
                        menu = &devMenu{id: "model", title: "MODEL LIST", items: dc.modelItems()}
                        if len(menu.items) == 0 {
                                dc.setResult("model list unavailable (no $dataActors)")
                                playSound(sndError)
                                return
                        }
                }
                playSound(sndSelect)
                dc.layers = append(dc.layers, &devLayer{menu: menu, sel: 0})
                dc.pushT = 0
                dc.lastSel = -1
        case devAction:
                playSound(sndSelect)
                dc.runAction(item, a)
        }
}


func (dc *devConsole) tapColumns(a *app) {
        px, py, ok := tapOrClick()
        if !ok {
                mv := hoverPointerMoved()
                mx, my := ebiten.CursorPosition()
                hx, hy := float64(mx), float64(my)
                if len(dc.layers) > 0 {
                        l := dc.layers[len(dc.layers)-1]
                        if idx, hit := dc.itemAt(hx, hy, l); hit {
                                _ = idx
                                uiCursor = curHand
                                if mv && l.sel != idx {
                                        l.sel = idx
                                        dc.lastSel = idx
                                        dc.selPulse = 0.22
                                }
                        }
                }
                return
        }
        for li := len(dc.layers) - 1; li >= 0; li-- {
                l := dc.layers[li]
                if idx, hit := dc.itemAt(px, py, l); hit {
                        if li == len(dc.layers)-1 && l.sel == idx {
                                // Same unlock-around as in updateColumns:
                                // activate() may lock dc.mu again.
                                dc.mu.Unlock()
                                dc.activate(l.menu.items[idx], a)
                                dc.mu.Lock()
                        } else {
                                l.sel = idx
                                dc.lastSel = idx
                                dc.selPulse = 0.22
                                playSound(sndMove)
                        }
                        return
                }
        }
}



func (dc *devConsole) itemAt(px, py float64, l *devLayer) (int, bool) {
        col := 0
        for i, lay := range dc.layers {
                if lay == l {
                        col = i
                        break
                }
        }
        x := dcColX + float64(col)*(dcColW+dcColGap)
        if px < x || px > x+dcColW || py < dcColY+dcHdrH {
                return 0, false
        }
        top := 0
        if col == len(dc.layers)-1 {
                top = dc.colScrollTarget(l)
        }
        idx := int((py-dcColY-dcHdrH)/dcItemH) + top
        if idx < 0 || idx >= len(l.menu.items) || idx >= top+dcColVis() {
                return 0, false
        }
        return idx, true
}


func dcColVis() int {
        h := float64(LogicalH) - dcColY - dcHdrH - 20
        n := int(h / dcItemH)
        if n < 4 {
                n = 4
        }
        return n
}


func (dc *devConsole) colScrollTarget(l *devLayer) int {
        vis := dcColVis()
        n := len(l.menu.items)
        if n <= vis {
                return 0
        }
        top := l.sel - vis + 1
        if top < 0 {
                top = 0
        }
        if top > n-vis {
                top = n - vis
        }
        return top
}

func tapOrClick() (float64, float64, bool) {
        if tx, ty, ok := tapPoint(); ok {
                return tx, ty, true
        }
        if inpututil.IsMouseButtonJustPressed(ebiten.MouseButtonLeft) {
                x, y := ebiten.CursorPosition()
                return float64(x), float64(y), true
        }
        return 0, 0, false
}





func (dc *devConsole) openInput(title, def string, onOK func(string)) {
        dc.modal = devModal{kind: devModalInput, title: title, value: def, onOK: onOK}
        playSound(sndSelect)
}

func (dc *devConsole) openChoice(title string, choices []string, onOK func(string)) {
        dc.modal = devModal{kind: devModalChoice, title: title, choices: choices, sel: 0, onOK: onOK}
        playSound(sndSelect)
}

func (dc *devConsole) openConfirm(title string, onOK func()) {
        dc.modal = devModal{kind: devModalConfirm, title: title, choices: []string{"yes", "no"}, sel: 1,
                onOK: func(val string) {
                        if val == "yes" {
                                onOK()
                        }
                }}
        playSound(sndSelect)
}

func (dc *devConsole) updateModal(a *app) {
        m := &dc.modal
        switch m.kind {
        case devModalInput:
                for _, r := range ebiten.InputChars() {
                        if r >= 32 && r < 127 && len(m.value) < 60 {
                                m.value += string(r)
                        }
                }
                if inpututil.IsKeyJustPressed(ebiten.KeyBackspace) && len(m.value) > 0 {
                        m.value = m.value[:len(m.value)-1]
                }
                if inpututil.IsKeyJustPressed(ebiten.KeyEnter) {
                        val := m.value
                        onOK := m.onOK
                        dc.modal = devModal{}
                        playSound(sndSelect)
                        // onOK may run an action that locks dc.mu again.
                        dc.mu.Unlock()
                        onOK(val)
                        dc.mu.Lock()
                        return
                }
                if inpututil.IsKeyJustPressed(ebiten.KeyEscape) {
                        dc.modal = devModal{}
                        playSound(sndBack)
                        return
                }
        case devModalChoice, devModalConfirm:
                if inpututil.IsKeyJustPressed(ebiten.KeyDown) {
                        m.sel = (m.sel + 1) % len(m.choices)
                        playSound(sndMove)
                }
                if inpututil.IsKeyJustPressed(ebiten.KeyUp) {
                        m.sel = (m.sel - 1 + len(m.choices)) % len(m.choices)
                        playSound(sndMove)
                }
                if inpututil.IsKeyJustPressed(ebiten.KeyEnter) || inpututil.IsKeyJustPressed(ebiten.KeySpace) {
                        val := m.choices[m.sel]
                        onOK := m.onOK
                        dc.modal = devModal{}
                        playSound(sndSelect)
                        dc.mu.Unlock()
                        onOK(val)
                        dc.mu.Lock()
                        return
                }
                if inpututil.IsKeyJustPressed(ebiten.KeyEscape) {
                        dc.modal = devModal{}
                        playSound(sndBack)
                        return
                }
        }
        
        px, py, ok := tapOrClick()
        if !ok {
                mx, my := ebiten.CursorPosition()
                for i := range m.choices {
                        cy := dcModalY + 70 + float64(i)*40
                        if inRect(float64(mx), float64(my), dcModalX+40, cy, dcModalW-80, 36) {
                                uiCursor = curHand
                                m.sel = i
                        }
                }
                return
        }
        if m.kind == devModalInput {
                
                if inRect(px, py, dcModalX+dcModalW-190, dcModalY+dcModalH-56, 88, 36) {
                        val := m.value
                        onOK := m.onOK
                        dc.modal = devModal{}
                        playSound(sndSelect)
                        dc.mu.Unlock()
                        onOK(val)
                        dc.mu.Lock()
                        return
                }
                if inRect(px, py, dcModalX+dcModalW-96, dcModalY+dcModalH-56, 88, 36) {
                        dc.modal = devModal{}
                        playSound(sndBack)
                        return
                }
                return
        }
        for i, c := range m.choices {
                cy := dcModalY + 70 + float64(i)*40
                if inRect(px, py, dcModalX+40, cy, dcModalW-80, 36) {
                        if m.sel == i {
                                val := c
                                onOK := m.onOK
                                dc.modal = devModal{}
                                playSound(sndSelect)
                                dc.mu.Unlock()
                                onOK(val)
                                dc.mu.Lock()
                        } else {
                                m.sel = i
                                playSound(sndMove)
                        }
                        return
                }
        }
}


func (dc *devConsole) pushInputChar(s string) {
        dc.mu.Lock()
        defer dc.mu.Unlock()
        if dc.modal.kind != devModalInput {
                return
        }
        switch s {
        case "Backspace":
                if len(dc.modal.value) > 0 {
                        dc.modal.value = dc.modal.value[:len(dc.modal.value)-1]
                }
        case "Enter":
                
        default:
                if len(s) == 1 && s[0] >= 32 && s[0] < 127 && len(dc.modal.value) < 60 {
                        dc.modal.value += s
                }
        }
}

const (
        dcModalX = float64(LogicalW)/2 - 250
        dcModalY = 250.0
        dcModalW = 500.0
        dcModalH = 190.0
)





func (dc *devConsole) setResult(s string) {
        dc.result = s
        dc.resultTTL = 4
        dc.resultErr = false
}

func (dc *devConsole) rpc(js string) (string, error) {
        return supervisor.M().RPCCall("eval", js)
}

func (dc *devConsole) runAction(item devItem, a *app) {
        switch item.action {
        case "exit":
                a.closeDevConsole()

        
        case "recover":
                dc.runAsync("recover", `$gameParty.members().forEach(function(a){a.recoverAll();a.clearStates()}); 'Party Fully Recovered!'`)
        case "levelup":
                dc.openInput("Сколько уровней добавить?", "1", func(val string) {
                        n, err := strconv.Atoi(strings.TrimSpace(val))
                        if err != nil || n <= 0 {
                                a.showWarn("invalid level count")
                                return
                        }
                        dc.runAsync(fmt.Sprintf("level up +%d", n), fmt.Sprintf(`$gameParty.members().forEach(function(a){a.changeLevel(a.level+%d,false)}); 'Party leveled up by %d!'`, n, n))
                })
        case "money":
                dc.runAsync("max money", `$gameParty.gainGold(9999999); 'gold: '+$gameParty.gold()`)
        case "call_save":
                a.closeDevConsole()
                go func() { _, _ = dc.rpc(`SceneManager.push(Scene_Save); 'ok'`) }()

        
        case "add_member":
                dc.openInput("ID Актера для добавления:", "1", func(val string) {
                        id, _ := strconv.Atoi(strings.TrimSpace(val))
                        dc.runAsync(fmt.Sprintf("add actor %d", id), fmt.Sprintf(`(function(){if(!$dataActors[%d])return 'no actor '+%d;$gameParty.addActor(%d);$gamePlayer.refresh();return 'actor %d added'})()`, id, id, id, id))
                })
        case "remove_member":
                dc.openInput("ID Актера для удаления:", "1", func(val string) {
                        id, _ := strconv.Atoi(strings.TrimSpace(val))
                        dc.runAsync(fmt.Sprintf("remove actor %d", id), fmt.Sprintf(`(function(){$gameParty.removeActor(%d);$gamePlayer.refresh();return 'actor %d removed'})()`, id, id))
                })

        
        case "all_items":
                dc.runAsync("all items", `$dataItems.forEach(function(i){if(i&&i.name)$gameParty.gainItem(i,99)}); 'All items added (x99)!'`)
        case "all_weapons":
                dc.runAsync("all weapons", `$dataWeapons.forEach(function(w){if(w&&w.name)$gameParty.gainItem(w,99)}); 'All weapons added (x99)!'`)
        case "all_armors":
                dc.runAsync("all armors", `$dataArmors.forEach(function(a){if(a&&a.name)$gameParty.gainItem(a,99)}); 'All armors added (x99)!'`)
        case "clear_inv":
                dc.openConfirm("Точно удалить ВСЕ предметы, оружие и броню?", func() {
                        dc.runAsync("clear inventory", `(function(){$gameParty.items().forEach(function(i){$gameParty.loseItem(i,$gameParty.numItems(i))});$gameParty.weapons().forEach(function(w){$gameParty.loseItem(w,$gameParty.numItems(w))});$gameParty.armors().forEach(function(a){$gameParty.loseItem(a,$gameParty.numItems(a))});return 'Инвентарь полностью очищен!'})()`)
                })

        
        case "change_model":
                
                
                
                
                
                
                dc.runAsync(fmt.Sprintf("model #%d", item.value), fmt.Sprintf(`(function(){var a=$dataActors[%d];if(!a)return 'no model';var L=$gameParty.leader();if(!L)return 'no party leader';L.setCharacterImage(a.characterName,a.characterIndex);try{if(a.faceName!==undefined)L.setFaceImage(a.faceName,a.faceIndex)}catch(e){};try{if(a.battlerName!==undefined)L.setBattlerImage(a.battlerName)}catch(e){};$gamePlayer.refresh();try{$gamePlayer.followers().forEach(function(f){f.refresh()})}catch(e){};try{$gameMap.requestRefresh()}catch(e){};return 'model: '+a.name})()`, item.value))

        
        case "map_jump":
                dc.openInput("MAP ID:", "1", func(mapStr string) {
                        m, err1 := strconv.Atoi(strings.TrimSpace(mapStr))
                        if err1 != nil {
                                a.showWarn("invalid map id")
                                return
                        }
                        dc.openInput("X Координата:", "0", func(xs string) {
                                x, err2 := strconv.Atoi(strings.TrimSpace(xs))
                                if err2 != nil {
                                        a.showWarn("invalid x")
                                        return
                                }
                                dc.openInput("Y Координата:", "0", func(ys string) {
                                        y, err3 := strconv.Atoi(strings.TrimSpace(ys))
                                        if err3 != nil {
                                                a.showWarn("invalid y")
                                                return
                                        }
                                        a.closeDevConsole()
                                        go func() {
                                                _, _ = dc.rpc(fmt.Sprintf(`$gamePlayer.reserveTransfer(%d,%d,%d); 'ok'`, m, x, y))
                                        }()
                                })
                        })
                })
        case "reload_map":
                a.closeDevConsole()
                go func() {
                        _, _ = dc.rpc(`$gamePlayer.reserveTransfer($gameMap.mapId(),$gamePlayer.x,$gamePlayer.y,$gamePlayer.direction(),2); 'ok'`)
                }()
        case "noclip":
                dc.runAsync("noclip", `$gamePlayer._through=!$gamePlayer._through; 'Прохождение сквозь стены: '+($gamePlayer._through?'ВКЛЮЧЕНО':'ОТКЛЮЧЕНО')`)
        case "encounters":
                dc.runAsync("encounters", `(function(){if($gameSystem.isEncounterEnabled()){$gameSystem.disableEncounter();return 'Случайные бои: ОТКЛЮЧЕНЫ'}$gameSystem.enableEncounter();return 'Случайные бои: ВКЛЮЧЕНЫ'})()`)
        case "speed_up":
                dc.runAsync("speed", `(function(){var s=$gamePlayer.moveSpeed()>5?4:6;$gamePlayer.setMoveSpeed(s);return 'Скорость перемещения изменена на: '+s})()`)
        case "weather":
                dc.openChoice("Тип погоды", []string{"none", "rain", "storm", "snow"}, func(w string) {
                        a.closeDevConsole()
                        go func() {
                                _, _ = dc.rpc(fmt.Sprintf(`$gameScreen.changeWeather(%q,5,0); 'ok'`, w))
                        }()
                })

        
        case "switch_edit":
                dc.openInput("ID Переключателя (Switch):", "1", func(val string) {
                        id, err := strconv.Atoi(strings.TrimSpace(val))
                        if err != nil || id <= 0 {
                                a.showWarn("invalid switch id")
                                return
                        }
                        go func() {
                                res, err := dc.rpc(fmt.Sprintf(`$gameSwitches.value(%d)`, id))
                                if err != nil {
                                        return
                                }
                                cur := res == "true"
                                dc.mu.Lock()
                                dc.openInput(fmt.Sprintf("Переключатель #%d сейчас %v. Изменить на %v?", id, cur, !cur), "yes", func(v string) {
                                        dc.runAsync(fmt.Sprintf("switch %d", id), fmt.Sprintf(`$gameSwitches.setValue(%d,%v); 'switch %d = %v'`, id, !cur, id, !cur))
                                })
                                dc.mu.Unlock()
                        }()
                })
        case "var_edit":
                dc.openInput("ID Переменной:", "1", func(val string) {
                        id, err := strconv.Atoi(strings.TrimSpace(val))
                        if err != nil || id <= 0 {
                                a.showWarn("invalid variable id")
                                return
                        }
                        go func() {
                                res, err := dc.rpc(fmt.Sprintf(`$gameVariables.value(%d)`, id))
                                if err != nil {
                                        return
                                }
                                dc.mu.Lock()
                                dc.openInput("Текущее значение: "+res+". Введите новое:", res, func(nv string) {
                                        n, err := strconv.Atoi(strings.TrimSpace(nv))
                                        if err != nil {
                                                a.showWarn("invalid value")
                                                return
                                        }
                                        dc.runAsync(fmt.Sprintf("var %d", id), fmt.Sprintf(`$gameVariables.setValue(%d,%d); 'var %d = %d'`, id, n, id, n))
                                })
                                dc.mu.Unlock()
                        }()
                })
        case "common_event":
                dc.openInput("ID Общего события:", "1", func(val string) {
                        id, err := strconv.Atoi(strings.TrimSpace(val))
                        if err != nil || id <= 0 {
                                a.showWarn("invalid event id")
                                return
                        }
                        a.closeDevConsole()
                        go func() {
                                _, _ = dc.rpc(fmt.Sprintf(`$gameTemp.reserveCommonEvent(%d); 'ok'`, id))
                        }()
                })

        
        case "rm_debug":
                a.closeDevConsole()
                go func() { _, _ = dc.rpc(`SceneManager.push(Scene_Debug); 'ok'`) }()
        case "force_error":
                a.closeDevConsole()
                go func() {
                        _, _ = dc.rpc(`SceneManager.catchException(new Error("СЕКС БОМБА, ПХАХАХАХХАХАХАХ.... а если без шуток то это тестовая ошибка")); 'ok'`)
                }()
        case "eval_js":
                dc.openInput("Введите JS код:", "console.log('Test');", func(code string) {
                        dc.runAsync("eval", code)
                })
        }
}





// The dev console used to redraw every panel, every separator and every
// label on every frame. On a software renderer (llvmpipe) that halved the
// frame rate of the whole shell (9 fps -> 4 fps), which is what the
// "dev console is laggy" reports were about.
//
// The console is static most of the time, so the chrome (background, info
// panel frame, column frames, headers, separators and the item labels) is
// rendered into a texture once and blitted afterwards. Only the few animated
// parts are drawn live:
//
//   - the live values of the info panel (MAP/X/Y/GOLD/PLAYTIME),
//   - the NOCLIP/NO ENC chips,
//   - the selection plate of the last column (it pulses),
//   - the result banner, the modal and the slide-in animation.
func (dc *devConsole) draw(screen *ebiten.Image) {
        dc.mu.Lock()
        defer dc.mu.Unlock()

        // While the last column slides in, everything is drawn live.
        if dc.pushT < 1 {
                dc.drawChrome(screen, 1.0, false)
                dc.drawOverlays(screen, 1.0)
        } else {
                if img := dc.chromeImage(); img != nil {
                        screen.DrawImage(img, &ebiten.DrawImageOptions{})
                } else {
                        dc.drawChrome(screen, 1.0, false)
                }
                dc.drawOverlays(screen, 1.0)
        }

        if dc.resultA > 0.01 {
                busy := atomic.LoadInt32(&dc.busy) == 1
                rl := dc.result
                if rl == "" && busy {
                        rl = "..."
                }
                if rl != "" {
                        rc := MgGreen
                        if dc.resultErr {
                                rc = MgRed
                        }
                        if busy {
                                rc = MgLight
                        }
                        fnt := monoFace(13)
                        rw, _ := measure(rl, fnt, 17)
                        cw := rw + 44
                        if busy {
                                cw += 8
                        }
                        e := easeOutCubic(dc.resultA)
                        cx := float64(LogicalW)/2 - cw/2
                        cy := float64(LogicalH) - 52.0 + (1-e)*16
                        fillRoundRectGradV(screen, cx, cy, cw, 32, 16, withAlphaF(RGBA{0x14, 0x16, 0x18, 0xf0}, e), withAlphaF(RGBA{0x0a, 0x0c, 0x0e, 0xf0}, e))
                        strokeRoundRect(screen, cx+0.5, cy+0.5, cw-1, 31, 15.5, 1, withAlphaF(RGBA{0x4a, 0x52, 0x58, 0xff}, e))
                        if busy {
                                drawSpinner(screen, cx+18, cy+16, 7, dc.t, rc)
                                drawText(screen, rl, cx+34, cy+9, fnt, withAlphaF(rc, e))
                        } else {
                                fillCircle(screen, cx+16, cy+16, 3, withAlphaF(rc, e))
                                drawText(screen, rl, cx+28, cy+9, fnt, withAlphaF(rc, e))
                        }
                }
        }

        if dc.modal.kind != devModalNone {
                dc.drawModal(screen)
        }
}

// chromeKey describes everything the cached chrome depends on.
func (dc *devConsole) chromeSignature() string {
        var b strings.Builder
        b.WriteString("v2")
        for _, l := range dc.layers {
                b.WriteString("|")
                b.WriteString(l.menu.title)
                b.WriteString(",sel")
                b.WriteString(strconv.Itoa(l.sel))
                for i := range l.menu.items {
                        it := &l.menu.items[i]
                        b.WriteString(";")
                        b.WriteString(it.name)
                        b.WriteString(",")
                        b.WriteString(strconv.Itoa(int(it.kind)))
                }
        }
        b.WriteString(",off")
        b.WriteString(strconv.Itoa(int(dc.colOff)))
        b.WriteString(",ok")
        if dc.info.ok {
                b.WriteString("1")
        } else {
                b.WriteString("0")
        }
        return b.String()
}

// chromeImage returns the cached chrome, rebuilding it when it changed.
func (dc *devConsole) chromeImage() *ebiten.Image {
        key := dc.chromeSignature()
        if dc.chromeImg != nil && dc.chromeKey == key {
                return dc.chromeImg
        }
        if dc.chromeImg != nil {
                dc.chromeImg.Deallocate()
        }
        img := ebiten.NewImage(LogicalW, LogicalH)
        dc.drawChrome(img, 1.0, true)
        dc.chromeImg = img
        dc.chromeKey = key
        return img
}

// drawChrome renders the static part of the console.
//
// skipSelectedPlate: when the chrome is rendered into the cache, the
// selected item of the last column is skipped (its plate and label are drawn
// live on top, because the plate pulses).
func (dc *devConsole) drawChrome(screen *ebiten.Image, al float64, cached bool) {
        fillRect(screen, 0, 0, LogicalW, LogicalH, RGBA{0x00, 0x00, 0x00, 0xd9})

        ix, iy := dcColX, 34.0
        iw, ih := 470.0, 96.0
        fillRoundRectGradV(screen, ix, iy, iw, ih, 6, RGBA{0x1a, 0x1c, 0x30, 0xe6}, RGBA{0x0c, 0x0e, 0x18, 0xe6})
        strokeRoundRect(screen, ix+0.5, iy+0.5, iw-1, ih-1, 5.5, 1, RGBA{0x55, 0x55, 0xaa, 0xff})
        drawInnerTopLine(screen, ix, iy, iw, 6, RGBA{0x8a, 0x8a, 0xcc, 0x30})
        drawText(screen, "[ DEV INFO ]", ix+12, iy+8, monoFace(12), RGBA{0xff, 0x55, 0x55, 0xff})
        if !dc.info.ok {
                drawText(screen, "no game data ($gameMap not ready — start gameplay first)", ix+12, iy+34, monoFace(12), RGBA{0xff, 0xaa, 0x00, 0xff})
        }

        kx := ix + 12.0
        drawKeycap(screen, kx, iy+66, 18, "F10", RGBA{0x2a, 0x2c, 0x34, 0xff})
        kx += keycapWidth("F10", 18) + 7
        drawText(screen, "close", kx, iy+71, monoFace(11), RGBA{0x77, 0x77, 0x77, 0xff})
        kx += 52
        drawKeycap(screen, kx, iy+66, 18, "←→↑↓", RGBA{0x2a, 0x2c, 0x34, 0xff})
        kx += keycapWidth("←→↑↓", 18) + 7
        drawText(screen, "navigate · enter activate", kx, iy+71, monoFace(11), RGBA{0x77, 0x77, 0x77, 0xff})

        for ci := range dc.layers {
                layer := dc.layers[ci]
                lastCol := ci == len(dc.layers)-1
                xoff := 0.0
                if lastCol {
                        xoff = (1 - easeOutCubic(dc.pushT)) * 22
                }
                x := dcColX + float64(ci)*(dcColW+dcColGap) + xoff
                nItems := len(layer.menu.items)

                visN := nItems
                if lastCol && visN > dcColVis() {
                        visN = dcColVis()
                }
                colH := dcHdrH + float64(visN)*dcItemH + 8

                fillRoundRectGradV(screen, x, dcColY, dcColW, colH, 6, withAlphaF(RGBA{0x24, 0x24, 0x26, 0xee}, al), withAlphaF(RGBA{0x14, 0x14, 0x16, 0xee}, al))
                strokeRoundRect(screen, x+0.5, dcColY+0.5, dcColW-1, colH-1, 5.5, 1, withAlphaF(MgTermBorder, al))

                hdrAcc := RGBA{0x55, 0x55, 0x66, 0xff}
                if lastCol {
                        hdrAcc = MgRed
                }
                fillRoundRectGradV(screen, x, dcColY, dcColW, dcHdrH, 6, withAlphaF(mixRGBA(hdrAcc, RGBA{0x00, 0x00, 0x00, 0xff}, 0.25), al), withAlphaF(mixRGBA(hdrAcc, RGBA{0x00, 0x00, 0x00, 0xff}, 0.55), al))
                fillRect(screen, x+1, dcColY+dcHdrH-6, dcColW-2, 6, withAlphaF(mixRGBA(hdrAcc, RGBA{0x00, 0x00, 0x00, 0xff}, 0.55), al))
                drawText(screen, layer.menu.title, x+12, dcColY+7, monoFace(12), withAlphaF(MgLight, al))
                drawTextRight(screen, fmt.Sprintf("%d", nItems), x+dcColW-12, dcColY+7, monoFace(11), withAlphaF(RGBA{0x99, 0x99, 0xaa, 0xff}, al))

                off := 0.0
                if lastCol {
                        off = dc.colOff
                }
                for i := range layer.menu.items {
                        item := &layer.menu.items[i]
                        y := dcColY + dcHdrH + (float64(i)-off)*dcItemH
                        if y < dcColY+dcHdrH-dcItemH || y > dcColY+colH-dcItemH {
                                continue
                        }
                        last := i == nItems-1
                        if !last {
                                fillRect(screen, x+1, y+dcItemH, dcColW-2, 1, withAlphaF(RGBA{0x33, 0x33, 0x33, 0xff}, al))
                        }
                        selected := i == layer.sel
                        if selected && lastCol {
                                // The plate pulses, so it is drawn live.
                                if !cached {
                                        dc.drawSelPlate(screen, x, y, al)
                                }
                                continue
                        }
                        c := MgLightDim
                        if !lastCol {
                                c = RGBA{0x8f, 0x93, 0x99, 0xff}
                        }
                        if selected && !lastCol {
                                c = MgLight
                        }
                        drawText(screen, clampText(item.name, monoFace(14), dcColW-34), x+12, y+7, monoFace(14), withAlphaF(c, al))
                        if item.kind == devSubmenu && lastCol {
                                iconChevron(screen, x+dcColW-16, y+dcItemH/2, 4, withAlphaF(RGBA{0x8f, 0x93, 0x99, 0xff}, 0.7*al))
                        }
                }

                if lastCol && nItems > visN {
                        railX := x + dcColW - 4
                        fillRect(screen, railX, dcColY+dcHdrH+4, 2, float64(visN)*dcItemH-12, withAlphaF(RGBA{0x33, 0x33, 0x38, 0xff}, al))
                        thumbH := float64(visN) * (float64(visN)*dcItemH - 12) / float64(nItems)
                        thumbY := dcColY + dcHdrH + 4 + (off/float64(nItems-visN))*(float64(visN)*dcItemH-12-thumbH)
                        fillRect(screen, railX, thumbY, 2, thumbH, withAlphaF(MgRed, al))
                }
        }
}

// drawSelPlate draws the selection plate of the last column.
func (dc *devConsole) drawSelPlate(screen *ebiten.Image, x, y, al float64) {
        pulse := 1.0
        if dc.selPulse > 0 {
                pulse = 0.7 + 0.3*(dc.selPulse/0.22)
        }
        fillRoundRectGradV(screen, x+3, y+1, dcColW-6, dcItemH-2, 4, withAlphaF(mixRGBA(MgRed, RGBA{0xff, 0xff, 0xff, 0xff}, 0.18*pulse), al), withAlphaF(mixRGBA(MgRed, RGBA{0x00, 0x00, 0x00, 0xff}, 0.28), al))
        drawGloss(screen, x+3, y+1, dcColW-6, dcItemH-2, 4, 0.5, 0.16*al)
}

// drawOverlays draws the parts of the console that change every frame.
func (dc *devConsole) drawOverlays(screen *ebiten.Image, al float64) {
        f := monoFace(14)
        ix, iy := dcColX, 34.0

        bx := ix + 128
        if dc.info.noclip {
                drawChip(screen, bx, iy+6, "NOCLIP", RGBA{0xff, 0x55, 0x55, 0xff}, true)
                bx += 78
        }
        if dc.info.noenc {
                drawChip(screen, bx, iy+6, "NO ENC", RGBA{0x55, 0xaa, 0xff, 0xff}, true)
        }
        if dc.info.ok {
                seg := func(s string, x float64, c RGBA) float64 {
                        drawText(screen, s, x, iy+34, f, c)
                        w, _ := measure(s, f, 18)
                        return x + w
                }
                x := ix + 12.0
                x = seg("MAP: ", x, MgLight)
                x = seg(dc.info.mapID, x, RGBA{0xff, 0xaa, 0x00, 0xff})
                x = seg("  |  X: ", x, MgLight)
                x = seg(dc.info.x, x, RGBA{0x55, 0xff, 0x55, 0xff})
                x = seg(" Y: ", x, MgLight)
                x = seg(dc.info.y, x, RGBA{0x55, 0xff, 0x55, 0xff})
                x = seg("  |  GOLD: ", x, MgLight)
                x = seg(dc.info.gold, x, MgLight)
                x = seg("  |  PLAYTIME: ", x, MgLight)
                x = seg(dc.info.playtime, x, MgLight)
        }

        // The selection plate of the last column pulses, so it is drawn live.
        if len(dc.layers) > 0 {
                ci := len(dc.layers) - 1
                layer := dc.layers[ci]
                if layer.sel >= 0 && layer.sel < len(layer.menu.items) {
                        off := dc.colOff
                        y := dcColY + dcHdrH + (float64(layer.sel)-off)*dcItemH
                        if y >= dcColY+dcHdrH-dcItemH && y <= dcColY+dcHdrH+float64(dcColVis())*dcItemH {
                                x := dcColX + float64(ci)*(dcColW+dcColGap) + (1-easeOutCubic(dc.pushT))*22
                                dc.drawSelPlate(screen, x, y, al)
                                item := &layer.menu.items[layer.sel]
                                drawText(screen, clampText(item.name, monoFace(14), dcColW-44), x+22, y+7, monoFace(14), withAlphaF(MgLight, al))
                                if item.kind == devSubmenu {
                                        iconChevron(screen, x+dcColW-16, y+dcItemH/2, 4, withAlphaF(MgLight, 0.7*al))
                                }
                        }
                }
        }
}

func (dc *devConsole) drawModal(screen *ebiten.Image) {
        m := &dc.modal
        fillRect(screen, 0, 0, LogicalW, LogicalH, withAlphaF(RGBA{0x00, 0x00, 0x00, 0x66}, dc.modalT))
        
        pop := easeOutBack(dc.modalT)
        mx := dcModalX + dcModalW/2
        my := dcModalY + dcModalH/2
        nw := dcModalW * (0.9 + 0.1*pop)
        nh := dcModalH * (0.9 + 0.1*pop)
        x := mx - nw/2
        y := my - nh/2 + (1-dc.modalT)*10
        dropShadow(screen, x, y, nw, nh)
        fillRoundRectGradV(screen, x, y, nw, nh, 10, withAlphaF(RGBA{0x26, 0x2a, 0x2e, 0xfa}, dc.modalT), withAlphaF(RGBA{0x0e, 0x10, 0x12, 0xfa}, dc.modalT))
        strokeRoundRect(screen, x+0.5, y+0.5, nw-1, nh-1, 9.5, 1, withAlphaF(MgBarBorder, dc.modalT))
        drawInnerTopLine(screen, x, y, nw, 10, withAlphaF(RGBA{0xff, 0xff, 0xff, 0x2c}, dc.modalT))
        
        fillRect(screen, x+24, y+24, 3, 14, withAlphaF(MgCyan, dc.modalT))
        drawText(screen, m.title, x+35, y+18, medium(15), withAlphaF(MgLight, dc.modalT))
        switch m.kind {
        case devModalInput:
                fx := x + 24
                fy := y + 58
                fw := nw - 48
                fillRoundRectGradV(screen, fx, fy, fw, 38, 8, RGBA{0x0a, 0x0c, 0x0e, 0xff}, RGBA{0x16, 0x18, 0x1a, 0xff})
                strokeRoundRect(screen, fx+0.5, fy+0.5, fw-1, 37, 7.5, 1, withAlphaF(MgCyan, dc.modalT))
                drawText(screen, clampText(m.value, monoFace(15), fw-40), fx+12, fy+10, monoFace(15), withAlphaF(MgLight, dc.modalT))
                if int(dc.t*2)%2 == 0 {
                        w, _ := measure(m.value, monoFace(15), 19)
                        if w < fw-24 {
                                fillRect(screen, fx+12+w+2, fy+8, 1.5, 20, withAlphaF(MgLight, dc.modalT))
                        }
                }
                
                drawButtonPill(screen, x+nw-190, y+nh-56, 88, 36, MgCyan, 0, 0)
                drawTextCenter(screen, "ok", x+nw-190+44, y+nh-47, medium(13), MgLight)
                strokeRoundRect(screen, x+nw-96+0.5, y+nh-56+0.5, 87, 35, 8, 1.5, withAlphaF(MgLightDim, dc.modalT))
                fillRoundRectGradV(screen, x+nw-96, y+nh-56, 88, 36, 9, withAlphaF(RGBA{0x33, 0x37, 0x3b, 0xff}, dc.modalT), withAlphaF(RGBA{0x1b, 0x1e, 0x21, 0xff}, dc.modalT))
                drawGloss(screen, x+nw-96, y+nh-56, 88, 36, 9, 0.5, 0.10)
                drawTextCenter(screen, "cancel", x+nw-96+44, y+nh-47, medium(13), withAlphaF(MgLightDim, dc.modalT))
                drawText(screen, "enter confirm · esc cancel", x+24, y+nh-46, regular(11), withAlphaF(MgLightFaint, dc.modalT))
        case devModalChoice, devModalConfirm:
                for i, c := range m.choices {
                        cy := y + 70 + float64(i)*40
                        if i == m.sel {
                                fillRoundRectGradV(screen, x+40, cy, nw-80, 36, 8, MgTileHoverTop, MgTileHover)
                                strokeRoundRect(screen, x+40.5, cy+0.5, nw-81, 35, 7.5, 1.5, MgTileHoverBd)
                        }
                        drawTextCenter(screen, c, x+nw/2, cy+9, medium(14), withAlphaF(MgText, dc.modalT))
                }
        }
}
