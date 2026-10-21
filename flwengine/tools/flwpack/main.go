package main

import (
        "crypto/ed25519"
        "encoding/json"
        "flag"
        "fmt"
        "os"
        "path/filepath"
        "strings"

        "flwengine/internal/cryptx"
        "flwengine/internal/nkpkg"
)

type buildConfig struct {
        Game     string            `json:"game"`
        Includes []string          `json:"includes"`
        Excludes []string          `json:"excludes"`
        Plugins  map[string]string `json:"plugins"`
}

func main() {
        dir := flag.String("dir", "", "game directory")
        out := flag.String("out", "game.nkpkg", "output package path")
        id := flag.String("id", "prodgame", "package id")
        kind := flag.String("kind", "game", "package kind: game|runtime")
        title := flag.String("title", "", "package title")
        version := flag.String("version", "1.0.0", "package version")
        entry := flag.String("entry", "index.html", "entry point")
        buildType := flag.String("buildtype", "dev", "build type")
        platform := flag.String("platform", "", "target platform")
        encrypt := flag.Bool("encrypt", false, "encrypt package")
        sign := flag.Bool("sign", false, "sign package")
        keyFile := flag.String("key", "", "key file (base64 32 bytes)")
        keygen := flag.String("keygen", "", "generate key file and exit")
        signKey := flag.String("signkey", "", "signing key file (ed25519, base64 seed)")
        signPub := flag.String("signpub", "", "signing public key file to write")
        pluginOverrides := flag.String("pluginoverrides", "", "json map of plugin name to on/off")
        cfgPath := flag.String("config", "", "buildconfig json with packaging rules")
        flag.Parse()

        if *keygen != "" {
                k := cryptx.RandomBytes(32)
                if err := os.WriteFile(*keygen, []byte(toB64(k)), 0o600); err != nil {
                        fatal("write key: %v", err)
                }
                fmt.Printf("key written to %s\n", *keygen)
                return
        }

        if *dir == "" {
                fatal("dir required")
        }

        var cfg buildConfig
        if *pluginOverrides != "" {
                if err := json.Unmarshal([]byte(*pluginOverrides), &cfg.Plugins); err != nil {
                        fatal("parse plugin overrides: %v", err)
                }
        }
        if *cfgPath != "" {
                raw, err := os.ReadFile(*cfgPath)
                if err != nil {
                        fatal("read config: %v", err)
                }
                if err := json.Unmarshal(raw, &cfg); err != nil {
                        fatal("parse config: %v", err)
                }
        }

        var key []byte
        if *encrypt {
                if *keyFile == "" {
                        fatal("encrypt requires key file")
                }
                raw, err := os.ReadFile(*keyFile)
                if err != nil {
                        fatal("read key: %v", err)
                }
                k, err := fromB64(strings.TrimSpace(string(raw)))
                if err != nil || len(k) != 32 {
                        fatal("bad key file")
                }
                key = k
        }

        src := newFilteredSource(*dir, cfg)
        man := nkpkg.Manifest{
                ID:        *id,
                Kind:      *kind,
                Title:     *title,
                Version:   *version,
                Entry:     *entry,
                BuildType: *buildType,
                Platform:  *platform,
                Meta:      map[string]string{},
        }
        if man.Title == "" {
                man.Title = filepath.Base(*dir)
        }

        opts := nkpkg.PackOptions{
                Manifest: man,
                Encrypt:  *encrypt,
                Sign:     *sign,
                Key:      key,
        }
        if *sign {
                sk, pub, err := loadSigner(*signKey)
                if err != nil {
                        fatal("load signer: %v", err)
                }
                opts.Signer = sk
                if *signPub != "" {
                        os.WriteFile(*signPub, []byte(toB64(pub)), 0o644)
                }
        }

        p, err := nkpkg.Pack(src, opts)
        if err != nil {
                fatal("pack: %v", err)
        }
        if err := nkpkg.Save(p, *out); err != nil {
                fatal("save: %v", err)
        }
        fmt.Printf("packed %s: %d files, %d bytes\n", *out, len(p.Files), len(p.Bytes()))
}

type filteredSource struct {
        root     string
        includes []string
        excludes []string
        plugins  map[string]string
        names    []string
}

func newFilteredSource(root string, cfg buildConfig) *filteredSource {
        fs := &filteredSource{
                root:     root,
                includes: cfg.Includes,
                excludes: cfg.Excludes,
                plugins:  cfg.Plugins,
        }
        if len(fs.includes) == 0 {
                fs.includes = []string{"data", "img", "audio", "js", "css", "fonts", "icon", "movies", "effects", "locales", "index.html", "package.json", "game.rmmzproject"}
        }
        if len(fs.excludes) == 0 {
                fs.excludes = []string{"save", ".git", "node_modules", "*.txt", "*копия*", "*.log", "*~", "*.rdcat"}
        }
        fs.scan()
        return fs
}

