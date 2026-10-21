package nkpkg

import (
        "bytes"
        "compress/flate"
        "crypto/ed25519"
        "crypto/sha256"
        "encoding/binary"
        "encoding/hex"
        "encoding/json"
        "fmt"
        "io"
        "os"
        "path"
        "sort"
        "strings"
        "time"

        "flwengine/internal/cryptx"
)

const (
        Magic     = "NKPKG"
        VersionV1 = 1
)

const (
        FlagEncrypted = 0x01
        FlagSigned    = 0x02
)

const (
        MethodStore   = 0
        MethodDeflate = 1
)

const headerSize = 5 + 2 + 1 + 4 + 16

type FileEntry struct {
        Name   string `json:"n"`
        Offset int64  `json:"o"`
        Size   int64  `json:"sz"`
        CSize  int64  `json:"cs"`
        Method uint8  `json:"m"`
        Mode   uint32 `json:"mode,omitempty"`
        SHA256 string `json:"h,omitempty"`
        Nonce  string `json:"nn,omitempty"`
}

type Manifest struct {
        ID        string            `json:"id"`
        Kind      string            `json:"kind"`
        Title     string            `json:"title"`
        Version   string            `json:"version"`
        Entry     string            `json:"entry,omitempty"`
        BuildType string            `json:"buildType,omitempty"`
        Platform  string            `json:"platform,omitempty"`
        Created   int64             `json:"created"`
        Meta      map[string]string `json:"meta,omitempty"`
}

type Package struct {
        Manifest    Manifest
        Files       []FileEntry
        flags       uint8
        pkgID       [16]byte
        data        []byte
        payloadBase int64
}

type indexJSON struct {
        Manifest Manifest    `json:"manifest"`
        Files    []FileEntry `json:"files"`
}

func (p *Package) Flags() uint8 {
        return p.flags
}

func (p *Package) Encrypted() bool {
        return p.flags&FlagEncrypted != 0
}

func (p *Package) Signed() bool {
        return p.flags&FlagSigned != 0
}

func (p *Package) FileNames() []string {
        names := make([]string, 0, len(p.Files))
        for _, f := range p.Files {
                names = append(names, f.Name)
        }
        return names
}

func (p *Package) Has(name string) bool {
        for _, f := range p.Files {
                if f.Name == name {
                        return true
                }
        }
        return false
}

func (p *Package) ReadFile(name string, key []byte) ([]byte, error) {
        entry := p.find(name)
        if entry == nil {
                return nil, fmt.Errorf("file not found: %s", name)
        }
        start := entry.Offset + p.payloadBase
        end := start + entry.CSize
        if p.payloadBase == 0 && entry.Offset > 0 && end > int64(len(p.data)) {
                return nil, fmt.Errorf("bad offsets for %s", name)
        }
        if start < 0 || end > int64(len(p.data)) {
                return nil, fmt.Errorf("bad offsets for %s", name)
        }
        return p.decodeEntry(entry, p.data[start:end], key)
}



func (p *Package) ReadFileFrom(path, name string, key []byte) ([]byte, error) {
        entry := p.find(name)
        if entry == nil {
                return nil, fmt.Errorf("file not found: %s", name)
        }
        start := entry.Offset + p.payloadBase
        end := start + entry.CSize
        if start < 0 || end <= start || end-start > 512<<20 {
                return nil, fmt.Errorf("bad offsets for %s", name)
        }
        f, err := os.Open(path)
        if err != nil {
                return nil, err
        }
        defer f.Close()
        raw := make([]byte, end-start)
        if _, err := f.ReadAt(raw, start); err != nil {
                return nil, fmt.Errorf("read failed for %s: %w", name, err)
        }
        return p.decodeEntry(entry, raw, key)
}

