#!/usr/bin/env python3
"""Regression test for the shake on Android.

On Android the window cannot be moved, so the shake of the game is turned
into a vibration: every nw.Window.get().moveTo() of the shake script makes
the phone vibrate for 26 ms.

The test drives the game page through the WebView devtools protocol, starts a
shake and watches the vibrator of the system.

Usage: scripts/android_shake_test.py
"""
import os
import re
import subprocess
import sys
import time

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import android_cdp  # noqa: E402

SHAKE_JS = """
(function(){
  var w = nw.Window.get();
  var i = 0;
  if (window.__flwShakeTimer) clearInterval(window.__flwShakeTimer);
  window.__flwShakeTimer = setInterval(function(){
    var dx = Math.floor((Math.random()-0.5)*26);
    var dy = Math.floor((Math.random()-0.5)*26);
    w.moveTo(dx, dy);
    if (++i >= 60) { clearInterval(window.__flwShakeTimer); window.__flwShakeTimer = null; }
  }, 30);
  return 'shake started';
})()
"""


def sh(*args):
    return subprocess.run(["adb"] + list(args), capture_output=True, text=True).stdout


def vibrator_state():
    out = sh("shell", "dumpsys", "vibrator_manager")
    m = re.search(r"mIsVibrating=(true|false)", out)
    a = re.search(r"mCurrentAmplitude=([0-9.]+)", out)
    return (m.group(1) == "true" if m else None,
            float(a.group(1)) if a else None)


def main():
    fails = []

    def check(name, ok, detail=""):
        print("%s %s%s" % ("PASS" if ok else "FAIL", name,
                           ("  -- " + str(detail)) if detail else ""))
        if not ok:
            fails.append(name)

    # The bridge must know that it runs on Android.
    res = android_cdp.main.__module__ and None
    import json
    import urllib.request
    android_cdp.forward()
    time.sleep(0.5)
    pages = android_cdp.pages()
    url = None
    for p in pages:
        if p.get("type") == "page" and "127.0.0.1" in p.get("url", ""):
            url = p.get("webSocketDebuggerUrl")
    if not url:
        for p in pages:
            if p.get("type") == "page":
                url = p.get("webSocketDebuggerUrl")
    if not url:
        raise SystemExit("no debuggable page (is the app running?)")

    import websocket
    ws = websocket.create_connection(url, timeout=20, suppress_origin=True)

    def evaluate(js):
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
                r = msg.get("result", {})
                if "exceptionDetails" in r:
                    return "EXC:" + json.dumps(r["exceptionDetails"])[:200]
                return r.get("result", {}).get("value")
        return None

    try:
        env = evaluate("String(window.__FLWENV && window.__FLWENV.FLW_OS_NAME)")
        check("the bridge runs on android", env == "android", env)
        check("the native bridge is available",
              evaluate("typeof window.flwNativeAndroid") == "object")
        check("the native bridge can vibrate",
              evaluate("typeof (window.flwNativeAndroid && window.flwNativeAndroid.vibrate)")
              == "function")

        # The vibrator must be idle before the shake.
        time.sleep(0.6)
        before = vibrator_state()
        check("the vibrator is idle before the shake", before[0] is False, before)

        evaluate(SHAKE_JS)
        active = False
        amp = 0.0
        for _ in range(14):
            time.sleep(0.35)
            on, a = vibrator_state()
            if on:
                active = True
                amp = max(amp, a or 0.0)
        check("the shake makes the phone vibrate", active, "amplitude=%s" % amp)
        check("the vibration is not silent", amp > 0.1, "amplitude=%s" % amp)

        time.sleep(1.5)
        after = vibrator_state()
        check("the vibration stops with the shake", after[0] is False, after)
    finally:
        ws.close()

    print("\n%d failed" % len(fails))
    for f_ in fails:
        print("  -", f_)
    return 1 if fails else 0


if __name__ == "__main__":
    sys.exit(main())
