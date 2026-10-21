package gamehost

import (
        "os"
        "path/filepath"
        "sort"
        "strings"
)

type VFS struct {
        Root string
}

func NewVFS(root string) *VFS {
        return &VFS{Root: root}
}




func MigrateSaveDir(root string) {
        nested := filepath.Join(root, "save", "save")
        st, err := os.Stat(nested)
        if err != nil || !st.IsDir() {
                return
        }
        entries, err := os.ReadDir(nested)
        if err != nil {
                return
        }
        moved := 0
        for _, e := range entries {
                src := filepath.Join(nested, e.Name())
                dst := filepath.Join(root, "save", e.Name())
                if _, err := os.Stat(dst); err == nil {
                        
                        os.Remove(src)
                        continue
                }
                if err := os.Rename(src, dst); err == nil {
                        moved++
                }
        }
        
        os.Remove(nested)
        if moved > 0 {
                _ = moved
        }
}

func (v *VFS) clean(p string) (string, error) {
        p = strings.ReplaceAll(p, "\\", "/")
        parts := strings.Split(p, "/")
        var out []string
        for _, part := range parts {
                if part == "" || part == "." {
                        continue
                }
                if part == ".." {
                        return "", os.ErrPermission
                }
                out = append(out, part)
        }
        if len(out) == 0 {
                return "", os.ErrInvalid
        }
        return filepath.Join(v.Root, filepath.Join(out...)), nil
}

func (v *VFS) Read(p string) ([]byte, error) {
        full, err := v.clean(p)
        if err != nil {
                return nil, err
        }
        return os.ReadFile(full)
}

func (v *VFS) Write(p string, data []byte) error {
        full, err := v.clean(p)
        if err != nil {
                return err
        }
        if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
                return err
        }
        return os.WriteFile(full, data, 0o644)
}

func (v *VFS) List(p string) ([]string, error) {
        full, err := v.clean(p)
        if err != nil {
                return nil, err
        }
        entries, err := os.ReadDir(full)
        if err != nil {
                if os.IsNotExist(err) {
                        return []string{}, nil
                }
                return nil, err
        }
        names := make([]string, 0, len(entries))
        for _, e := range entries {
                names = append(names, e.Name())
        }
        sort.Strings(names)
        return names, nil
}

func (v *VFS) Exists(p string) bool {
        full, err := v.clean(p)
        if err != nil {
                return false
        }
        _, err = os.Stat(full)
        return err == nil
}

func (v *VFS) Unlink(p string) error {
        full, err := v.clean(p)
        if err != nil {
                return err
        }
        err = os.Remove(full)
        if os.IsNotExist(err) {
                return nil
        }
        return err
}
