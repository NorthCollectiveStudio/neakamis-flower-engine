#!/usr/bin/env python3
"""check_tree.py - verify the engine source tree is intact before building.

Why this exists
---------------
The game itself runs `killall -9 gedit kate mousepad pluma xed leafpad`
(data/Map053.json, the Scarlet event). A SIGKILLed editor that holds unsaved
changes can restore its stale buffer over the source on the next start. That
is exactly what happened twice:

  * 2026-10-18 04:23 - ~40 files lost their `//go:build` tags, every
    `//go:embed` directive and the cgo preamble of native_linux.go. The tree
    stopped compiling; every binary built from it shipped an EMPTY bridge,
    which made every previously fixed bug reappear at once.
  * 2026-10-18 12:45 - the same damage, the second time.

So the build must never silently produce binaries from a damaged tree. This
script checks the invariants that the damage broke and, with --fix, restores
them from the last known-good backup.

Usage:
    scripts/check_tree.py          # verify only, exit 1 on damage
    scripts/check_tree.py --fix    # verify and repair from the backup
"""
import os
import re
import shutil
import subprocess
import sys
import tarfile
import tempfile

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
ENGINE = os.path.join(ROOT, "flwengine")
BACKUP_DIR = os.path.join(ROOT, "backups")

# Known-good tree snapshot. Newer snapshots are preferred.
BACKUP_CANDIDATES = [
    "tree-healthy-20261018-1700-final.tgz",
]

# file -> required build constraint (must be the first //go:build line)
BUILD_TAGS = {
    "internal/guard/guard_windows.go": "windows",
    "internal/guard/guard_other.go": "!windows && !linux",
    "internal/guard/guard_proc.go": "linux",
    "internal/gamehost/window_desktop.go": "!flwnogui && !android",
    "internal/gamehost/window_nogui.go": "flwnogui && !android",
    "internal/gamehost/window_other.go": "android",
    "internal/gamehost/native_linux.go": "linux && !android",
    "internal/gamehost/native_windows.go": "windows",
    "internal/gamehost/native_other.go": "!windows && (!linux || android)",
    "internal/gamehost/gdi_attach_windows.go": "windows",
    "internal/gamehost/gdi_attach_other.go": "!windows",
    "internal/gamehost/hostpaths_windows.go": "windows",
    "internal/gamehost/hostpaths_other.go": "!windows",
    "internal/plat/plat_windows.go": "windows",
    "internal/plat/plat_other.go": "!windows",
    "internal/nuggetos/dashboard.go": "!flwnodash",
    "internal/nuggetos/dashboard_stub.go": "flwnodash",
    # The bridge is embedded only in the development builds. In a production
    # build it lives in the encrypted runtime package (js_slim.go).
    "internal/bridge/js_embed.go": "!flwprodslim",
    "internal/bridge/js_slim.go": "flwprodslim",
}

# file -> list of embed directives that must be present
EMBEDS = {
    "internal/nuggetos/font.go": [
        "//go:embed assets/meego/fonts/DroidSans.ttf",
        "//go:embed assets/meego/fonts/DroidSans-Bold.ttf",
        "//go:embed assets/fonts/DejaVuSansMono.ttf",
    ],
    "internal/nuggetos/meego.go": [
        "//go:embed assets/meego/icons",
        "//go:embed assets/meego/wallpaper.png",
    ],
    "internal/nuggetos/sound.go": ["//go:embed assets/meego/sounds"],
    "internal/nuggetos/cursors.go": ["//go:embed assets/meego/cursors"],
    "internal/nuggetos/logo.go": ["//go:embed logo.svg"],
    "internal/bridge/js_embed.go": ["//go:embed web/flwbridge.js"],
}

# Files that must not be empty (an empty embed means a broken binary).
NON_EMPTY = [
    "internal/bridge/web/flwbridge.js",
    "internal/nuggetos/assets/meego/wallpaper.png",
    "internal/nuggetos/assets/meego/fonts/DroidSans.ttf",
    "internal/nuggetos/assets/fonts/DejaVuSansMono.ttf",
    "internal/nuggetos/logo.svg",
]

# The cgo preamble of native_linux.go must contain the import "C" line.
CGO_PREAMBLE = {
    "internal/gamehost/native_linux.go": ['import "C"'],
}

# Files that must contain exactly one logo.svg in the whole tree.
SINGLETON = ["logo.svg"]


def read(path):
    with open(path, "r", errors="replace") as f:
        return f.read()


