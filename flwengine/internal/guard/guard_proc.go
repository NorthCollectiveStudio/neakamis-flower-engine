//go:build linux





package guard

import (
        "bytes"
        "os"
        "syscall"
        "unsafe"
)

func debuggerPresent() string {
        tp := tracerPid()
        if tp != 0 {
                return "tracerpid"
        }
        if st := procState(); st == 't' {
                return "tracing stop"
        }
        return ""
}

func tracerPid() int {
        raw, err := os.ReadFile("/proc/self/status")
        if err != nil {
                return 0
        }
        for _, line := range bytes.Split(raw, []byte("\n")) {
                if bytes.HasPrefix(line, []byte("TracerPid:")) {
                        v := 0
                        for _, c := range line[len("TracerPid:"):] {
                                if c >= '0' && c <= '9' {
                                        v = v*10 + int(c-'0')
                                }
                        }
                        return v
                }
        }
        return 0
}

func procState() byte {
        raw, err := os.ReadFile("/proc/self/stat")
        if err != nil {
                return 0
        }
        i := bytes.LastIndexByte(raw, ')')
        if i < 0 || i+2 >= len(raw) {
                return 0
        }
        return raw[i+2]
}

func lockRegion(b []byte) error {
        if len(b) == 0 {
                return nil
        }
        hdr := (*struct {
                data uintptr
                len  int
                cap  int
        })(unsafe.Pointer(&b))
        base := hdr.data &^ 4095
        size := (hdr.data + uintptr(hdr.len) - base + 4095) &^ 4095
        _, _, errno := syscall.Syscall6(syscall.SYS_MLOCK, base, size, 0, 0, 0, 0)
        if errno != 0 {
                return errno
        }
        return nil
}
