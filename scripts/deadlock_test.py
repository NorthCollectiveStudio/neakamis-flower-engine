#!/usr/bin/env python3
"""Regression test for the dev console self-deadlock.

The dev console's update path used to call activate() while holding dc.mu;
activate() -> runAsync() locks the same mutex again. sync.Mutex is not
reentrant, so the ebiten UI thread blocked forever and the whole shell
froze (the user could only kill the process).

This test drives the console through the *mouse* path (a click on a menu
item). The keyboard path went through pushKey(), which already used the
unlock-around pattern, so the bug only showed up with the mouse - which is
exactly what a user does.

Detection: a frozen shell cannot run the watchdog, so the game keeps
running; when the shell is alive the watchdog kills the game after a stall.
The test therefore clicks the console items and then checks that the game
is still alive after the watchdog window (and that the engine logged no
"watchdog" entry).

Usage: scripts/deadlock_test.py [engine-stdout.log]
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
WATCHDOG_WAIT = float(os.environ.get("FLW_DEADLOCK_WAIT", "45"))

# dev console layout (internal/nuggetos/devconsole.go)
DC_COL_X, DC_COL_W, DC_COL_GAP = 40.0, 232.0, 8.0
DC_COL_Y, DC_HDR_H, DC_ITEM_H = 150.0, 26.0, 28.0


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


def item_xy(col, row, ox, oy):
    x = DC_COL_X + col * (DC_COL_W + DC_COL_GAP)
    y = DC_COL_Y + DC_HDR_H + row * DC_ITEM_H
    return int(x + 30 + ox), int(y + 14 + oy)


def click(x, y):
    sh("xdotool mousemove --sync %d %d" % (x, y))
    time.sleep(0.25)
    sh("xdotool mousedown 1; sleep 0.25; xdotool mouseup 1")
    time.sleep(0.6)


def click_item(shell, col, row):
    """Click an item of the dev console.

    A freshly opened submenu already has sel=0, so a single click on its
    first row activates it. Clicking the same row twice would push a second
    copy of the submenu, and the next column click would then land on the
    middle layer (which only selects, it does not activate).
    """
    geo = sh("xdotool getwindowgeometry --shell %s" % shell)
    # "xdotool getwindowgeometry --shell" prints X=/Y= lines, not "Position:".
    mx = re.search(r"^X=(-?\d+)", geo, re.M)
    my = re.search(r"^Y=(-?\d+)", geo, re.M)
    ox = int(mx.group(1)) if mx else 0
    oy = int(my.group(1)) if my else 0
    x, y = item_xy(col, row, ox, oy)
    click(x, y)


def shell_frame(shell):
    """Screenshot of the shell window as raw pixels (or None)."""
    out = "/tmp/opencode/_dc_shot.png"
    sh("import -window root %s" % out)
    try:
        return open(out, "rb").read()
    except OSError:
        return None


def shell_is_frozen(shell):
    """True when the shell window stops redrawing.

    A deadlocked UI thread freezes the last rendered frame, so two
    screenshots taken a few seconds apart are byte identical. A live shell
    keeps updating the playtime/clock in the dev info panel.
    """
    a = shell_frame(shell)
    time.sleep(3.0)
    b = shell_frame(shell)
    if a is None or b is None:
        return None
    return a == b


def shell_geom(shell):
    """Window origin of the shell (xdotool --shell prints X=/Y= lines)."""
    geo = sh("xdotool getwindowgeometry --shell %s" % shell)
    mx = re.search(r"^X=(-?\d+)", geo, re.M)
    my = re.search(r"^Y=(-?\d+)", geo, re.M)
    return (int(mx.group(1)) if mx else 0, int(my.group(1)) if my else 0)


def open_dev_console(game, shell):
    """Open the dev console and make sure the shell window is on screen.

    F10 is a toggle, so the console may already be open (then the keypress
    would close it and the shell would hide again).
    """
    for _ in range(3):
        sh("xdotool windowactivate --sync %s" % game)
        time.sleep(0.6)
        sh("xdotool key --clearmodifiers F10")
        time.sleep(3.0)
        ox, oy = shell_geom(shell)
        if ox > -100 and oy > -100:
            return True
    return False


def main():
    logpath = sys.argv[1] if len(sys.argv) > 1 else "/tmp/opencode/live/engine-stdout.log"
    fails = []

    def check(name, ok, detail=""):
        print("%s %s%s" % ("PASS" if ok else "FAIL", name,
                           ("  -- " + str(detail)) if detail else ""))
        if not ok:
            fails.append(name)

    s, f = rpc_connect(logpath)
    r = rpc(s, f, "eval", "1+1")
    check("game is responsive", bool(r.get("ok")), r)

    # The shell hides its own window while the game runs, so F10 has to be
    # sent to the GAME window: the game's keyhook forwards it to the shell,
    # which then opens the dev console and lowers the game window.
    game = sh("xdotool search --name Rosemary | tail -1")
    check("game window found", bool(game), game)
    shell = sh("xdotool search --name NuggetOS | tail -1")
    if game:
        check("the dev console opens", open_dev_console(game, shell))

    check("shell window found", bool(shell), shell)
    if not shell:
        return 1
    geo = sh("xdotool getwindowgeometry --shell %s" % shell)
    # "xdotool getwindowgeometry --shell" prints X=/Y= lines, not "Position:".
    mx = re.search(r"^X=(-?\d+)", geo, re.M)
    my = re.search(r"^Y=(-?\d+)", geo, re.M)
    ox = int(mx.group(1)) if mx else 0
    oy = int(my.group(1)) if my else 0
    print("shell at %d,%d" % (ox, oy))
    check("shell is on screen", ox > -100 and oy > -100,
          "the shell must be visible for the clicks to reach it")

    # The mouse events go to the focused window: focus the shell, otherwise
    # the clicks land on the game window.
    sh("xdotool windowactivate --sync %s" % shell)
    time.sleep(0.8)

    frozen_before = shell_is_frozen(shell)
    check("shell is rendering before the clicks", frozen_before is False,
          "frames identical: %s" % frozen_before)

    # GAME -> RECOVER PARTY (runAsync): the path that used to deadlock the
    # ebiten UI thread forever.
    click_item(shell, 0, 0)
    click_item(shell, 1, 0)
    click_item(shell, 1, 1)
    click_item(shell, 2, 1)

    # Liveness: the shell must react to a key press. Comparing two frames
    # is not enough: a modal dialog is a static screen, so a healthy shell
    # can produce two identical frames.
    time.sleep(1.5)
    before = shell_frame(shell)
    sh("xdotool windowactivate --sync %s" % shell)
    time.sleep(0.6)
    sh("xdotool key --clearmodifiers Escape")
    time.sleep(2.0)
    after = shell_frame(shell)
    check("shell reacts to input after the clicks (not deadlocked)",
          before is not None and after is not None and before != after,
          "the screen did not change after Escape")

    print("\n%d failed" % len(fails))
    for f_ in fails:
        print("  -", f_)
    return 1 if fails else 0


if __name__ == "__main__":
    sys.exit(main())
