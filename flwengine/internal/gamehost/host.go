package gamehost

import (
        crand "crypto/rand"
        "context"
        "crypto/subtle"
        "encoding/base64"
        "encoding/json"
        "fmt"
        "io"
        "net"
        "net/http"
        "os"
        "path/filepath"
        "runtime"
        "strconv"
        "strings"
        "sync"
        "time"

        "flwengine/internal/bridge"
        "flwengine/internal/buildinfo"
        "flwengine/internal/nklog"
        "flwengine/internal/nkpkg"
        "flwengine/internal/plat"
)

type Host struct {
        mu         sync.Mutex
        GamePkg    *nkpkg.Package
        GameKey    []byte
        RuntimePkg *nkpkg.Package
        RuntimeKey []byte
        log        *nklog.Writer
        port       int
        token      string
        srv        *http.Server
        rpc        *rpcServer
        rpcPort    int
        rpcToken   string
        onReady    func()
        onExit     func(code int)
        onHung     func()
        mimeCache  map[string]string
        brg        *bridge.Bridge
        vfs        *VFS
        exitFlag   bool
        evalPending map[string]chan map[string]string
}

func New(log *nklog.Writer) *Host {
        h := &Host{
                log:        log,
                token:      randToken(24),
                rpcToken:   randToken(24),
                mimeCache: map[string]string{},
        }
        return h
}

func randToken(n int) string {
        b := make([]byte, n)
        io.ReadFull(crand.Reader, b)
        return base64.RawURLEncoding.EncodeToString(b)
}

func (h *Host) Port() int {
        return h.port
}

func (h *Host) Token() string {
        return h.token
}

func (h *Host) RPCPort() int {
        return h.rpcPort
}



func (h *Host) StartHTTPOn(port int) error {
        ln, err := net.Listen("tcp", "127.0.0.1:"+strconv.Itoa(port))
        if err != nil {
                return err
        }
        h.port = ln.Addr().(*net.TCPAddr).Port
        h.srv = &http.Server{Handler: h}
        go func() {
                if err := h.srv.Serve(ln); err != nil && err != http.ErrServerClosed {
                        h.log.Error("gamehost", "http server stopped: %v", err)
                }
        }()
        h.log.Info("gamehost", "http listening on 127.0.0.1:%d", h.port)
        return nil
}

func (h *Host) StartHTTP() error {
        ln, err := net.Listen("tcp", "127.0.0.1:0")
        if err != nil {
                return err
        }
        h.port = ln.Addr().(*net.TCPAddr).Port
        h.srv = &http.Server{Handler: h}
        go func() {
                if err := h.srv.Serve(ln); err != nil && err != http.ErrServerClosed {
                        h.log.Error("gamehost", "http server stopped: %v", err)
                }
        }()
        h.log.Info("gamehost", "http listening on 127.0.0.1:%d", h.port)
        return nil
}

func (h *Host) StartRPC() error {
        h.rpc = &rpcServer{host: h, token: h.token}
        return h.rpc.start()
}

func (h *Host) BaseURL() string {
        return fmt.Sprintf("http://127.0.0.1:%d", h.port)
}

func (h *Host) BootURL() string {
        return h.BaseURL() + "/boot?token=" + h.token
}

func (h *Host) authed(r *http.Request) bool {
        token := r.URL.Query().Get("token")
        if token == "" {
                c, err := r.Cookie("flwtoken")
                if err == nil {
                        token = c.Value
                }
        }
        return subtle.ConstantTimeCompare([]byte(token), []byte(h.token)) == 1
}

func (h *Host) ServeHTTP(w http.ResponseWriter, r *http.Request) {
        if !h.authed(r) {
                http.Error(w, "forbidden", http.StatusForbidden)
                return
        }
        path := r.URL.Path
        switch {
        case path == "/" || path == "/boot" || path == "/game":
                h.serveBoot(w, r)
        case path == "/bridge.js":
                h.serveBridge(w, r)
        case strings.HasPrefix(path, "/g/"):
                h.serveGameFile(w, r, strings.TrimPrefix(path, "/g/"))
        case strings.HasPrefix(path, "/flwfs/"):
                h.serveFS(w, r)
        case strings.HasPrefix(path, "/b/"):
                h.serveRuntimeFile(w, r, strings.TrimPrefix(path, "/b/"))
        default:
                http.NotFound(w, r)
        }
}

