#!/usr/bin/env python3
"""Memory leak test for the engine.

The tester reported that the process grows on Windows (the game host runs
inside the engine in the production build, so both are visible as
flwengine.exe). This test starts the engine with the game, opens the dev
console - the heaviest user of the RPC channel, it polls the game every two
seconds - and samples the resident memory of both processes.

Usage: scripts/leak_test.py [minutes]
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


def rss_kb(pattern):
    """Resident memory of the matching processes, in kB."""
    total = 0
    found = []
    for pid in sh("pgrep -f '%s'" % pattern).split():
        try:
            with open("/proc/%s/status" % pid) as f:
                m = re.search(r"VmRSS:\s+(\d+) kB", f.read())
            if m:
                total += int(m.group(1))
                found.append(pid)
        except OSError:
            pass
    return total, found


def main():
    minutes = float(sys.argv[1]) if len(sys.argv) > 1 else 3.0
    subprocess.run(["pkill", "-x", "flwengine"], capture_output=True)
    subprocess.run(["pkill", "-x", "flwgamehost"], capture_output=True)
    time.sleep(1)

    env = dict(os.environ, DISPLAY=DISP, GDK_BACKEND="x11",
               WEBKIT_DISABLE_DMABUF_RENDERER="1", GSK_RENDERER="cairo")
    log = open("/tmp/opencode/leak.log", "w")
    eng = subprocess.Popen([os.path.join(ROOT, "output/linux-dev/flwengine")],
                            cwd=os.path.join(ROOT, "output/linux-dev"),
                            env=env, stdout=log, stderr=log)
    time.sleep(4)
    # Launch the game: click the game card on the dashboard.
    sh("xdotool mousemove --sync 254 260; xdotool mousedown 1; sleep 0.25; xdotool mouseup 1")
    time.sleep(12)
    # Open the dev console (F10 from the game window) - it polls the game.
    gw = sh("xdotool search --name Rosemary | tail -1")
    if gw:
        sh("xdotool windowactivate --sync %s" % gw)
        time.sleep(0.5)
        sh("xdotool key --clearmodifiers F10")
    time.sleep(4)

    samples = []
    end = time.time() + minutes * 60
    print("sampling the memory for %g minutes" % minutes)
    while time.time() < end:
        e, ep = rss_kb("output/linux-dev/flwengine")
        g, gp = rss_kb("output/linux-dev/flwgamehost")
        samples.append((time.time(), e, g))
        print("  engine %6.1f MB   gamehost %6.1f MB" % (e / 1024.0, g / 1024.0))
        time.sleep(20)

    eng.terminate()
    subprocess.run(["pkill", "-x", "flwgamehost"], capture_output=True)

    if len(samples) < 3:
        print("not enough samples")
        return 1
    first, last = samples[0], samples[-1]
    d_engine = (last[1] - first[1]) / 1024.0
    d_game = (last[2] - first[2]) / 1024.0
    print("\ngrowth over the run: engine %+.1f MB, gamehost %+.1f MB" % (d_engine, d_game))
    # A leak of more than 40 MB per minute is a leak (the caches are a few
    # megabytes and the poll allocates a few kB per second).
    if d_engine > 40:
        print("FAIL: the engine leaks memory")
        return 1
    if d_game > 40:
        print("FAIL: the game host leaks memory")
        return 1
    print("PASS: no runaway memory growth")
    return 0


if __name__ == "__main__":
    sys.exit(main())
