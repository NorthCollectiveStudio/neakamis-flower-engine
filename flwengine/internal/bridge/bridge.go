package bridge

import (
        "encoding/base64"
        "encoding/json"
        "fmt"
        "runtime"
        "sync"

        "flwengine/internal/nklog"
        "flwengine/internal/plat"
)

type Env struct {
        Log          *nklog.Writer
        GameDir      string
        OnExit       func()
        OnEvent      func(name string, data string)
        CloseGame    func()
        SetTitle     func(string)
        SetSize      func(w, h int, fullscreen bool)
        MoveWindow   func(x, y int)
        WindowPos    func() (int, int, bool)
        CenterWindow func(pos string)
        AlwaysOnTop  func(on bool)
        LowerWindow  func()
        ShowDialog   func(title, text string) bool
        TouchEnabled bool
        VFSRead      func(path string) ([]byte, error)
        VFSWrite     func(path string, data []byte) error
        VFSList      func(path string) ([]string, error)
        VFSExists    func(path string) bool
        VFSUnlink    func(path string) error
        SysInfo      *SysInfo
        HostWrite    func(path string, data []byte) error
        HostExists   func(path string) bool
        RPC          RPCClient
}

type RPCClient interface {
        Login(clientID string) bool
        SetActivity(activity json.RawMessage) bool
        Clear() bool
        Connected() bool
}

type Bridge struct {
        mu  sync.Mutex
        env *Env
        os  plat.OSInfo
}

type SysInfo struct {
        Platform string            `json:"platform"`
        Node     string            `json:"nodePlatform"`
        Env      map[string]string `json:"env"`
}

func New(env *Env) *Bridge {
        info, _ := plat.Detect()
        node := "linux"
        switch info.Platform {
        case "windows":
                node = "win32"
        case "darwin":
                node = "darwin"
        }
        env.SysInfo = &SysInfo{Platform: info.Platform, Node: node, Env: map[string]string{}}
        return &Bridge{env: env, os: info}
}

func (b *Bridge) SysInfo() *SysInfo {
        return b.env.SysInfo
}

type Call struct {
        Name string            `json:"n"`
        Args json.RawMessage   `json:"a"`
}

type Result struct {
        OK    bool   `json:"ok"`
        Value any    `json:"v,omitempty"`
        Error string `json:"e,omitempty"`
}

