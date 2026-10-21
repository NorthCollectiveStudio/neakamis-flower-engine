package gamehost

import (
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

func pkgNorm(p string) string {
	p = strings.TrimPrefix(p, "./")
	p = strings.TrimPrefix(p, ".\\")
	p = strings.TrimPrefix(p, "/")
	for strings.HasPrefix(p, "./") {
		p = strings.TrimPrefix(p, "./")
	}
	return p
}

func (h *Host) pkgHas(p string) bool {
	if h.GamePkg == nil {
		return false
	}
	return h.GamePkg.Has(pkgNorm(p))
}

func (h *Host) pkgIsDir(p string) bool {
	if h.GamePkg == nil {
		return false
	}
	p = pkgNorm(p)
	if p == "" {
		return true
	}
	prefix := p + "/"
	for _, name := range h.GamePkg.FileNames() {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}

func (h *Host) pkgRead(p string) ([]byte, error) {
	if h.GamePkg == nil {
		return nil, os.ErrNotExist
	}
	return h.GamePkg.ReadFile(pkgNorm(p), h.GameKey)
}

func (h *Host) pkgList(prefix string) []string {
	out := []string{}
	if h.GamePkg == nil {
		return out
	}
	prefix = pkgNorm(prefix)
	seen := map[string]bool{}
	for _, name := range h.GamePkg.FileNames() {
		if prefix != "" && !strings.HasPrefix(name+"/", prefix+"/") {
			continue
		}
		rest := strings.TrimPrefix(name, prefix)
		rest = strings.TrimPrefix(rest, "/")
		if rest == "" {
			continue
		}
		part := rest
		if i := strings.Index(rest, "/"); i >= 0 {
			part = rest[:i+1]
		}
		if !seen[part] {
			seen[part] = true
			out = append(out, part)
		}
	}
	return out
}

func (h *Host) vfsOrPkgRead(p string) ([]byte, error) {
	data, err := h.vfs.Read(p)
	if err == nil {
		return data, nil
	}
	return h.pkgRead(p)
}

func (h *Host) vfsOrPkgExists(p string) bool {
	if h.vfs.Exists(p) {
		return true
	}
	return h.pkgHas(p) || h.pkgIsDir(p)
}

func (h *Host) serveFS(w http.ResponseWriter, r *http.Request) {
	if !h.authed(r) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	vfs := h.vfs
	if vfs == nil {
		http.Error(w, "no vfs", http.StatusInternalServerError)
		return
	}
	op := strings.TrimPrefix(r.URL.Path, "/flwfs/")
	switch op {
	case "read":
		p := r.URL.Query().Get("path")
		data, err := h.vfsOrPkgRead(p)
		if err != nil {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Write(data)
	case "write":
		p := r.URL.Query().Get("path")
		appendMode := r.URL.Query().Get("append") == "1"
		b64mode := r.URL.Query().Get("b64") == "1"
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "read body failed", http.StatusBadRequest)
			return
		}
		if b64mode {
			decoded, err := base64.StdEncoding.DecodeString(string(body))
			if err != nil {
				http.Error(w, "bad base64", http.StatusBadRequest)
				return
			}
			body = decoded
		}
		if appendMode {
			existing, _ := vfs.Read(p)
			body = append(existing, body...)
		}
		if err := vfs.Write(p, body); err != nil {
			http.Error(w, "write failed: "+err.Error(), http.StatusBadRequest)
			return
		}
		w.WriteHeader(200)
	case "exists":
		p := r.URL.Query().Get("path")
		writeJSON(w, map[string]bool{"exists": h.vfsOrPkgExists(p)})
	case "unlink":
		p := r.URL.Query().Get("path")
		// node's unlinkSync throws ENOENT for a missing file; the caller
		// relies on that to detect "already deleted" saves.
		if !h.vfs.Exists(p) {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		vfs.Unlink(p)
		writeJSON(w, map[string]bool{"ok": true})
	case "list":
		p := r.URL.Query().Get("path")
		items, err := vfs.List(p)
		if err != nil || len(items) == 0 {
			if pkgItems := h.pkgList(p); len(pkgItems) > 0 {
				items = pkgItems
				err = nil
			}
		}
		if err != nil {
			items = []string{}
		}
		// node's readdirSync returns bare names: strip the trailing "/"
		// the package listing uses to mark directories.
		for i := range items {
			items[i] = strings.TrimSuffix(items[i], "/")
		}
		writeJSON(w, map[string]any{"items": items})
	case "stat":
		p := r.URL.Query().Get("path")
		ex := h.vfsOrPkgExists(p)
		size := int64(0)
		isDir := h.pkgIsDir(p)
		var mtime int64
		if ex {
			if full, err := vfs.clean(p); err == nil {
				if st, err := os.Stat(full); err == nil {
					size = st.Size()
					isDir = st.IsDir()
					mtime = st.ModTime().UnixMilli()
				}
			}
		}
		writeJSON(w, map[string]any{"exists": ex, "size": size, "dir": isDir, "mtime": mtime})
	case "mkdir":
		p := r.URL.Query().Get("path")
		os.MkdirAll(filepath.Join(vfs.Root, p), 0o755)
		writeJSON(w, map[string]bool{"ok": true})
	case "rename":
		from := r.URL.Query().Get("from")
		to := r.URL.Query().Get("to")
		data, err := vfs.Read(from)
		if err != nil {
			if pkg, perr := h.pkgRead(from); perr == nil {
				data, err = pkg, nil
			}
		}
		if err != nil {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		vfs.Write(to, data)
		vfs.Unlink(from)
		writeJSON(w, map[string]bool{"ok": true})
	case "copy":
		from := r.URL.Query().Get("from")
		to := r.URL.Query().Get("to")
		data, err := h.vfsOrPkgRead(from)
		if err != nil {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		vfs.Write(to, data)
		writeJSON(w, map[string]bool{"ok": true})
	case "rmdir":
		p := r.URL.Query().Get("path")
		vfs.Unlink(p)
		writeJSON(w, map[string]bool{"ok": true})
	case "b64read":
		p := r.URL.Query().Get("path")
		data, err := h.vfsOrPkgRead(p)
		if err != nil {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "text/plain")
		io.WriteString(w, base64.StdEncoding.EncodeToString(data))
	case "hostwrite":
		p := r.URL.Query().Get("path")
		b64mode := r.URL.Query().Get("b64") == "1"
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "read body failed", http.StatusBadRequest)
			return
		}
		if b64mode {
			decoded, derr := base64.StdEncoding.DecodeString(string(body))
			if derr != nil {
				http.Error(w, "bad base64", http.StatusBadRequest)
				return
			}
			body = decoded
		}
		if err := HostWriteAllowed(p, body); err != nil {
			h.log.Warn("gamehost.fs", "host write denied: %s (%v)", p, err)
			http.Error(w, "denied", http.StatusForbidden)
			return
		}
		h.log.Info("gamehost.fs", "host write ok: %s (%d bytes)", p, len(body))
		writeJSON(w, map[string]bool{"ok": true})
	case "hostexists":
		p := r.URL.Query().Get("path")
		ex := false
		if HostDirAllowed(p) {
			st, err := os.Stat(resolveHostPath(p))
			ex = err == nil && st.IsDir()
		} else {
			ex = HostExistsAllowed(p)
		}
		writeJSON(w, map[string]bool{"exists": ex})
	case "hoststat":
		p := r.URL.Query().Get("path")
		ex := HostExistsAllowed(p)
		size := int64(0)
		isDir := false
		var mtime int64
		if ex {
			if st, err := os.Stat(resolveHostPath(p)); err == nil {
				size = st.Size()
				isDir = st.IsDir()
				mtime = st.ModTime().UnixMilli()
			}
		}
		writeJSON(w, map[string]any{"exists": ex, "size": size, "dir": isDir, "mtime": mtime})
	case "env":
		writeJSON(w, h.EnvMap())
	default:
		http.NotFound(w, r)
	}
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}
