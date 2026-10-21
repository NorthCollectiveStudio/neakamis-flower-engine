#!/usr/bin/env python3
"""Build bridge variants for bisecting.

Usage: mkvariant.py <out.js> <group> [<group> ...]

Each group is a region of the new bridge that is replaced by the
corresponding region of the original bridge. Groups that are not listed keep
the new implementation.

Group name -> (new_start, new_end, orig_start, orig_end)
"""
import sys

ORIG = "/tmp/opencode/flwbridge.orig.js"
NEW = "/tmp/opencode/flwbridge.new.js"

GROUPS = {
    # rawCall payload parsing
    "rawcall": ("    function rawCall(name, args) {", "    var splashEl = null;",
                 "    function rawCall(name, args) {", "    var splashEl = null;"),
    # Buffer / byte helpers
    "buffer": ("    function flwBytesOf(data) {", "    var osCache = null;",
               "    function base64ToBytes(b64) {", "    var osCache = null;"),
    # fs shim
    "fs": ("    var fsShim = {", "    function flwMakeBuffer(bytes) {",
           "    var fsShim = {", "    function base64ToArrayBuffer(b64) {"),
    # child_process shim
    "child": ("    // node quotes an argv element so the shell sees it as one argument.",
              "    var nativeModules = ",
              "    var childShim = {", "    var nativeModules = {"),
    # process shim
    "process": ("    var processShim = {", "    // moduleCache/nativeModules must not inherit",
                "    var processShim = {", "    var moduleCache = {};"),
    # os shim
    "os": ("    var osShim = {", "    // node's \"url\" module",
           "    var osShim = {", "    function DiscordRpcClient(opts) {"),
    # url module
    "url": ("    // node's \"url\" module", "    function DiscordRpcClient(opts) {",
            "    function DiscordRpcClient(opts) {", "    function DiscordRpcClient(opts) {"),
    # require + nativeModules
    "require": ("    var nativeModules = ", "    window.nw = {",
                "    var nativeModules = {", "    window.nw = {"),
    # nw.Window singleton / moveBy / confirm
    "nwsingle": ("    // nw.js returns the same window singleton",
                 "            open: function (u, o, cb) { if (cb) cb(null); }",
                 "            get: function (cb) {",
                 "            open: function (u, o, cb) { if (cb) cb(null); }"),
}


def find(src, marker, what):
    if marker == "":
        return len(src)
    i = src.find(marker)
    if i < 0:
        print("marker not found (%s): %r" % (what, marker))
        sys.exit(1)
    return i


def main():
    out = sys.argv[1]
    groups = sys.argv[2:]
    orig = open(ORIG).read()
    src = open(NEW).read()
    for g in groups:
        if g not in GROUPS:
            print("unknown group %r" % g)
            sys.exit(1)
        ns, ne, os_, oe = GROUPS[g]
        ni = find(src, ns, g + ".new")
        nj = find(src[ni:], ne, g + ".new")
        oi = find(orig, os_, g + ".orig")
        oj = find(orig[oi:], oe, g + ".orig")
        src = src[:ni] + orig[oi:oi + oj] + src[ni + nj:]
    open(out, "w").write(src)
    print("wrote %s (reverted: %s)" % (out, ",".join(groups) or "none"))


if __name__ == "__main__":
    main()
