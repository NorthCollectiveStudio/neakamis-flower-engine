package gamehost

import (
        "bufio"
        "encoding/base64"
        "encoding/json"
        "flag"
        "fmt"
        "io"
        "os"
        "os/signal"
        "path/filepath"
        "strings"
        "syscall"
        "time"

        "flwengine/internal/bridge"
        "flwengine/internal/buildinfo"
        "flwengine/internal/cryptx"
        "flwengine/internal/drpc"
        "flwengine/internal/nklog"
        "flwengine/internal/nkpkg"
        "flwengine/internal/plat"
)

func CliMain(args []string) int {
        fs := flag.NewFlagSet("gamehost", flag.ContinueOnError)
        fs.SetOutput(io.Discard)
        mode := fs.String("mode", "gamehost", "gamehost|serve")
        pkgPath := fs.String("pkg", "", "game package path")
        runtimePath := fs.String("runtime", "", "runtime package path")
        keyPath := fs.String("key", "", "symmetric key file, - for stdin")
        dataDir := fs.String("data", "flwdata", "engine data directory")
        fixedPort := fs.Int("port", 0, "fixed http port (serve mode)")
        ctrlAddr := fs.String("ctrl", "", "supervisor ctrl address")
        windowTitle := fs.String("title", "The Rosemary Field", "game window title")
        winW := fs.Int("w", 816, "window width")
        winH := fs.Int("h", 624, "window height")
        showBuild := fs.Bool("version", false, "print version")
        if err := fs.Parse(args); err != nil {
                fmt.Fprintf(os.Stderr, "args parse failed: %v\n", err)
                return 2
        }
        if *showBuild {
                fmt.Printf("flower gamehost %s type=%s platform=%s\n", buildinfo.Version, buildinfo.BuildType, buildinfo.CurrentPlatform())
                return 0
        }
        if err := cryptx.Init(); err != nil {
                fmt.Fprintf(os.Stderr, "cryptx init failed: %v\n", err)
                return 1
        }
        if *mode == "serve" && buildinfo.IsProd() {
                fmt.Fprintf(os.Stderr, "serve mode disabled in this build type\n")
                return 1
        }
        dataRoot := *dataDir
        if err := os.MkdirAll(filepath.Join(dataRoot, "logs"), 0o755); err != nil {
                fmt.Fprintf(os.Stderr, "datadir failed: %v\n", err)
                return 1
        }
        return runGameHost(*mode, *pkgPath, *runtimePath, *keyPath, dataRoot, *fixedPort, *windowTitle, *winW, *winH, *ctrlAddr)
}

func openLogger(dataRoot string, plain bool) *nklog.Writer {
        name := fmt.Sprintf("run-%s-%d.nklog", strings.ReplaceAll(time.Now().Format("20060102-150405"), ".", ""), time.Now().UnixNano()%1000)
        w, err := nklog.New(filepath.Join(dataRoot, "logs", name), plain)
        if err != nil {
                fmt.Fprintf(os.Stderr, "logger failed: %v\n", err)
                w, _ = nklog.New(filepath.Join(os.TempDir(), "flw-fallback.nklog"), true)
        }
        return w
}

func loadKey(path string) []byte {
        if path == "" {
                return cryptx.PkgKey()
        }
        var raw []byte
        if path == "-" {
                sc := bufio.NewReader(os.Stdin)
                line, err := sc.ReadString('\n')
                if err != nil && line == "" {
                        fmt.Fprintf(os.Stderr, "key read failed: %v\n", err)
                        os.Exit(1)
                }
                raw = []byte(strings.TrimSpace(line))
        } else {
                b, err := os.ReadFile(path)
                if err != nil {
                        fmt.Fprintf(os.Stderr, "key read failed: %v\n", err)
                        os.Exit(1)
                }
                raw = []byte(strings.TrimSpace(string(b)))
        }
        k, err := base64.StdEncoding.DecodeString(string(raw))
        if err != nil || len(k) != 32 {
                fmt.Fprintf(os.Stderr, "bad key\n")
                os.Exit(1)
        }
        return k
}

func loadPackage(path string, key []byte, logger *nklog.Writer) *nkpkg.Package {
        if path == "" {
                fmt.Fprintf(os.Stderr, "package path required\n")
                os.Exit(1)
        }
        p, err := nkpkg.Open(path, key)
        if err != nil {
                fmt.Fprintf(os.Stderr, "package load failed: %s: %v\n", path, err)
                logger.Crit("boot", "package load failed: %s: %v", path, err)
                os.Exit(1)
        }
        features := buildinfo.ParseFeatures()
        if features.Signing {
                pub := cryptx.SignPub()
                if pub == nil {
                        logger.Crit("boot", "signing required but no public key")
                        fmt.Fprintf(os.Stderr, "no signing public key\n")
                        os.Exit(1)
                }
                if !p.Signed() {
                        logger.Crit("boot", "package not signed: %s", path)
                        fmt.Fprintf(os.Stderr, "package not signed: %s\n", path)
                        os.Exit(1)
                }
                if err := p.VerifySignature(pub); err != nil {
                        logger.Crit("boot", "signature invalid: %s: %v", path, err)
                        fmt.Fprintf(os.Stderr, "signature invalid: %s: %v\n", path, err)
                        os.Exit(1)
                }
                logger.Info("boot", "package signature verified: %s", path)
        }
        return p
}

