package supervisor

import (
        "bufio"
        "encoding/json"
        "fmt"
        "io"
        "net"
        "net/http"
        "os"
        "os/exec"
        "path/filepath"
        "runtime"
        "strconv"
        "strings"
        "sync"
        "time"

        "flwengine/internal/buildinfo"
        "flwengine/internal/nklog"
        "flwengine/internal/nkpkg"
)

type GameState int

const (
        StateIdle GameState = iota
        StateLaunching
        StateRunning
        StateStopping
)

type GameHandle struct {
        Port  int    `json:"port"`
        RPC   int    `json:"rpc"`
        Token string `json:"token"`
        PID   int    `json:"pid"`
}

type Settings struct {
        DisplayMode   string  `json:"displayMode"`
        Resolution    string  `json:"resolution"`
        Volume        float64 `json:"volume"`
        UISounds      bool    `json:"uiSounds"`
        ShowFPS       bool    `json:"showFps"`
        HotReloadOn   bool    `json:"hotreloadOn"`
        HotReloadIP   string  `json:"hotreloadIp"`
        HotReloadPort int     `json:"hotreloadPort"`
        Workspace     int     `json:"workspace"`
}

func defaultSettings() Settings {
        return Settings{
                DisplayMode:   "windowed",
                Resolution:    "1280x720",
                Volume:        0.8,
                UISounds:      true,
                HotReloadOn:   false,
                HotReloadIP:   "127.0.0.1",
                HotReloadPort: 7777,
                Workspace:     0,
        }
}

type Manager struct {
        mu         sync.Mutex
        dataDir    string
        logger     *nklog.Writer
        state      GameState
        handle     *GameHandle
        cmd        *exec.Cmd
        ctrlLn     net.Listener
        hrLn       net.Listener
        hrAddr     string
        settings   Settings
        onEvent    func(name, data string)
        onExit     func()
        onReload   func(note string)
        pkgKey     []byte
        metaCache  GameMeta
        metaCacheKey string
        plainOK    bool
        plainBroken error
        exitNotified bool
        stderrTail   *stderrTail
        launchID     uint64
        curTitle     string
}

var std *Manager
var stdOnce sync.Once
var presetDataDir string

// ctrlIdleTimeout is how long a control connection may stay silent before it
// is closed. Without it every half-open connection leaks a goroutine.
const ctrlIdleTimeout = 10 * time.Minute

func SetDataDir(dir string) {
        presetDataDir = dir
}

func Init(dataDir string, logger *nklog.Writer) *Manager {
        stdOnce.Do(func() {
                if presetDataDir != "" {
                        dataDir = presetDataDir
                }
                std = &Manager{
                        dataDir:  dataDir,
                        logger:   logger,
                        settings: defaultSettings(),
                        hrAddr:   "127.0.0.1:7777",
                }
                std.loadSettings()
                if err := std.verifyPlainAtBoot(); err != nil {
                        std.plainBroken = err
                } else {
                        std.plainOK = true
                }
        })
        return std
}

func M() *Manager {
        if std == nil {
                return Init("flwdata", nil)
        }
        return std
}

func (m *Manager) Settings() Settings {
        m.mu.Lock()
        defer m.mu.Unlock()
        return m.settings
}

func (m *Manager) UpdateSettings(f func(*Settings)) {
        m.mu.Lock()
        f(&m.settings)
        st := m.settings
        m.mu.Unlock()
        m.saveSettings(st)
}

func (m *Manager) settingsPath() string {
        return filepath.Join(m.dataDir, "settings.json")
}

func (m *Manager) loadSettings() {
        raw, err := os.ReadFile(m.settingsPath())
        if err != nil {
                return
        }
        var st Settings
        if json.Unmarshal(raw, &st) == nil {
                m.settings = st
        }
}

func (m *Manager) saveSettings(st Settings) {
        raw, _ := json.MarshalIndent(st, "", "  ")
        os.MkdirAll(m.dataDir, 0o755)
        os.WriteFile(m.settingsPath(), raw, 0o644)
}

func (m *Manager) DataDir() string {
        return m.dataDir
}

func (m *Manager) SaveDir() string {
        return filepath.Join(m.dataDir, "save")
}

func (m *Manager) PackagesDir() string {
        return filepath.Join(m.dataDir, "packages")
}

