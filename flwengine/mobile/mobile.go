package mobile

import (
        "encoding/json"
        "fmt"
        "os"
        "path/filepath"
        "strings"
        "sync"
        "time"

        "flwengine/internal/bridge"
        "flwengine/internal/buildinfo"
        "flwengine/internal/cryptx"
        "flwengine/internal/drpc"
        "flwengine/internal/gamehost"
        "flwengine/internal/guard"
        "flwengine/internal/nklog"
        "flwengine/internal/nkpkg"
        "flwengine/internal/plat"
        "flwengine/internal/supervisor"
)

type Engine struct {
        mu      sync.Mutex
        host    *gamehost.Host
        br      *bridge.Bridge
        logger  *nklog.Writer
        saveVFS *gamehost.VFS
        rpc     *drpc.Client
        events  [][]string
        started bool
}

func NewEngine() *Engine {
        return &Engine{}
}

func (e *Engine) Version() string {
        return buildinfo.Version
}

func openLogger(dataDir string) *nklog.Writer {
        name := fmt.Sprintf("run-%s-%d.nklog", strings.ReplaceAll(time.Now().Format("20060102-150405"), ".", ""), time.Now().UnixNano()%1000)
        w, err := nklog.New(filepath.Join(dataDir, "logs", name), !buildinfo.IsProd())
        if err != nil {
                w, _ = nklog.New(filepath.Join(dataDir, "flw-fallback.nklog"), true)
        }
        return w
}

func (e *Engine) Start(dataDir string, pkgPath string, runtimePath string, port int) error {
        e.mu.Lock()
        if e.started {
                e.mu.Unlock()
                return fmt.Errorf("engine already started")
        }
        e.mu.Unlock()

        if err := cryptx.Init(); err != nil {
                return fmt.Errorf("cryptx: %w", err)
        }

        logger := openLogger(dataDir)
        logger.Info("mobile", "flower engine %s (type=%s platform=%s)", buildinfo.Version, buildinfo.BuildType, buildinfo.CurrentPlatform())

        var key []byte
        features := buildinfo.ParseFeatures()
        if features.Guard {
                if err := guard.Enable(logger); err != nil {
                        logger.Crit("mobile", "guard init failed: %v", err)
                        return fmt.Errorf("guard: %w", err)
                }
        }
        if features.Encryption {
                key = cryptx.PkgKey()
        }
        gp, err := nkpkg.Open(pkgPath, key)
        if err != nil {
                logger.Crit("mobile", "game package load failed: %v", err)
                return fmt.Errorf("game package: %w", err)
        }
        if features.Signing {
                pub := cryptx.SignPub()
                if pub == nil {
                        logger.Crit("mobile", "signing required but no public key")
                        return fmt.Errorf("no signing public key")
                }
                if !gp.Signed() {
                        logger.Crit("mobile", "game package not signed")
                        return fmt.Errorf("game package not signed")
                }
                if err := gp.VerifySignature(pub); err != nil {
                        logger.Crit("mobile", "game signature invalid: %v", err)
                        return fmt.Errorf("game signature: %w", err)
                }
                logger.Info("mobile", "game package signature verified")
        }
        logger.Info("mobile", "game package loaded: %d files (enc=%v signed=%v)", len(gp.Files), gp.Encrypted(), gp.Signed())

        var rp *nkpkg.Package
        if runtimePath != "" {
                if p, err := nkpkg.Open(runtimePath, key); err == nil {
                        if features.Signing && p.Signed() {
                                if err := p.VerifySignature(cryptx.SignPub()); err != nil {
                                        logger.Crit("mobile", "runtime signature invalid: %v", err)
                                        return fmt.Errorf("runtime signature: %w", err)
                                }
                                logger.Info("mobile", "runtime package signature verified")
                        }
                        rp = p
                } else {
                        logger.Crit("mobile", "runtime package unreadable: %v", err)
                        if features.Encryption {
                                // The bridge is only in the runtime package
                                // in a production build.
                                return fmt.Errorf("runtime package: %w", err)
                        }
                }
        }
        if features.Encryption && rp == nil {
                logger.Crit("mobile", "runtime package missing")
                return fmt.Errorf("runtime package missing")
        }
        if features.Signing {
                supervisor.SetDataDir(dataDir)
                m := supervisor.Init(dataDir, logger)
                if _, err := m.PlainStatus(); err != nil {
                        return fmt.Errorf("plain runtime integrity: %w", err)
                }
                logger.Info("mobile", "plain runtime integrity verified")
        }

        host := gamehost.New(logger)
        host.GamePkg = gp
        host.GameKey = key
        host.RuntimePkg = rp
        host.RuntimeKey = key

        
        
        if os.Getenv("HOME") == "" || os.Getenv("HOME") == "/" {
                homeDir := filepath.Join(dataDir, "home")
                os.MkdirAll(homeDir, 0o755)
                os.Setenv("HOME", homeDir)
        }

        saveVFS := gamehost.NewVFS(dataDir)
        gamehost.MigrateSaveDir(dataDir)
        host.SetVFS(saveVFS)

        if port <= 0 {
                port = 18080
        }
        if err := host.StartHTTPOn(port); err != nil {
                return fmt.Errorf("http: %w", err)
        }

        osInfo, _ := plat.Detect()

        rpcClient := drpc.NewClient(logger)

        forwardEvent := func(name, data string) {
                host.HandleEvent(name, data)
                e.mu.Lock()
                e.events = append(e.events, []string{name, data})
                e.mu.Unlock()
        }

        env := &bridge.Env{
                Log:          logger,
                GameDir:      dataDir,
                TouchEnabled: osInfo.Platform == "android",
                VFSRead:      saveVFS.Read,
                VFSWrite:     saveVFS.Write,
                VFSList:      saveVFS.List,
                VFSExists:    saveVFS.Exists,
                VFSUnlink:    saveVFS.Unlink,
                HostWrite:    gamehost.HostWriteAllowed,
                HostExists: func(p string) bool {
                        return gamehost.HostExistsAllowed(p) || gamehost.HostDirAllowed(p)
                },
                CloseGame: func() {
                        forwardEvent("game.exit", "")
                },
                OnEvent: forwardEvent,
        }
        env.RPC = rpcClient

        br := bridge.New(env)
        if si := br.SysInfo(); si != nil {
                em := host.EnvMap()
                for k, v := range em {
                        if k == "DESKTOP_DIR" || k == "FLW_PID" {
                                continue
                        }
                        si.Env[k] = v
                }
        }
        host.SetBridge(br)

        e.mu.Lock()
        e.host = host
        e.br = br
        e.logger = logger
        e.saveVFS = saveVFS
        e.rpc = rpcClient
        e.started = true
        e.mu.Unlock()

        logger.Info("mobile", "engine started port=%d", port)
        return nil
}

