


(function () {
    "use strict";

    var N = window.Nugget || null;
    var rawCall = window.flwCall || null;
    var echoEvents = false;

    
    var termBody = document.getElementById("term-body");
    var termStatus = document.getElementById("term-status");

    function ts() {
        var d = new Date();
        function p(n, w) { n = String(n); while (n.length < (w || 2)) n = "0" + n; return n; }
        return p(d.getHours()) + ":" + p(d.getMinutes()) + ":" + p(d.getSeconds());
    }
    function log(cls, msg) {
        var ln = document.createElement("div");
        ln.className = "ln";
        var t = document.createElement("span");
        t.className = "ts"; t.textContent = ts() + " ";
        ln.appendChild(t);
        var m = document.createElement("span");
        m.className = cls; m.textContent = msg;
        ln.appendChild(m);
        termBody.appendChild(ln);
        while (termBody.childNodes.length > 250) termBody.removeChild(termBody.firstChild);
        termBody.scrollTop = termBody.scrollHeight;
    }
    function fmt(v) {
        if (v === undefined) return "undefined";
        try { return typeof v === "string" ? v : JSON.stringify(v); }
        catch (e) { return String(v); }
    }
    function status(s) { termStatus.textContent = s; }

    
    function call(label, op, args, pre) {
        status("→ " + label);
        log("in", "▸ " + label);
        if (!rawCall) {
            log("err", "  no bridge (window.flwCall missing) — running outside the engine?");
            status("no bridge");
            return Promise.resolve(null);
        }
        return rawCall(op, args || {}).then(function (r) {
            if (pre) r = pre(r);
            log(r && r.ok ? "ok" : "err", "  " + fmt(r));
            status(r && r.ok ? "ok: " + label : "err: " + label);
            return r;
        }, function (e) {
            log("err", "  " + String(e));
            status("err: " + label);
        });
    }

    
    var chip = document.getElementById("bridge-chip");
    if (N && rawCall) {
        chip.textContent = "bridge: ready";
        chip.classList.add("ok");
        log("ok", "Nugget bridge detected — flwbridge.js injected");
    } else {
        chip.textContent = "bridge: missing";
        chip.classList.add("err");
        log("err", "Nugget bridge NOT found. Package this app (flwpack) to get the bridge.");
    }

    
    function tick() {
        var d = new Date();
        function p(n) { return (n < 10 ? "0" : "") + n; }
        document.getElementById("tb-clock").textContent = p(d.getHours()) + ":" + p(d.getMinutes());
    }
    tick(); setInterval(tick, 5000);

    
    var apiFns = {
        os: function () { return call("Nugget.getOS()", "os", {}); },
        engine: function () { return call("Nugget.getEngineInfo()", "engine", {}); },
        touch: function () { return call("Nugget.isTouch()", "touch", {}); },
        sysinfo: function () { return call('flwCall("sysinfo")', "sysinfo", {}); },
    };
    Array.prototype.forEach.call(document.querySelectorAll("[data-api]"), function (b) {
        b.addEventListener("click", function () { apiFns[b.dataset.api](); });
    });

    
    var lvl = document.getElementById("log-level");
    lvl.addEventListener("input", function () {
        document.getElementById("log-level-v").textContent = lvl.value;
    });
    document.getElementById("btn-log").addEventListener("click", function () {
        var cat = document.getElementById("log-cat").value || "apidemos";
        var msg = document.getElementById("log-msg").value || "";
        call("Nugget.log(" + lvl.value + ", " + cat + ", " + msg + ")", "log",
            { level: Number(lvl.value), cat: cat, msg: msg });
    });

    
    document.getElementById("btn-shell").addEventListener("click", function () {
        var cmd = document.getElementById("sh-cmd").value;
        call("Nugget.shellExec('sh', ['-c', …])", "shell",
            { cmd: "sh", args: ["-c", cmd], dir: "", timeout: 30 },
            function (r) {
                if (r && r.ok && r.v && typeof r.v.stdout !== "undefined") {
                    log("dim", "  stdout: " + String(r.v.stdout).trim() + " (code " + r.v.code + ")");
                    return null;
                }
                return r;
            });
    });
    document.getElementById("btn-shell-raw").addEventListener("click", function () {
        var cmd = document.getElementById("sh-cmd").value;
        call('flwCall("shell.exec")', "shell.exec", { command: cmd }, function (r) {
            if (r && r.ok && r.v && typeof r.v.stdout !== "undefined") {
                log("dim", "  stdout: " + String(r.v.stdout).trim() + " (code " + r.v.code + ")");
                return null;
            }
            return r;
        });
    });

    
    Array.prototype.forEach.call(document.querySelectorAll("[data-move]"), function (b) {
        b.addEventListener("click", function () {
            var xy = b.dataset.move.split(",");
            call("Nugget.moveWindow(" + xy[0] + ", " + xy[1] + ")", "window.move",
                { x: Number(xy[0]), y: Number(xy[1]) });
        });
    });
    document.getElementById("btn-wtitle").addEventListener("click", function () {
        var t = document.getElementById("win-title").value;
        if (N && N.setWindow) { N.setWindow(t); log("ok", "▸ setWindow(" + t + ")"); }
    });
    document.getElementById("btn-wpos").addEventListener("click", function () {
        call('flwCall("window.pos")', "window.pos", {});
    });
    function setSize(w, h, fs) {
        if (N && N.setWindow) { N.setWindow(null, w, h, fs); log("ok", "▸ setWindow(null, " + w + ", " + h + ", " + !!fs + ")"); }
    }
    document.getElementById("btn-wsmall").addEventListener("click", function () { setSize(640, 360, false); });
    document.getElementById("btn-wnormal").addEventListener("click", function () { setSize(1280, 720, false); });
    document.getElementById("btn-wfs").addEventListener("click", function () { setSize(1280, 720, true); });

    
    document.getElementById("btn-dialog").addEventListener("click", function () {
        call("Nugget.dialog(text, title)", "dialog", {
            text: document.getElementById("dlg-text").value,
            title: document.getElementById("dlg-title").value || "API Demos",
        });
    });

    
    Array.prototype.forEach.call(document.querySelectorAll("[data-fs]"), function (b) {
        b.addEventListener("click", function () {
            var op = b.dataset.fs;
            var path = document.getElementById("fs-path").value || "";
            var args = { path: path };
            if (op === "fs.write") args.content = document.getElementById("fs-content").value;
            call('flwCall("' + op + '")', op, args, function (r) {
                if (r && r.ok && op === "fs.list" && r.v) {
                    var names = (r.v || []).map(function (it) { return it.name || String(it); });
                    log("dim", "  " + names.length + " entries: " + names.slice(0, 24).join(", ") + (names.length > 24 ? ", …" : ""));
                    return null;
                }
                return r;
            });
        });
    });

    
    var tumbEcho = document.getElementById("tumb-echo");
    tumbEcho.addEventListener("click", function () {
        echoEvents = !echoEvents;
        tumbEcho.setAttribute("aria-pressed", echoEvents ? "true" : "false");
        log("dim", "▸ echo events " + (echoEvents ? "on" : "off"));
    });
    document.getElementById("btn-emit").addEventListener("click", function () {
        var name = document.getElementById("ev-name").value || "apidemos.ping";
        var data = document.getElementById("ev-data").value || "";
        if (N && N.emitEvent) {
            N.emitEvent(name, data);
            log("ok", "▸ emitEvent(" + name + ", " + data + ")");
        }
    });
    document.getElementById("btn-rpc-login").addEventListener("click", function () {
        call('flwCall("rpc.login")', "rpc.login", { clientid: "apidemos" });
    });
    document.getElementById("btn-rpc-clear").addEventListener("click", function () {
        call('flwCall("rpc.clear")', "rpc.clear", {});
    });

    
    if (N && N.onEvent) {
        N.onEvent(function (name, data) {
            if (echoEvents) log("dim", "◀ event " + name + " " + (data || ""));
        });
    }

    
    var tumbVibe = document.getElementById("tumb-vibe");
    tumbVibe.addEventListener("click", function () {
        var on = tumbVibe.getAttribute("aria-pressed") !== "true";
        tumbVibe.setAttribute("aria-pressed", on ? "true" : "false");
    });
    document.getElementById("btn-vibe").addEventListener("click", function () {
        if (N && N.vibrate) { N.vibrate(80); log("ok", "▸ vibrate(80)"); }
        else log("err", "vibrate unavailable");
    });
    document.getElementById("btn-exit").addEventListener("click", function () {
        if (tumbVibe.getAttribute("aria-pressed") === "true" && N && N.vibrate) N.vibrate(120);
        if (N && N.exitGame) {
            log("dim", "▸ exitGame() — back to nuggetos");
            setTimeout(function () { N.exitGame(); }, 120);
        } else {
            log("err", "exitGame unavailable");
        }
    });

    
    document.getElementById("btn-clear").addEventListener("click", function () {
        termBody.innerHTML = "";
        status("idle");
    });

    
    document.getElementById("sh-cmd").addEventListener("keydown", function (e) {
        if (e.key === "Enter") document.getElementById("btn-shell").click();
    });

    log("dim", "api demos ready — every button is a live bridge call");
})();
