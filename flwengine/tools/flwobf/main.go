package main

import (
        "encoding/hex"
        "flag"
        "fmt"
        "os"
        "path/filepath"
        "strings"
        "unicode/utf8"
)

type token struct {
        kind string
        text string
}

func main() {
        dir := flag.String("dir", "", "directory to obfuscate")
        minLen := flag.Int("minlen", 6, "minimum string length to encrypt")
        skip := flag.String("skip", "flwbridge.js,plugins.js", "files to skip")
        flag.Parse()
        if *dir == "" {
                fmt.Fprintln(os.Stderr, "flwobf: dir required")
                os.Exit(1)
        }
        skipMap := map[string]bool{}
        for _, s := range strings.Split(*skip, ",") {
                skipMap[strings.TrimSpace(s)] = true
        }
        count := 0
        err := filepath.Walk(*dir, func(path string, info os.FileInfo, err error) error {
                if err != nil {
                        return err
                }
                if info.IsDir() {
                        if info.Name() == ".git" || info.Name() == "node_modules" {
                                return filepath.SkipDir
                        }
                        return nil
                }
                if !strings.HasSuffix(path, ".js") && !strings.HasSuffix(path, ".mjs") {
                        return nil
                }
                if skipMap[info.Name()] {
                        return nil
                }
                rel, relErr := filepath.Rel(*dir, path)
                if relErr == nil {
                        rel = filepath.ToSlash(rel)
                        if strings.HasPrefix(rel, "js/libs/") || strings.Contains(rel, "/libs/") {
                                return nil
                        }
                }
                if strings.HasPrefix(info.Name(), "rmmz_") || strings.HasSuffix(info.Name(), ".min.js") {
                        return nil
                }
                raw, err := os.ReadFile(path)
                if err != nil {
                        return err
                }
                tag := fileTag(path)
                out, changed := obfuscate(string(raw), *minLen, tag)
                if !changed {
                        return nil
                }
                if !utf8.ValidString(out) {
                        return nil
                }
                if err := os.WriteFile(path, []byte(out), info.Mode()); err != nil {
                        return err
                }
                count++
                fmt.Printf("obf %s\n", path)
                return nil
        })
        if err != nil {
                fmt.Fprintf(os.Stderr, "flwobf: %v\n", err)
                os.Exit(1)
        }
        fmt.Printf("obfuscated %d files\n", count)
}

func fileTag(path string) string {
        h := uint32(2166136261)
        for i := 0; i < len(path); i++ {
                h = (h ^ uint32(path[i])) * 16777619
        }
        return fmt.Sprintf("%04x", h&0xffff)
}

func obfuscate(src string, minLen int, tag string) (string, bool) {
        var strings_ []string
        var out strings.Builder
        changed := false
        i := 0
        n := len(src)
        prevMeaning := byte(0)
        for i < n {
                c := src[i]
                if c == '/' && i+1 < n && src[i+1] == '/' {
                        j := strings.IndexByte(src[i:], '\n')
                        if j < 0 {
                                i = n
                        } else {
                                out.WriteByte('\n')
                                i += j + 1
                        }
                        changed = true
                        continue
                }
                if c == '/' && i+1 < n && src[i+1] == '*' {
                        j := strings.Index(src[i+2:], "*/")
                        if j < 0 {
                                i = n
                        } else {
                                chunk := src[i+2 : i+2+j]
                                out.WriteByte(' ')
                                if strings.Contains(chunk, "\n") {
                                        out.WriteByte('\n')
                                }
                                i += 2 + j + 2
                        }
                        changed = true
                        continue
                }
                if c == '"' || c == '\'' {
                        j, lit, ok := readStringLit(src, i)
                        if !ok {
                                out.WriteByte(c)
                                i++
                                prevMeaning = c
                                continue
                        }
                        nextCh := byte(0)
                        for k := i + j; k < n; k++ {
                                if src[k] != ' ' && src[k] != '\t' {
                                        nextCh = src[k]
                                        break
                                }
                        }
                        if nextCh == ':' || nextCh == '=' || nextCh == '(' || nextCh == '[' {
                                out.WriteString(src[i : i+j])
                                prevMeaning = '"'
                                i += j
                                continue
                        }
                        if shouldEncrypt(lit, minLen, prevMeaning, strings_) {
                                idx := len(strings_)
                                strings_ = append(strings_, lit)
                                lastByte := byte(0)
                                if out.Len() > 0 {
                                        lastByte = out.String()[out.Len()-1]
                                }
                                if isIdentByte(lastByte) {
                                        out.WriteByte(' ')
                                }
                                fmt.Fprintf(&out, "__flw%s(%d)", tag, idx)
                                changed = true
                        } else {
                                out.WriteString(src[i : i+j])
                        }
                        prevMeaning = '"'
                        i += j
                        continue
                }
                if c == '`' {
                        j, ok := readTemplateLit(src, i)
                        if ok {
                                out.WriteString(src[i : i+j])
                                i += j
                                prevMeaning = '`'
                                continue
                        }
                }
                if c == '/' && regexAllowed(prevMeaning) {
                        j, ok := readRegex(src, i)
                        if ok {
                                out.WriteString(src[i : i+j])
                                i += j
                                prevMeaning = '/'
                                continue
                        }
                }
                if !(c == ' ' || c == '\t' || c == '\n' || c == '\r') {
                        prevMeaning = c
                }
                out.WriteByte(c)
                i++
        }
        if !changed || len(strings_) == 0 {
                return src, false
        }
        boot := buildBootstrap(strings_, tag)
        return boot + out.String(), true
}