var (
        envOnce sync.Once
        envCached map[string]string
)

func (h *Host) EnvMap() map[string]string {
        envOnce.Do(func() {
                envCached = buildEnvMap()
        })
        return envCached
}

func buildEnvMap() map[string]string {
        m := map[string]string{}
        keys := []string{"HOME", "USER", "USERNAME", "LANG", "LC_ALL", "TMPDIR", "TEMP", "TMP",
                "XDG_RUNTIME_DIR", "XDG_DESKTOP_DIR", "XDG_DATA_HOME", "XDG_CONFIG_HOME",
                "APPDATA", "LOCALAPPDATA", "USERPROFILE", "COMPUTERNAME", "NUMBER_OF_PROCESSORS"}
        for _, k := range keys {
                if v := os.Getenv(k); v != "" {
                        m[k] = v
                }
        }
        if m["XDG_DESKTOP_DIR"] == "" {
                if v := xdgDesktopFromUserDirs(); v != "" {
                        m["XDG_DESKTOP_DIR"] = v
                }
        }
        switch runtime.GOOS {
        case "windows":
                m["FLW_NODE_PLATFORM"] = "win32"
                m["FLW_OS_NAME"] = "windows"
        case "darwin":
                m["FLW_NODE_PLATFORM"] = "darwin"
                m["FLW_OS_NAME"] = "macos"
        case "android":
                m["FLW_NODE_PLATFORM"] = "linux"
                m["FLW_OS_NAME"] = "android"
        default:
                m["FLW_NODE_PLATFORM"] = "linux"
                m["FLW_OS_NAME"] = "linux"
        }
        if info, err := plat.Detect(); err == nil {
                m["FLW_OS_VERSION"] = info.PlatformVer
                m["FLW_OS_PLATFORM"] = info.Platform
        }
        m["FLW_PID"] = strconv.Itoa(os.Getpid())
        m["DESKTOP_DIR"] = DesktopDir()
        return m
}

func (h *Host) serveBoot(w http.ResponseWriter, r *http.Request) {
        http.SetCookie(w, &http.Cookie{Name: "flwtoken", Value: h.token, Path: "/", HttpOnly: false, SameSite: http.SameSiteLaxMode})
        raw, err := h.GamePkg.ReadFile("index.html", h.GameKey)
        if err != nil {
                http.Error(w, "game index missing", http.StatusInternalServerError)
                return
        }
        html := string(raw)
        // The base href has to be injected BEFORE the elements that use
        // relative URLs. The game page has <link rel="stylesheet"
        // href="css/game.css"> in the head: with the base tag inserted at
        // </head> the stylesheet was resolved against the document root
        // ("/css/game.css") and answered with 404, so the game ran without
        // its stylesheet.
        inject := `<base href="/g/">`
        idx := strings.Index(strings.ToLower(html), "<head>")
        if idx < 0 {
                idx = strings.Index(strings.ToLower(html), "<head ")
        }
        if idx < 0 {
                idx = strings.Index(html, "</head>")
        }
        if idx >= 0 {
                if lt := strings.Index(html[:idx], "<head"); lt >= 0 {
                        close := strings.Index(html[lt:], ">")
                        if close >= 0 {
                                idx = lt + close + 1
                        }
                }
                envJSON, _ := json.Marshal(h.EnvMap())
                devFlag := "false"
                if buildinfo.ParseFeatures().DevComponents {
                        devFlag = "true"
                }
                head := `<script>window.__FLWENV=` + string(envJSON) + `;window.__FLWDEV=` + devFlag + `;</script>`
                if runtime.GOOS == "android" {
                        head += `<script>window.flwNativeCall=function(n,a){try{return window.flwNativeAndroid.call(n,String(a));}catch(e){return '{"ok":false,"e":"native bridge unavailable"}';}};</script>`
                }
                head += `<script src="/bridge.js?token=` + h.token + `"></script>`
                html = html[:idx] + inject + head + html[idx:]
        }
        w.Header().Set("Content-Type", "text/html; charset=utf-8")
        w.Header().Set("Cache-Control", "no-store")
        io.WriteString(w, html)
}