func (p *Package) decodeEntry(entry *FileEntry, raw []byte, key []byte) ([]byte, error) {
        if p.Encrypted() {
                if key == nil {
                        return nil, fmt.Errorf("package encrypted but no key")
                }
                nonce, err := hex.DecodeString(entry.Nonce)
                if err != nil || len(nonce) != 12 {
                        return nil, fmt.Errorf("bad nonce for %s", entry.Name)
                }
                fkey := cryptx.Derive(key, p.pkgID[:], []byte("nkpkg.file:"+entry.Name))
                aead, err := cryptx.NewAEAD(fkey)
                if err != nil {
                        return nil, err
                }
                pt, err := aead.Open(nil, nonce, raw, []byte(entry.SHA256))
                if err != nil {
                        return nil, fmt.Errorf("decrypt failed for %s", entry.Name)
                }
                raw = pt
        }
        if entry.Method == MethodDeflate {
                fr := flate.NewReader(bytes.NewReader(raw))
                defer fr.Close()
                out, err := io.ReadAll(fr)
                if err != nil {
                        return nil, fmt.Errorf("inflate failed for %s: %w", entry.Name, err)
                }
                raw = out
        }
        if entry.SHA256 != "" {
                sum := sha256.Sum256(raw)
                if hex.EncodeToString(sum[:]) != entry.SHA256 {
                        return nil, fmt.Errorf("hash mismatch for %s", entry.Name)
                }
        }
        return raw, nil
}

func (p *Package) find(name string) *FileEntry {
        for i := range p.Files {
                if p.Files[i].Name == name {
                        return &p.Files[i]
                }
        }
        return nil
}

func (p *Package) VerifySignature(pub ed25519.PublicKey) error {
        if !p.Signed() {
                return fmt.Errorf("package not signed")
        }
        if len(pub) == 0 {
                return fmt.Errorf("no public key")
        }
        trailer := p.signatureTrailerSize()
        sigLen := int(binary.LittleEndian.Uint16(p.data[len(p.data)-2:]))
        sigStart := len(p.data) - trailer
        if sigStart < 0 || sigLen+2 != trailer {
                return fmt.Errorf("bad signature block")
        }
        digest := p.digestBytes(sigStart)
        if !ed25519.Verify(pub, digest, p.data[sigStart:sigStart+sigLen]) {
                return fmt.Errorf("signature invalid")
        }
        return nil
}

func (p *Package) signatureTrailerSize() int {
        if !p.Signed() {
                return 0
        }
        return int(binary.LittleEndian.Uint16(p.data[len(p.data)-2:])) + 2
}

func (p *Package) digestBytes(sigStart int) []byte {
        h := sha256.New()
        h.Write(p.data[:sigStart])
        binary.Write(h, binary.LittleEndian, p.flags)
        h.Write(p.pkgID[:])
        for _, f := range p.Files {
                h.Write([]byte(f.Name))
                h.Write([]byte(f.SHA256))
                binary.Write(h, binary.LittleEndian, f.Size)
        }
        return h.Sum(nil)
}

func (p *Package) Bytes() []byte {
        return p.data
}

func Save(p *Package, path string) error {
        return os.WriteFile(path, p.data, 0o644)
}

func Open(path string, key []byte) (*Package, error) {
        data, err := os.ReadFile(path)
        if err != nil {
                return nil, err
        }
        return Parse(data, key)
}





func OpenMeta(path string, key []byte) (*Package, error) {
        f, err := os.Open(path)
        if err != nil {
                return nil, err
        }
        defer f.Close()
        head := make([]byte, headerSize)
        if _, err := io.ReadFull(f, head); err != nil {
                return nil, fmt.Errorf("short package")
        }
        if string(head[:5]) != Magic {
                return nil, fmt.Errorf("bad magic")
        }
        ver := binary.LittleEndian.Uint16(head[5:7])
        if ver != VersionV1 {
                return nil, fmt.Errorf("unsupported version %d", ver)
        }
        flags := head[7]
        idxLen := int(binary.LittleEndian.Uint32(head[8:12]))
        var pkgID [16]byte
        copy(pkgID[:], head[12:headerSize])
        if idxLen < 0 || idxLen > 512<<20 {
                return nil, fmt.Errorf("bad index length")
        }
        indexRaw := make([]byte, idxLen)
        if _, err := io.ReadFull(f, indexRaw); err != nil {
                return nil, fmt.Errorf("short index")
        }
        p := &Package{flags: flags, pkgID: pkgID, payloadBase: int64(headerSize + idxLen)}
        if flags&FlagEncrypted != 0 {
                if key == nil {
                        return nil, fmt.Errorf("package encrypted but no key")
                }
                if len(indexRaw) < 12+16 {
                        return nil, fmt.Errorf("short sealed index")
                }
                nonce := indexRaw[:12]
                ikey := cryptx.Derive(key, pkgID[:], []byte("nkpkg.index"))
                aead, err := cryptx.NewAEAD(ikey)
                if err != nil {
                        return nil, err
                }
                pt, err := aead.Open(nil, nonce, indexRaw[12:], []byte("nkpkg.index"))
                if err != nil {
                        return nil, fmt.Errorf("index decrypt failed")
                }
                indexRaw = pt
        }
        var idx indexJSON
        if err := json.Unmarshal(indexRaw, &idx); err != nil {
                return nil, fmt.Errorf("index parse failed: %w", err)
        }
        p.Manifest = idx.Manifest
        p.Files = idx.Files
        return p, nil
}

