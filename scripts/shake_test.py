#!/usr/bin/env python3
"""Regression test for the window shake.

The game shakes its window (data/Map003.json / Map053.json): it captures the
current position once and then calls nw.Window.get().moveTo(base+dx, base+dy)
every 30 ms. The engine has to

  * keep the window around the real base position (not around a stale
    cached one - the window used to jump to the top left corner),
  * restore the exact base position when the shaking stops (the game never
    sends a "stop" itself).

Usage: scripts/shake_test.py [engine-stdout.log]
"""
import json
import os
import re
import socket
import subprocess
import sys
import time

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
DISP = os.environ.get("FLW_DISPLAY", ":99")


def sh(cmd):
    env = dict(os.environ, DISPLAY=DISP)
    return subprocess.run(cmd, shell=True, env=env,
                          capture_output=True, text=True).stdout.strip()


def rpc_connect(logpath):
    data = open(logpath, errors="replace").read()
    m = re.search(r"FLWGAME port=(\d+) rpc=(\d+) token=(\S+)", data)
    if not m:
        raise SystemExit("no FLWGAME handle in " + logpath)
    s = socket.create_connection(("127.0.0.1", int(m.group(2))), 10)
    s.settimeout(20)
    f = s.makefile("r", encoding="utf-8")
    s.sendall((json.dumps({"cmd": "auth", "name": m.group(3)}) + "\n").encode())
    f.readline()
    return s, f


def rpc(s, f, cmd, js=""):
    s.sendall((json.dumps({"cmd": cmd, "js": js}) + "\n").encode())
    return json.loads(f.readline())


def game_window():
    out = sh("xdotool search --name Rosemary | tail -1")
    return out


def game_pos(w):
    geo = sh("xdotool getwindowgeometry --shell %s" % w)
    m = re.search(r"^X=(-?\d+)", geo, re.M)
    n = re.search(r"^Y=(-?\d+)", geo, re.M)
    if not m or not n:
        return None
    return int(m.group(1)), int(n.group(1))