func (h *Host) serveGameFile(w http.ResponseWriter, r *http.Request, name string) {
        name = nkpkg.Clean(strings.TrimPrefix(name, "g/"))
        if name == "" {
                http.NotFound(w, r)
                return
        }
        if !h.GamePkg.Has(name) {
                http.NotFound(w, r)
                return
        }
        raw, err := h.GamePkg.ReadFile(name, h.GameKey)
        if err != nil {
                http.Error(w, "read error", http.StatusInternalServerError)
                return
        }
        h.writeMime(w, name, raw)
}

func (h *Host) serveRuntimeFile(w http.ResponseWriter, r *http.Request, name string) {
        name = nkpkg.Clean(name)
        if h.RuntimePkg != nil && h.RuntimePkg.Has(name) {
                raw, err := h.RuntimePkg.ReadFile(name, h.RuntimeKey)
                if err == nil {
                        h.writeMime(w, name, raw)
                        return
                }
        }
        http.NotFound(w, r)
}

func (h *Host) serveBridge(w http.ResponseWriter, r *http.Request) {
        w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
        w.Header().Set("Cache-Control", "no-store")
        if buildinfo.ParseFeatures().Encryption {
                // In a production build the bridge is only in the runtime
                // package: it is not part of the engine binary at all. The
                // package is encrypted and signed, so the source of the
                // bridge cannot be read out of the build.
                if h.RuntimePkg != nil && h.RuntimePkg.Has("browser/flwbridge.js") {
                        if raw, err := h.RuntimePkg.ReadFile("browser/flwbridge.js", h.RuntimeKey); err == nil {
                                w.Write(raw)
                                return
                        }
                }
                h.log.Crit("boot", "the bridge is missing: the runtime package has no browser/flwbridge.js")
                http.Error(w, "bridge missing", http.StatusInternalServerError)
                return
        }
        js := h.BridgeJS()
        if js == "" {
                h.log.Crit("boot", "the bridge is missing")
                http.Error(w, "bridge missing", http.StatusInternalServerError)
                return
        }
        io.WriteString(w, js)
}

func (h *Host) writeMime(w http.ResponseWriter, name string, raw []byte) {
        w.Header().Set("Content-Type", h.mimeOf(name))
        w.Header().Set("Content-Length", strconv.Itoa(len(raw)))
        w.WriteHeader(http.StatusOK)
        w.Write(raw)
}

func (h *Host) mimeOf(name string) string {
        ext := strings.ToLower(filepath.Ext(name))
        switch ext {
        case ".html", ".htm":
                return "text/html; charset=utf-8"
        case ".js", ".mjs":
                return "application/javascript; charset=utf-8"
        case ".css":
                return "text/css; charset=utf-8"
        case ".json":
                return "application/json; charset=utf-8"
        case ".png":
                return "image/png"
        case ".jpg", ".jpeg":
                return "image/jpeg"
        case ".gif":
                return "image/gif"
        case ".webp":
                return "image/webp"
        case ".svg":
                return "image/svg+xml"
        case ".ico":
                return "image/x-icon"
        case ".ogg", ".oga":
                return "audio/ogg"
        case ".m4a":
                return "audio/mp4"
        case ".mp3":
                return "audio/mpeg"
        case ".wav":
                return "audio/wav"
        case ".webm":
                return "video/webm"
        case ".mp4":
                return "video/mp4"
        case ".wasm":
                return "application/wasm"
        case ".woff":
                return "font/woff"
        case ".woff2":
                return "font/woff2"
        case ".ttf":
                return "font/ttf"
        case ".otf":
                return "font/otf"
        case ".txt", ".rdcat":
                return "text/plain; charset=utf-8"
        case ".csv":
                return "text/csv; charset=utf-8"
        default:
                return "application/octet-stream"
        }
}

func (h *Host) Stop() {
        ctx, cancel := context.WithTimeout(context.Background(), time.Second)
        defer cancel()
        h.srv.Shutdown(ctx)
        if h.rpc != nil {
                h.rpc.stop()
        }
}

type RPCMessage struct {
        Cmd  string `json:"cmd"`
        JS   string `json:"js,omitempty"`
        Name string `json:"name,omitempty"`
}