func (b *Bridge) Handle(name string, args json.RawMessage) Result {
        switch name {
        case "os":
                return ok(b.os)
        case "engine":
                return ok(map[string]any{
                        "name":      "flower engine",
                        "nuggetos":  true,
                        "platform":  b.os.Platform,
                        "touch":     b.env.TouchEnabled,
                })
        case "shell":
                if !plat.ShellSupported() {
                        return fail("shell not supported on this platform")
                }
                var p struct {
                        Cmd     string   `json:"cmd"`
                        Args    []string `json:"args"`
                        Dir     string   `json:"dir"`
                        Timeout int      `json:"timeout"`
                }
                if err := json.Unmarshal(args, &p); err != nil {
                        return fail("bad args")
                }
                if p.Cmd == "" {
                        return fail("empty command")
                }
                res := plat.ShellExec(p.Dir, p.Cmd, p.Args, p.Timeout)
                b.env.Log.Info("bridge.shell", "exec %s -> code %d", p.Cmd, res.Code)
                return ok(res)
        case "gdi":
                return b.handleGDI(args)
        case "shell.exec":
                var p struct {
                        Command string `json:"command"`
                }
                if err := json.Unmarshal(args, &p); err != nil {
                        return fail("bad args")
                }
                if p.Command == "" {
                        return fail("empty command")
                }
                if !plat.ShellSupported() {
                        return fail("shell not supported on this platform")
                }
                var bin string
                var binArgs []string
                if runtime.GOOS == "windows" {
                        bin, binArgs = "cmd", []string{"/C", p.Command}
                } else {
                        bin, binArgs = "sh", []string{"-c", p.Command}
                }
                res := plat.ShellExec("", bin, binArgs, 30)
                b.env.Log.Info("bridge.shell", "exec %.120s -> code %d", p.Command, res.Code)
                return ok(res)
        case "dialog":
                var p struct {
                        Title string `json:"title"`
                        Text  string `json:"text"`
                }
                if err := json.Unmarshal(args, &p); err != nil {
                        return fail("bad args")
                }
                if b.env.ShowDialog == nil {
                        return fail("dialog unavailable")
                }
                if !b.env.ShowDialog(p.Title, p.Text) {
                        return fail("dialog failed")
                }
                return ok(true)
        case "window.move":
                var p struct {
                        X int `json:"x"`
                        Y int `json:"y"`
                }
                if err := json.Unmarshal(args, &p); err != nil {
                        return fail("bad args")
                }
                if b.env.MoveWindow == nil {
                        return fail("window control unavailable")
                }
                b.env.MoveWindow(p.X, p.Y)
                return ok(true)
        case "window.pos":
                if b.env.WindowPos == nil {
                        return fail("window control unavailable")
                }
                x, y, known := b.env.WindowPos()
                return ok(map[string]any{"x": x, "y": y, "known": known})
        case "window.center":
                var p struct {
                        Pos string `json:"pos"`
                }
                json.Unmarshal(args, &p)
                if b.env.CenterWindow == nil {
                        return fail("window control unavailable")
                }
                b.env.CenterWindow(p.Pos)
                if b.env.WindowPos != nil {
                        x, y, _ := b.env.WindowPos()
                        return ok(map[string]any{"x": x, "y": y})
                }
                return ok(true)
        case "window.alwaysTop":
                var p struct {
                        On bool `json:"on"`
                }
                json.Unmarshal(args, &p)
                if b.env.AlwaysOnTop == nil {
                        return fail("window control unavailable")
                }
                b.env.AlwaysOnTop(p.On)
                return ok(true)
        case "window.lower":
                
                
                if b.env.LowerWindow == nil {
                        return fail("window control unavailable")
                }
                b.env.LowerWindow()
                return ok(true)
        case "log":
                var p struct {
                        Level int    `json:"level"`
                        Cat   string `json:"cat"`
                        Msg   string `json:"msg"`
                }
                json.Unmarshal(args, &p)
                b.env.Log.Log(p.Level, "game."+p.Cat, "%s", p.Msg)
                return ok(true)
        case "fs.read":
                var p struct{ Path string `json:"path"` }
                json.Unmarshal(args, &p)
                data, err := b.env.VFSRead(p.Path)
                if err != nil {
                        return fail(fmt.Sprintf("%v", err))
                }
                return ok(string(data))
        case "fs.write":
                var p struct {
                        Path    string `json:"path"`
                        Content string `json:"content"`
                }
                json.Unmarshal(args, &p)
                err := b.env.VFSWrite(p.Path, []byte(p.Content))
                if err != nil {
                        return fail(fmt.Sprintf("%v", err))
                }
                return ok(true)
        case "fs.list":
                var p struct{ Path string `json:"path"` }
                json.Unmarshal(args, &p)
                items, err := b.env.VFSList(p.Path)
                if err != nil {
                        return fail(fmt.Sprintf("%v", err))
                }
                return ok(items)
        case "fs.exists":
                var p struct{ Path string `json:"path"` }
                json.Unmarshal(args, &p)
                return ok(b.env.VFSExists(p.Path))
        case "fs.hostwrite":
                var p struct {
                        Path    string `json:"path"`
                        Content string `json:"content"`
                        B64     string `json:"b64"`
                }
                json.Unmarshal(args, &p)
                if p.Path == "" || b.env.HostWrite == nil {
                        return fail("host write unavailable")
                }
                var data []byte
                if p.B64 != "" {
                        data, _ = base64.StdEncoding.DecodeString(p.B64)
                } else {
                        data = []byte(p.Content)
                }
                if err := b.env.HostWrite(p.Path, data); err != nil {
                        return fail(fmt.Sprintf("%v", err))
                }
                return ok(true)
        case "fs.hostexists":
                var p struct{ Path string `json:"path"` }
                json.Unmarshal(args, &p)
                if b.env.HostExists == nil {
                        return ok(false)
                }
                return ok(b.env.HostExists(p.Path))
        case "sysinfo":
                return ok(b.env.SysInfo)
        case "rpc.login":
                var p struct{ ClientID string `json:"clientid"` }
                json.Unmarshal(args, &p)
                if b.env.RPC == nil {
                        return ok(false)
                }
                return ok(b.env.RPC.Login(p.ClientID))
        case "rpc.activity":
                var p struct{ Activity json.RawMessage `json:"activity"` }
                json.Unmarshal(args, &p)
                if b.env.RPC == nil {
                        return ok(false)
                }
                if !b.env.RPC.Connected() {
                        return ok(false)
                }
                return ok(b.env.RPC.SetActivity(p.Activity))
        case "rpc.clear":
                if b.env.RPC == nil {
                        return ok(true)
                }
                return ok(b.env.RPC.Clear())
        case "fs.unlink":
                var p struct{ Path string `json:"path"` }
                json.Unmarshal(args, &p)
                return ok(b.env.VFSUnlink(p.Path) == nil)
        case "window.title":
                var p struct{ Title string `json:"title"` }
                json.Unmarshal(args, &p)
                if b.env.SetTitle != nil {
                        b.env.SetTitle(p.Title)
                }
                return ok(true)
        case "window.size":
                var p struct {
                        W         int  `json:"w"`
                        H         int  `json:"h"`
                        Fullscreen bool `json:"fullscreen"`
                }
                json.Unmarshal(args, &p)
                if b.env.SetSize != nil {
                        b.env.SetSize(p.W, p.H, p.Fullscreen)
                }
                return ok(true)
        case "game.exit":
                if b.env.CloseGame != nil {
                        go b.env.CloseGame()
                }
                return ok(true)
        case "event":
                var p struct {
                        Name string `json:"name"`
                        Data string `json:"data"`
                }
                json.Unmarshal(args, &p)
                if b.env.OnEvent != nil {
                        b.env.OnEvent(p.Name, p.Data)
                }
                return ok(true)
        case "touch":
                return ok(b.env.TouchEnabled)
        default:
                return fail("unknown call: " + name)
        }
}

func ok(v any) Result {
        return Result{OK: true, Value: v}
}

func fail(msg string) Result {
        return Result{OK: false, Error: msg}
}