func (m *Manager) FindGamePackage() (string, []byte) {
        candidates := []struct {
                path string
                key  bool
        }{
                {filepath.Join(m.PackagesDir(), "game.nkpkg"), false},
                {filepath.Join(m.PackagesDir(), "prodgame.nkpkgenc"), true},
                {filepath.Join("packages", "game.nkpkg"), false},
                {filepath.Join("packages", "prodgame.nkpkgenc"), true},
        }
        for _, c := range candidates {
                if _, err := os.Stat(c.path); err == nil {
                        var key []byte
                        if c.key {
                                key = m.packageKey()
                        }
                        return c.path, key
                }
        }
        return "", nil
}

func (m *Manager) FindRuntimePackage() (string, []byte) {
        candidates := []struct {
                path string
                key  bool
        }{
                {filepath.Join(m.PackagesDir(), "runtime.nkpkg"), false},
                {filepath.Join(m.PackagesDir(), "runtime.nkpkgenc"), true},
                {filepath.Join("packages", "runtime.nkpkg"), false},
                {filepath.Join("packages", "runtime.nkpkgenc"), true},
                // On android the packages are copied next to the data
                // directory, not into a packages sub directory.
                {filepath.Join(m.dataDir, "runtime.nkpkg"), false},
                {filepath.Join(m.dataDir, "runtime.nkpkgenc"), true},
        }
        for _, c := range candidates {
                if _, err := os.Stat(c.path); err == nil {
                        var key []byte
                        if c.key {
                                key = m.packageKey()
                        }
                        return c.path, key
                }
        }
        return "", nil
}

func (m *Manager) packageKey() []byte {
        return m.pkgKey
}

func (m *Manager) SetPackageKey(k []byte) {
        m.mu.Lock()
        m.pkgKey = k
        m.mu.Unlock()
}

func (m *Manager) State() GameState {
        m.mu.Lock()
        defer m.mu.Unlock()
        return m.state
}

func (m *Manager) Handle() *GameHandle {
        m.mu.Lock()
        defer m.mu.Unlock()
        return m.handle
}

func (m *Manager) SetEventCallbacks(onEvent func(name, data string), onExit func(), onReload func(string)) {
        m.mu.Lock()
        m.onEvent = onEvent
        m.onExit = onExit
        m.onReload = onReload
        m.mu.Unlock()
}

func (m *Manager) StartCtrl() error {
        ln, err := net.Listen("tcp", "127.0.0.1:0")
        if err != nil {
                return err
        }
        m.mu.Lock()
        m.ctrlLn = ln
        m.mu.Unlock()
        go m.acceptCtrl(ln)
        if m.logger != nil {
                m.logger.Info("supervisor", "ctrl listening on %s", ln.Addr().String())
        }
        return nil
}

func (m *Manager) CtrlAddr() string {
        m.mu.Lock()
        defer m.mu.Unlock()
        if m.ctrlLn == nil {
                return ""
        }
        return m.ctrlLn.Addr().String()
}

func (m *Manager) acceptCtrl(ln net.Listener) {
        for {
                c, err := ln.Accept()
                if err != nil {
                        return
                }
                go m.handleCtrlConn(c)
        }
}

// notifyExitOnce reports the exit of a game process exactly once.
//
// The exit belongs to a specific launch: the hot reload flow kills the old
// game and starts a new one before the old process has actually died. The
// old process's exit must not clear the NEW game's handle/state, so the
// launch id is compared before anything is reset.
func (m *Manager) notifyExitOnce(id uint64) {
        m.mu.Lock()
        if m.launchID != id {
                // A newer launch is already running: this exit belongs to an
                // older process and must not touch the current state.
                m.mu.Unlock()
                if m.logger != nil {
                        m.logger.Info("supervisor", "stale game exit ignored (launch %d, current %d)", id, m.launchID)
                }
                return
        }
        if m.exitNotified {
                m.mu.Unlock()
                return
        }
        m.exitNotified = true
        m.state = StateIdle
        m.handle = nil
        cb := m.onExit
        m.mu.Unlock()
        if cb != nil {
                cb()
        }
}

