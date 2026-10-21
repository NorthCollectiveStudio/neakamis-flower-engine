package cryptx

import (
        "crypto/aes"
        "crypto/cipher"
        "crypto/ed25519"
        "crypto/hmac"
        "crypto/rand"
        "crypto/sha256"
        "crypto/subtle"
        "encoding/base64"
        "encoding/binary"
        "errors"
        "fmt"
        "io"
        "sync"

        "golang.org/x/crypto/chacha20poly1305"
)

var shardA = []byte{
        0x6e, 0x3b, 0xa7, 0xf2, 0x0c, 0x98, 0x4d, 0x11, 0x77, 0xd5, 0xe0, 0x2a, 0x9f, 0x66, 0x1c, 0x44,
        0xd8, 0x13, 0x51, 0xfe, 0x83, 0x2c, 0x60, 0xb9, 0x17, 0x0e, 0xa4, 0x5d, 0xc6, 0x79, 0x22, 0x8b,
}

var shardB = []byte{
        0x2f, 0x91, 0x46, 0x08, 0xd3, 0x7a, 0xec, 0x59, 0x10, 0x66, 0x2d, 0x97, 0xc4, 0x3b, 0xf0, 0x85,
        0x01, 0x9e, 0x64, 0x33, 0x5a, 0xd7, 0x88, 0x0f, 0x76, 0x24, 0xba, 0x4e, 0x31, 0xc0, 0x9d, 0xe5,
}

var shardC = []byte{
        0x84, 0x12, 0xf9, 0x66, 0x2d, 0x50, 0x87, 0x0b, 0xc9, 0x21, 0x7e, 0x63, 0x14, 0xa8, 0x3c, 0xf7,
        0x5b, 0x90, 0x02, 0x7d, 0xe1, 0x48, 0x93, 0x36, 0xaf, 0x6c, 0x19, 0xda, 0x70, 0x25, 0xbe, 0x58,
}

var prodBlobA = ""
var prodBlobB = ""
var signPubB64 = ""

type KeyRing struct {
        mu        sync.RWMutex
        logMaster []byte
        pkgMaster []byte
        sigPub    ed25519.PublicKey
        canary    [32]byte
        generation uint64
}

var ring = &KeyRing{}

func Init() error {
        ring.mu.Lock()
        defer ring.mu.Unlock()
        if ring.logMaster != nil {
                return nil
        }
        ring.logMaster = combine(shardA, shardB, shardC, []byte("flw.nklog.v1"))
        ring.pkgMaster = combine(shardA, shardC, shardB, []byte("flw.nkpkg.v1"))
        ring.generation = 1
        if _, err := io.ReadFull(rand.Reader, ring.canary[:]); err != nil {
                return err
        }
        if signPubB64 != "" {
                raw, err := base64.StdEncoding.DecodeString(signPubB64)
                if err != nil {
                        return err
                }
                if len(raw) != ed25519.PublicKeySize {
                        return errors.New("bad sign pubkey size")
                }
                ring.sigPub = ed25519.PublicKey(raw)
        }
        if prodBlobA != "" && prodBlobB != "" {
                a, err := base64.StdEncoding.DecodeString(prodBlobA)
                if err != nil {
                        return err
                }
                b, err := base64.StdEncoding.DecodeString(prodBlobB)
                if err != nil {
                        return err
                }
                if len(a) == 64 && len(b) == 64 {
                        k := make([]byte, 32)
                        for i := 0; i < 32; i++ {
                                k[i] = a[i] ^ a[32+i] ^ b[i] ^ b[32+i]
                        }
                        ring.pkgMaster = k
                        ring.logMaster = derive(k, []byte("nklog"))
                }
        }
        return nil
}

func combine(a, b, c, domain []byte) []byte {
        out := make([]byte, 32)
        for i := 0; i < 32; i++ {
                out[i] = a[i] ^ b[i] ^ c[i]
        }
        return derive(out, domain)
}

func derive(key, info []byte) []byte {
        m := hmac.New(sha256.New, key)
        m.Write(info)
        return m.Sum(nil)
}

func Derive(key []byte, salt []byte, info []byte) []byte {
        prk := hmac.New(sha256.New, key)
        prk.Write(salt)
        base := prk.Sum(nil)
        ex := hmac.New(sha256.New, base)
        ex.Write(info)
        return ex.Sum(nil)
}

func LogKey() []byte {
        ring.mu.RLock()
        defer ring.mu.RUnlock()
        if ring.logMaster == nil {
                panic("cryptx not initialized")
        }
        return ring.logMaster
}

func PkgKey() []byte {
        ring.mu.RLock()
        defer ring.mu.RUnlock()
        if ring.pkgMaster == nil {
                panic("cryptx not initialized")
        }
        return ring.pkgMaster
}

// PkgKeyOrNil returns the package key or nil when the key ring is not
// initialized yet. It is used where the key is optional.
func PkgKeyOrNil() []byte {
        ring.mu.RLock()
        defer ring.mu.RUnlock()
        return ring.pkgMaster
}

func SignPub() ed25519.PublicKey {
        ring.mu.RLock()
        defer ring.mu.RUnlock()
        return ring.sigPub
}

func NewAEAD(key []byte) (cipher.AEAD, error) {
        if len(key) != 32 {
                return nil, fmt.Errorf("key size %d", len(key))
        }
        blk, err := aes.NewCipher(key)
        if err != nil {
                return nil, err
        }
        return cipher.NewGCM(blk)
}

func NewChaCha(key []byte) (cipher.AEAD, error) {
        if len(key) != 32 {
                return nil, fmt.Errorf("key size %d", len(key))
        }
        return chacha20poly1305.New(key)
}

func SealMessage(key []byte, plaintext []byte, aad []byte) ([]byte, error) {
        a, err := NewAEAD(key)
        if err != nil {
                return nil, err
        }
        nonce := make([]byte, a.NonceSize())
        if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
                return nil, err
        }
        out := a.Seal(nil, nonce, plaintext, aad)
        return append(nonce, out...), nil
}

func OpenMessage(key []byte, data []byte, aad []byte) ([]byte, error) {
        a, err := NewAEAD(key)
        if err != nil {
                return nil, err
        }
        if len(data) < a.NonceSize()+a.Overhead() {
                return nil, errors.New("ciphertext too short")
        }
        nonce, ct := data[:a.NonceSize()], data[a.NonceSize():]
        return a.Open(nil, nonce, ct, aad)
}

func VerifySealed(key []byte, data []byte, aad []byte) bool {
        _, err := OpenMessage(key, data, aad)
        return err == nil
}

func RandomBytes(n int) []byte {
        b := make([]byte, n)
        io.ReadFull(rand.Reader, b)
        return b
}

func ConstantEqual(a, b []byte) bool {
        return subtle.ConstantTimeCompare(a, b) == 1
}

func Uint64Le(v uint64) []byte {
        b := make([]byte, 8)
        binary.LittleEndian.PutUint64(b, v)
        return b
}

func Zero(b []byte) {
        for i := range b {
                b[i] = 0
        }
}

var lockFn func([]byte) error

func SetLocker(fn func([]byte) error) {
        ring.mu.Lock()
        lockFn = fn
        ring.mu.Unlock()
}

func LockMasters() error {
        if lockFn == nil {
                return errors.New("no locker installed")
        }
        ring.mu.Lock()
        defer ring.mu.Unlock()
        var firstErr error
        for _, b := range [][]byte{ring.logMaster, ring.pkgMaster} {
                if len(b) == 0 {
                        continue
                }
                if err := lockFn(b); err != nil && firstErr == nil {
                        firstErr = err
                }
        }
        return firstErr
}
