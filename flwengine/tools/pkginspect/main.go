package main

import (
        "encoding/base64"
        "flag"
        "fmt"
        "os"
        "strings"

        "flwengine/internal/cryptx"
        "flwengine/internal/nkpkg"
)

// pkginspect shows the content of a package. With -key the package can be
// opened (a production package is encrypted) and the files can be read.
func main() {
        keyFile := flag.String("key", "", "key file of an encrypted package (base64)")
        list := flag.Bool("list", false, "list all the files of the package")
        flag.Parse()
        if err := cryptx.Init(); err != nil {
                fmt.Println("cryptx init err:", err)
                return
        }
        if flag.NArg() < 1 {
                fmt.Println("usage: pkginspect [-key file] [-list] <package> [file]")
                return
        }
        path := flag.Arg(0)
        var key []byte
        if *keyFile != "" {
                raw, err := os.ReadFile(*keyFile)
                if err != nil {
                        fmt.Println("key err:", err)
                        return
                }
                key, err = base64.StdEncoding.DecodeString(strings.TrimSpace(string(raw)))
                if err != nil || len(key) != 32 {
                        fmt.Println("bad key file")
                        return
                }
        }
        p, err := nkpkg.Open(path, key)
        if err != nil {
                fmt.Println("open err:", err)
                return
        }
        fmt.Println("files:", len(p.Files), "flags:", p.Flags(), "enc:", p.Encrypted(), "signed:", p.Signed())
        if *list {
                for _, f := range p.Files {
                        fmt.Println(" ", f.Name, "sz:", f.Size)
                }
                return
        }
        for i, f := range p.Files {
                if i < 5 {
                        fmt.Println(" ", f.Name, "off:", f.Offset, "sz:", f.Size, "cs:", f.CSize, "m:", f.Method)
                }
        }
        name := "index.html"
        if flag.NArg() > 1 {
                name = flag.Arg(1)
        }
        data, err := p.ReadFile(name, key)
        fmt.Println("read", name, "err:", err, "len:", len(data))
}
