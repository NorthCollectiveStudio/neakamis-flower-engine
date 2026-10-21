
import json
import struct
import sys

MAGIC = b"NKLG"
VERSION = 1
NONCE = 12
TAG = 16
LEVELS = {0: "trace", 1: "debug", 2: "info", 3: "warn", 4: "error", 5: "critical"}


def load_log_key(pkg_key_path=None):
    if pkg_key_path:
        import base64
        raw = open(pkg_key_path).read().strip()
        k = base64.b64decode(raw)
        if len(k) != 32:
            raise ValueError("bad key file")
        return hmac_sha256(k, b"nklog")
    shard_a = bytes([
        0x6e, 0x3b, 0xa7, 0xf2, 0x0c, 0x98, 0x4d, 0x11, 0x77, 0xd5, 0xe0, 0x2a, 0x9f, 0x66, 0x1c, 0x44,
        0xd8, 0x13, 0x51, 0xfe, 0x83, 0x2c, 0x60, 0xb9, 0x17, 0x0e, 0xa4, 0x5d, 0xc6, 0x79, 0x22, 0x8b])
    shard_b = bytes([
        0x2f, 0x91, 0x46, 0x08, 0xd3, 0x7a, 0xec, 0x59, 0x10, 0x66, 0x2d, 0x97, 0xc4, 0x3b, 0xf0, 0x85,
        0x01, 0x9e, 0x64, 0x33, 0x5a, 0xd7, 0x88, 0x0f, 0x76, 0x24, 0xba, 0x4e, 0x31, 0xc0, 0x9d, 0xe5])
    shard_c = bytes([
        0x84, 0x12, 0xf9, 0x66, 0x2d, 0x50, 0x87, 0x0b, 0xc9, 0x21, 0x7e, 0x63, 0x14, 0xa8, 0x3c, 0xf7,
        0x5b, 0x90, 0x02, 0x7d, 0xe1, 0x48, 0x93, 0x36, 0xaf, 0x6c, 0x19, 0xda, 0x70, 0x25, 0xbe, 0x58])
    master = bytes(a ^ b ^ c for a, b, c in zip(shard_a, shard_b, shard_c))
    return hmac_sha256(master, b"flw.nklog.v1")


def hmac_sha256(key, msg):
    import hashlib
    return hmac_new(key, msg, hashlib.sha256)


def hmac_new(key, msg, digestmod):
    import hashlib
    block = 64
    if len(key) > block:
        key = hashlib.new("sha256", key).digest()
    key = key + b"\x00" * (block - len(key))
    o = bytes(k ^ 0x5c for k in key)
    i = bytes(k ^ 0x36 for k in key)
    inner = hashlib.new("sha256", i + msg).digest()
    return hashlib.new("sha256", o + inner).digest()


def derive(key, salt, info):
    prk = hmac_sha256(key, salt)
    return hmac_sha256(prk, info)


def rotl(n, c):
    c %= 32
    return ((n << c) & 0xffffffff) | (n >> (32 - c))


def chacha20_block(key, counter, nonce):
    consts = [0x61707865, 0x3320646e, 0x79622d32, 0x6b206574]
    k = list(struct.unpack("<8I", key))
    n = list(struct.unpack("<3I", nonce))
    st = consts + k + [counter & 0xffffffff] + n
    work = list(st)
    def qr(a, b, c, d):
        work[a] = (work[a] + work[b]) & 0xffffffff
        work[d] = rotl(work[d] ^ work[a], 16)
        work[c] = (work[c] + work[d]) & 0xffffffff
        work[b] = rotl(work[b] ^ work[c], 12)
        work[a] = (work[a] + work[b]) & 0xffffffff
        work[d] = rotl(work[d] ^ work[a], 8)
        work[c] = (work[c] + work[d]) & 0xffffffff
        work[b] = rotl(work[b] ^ work[c], 7)
    for _ in range(10):
        qr(0, 4, 8, 12)
        qr(1, 5, 9, 13)
        qr(2, 6, 10, 14)
        qr(3, 7, 11, 15)
        qr(0, 5, 10, 15)
        qr(1, 6, 11, 12)
        qr(2, 7, 8, 13)
        qr(3, 4, 9, 14)
    out = [(w + s) & 0xffffffff for w, s in zip(work, st)]
    return struct.pack("<16I", *out)


def poly1305_mac(msg, key):
    p = (1 << 130) - 5
    r = int.from_bytes(key[:16], "little")
    r &= 0x0ffffffc0ffffffc0ffffffc0fffffff
    s = int.from_bytes(key[16:], "little")
    acc = 0
    for i in range(0, len(msg), 16):
        blk = msg[i:i + 16]
        n = int.from_bytes(blk + b"\x01", "little")
        acc = (acc + n) * r % p
    acc = (acc + s) & ((1 << 128) - 1)
    return acc.to_bytes(16, "little")


