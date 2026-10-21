#!/usr/bin/env python3
"""sound_test.py - the sounds of the engine must be audible.

On linux the audio of the engine goes through PulseAudio (PipeWire provides
the server on the modern desktops). The ALSA device "default" is often a
null device there: the context opens without an error and the samples are
silently thrown away - the engine has no sound at all while the game
(WebAudio) has one.

The test starts the engine and checks that

  * a backend was chosen (the log names it),
  * the engine appears in the mixer of the sound server (if there is one).

Usage: scripts/sound_test.py [path-to-engine-stdout.log]
"""
import os
import re
import shutil
import subprocess
import sys
import time

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
DISP = os.environ.get("FLW_DISPLAY", ":99")

fails = []


def check(name, ok, detail=""):
    print(("PASS " if ok else "FAIL ") + name + ("  -- " + str(detail) if detail else ""))
    if not ok:
        fails.append(name)
    return ok


def sh(cmd):
    env = dict(os.environ, DISPLAY=DISP)
    return subprocess.run(cmd, shell=True, env=env,
                          capture_output=True, text=True).stdout.strip()


def mixer_has_engine():
    """True when the engine has a stream in the sound server."""
    if not shutil.which("pactl"):
        return None
    out = sh("pactl list sink-inputs 2>/dev/null")
    if not out:
        return None
    return "flower engine" in out


def main():
    logpath = sys.argv[1] if len(sys.argv) > 1 else ""
    subprocess.run(["pkill", "-x", "flwengine"], capture_output=True)
    subprocess.run(["pkill", "-x", "flwgamehost"], capture_output=True)
    time.sleep(1)

    outdir = os.path.join(ROOT, "output/linux-dev")
    env = dict(os.environ, DISPLAY=DISP, GDK_BACKEND="x11",
               WEBKIT_DISABLE_DMABUF_RENDERER="1", GSK_RENDERER="cairo")
    log = open("/tmp/opencode/sound.log", "w")
    eng = subprocess.Popen([os.path.join(outdir, "flwengine")], cwd=outdir,
                            env=env, stdout=log, stderr=log)
    time.sleep(8)

    nklog = os.path.join(outdir, "flwdata/logs/supervisor.nklog")
    text = ""
    if os.path.exists(nklog):
        text = subprocess.run(
            ["python3", os.path.join(ROOT, "scripts", "nklogdec.py"), nklog],
            capture_output=True, text=True).stdout
    m = re.search(r"sounds of the engine: (\S+)", text)
    check("the engine has an audio backend", m is not None,
          text[-300:] if m is None else m.group(1))
    if m:
        check("the backend is not the silent one", m.group(1) != "none", m.group(1))

    in_mixer = mixer_has_engine()
    if in_mixer is None:
        print("SKIP the mixer check: no sound server on this machine")
    else:
        check("the engine is in the mixer of the sound server", in_mixer,
              "the sounds would be discarded")

    eng.terminate()
    try:
        eng.wait(timeout=10)
    except Exception:
        eng.kill()
    subprocess.run(["pkill", "-x", "flwgamehost"], capture_output=True)

    print()
    if fails:
        print("%d failed:" % len(fails))
        for f in fails:
            print("  - " + f)
        return 1
    print("all sound checks passed")
    return 0


if __name__ == "__main__":
    sys.exit(main())