def main():
    logpath = sys.argv[1] if len(sys.argv) > 1 else "/tmp/opencode/live/engine-stdout.log"
    s, f = rpc_connect(logpath)
    fails = []

    def check(name, ok, detail=""):
        print("%s %s%s" % ("PASS" if ok else "FAIL", name,
                           ("  -- " + str(detail)) if detail else ""))
        if not ok:
            fails.append(name)

    w = game_window()
    check("game window found", bool(w), w)
    if not w:
        return 1

    base = game_pos(w)
    print("window base: %s" % (base,))
    check("the window is on screen", base is not None and base[0] > -100 and base[1] > -100, base)

    # The page must know the real position, otherwise it would shake around
    # a stale value and the window would jump to the top left corner.
    # The engine syncs the position every 75 ms, but the sync is queued
    # behind the game's frames, so wait for the convergence.
    page = ""
    for _ in range(20):
        js_pos = rpc(s, f, "eval", "(function(){var w=nw.Window.get();return w.x+','+w.y})()")
        page = str(js_pos.get("result", ""))
        if "," in page and base:
            try:
                px, py = [int(v) for v in page.split(",")]
                if abs(px - base[0]) <= 60 and abs(py - base[1]) <= 60:
                    break
            except ValueError:
                pass
        time.sleep(0.4)
    check("the page knows the window position", "," in page, page)
    if "," in page and base:
        try:
            px, py = [int(v) for v in page.split(",")]
            # The page uses the client area origin, X11 reports the window
            # frame: a small offset is expected.
            check("the page position matches the real one",
                  abs(px - base[0]) <= 60 and abs(py - base[1]) <= 60,
                  "page=%s real=%s" % ((px, py), base))
        except ValueError:
            pass

    # A window manager move (the user drags the window) must be picked up.
    rpc(s, f, "eval", "(function(){var w=nw.Window.get();return w.x+','+w.y})()")
    sh("xdotool windowmove %s %d %d" % (w, base[0] + 70, base[1] + 40))
    time.sleep(0.8)
    moved_real = game_pos(w)
    js_pos2 = rpc(s, f, "eval", "(function(){var w=nw.Window.get();return w.x+','+w.y})()")
    page2 = str(js_pos2.get("result", ""))
    check("the page follows a window manager move", "," in page2 and moved_real != base,
          "real=%s page=%s" % (moved_real, page2))
    if "," in page2 and moved_real:
        try:
            px, py = [int(v) for v in page2.split(",")]
            check("the page position is up to date after the move",
                  abs(px - moved_real[0]) <= 40 and abs(py - moved_real[1]) <= 40,
                  "page=%s real=%s" % ((px, py), moved_real))
        except ValueError:
            pass
    # Put the window back where it was.
    sh("xdotool windowmove %s %d %d" % (w, base[0], base[1]))
    time.sleep(0.6)
    base = game_pos(w)

    # Run the shake: 20 moves around the base, 30 ms apart (like the game).
    # The offset is constant so that the window clearly ends up away from the
    # base while shaking and has to return to it afterwards.
    rpc(s, f, "eval", """
(function(){
  var w = nw.Window.get();
  window.__shakeBase = {x: w.x, y: w.y};
  window.__shakeLog = [];
  if (window.__shakeTimer) clearInterval(window.__shakeTimer);
  var i = 0;
  window.__shakeTimer = setInterval(function(){
    w.moveTo(window.__shakeBase.x + 10, window.__shakeBase.y + 10);
    window.__shakeLog.push([w.x, w.y]);
    if (++i >= 20) { clearInterval(window.__shakeTimer); window.__shakeTimer = null; }
  }, 30);
  return 'started';
})()""")

    # Watch the window while it is shaking.
    moved = False
    samples = []
    for _ in range(12):
        time.sleep(0.2)
        p = game_pos(w)
        samples.append(p)
        if p and base and (abs(p[0] - base[0]) > 4 or abs(p[1] - base[1]) > 4):
            moved = True
            break
    during = samples[-1] if samples else None
    print("window during the shake: %s" % (during,))
    check("the window moved while shaking", moved,
          "base=%s during=%s" % (base, during))
    if during and base:
        dx = abs(during[0] - base[0])
        dy = abs(during[1] - base[1])
        # The jitter is at most 13 px around the base (plus the window frame
        # offset of a few pixels).
        check("the window stayed around the base position", dx <= 45 and dy <= 45,
              "dx=%d dy=%d" % (dx, dy))

    # Stop the shake (the game stops it when the event ends) and let the
    # engine snap the window back to the base. The game never sends a "stop"
    # itself, the engine has to notice that the moves stopped.
    rpc(s, f, "eval", "(function(){if(window.__shakeTimer){clearInterval(window.__shakeTimer);window.__shakeTimer=null;}return 'stopped'})()")
    time.sleep(2.0)
    after = game_pos(w)
    print("window after the shake: %s" % (after,))
    # The window must be back at the base: the engine snaps it back on its
    # own, the game never sends a "stop". A few pixels of tolerance for the
    # window manager.
    check("the window snapped back to the base position",
          after is not None and base is not None
          and abs(after[0] - base[0]) <= 12 and abs(after[1] - base[1]) <= 12,
          "base=%s after=%s" % (base, after))

    # An isolated move must not be snapped back (the apidemos window demo
    # moves the window on purpose).
    rpc(s, f, "eval", "(function(){var w=nw.Window.get();w.moveTo(w.x+120, w.y+90);return 'moved'})()")
    time.sleep(1.5)
    moved = game_pos(w)
    print("window after an isolated move: %s" % (moved,))
    check("an isolated move is kept", moved is not None and moved != base,
          "base=%s moved=%s" % (base, moved))
    # Put it back.
    rpc(s, f, "eval", "(function(){var w=nw.Window.get();w.moveTo(%d, %d);return 'back'})()"
        % (base[0], base[1]))
    time.sleep(1.0)

    print("\n%d failed" % len(fails))
    for f_ in fails:
        print("  -", f_)
    return 1 if fails else 0


if __name__ == "__main__":
    sys.exit(main())
