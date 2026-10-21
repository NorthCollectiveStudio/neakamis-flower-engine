#!/usr/bin/env python3
"""Memory test for the dashboard caches.

The dashboard caches the wallpaper, the panel, the toolbar and the composed
frame as GPU images. Whenever the cache key changed (the volume, the
workspace, the search text, the battery level, the clock) a new image was
allocated and the old one was dropped WITHOUT being released. On Windows the
D3D11 textures of the process are part of its footprint in the task
manager, so the process grew to more than a gigabyte.

This test triggers many cache rebuilds (every volume change rebuilds the
panel and the frame) and measures the resident memory of the engine.

Usage: scripts/cache_leak_test.py [rebuilds]
"""
import os
import re
import subprocess
import sys
import time

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
DISP = os.environ.get("FLW_DISPLAY", ":99")


def sh(cmd):
    env = dict(os.environ, DISPLAY=DISP)
    return subprocess.run(cmd, shell=True, env=env,
                          capture_output=True, text=True).stdout.strip()


def engine_rss_mb():
    total = 0
    for pid in sh("pgrep -f 'output/linux-dev/flwengine'").split():
        try:
            with open("/proc/%s/status" % pid) as f:
                m = re.search(r"VmRSS:\s+(\d+) kB", f.read())
            if m:
                total += int(m.group(1))
        except OSError:
            pass
    return total / 1024.0


def main():
    rounds = int(sys.argv[1]) if len(sys.argv) > 1 else 40
    subprocess.run(["pkill", "-x", "flwengine"], capture_output=True)
    subprocess.run(["pkill", "-x", "flwgamehost"], capture_output=True)
    time.sleep(1)
    env = dict(os.environ, DISPLAY=DISP, GDK_BACKEND="x11",
               WEBKIT_DISABLE_DMABUF_RENDERER="1", GSK_RENDERER="cairo")
    log = open("/tmp/opencode/cacheleak.log", "w")
    eng = subprocess.Popen([os.path.join(ROOT, "output/linux-dev/flwengine")],
                            cwd=os.path.join(ROOT, "output/linux-dev"),
                            env=env, stdout=log, stderr=log)
    time.sleep(6)
    shell = sh("xdotool search --name NuggetOS | tail -1")
    if not shell:
        print("no shell window")
        eng.terminate()
        return 1
    geo = sh("xdotool getwindowgeometry --shell %s" % shell)
    m = re.search(r"Position:\s*(-?\d+),(-?\d+)", geo)
    ox, oy = (int(m.group(1)), int(m.group(2))) if m else (0, 0)

    before = engine_rss_mb()
    print("engine RSS before the rebuilds: %.1f MB" % before)
    # The volume icon of the toolbar changes the volume -> the panel and the
    # frame cache are rebuilt.
    for i in range(rounds):
        # The volume slider of the dashboard toolbar.
        x = ox + 1280 - 234 + 20
        y = oy + 17
        sh("xdotool mousemove --sync %d %d" % (x, y))
        time.sleep(0.05)
        sh("xdotool mousedown 1; sleep 0.05; xdotool mouseup 1")
        time.sleep(0.05)
        # And the settings, which change the volume through the panel.
        sh("xdotool key --clearmodifiers Escape")
        time.sleep(0.05)
    time.sleep(3)
    after = engine_rss_mb()
    print("engine RSS after %d rebuilds: %.1f MB (growth %.1f MB)" %
          (rounds, after, after - before))
    eng.terminate()
    subprocess.run(["pkill", "-x", "flwgamehost"], capture_output=True)
    if after - before > 60:
        print("FAIL: the cache rebuilds leak memory")
        return 1
    print("PASS: the cache rebuilds do not leak")
    return 0


if __name__ == "__main__":
    sys.exit(main())
