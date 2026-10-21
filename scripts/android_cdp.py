#!/usr/bin/env python3
"""Drive the Android build through the WebView devtools protocol.

Used to check the Android specific behaviour of the bridge (the window shake
becomes a vibration there) without a display.

Usage: scripts/android_cdp.py <command> [args...]
    eval <javascript>   evaluate javascript in the game page
    pages              list the debuggable pages
"""
import json
import os
import subprocess
import sys
import time
import urllib.request

import websocket


def adb(*args):
    return subprocess.run(["adb"] + list(args), capture_output=True, text=True).stdout


def devtools_socket():
    out = adb("shell", "cat", "/proc/net/unix")
    for line in out.splitlines():
        if "webview_devtools_remote" in line:
            # "@webview_devtools_remote_1234" -> the full socket name
            name = line.split("@", 1)[1].strip()
            return name
    return None


def devtools_socket_for(pkg):
    """The devtools socket of the process that serves our package."""
    out = adb("shell", "cat", "/proc/net/unix")
    pids = subprocess.run(["adb", "shell", "ps", "-A", "-o", "PID,NAME"],
                          capture_output=True, text=True).stdout
    wanted = []
    for line in pids.splitlines():
        if pkg in line:
            wanted.append(line.split()[0])
    for line in out.splitlines():
        if "webview_devtools_remote" not in line:
            continue
        name = line.split("@", 1)[1].strip()
        pid = name.rsplit("_", 1)[-1]
        if pid in wanted:
            return name
    return None


def forward():
    sock = devtools_socket_for("su.neakami.flower") or devtools_socket()
    if not sock:
        raise SystemExit("no webview devtools socket found (is the app running?)")
    subprocess.run(["adb", "forward", "--remove-all"], capture_output=True)
    r = subprocess.run(["adb", "forward", "tcp:9222", "localabstract:" + sock],
                       capture_output=True, text=True)
    if r.returncode != 0:
        raise SystemExit("adb forward failed: " + r.stderr)
    return sock


def pages():
    last = None
    for _ in range(10):
        try:
            with urllib.request.urlopen("http://127.0.0.1:9222/json", timeout=10) as r:
                return json.load(r)
        except Exception as e:      # the forward needs a moment
            last = e
            time.sleep(0.5)
    raise SystemExit("devtools not reachable: %s" % last)


def main():
    if len(sys.argv) < 2:
        print(__doc__)
        return 1
    cmd = sys.argv[1]
    forward()
    time.sleep(0.5)
    if cmd == "pages":
        for p in pages():
            print(p.get("title"), p.get("url"), p.get("webSocketDebuggerUrl"))
        return 0
    if cmd != "eval":
        print(__doc__)
        return 1
    js = sys.argv[2]
    url = None
    for p in pages():
        if p.get("type") == "page" and "127.0.0.1" in p.get("url", ""):
            url = p.get("webSocketDebuggerUrl")
    if not url:
        for p in pages():
            if p.get("type") == "page":
                url = p.get("webSocketDebuggerUrl")
    if not url:
        raise SystemExit("no debuggable page found")
    ws = websocket.create_connection(url, timeout=20,
                                     origin="http://127.0.0.1:9222",
                                     suppress_origin=True)
    try:
        ws.send(json.dumps({
            "id": 1,
            "method": "Runtime.evaluate",
            "params": {"expression": js, "returnByValue": True,
                       "awaitPromise": True},
        }))
        deadline = time.time() + 20
        while time.time() < deadline:
            msg = json.loads(ws.recv())
            if msg.get("id") == 1:
                res = msg.get("result", {})
                if "exceptionDetails" in res:
                    print("EXCEPTION:", json.dumps(res["exceptionDetails"])[:400])
                    return 2
                out = res.get("result", {})
                print(json.dumps(out.get("value", out), ensure_ascii=False)[:2000])
                return 0
    finally:
        ws.close()
    return 1


if __name__ == "__main__":
    sys.exit(main())