func (m *Manager) handleCtrlConn(c net.Conn) {
        defer c.Close()
        dec := json.NewDecoder(c)
        for {
                // An idle connection would block in Decode forever and leak a
                // goroutine and a socket. The gamehost keeps the connection
                // open for the whole session, so the deadline is generous and
                // is refreshed on every message.
                c.SetReadDeadline(time.Now().Add(ctrlIdleTimeout))
                var ev struct {
                        Type string `json:"type"`
                        Name string `json:"name"`
                        Data string `json:"data"`
                }
                if err := dec.Decode(&ev); err != nil {
                        return
                }
                switch ev.Type {
                case "event":
                        m.mu.Lock()
                        cb := m.onEvent
                        m.mu.Unlock()
                        if cb != nil {
                                cb(ev.Name, ev.Data)
                        }
                case "exit":
                        if m.logger != nil {
                                m.logger.Info("supervisor", "game exited via ctrl")
                        }
                        m.mu.Lock()
                        id := m.launchID
                        m.mu.Unlock()
                        m.notifyExitOnce(id)
                        return
                }
        }
}

func (m *Manager) PlainStatus() (bool, error) {
        m.mu.Lock()
        defer m.mu.Unlock()
        return m.plainOK, m.plainBroken
}

func (m *Manager) LaunchGame() error {
        pkgPath, key := m.FindGamePackage()
        if pkgPath == "" {
                return fmt.Errorf("no game package found")
        }
        title := "The Rosemary Field"
        if meta := m.GameMeta(); meta.Found && meta.Title != "" {
                title = meta.Title
        }
        return m.launch(pkgPath, key, title)
}



func (m *Manager) LaunchPackage(pkgName, defTitle string) error {
        var path string
        for _, c := range []string{filepath.Join(m.PackagesDir(), pkgName), filepath.Join("packages", pkgName)} {
                if _, err := os.Stat(c); err == nil {
                        path = c
                        break
                }
        }
        if path == "" {
                return fmt.Errorf("package not found: %s", pkgName)
        }
        title := defTitle
        if p, err := nkpkg.Open(path, nil); err == nil && p.Manifest.Title != "" {
                title = p.Manifest.Title
        }
        return m.launch(path, nil, title)
}


func (m *Manager) CurrentTitle() string {
        m.mu.Lock()
        defer m.mu.Unlock()
        return m.curTitle
}


func (m *Manager) PackageExists(pkgName string) bool {
        for _, c := range []string{filepath.Join(m.PackagesDir(), pkgName), filepath.Join("packages", pkgName)} {
                if _, err := os.Stat(c); err == nil {
                        return true
                }
        }
        return false
}

// launchCandidates returns the executables to try, in order.
//
// Normally the game host is used; if it cannot be started, the engine
// starts itself in game host mode. As a last resort a copy of the engine is
// executed from the temporary directory: some locations (cloud drive
// placeholders, folders with a mark of the web) refuse to execute files
// although the file itself is fine.
func (m *Manager) launchCandidates(exe, selfExe string) []string {
        cands := []string{}
        if exe != "" {
                cands = append(cands, exe)
        }
        if selfExe != "" && selfExe != exe {
                cands = append(cands, selfExe)
        }
        return cands
}