def chacha20poly1305_decrypt(key, nonce, ct, aad):
    poly_key = chacha20_block(key, 0, nonce)[:32]
    stream = b""
    counter = 1
    while len(stream) < len(ct):
        stream += chacha20_block(key, counter, nonce)
        counter += 1
    tag = ct[-16:]
    body = ct[:-16]
    stream = stream[:len(body)]
    pt = bytes(a ^ b for a, b in zip(body, stream))
    mac_data = aad + pad16(aad) + body + pad16(body) + struct.pack("<QQ", len(aad), len(body))
    expect = poly1305_mac(mac_data, poly_key)
    if expect != tag:
        return None
    return pt


def pad16(b):
    if len(b) % 16 == 0:
        return b""
    return b"\x00" * (16 - len(b) % 16)


def uvarint(data, pos):
    x = 0
    s = 0
    for i in range(10):
        b = data[pos + i]
        if b < 0x80:
            return x | (b << s), pos + i + 1
        x |= (b & 0x7f) << s
        s += 7
    raise ValueError("uvarint overflow")


def parse_record(buf):
    p = 0
    ts, p = uvarint(buf, p)
    level = buf[p]
    p += 1
    cat_len = buf[p]
    p += 1
    cat = buf[p:p + cat_len].decode("utf-8", "replace")
    p += cat_len
    mlen, p = uvarint(buf, p)
    msg = buf[p:p + mlen].decode("utf-8", "replace")
    p += mlen
    return ts, level, cat, msg, p


def decode_file(path, pkg_key_path=None):
    data = open(path, "rb").read()
    if len(data) < 22 or data[:4] != MAGIC:
        raise ValueError("not an nklog file")
    ver = data[4]
    if ver != VERSION:
        raise ValueError(f"unsupported version {ver}")
    log_key = load_log_key(pkg_key_path)
    out = []
    pos = 0
    # A log file may hold several concatenated segments (one header per run):
    # supervisor.nklog is opened O_APPEND and every run appends its own
    # header. Walking a single header desynchronises on the second run
    # ("unknown record kind 78" == the 'N' of the next "NKLG" magic).
    while pos < len(data):
        if pos + 22 > len(data):
            break
        if data[pos:pos + 4] != MAGIC:
            raise ValueError(f"bad magic at {pos}")
        run_id = data[pos + 6:pos + 22]
        key = derive(log_key, run_id, b"nklog.file")
        pos += 22
        while pos < len(data):
            if data[pos:pos + 4] == MAGIC:
                break  # next segment
            kind = data[pos]
            pos += 1
            if kind == 0xff:
                ts, level, cat, msg, consumed = parse_record(data[pos:])
                out.append((ts, level, cat, msg))
                pos += consumed
                continue
            if kind != 0x01:
                raise ValueError(f"unknown record kind {kind}")
            seq = struct.unpack_from("<Q", data, pos)[0]
            pos += 8
            ct_len = struct.unpack_from("<I", data, pos)[0]
            pos += 4
            nonce = data[pos:pos + NONCE]
            pos += NONCE
            ct = data[pos:pos + ct_len]
            pos += ct_len
            aad = run_id + struct.pack("<Q", seq)
            pt = chacha20poly1305_decrypt(key, nonce, ct, aad)
            if pt is None:
                out.append((0, 4, "decoder", "record authentication failed (seq=%d)" % seq))
                continue
            ts, level, cat, msg, _ = parse_record(pt)
            out.append((ts, level, cat, msg))
    return out


def main():
    if len(sys.argv) < 2:
        print("usage: nklogdec.py <file.nklog> [--json] [--key pkgkey.b64]")
        sys.exit(1)
    key_path = None
    if "--key" in sys.argv:
        key_path = sys.argv[sys.argv.index("--key") + 1]
    recs = decode_file(sys.argv[1], key_path)
    as_json = "--json" in sys.argv
    import datetime
    if as_json:
        print(json.dumps([
            {"ts": r[0], "level": LEVELS.get(r[1], str(r[1])), "cat": r[2], "msg": r[3]}
            for r in recs], indent=1))
    else:
        for ts, level, cat, msg in recs:
            t = datetime.datetime.fromtimestamp(ts / 1e9).strftime("%H:%M:%S.%f")[:-3]
            print(f"{t} [{LEVELS.get(level, level)}] {cat}: {msg}")


if __name__ == "__main__":
    main()
