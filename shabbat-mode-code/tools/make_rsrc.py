#!/usr/bin/env python3
"""Writes rsrc_windows_amd64.syso: the program icon and version details.
Usage: make_rsrc.py source.png out.syso"""
import io, struct, sys
from PIL import Image

src = Image.open(sys.argv[1]).convert("RGBA")
sizes = [256, 64, 48, 32, 24, 16]
pngs = []
for s in sizes:
    b = io.BytesIO(); src.resize((s, s), Image.LANCZOS).save(b, "PNG", optimize=True); pngs.append(b.getvalue())
group = struct.pack("<HHH", 0, 1, len(pngs))
for i, (s, p) in enumerate(zip(sizes, pngs)):
    group += struct.pack("<BBBBHHIH", s % 256, s % 256, 0, 0, 1, 32, len(p), i + 1)

def u(s): return s.encode("utf-16le") + b"\0\0"
def pad(b): return b + b"\0" * (-len(b) % 4)
def block(key, value=b"", vtype=1, vlen=None, kids=b""):
    head = struct.pack("<HHH", 0, len(value) // 2 if vtype == 1 and vlen is None else (vlen if vlen is not None else len(value)), vtype)
    body = pad(head + u(key)) + pad(value) + kids
    return struct.pack("<H", len(body)) + body[2:]

V = (1, 1, 0, 0)
strings = [("CompanyName", "יהודי אחד ומיוחד"), ("FileDescription", "מצב שבת"), ("FileVersion", "1.1.0.0"),
           ("InternalName", "ShabbatMode"), ("LegalCopyright", "© 2026 יהודי אחד ומיוחד. MIT License"),
           ("OriginalFilename", "ShabbatMode.exe"), ("ProductName", "מצב שבת"), ("ProductVersion", "1.1.0.0")]
table = b"".join(pad(block(k, u(v))) for k, v in strings)
sfi = block("StringFileInfo", vlen=0, kids=pad(block("040D04B0", vlen=0, kids=table)))
vfi = block("VarFileInfo", vlen=0, kids=block("Translation", struct.pack("<HH", 0x040D, 0x04B0), vtype=0))
fixed = struct.pack("<13I", 0xFEEF04BD, 0x00010000, V[0] << 16 | V[1], V[2] << 16 | V[3], V[0] << 16 | V[1], V[2] << 16 | V[3],
                    0x3F, 0, 0x40004, 1, 0, 0, 0)
version = block("VS_VERSION_INFO", fixed, vtype=0, kids=pad(sfi) + vfi)

LANG = 0x040D
res = {3: [(i + 1, p) for i, p in enumerate(pngs)], 14: [(1, group)], 16: [(1, version)]}

# Layout: root dir, type dirs, name dirs (language), data entries, then raw data.
types = sorted(res)
def dirsize(n): return 16 + 8 * n
root = dirsize(len(types))
offs = {}; o = root
for t in types: offs[t] = o; o += dirsize(len(res[t]))
lang = {}
for t in types:
    for i, _ in res[t]: lang[t, i] = o; o += dirsize(1)
entry = {}
for t in types:
    for i, _ in res[t]: entry[t, i] = o; o += 16
data = {}
for t in types:
    for i, d in res[t]:
        data[t, i] = o; o += len(d) + (-len(d) % 8)
buf = bytearray(o)
def put(at, n, base):
    buf[at:at + 16] = struct.pack("<IIHHHH", 0, 0, 0, 0, 0, n)
put(0, len(types), 0)
for k, t in enumerate(types):
    struct.pack_into("<II", buf, 16 + 8 * k, t, 0x80000000 | offs[t])
    put(offs[t], len(res[t]), 0)
    for m, (i, d) in enumerate(res[t]):
        struct.pack_into("<II", buf, offs[t] + 16 + 8 * m, i, 0x80000000 | lang[t, i])
        put(lang[t, i], 1, 0)
        struct.pack_into("<II", buf, lang[t, i] + 16, LANG, entry[t, i])
        struct.pack_into("<IIII", buf, entry[t, i], data[t, i], len(d), 0, 0)
        buf[data[t, i]:data[t, i] + len(d)] = d
relocs = [entry[t, i] for t in types for i, _ in res[t]]

# COFF object: header, one .rsrc section, data, relocations, symbol, string table.
hdr, sec = 20, 40
raw = hdr + sec
rel = raw + len(buf)
sym = rel + 10 * len(relocs)
coff = struct.pack("<HHIIIHH", 0x8664, 1, 0, sym, 1, 0, 0)
sect = b".rsrc\0\0\0" + struct.pack("<IIIIIIHHI", 0, 0, len(buf), raw, rel, 0, len(relocs), 0, 0x40000040)
out = coff + sect + bytes(buf)
for r in sorted(relocs):
    out += struct.pack("<IIH", r, 0, 3)  # IMAGE_REL_AMD64_ADDR32NB
out += b".rsrc\0\0\0" + struct.pack("<IhHBB", 0, 1, 0, 3, 0)
out += struct.pack("<I", 4)
open(sys.argv[2], "wb").write(out)