func Parse(data []byte, key []byte) (*Package, error) {
        if len(data) < headerSize {
                return nil, fmt.Errorf("short package")
        }
        if string(data[:5]) != Magic {
                return nil, fmt.Errorf("bad magic")
        }
        ver := binary.LittleEndian.Uint16(data[5:7])
        if ver != VersionV1 {
                return nil, fmt.Errorf("unsupported version %d", ver)
        }
        flags := data[7]
        idxLen := int(binary.LittleEndian.Uint32(data[8:12]))
        var pkgID [16]byte
        copy(pkgID[:], data[12:headerSize])
        idxStart := headerSize
        idxEnd := idxStart + idxLen
        if idxEnd > len(data) {
                return nil, fmt.Errorf("bad index length")
        }
        p := &Package{flags: flags, pkgID: pkgID, data: data, payloadBase: int64(idxEnd)}
        indexRaw := data[idxStart:idxEnd]
        if flags&FlagEncrypted != 0 {
                if key == nil {
                        return nil, fmt.Errorf("package encrypted but no key")
                }
                if len(indexRaw) < 12+16 {
                        return nil, fmt.Errorf("short sealed index")
                }
                nonce := indexRaw[:12]
                ikey := cryptx.Derive(key, pkgID[:], []byte("nkpkg.index"))
                aead, err := cryptx.NewAEAD(ikey)
                if err != nil {
                        return nil, err
                }
                pt, err := aead.Open(nil, nonce, indexRaw[12:], []byte("nkpkg.index"))
                if err != nil {
                        return nil, fmt.Errorf("index decrypt failed")
                }
                indexRaw = pt
        }
        var idx indexJSON
        if err := json.Unmarshal(indexRaw, &idx); err != nil {
                return nil, fmt.Errorf("index parse failed: %w", err)
        }
        p.Manifest = idx.Manifest
        p.Files = idx.Files
        return p, nil
}

type PackOptions struct {
        Manifest Manifest
        Encrypt  bool
        Sign     bool
        Key      []byte
        Signer   ed25519.PrivateKey
}

type PackSource interface {
        Names() ([]string, error)
        Open(name string) ([]byte, error)
        Mode(name string) uint32
}

type DirSource string

func (d DirSource) Names() ([]string, error) {
        var names []string
        root := string(d)
        err := walkFiles(root, "", func(rel string) {
                names = append(names, rel)
        })
        if err != nil {
                return nil, err
        }
        sort.Strings(names)
        return names, nil
}

func (d DirSource) Open(name string) ([]byte, error) {
        return os.ReadFile(path.Join(string(d), Clean(name)))
}

func (d DirSource) Mode(name string) uint32 {
        st, err := os.Stat(path.Join(string(d), Clean(name)))
        if err != nil {
                return 0o644
        }
        return uint32(st.Mode().Perm())
}

func walkFiles(root, rel string, fn func(string)) error {
        entries, err := os.ReadDir(path.Join(root, rel))
        if err != nil {
                return err
        }
        for _, e := range entries {
                name := e.Name()
                if name == ".git" || name == "node_modules" {
                        continue
                }
                child := name
                if rel != "" {
                        child = rel + "/" + name
                }
                if e.IsDir() {
                        if err := walkFiles(root, child, fn); err != nil {
                                return err
                        }
                } else {
                        fn(child)
                }
        }
        return nil
}

