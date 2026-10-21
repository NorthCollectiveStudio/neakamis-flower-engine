#!/usr/bin/env bash
# Bisect harness: install a candidate flwbridge.js, rebuild the gamehost,
# boot the game and report whether the database loaded.
#
# Usage: scripts/try_bridge.sh <bridge-file> [display]
set -u
BR="$1"
DISP="${2:-:99}"
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
TARGET="$ROOT/output/linux-dev"

cp "$BR" "$ROOT/flwengine/internal/bridge/web/flwbridge.js"
cd "$ROOT/flwengine" || exit 1
go build -o "$TARGET/flwgamehost" ./cmd/flwgamehost || { echo "BUILD FAILED"; exit 1; }

pkill -x flwengine 2>/dev/null
pkill -x flwgamehost 2>/dev/null
sleep 1
cd "$TARGET" || exit 1
rm -f /tmp/opencode/bridge-test.log
DISPLAY="$DISP" GDK_BACKEND=x11 WEBKIT_DISABLE_DMABUF_RENDERER=1 GSK_RENDERER=cairo \
    ./flwengine > /tmp/opencode/bridge-test.log 2>&1 &
for i in $(seq 1 60); do
    DISPLAY="$DISP" xdotool search --name "NuggetOS" >/dev/null 2>&1 && break
    sleep 0.5
done
sleep 2
DISPLAY="$DISP" xdotool windowactivate --sync $(DISPLAY="$DISP" xdotool search --name "NuggetOS" | tail -1) 2>/dev/null
sleep 0.4
DISPLAY="$DISP" xdotool key --clearmodifiers Enter
sleep 12

LOG=/tmp/opencode/bridge-test.log
LINE=$(grep FLWGAME "$LOG" | tail -1)
if [ -z "$LINE" ]; then
    echo "RESULT: game did not start"
    pkill -x flwengine 2>/dev/null; pkill -x flwgamehost 2>/dev/null
    exit 1
fi
RPCPORT=$(echo "$LINE" | sed -n 's/.*rpc=\([0-9]*\).*/\1/p')
TOKEN=$(echo "$LINE" | sed -n 's/.*token=\([^ ]*\).*/\1/p')
python3 - "$RPCPORT" "$TOKEN" <<'PY'
import json, socket, sys
port, token = int(sys.argv[1]), sys.argv[2]
s = socket.create_connection(("127.0.0.1", port), 10)
s.settimeout(10)
f = s.makefile("r", encoding="utf-8")
def call(cmd, js=""):
    msg = {"cmd": cmd, "js": js}
    if cmd == "auth":
        msg = {"cmd": "auth", "name": token}
    s.sendall((json.dumps(msg) + "\n").encode())
    return json.loads(f.readline())
call("auth", token)
for label, js in [("dbLoaded", "DataManager.isDatabaseLoaded()"),
                  ("scene", "String(SceneManager._scene && SceneManager._scene.constructor.name)"),
                  ("frameCount", "String(Graphics.frameCount)"),
                  ("ticker", "String(Graphics._app.ticker.started)")]:
    r = call("eval", js)
    print("  %-11s %s" % (label, r.get("result", r.get("error"))))
PY
pkill -x flwengine 2>/dev/null
pkill -x flwgamehost 2>/dev/null