func (e *Engine) BootURL() string {
        e.mu.Lock()
        defer e.mu.Unlock()
        if e.host == nil {
                return ""
        }
        return e.host.BootURL()
}

func (e *Engine) Token() string {
        e.mu.Lock()
        defer e.mu.Unlock()
        if e.host == nil {
                return ""
        }
        return e.host.Token()
}

func (e *Engine) Port() int {
        e.mu.Lock()
        defer e.mu.Unlock()
        if e.host == nil {
                return 0
        }
        return e.host.Port()
}

func (e *Engine) HandleNative(name string, args string) string {
        e.mu.Lock()
        br := e.br
        e.mu.Unlock()
        if br == nil {
                out, _ := json.Marshal(bridge.Result{OK: false, Error: "engine not started"})
                return string(out)
        }
        res := br.Handle(name, json.RawMessage(args))
        out, err := json.Marshal(res)
        if err != nil {
                return `{"ok":false,"e":"marshal"}`
        }
        return string(out)
}

func (e *Engine) GetEvents() string {
        e.mu.Lock()
        evs := e.events
        e.events = nil
        e.mu.Unlock()
        if len(evs) == 0 {
                return "[]"
        }
        out, err := json.Marshal(evs)
        if err != nil {
                return "[]"
        }
        return string(out)
}

func (e *Engine) Stop() {
        e.mu.Lock()
        host := e.host
        rpc := e.rpc
        logger := e.logger
        e.host = nil
        e.br = nil
        e.saveVFS = nil
        e.started = false
        e.mu.Unlock()
        if host != nil {
                host.Stop()
        }
        if rpc != nil {
                rpc.Stop()
        }
        if logger != nil {
                logger.Info("mobile", "engine stopped")
                logger.Close()
        }
}

func (e *Engine) Running() bool {
        e.mu.Lock()
        defer e.mu.Unlock()
        return e.started && e.host != nil
}
