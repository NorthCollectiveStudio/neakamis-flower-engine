package supervisor

import (
        "crypto/sha256"
        "encoding/hex"
        "encoding/json"
        "fmt"
        "os"
        "path/filepath"
        "sort"
        "strings"

        "flwengine/internal/buildinfo"
        "flwengine/internal/cryptx"
        "flwengine/internal/nkpkg"
)

type PlainManifest struct {
        Game    string          `json:"game"`
        Runtime string          `json:"runtime"`
        Files   []PlainFileRec  `json:"files"`
        Root    string          `json:"root"`
}

type PlainFileRec struct {
        Name string `json:"n"`
        Size int64  `json:"s"`
        Hash string `json:"h"`
}

// runtimeKey returns the key of the runtime package: the one that was given
// explicitly or the one that is built into the engine.
func (m *Manager) runtimeKey() []byte {
        m.mu.Lock()
        k := m.pkgKey
        m.mu.Unlock()
        if len(k) > 0 {
                return k
        }
        return cryptx.PkgKeyOrNil()
}

// VerifyPlain checks the integrity of the runtime files.
//
// The files are verified INSIDE the runtime package: in a production build
// that package is encrypted and signed, so this is the only copy of the
// bridge and of the boot page. A plain copy next to the engine would make
// the encryption pointless and would verify a file that is not even used.
func (m *Manager) VerifyPlain(expectedRoot string) error {
        runtimePath, _ := m.FindRuntimePackage()
        if runtimePath == "" {
                return fmt.Errorf("runtime package missing")
        }
        key := m.runtimeKey()
        p, err := nkpkg.Open(runtimePath, key)
        if err != nil {
                return fmt.Errorf("runtime package unreadable: %w", err)
        }
        raw, err := p.ReadFile("nkmanifest.json", key)
        if err != nil {
                return fmt.Errorf("plain manifest missing: %w", err)
        }
        var mf PlainManifest
        if err := json.Unmarshal(raw, &mf); err != nil {
                return fmt.Errorf("plain manifest corrupt: %w", err)
        }
        byName := map[string]PlainFileRec{}
        for _, f := range mf.Files {
                byName[f.Name] = f
        }
        // The package must contain exactly the files of the manifest and the
        // manifest itself: an extra file in the runtime package would be
        // served to the game.
        have := map[string]bool{}
        for _, n := range p.FileNames() {
                have[n] = true
        }
        for n := range have {
                if n == "nkmanifest.json" {
                        continue
                }
                if _, ok := byName[n]; !ok {
                        return fmt.Errorf("plain file set mismatch: %s", n)
                }
        }
        for n := range byName {
                if !have[n] {
                        return fmt.Errorf("plain file missing: %s", n)
                }
        }
        var digestSrc strings.Builder
        for _, name := range sortedNames(byName) {
                rec := byName[name]
                data, err := p.ReadFile(name, key)
                if err != nil {
                        return fmt.Errorf("plain read failed: %s: %w", name, err)
                }
                sum := sha256.Sum256(data)
                sumHex := hex.EncodeToString(sum[:])
                if sumHex != rec.Hash || int64(len(data)) != rec.Size {
                        return fmt.Errorf("plain integrity mismatch: %s", name)
                }
                digestSrc.WriteString(name)
                digestSrc.WriteString(":")
                digestSrc.WriteString(sumHex)
                digestSrc.WriteString("\n")
        }
        root := sha256.Sum256([]byte(digestSrc.String()))
        rootHex := hex.EncodeToString(root[:])
        if expectedRoot != "" && rootHex != expectedRoot {
                return fmt.Errorf("plain root mismatch")
        }
        if mf.Root != "" && rootHex != mf.Root {
                return fmt.Errorf("plain root mismatch (manifest)")
        }
        return nil
}

// sortedNames returns the file names of the manifest in a stable order.
func sortedNames(byName map[string]PlainFileRec) []string {
        names := make([]string, 0, len(byName))
        for n := range byName {
                names = append(names, n)
        }
        sort.Strings(names)
        return names
}

func (m *Manager) verifyPlainAtBoot() error {
        if !buildinfo.ParseFeatures().Signing {
                return nil
        }
        expected := buildinfo.PlainRoot
        if err := m.VerifyPlain(expected); err != nil {
                if m.logger != nil {
                        m.logger.Crit("plain", "integrity check failed: %v", err)
                }
                return err
        }
        if m.logger != nil {
                m.logger.Info("plain", "runtime integrity verified")
        }
        return nil
}

