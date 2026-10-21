#!/usr/bin/env python3
"""End-to-end UI test: F10 -> MODEL -> change the model with the mouse.

This is the exact workflow a tester uses: F10 from the game window opens
the dev console, the MODEL list is opened with the mouse and an actor is
picked. It exercises the click path that used to deadlock the shell.

Usage: scripts/ui_model_test.py [engine-stdout.log]
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


def shell_geom(shell):
    geo = sh("xdotool getwindowgeometry --shell %s" % shell)
    # "xdotool getwindowgeometry --shell" prints X=/Y= lines, not "Position:".
    mx = re.search(r"^X=(-?\d+)", geo, re.M)
    my = re.search(r"^Y=(-?\d+)", geo, re.M)
    return (int(mx.group(1)) if mx else 0, int(my.group(1)) if my else 0)


def item_xy(col, row):
    return int(DC_COL_X + col * (DC_COL_W + DC_COL_GAP) + 30), \
           int(DC_COL_Y + DC_HDR_H + row * DC_ITEM_H + 14)


def click(shell, col, row, times=1):
    ox, oy = shell_geom(shell)
    x, y = item_xy(col, row)
    for _ in range(times):
        sh("xdotool mousemove --sync %d %d" % (x + ox, y + oy))
        time.sleep(0.3)
        sh("xdotool mousedown 1; sleep 0.3; xdotool mouseup 1")
        time.sleep(1.0)


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
    s, f = rpc_connect(logpath)
    fails = []

    def check(name, ok, detail=""):
        print("%s %s%s" % ("PASS" if ok else "FAIL", name,
                           ("  -- " + str(detail)) if detail else ""))
        if not ok:
            fails.append(name)

    def ev(js):
        r = rpc(s, f, "eval", js)
        return r.get("result", r.get("error", ""))

    game = sh("xdotool search --name Rosemary | tail -1")
    check("game window found", bool(game), game)
    shell = sh("xdotool search --name NuggetOS | tail -1")
    check("shell window found", bool(shell), shell)
    if not game or not shell:
        return 1

    before = ev("$gameParty.leader() ? ($gameParty.leader()._characterName + ':' + $gameParty.leader()._characterIndex) : 'none'")
    print("leader before: %s" % before)

    # F10 from the game window opens the dev console (game keyhook).
    check("the dev console opens", open_dev_console(game, shell))
    supervisor_log = os.path.join(ROOT, "output", "linux-dev", "flwdata", "logs",
                                  "supervisor.nklog")
    check("the dev console is open",
          "nugget.button f10" in open(supervisor_log, errors="replace").read()[-20000:])

    # The mouse events go to the focused window: focus the shell.
    sh("xdotool windowactivate --sync %s" % shell)
    time.sleep(0.8)

    # Pick an actor whose character index differs from the current one so
    # the check does not depend on the state a previous run left behind.
    row = 1
    for i in range(1, 9):
        idx = ev("String($dataActors[%d] ? $dataActors[%d].characterIndex : -1)" % (i, i))
        if idx.isdigit() and idx != before.split(":")[-1]:
            row = i - 1        # the MODEL list skips the empty actor 0
            break
    print("picking row %d of the model list" % row)

    # DEBUG column: MODEL is the 4th entry. The console is two-stage: the
    # first click on a different item only selects it, the second activates.
    click(shell, 0, 3, times=2)
    time.sleep(1.0)
    # The MODEL submenu opens in the second column.
    click(shell, 1, row)
    time.sleep(2.5)

    after = ev("$gameParty.leader() ? ($gameParty.leader()._characterName + ':' + $gameParty.leader()._characterIndex) : 'none'")
    print("leader after:  %s" % after)
    check("the model was changed through the UI", after != before and after != "none",
          "before=%s after=%s" % (before, after))

    # The shell must still be alive (the click path used to deadlock it).
    time.sleep(1.0)
    a = sh("import -window root /tmp/opencode/_uim1.png 2>/dev/null; echo ok")
    time.sleep(3.0)
    sh("import -window root /tmp/opencode/_uim2.png 2>/dev/null")
    try:
        same = open("/tmp/opencode/_uim1.png", "rb").read() == open("/tmp/opencode/_uim2.png", "rb").read()
    except OSError:
        same = None
    check("the shell is still rendering", same is False, "frames identical: %s" % same)

    print("\n%d failed" % len(fails))
    for f_ in fails:
        print("  -", f_)
    return 1 if fails else 0


if __name__ == "__main__":
    sys.exit(main())
