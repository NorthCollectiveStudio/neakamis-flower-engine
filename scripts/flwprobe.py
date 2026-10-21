#!/usr/bin/env python3
"""flwprobe - drive the running flower-engine game through its RPC channel.

The engine prints a line on stdout when a game starts:

    FLWGAME port=<http> rpc=<rpc> token=<token> pid=<pid>

This tool parses that line (from a log file or stdin), then talks to the
gamehost RPC server so tests can assert on real game state instead of
guessing from screenshots.

Usage:
    flwprobe.py eval "<javascript>"            # evaluate JS, print the result
    flwprobe.py script <file>                  # evaluate each line of a file
    flwprobe.py wait "js" <timeout>            # poll until JS returns non-empty
"""
import json
import os
import re
import socket
import sys
import time

LINE_RE = re.compile(
    r"FLWGAME port=(\d+) rpc=(\d+) token=(\S+) pid=(\d+)")


def find_handle(paths):
    """Return (port, rpc, token) from the first FLWGAME line found."""
    for path in paths:
        if not os.path.exists(path):
            continue
        try:
            with open(path, "r", errors="replace") as f:
                data = f.read()
        except OSError:
            continue
        m = LINE_RE.search(data)
        if m:
            return int(m.group(1)), int(m.group(2)), m.group(3)
    return None


class RPC:
    def __init__(self, addr, token, timeout=15.0):
        host, _, port = addr.rpartition(":")
        self.sock = socket.create_connection((host, int(port)), timeout)
        self.sock.settimeout(timeout)
        self.token = token
        self.enc = self.sock.makefile("w", encoding="utf-8")
        self.dec = self.sock.makefile("r", encoding="utf-8")
        self._send({"cmd": "auth", "name": token})
        resp = json.loads(self.dec.readline())
        if not resp.get("ok"):
            raise RuntimeError("auth failed: %r" % resp)

    def _send(self, obj):
        self.enc.write(json.dumps(obj) + "\n")
        self.enc.flush()

    def call(self, cmd, js=""):
        self._send({"cmd": cmd, "js": js})
        line = self.dec.readline()
        if not line:
            raise RuntimeError("rpc connection closed")
        return json.loads(line)

    def eval(self, js):
        r = self.call("eval", js)
        if not r.get("ok"):
            raise RuntimeError(r.get("error", "eval failed"))
        return r.get("result", "")

    def close(self):
        try:
            self.sock.close()
        except OSError:
            pass


def default_paths():
    root = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
    out = []
    for target in ("linux-dev", "linux-test"):
        out.append(os.path.join(root, "output", target, "engine-stdout.log"))
    out.append("/tmp/opencode/e2e/engine-stdout.log")
    return out


def connect(paths):
    handle = find_handle(paths)
    if not handle:
        print("flwprobe: no FLWGAME handle found (game not running?)",
              file=sys.stderr)
        sys.exit(2)
    _port, rpc_port, token = handle
    return RPC("127.0.0.1:%d" % rpc_port, token)


def main():
    if len(sys.argv) < 2:
        print(__doc__)
        sys.exit(1)
    cmd = sys.argv[1]
    # An explicit log path may be given as the last argument.
    paths = default_paths()
    for a in sys.argv[2:]:
        if a.endswith(".log"):
            paths = [a]
            break
    rpc = connect(paths)
    try:
        if cmd == "eval":
            print(rpc.eval(sys.argv[2]))
        elif cmd == "script":
            with open(sys.argv[2]) as f:
                for line in f:
                    line = line.strip()
                    if not line or line.startswith("#"):
                        continue
                    print("%s => %s" % (line, rpc.eval(line)))
        elif cmd == "wait":
            js = sys.argv[2]
            timeout = float(sys.argv[3]) if len(sys.argv) > 3 else 30.0
            deadline = time.time() + timeout
            last = ""
            while time.time() < deadline:
                try:
                    last = rpc.eval(js)
                except Exception as e:  # transient during a scene switch
                    last = "ERR:%s" % e
                if last:
                    print(last)
                    return
                time.sleep(0.5)
            print("TIMEOUT (last=%r)" % last, file=sys.stderr)
            sys.exit(1)
        else:
            print("unknown command %r" % cmd, file=sys.stderr)
            sys.exit(1)
    finally:
        rpc.close()


if __name__ == "__main__":
    main()
