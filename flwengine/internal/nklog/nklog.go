package nklog

import (
        "crypto/cipher"
        "encoding/binary"
        "fmt"
        "io"
        "os"
        "path/filepath"
        "sync"
        "time"

        "flwengine/internal/cryptx"
)

const (
        LevelTrace    = 0
        LevelDebug    = 1
        LevelInfo     = 2
        LevelWarn     = 3
        LevelError    = 4
        LevelCritical = 5
)

const magic = "NKLG"
const version = 1
const nonceSize = 12
const tagSize = 16

var levelNames = map[int]string{
        LevelTrace: "trace", LevelDebug: "debug", LevelInfo: "info",
        LevelWarn: "warn", LevelError: "error", LevelCritical: "critical",
}

type Writer struct {
        mu      sync.Mutex
        f       *os.File
        aead    cipher.AEAD
        runID   []byte
        seq     uint64
        plain   bool
        closed  bool
        discard bool
}

type recordPlain struct {
        ts    uint64
        level uint8
        cat   string
        msg   string
}

func (r recordPlain) encode() []byte {
        buf := make([]byte, 0, 32+len(r.cat)+len(r.msg))
        buf = binary.AppendUvarint(buf, r.ts)
        buf = append(buf, r.level)
        // The category length is a single byte, so the category must be
        // truncated to 255 bytes. Writing a longer category desynchronised
        // the decoder for the rest of the file.
        cat := r.cat
        if len(cat) > 255 {
                cat = cat[:255]
        }
        buf = append(buf, uint8(len(cat)))
        buf = append(buf, cat...)
        buf = binary.AppendUvarint(buf, uint64(len(r.msg)))
        buf = append(buf, r.msg...)
        return buf
}

func New(path string, plain bool) (*Writer, error) {
        if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
                return nil, err
        }
        f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
        if err != nil {
                return nil, err
        }
        runID := cryptx.RandomBytes(16)
        w := &Writer{f: f, runID: runID, plain: plain}
        if !plain {
                key := cryptx.Derive(cryptx.LogKey(), runID, []byte("nklog.file"))
                a, err := cryptx.NewChaCha(key)
                if err != nil {
                        f.Close()
                        return nil, err
                }
                w.aead = a
        }
        hdr := make([]byte, 0, 22)
        hdr = append(hdr, magic...)
        hdr = append(hdr, version)
        // byte 5 = segment mode marker. Legacy writers always wrote 0x00,
        // which the readers now auto-detect from the first record kind byte.
        if plain {
                hdr = append(hdr, 0xff)
        } else {
                hdr = append(hdr, 0x01)
        }
        hdr = append(hdr, runID...)
        if _, err := f.Write(hdr); err != nil {
                f.Close()
                return nil, err
        }
        return w, nil
}

// Discard returns a writer that throws everything away. It is used when no
// log file could be opened, so the caller never has to nil-check the writer.
func Discard() *Writer {
        return &Writer{plain: true, discard: true}
}

func (w *Writer) writeRecord(level int, cat string, msg string) error {
        if w == nil {
                return nil
        }
        rec := recordPlain{ts: uint64(time.Now().UnixNano()), level: uint8(level), cat: cat, msg: msg}
        if w.plain || w.aead == nil {
                buf := append([]byte{0}, rec.encode()...)
                buf[0] = 0xff
                _, err := w.f.Write(buf)
                return err
        }
        w.seq++
        aad := make([]byte, 0, 24)
        aad = append(aad, w.runID...)
        aad = binary.LittleEndian.AppendUint64(aad, w.seq)
        nonce := cryptx.RandomBytes(nonceSize)
        ct := w.aead.Seal(nil, nonce, rec.encode(), aad)
        out := make([]byte, 0, 17+len(nonce)+len(ct))
        out = append(out, 0x01)
        out = binary.LittleEndian.AppendUint64(out, w.seq)
        out = binary.LittleEndian.AppendUint32(out, uint32(len(ct)))
        out = append(out, nonce...)
        out = append(out, ct...)
        _, err := w.f.Write(out)
        return err
}