type GameMeta struct {
        Title    string `json:"title"`
        Version string `json:"version"`
        Icon     string `json:"icon"`
        Files    int    `json:"files"`
        Enc      bool   `json:"enc"`
        Found    bool   `json:"found"`
}

func (m *Manager) GameMeta() GameMeta {
        meta := GameMeta{}
        pkgPath, key := m.FindGamePackage()
        if pkgPath == "" {
                return meta
        }
        
        
        
        cacheKey := ""
        if st, err := os.Stat(pkgPath); err == nil {
                cacheKey = fmt.Sprintf("%s|%d|%d", pkgPath, st.Size(), st.ModTime().UnixNano())
                m.mu.Lock()
                hit := m.metaCacheKey == cacheKey
                cached := m.metaCache
                m.mu.Unlock()
                if hit && cached.Found {
                        return cached
                }
        }
        p, err := nkpkg.OpenMeta(pkgPath, key)
        if err != nil {
                return meta
        }
        meta.Found = true
        meta.Files = len(p.Files)
        meta.Enc = p.Encrypted()
        meta.Title = p.Manifest.Title
        meta.Version = p.Manifest.Version
        if meta.Title == "" {
                meta.Title = "the rosemary field"
        }
        for _, cand := range []string{"icon/icon.png", "icon.png"} {
                if p.Has(cand) {
                        cacheDir := filepath.Join(m.dataDir, "cache")
                        os.MkdirAll(cacheDir, 0o755)
                        iconPath := filepath.Join(cacheDir, "gameicon.png")
                        
                        if st, err := os.Stat(pkgPath); err == nil {
                                if prev, err := os.Stat(iconPath); err != nil || prev.ModTime().Before(st.ModTime()) {
                                        if raw, err := p.ReadFileFrom(pkgPath, cand, key); err == nil {
                                                os.WriteFile(iconPath, raw, 0o644)
                                        }
                                }
                        }
                        if _, err := os.Stat(iconPath); err == nil {
                                meta.Icon = iconPath
                        }
                        break
                }
        }
        m.mu.Lock()
        m.metaCache, m.metaCacheKey = meta, cacheKey
        m.mu.Unlock()
        return meta
}

func (m *Manager) InstallPackage(path string) error {
        if buildinfo.IsProd() {
                return fmt.Errorf("package install disabled in prod")
        }
        data, err := os.ReadFile(path)
        if err != nil {
                return fmt.Errorf("read failed: %w", err)
        }
        p, err := nkpkg.Parse(data, nil)
        if err != nil {
                return fmt.Errorf("bad package: %w", err)
        }
        if p.Encrypted() {
                return fmt.Errorf("encrypted packages rejected in dev install")
        }
        if p.Manifest.Kind != "game" {
                return fmt.Errorf("not a game package (kind=%s)", p.Manifest.Kind)
        }
        os.MkdirAll(m.PackagesDir(), 0o755)
        dst := filepath.Join(m.PackagesDir(), "game.nkpkg")
        if err := os.WriteFile(dst, data, 0o644); err != nil {
                return fmt.Errorf("write failed: %w", err)
        }
        if m.logger != nil {
                m.logger.Info("install", "package installed: %s -> %s (%d files, title=%s)", path, dst, len(p.Files), p.Manifest.Title)
        }
        return nil
}

func (m *Manager) IncomingDir() string {
        return filepath.Join(m.dataDir, "incoming")
}

func (m *Manager) ScanIncoming() []string {
        var out []string
        entries, err := os.ReadDir(m.IncomingDir())
        if err != nil {
                return out
        }
        for _, e := range entries {
                if e.IsDir() {
                        continue
                }
                name := e.Name()
                if strings.HasSuffix(name, ".nkpkg") {
                        out = append(out, filepath.Join(m.IncomingDir(), name))
                }
        }
        sort.Strings(out)
        return out
}

func (m *Manager) VerifyPackageSignature(path string) error {
        p, err := nkpkg.Open(path, nil)
        if err != nil {
                return err
        }
        pub := cryptx.SignPub()
        if pub == nil {
                return fmt.Errorf("no public key")
        }
        return p.VerifySignature(pub)
}