func runGameHost(mode string, pkgPath, runtimePath, keyPath, dataRoot string, fixedPort int, title string, winW, winH int, ctrlAddress string) int {
        logPlain := buildinfo.IsDev() && os.Getenv("FLW_PLAIN_LOG") != "0"
        logger := openLogger(dataRoot, logPlain)
        defer logger.Close()

        logger.Info("boot", "flower engine %s starting (mode=%s type=%s platform=%s)", buildinfo.Version, mode, buildinfo.BuildType, buildinfo.CurrentPlatform())

        key := loadKey(keyPath)
        gp := loadPackage(pkgPath, key, logger)
        logger.Info("boot", "game package loaded: %s (%d files, enc=%v signed=%v)", pkgPath, len(gp.Files), gp.Encrypted(), gp.Signed())

        var rp *nkpkg.Package
        if runtimePath != "" {
                if p, err := nkpkg.Open(runtimePath, key); err == nil {
                        rp = p
                        logger.Info("boot", "runtime package loaded: %s", runtimePath)
                } else {
                        logger.Warn("boot", "runtime package load failed: %v", err)
                }
        }
        if rp == nil && buildinfo.ParseFeatures().Encryption {
                // In a production build the bridge is only in the runtime
                // package: without it the game cannot start.
                logger.Crit("boot", "runtime package missing: %s", runtimePath)
                fmt.Fprintf(os.Stderr, "the runtime package is missing: %s\n", runtimePath)
                return 1
        }

        host := New(logger)
        host.GamePkg = gp
        host.GameKey = key
        host.RuntimePkg = rp
        host.RuntimeKey = key

        if fixedPort > 0 {
                if err := host.StartHTTPOn(fixedPort); err != nil {
                        fmt.Fprintf(os.Stderr, "http listen failed: %v\n", err)
                        logger.Crit("boot", "http listen failed: %v", err)
                        return 1
                }
        } else {
                if err := host.StartHTTP(); err != nil {
                        fmt.Fprintf(os.Stderr, "http listen failed: %v\n", err)
                        logger.Crit("boot", "http listen failed: %v", err)
                        return 1
                }
        }

        saveVFS := NewVFS(dataRoot)
        MigrateSaveDir(dataRoot)
        host.SetVFS(saveVFS)
        osInfo, _ := plat.Detect()
        var ctrl *CtrlClient
        if ctrlAddress != "" {
                ctrl = NewCtrlClient(ctrlAddress)
                logger.Info("boot", "ctrl enabled: %s", ctrlAddress)
                defer ctrl.Close()
                defer ctrl.Push("exit", "", "")
        }
        forwardEvent := func(name, data string) {
                host.HandleEvent(name, data)
                if ctrl != nil {
                        ctrl.Push("event", name, data)
                }
        }
        env := &bridge.Env{
                Log:          logger,
                GameDir:      dataRoot,
                TouchEnabled: osInfo.Platform == "android",
                VFSRead:      saveVFS.Read,
                VFSWrite:     saveVFS.Write,
                VFSList:      saveVFS.List,
                VFSExists:    saveVFS.Exists,
                VFSUnlink:    saveVFS.Unlink,
                CloseGame:    func() { host.RequestExit() },
                OnEvent:      forwardEvent,
        }
        env.SetTitle = func(t string) { host.SetGameWindowTitle(t) }
        env.SetSize = func(w, hh int, fs bool) { host.SetGameWindowSize(w, hh, fs) }
        env.MoveWindow = func(x, y int) { host.MoveGameWindow(x, y) }
        env.WindowPos = func() (int, int, bool) { return host.GameWindowPos() }
        env.CenterWindow = func(pos string) { host.CenterGameWindow(pos) }
        env.AlwaysOnTop = func(on bool) { host.SetGameWindowAlwaysOnTop(on) }
        env.LowerWindow = func() { host.LowerGameWindow() }
        env.ShowDialog = func(title, text string) bool { return host.ShowGameDialog(title, text) }
        env.HostWrite = HostWriteAllowed
        env.HostExists = func(p string) bool {
                return HostExistsAllowed(p) || HostDirAllowed(p)
        }
        drpcClient := drpc.NewClient(logger)
        defer drpcClient.Stop()
        env.RPC = drpcClient
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

        
        
        
        host.OnHung(func() {
                if ctrl != nil {
                        ctrl.Push("event", "game.hung", "eval timeout")
                }
        })

        if err := host.StartRPC(); err != nil {
                logger.Warn("boot", "rpc start failed: %v", err)
        }

        if mode == "serve" {
                info := map[string]any{
                        "mode":  "serve",
                        "port":  host.Port(),
                        "rpc":   host.RPCPort(),
                        "token": host.Token(),
                        "url":   host.BootURL(),
                }
                out, _ := json.Marshal(info)
                fmt.Printf("FLWREADY %s\n", out)
                logger.Info("boot", "serve mode ready port=%d rpc=%d", host.Port(), host.RPCPort())
                sig := make(chan os.Signal, 1)
                signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
                <-sig
                host.Stop()
                return 0
        }

        info := map[string]any{
                "mode":  "gamehost",
                "port":  host.Port(),
                "rpc":   host.RPCPort(),
                "token": host.Token(),
        }
        out, _ := json.Marshal(info)
        fmt.Printf("FLWREADY %s\n", out)
        logger.Info("boot", "gamehost ready port=%d rpc=%d", host.Port(), host.RPCPort())

        host.OnExit(func(code int) {
                logger.Info("boot", "game window closed")
                host.Stop()
        })
        err := host.RunWindow(title, winW, winH, br)
        logger.Info("boot", "gamehost exit: %v", err)
        host.Stop()
        if err != nil {
                // The message goes to the stderr of the game host: the
                // shell shows it instead of a bare "game stopped".
                fmt.Fprintf(os.Stderr, "the game window failed: %v\n", err)
                return 1
        }
        return 0
}