func (w *Writer) Log(level int, cat string, format string, args ...any) {
        if w == nil {
                return
        }
        w.mu.Lock()
        defer w.mu.Unlock()
        if w.closed || w.discard {
                return
        }
        msg := fmt.Sprintf(format, args...)
        if len(msg) > 8192 {
                msg = msg[:8192]
        }
        w.writeRecord(level, cat, msg)
}

func (w *Writer) Trace(cat string, format string, args ...any) { w.Log(LevelTrace, cat, format, args...) }
func (w *Writer) Debug(cat string, format string, args ...any) { w.Log(LevelDebug, cat, format, args...) }
func (w *Writer) Info(cat string, format string, args ...any)  { w.Log(LevelInfo, cat, format, args...) }
func (w *Writer) Warn(cat string, format string, args ...any)  { w.Log(LevelWarn, cat, format, args...) }
func (w *Writer) Error(cat string, format string, args ...any) { w.Log(LevelError, cat, format, args...) }
func (w *Writer) Crit(cat string, format string, args ...any)  { w.Log(LevelCritical, cat, format, args...) }

func (w *Writer) Close() error {
        if w == nil {
                return nil
        }
        w.mu.Lock()
        defer w.mu.Unlock()
        if w.closed {
                return nil
        }
        w.closed = true
        if w.f == nil {
                return nil
        }
        return w.f.Close()
}

func LevelName(level int) string {
        if n, ok := levelNames[level]; ok {
                return n
        }
        return fmt.Sprintf("lvl%d", level)
}

// ReadPlain decodes a plain-mode log file.
//
// A log file may contain several concatenated segments (one header per run):
// supervisor.nklog is opened with O_APPEND and every run appends its own
// header, so a decoder that assumes a single header desynchronises on the
// second run and reports garbage ("unknown record kind 78" == the 'N' of the
// next "NKLG" magic). readLog therefore walks every segment.
func ReadPlain(path string) ([]recordPlain, error) {
        data, err := os.ReadFile(path)
        if err != nil {
                return nil, err
        }
        return readLog(data, nil)
}

// DecodeEncrypted decodes an encrypted log file. logKey is the package key
// the log key was derived from; pass nil to skip encrypted segments.
func DecodeEncrypted(data []byte, logKey []byte) ([]recordPlain, error) {
        return readLog(data, logKey)
}

// readLog walks a log file segment by segment. Each segment starts with a
// 22-byte header (magic, version, mode, 16-byte run id) and carries records
// in the format selected by the segment mode:
//
//	0xff -> plain:  ts, level, cat, msg
//	0x01 -> encrypted: seq, ctLen, nonce, ciphertext
//
// The mode byte is advisory: the actual record kind byte decides, so files
// written by older builds (mode 0x00) still decode.
func readLog(data []byte, logKey []byte) ([]recordPlain, error) {
        if len(data) < 22 {
                return nil, fmt.Errorf("short file")
        }
        if string(data[:4]) != magic {
                return nil, fmt.Errorf("bad magic")
        }
        var recs []recordPlain
        pos := 0
        for pos < len(data) {
                if pos+22 > len(data) {
                        return recs, io.ErrUnexpectedEOF
                }
                if string(data[pos:pos+4]) != magic {
                        return recs, fmt.Errorf("bad magic at %d", pos)
                }
                if data[pos+4] != version {
                        return recs, fmt.Errorf("unsupported version %d", data[pos+4])
                }
                runID := data[pos+6 : pos+22]
                pos += 22

                var aead cipher.AEAD
                if logKey != nil {
                        key := cryptx.Derive(logKey, runID, []byte("nklog.file"))
                        a, err := cryptx.NewChaCha(key)
                        if err != nil {
                                return recs, err
                        }
                        aead = a
                }

                for pos < len(data) {
                        // A following segment header starts a new run: it has
                        // its own run id and therefore its own derived key.
                        if pos+4 <= len(data) && string(data[pos:pos+4]) == magic {
                                break
                        }
                        kind := data[pos]
                        pos++
                        switch kind {
                        case 0xff:
                                rec, np, err := decodePlainRecord(data, pos)
                                if err != nil {
                                        return recs, err
                                }
                                recs = append(recs, rec)
                                pos = np
                        case 0x01:
                                if aead == nil {
                                        return recs, fmt.Errorf("encrypted segment but no log key supplied")
                                }
                                if pos+8+4+nonceSize+tagSize > len(data) {
                                        return recs, io.ErrUnexpectedEOF
                                }
                                seq := binary.LittleEndian.Uint64(data[pos : pos+8])
                                pos += 8
                                ctLen := int(binary.LittleEndian.Uint32(data[pos : pos+4]))
                                pos += 4
                                if pos+nonceSize+ctLen > len(data) {
                                        return recs, io.ErrUnexpectedEOF
                                }
                                nonce := data[pos : pos+nonceSize]
                                pos += nonceSize
                                ct := data[pos : pos+ctLen]
                                pos += ctLen
                                aad := make([]byte, 0, 24)
                                aad = append(aad, runID...)
                                aad = binary.LittleEndian.AppendUint64(aad, seq)
                                pt, err := aead.Open(nil, nonce, ct, aad)
                                if err != nil {
                                        return recs, err
                                }
                                rec, err := decodeRecord(pt)
                                if err != nil {
                                        return recs, err
                                }
                                recs = append(recs, rec)
                        default:
                                return recs, fmt.Errorf("unknown record kind %d", kind)
                        }
                }
        }
        return recs, nil
}

