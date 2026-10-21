
import hashlib
import json
import os
import subprocess
import sys
import time
import urllib.request
import urllib.error

sys.stdout.reconfigure(line_buffering=True)

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
CFG = os.path.join(ROOT, "buildconfig.json")
GAME = "therosemaryfield"
ENGINE_IP = os.environ.get("FLW_HOTRELOAD_IP", "127.0.0.1")
ENGINE_PORT = int(os.environ.get("FLW_HOTRELOAD_PORT", "7777"))
POLL_SEC = float(os.environ.get("FLW_HOTRELOAD_POLL", "1.0"))

IGNORE_DIRS = {".git", "node_modules", "save", "logs", "output", "keys", "bin", "flwengine", ".obfcache"}
IGNORE_FILES = {"crash_log.txt", "FOR DATA MINERS.txt"}


def file_hash(path):
    h = hashlib.sha256()
    with open(path, "rb") as f:
        while True:
            b = f.read(65536)
            if not b:
                break
            h.update(b)
    return h.hexdigest()


def scan_state():
    state = {}
    base = os.path.join(ROOT, GAME)
    for dirpath, dirnames, filenames in os.walk(base):
        dirnames[:] = [d for d in dirnames if d not in IGNORE_DIRS]
        for fn in filenames:
            if fn in IGNORE_FILES or fn.endswith((".log", ".rdcat")) or fn.endswith("~"):
                continue
            p = os.path.join(dirpath, fn)
            try:
                rel = os.path.relpath(p, base)
                state[rel] = file_hash(p) + ":" + str(os.path.getmtime(p))
            except OSError:
                pass
    return state


def build_package(tmp_path):
    flwpack = os.path.join(ROOT, "bin", "flwpack")
    game_dir = os.path.join(ROOT, GAME)
    r = subprocess.run([flwpack, "-dir", game_dir, "-out", tmp_path, "-id", "game",
                        "-kind", "game", "-title", "The Rosemary Field",
                        "-version", "hotreload", "-buildtype", "dev"],
                       capture_output=True, text=True)
    if r.returncode != 0:
        print("flwpack failed:", r.stderr.strip())
        return False
    return True


def push_package(tmp_path):
    url = f"http://{ENGINE_IP}:{ENGINE_PORT}/push"
    data = open(tmp_path, "rb").read()
    req = urllib.request.Request(url, data=data, method="POST")
    try:
        with urllib.request.urlopen(req, timeout=120) as resp:
            body = resp.read().decode()
            return True, body
    except urllib.error.HTTPError as e:
        return False, e.read().decode()
    except Exception as e:
        return False, str(e)


def main():
    if len(sys.argv) > 1:
        global ENGINE_IP, ENGINE_PORT
        parts = sys.argv[1].split(":")
        ENGINE_IP = parts[0]
        if len(parts) > 1:
            ENGINE_PORT = int(parts[1])
    print(f"hotreload scanner: watching {GAME}/ -> {ENGINE_IP}:{ENGINE_PORT}")
    print("press ctrl+c to stop")
    last = scan_state()
    print(f"baseline: {len(last)} files")
    tmp_path = os.path.join("/tmp", f"hotreload-{int(time.time())}.nkpkg")
    quiet_until = 0
    while True:
        time.sleep(POLL_SEC)
        current = scan_state()
        if current == last:
            continue
        changed = [k for k in current if k not in last or current[k] != last.get(k)]
        removed = [k for k in last if k not in current]
        now = time.time()
        if now < quiet_until:
            last = current
            continue
        if changed or removed:
            print(f"change detected: {len(changed)} changed, {len(removed)} removed")
            for c in changed[:5]:
                print(f"  + {c}")
            if build_package(tmp_path):
                ok, body = push_package(tmp_path)
                if ok:
                    print(f"push ok: {body.strip()}")
                    quiet_until = now + 2.0
                else:
                    print(f"push failed: {body}")
            last = scan_state()


if __name__ == "__main__":
    main()