func prevMeanific(c byte, prev *byte) {
        *prev = c
}

func shouldEncrypt(lit string, minLen int, prev byte, _ []string) bool {
        if len(lit) < minLen {
                return false
        }
        if lit == "use strict" {
                return false
        }
        if strings.Contains(lit, "${") {
                return false
        }
        if prev == '.' {
                return false
        }
        if strings.ContainsAny(lit, "\\\n\r") {
                return false
        }
        return true
}

func readStringLit(src string, i int) (int, string, bool) {
        quote := src[i]
        j := i + 1
        n := len(src)
        for j < n {
                c := src[j]
                if c == '\\' {
                        j += 2
                        continue
                }
                if c == quote {
                        raw := src[i+1 : j]
                        return j + 1 - i, raw, true
                }
                if c == '\n' {
                        return 0, "", false
                }
                j++
        }
        return 0, "", false
}

func readTemplateLit(src string, i int) (int, bool) {
        j := i + 1
        n := len(src)
        for j < n {
                c := src[j]
                if c == '\\' {
                        j += 2
                        continue
                }
                if c == '`' {
                        return j + 1 - i, true
                }
                j++
        }
        return 0, false
}

func readRegex(src string, i int) (int, bool) {
        j := i + 1
        n := len(src)
        inClass := false
        for j < n {
                c := src[j]
                if c == '\\' {
                        j += 2
                        continue
                }
                if c == '\n' {
                        return 0, false
                }
                if c == '[' {
                        inClass = true
                } else if c == ']' {
                        inClass = false
                } else if c == '/' && !inClass {
                        k := j + 1
                        for k < n && isRegexFlag(src[k]) {
                                k++
                        }
                        return k - i, true
                }
                j++
        }
        return 0, false
}

func isRegexFlag(c byte) bool {
        return c >= 'a' && c <= 'z'
}

func regexAllowed(prev byte) bool {
        switch prev {
        case 0, '(', ',', '=', ':', '[', '!', '&', '|', '?', '{', '}', ';', '+', '-', '*', '%', '<', '>', '~', '^':
                return true
        }
        return false
}

func buildBootstrap(strs []string, tag string) string {
        key := deriveKey(strs)
        var enc []string
        for idx, s := range strs {
                enc = append(enc, encodeString(s, key, idx))
        }
        var sb strings.Builder
        sb.WriteString("var __flw" + tag + "=(function(){var K=[")
        for i, k := range key {
                if i > 0 {
                        sb.WriteByte(',')
                }
                fmt.Fprintf(&sb, "%d", k)
        }
        sb.WriteString("];var S=[")
        for i, e := range enc {
                if i > 0 {
                        sb.WriteByte(',')
                }
                sb.WriteByte('"')
                sb.WriteString(e)
                sb.WriteByte('"')
        }
        sb.WriteString("];return function(i){var s=S[i],o=\"\";for(var j=0;j<s.length;j+=2){var c=parseInt(s.substr(j,2),16);o+=String.fromCharCode(c^K[(j>>1)%K.length]^((i*73+(j>>1)*17)&255));}try{return decodeURIComponent(escape(o))}catch(e){return o}};})();")
        return sb.String()
}

func deriveKey(strs []string) []byte {
        key := make([]byte, 16)
        seed := 0
        for i, s := range strs {
                for j := 0; j < len(s); j++ {
                        seed = seed*131 + int(s[j]) + i*17 + j*7
                }
        }
        r := uint32(seed)
        for i := range key {
                r = r*1664525 + 1013904223
                key[i] = byte(r>>24) ^ byte(r>>16) ^ 0x5a
        }
        if key[0] == 0 {
                key[0] = 0x37
        }
        return key
}

func encodeString(s string, key []byte, idx int) string {
        var b strings.Builder
        for j := 0; j < len(s); j++ {
                c := s[j] ^ key[j%len(key)] ^ byte((idx*73+j*17)&255)
                fmt.Fprintf(&b, "%02x", c)
        }
        return b.String()
}

var _ = hex.EncodeToString

func isIdentByte(c byte) bool {
        return c == '_' || c == '$' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
}