func (m *Manager) launch(pkgPath string, key []byte, title string) error {
        m.mu.Lock()
        if m.state == StateRunning || m.state == StateLaunching || m.state == StateStopping {
                m.mu.Unlock()
                return fmt.Errorf("game already active")
        }
        if !m.plainOK && m.plainBroken != nil {
                err := fmt.Errorf("runtime integrity check failed: %w", m.plainBroken)
                m.mu.Unlock()
                return err
        }
        m.state = StateLaunching
        m.exitNotified = false
        m.launchID++
        launchID := m.launchID
        m.curTitle = title
        m.mu.Unlock()
        // Every error path must leave the supervisor in a usable state,
        // otherwise the next launch fails with "game already active".
        launched := false
        defer func() {
                if launched {
                        return
                }
                m.mu.Lock()
                if m.launchID == launchID {
                        m.state = StateIdle
                }
                m.mu.Unlock()
        }()

        runtimePath, _ := m.FindRuntimePackage()
        exe, err := os.Executable()
        if err != nil {
                exe = os.Args[0]
        }
        suffix := ""
        if runtime.GOOS == "windows" {
                suffix = ".exe"
        }
        ghExe := filepath.Join(filepath.Dir(exe), "flwgamehost"+suffix)
        if _, err := os.Stat(ghExe); err == nil {
                exe = ghExe
        }
        dataDir, _ := filepath.Abs(m.dataDir)
        args := []string{
                "-mode", "gamehost",
                "-pkg", pkgPath,
                "-data", dataDir,
                "-title", title,
        }
        if runtimePath != "" {
                args = append(args, "-runtime", runtimePath)
        }
        if key != nil {
                args = append(args, "-key", "-")
        }
        if ctrl := m.CtrlAddr(); ctrl != "" {
                args = append(args, "-ctrl", ctrl)
        }

        selfExe := exe
        // A build that travelled through a messenger or a cloud drive keeps
        // the Mark-of-the-Web, and Windows then refuses to run it ("The
        // request is not supported"). Removing the mark is the standard
        // remedy.
        unblockExecutables(exe, selfExe, ghExe)
        var startErr error
        var cmd *exec.Cmd
        var stdout io.ReadCloser
        // The stderr of the game host is kept: if it dies, the reason is
        // shown in the shell instead of a bare "game stopped".
        stderr := newStderrTail(8192)
        m.mu.Lock()
        m.stderrTail = stderr
        m.mu.Unlock()
        // The candidates are tried in order: the game host and the engine
        // in game host mode. If the directory itself refuses to execute
        // files (a cloud drive with placeholders for example), a copy of
        // the engine in the temporary directory is tried as well.
        candidates := m.launchCandidates(exe, selfExe)
        for _, cand := range candidates {
                cmd = exec.Command(cand, args...)
                cmd.Env = childEnv()
                if key != nil {
                        cmd.Stdin = strings.NewReader(base64Of(key) + "\n")
                } else {
                        cmd.Stdin = nil
                }
                stdout, err = cmd.StdoutPipe()
                if err != nil {
                        startErr = err
                        continue
                }
                cmd.Stderr = stderr
                if err = cmd.Start(); err != nil {
                        startErr = err
                        // The pipe of the failed attempt must be closed,
                        // otherwise every failed launch leaks a handle.
                        if stdout != nil {
                                stdout.Close()
                                stdout = nil
                        }
                        if m.logger != nil {
                                m.logger.Warn("supervisor", "start of %s failed: %v", filepath.Base(cand), err)
                        }
                        if !isUnsupportedStart(err) {
                                break
                        }
                        continue
                }
                startErr = nil
                exe = cand
                break
        }
        if startErr != nil && isUnsupportedStart(startErr) {
                // The place of the build refuses to execute the file: a copy
                // in the temporary directory is tried.
                if staged, ok := stageExecutable(selfExe); ok {
                        if m.logger != nil {
                                m.logger.Warn("supervisor", "the build cannot be started from its folder, trying a copy in the temporary directory")
                        }
                        cmd = exec.Command(staged, args...)
                        cmd.Env = childEnv()
                        if key != nil {
                                cmd.Stdin = strings.NewReader(base64Of(key) + "\n")
                        } else {
                                cmd.Stdin = nil
                        }
                        stdout, err = cmd.StdoutPipe()
                        if err == nil {
                                cmd.Stderr = stderr
                                if err = cmd.Start(); err == nil {
                                        startErr = nil
                                        exe = staged
                                } else {
                                        stdout.Close()
                                        stdout = nil
                                        startErr = err
                                }
                        }
                }
        }
        if startErr != nil {
                m.mu.Lock()
                m.state = StateIdle
                m.mu.Unlock()
                if isUnsupportedStart(startErr) {
                        return fmt.Errorf("cannot start the game: %w (Windows refused to run the file - "+
                                "extract the build into a local folder instead of a cloud drive, "+
                                "or unblock it: right click > Properties > Unblock)", startErr)
                }
                return fmt.Errorf("cannot start the game: %w", startErr)
        }
        m.mu.Lock()
        m.cmd = cmd
        m.mu.Unlock()

        // Read the gamehost's stdout exactly once: a single goroutine both
        // parses the FLWREADY line and forwards the rest to the log. Two
        // readers on the same pipe stole the FLWREADY line from each other
        // and garbled the gamehost's output.
        readyCh := make(chan *GameHandle, 1)
        go func() {
                sc := bufio.NewScanner(stdout)
                sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
                got := false
                for sc.Scan() {
                        line := sc.Text()
                        if strings.HasPrefix(line, "FLWREADY ") {
                                if !got {
                                        var g GameHandle
                                        if json.Unmarshal([]byte(line[len("FLWREADY "):]), &g) == nil {
                                                got = true
                                                readyCh <- &g
                                        }
                                }
                                continue
                        }
                        if m.logger != nil {
                                m.logger.Debug("gamehost", "%s", line)
                        }
                }
                if !got {
                        readyCh <- nil
                }
        }()

        var handle *GameHandle
        select {
        case handle = <-readyCh:
        case <-time.After(20 * time.Second):
                // The gamehost never reported its ports: treat it as a failed
                // launch. Storing a zero handle made every RPC call fail
                // with "game not running" for the whole session while the
                // game window was up.
                if cmd.Process != nil {
                        cmd.Process.Kill()
                }
                return fmt.Errorf("gamehost did not report readiness within 20s%s", stderrSuffix(stderr))
        }
        if handle == nil {
                if cmd.Process != nil {
                        cmd.Process.Kill()
                }
                if reason := stderr.lastError(); reason != "" && m.logger != nil {
                        m.logger.Crit("gamehost", "the game host failed: %s", reason)
                }
                return fmt.Errorf("gamehost exited before reporting readiness%s", stderrSuffix(stderr))
        }
        m.mu.Lock()
        cmdProc := m.cmd
        m.mu.Unlock()
        if cmdProc != nil && cmdProc.Process != nil {
                handle.PID = cmdProc.Process.Pid
        }
        m.mu.Lock()
        if m.launchID != launchID || m.exitNotified {
                m.handle = nil
                m.mu.Unlock()
                if cmd.Process != nil {
                        cmd.Process.Kill()
                }
                return fmt.Errorf("game exited during startup")
        }
        m.handle = handle
        m.state = StateRunning
        m.mu.Unlock()
        launched = true

        if !buildinfo.IsProd() {
                fmt.Fprintf(os.Stderr, "FLWGAME port=%d rpc=%d token=%s pid=%d\n", handle.Port, handle.RPC, handle.Token, handle.PID)
        }

        go func() {
                cmd.Wait()
                m.mu.Lock()
                if m.cmd == cmd {
                        m.cmd = nil
                }
                m.mu.Unlock()
                if m.logger != nil {
                        m.logger.Info("supervisor", "game process finished")
                }
                m.notifyExitOnce(launchID)
        }()
        return nil
}