def problems():
    """Return a list of human readable problems with the tree."""
    out = []

    for rel, tag in BUILD_TAGS.items():
        p = os.path.join(ENGINE, rel)
        if not os.path.exists(p):
            out.append("%s: MISSING" % rel)
            continue
        src = read(p)
        m = re.search(r"^//go:build\s+(.*)$", src, re.M)
        if not m:
            out.append("%s: missing //go:build %s" % (rel, tag))
        elif m.group(1).strip() != tag:
            out.append("%s: //go:build is %r, expected %r"
                       % (rel, m.group(1).strip(), tag))

    for rel, directives in EMBEDS.items():
        p = os.path.join(ENGINE, rel)
        if not os.path.exists(p):
            out.append("%s: MISSING" % rel)
            continue
        src = read(p)
        for d in directives:
            if d not in src:
                out.append("%s: missing %r" % (rel, d))

    for rel in NON_EMPTY:
        p = os.path.join(ENGINE, rel)
        if not os.path.exists(p):
            out.append("%s: MISSING" % rel)
        elif os.path.getsize(p) == 0:
            out.append("%s: EMPTY (embed would ship nothing)" % rel)

    for rel, needles in CGO_PREAMBLE.items():
        p = os.path.join(ENGINE, rel)
        if not os.path.exists(p):
            out.append("%s: MISSING" % rel)
            continue
        src = read(p)
        for n in needles:
            if n not in src:
                out.append("%s: missing %r (cgo preamble broken)" % (rel, n))
        # A blank line between the preamble comment and `import "C"` breaks cgo.
        if re.search(r"\*/\s*\n\s*\n\s*import \"C\"", src):
            out.append("%s: blank line between cgo preamble and import \"C\""
                       % rel)

    # exactly one logo.svg in the tree
    logos = []
    for dirpath, dirnames, filenames in os.walk(ROOT):
        dirnames[:] = [d for d in dirnames
                       if d not in (".git", "output", ".buildcache", ".obfcache",
                                    "node_modules", "backups")]
        for fn in filenames:
            if fn == "logo.svg":
                logos.append(os.path.relpath(os.path.join(dirpath, fn), ROOT))
    if len(logos) != 1:
        out.append("expected exactly one logo.svg, found %d: %s"
                   % (len(logos), ", ".join(logos)))

    return out


def newest_backup():
    best = None
    for name in BACKUP_CANDIDATES:
        p = os.path.join(BACKUP_DIR, name)
        if os.path.exists(p):
            best = p
    if best:
        return best
    # fall back to any tree-*.tgz in backups/
    cands = [os.path.join(BACKUP_DIR, n) for n in os.listdir(BACKUP_DIR)
             if n.startswith("tree-") and n.endswith(".tgz")]
    if not cands:
        return None
    return max(cands, key=os.path.getmtime)


def repair():
    """Restore the damaged files from the newest known-good backup."""
    backup = newest_backup()
    if not backup:
        print("check_tree: no known-good backup found in %s" % BACKUP_DIR)
        return False
    print("check_tree: repairing from %s" % os.path.basename(backup))
    tmp = tempfile.mkdtemp(prefix="flwrestore-")
    try:
        with tarfile.open(backup) as tf:
            tf.extractall(tmp)
        base = os.path.join(tmp, "flwengine")
        if not os.path.isdir(base):
            base = tmp
        fixed = 0
        for rel in sorted(set(list(BUILD_TAGS) + list(EMBEDS)
                              + list(NON_EMPTY) + list(CGO_PREAMBLE))):
            src = os.path.join(base, rel)
            dst = os.path.join(ENGINE, rel)
            if not os.path.exists(src):
                continue
            cur = read(dst) if os.path.exists(dst) else ""
            good = read(src)
            # Only restore when the current file is actually damaged.
            damaged = False
            if rel in BUILD_TAGS:
                m = re.search(r"^//go:build\s+(.*)$", cur, re.M)
                if not m or m.group(1).strip() != BUILD_TAGS[rel]:
                    damaged = True
            for d in EMBEDS.get(rel, []):
                if d not in cur:
                    damaged = True
            if rel in NON_EMPTY and os.path.getsize(dst) == 0:
                damaged = True
            for n in CGO_PREAMBLE.get(rel, []):
                if n not in cur:
                    damaged = True
            if not damaged:
                continue
            os.makedirs(os.path.dirname(dst), exist_ok=True)
            shutil.copyfile(src, dst)
            print("  restored %s" % rel)
            fixed += 1
        if fixed:
            print("check_tree: %d file(s) restored" % fixed)
        else:
            print("check_tree: nothing to restore from the backup")
        return fixed > 0
    finally:
        shutil.rmtree(tmp, ignore_errors=True)


def main():
    fix = "--fix" in sys.argv
    probs = problems()
    if not probs:
        print("check_tree: OK - source tree intact")
        return 0
    print("check_tree: %d problem(s) found:" % len(probs))
    for p in probs:
        print("  - %s" % p)
    print("")
    print("The source tree was damaged (most likely an editor was SIGKILLed by")
    print("the game and restored a stale buffer). Building now would silently")
    print("produce binaries with an empty bridge and re-introduce every bug.")
    if fix:
        if repair():
            remaining = problems()
            if remaining:
                print("check_tree: still broken:")
                for p in remaining:
                    print("  - %s" % p)
                return 1
            print("check_tree: repaired")
            return 0
        return 1
    print("Run scripts/check_tree.py --fix to repair from the last good backup.")
    return 1


if __name__ == "__main__":
    sys.exit(main())
