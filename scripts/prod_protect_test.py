#!/usr/bin/env python3
"""prod_protect_test.py - check the protection of a production build.

A production build must not give away its content:

  * no plain/ folder next to the engine: the runtime (bridge, boot page)
    lives inside the signed and encrypted runtime package;
  * the runtime package holds the integrity manifest of its own files;
  * the engine binary does not contain the source of the bridge;
  * the engine binary is obfuscated: no readable strings of the engine;
  * the guard (anti debug) is active at runtime.

Usage: scripts/prod_protect_test.py <output dir> [--run]
"""
import os
import re
import subprocess
import sys
import time

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
DISP = os.environ.get("FLW_DISPLAY", ":99")

fails = []


def check(what, ok, detail=""):
    print(("PASS " if ok else "FAIL ") + what + ("  -- " + str(detail) if detail else ""))
    if not ok:
        fails.append(what)
    return ok


def main():
    out = sys.argv[1] if len(sys.argv) > 1 else os.path.join(ROOT, "output/linux-prod")
    run = "--run" in sys.argv
    out = os.path.abspath(out)

    check("the output directory exists", os.path.isdir(out), out)

    # 1. no plain folder
    plain = os.path.join(out, "plain")
    check("there is no plain/ folder in the production build",
          not os.path.exists(plain), plain)

    # 2. the runtime package holds the manifest
    pkg_dir = os.path.join(out, "flwdata", "packages")
    runtime = os.path.join(pkg_dir, "runtime.nkpkgenc")
    if not os.path.exists(runtime):
        runtime = os.path.join(pkg_dir, "runtime.nkpkg")
    check("the runtime package exists", os.path.exists(runtime), runtime)

    if os.path.exists(runtime):
        # The package is encrypted, so the manifest can only be found with
        # the key of the build.
        key = os.path.join(ROOT, "keys", "pkgkey.b64")
        names = []
        inspect = os.path.join(ROOT, "bin", "pkginspect")
        if os.path.exists(inspect) and os.path.exists(key):
            outp = subprocess.run(
                [inspect, "-list", "-key", key, runtime],
                capture_output=True, text=True)
            for line in outp.stdout.splitlines():
                line = line.strip()
                if line.startswith("browser/") or line.startswith("node/") \
                        or line.startswith("nkmanifest.json"):
                    names.append(line.split()[0])
        if names:
            check("the manifest is inside the runtime package",
                  "nkmanifest.json" in names, names)
            check("the bridge is inside the runtime package",
                  "browser/flwbridge.js" in names, names)
        else:
            # Fall back: the package must at least not be readable as text.
            raw = open(runtime, "rb").read()
            check("the runtime package is not plain text",
                  b"flwNativeCall" not in raw and b"nw.Window" not in raw,
                  "%d bytes" % len(raw))

    # 3. the binary does not contain the bridge
    engine = os.path.join(out, "flwengine")
    if not os.path.exists(engine):
        engine = os.path.join(out, "flwengine.exe")
    has_binary = os.path.exists(engine)
    is_apk = any(f.endswith(".apk") for f in os.listdir(out))
    if has_binary:
        pass
    elif not is_apk:
        check("the engine binary exists", False, engine)
    if has_binary:
        raw = open(engine, "rb").read()
        bridge = open(os.path.join(ROOT, "flwengine", "internal", "bridge",
                                   "web", "flwbridge.js"), "rb").read()
        # A part of the bridge that is unique enough.
        needle = b"window.flwNative.call"
        check("the bridge source is not in the binary", needle not in raw,
              "%d bytes of bridge" % len(bridge))

        # 4. the strings of the engine are obfuscated
        strs = subprocess.run(["strings", "-n", "8", engine],
                              capture_output=True, text=True).stdout
        leaked = [s for s in ("flower engine supervisor", "cannot start the game",
                              "the rosemary field", "flwNativeCall")
                  if s in strs]
        check("the strings of the engine are obfuscated", not leaked, leaked)

    # 5. the guard is active at runtime
    if run:
        log = os.path.join(out, "flwdata", "logs", "supervisor.nklog")
        env = dict(os.environ, DISPLAY=DISP, GDK_BACKEND="x11")
        proc = subprocess.Popen([engine], cwd=out, env=env,
                                stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        time.sleep(12)
        proc.terminate()
        try:
            proc.wait(timeout=10)
        except Exception:
            proc.kill()
        text = ""
        if os.path.exists(log):
            key = os.path.join(ROOT, "keys", "pkgkey.b64")
            text = subprocess.run(
                ["python3", os.path.join(ROOT, "scripts", "nklogdec.py"), log,
                 "--key", key], capture_output=True, text=True).stdout
        check("the guard is active", "guard: active" in text, text[-300:])
        check("the runtime integrity is verified",
              "runtime integrity verified" in text, text[-300:])

    # 6. the android apk: no plain assets, an obfuscated library
    apks = [f for f in os.listdir(out) if f.endswith(".apk")]
    for apk in apks:
        path = os.path.join(out, apk)
        tmp = os.path.join("/tmp/opencode", "apkcheck")
        subprocess.run(["rm", "-rf", tmp], check=False)
        os.makedirs(tmp, exist_ok=True)
        subprocess.run(["unzip", "-q", "-o", path, "-d", tmp], check=False)
        assets = os.path.join(tmp, "assets")
        if os.path.isdir(assets):
            names = os.listdir(assets)
            check("the apk has no plain assets", "plain" not in names, names)
            check("the apk has the runtime package",
                  any(n.startswith("runtime.nkpkg") for n in names), names)
        libs = []
        libdir = os.path.join(tmp, "lib")
        if os.path.isdir(libdir):
            for abi in os.listdir(libdir):
                p = os.path.join(libdir, abi, "libgojni.so")
                if os.path.exists(p):
                    libs.append(p)
        if libs:
            leaked = []
            for p in libs:
                strs = subprocess.run(["strings", "-n", "8", p],
                                      capture_output=True, text=True).stdout
                for s in ("flower engine supervisor", "cannot start the game",
                          "flwNativeCall"):
                    if s in strs:
                        leaked.append((os.path.basename(os.path.dirname(p)), s))
            check("the strings of the engine library are obfuscated",
                  not leaked, leaked)
        subprocess.run(["rm", "-rf", tmp], check=False)

    print()
    if fails:
        print("%d failed:" % len(fails))
        for f in fails:
            print("  - " + f)
        return 1
    print("all production protection checks passed")
    return 0


if __name__ == "__main__":
    sys.exit(main())
