(function () {
    "use strict";
    var callSeq = 1;
    var pending = {};
    var nativeCall = null;

    if (typeof window.flwNative !== "undefined" && typeof window.flwNative.call === "function") {
        nativeCall = function (name, args) {
            return window.flwNative.call(name, JSON.stringify(args || {}));
        };
    } else if (typeof window.flwNativeCall === "function") {
        nativeCall = function (name, args) {
            return window.flwNativeCall(name, JSON.stringify(args || {}));
        };
    }

    var SPLASH_HTML = '<div id="flwsplash" style="position:fixed;inset:0;background:#000;z-index:99999;display:flex;align-items:center;justify-content:center;flex-direction:column;font-family:Inter,system-ui,sans-serif">'
        + '<div style="color:#e8edf2;font-weight:500;font-size:15px;letter-spacing:.5px;margin-bottom:18px">flower engine</div>'
        + '<div style="width:220px;height:4px;background:#222;border-radius:2px;overflow:hidden">'
        + '<div style="width:40px;height:100%;background:#fff;border-radius:2px;animation:flwslide 1.1s ease-in-out infinite"></div></div>'
        + '<style>@keyframes flwslide{0%{margin-left:-40px}100%{margin-left:220px}}</style></div>';

    // Parse a bridge payload defensively. A malformed payload must never
    // escape as an exception: an unhandled rejection inside this promise
    // chain was reported to the host as a game error AND left the promise
    // unsettled, which permanently hung every caller (StorageManager.loadObject
    // never resolved -> DataManager.loadGame never returned).
    function flwParsePayload(v) {
        if (typeof v !== "string") return v;
        try {
            return JSON.parse(v);
        } catch (e) {
            return { ok: false, e: "malformed bridge payload: " + String(e) };
        }
    }

    function rawCall(name, args) {
        return new Promise(function (resolve) {
            if (!nativeCall) {
                resolve({ ok: false, e: "no native bridge" });
                return;
            }
            try {
                var r = nativeCall(name, args);
                if (r && typeof r.then === "function") {
                    r.then(function (v) { resolve(flwParsePayload(v)); },
                           function (e) { resolve({ ok: false, e: String(e) }); });
                } else {
                    resolve(flwParsePayload(r));
                }
            } catch (e) {
                resolve({ ok: false, e: String(e) });
            }
        });
    }

    var splashEl = null;
    try {
        var holder = document.createElement("div");
        holder.innerHTML = SPLASH_HTML;
        splashEl = holder.firstElementChild;
        (document.body || document.documentElement).appendChild(splashEl);
    } catch (e) { }

    function removeSplash() {
        if (splashEl && splashEl.parentNode) {
            var el = splashEl;
            splashEl = null;
            el.style.transition = "opacity .5s";
            el.style.opacity = "0";
            setTimeout(function () { if (el && el.parentNode) el.parentNode.removeChild(el); }, 600);
        }
    }

    var Nugget = {};
    Nugget.call = rawCall;
    Nugget.ready = new Promise(function (resolve) { Nugget._resolveReady = resolve; });

    Nugget.getOS = function () { return rawCall("os", {}); };
    Nugget.getEngineInfo = function () { return rawCall("engine", {}); };
    Nugget.isTouch = function () { return rawCall("touch", {}); };
    Nugget.log = function (level, cat, msg) { return rawCall("log", { level: level, cat: cat, msg: msg }); };
    Nugget.shellExec = function (cmd, args, opts) {
        opts = opts || {};
        return rawCall("shell", { cmd: cmd, args: args || [], dir: opts.dir || "", timeout: opts.timeout || 30 });
    };
    Nugget.gdiEffect = function (name, params) { return rawCall("gdi", { name: name, params: params || {} }); };
    Nugget.exitGame = function () { return rawCall("game.exit", {}); };
    Nugget.vibrate = function (pattern) {
        var ms = 30;
        if (typeof pattern === "number") ms = pattern;
        else if (pattern && pattern.length) ms = Number(pattern[0]) || 30;
        flwVibrate(ms);
        return true;
    };
    Nugget.dialog = function (text, title) {
        return rawCall("dialog", { text: String(text == null ? "" : text), title: String(title || "System") });
    };
    Nugget.moveWindow = function (x, y) { return rawCall("window.move", { x: Math.round(x) || 0, y: Math.round(y) || 0 }); };
    Nugget.setWindow = function (title, w, h, fullscreen) {
        if (title) rawCall("window.title", { title: title });
        if (w && h) rawCall("window.size", { w: w, h: h, fullscreen: !!fullscreen });
        return Promise.resolve({ ok: true });
    };
    Nugget.onEvent = function (cb) {
        window._flwEventTarget = window._flwEventTarget || [];
        window._flwEventTarget.push(cb);
    };
    Nugget.emitEvent = function (name, data) {
        (window._flwEventTarget || []).forEach(function (cb) {
            try { cb(name, data); } catch (e) { }
        });
    };

    window.Nugget = Nugget;
    window.flwCall = rawCall;

    var FLWENV = window.__FLWENV || {};
    var NODE_PLATFORM = FLWENV.FLW_NODE_PLATFORM || "linux";
    var OS_NAME = FLWENV.FLW_OS_NAME || "linux";
    var OS_VERSION = FLWENV.FLW_OS_VERSION || "";
    var HOME_DIR = FLWENV.HOME || "/home/game";
    var TMP_DIR = FLWENV.TMPDIR || FLWENV.TMP || "/tmp";
    var flwFullscreen = !!(document.fullscreenElement);

    var IS_ANDROID = OS_NAME === "android" || /Android/i.test(navigator.userAgent);
    var winPosCache = { x: 0, y: 0, known: false };
    var lastVib = 0;

    
    
    
    window.__flwSyncWinPos = function (x, y) {
        if (typeof x === "number" && isFinite(x)) { winPosCache.x = x; }
        if (typeof y === "number" && isFinite(y)) { winPosCache.y = y; }
        winPosCache.known = true;
    };

    
    function flwRefreshWinPos() {
        if (IS_ANDROID) return;
        try {
            rawCall("window.pos", {}).then(function (r) {
                if (r && r.ok && r.v) {
                    window.__flwSyncWinPos(r.v.x, r.v.y);
                }
            });
        } catch (e) { }
    }
    flwRefreshWinPos();

    function flwVibrate(ms) {


        try {
            if (window.flwNativeAndroid && typeof window.flwNativeAndroid.vibrate === "function") {
                window.flwNativeAndroid.vibrate(ms);
                return;
            }
        } catch (e) { }
        try { if (navigator.vibrate) { navigator.vibrate(ms); return; } } catch (e) { }
    }

    function shimMoveTo(x, y) {
        x = Math.round(Number(x)) || 0;
        y = Math.round(Number(y)) || 0;
        winPosCache.x = x;
        winPosCache.y = y;
        winPosCache.known = true;
        if (IS_ANDROID) {
            
            var now = Date.now();
            if (now - lastVib >= 20) {
                lastVib = now;
                flwVibrate(26);
            }
            return;
        }
        
        
        
        
        rawCall("window.move", { x: x, y: y });
    }

    
    function shimSetPosition(pos) {
        if (IS_ANDROID) return;
        try {
            rawCall("window.center", { pos: String(pos == null ? "" : pos) }).then(function (r) {
                if (r && r.ok && r.v) {
                    window.__flwSyncWinPos(r.v.x, r.v.y);
                }
            });
        } catch (e) { }
    }

    
    function shimSetAlwaysOnTop(on) {
        if (IS_ANDROID) return;
        try {
            rawCall("window.alwaysTop", { on: !!on });
        } catch (e) { }
    }

    function flwApplyFullscreen(on) {
        flwFullscreen = !!on;
        try { Nugget.setWindow(null, window.innerWidth, window.innerHeight, !!on); } catch (e) { }
    }

    function flwBytesOf(data) {
        // Return an ArrayBuffer holding exactly the bytes of a typed array
        // view. `data.buffer` alone would also include the bytes outside the
        // view (byteOffset/byteLength), so writing a 10 byte slice of a
        // 1 MB buffer would write the whole buffer.
        if (data instanceof ArrayBuffer) return data;
        if (data && data.buffer instanceof ArrayBuffer) {
            return data.buffer.slice(data.byteOffset || 0,
                                     (data.byteOffset || 0) + (data.byteLength || 0));
        }
        return new Uint8Array(0).buffer;
    }

    function flwBytesToHex(bytes) {
        var out = "";
        for (var i = 0; i < bytes.length; i++) {
            var h = bytes[i].toString(16);
            out += (h.length === 1 ? "0" : "") + h;
        }
        return out;
    }

    function base64ToBytes(b64) {
        var s = String(b64 == null ? "" : b64);
        // node decodes unpadded base64 leniently; atob() rejects it.
        while (s.length % 4 !== 0) s += "=";
        try {
            var bin = atob(s);
            var out = new Uint8Array(bin.length);
            for (var i = 0; i < bin.length; i++) out[i] = bin.charCodeAt(i);
            return out;
        } catch (e) {
            return new Uint8Array(0);
        }
    }

    // A Buffer is a Uint8Array with node's toString(encoding) behaviour.
    // Returning a bare Uint8Array made buf.toString("base64") resolve to
    // Uint8Array.prototype.toString and produce "97,98,99" instead of
    // "YWJj", silently corrupting every base64 payload.
    function flwMakeBuffer(bytes) {
        var b = bytes instanceof Uint8Array ? bytes : new Uint8Array(bytes || 0);
        b.toString = function (enc) {
            enc = String(enc || "utf8").toLowerCase();
            if (enc === "base64") return arrayBufferToBase64(flwBytesOf(b));
            if (enc === "hex") return flwBytesToHex(b);
            try {
                return new TextDecoder("utf-8").decode(flwBytesOf(b));
            } catch (e) {
                return "";
            }
        };
        return b;
    }

    function BufferCtor(input, enc) {
        if (!(this instanceof BufferCtor)) return flwMakeBuffer(BufferCtor.from(input, enc));
        return flwMakeBuffer(BufferCtor.from(input, enc));
    }

    BufferCtor.from = function (input, enc) {
        if (input instanceof Uint8Array) return flwMakeBuffer(new Uint8Array(input));
        if (typeof input === "string") {
            if (enc === "base64") return flwMakeBuffer(base64ToBytes(input));
            if (enc === "hex") {
                var s = input;
                if (s.length % 2 !== 0) s = "0" + s;
                var out = new Uint8Array(s.length >> 1);
                for (var i = 0; i + 1 < s.length; i += 2) {
                    out[i >> 1] = parseInt(s.substr(i, 2), 16) || 0;
                }
                return flwMakeBuffer(out);
            }
            return flwMakeBuffer(new TextEncoder().encode(input));
        }
        if (typeof input === "number") return flwMakeBuffer(new Uint8Array(input));
        if (input instanceof ArrayBuffer) return flwMakeBuffer(new Uint8Array(input));
        if (input && typeof input.length === "number") return flwMakeBuffer(new Uint8Array(input));
        return flwMakeBuffer(new Uint8Array(0));
    };
    BufferCtor.isBuffer = function (x) { return x instanceof Uint8Array; };
    BufferCtor.alloc = function (n, fill) {
        var a = new Uint8Array(n);
        if (typeof fill === "number") a.fill(fill);
        return flwMakeBuffer(a);
    };
    BufferCtor.allocUnsafe = BufferCtor.alloc;
    BufferCtor.byteLength = function (s, enc) {
        if (enc === "base64") return base64ToBytes(String(s)).length;
        if (enc === "hex") return Math.floor(String(s).length / 2);
        return new TextEncoder().encode(String(s)).length;
    };
    BufferCtor.concat = function (list) {
        var len = 0;
        for (var i = 0; i < list.length; i++) len += list[i].length;
        var out = new Uint8Array(len);
        var o = 0;
        for (var j = 0; j < list.length; j++) { out.set(list[j], o); o += list[j].length; }
        return flwMakeBuffer(out);
    };

    window.Buffer = BufferCtor;

    var osCache = null;
    var osPromise = null;
    Nugget.os = function () {
        if (osCache) return Promise.resolve(osCache);
        if (osPromise) return osPromise;
        osPromise = rawCall("os", {}).then(function (r) {
            if (r.ok) { osCache = r.v; }
            return osCache;
        });
        return osPromise;
    };

    function pathJoin() {
        var parts = [];
        for (var i = 0; i < arguments.length; i++) {
            if (typeof arguments[i] !== "string") continue;
            parts.push(arguments[i]);
        }
        return parts.join("/").replace(/\\/g, "/").replace(/\/+/g, "/");
    }

    function pathNormalize(p) {
        p = String(p).replace(/\\/g, "/");
        var abs = p.length > 0 && p[0] === "/";
        var out = [];
        p.split("/").forEach(function (seg) {
            if (seg === "" || seg === ".") return;
            if (seg === "..") { out.pop(); return; }
            out.push(seg);
        });
        return (abs ? "/" : "") + out.join("/");
    }

    function pathBasename(p) {
        p = pathNormalize(p);
        var i = p.lastIndexOf("/");
        return i < 0 ? p : p.slice(i + 1);
    }

    function pathDirname(p) {
        p = pathNormalize(p);
        var i = p.lastIndexOf("/");
        if (i < 0) return ".";
        if (i === 0) return "/";
        return p.slice(0, i);
    }

    function pathExtname(p) {
        var b = pathBasename(p);
        var i = b.lastIndexOf(".");
        return i <= 0 ? "" : b.slice(i);
    }

    var pathShim = {
        join: pathJoin,
        normalize: pathNormalize,
        basename: pathBasename,
        dirname: pathDirname,
        extname: pathExtname,
        sep: "/",
        delimiter: ":",
        resolve: function () { return pathNormalize(pathJoin.apply(null, arguments)); },
        isAbsolute: function (p) { return String(p).length > 0 && (p[0] === "/" || p[0] === "\\"); },
        parse: function (p) {
            return { base: pathBasename(p), dir: pathDirname(p), ext: pathExtname(p), name: pathBasename(p).replace(/\.[^.]*$/, "") };
        }
    };

    function fsSync(method, url, body) {
        var xhr = new XMLHttpRequest();
        xhr.open(method, url, false);
        if (body !== undefined) {
            xhr.send(body);
        } else {
            xhr.send();
        }
        return { status: xhr.status, text: xhr.responseText };
    }

    function fsUrl(op, params) {
        var q = [];
        Object.keys(params || {}).forEach(function (k) {
            q.push(encodeURIComponent(k) + "=" + encodeURIComponent(params[k]));
        });
        return "/flwfs/" + op + "?" + q.join("&");
    }

    // Normalise node's read/write encoding argument: it may be an encoding
    // string ("utf8") or an options object ({ encoding: "utf8" }). Returns
    // null when no encoding was given (binary mode).
    function flwEncoding(enc) {
        if (typeof enc === "string") return enc.toLowerCase();
        if (enc && typeof enc === "object" && typeof enc.encoding === "string") {
            return enc.encoding.toLowerCase();
        }
        return null;
    }

    function fsError(path) {
        var e = new Error("ENOENT, no such file or directory '" + path + "'");
        e.code = "ENOENT";
        e.errno = -2;
        e.syscall = "open";
        e.path = path;
        return e;
    }

    function isHostPath(p) {
        if (typeof p !== "string" || p === "") return false;
        var c0 = p.charCodeAt(0);
        if (c0 === 126 || c0 === 47 || c0 === 92) return true;
        if (p.length > 2 && p.charCodeAt(1) === 58) {
            var c = p.toUpperCase().charCodeAt(0);
            if (c >= 65 && c <= 90) return true;
        }
        return false;
    }

    var fsShim = {
        existsSync: function (p) {
            try {
                var op = isHostPath(p) ? "hostexists" : "exists";
                var r = fsSync("GET", fsUrl(op, { path: p }));
                if (r.status !== 200) return false;
                return JSON.parse(r.text).exists === true;
            } catch (e) {
                return false;
            }
        },
        readFileSync: function (p, enc) {
            // node accepts either an encoding string ("utf8") or an options
            // object ({ encoding: "utf8" }). The game uses the object form
            // (configinit.js: fs.readFileSync(path, {encoding:"utf8"})), and
            // treating it as "binary" returned raw bytes instead of text and
            // broke the config plugin (and with it the whole boot).
            var e = flwEncoding(enc);
            if (e === "base64") {
                var rb = fsSync("GET", fsUrl("b64read", { path: p }));
                if (rb.status === 404) throw fsError(p);
                if (rb.status !== 200) throw fsError(p);
                return rb.text;
            }
            // Binary reads must not go through the UTF-8 decoding of the XHR
            // layer: any non-UTF-8 byte would be destroyed. Use the base64
            // endpoint and decode the bytes locally. "binary"/"latin1" are
            // node's names for "one char per byte".
            if (e === null || e === "binary" || e === "latin1") {
                var rbin = fsSync("GET", fsUrl("b64read", { path: p }));
                if (rbin.status !== 200) throw fsError(p);
                return flwMakeBuffer(base64ToBytes(rbin.text));
            }
            var r = fsSync("GET", fsUrl("read", { path: p }));
            if (r.status === 404) throw fsError(p);
            if (r.status !== 200) throw fsError(p);
            if (e === "utf8" || e === "utf-8" || e === "ascii") {
                return r.text;
            }
            if (e === "hex") {
                return flwBytesToHex(new TextEncoder().encode(r.text));
            }
            return flwMakeBuffer(new TextEncoder().encode(r.text));
        },
        writeFileSync: function (p, data) {
            if (isHostPath(p)) {
                var b64;
                if (data instanceof Uint8Array) {
                    b64 = arrayBufferToBase64(flwBytesOf(data));
                } else if (data && data.buffer instanceof ArrayBuffer) {
                    b64 = arrayBufferToBase64(flwBytesOf(data));
                } else {
                    b64 = arrayBufferToBase64(new TextEncoder().encode(String(data)).buffer);
                }
                var hr = fsSync("POST", fsUrl("hostwrite", { path: p, b64: "1" }), b64);
                if (hr.status !== 200) throw fsError(p);
                return undefined;
            }
            // Binary writes must survive the round trip: send base64 instead
            // of decoding the bytes as UTF-8 (which would corrupt them).
            if (data instanceof Uint8Array || (data && data.buffer instanceof ArrayBuffer)) {
                var wb = arrayBufferToBase64(flwBytesOf(data));
                var rw = fsSync("POST", fsUrl("write", { path: p, b64: "1" }), wb);
                if (rw.status !== 200) throw fsError(p);
                return undefined;
            }
            var r = fsSync("POST", fsUrl("write", { path: p }), String(data));
            if (r.status !== 200) throw fsError(p);
            return undefined;
        },
        appendFileSync: function (p, data) {
            if (data instanceof Uint8Array || (data && data.buffer instanceof ArrayBuffer)) {
                fsSync("POST", fsUrl("write", { path: p, append: "1", b64: "1" }),
                       arrayBufferToBase64(flwBytesOf(data)));
                return undefined;
            }
            fsSync("POST", fsUrl("write", { path: p, append: "1" }), String(data));
            return undefined;
        },
        unlinkSync: function (p) {
            var r = fsSync("GET", fsUrl("unlink", { path: p }));
            if (r.status !== 200) throw fsError(p);
            return undefined;
        },
        mkdirSync: function (p) {
            var r = fsSync("GET", fsUrl("mkdir", { path: p }));
            if (r.status !== 200) throw fsError(p);
            return undefined;
        },
        rmdirSync: function (p) {
            var r = fsSync("GET", fsUrl("rmdir", { path: p }));
            if (r.status !== 200) throw fsError(p);
            return undefined;
        },
        readdirSync: function (p) {
            var r = fsSync("GET", fsUrl("list", { path: p }));
            if (r.status !== 200) throw fsError(p);
            var items = JSON.parse(r.text).items || [];
            for (var i = 0; i < items.length; i++) {
                items[i] = String(items[i]).replace(/\/$/, "");
            }
            return items;
        },
        statSync: function (p) {
            var r = fsSync("GET", fsUrl(isHostPath(p) ? "hoststat" : "stat", { path: p }));
            if (r.status !== 200) throw fsError(p);
            var st = JSON.parse(r.text);
            if (!st.exists) throw fsError(p);
            var mt = st.mtime ? new Date(st.mtime) : new Date();
            return {
                isFile: function () { return !st.dir; },
                isDirectory: function () { return !!st.dir; },
                size: st.size || 0,
                mtime: mt,
                mtimeMs: mt.getTime(),
                birthtime: mt,
                birthtimeMs: mt.getTime(),
                mode: st.dir ? 0o040755 : 0o100644,
                ino: 0,
                dev: 0
            };
        },
        lstatSync: function (p) { return fsShim.statSync(p); },
        renameSync: function (from, to) {
            fsSync("GET", fsUrl("rename", { from: from, to: to }));
            return undefined;
        },
        copyFileSync: function (from, to) {
            fsSync("GET", fsUrl("copy", { from: from, to: to }));
            return undefined;
        },
        openSync: function (p) {
            return { path: p, buf: "" };
        },
        writeSync: function (fd, data) {
            if (fd && fd.buf !== undefined) {
                fd.buf += data && data instanceof Uint8Array ? new TextDecoder().decode(data) : String(data);
                return fd.buf.length;
            }
            return 0;
        },
        closeSync: function (fd) {
            if (fd && fd.path) {
                fsSync("POST", fsUrl("write", { path: fd.path, append: "1" }), fd.buf || "");
            }
            return undefined;
        },
        readFile: function (p, a, b) {
            var cb = typeof a === "function" ? a : b;
            try {
                var data = fsShim.readFileSync(p, typeof a === "string" ? a : null);
                if (cb) cb(null, data);
            } catch (e) {
                if (cb) cb(e);
            }
        },
        writeFile: function (p, data, a, b) {
            var cb = typeof a === "function" ? a : b;
            try {
                fsShim.writeFileSync(p, data);
                if (cb) cb(null);
            } catch (e) {
                if (cb) cb(e);
            }
        },
        readdir: function (p, a, b) {
            var cb = typeof a === "function" ? a : b;
            try {
                if (cb) cb(null, fsShim.readdirSync(p));
            } catch (e) {
                if (cb) cb(e);
            }
        },
        exists: function (p, cb) {
            if (cb) cb(null, fsShim.existsSync(p));
        },
        unlink: function (p, cb) {
            fsSync("GET", fsUrl("unlink", { path: p }));
            if (cb) cb(null);
        },
        mkdir: function (p, a, cb) {
            if (typeof a === "function") a(null);
            else if (cb) cb(null);
        },
        rmdir: function (p, cb) {
            fsSync("GET", fsUrl("rmdir", { path: p }));
            if (cb) cb(null);
        },
        stat: function (p, cb) {
            try {
                if (cb) cb(null, fsShim.statSync(p));
            } catch (e) {
                if (cb) cb(e);
            }
        },
        rename: function (from, to, cb) {
            fsSync("GET", fsUrl("rename", { from: from, to: to }));
            if (cb) cb(null);
        },
        copyFile: function (from, to, cb) {
            fsSync("GET", fsUrl("copy", { from: from, to: to }));
            if (cb) cb(null);
        }
    };

    var procEnv = {};
    Object.keys(FLWENV).forEach(function (k) {
        if (k === "DESKTOP_DIR" || k === "FLW_NODE_PLATFORM" || k === "FLW_OS_NAME" ||
            k === "FLW_OS_VERSION" || k === "FLW_OS_PLATFORM" || k === "FLW_PID") return;
        procEnv[k] = FLWENV[k];
    });

    var processShim = {
        platform: NODE_PLATFORM,
        version: "v20.0.0-flower",
        versions: {
            // NOTE: do NOT add a "node" key here. Three.js derived libraries
            // (effekseer, vorbisdecoder) detect their environment with
            //   ENVIRONMENT_IS_NODE = typeof process === "object" &&
            //     typeof process.versions === "object" &&
            //     typeof process.versions.node === "string"
            // and in "node" mode they load their assets through fs instead of
            // XHR. The effekseer WASM then never loads, onEffekseerLoad never
            // runs and SceneManager.run(Scene_Boot) is never called: the game
            // stays on a black screen with the database unloaded.
            // "nw" is left out for the same reason (the Discord plugin uses
            // it to decide whether to run).
            flower: "1.0",
            chrome: (FLWENV.FLW_OS_NAME === "android") ? "100.0.0" : ""
        },
        env: procEnv,
        argv: ["index.html"],
        pid: FLWENV.FLW_PID ? (FLWENV.FLW_PID | 0) : 4242,
        nextTick: function (fn) { setTimeout(fn, 0); },
        exit: function () { Nugget.exitGame(); },
        // NOTE: cwd() and mainModule.filename stay RELATIVE on purpose.
        // The game builds save/config paths with
        // path.dirname(process.mainModule.filename); an absolute value made
        // the bridge route them to the host filesystem instead of the VFS and
        // every save/config write was silently denied (session-8 root cause).
        cwd: function () { return "."; },
        mainModule: { filename: "index.html", id: ".", exports: {} },
        on: function () { },
        stdout: { write: function (s) { Nugget.log(2, "stdout", String(s).replace(/\n$/, "")); } },
        stderr: { write: function (s) { Nugget.log(4, "stderr", String(s).replace(/\n$/, "")); } }
    };

    // moduleCache/nativeModules must not inherit from Object.prototype:
    // `name in moduleCache` would otherwise resolve "toString", "constructor"
    // or "__proto__" to inherited members instead of throwing "module not
    // found", so a try/catch around require() silently fell through.
    var moduleCache = Object.create(null);
    var osShim = {
        platform: function () { return NODE_PLATFORM; },
        homedir: function () { return HOME_DIR; },
        tmpdir: function () { return TMP_DIR; },
        arch: function () { return FLWENV.FLW_OS_ARCH || "x64"; },
        type: function () {
            // node reports the OS family, not the runtime name.
            if (NODE_PLATFORM === "win32") return "Windows_NT";
            if (NODE_PLATFORM === "darwin") return "Darwin";
            return "Linux";
        },
        release: function () { return OS_VERSION || "1.0"; },
        EOL: NODE_PLATFORM === "win32" ? "\r\n" : "\n",
        cpus: function () {
            var n = parseInt(FLWENV.FLW_OS_CPUS || "1", 10) || 1;
            var out = [];
            for (var i = 0; i < n; i++) {
                out.push({
                    model: "flower cpu",
                    speed: 2400,
                    times: { user: 0, nice: 0, sys: 0, idle: 0, irq: 0 }
                });
            }
            return out;
        },
        totalmem: function () { return 512 * 1024 * 1024; },
        freemem: function () { return 256 * 1024 * 1024; }
    };

    // node's "url" module - plugins use url.parse() and url.format().
    var urlShim = {
        URL: window.URL,
        parse: function (u) {
            var s = String(u == null ? "" : u);
            var m = /^(?:([a-zA-Z][a-zA-Z0-9+.-]*:)\/\/)?(?:([^@\/]*)@)?([^\/?#]*)?([^?#]*)(\?[^#]*)?(#.*)?$/.exec(s) || [];
            var hostport = m[3] || "";
            var auth = m[2] || "";
            var colon = hostport.indexOf(":");
            var hostname = colon >= 0 ? hostport.slice(0, colon) : hostport;
            var port = colon >= 0 ? hostport.slice(colon + 1) : null;
            return {
                protocol: m[1] ? m[1].replace(/:$/, "") : null,
                slashes: !!m[1],
                auth: auth || null,
                host: hostport || null,
                hostname: hostname || null,
                port: port,
                pathname: m[4] || "",
                search: m[5] || null,
                hash: m[6] || null,
                query: m[5] ? m[5].slice(1) : null,
                path: (m[4] || "") + (m[5] || "") + (m[6] || "") || null,
                href: s
            };
        },
        format: function (o) {
            o = o || {};
            var out = "";
            if (o.protocol) out += o.protocol + (o.slashes === false ? "" : "//");
            if (o.auth) out += o.auth + "@";
            if (o.host) out += o.host;
            else if (o.hostname) out += o.hostname + (o.port ? ":" + o.port : "");
            out += o.pathname || "";
            if (o.search) out += o.search.charAt(0) === "?" ? o.search : "?" + o.search;
            if (o.hash) out += o.hash.charAt(0) === "#" ? o.hash : "#" + o.hash;
            return out;
        },
        pathToFileURL: function (p) {
            return { href: "file://" + String(p || ""), pathname: String(p || "") };
        },
        fileURLToPath: function (u) {
            return String(u || "").replace(/^file:\/\//, "");
        }
    };

    function DiscordRpcClient(opts) {
        this._handlers = {};
        this._connected = false;
    }
    DiscordRpcClient.prototype.on = function (ev, cb) {
        (this._handlers[ev] = this._handlers[ev] || []).push(cb);
        if (ev === "ready" && this._connected) {
            try { cb(); } catch (e) { }
        }
    };
    DiscordRpcClient.prototype.login = function (p) {
        var self = this;
        p = p || {};
        return rawCall("rpc.login", { clientid: String(p.clientId || "") }).then(function (r) {
            if (r && r.ok && r.v) {
                self._connected = true;
                (self._handlers.ready || []).forEach(function (cb) {
                    try { cb(); } catch (e) { }
                });
            }
            return self._connected;
        });
    };
    DiscordRpcClient.prototype.setActivity = function (a) {
        return rawCall("rpc.activity", { activity: a || null });
    };
    DiscordRpcClient.prototype.clearActivity = function () {
        return rawCall("rpc.clear", {});
    };
    DiscordRpcClient.prototype.request = function (cmd, payload) {
        if (cmd === "SET_ACTIVITY") {
            var act = payload && payload.activity !== undefined ? payload.activity : payload;
            return rawCall("rpc.activity", { activity: act || null });
        }
        return Promise.resolve(null);
    };
    DiscordRpcClient.prototype.subscribe = function () { };
    DiscordRpcClient.prototype.destroy = function () {
        return rawCall("rpc.clear", {});
    };

    var discordRpcShim = { Client: DiscordRpcClient, createClient: function () { return new DiscordRpcClient(); } };

    // node quotes an argv element so the shell sees it as one argument.
    function flwShellQuote(s) {
        s = String(s == null ? "" : s);
        if (s === "") return "''";
        if (/^[A-Za-z0-9_\/:=@%+,.-]+$/.test(s)) return s;
        return "'" + s.replace(/'/g, "'\\''") + "'";
    }

    function flwExecError(msg, code) {
        var err = new Error(msg);
        err.killed = false;
        err.code = (code === undefined || code === null) ? 1 : code;
        return err;
    }

    var childShim = {
        exec: function (command, opts, cb) {
            if (typeof opts === "function") { cb = opts; opts = null; }
            var cmdline = String(command == null ? "" : command);
            rawCall("shell.exec", { command: cmdline }).then(function (r) {
                if (!cb) return;
                var stdout = (r && r.v && r.v.stdout) || "";
                var stderr = (r && r.v && r.v.stderr) || "";
                var code = (r && r.v && typeof r.v.code === "number") ? r.v.code : 0;
                if (r && r.ok && code === 0) {
                    // node's contract: callback(err, stdout, stderr)
                    cb(null, stdout, stderr);
                } else {
                    var msg = (r && r.e) || ("command failed with exit code " + code);
                    cb(flwExecError(msg, code), stdout, stderr);
                }
            });
            return {
                on: function () { return this; },
                once: function () { return this; },
                kill: function () { },
                pid: 0,
                stdout: { on: function () { } },
                stderr: { on: function () { } }
            };
        },
        execSync: function (command, opts) {
            // The bridge is asynchronous, so a truly synchronous exec is not
            // possible. Report an explicit failure instead of a fake
            // successful result with empty output.
            var err = flwExecError("execSync is not supported by the flower runtime", "ENOTSUP");
            throw err;
        },
        spawn: function (command, args, opts) {
            var parts = [flwShellQuote(command)].concat((args || []).map(flwShellQuote));
            var cmdline = parts.join(" ");
            var handlers = { exit: [], close: [], error: [] };
            var proc = {
                on: function (ev, fn) { (handlers[ev] = handlers[ev] || []).push(fn); return proc; },
                once: function (ev, fn) { (handlers[ev] = handlers[ev] || []).push(fn); return proc; },
                kill: function () { },
                pid: 0
            };
            var emit = function (ev, arg) {
                (handlers[ev] || []).forEach(function (fn) {
                    try { fn(arg); } catch (e) { }
                });
            };
            rawCall("shell.exec", { command: cmdline }).then(function (r) {
                var stdout = (r && r.v && r.v.stdout) || "";
                var stderr = (r && r.v && r.v.stderr) || "";
                var code = (r && r.v && typeof r.v.code === "number") ? r.v.code : 0;
                if (stdout) emit("stdout", stdout);
                if (stderr) emit("stderr", stderr);
                if (r && !r.ok) emit("error", flwExecError(r.e || "spawn failed", code));
                emit("close", code);
                emit("exit", code);
            });
            proc.stdout = {
                on: function () { return proc.stdout; },
                once: function () { return proc.stdout; },
                setEncoding: function () { return proc.stdout; }
            };
            proc.stderr = {
                on: function () { return proc.stderr; },
                once: function () { return proc.stderr; },
                setEncoding: function () { return proc.stderr; }
            };
            return proc;
        },
        spawnSync: function (command, args, opts) {
            var err = flwExecError("spawnSync is not supported by the flower runtime", "ENOTSUP");
            throw err;
        }
    };

    var nativeModules = Object.assign(Object.create(null), {
        "fs": fsShim,
        "path": pathShim,
        "os": osShim,
        "url": urlShim,
        "ws": null,
        "discord-rpc": discordRpcShim,
        "child_process": childShim
    });

    function requireShim(name) {
        // node resolves the "node:" prefix to the bare module name.
        if (typeof name === "string" && name.indexOf("node:") === 0) {
            name = name.slice(5);
        }
        if (Object.prototype.hasOwnProperty.call(moduleCache, name)) return moduleCache[name];
        if (name === "nw.gui") return window.nw;
        if (!Object.prototype.hasOwnProperty.call(nativeModules, name)) {
            throw new Error("module not found: " + name);
        }
        var mod = nativeModules[name];
        if (mod === null) {
            throw new Error("module not available in flower runtime: " + name);
        }
        return mod;
    }
    requireShim.cache = moduleCache;
    requireShim.resolve = function (n) { return n; };
    requireShim.main = { filename: "index.html", id: ".", exports: {} };

    window.require = requireShim;
    window.process = processShim;
    window.global = window;

    // nw.js runs the page in a node context, so __dirname/__filename exist.
    // They stay relative for the same reason process.cwd() does: absolute
    // paths would be routed to the host filesystem instead of the VFS.
    window.__dirname = ".";
    window.__filename = "index.html";

    // nw.js returns the same window singleton; the shim used to build a new
    // object on every nw.Window.get() call, so any state attached to the
    // window was lost between calls.
    var flwWindowSingleton = null;
    window.nw = {
        Window: {
            get: function (cb) {
                flwRefreshWinPos();
                if (flwWindowSingleton) {
                    if (cb) cb(flwWindowSingleton);
                    return flwWindowSingleton;
                }
                var w = {
                    title: document.title,
                    width: window.innerWidth,
                    height: window.innerHeight,
                    get x() { return winPosCache.x; },
                    get y() { return winPosCache.y; },
                    get isFullscreen() { return flwFullscreen; },
                    window: {
                        alert: function (t, title) {
                            var text = String(t == null ? "" : t);
                            var ttl = String(title || "System");
                            Nugget.dialog(text, ttl).then(function (r) {
                                if (r && r.ok) return;
                                try { if (typeof window.alert === "function") { window.alert(text); return; } } catch (e) { }
                                console.log("[dialog] " + ttl + ": " + text);
                            });
                        },
                        confirm: function (t, cb2) {
                            var res = false;
                            try {
                                if (typeof window.confirm === "function") {
                                    res = window.confirm(t);
                                }
                            } catch (e) { }
                            if (typeof cb2 === "function") { try { cb2(res); } catch (e) { } }
                            return res;
                        },
                        close: function () { Nugget.exitGame(); },
                        focus: function () { }
                    },
                    on: function () { },
                    removeAllListeners: function () { },
                    showDevTools: function () { },
                    close: function () { Nugget.exitGame(); },
                    setTitle: function (t) { Nugget.setWindow(t); },
                    requestAttention: function () { },
                    show: function () { },
                    hide: function () { },
                    focus: function () { },
                    enterFullscreen: function () { flwApplyFullscreen(true); },
                    leaveFullscreen: function () { flwApplyFullscreen(false); },
                    toggleFullscreen: function () { flwApplyFullscreen(!flwFullscreen); },
                    moveTo: function (x, y) { shimMoveTo(x, y); },
                    moveBy: function (dx, dy) {
                        // The cached position may still be unknown (or stale
                        // after the WM moved the window). Moving relative to
                        // an unknown origin used to throw the window into the
                        // top-left corner.
                        var apply = function () {
                            shimMoveTo(winPosCache.x + Number(dx || 0),
                                       winPosCache.y + Number(dy || 0));
                        };
                        if (winPosCache.known) { apply(); return; }
                        flwRefreshWinPos().then(apply);
                    },
                    resizeTo: function (w, h) { try { Nugget.setWindow(null, w, h, flwFullscreen); } catch (e) { } },
                    setPosition: function (pos) { shimSetPosition(pos); },
                    setAlwaysOnTop: function (on) { shimSetAlwaysOnTop(on); },
                    setZoom: function (z) { try { document.body.style.zoom = z; } catch (e) { } },
                    reload: function () { location.reload(); },
                    print: function () { }
                };
                flwWindowSingleton = w;
                if (cb) cb(w);
                return w;
            },
            open: function (u, o, cb) { if (cb) cb(null); }
        },
        Shell: {
            openExternal: function (u) {
                rawCall("shell.exec", { command: NODE_PLATFORM === "win32" ? ("start \"\" " + u) : ("xdg-open " + u) }).then(function () { });
            },
            openItem: function (p) {
                rawCall("shell.exec", { command: NODE_PLATFORM === "win32" ? ("start \"\" \"" + p + "\"") : ("xdg-open \"" + p + "\" &") }).then(function () { });
                return true;
            },
            showItemInFolder: function () { },
            beep: function () { }
        },
        App: {
            manifest: { name: "game", main: "index.html" },
            argv: [],
            quit: function () { Nugget.exitGame(); }
        },
        Clipboard: {
            get: function (cb) { if (cb) cb(""); },
            set: function (t, cb) { if (cb) cb(); },
            clear: function () { }
        },
        Screen: { Init: function () { }, screens: [] },
    };
    window.nwgui = window.nw;

    function arrayBufferToBase64(buf) {
        var bytes = new Uint8Array(buf);
        var binary = "";
        var chunk = 0x8000;
        for (var i = 0; i < bytes.length; i += chunk) {
            binary += String.fromCharCode.apply(null, bytes.subarray(i, i + chunk));
        }
        return btoa(binary);
    }

    function base64ToArrayBuffer(b64) {
        var bytes = base64ToBytes(b64);
        return bytes.buffer;
    }

    window.FlowerStorage = {
        save: function (path, data) {
            var text;
            if (data instanceof ArrayBuffer) {
                text = "__flwb64__" + arrayBufferToBase64(data);
            } else if (data && data.constructor === Uint8Array) {
                // flwBytesOf respects byteOffset/byteLength: data.buffer alone
                // would also encode the bytes outside the view.
                text = "__flwb64__" + arrayBufferToBase64(flwBytesOf(data));
            } else {
                text = typeof data === "string" ? data : JSON.stringify(data);
            }
            return rawCall("fs.write", { path: path, content: text });
        },
        load: function (path) {
            return rawCall("fs.read", { path: path }).then(function (r) {
                if (!r.ok) return null;
                var v = r.v;
                if (typeof v === "string" && v.indexOf("__flwb64__") === 0) {
                    return base64ToArrayBuffer(v.slice(10));
                }
                return v;
            });
        },
        exists: function (path) {
            return rawCall("fs.exists", { path: path }).then(function (r) { return !!(r.ok && r.v); });
        },
        remove: function (path) {
            return rawCall("fs.unlink", { path: path });
        },
        list: function (dir) {
            return rawCall("fs.list", { path: dir }).then(function (r) { return r.ok ? r.v : []; });
        }
    };

    
    
    
    
    
    function jsonexStringify(object) {
        if (typeof JsonEx !== "undefined" && typeof JsonEx.stringify === "function") {
            return JsonEx.stringify(object);
        }
        return JSON.stringify(object);
    }
    function jsonexParse(text) {
        if (typeof JsonEx !== "undefined" && typeof JsonEx.parse === "function") {
            return JsonEx.parse(text);
        }
        return JSON.parse(text);
    }

    
    
    var FLW_CLASS_BY_KEY = {
        system: "Game_System", screen: "Game_Screen", timer: "Game_Timer",
        switches: "Game_Switches", variables: "Game_Variables",
        selfSwitches: "Game_SelfSwitches", actors: "Game_Actors",
        party: "Game_Party", map: "Game_Map", player: "Game_Player",
        _interpreter: "Game_Interpreter", _followers: "Game_Followers"
    };
    var FLW_ARR_CLASS_BY_KEY = {
        _events: "Game_Event", _pictures: "Game_Picture", _equips: "Game_Item"
    };
    var FLW_ARR_CLASS_BY_PARENT = {
        Game_Actors: { _data: "Game_Actor" },
        Game_Followers: { _data: "Game_Follower" }
    };
    
    var FLW_OBJ_CLASS_BY_PARENT = {
        Game_Map: { _events: "Game_Event" }
    };

    function flwIsPlainObject(v) {
        return v !== null && typeof v === "object" && !Array.isArray(v) &&
            Object.getPrototypeOf(v) === Object.prototype;
    }

    
    
    
    
    
    
    
    function flwMarkTree(node, parentCls) {
        if (!node || typeof node !== "object") return node;
        if (Array.isArray(node)) {
            for (var i = 0; i < node.length; i++) flwMarkTree(node[i], parentCls);
            return node;
        }
        if (node.__class__ || node["@"] || !flwIsPlainObject(node)) return node;
        for (var key in node) {
            if (!Object.prototype.hasOwnProperty.call(node, key)) continue;
            var v = node[key];
            if (v && typeof v === "object" && !Array.isArray(v)) {
                if (flwIsPlainObject(v)) {
                    var k1 = FLW_CLASS_BY_KEY[key];
                    if (k1) {
                        flwMarkTree(v, k1);
                        v["@"] = k1;
                        continue;
                    }
                    
                    var objCls = parentCls && FLW_OBJ_CLASS_BY_PARENT[parentCls] ? FLW_OBJ_CLASS_BY_PARENT[parentCls][key] : null;
                    if (objCls) {
                        for (var ek in v) {
                            if (!Object.prototype.hasOwnProperty.call(v, ek)) continue;
                            var el = v[ek];
                            if (el && typeof el === "object" && flwIsPlainObject(el)) {
                                flwMarkTree(el, objCls);
                                el["@"] = objCls;
                            }
                        }
                        continue;
                    }
                    flwMarkTree(v, parentCls);
                } else {
                    flwMarkTree(v, parentCls);
                }
                continue;
            }
            if (v && Array.isArray(v)) {
                var arrCls = null;
                if (parentCls && FLW_ARR_CLASS_BY_PARENT[parentCls] && FLW_ARR_CLASS_BY_PARENT[parentCls][key]) {
                    arrCls = FLW_ARR_CLASS_BY_PARENT[parentCls][key];
                } else if (FLW_ARR_CLASS_BY_KEY[key]) {
                    arrCls = FLW_ARR_CLASS_BY_KEY[key];
                }
                for (var j = 0; j < v.length; j++) {
                    var el2 = v[j];
                    if (el2 && typeof el2 === "object" && !Array.isArray(el2) && flwIsPlainObject(el2)) {
                        flwMarkTree(el2, arrCls || parentCls);
                        if (arrCls) el2["@"] = arrCls;
                    }
                }
            }
        }
        return node;
    }

    
    
    function flwReclass(obj, clsName) {
        var Ctor = window[clsName];
        if (typeof Ctor !== "function") return obj;
        var inst;
        try { inst = new Ctor(); } catch (e) { return obj; }
        for (var k in obj) {
            if (Object.prototype.hasOwnProperty.call(obj, k) && k !== "__class__" && k !== "@") {
                inst[k] = obj[k];
            }
        }
        return inst;
    }

    function flwReclassTree(node, parentCls) {
        if (!node || typeof node !== "object") return node;
        if (Array.isArray(node)) {
            for (var i = 0; i < node.length; i++) node[i] = flwReclassTree(node[i], parentCls);
            return node;
        }
        if (node.__class__ || node["@"]) return node;
        
        
        
        for (var key in node) {
            if (!Object.prototype.hasOwnProperty.call(node, key)) continue;
            var v = node[key];
            if (v && typeof v === "object" && !Array.isArray(v)) {
                var k1 = FLW_CLASS_BY_KEY[key];
                if (k1 && flwIsPlainObject(v)) {
                    node[key] = flwReclass(v, k1);
                    flwReclassTree(node[key], k1);
                } else {
                    var objCls = parentCls && FLW_OBJ_CLASS_BY_PARENT[parentCls] ? FLW_OBJ_CLASS_BY_PARENT[parentCls][key] : null;
                    if (objCls) {
                        for (var ek in v) {
                            if (!Object.prototype.hasOwnProperty.call(v, ek)) continue;
                            var el = v[ek];
                            if (el && typeof el === "object" && flwIsPlainObject(el)) {
                                v[ek] = flwReclass(el, objCls);
                            }
                            flwReclassTree(v[ek], objCls);
                        }
                    } else {
                        flwReclassTree(v, parentCls);
                    }
                }
                continue;
            }
            if (v && Array.isArray(v)) {
                var arrCls = null;
                if (parentCls && FLW_ARR_CLASS_BY_PARENT[parentCls] && FLW_ARR_CLASS_BY_PARENT[parentCls][key]) {
                    arrCls = FLW_ARR_CLASS_BY_PARENT[parentCls][key];
                } else if (FLW_ARR_CLASS_BY_KEY[key]) {
                    arrCls = FLW_ARR_CLASS_BY_KEY[key];
                }
                if (arrCls) {
                    for (var j = 0; j < v.length; j++) {
                        if (v[j] && typeof v[j] === "object" && flwIsPlainObject(v[j])) {
                            v[j] = flwReclass(v[j], arrCls);
                        }
                        flwReclassTree(v[j], arrCls);
                    }
                } else {
                    flwReclassTree(v, parentCls);
                }
            }
        }
        return node;
    }

    function repairLegacySave(text) {
        var tree = JSON.parse(text);
        if (typeof JsonEx !== "undefined" && typeof JsonEx._decode === "function") {
            flwMarkTree(tree, "");
            return JsonEx._decode(tree);
        }
        return flwReclassTree(tree, "");
    }

    // The bridge replaces StorageManager with a VFS/JsonEx based
    // implementation (no pako zip). The game loads rmmz_managers.js
    // AFTERWARDS and overwrites saveObject/loadObject with its own pako
    // based versions - and pako's binary zip does not survive the file
    // round trip, so every save was written corrupted.
    //
    // The hook is re-applied whenever the game replaced it: the _flwHooked
    // flag alone was not enough, it stayed set while the functions
    // underneath had been replaced.
    var flwStorageFns = null;

    function hookStorageManager() {
        if (typeof StorageManager === "undefined") return;
        if (flwStorageFns && StorageManager.saveObject === flwStorageFns.saveObject) {
            StorageManager._flwHooked = true;
            return;
        }
        if (!flwStorageFns) {
                flwStorageFns = {
                        saveObject: function (saveName, object) {
                                return window.FlowerStorage.save(this.filePath(saveName, ".rmmzsave"), jsonexStringify(object));
                        },
                        loadObject: function (saveName) {
                                return window.FlowerStorage.load(this.filePath(saveName, ".rmmzsave")).then(function (text) {
                                        if (text === null || typeof text !== "string") return null;
                                        if (text.indexOf('"@"') >= 0 || text.indexOf("__class__") >= 0) {
                                                try { return jsonexParse(text); } catch (e) { }
                                        }
                                        try {
                                                return repairLegacySave(text);
                                        } catch (e) { return null; }
                                });
                        },
                        fileExistsPath: function (path) {
                                return window.FlowerStorage.exists(path);
                        },
                        removeFile: function (path) {
                                return window.FlowerStorage.remove(path);
                        },
                        writeFileToForage: function (path, data) {
                                return window.FlowerStorage.save(path, data);
                        },
                        readFileFromForage: function (path) {
                                return window.FlowerStorage.load(path);
                        },
                        writeFileToFile: function (path, data) {
                                return window.FlowerStorage.save(path, data);
                        },
                        readFileFromFile: function (path) {
                                return window.FlowerStorage.load(path);
                        }
                };
        }
        StorageManager._flwHooked = true;
        StorageManager.isLocalMode = function () { return true; };
        StorageManager.saveObject = flwStorageFns.saveObject;
        StorageManager.loadObject = flwStorageFns.loadObject;
        StorageManager.fileExistsPath = flwStorageFns.fileExistsPath;
        StorageManager.removeFile = flwStorageFns.removeFile;
        StorageManager.writeFileToForage = flwStorageFns.writeFileToForage;
        StorageManager.readFileFromForage = flwStorageFns.readFileFromForage;
        StorageManager.writeFileToFile = flwStorageFns.writeFileToFile;
        StorageManager.readFileFromFile = flwStorageFns.readFileFromFile;
    }

    function hookUtils() {
        if (typeof Utils === "undefined") return;
        if (Utils._flwHooked) return;
        Utils._flwHooked = true;
        Utils.isNwjs = function () { return true; };
        Utils.isMobileDevice = function () {
            return /Android|iPhone|iPad|iPod|Mobile/i.test(navigator.userAgent) || window.Nugget._touch === true;
        };
        Utils.isMobileSafari = function () { return false; };
        Utils.isAndroidChrome = function () { return /Android/i.test(navigator.userAgent); };
    }

    window.onerror = function (msg, src, line, col) {
        rawCall("log", { level: 4, cat: "window", msg: "js error: " + msg + " at " + src + ":" + line + ":" + col });
    };
    window.addEventListener("unhandledrejection", function (ev) {
        rawCall("log", { level: 3, cat: "promise", msg: "unhandled rejection: " + (ev.reason && ev.reason.message ? ev.reason.message : String(ev.reason)) });
    });

    var everFocused = false;
    window.addEventListener("focus", function () { everFocused = true; }, true);

    function hookGraphics() {
        if (typeof Graphics === "undefined" || Graphics._flwHooked) return;
        Graphics._flwHooked = true;
        Graphics._requestFullScreen = function () { flwApplyFullscreen(true); };
        Graphics._cancelFullScreen = function () { flwApplyFullscreen(false); };
        Graphics._isFullScreen = function () { return flwFullscreen; };
    }

    function hookTouchClicks() {
        if (!("ontouchstart" in window) && window.Nugget._touch !== true) return;
        var started = null;
        document.addEventListener("touchstart", function (ev) {
            started = null;
            if (!ev.changedTouches || !ev.changedTouches.length) return;
            var t = ev.changedTouches[0];
            var el = document.elementFromPoint(t.clientX, t.clientY);
            if (!el) return;
            if (el.id === "flwpad" || (el.closest && el.closest("#flwpad"))) return;
            var canvas = null;
            try {
                if (window.Graphics && Graphics.app && Graphics.app.view) canvas = Graphics.app.view;
            } catch (e) { }
            if (el === canvas) return;
            started = { el: el, x: t.clientX, y: t.clientY };
        }, true);
        document.addEventListener("touchend", function (ev) {
            if (!started || !ev.changedTouches || !ev.changedTouches.length) return;
            var t = ev.changedTouches[0];
            var dx = t.clientX - started.x;
            var dy = t.clientY - started.y;
            if (dx * dx + dy * dy > 40 * 40) {
                started = null;
                return;
            }
            var el = started.el;
            started = null;
            try {
                var opts = { bubbles: true, cancelable: true, view: window, clientX: t.clientX, clientY: t.clientY, screenX: t.clientX, screenY: t.clientY, button: 0, buttons: 1 };
                el.dispatchEvent(new MouseEvent("mouseover", opts));
                el.dispatchEvent(new MouseEvent("mousedown", opts));
                var up = { bubbles: true, cancelable: true, view: window, clientX: t.clientX, clientY: t.clientY, screenX: t.clientX, screenY: t.clientY, button: 0, buttons: 0 };
                el.dispatchEvent(new MouseEvent("mouseup", up));
                el.dispatchEvent(new MouseEvent("click", up));
            } catch (e) { }
        }, true);
    }

    function hookSceneManager() {
        if (typeof SceneManager === "undefined" || SceneManager._flwFocusPatch) return;
        SceneManager._flwFocusPatch = true;
        var origActive = SceneManager.isGameActive;
        SceneManager.isGameActive = function () {
            var r = origActive.call(this);
            if (r) return true;
            if (everFocused) {
                return !document.hidden;
            }
            return true;
        };
    }


    var padKeyNames = { 13: "Enter", 16: "Shift", 27: "Escape", 37: "ArrowLeft", 38: "ArrowUp", 39: "ArrowRight", 40: "ArrowDown", 81: "q", 87: "w" };

    function firePadKey(type, keyCode) {
        var ev;
        try {
            ev = new KeyboardEvent(type, { bubbles: true, cancelable: true });
        } catch (e) {
            ev = document.createEvent("KeyboardEvent");
            ev.initKeyboardEvent(type, true, true, window, "", 0, false, false, false, false);
        }
        try {
            Object.defineProperty(ev, "keyCode", { get: function () { return keyCode; } });
            Object.defineProperty(ev, "which", { get: function () { return keyCode; } });
            Object.defineProperty(ev, "key", { get: function () { return padKeyNames[keyCode] || ""; } });
        } catch (e) { }
        document.dispatchEvent(ev);
    }

    function padButton(glyph, size, right, bottom, keyCode, arg6) {
        var onTap = typeof arg6 === "function" ? arg6 : null;
        var leftPos = typeof arg6 === "number" ? arg6 : null;
        var b = document.createElement("div");
        b.className = "flwpb";
        b.style.width = b.style.height = size + "px";
        if (right !== null && right !== undefined) {
            b.style.right = right + "px";
        } else if (leftPos !== null) {
            b.style.left = leftPos + "px";
        }
        b.style.bottom = bottom + "px";
        b.style.fontSize = Math.round(size * 0.42) + "px";
        b.textContent = glyph;
        if (onTap) {
            var tapDown = false;
            b.addEventListener("touchstart", function (ev) {
                ev.preventDefault();
                ev.stopPropagation();
                tapDown = true;
                b.classList.add("flwpba");
                onTap();
            }, false);
            var upFn = function (ev) {
                if (!tapDown) return;
                ev.preventDefault();
                ev.stopPropagation();
                tapDown = false;
                b.classList.remove("flwpba");
            };
            b.addEventListener("touchend", upFn, false);
            b.addEventListener("touchcancel", upFn, false);
        } else {
            var held = {};
            b.addEventListener("touchstart", function (ev) {
                ev.preventDefault();
                ev.stopPropagation();
                for (var i = 0; i < ev.changedTouches.length; i++) {
                    var id = ev.changedTouches[i].identifier;
                    if (held[id]) continue;
                    held[id] = true;
                    if (Object.keys(held).length === 1) {
                        firePadKey("keydown", keyCode);
                        b.classList.add("flwpba");
                    }
                }
            }, false);
            var endFn = function (ev) {
                for (var i = 0; i < ev.changedTouches.length; i++) {
                    var id = ev.changedTouches[i].identifier;
                    if (held[id]) {
                        delete held[id];
                        if (Object.keys(held).length === 0) {
                            firePadKey("keyup", keyCode);
                            b.classList.remove("flwpba");
                        }
                    }
                }
                ev.preventDefault();
                ev.stopPropagation();
            };
            b.addEventListener("touchend", endFn, false);
            b.addEventListener("touchcancel", endFn, false);
        }
        return b;
    }

    var kbdInput = null;

    function toggleSoftKbd() {
        if (kbdInput) {
            kbdInput.blur();
            kbdInput.remove();
            kbdInput = null;
            return;
        }
        kbdInput = document.createElement("input");
        kbdInput.type = "text";
        kbdInput.setAttribute("autocapitalize", "off");
        kbdInput.setAttribute("autocomplete", "off");
        kbdInput.style.cssText = "position:fixed;left:6px;bottom:6px;width:12px;height:12px;opacity:.3;z-index:99999;border:none;background:transparent;color:transparent;caret-color:transparent;outline:none";
        document.body.appendChild(kbdInput);
        kbdInput.addEventListener("blur", function () {
            setTimeout(function () {
                if (kbdInput && document.activeElement !== kbdInput) {
                    kbdInput.remove();
                    kbdInput = null;
                }
            }, 900);
        });
        kbdInput.focus();
    }

    var padInstalled = false;

    function installVirtualGamepad() {
        if (padInstalled) return;
        if (!document.body) {
            document.addEventListener("DOMContentLoaded", function () {
                installVirtualGamepad();
            }, { once: true });
            return;
        }
        padInstalled = true;
        var pad = document.createElement("div");
        pad.id = "flwpad";
        pad.style.cssText = "position:fixed;inset:0;z-index:99998;pointer-events:none;-webkit-user-select:none;user-select:none";
        var st = document.createElement("style");
        st.textContent = ".flwpb{position:absolute;pointer-events:auto;touch-action:none;box-sizing:border-box;border-radius:50%;background:rgba(16,20,31,.38);border:1.5px solid rgba(255,255,255,.30);color:rgba(255,255,255,.92);display:flex;align-items:center;justify-content:center;font-family:Inter,system-ui,sans-serif;font-weight:600;-webkit-tap-highlight-color:transparent;transition:opacity .18s}"
            + ".flwpba{background:rgba(255,255,255,.34);border-color:rgba(255,255,255,.85);color:#fff}"
            + "#flwpad .flwpbs{font-size:12px;font-weight:600}"
            + "#flwpad.flwpad-off .flwpb:not(.flwpad-tg){display:none}"
            + "#flwpad .flwpad-tg{opacity:.55}";
        pad.appendChild(st);
        pad.appendChild(padButton("\u25B2", 56, null, 148, 38, 82));
        pad.appendChild(padButton("\u25BC", 56, null, 36, 40, 82));
        pad.appendChild(padButton("\u25C0", 56, null, 92, 37, 18));
        pad.appendChild(padButton("\u25B6", 56, null, 92, 39, 146));
        pad.appendChild(padButton("Q", 40, null, 216, 81, 18));
        pad.appendChild(padButton("W", 40, null, 216, 87, 66));
        pad.appendChild(padButton("\u2715", 72, 18, 88, 13));
        pad.appendChild(padButton("\u25CB", 56, 102, 118, 27));
        pad.appendChild(padButton("\u25A1", 50, 34, 176, 16));
        var kbd = padButton("abc", 44, 144, 184, 0, toggleSoftKbd);
        kbd.classList.add("flwpbs");
        pad.appendChild(kbd);
        var vis = true;
        try { vis = localStorage.getItem("flwpad") !== "0"; } catch (e) { }
        if (!vis) pad.classList.add("flwpad-off");
        var tg = padButton("\u2261", 46, null, 6, 0, function () {
            var off = !pad.classList.contains("flwpad-off");
            if (off) pad.classList.add("flwpad-off");
            else pad.classList.remove("flwpad-off");
            try { localStorage.setItem("flwpad", off ? "0" : "1"); } catch (e) { }
        });
        tg.classList.add("flwpbs");
        tg.classList.add("flwpad-tg");
        tg.style.left = "calc(50% - 23px)";
        tg.style.right = "auto";
        pad.appendChild(tg);
        document.body.appendChild(pad);
    }

    var started = false;
    hookTouchClicks();
    var pollTimer = setInterval(function () {
        hookUtils();
        hookStorageManager();
        hookSceneManager();
        hookGraphics();
        if (!started && typeof SceneManager !== "undefined" && SceneManager._scene &&
            !(SceneManager._scene instanceof Scene_Boot)) {
            started = true;
            clearInterval(pollTimer);
            removeSplash();
            rawCall("event", { name: "game.ready", data: "" });
            if (window.Nugget && Nugget._resolveReady) {
                Nugget._resolveReady(window.Nugget);
            }
        }
    }, 120);
    setTimeout(function () { clearInterval(pollTimer); removeSplash(); }, 90000);

    
    
    
    if (!document.querySelector('script[src*="rmmz"], script[src*="RMMZ"], script[src*="plugins.js"]')) {
        setTimeout(function () {
            if (!started) {
                started = true;
                clearInterval(pollTimer);
                removeSplash();
                rawCall("event", { name: "game.ready", data: "plain" });
                if (window.Nugget && Nugget._resolveReady) {
                    Nugget._resolveReady(window.Nugget);
                }
            }
        }, 700);
    }

    Nugget.os().then(function (osinfo) {
        if (osinfo && osinfo.platform === "android") {
            Nugget._touch = true;
            document.documentElement.classList.add("flw-touch");
            installVirtualGamepad();
        }
        setTimeout(function () {
            if (!padInstalled && /Android/i.test(navigator.userAgent) && "ontouchstart" in window) {
                Nugget._touch = true;
                installVirtualGamepad();
            }
        }, 4000);
        if (osinfo && osinfo.wine) {
            document.documentElement.classList.add("flw-wine");
        }
    });

    rawCall("window.pos", {}).then(function (r) {
        if (r && r.ok && r.v && r.v.known) {
            winPosCache.x = r.v.x | 0;
            winPosCache.y = r.v.y | 0;
            winPosCache.known = true;
        }
    });

    
    
    
    window.addEventListener("keydown", function (ev) {
        if (ev.key === "F10" && window.__FLWDEV) {
            ev.preventDefault();
            ev.stopPropagation();
            rawCall("event", { name: "nugget.button", data: "f10" });
        }
    }, { capture: true });

    
    
    
    function flwReportError(msg, file, line) {
        var text = String(msg || "error").slice(0, 280);
        var where = "";
        if (file) where = " (" + String(file).split("/").slice(-1)[0] + ":" + (line || 0) + ")";
        rawCall("event", { name: "game.error", data: text + where });
    }
    window.addEventListener("error", function (ev) {
        if (ev && (ev.message || ev.error)) {
            flwReportError(ev.message || (ev.error && ev.error.message), ev.filename, ev.lineno);
        }
    });
    window.addEventListener("unhandledrejection", function (ev) {
        var r = ev && ev.reason;
        flwReportError("unhandled rejection: " + (r && r.message ? r.message : r), "", 0);
    });

    
    
    
    var flwOrigClose = window.close;
    window.close = function () {
        try { rawCall("game.exit", {}); } catch (e) {  }
        try { if (typeof flwOrigClose === "function") flwOrigClose.call(window); } catch (e2) {  }
    };

    
    
    
    
    var NUGGET_NAV_KEYS = { ArrowUp: 1, ArrowDown: 1, ArrowLeft: 1, ArrowRight: 1, Enter: 1, Escape: 1, Backspace: 1 };
    document.addEventListener("keydown", function (ev) {
        if (!window.__NUGGET_KEYHOOK) return;
        if (ev.key === "F10") return;
        if (ev.key.length === 1 || NUGGET_NAV_KEYS[ev.key]) {
            ev.preventDefault();
            ev.stopPropagation();
            rawCall("event", { name: "nugget.input", data: ev.key });
        }
    }, { capture: true });

    
    
    
    (function patchFreeze() {
        var tries = 0;
        function tryPatch() {
            if (typeof Game_Map === "undefined" || !Game_Map.prototype) {
                if (tries++ < 240) setTimeout(tryPatch, 500);
                return;
            }
            if (Game_Map.prototype.__nuggetFreeze) return;
            var orig = Game_Map.prototype.update;
            Game_Map.prototype.update = function (sceneActive) {
                if (window.__NUGGET_FREEZE) return;
                return orig.call(this, sceneActive);
            };
            Game_Map.prototype.__nuggetFreeze = true;
        }
        tryPatch();
    })();

    window.flwbridge = {
        version: "1.0",
        Nugget: Nugget,
        FlowerStorage: FlowerStorage,
        path: pathShim,
        fs: fsShim
    };
})();