func childEnv() []string {
        env := append([]string{}, os.Environ()...)
        if runtime.GOOS == "linux" {
                env = append(env, "GDK_BACKEND=x11")
                if os.Getenv("FLW_XVFB") != "" || os.Getenv("FLW_HEADLESS") != "" {
                        env = append(env, "WEBKIT_DISABLE_DMABUF_RENDERER=1", "WEBKIT_DISABLE_COMPOSITING_MODE=1", "GSK_RENDERER=cairo")
                }
        }
        return env
}

func base64Of(key []byte) string {
        const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"
        out := make([]byte, 0, 44)
        for i := 0; i < len(key); i += 3 {
                var n uint32
                rem := len(key) - i
                n = uint32(key[i]) << 16
                if rem > 1 {
                        n |= uint32(key[i+1]) << 8
                }
                if rem > 2 {
                        n |= uint32(key[i+2])
                }
                out = append(out, alphabet[(n>>18)&63], alphabet[(n>>12)&63])
                if rem > 1 {
                        out = append(out, alphabet[(n>>6)&63])
                } else {
                        out = append(out, '=')
                }
                if rem > 2 {
                        out = append(out, alphabet[n&63])
                } else {
                        out = append(out, '=')
                }
        }
        return string(out)
}

func (m *Manager) KillGame() {
        m.mu.Lock()
        cmd := m.cmd
        h := m.handle
        if cmd == nil || cmd.Process == nil {
                // Nothing to kill: do not enter StateStopping, otherwise the
                // supervisor would stay in that state forever (only the
                // cmd.Wait() goroutine resets it to Idle) and the shell would
                // keep vsync disabled and the watchdog armed.
                m.state = StateIdle
                m.handle = nil
                m.mu.Unlock()
                return
        }
        m.state = StateStopping
        m.mu.Unlock()
        if h != nil && h.RPC != 0 {
                rpcClose("127.0.0.1:"+strconv.Itoa(h.RPC), h.Token)
                time.Sleep(300 * time.Millisecond)
        }
        cmd.Process.Kill()
}

func (m *Manager) RPCCall(cmd string, js string) (string, error) {
        m.mu.Lock()
        h := m.handle
        m.mu.Unlock()
        if h == nil || h.RPC == 0 {
                return "", fmt.Errorf("game not running")
        }
        return rpcRoundtrip("127.0.0.1:"+strconv.Itoa(h.RPC), h.Token, cmd, js)
}