func (f *filteredSource) scan() {
        var walk func(rel string)
        walk = func(rel string) {
                entries, err := os.ReadDir(filepath.Join(f.root, rel))
                if err != nil {
                        return
                }
                for _, e := range entries {
                        name := e.Name()
                        child := name
                        if rel != "" {
                                child = rel + "/" + name
                        }
                        if e.IsDir() {
                                if f.excluded(child) {
                                        continue
                                }
                                walk(child)
                        } else {
                                if f.excluded(child) {
                                        continue
                                }
                                f.names = append(f.names, child)
                        }
                }
        }
        walk("")
        if len(f.plugins) > 0 {
                kept := f.names[:0]
                for _, name := range f.names {
                        if strings.HasPrefix(name, "js/plugins/") && strings.HasSuffix(name, ".js") {
                                pluginName := strings.TrimSuffix(strings.TrimPrefix(name, "js/plugins/"), ".js")
                                if state, ok := f.plugins[pluginName]; ok && state != "on" {
                                        continue
                                }
                        }
                        kept = append(kept, name)
                }
                f.names = kept
        }
}

func (f *filteredSource) excluded(name string) bool {
        for _, ex := range f.excludes {
                if strings.HasPrefix(ex, "*") && strings.HasSuffix(ex, "*") {
                        if strings.Contains(name, ex[1:len(ex)-1]) {
                                return true
                        }
                } else if strings.HasPrefix(ex, "*") {
                        if strings.HasSuffix(name, ex[1:]) {
                                return true
                        }
                } else if strings.HasSuffix(ex, "*") {
                        if strings.HasPrefix(name, ex[:len(ex)-1]) {
                                return true
                        }
                } else if name == ex || strings.HasPrefix(name, ex+"/") {
                        return true
                }
        }
        return false
}

func (f *filteredSource) Names() ([]string, error) {
        out := make([]string, 0, len(f.names))
        out = append(out, f.names...)
        return out, nil
}

func (f *filteredSource) Open(name string) ([]byte, error) {
        raw, err := os.ReadFile(filepath.Join(f.root, filepath.FromSlash(name)))
        if err != nil {
                return nil, err
        }
        if name == "js/plugins.js" && len(f.plugins) > 0 {
                raw = f.filterPluginsJS(raw)
        }
        return raw, nil
}

func (f *filteredSource) filterPluginsJS(raw []byte) []byte {
        text := string(raw)
        start := strings.Index(text, "[")
        end := strings.LastIndex(text, "]")
        if start < 0 || end < 0 || end <= start {
                return raw
        }
        var entries []map[string]any
        if err := json.Unmarshal([]byte(text[start:end+1]), &entries); err != nil {
                return raw
        }
        kept := entries[:0]
        for _, e := range entries {
                name, _ := e["name"].(string)
                if state, ok := f.plugins[name]; ok && state != "on" {
                        continue
                }
                kept = append(kept, e)
        }
        out, err := json.MarshalIndent(kept, "", "")
        if err != nil {
                return raw
        }
        body := strings.TrimSuffix(strings.TrimPrefix(string(out), "["), "]")
        return []byte(text[:start] + "[\n" + body + "\n];")
}

func (f *filteredSource) Mode(name string) uint32 {
        st, err := os.Stat(filepath.Join(f.root, filepath.FromSlash(name)))
        if err != nil {
                return 0o644
        }
        return uint32(st.Mode().Perm())
}

func toB64(b []byte) string {
        const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"
        out := make([]byte, 0, ((len(b)+2)/3)*4)
        for i := 0; i < len(b); i += 3 {
                var n uint32
                rem := len(b) - i
                n = uint32(b[i]) << 16
                if rem > 1 {
                        n |= uint32(b[i+1]) << 8
                }
                if rem > 2 {
                        n |= uint32(b[i+2])
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

func fromB64(s string) ([]byte, error) {
        const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"
        var out []byte
        var buf uint32
        var bits int
        for _, c := range s {
                if c == '=' || c == '\n' || c == '\r' {
                        continue
                }
                idx := -1
                for i := 0; i < len(alphabet); i++ {
                        if rune(alphabet[i]) == c {
                                idx = i
                                break
                        }
                }
                if idx < 0 {
                        return nil, fmt.Errorf("bad b64 char")
                }
                buf = buf<<6 | uint32(idx)
                bits += 6
                if bits >= 8 {
                        bits -= 8
                        out = append(out, byte(buf>>uint(bits)))
                }
        }
        return out, nil
}

func loadSigner(path string) (ed25519.PrivateKey, []byte, error) {
        raw, err := os.ReadFile(path)
        if err != nil {
                return nil, nil, err
        }
        seed, err := fromB64(strings.TrimSpace(string(raw)))
        if err != nil || len(seed) != 32 {
                return nil, nil, fmt.Errorf("bad seed")
        }
        sk := ed25519.NewKeyFromSeed(seed)
        pub, ok := sk.Public().(ed25519.PublicKey)
        if !ok {
                return nil, nil, fmt.Errorf("bad public key")
        }
        return sk, []byte(pub), nil
}

func fatal(format string, args ...any) {
        fmt.Fprintf(os.Stderr, "flwpack: "+format+"\n", args...)
        os.Exit(1)
}
