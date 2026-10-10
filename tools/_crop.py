"""Crop a rectangle out of a PNG and write a new PNG, with no dependencies.

`pip install pillow` is not an option for this repo (no Python deps, and the
managed interpreter must stay empty), and the screenshots `tools/drive.py`
writes are downscaled by Chrome -- the toolbar label is 12.5px tall in a
1080-wide capture, which is not enough to judge whether it lines up with the
control next to it. Cropping is the difference between "I looked at it" and
"I looked at it".

Only the subset Chrome actually writes: 8-bit truecolour, non-interlaced.

    python tools/_crop.py in.png out.png X Y W H
"""
import struct
import sys
import zlib


def chunks(blob):
    """(type, payload) for every chunk in a PNG, in file order."""
    at = 8                      # skip the 8-byte signature
    while at < len(blob):
        (length,) = struct.unpack(">I", blob[at:at + 4])
        kind = blob[at + 4:at + 8]
        yield kind, blob[at + 8:at + 8 + length]
        at += 12 + length


def paeth(a, b, c):
    p = a + b - c
    pa, pb, pc = abs(p - a), abs(p - b), abs(p - c)
    if pa <= pb and pa <= pc:
        return a
    return b if pb <= pc else c


def main(argv):
    src, dst, x, y, w, h = argv[1], argv[2], *map(int, argv[3:7])
    blob = open(src, "rb").read()

    head = b""
    idat = []
    for kind, payload in chunks(blob):
        if kind == b"IHDR":
            ihdr = payload
        elif kind == b"IDAT":
            idat.append(payload)
        elif kind == b"IEND":
            break
    width, height, depth, color, comp, filt, interlace = struct.unpack(">IIBBBBB", ihdr)
    if (depth, color, comp, filt, interlace) != (8, 2, 0, 0, 0):
        sys.exit(f"unsupported PNG: depth={depth} color={color} interlace={interlace}")

    stride = width * 3
    raw = zlib.decompress(b"".join(idat))

    # One filter byte per scanline; undo them all up front so cropping does not
    # have to care which line it starts from.
    lines = []
    prev = bytearray(stride)
    at = 0
    for _ in range(height):
        ft = raw[at]
        cur = bytearray(raw[at + 1:at + 1 + stride])
        at += 1 + stride
        for i in range(stride):
            left = cur[i - 3] if i >= 3 else 0
            up = prev[i]
            ul = prev[i - 3] if i >= 3 else 0
            if ft == 1:
                cur[i] = (cur[i] + left) & 0xFF
            elif ft == 2:
                cur[i] = (cur[i] + up) & 0xFF
            elif ft == 3:
                cur[i] = (cur[i] + (left + up) // 2) & 0xFF
            elif ft == 4:
                cur[i] = (cur[i] + paeth(left, up, ul)) & 0xFF
            elif ft != 0:
                sys.exit(f"unknown filter {ft}")
        lines.append(cur)
        prev = cur

    w = max(1, min(w, width - x))
    h = max(1, min(h, height - y))
    out = bytearray()
    for row in lines[y:y + h]:
        out.append(0)
        out += row[x * 3:(x + w) * 3]

    def chunk(kind, payload):
        return (struct.pack(">I", len(payload)) + kind + payload
                + struct.pack(">I", zlib.crc32(kind + payload) & 0xFFFFFFFF))

    with open(dst, "wb") as fh:
        fh.write(b"\x89PNG\r\n\x1a\n")
        fh.write(chunk(b"IHDR", struct.pack(">IIBBBBB", w, h, 8, 2, 0, 0, 0)))
        fh.write(chunk(b"IDAT", zlib.compress(bytes(out), 9)))
        fh.write(chunk(b"IEND", b""))


if __name__ == "__main__":
    main(sys.argv)