func Clean(p string) string {
        parts := strings.Split(p, "/")
        out := make([]string, 0, len(parts))
        for _, part := range parts {
                if part == "" || part == "." || part == ".." {
                        continue
                }
                out = append(out, part)
        }
        return strings.Join(out, "/")
}

func Pack(src PackSource, opts PackOptions) (*Package, error) {
        names, err := src.Names()
        if err != nil {
                return nil, err
        }
        if opts.Manifest.Created == 0 {
                opts.Manifest.Created = time.Now().Unix()
        }
        p := &Package{Manifest: opts.Manifest}
        if opts.Encrypt {
                p.flags |= FlagEncrypted
        }
        if opts.Sign {
                p.flags |= FlagSigned
        }
        idSeed := opts.Manifest.ID + "|" + opts.Manifest.Kind
        copy(p.pkgID[:], cryptx.Derive([]byte(idSeed), []byte("nkpkg.id"), []byte(opts.Manifest.ID)))
        var payload bytes.Buffer
        for _, name := range names {
                raw, err := src.Open(name)
                if err != nil {
                        return nil, fmt.Errorf("read %s: %w", name, err)
                }
                sum := sha256.Sum256(raw)
                sumHex := hex.EncodeToString(sum[:])
                store := raw
                var method uint8 = MethodStore
                var cb bytes.Buffer
                fw, _ := flate.NewWriter(&cb, flate.BestSpeed)
                fw.Write(raw)
                fw.Close()
                if cb.Len() < len(raw) {
                        store = cb.Bytes()
                        method = MethodDeflate
                }
                nonceHex := ""
                if opts.Encrypt {
                        if opts.Key == nil {
                                return nil, fmt.Errorf("encrypt requested but no key")
                        }
                        nonce := cryptx.RandomBytes(12)
                        fkey := cryptx.Derive(opts.Key, p.pkgID[:], []byte("nkpkg.file:"+name))
                        aead, err := cryptx.NewAEAD(fkey)
                        if err != nil {
                                return nil, err
                        }
                        store = aead.Seal(nil, nonce, store, []byte(sumHex))
                        nonceHex = hex.EncodeToString(nonce)
                }
                entry := FileEntry{
                        Name:   name,
                        Offset: int64(payload.Len()),
                        Size:   int64(len(raw)),
                        CSize:  int64(len(store)),
                        Method: method,
                        Mode:   src.Mode(name),
                        SHA256: sumHex,
                        Nonce:  nonceHex,
                }
                payload.Write(store)
                p.Files = append(p.Files, entry)
        }
        idx := indexJSON{Manifest: p.Manifest, Files: p.Files}
        indexRaw, err := json.Marshal(idx)
        if err != nil {
                return nil, err
        }
        if opts.Encrypt {
                if opts.Key == nil {
                        return nil, fmt.Errorf("encrypt requested but no key")
                }
                nonce := cryptx.RandomBytes(12)
                ikey := cryptx.Derive(opts.Key, p.pkgID[:], []byte("nkpkg.index"))
                aead, err := cryptx.NewAEAD(ikey)
                if err != nil {
                        return nil, err
                }
                indexRaw = append(nonce, aead.Seal(nil, nonce, indexRaw, []byte("nkpkg.index"))...)
        }
        var out bytes.Buffer
        out.WriteString(Magic)
        binary.Write(&out, binary.LittleEndian, uint16(VersionV1))
        out.WriteByte(p.flags)
        binary.Write(&out, binary.LittleEndian, uint32(len(indexRaw)))
        out.Write(p.pkgID[:])
        out.Write(indexRaw)
        out.Write(payload.Bytes())
        if opts.Sign {
                if opts.Signer == nil {
                        return nil, fmt.Errorf("sign requested but no signer")
                }
                p.data = out.Bytes()
                digest := p.digestBytes(len(p.data))
                sig := ed25519.Sign(opts.Signer, digest)
                out.Write(sig)
                binary.Write(&out, binary.LittleEndian, uint16(len(sig)))
        }
        p.data = out.Bytes()
        return p, nil
}
