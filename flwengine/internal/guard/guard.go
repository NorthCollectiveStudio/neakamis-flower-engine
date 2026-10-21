package guard

import (
        "crypto/ed25519"
        "crypto/sha256"
        "encoding/binary"
        "fmt"
        "os"
        "runtime"
        "sync"
        "sync/atomic"
        "time"

        "flwengine/internal/cryptx"
        "flwengine/internal/nklog"
)

var (
        enabled atomic.Bool
        mu      sync.Mutex
        logger  *nklog.Writer
        stopCh  chan struct{}
        seedOff uint64
)

const trailerMagic = "FLWSIG1"

func Enable(log *nklog.Writer) error {
        mu.Lock()
        defer mu.Unlock()
        if enabled.Load() {
                return nil
        }
        logger = log
        cryptx.SetLocker(lockRegion)
        if err := bootChecks(); err != nil {
                return err
        }
        if err := cryptx.LockMasters(); err != nil {
                if logger != nil {
                        logger.Warn("guard", "key lock failed: %v", err)
                }
        }
        enabled.Store(true)
        stopCh = make(chan struct{})
        go watchLoop()
        if logger != nil {
                logger.Info("guard", "active (self-verified, memory locked)")
        }
        return nil
}

func Enabled() bool {
        return enabled.Load()
}

func bootChecks() error {
        jitterRandomize()
        if r := debuggerPresent(); r != "" {
                return failBoot("debugger: " + r)
        }
        if runtime.GOOS != "android" {
                if err := VerifySelf(); err != nil {
                        return failBoot("self: " + err.Error())
                }
        }
        time.Sleep(time.Duration(10+seedOff%40) * time.Millisecond)
        if r := debuggerPresent(); r != "" {
                return failBoot("debugger: " + r)
        }
        return nil
}

func failBoot(reason string) error {
        if logger != nil {
                logger.Crit("guard", "blocked at boot: %s", reason)
        }
        return fmt.Errorf("guard: %s", reason)
}

func watchLoop() {
        last := time.Now()
        tk := time.NewTicker(4 * time.Second)
        defer tk.Stop()
        for {
                select {
                case <-stopCh:
                        return
                case <-tk.C:
                        now := time.Now()
                        d := now.Sub(last)
                        last = now
                        if r := debuggerPresent(); r != "" {
                                terminate("debugger: " + r)
                                return
                        }
                        if d > 12*time.Second {
                                terminate(fmt.Sprintf("timing anomaly: %v", d))
                                return
                        }
                }
        }
}

func terminate(reason string) {
        if logger != nil {
                logger.Crit("guard", "terminating: %s", reason)
                logger.Close()
        }
        enabled.Store(false)
        os.Exit(70)
}

func VerifySelf() error {
        exe, err := os.Executable()
        if err != nil {
                return err
        }
        data, err := os.ReadFile(exe)
        if err != nil {
                return err
        }
        pub := cryptx.SignPub()
        if pub == nil {
                return fmt.Errorf("no signing public key")
        }
        return verifyTrailer(data, pub)
}

func verifyTrailer(data []byte, pub ed25519.PublicKey) error {
        if len(data) < 7+4+64 {
                return fmt.Errorf("no integrity trailer")
        }
        tail := data[len(data)-75:]
        if string(tail[:7]) != trailerMagic {
                return fmt.Errorf("integrity trailer missing")
        }
        sigLen := binary.LittleEndian.Uint32(tail[7:11])
        if sigLen != 64 {
                return fmt.Errorf("bad trailer length")
        }
        sig := tail[11:75]
        body := data[:len(data)-75]
        digest := sha256.Sum256(body)
        if !ed25519.Verify(pub, digest[:], sig) {
                return fmt.Errorf("integrity signature invalid")
        }
        return nil
}

func jitterRandomize() {
        b := cryptx.RandomBytes(8)
        var v uint64
        for i := 0; i < 8; i++ {
                v = v<<8 | uint64(b[i])
        }
        seedOff = v
        for i := 0; i < 3; i++ {
                n := int(v%4096) + 1024
                _ = make([]byte, n)
                v = v*6364136223846793005 + 1442695040888963407
        }
}