type RPCResponse struct {
        OK     bool   `json:"ok"`
        Result string `json:"result,omitempty"`
        Error  string `json:"error,omitempty"`
}

type rpcServer struct {
        host  *Host
        token string
        ln    net.Listener
        mu    sync.Mutex
        conns map[net.Conn]bool
}

// rpcIdleTimeout is how long an RPC connection may stay silent before it is
// closed. Without it a half-open connection leaks a goroutine and a socket.
const rpcIdleTimeout = 10 * time.Minute

func (s *rpcServer) start() error {
        ln, err := net.Listen("tcp", "127.0.0.1:0")
        if err != nil {
                return err
        }
        s.ln = ln
        s.conns = map[net.Conn]bool{}
        s.host.rpcPort = ln.Addr().(*net.TCPAddr).Port
        go s.accept()
        return nil
}

func (s *rpcServer) accept() {
        for {
                c, err := s.ln.Accept()
                if err != nil {
                        return
                }
                s.mu.Lock()
                s.conns[c] = true
                s.mu.Unlock()
                go s.handle(c)
        }
}

func (s *rpcServer) handle(c net.Conn) {
        defer func() {
                s.mu.Lock()
                delete(s.conns, c)
                s.mu.Unlock()
                c.Close()
        }()
        dec := json.NewDecoder(c)
        enc := json.NewEncoder(c)
        var handshake RPCMessage
        if err := dec.Decode(&handshake); err != nil {
                return
        }
        if handshake.Cmd != "auth" || handshake.Name != s.token {
                enc.Encode(RPCResponse{OK: false, Error: "auth failed"})
                return
        }
        enc.Encode(RPCResponse{OK: true})
        for {
                // Close idle connections instead of blocking in Decode
                // forever (a leaked goroutine and socket per connection).
                c.SetReadDeadline(time.Now().Add(rpcIdleTimeout))
                var msg RPCMessage
                if err := dec.Decode(&msg); err != nil {
                        return
                }
                resp := s.dispatch(msg)
                if err := enc.Encode(resp); err != nil {
                        return
                }
        }
}

func (s *rpcServer) dispatch(msg RPCMessage) RPCResponse {
        h := s.host
        prodLock := buildinfo.IsProd()
        switch msg.Cmd {
        case "ping":
                return RPCResponse{OK: true, Result: "pong"}
        case "evalfire":
                if prodLock {
                        return RPCResponse{OK: false, Error: "disabled"}
                }
                h.EvalJS(msg.JS)
                return RPCResponse{OK: true, Result: "fired"}
        case "eval":
                if prodLock {
                        return RPCResponse{OK: false, Error: "disabled"}
                }
                if msg.JS == "" {
                        return RPCResponse{OK: false, Error: "empty js"}
                }
                res, err := h.EvalResult(msg.JS)
                if err != nil {
                        return RPCResponse{OK: false, Error: err.Error()}
                }
                return RPCResponse{OK: true, Result: res}
        case "info":
                if prodLock {
                        return RPCResponse{OK: false, Error: "disabled"}
                }
                out, _ := json.Marshal(map[string]any{
                        "port":  h.port,
                        "rpc":   h.rpcPort,
                        "files": len(h.GamePkg.Files),
                })
                return RPCResponse{OK: true, Result: string(out)}
        case "close":
                go func() {
                        time.Sleep(150 * time.Millisecond)
                        h.RequestExit()
                }()
                return RPCResponse{OK: true}
        default:
                return RPCResponse{OK: false, Error: "unknown cmd"}
        }
}

func (s *rpcServer) stop() {
        if s.ln != nil {
                s.ln.Close()
        }
        s.mu.Lock()
        for c := range s.conns {
                c.Close()
        }
        s.mu.Unlock()
}

func (h *Host) Log() *nklog.Writer {
        return h.log
}

func (h *Host) SetVFS(v *VFS) {
        h.mu.Lock()
        h.vfs = v
        h.mu.Unlock()
}

func (h *Host) SetBridge(b *bridge.Bridge) {
        h.mu.Lock()
        h.brg = b
        h.mu.Unlock()
}

func LoadPackage(path string, key []byte) (*nkpkg.Package, error) {
        return nkpkg.Open(path, key)
}