func rpcRoundtrip(addr, token, cmd, js string) (string, error) {
        conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
        if err != nil {
                return "", err
        }
        defer conn.Close()
        
        
        conn.SetDeadline(time.Now().Add(10 * time.Second))
        enc := json.NewEncoder(conn)
        dec := json.NewDecoder(conn)
        if err := enc.Encode(map[string]string{"cmd": "auth", "name": token}); err != nil {
                return "", err
        }
        var resp struct {
                OK bool `json:"ok"`
        }
        if err := dec.Decode(&resp); err != nil || !resp.OK {
                return "", fmt.Errorf("rpc auth failed")
        }
        if err := enc.Encode(map[string]string{"cmd": cmd, "js": js}); err != nil {
                return "", err
        }
        var r2 struct {
                OK     bool   `json:"ok"`
                Result string `json:"result"`
                Error  string `json:"error"`
        }
        if err := dec.Decode(&r2); err != nil {
                return "", err
        }
        if !r2.OK {
                return "", fmt.Errorf("%s", r2.Error)
        }
        return r2.Result, nil
}

func rpcClose(addr string, token string) {
        conn, err := net.DialTimeout("tcp", addr, 1*time.Second)
        if err != nil {
                return
        }
        defer conn.Close()
        conn.SetDeadline(time.Now().Add(3 * time.Second))
        enc := json.NewEncoder(conn)
        dec := json.NewDecoder(conn)
        enc.Encode(map[string]string{"cmd": "auth", "name": token})
        var resp struct{ OK bool }
        dec.Decode(&resp)
        enc.Encode(map[string]string{"cmd": "close"})
        dec.Decode(&resp)
}

func (m *Manager) StartHotReload() error {
        if m.hrLn != nil {
                return nil
        }
        st := m.Settings()
        addr := st.HotReloadIP + ":" + strconv.Itoa(st.HotReloadPort)
        ln, err := net.Listen("tcp", addr)
        if err != nil {
                return err
        }
        m.mu.Lock()
        m.hrLn = ln
        m.hrAddr = addr
        m.mu.Unlock()
        mux := http.NewServeMux()
        mux.HandleFunc("/push", func(w http.ResponseWriter, r *http.Request) {
                if r.Method != http.MethodPost {
                        http.Error(w, "post only", http.StatusMethodNotAllowed)
                        return
                }
                body, err := io.ReadAll(io.LimitReader(r.Body, 512<<20))
                if err != nil {
                        http.Error(w, "read failed", http.StatusBadRequest)
                        return
                }
                pkg, err := nkpkg.Parse(body, m.packageKey())
                if err != nil {
                        http.Error(w, "bad package: "+err.Error(), http.StatusBadRequest)
                        return
                }
                if pkg.Encrypted() && !buildinfo.IsProd() {
                        http.Error(w, "encrypted package rejected in dev", http.StatusBadRequest)
                        return
                }
                os.MkdirAll(m.PackagesDir(), 0o755)
                dst := filepath.Join(m.PackagesDir(), "game.nkpkg")
                if err := os.WriteFile(dst, body, 0o644); err != nil {
                        http.Error(w, "write failed", http.StatusInternalServerError)
                        return
                }
                if m.logger != nil {
                        m.logger.Info("hotreload", "package pushed: %s (%d files)", dst, len(pkg.Files))
                }
                m.mu.Lock()
                cb := m.onReload
                m.mu.Unlock()
                if cb != nil {
                        cb(fmt.Sprintf("hot reload: %d files applied", len(pkg.Files)))
                }
                if m.State() == StateRunning {
                        m.KillGame()
                }
                w.Write([]byte("ok"))
        })
        mux.HandleFunc("/status", func(w http.ResponseWriter, r *http.Request) {
                w.Header().Set("Content-Type", "application/json")
                json.NewEncoder(w).Encode(map[string]any{
                        "state":  m.State(),
                        "engine": buildinfo.Version,
                })
        })
        go http.Serve(ln, mux)
        if m.logger != nil {
                m.logger.Info("hotreload", "listening on %s", addr)
        }
        return nil
}

func (m *Manager) StopHotReload() {
        m.mu.Lock()
        ln := m.hrLn
        m.hrLn = nil
        m.mu.Unlock()
        if ln != nil {
                ln.Close()
        }
}

func (m *Manager) HotReloadRunning() bool {
        m.mu.Lock()
        defer m.mu.Unlock()
        return m.hrLn != nil
}

func (m *Manager) HotReloadAddr() string {
        m.mu.Lock()
        defer m.mu.Unlock()
        return m.hrAddr
}

func NotifyBootProgress(stage string) {
        m := M()
        if m.logger != nil {
                m.logger.Debug("boot", "progress: %s", stage)
        }
}