// decodePlainRecord decodes a plain record starting at pos and returns the
// record plus the position just after it.
func decodePlainRecord(data []byte, pos int) (recordPlain, int, error) {
        rec := recordPlain{}
        ts, np, err := decodeUvarint(data, pos)
        if err != nil {
                return rec, pos, err
        }
        rec.ts = ts
        if np >= len(data) {
                return rec, pos, io.ErrUnexpectedEOF
        }
        rec.level = data[np]
        np++
        if np >= len(data) {
                return rec, pos, io.ErrUnexpectedEOF
        }
        cl := int(data[np])
        np++
        if np+cl > len(data) {
                return rec, pos, io.ErrUnexpectedEOF
        }
        rec.cat = string(data[np : np+cl])
        np += cl
        ml, np2, err := decodeUvarint(data, np)
        if err != nil {
                return rec, pos, err
        }
        np = np2
        // The bounds check must be done in unsigned arithmetic: int(ml) is
        // negative for ml >= 2^63 and the slice below would panic.
        if uint64(np)+ml > uint64(len(data)) {
                return rec, pos, io.ErrUnexpectedEOF
        }
        rec.msg = string(data[np : np+int(ml)])
        return rec, np + int(ml), nil
}

func decodeRecord(buf []byte) (recordPlain, error) {
        rec := recordPlain{}
        p := 0
        ts, np, err := decodeUvarint(buf, p)
        if err != nil {
                return rec, err
        }
        rec.ts = ts
        p = np
        if p >= len(buf) {
                return rec, io.ErrUnexpectedEOF
        }
        rec.level = buf[p]
        p++
        if p >= len(buf) {
                return rec, io.ErrUnexpectedEOF
        }
        cl := int(buf[p])
        p++
        if p+cl > len(buf) {
                return rec, io.ErrUnexpectedEOF
        }
        rec.cat = string(buf[p : p+cl])
        p += cl
        ml, np2, err := decodeUvarint(buf, p)
        if err != nil {
                return rec, err
        }
        p = np2
        // Unsigned arithmetic: int(ml) overflows for ml >= 2^63.
        if uint64(p)+ml > uint64(len(buf)) {
                return rec, io.ErrUnexpectedEOF
        }
        rec.msg = string(buf[p : p+int(ml)])
        return rec, nil
}

func decodeUvarint(data []byte, pos int) (uint64, int, error) {
        var x uint64
        var s uint
        for i := 0; i < 10; i++ {
                if pos+i >= len(data) {
                        return 0, pos, io.ErrUnexpectedEOF
                }
                b := data[pos+i]
                if b < 0x80 {
                        return x | uint64(b)<<s, pos + i + 1, nil
                }
                x |= uint64(b&0x7f) << s
                s += 7
        }
        return 0, pos, fmt.Errorf("uvarint overflow")
}
