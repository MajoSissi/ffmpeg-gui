"""Generate the FFmpeg GUI app icon: the letter F on a dark squircle, plus favicon and .ico.

The glyph is a geometric letter F built from a few polygons, so the raster icon and the
`brandSvg()` path in frontend/dist/icons.js are the same shape. The middle arm ends in a
play triangle whose height equals the arm thickness: the arm tapers to a point but never
thins below the arm weight, so the F stays unambiguous at 16px while still reading as
"play" at large sizes.

`f_polys()` is the single geometric source of truth: it feeds the Pillow raster *and* the
SVG path, so the installed icon and the in-app brand mark cannot drift apart.

Three dark-background candidates live in VARIANTS. Each adds a different media hint on top
of the same F: film perforations, a fast-forward double triangle, or an offset "HUD"
composition with a status chip.

Everything is flat colour. A vertical gradient on a dark squircle is the Windows 11 stock
look, which is exactly what a custom icon should not look like -- and it cannot be mirrored
by the in-app mark, which has to sit on the rail's own background. `brand_svg()` therefore
reproduces the whole icon (tile, rim, F, and the media hint) as an SVG, so the rail mark and
the installed icon are the same picture rather than merely the same letter.

Proportions are measured from a bold system sans (Segoe UI Bold, Arial Bold) and fall back
to sane constants when no such font is installed. Pure Pillow, no external assets.

Run:
    python tools/genicon.py                  # write the three icon artefacts
    python tools/genicon.py --preview        # + .tmp-logo/preview.png
    python tools/genicon.py --variant cinema # try a different candidate
    python tools/genicon.py --all            # render every candidate, write nothing else
"""
import io
import os
import sys

from PIL import Image, ImageDraw, ImageFilter, ImageFont

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
SS = 8  # supersample factor

ICO_SIZES = [16, 24, 32, 48, 64, 128, 256]

# --- F construction (shared by every variant) ---
PAD = 0.18  # glyph inset, fraction of the tile edge
MID_Y = 0.435  # middle arm top, fraction of cap height
MID_LEN = 0.90  # middle arm length, fraction of glyph width
TRI_W = 0.34  # play triangle length, fraction of glyph width
TILE_RADIUS = 0.235  # corner radius, fraction of tile edge

# Glyph fill: one flat colour. A gradient reads as a rendering artefact at 16px in the
# tray and, worse, the in-app mark cannot be a single flat fill on the rail's own
# background — so the two would stop looking like the same letter.
GLYPH = (238, 243, 252)  # #eef3fc

# Tile fill: also flat. A vertical gradient on a squircle is the Windows 11 default
# look, which is exactly what a custom icon should not be.
TILE = (26, 29, 36)  # #1a1d24

FONT_CANDIDATES = [
    r"C:\Windows\Fonts\segoeuib.ttf",
    r"C:\Windows\Fonts\arialbd.ttf",
    r"C:\Windows\Fonts\verdanab.ttf",
]

# Proportions of a bold sans F, used when no candidate font can be measured.
FALLBACK = (0.60, 0.235, 0.205)  # width/H, stem/H, arm/H


# --------------------------------------------------------------------------- variants

def _perforations(size):
    """Variant `cinema`: a minimal film-strip perforation column down the left edge.

    Returns (rect, alpha) pairs. Tile-only: it lives outside the F, so the letter itself
    stays untouched at every size. Below 32px the holes collapse into a dirty line, so
    they are dropped there.
    """
    w = size * 0.050
    h = size * 0.082
    gap = size * 0.050
    x = size * 0.112
    total = 4 * h + 3 * gap
    y = (size - total) / 2
    out = []
    for i in range(4):
        top = y + i * (h + gap)
        out.append(((x, top, x + w, top + h), 150))
    return out


def _status_chip(size):
    """Variant `hud`: a status LED plus a small progress track in the freed top-right.

    A dim track with a brighter filled portion, so the accent reads as transport state
    rather than as random specks. Tile-only, dropped below 32px.
    """
    dot_r = size * 0.040
    cx, cy = size * 0.700, size * 0.208
    out = [((cx - dot_r, cy - dot_r, cx + dot_r, cy + dot_r), 255)]
    tw, th = size * 0.150, size * 0.040
    tx, ty = cx + dot_r * 2.1, cy - th / 2
    out.append(((tx, ty, tx + tw, ty + th), 70))  # track
    out.append(((tx, ty, tx + tw * 0.45, ty + th), 235))  # filled
    return out


VARIANTS = {
    "cinema": {
        "label": "cinema",
        "blurb": "film-perforation column, F shifted right",
        "bg": TILE,
        "hint": "tri",
        "mid_len": MID_LEN,
        "off": (0.045, 0.0),  # F nudged right to make room for the holes
        "accent": (150, 180, 232),
        "accent_alpha": 165,
        "decor": _perforations,
        "decor_min": 32,
    },
    "fastforward": {
        "label": "fast-forward",
        "blurb": "mid-arm ends in a double play triangle, F centred",
        "bg": TILE,
        "hint": "double",
        "mid_len": 0.99,  # arm runs nearly full width; the pair of tips sits at its end
        "tri_w": 0.155,  # each triangle
        "tri_gap": 0.045,  # slot between them, so the pair reads as two
        "off": (0.0, 0.0),
        "accent": None,
        "decor": None,
        "decor_min": 0,
    },
    "hud": {
        "label": "hud",
        "blurb": "F offset down-left, status dot + bar top-right",
        "bg": TILE,
        "hint": "tri",
        "mid_len": MID_LEN,
        "off": (-0.035, 0.035),  # F down-left, opening the top-right corner
        "accent": (168, 199, 250),  # MD3 --primary
        "accent_alpha": 235,
        "decor": _status_chip,
        "decor_min": 32,
    },
}

DEFAULT_VARIANT = "cinema"

def measure_bold_sans():
    """Read F proportions off a bold system sans so the letterform isn't invented.

    Returns (width/H, stem/H, arm thickness/H), or the fallback constants when nothing
    usable is installed. Clamped so an unexpected font cannot distort the glyph.
    """
    for path in FONT_CANDIDATES:
        if not os.path.exists(path):
            continue
        try:
            n = 256
            img = Image.new("L", (n * 3, n * 3), 0)
            ImageDraw.Draw(img).text((n, n), "F", font=ImageFont.truetype(path, n), fill=255)
            bbox = img.getbbox()
            if not bbox:
                continue
            img = img.crop(bbox)
            w, h = img.size
            px = img.load()

            def row(y):
                xs = [x for x in range(w) if px[x, y] > 127]
                return (min(xs), max(xs)) if xs else None

            # Stem: the horizontal run on a row below the middle arm.
            foot = row(int(h * 0.95))
            if not foot:
                continue
            stem = (foot[1] - foot[0] + 1) / h

            # Arm thickness: the top arm ends where the row extent first falls back to
            # roughly stem-only width. Using the top row's extent here would measure the
            # arm's length instead of its thickness.
            limit = (foot[1] - foot[0] + 1) * 1.6
            arm = next((y for y in range(1, h) if (row(y) or (0, 0))[1] < limit), 0)
            if arm <= 0:
                continue
            return (
                min(0.72, max(0.52, w / h)),
                min(0.28, max(0.17, stem)),
                min(0.24, max(0.15, arm / h)),
            )
        except (OSError, ValueError):
            continue
    return FALLBACK


# Single source of truth for the letterform, shared by the raster glyph and brandSvg().
W_RATIO, STEM_RATIO, ARM_RATIO = measure_bold_sans()


def f_polys(size, mid_len=MID_LEN, hint="tri", tri_w=TRI_W, tri_gap=0.0, off=(0.0, 0.0)):
    """The F as a list of polygons in a `size` square.

    Three overlapping quads (stem, top arm, middle arm) plus the play-triangle tip, all
    wound the same way so a nonzero fill rule unions them. `hint="double"` splits the tip
    into two triangles with a `tri_gap` slot between them, for a fast-forward read.
    """
    h = size * (1 - 2 * PAD)
    w = h * W_RATIO
    x0 = (size - w) / 2 + off[0] * size
    y0 = size * PAD + off[1] * size
    sw, at = h * STEM_RATIO, h * ARM_RATIO
    my = y0 + h * MID_Y
    mid_y, mid_y2 = my, my + at
    mid_x = x0 + w * mid_len
    mid_c = my + at / 2

    polys = [
        [(x0, y0), (x0 + sw, y0), (x0 + sw, y0 + h), (x0, y0 + h)],  # stem
        [(x0, y0), (x0 + w, y0), (x0 + w, y0 + at), (x0, y0 + at)],  # top arm
    ]
    if hint == "double":
        tw = w * tri_w
        gap = w * tri_gap
        bx = mid_x - (2 * tw + gap)
        polys.append([(x0, mid_y), (bx, mid_y), (bx, mid_y2), (x0, mid_y2)])
        polys.append([(bx, mid_y), (bx + tw, mid_c), (bx, mid_y2)])
        polys.append([(bx + tw + gap, mid_y), (bx + 2 * tw + gap, mid_c), (bx + tw + gap, mid_y2)])
    else:
        bx = mid_x - w * tri_w
        polys.append([(x0, mid_y), (bx, mid_y), (bx, mid_y2), (x0, mid_y2)])
        polys.append([(bx, mid_y), (mid_x, mid_c), (bx, mid_y2)])
    return polys


def f_mask(size, **kw):
    """Antialiased letter F mask: drawn supersampled, then downscaled."""
    s = size * SS
    m = Image.new("L", (s, s), 0)
    d = ImageDraw.Draw(m)
    for poly in f_polys(s, **kw):
        d.polygon(poly, fill=255)
    return m.resize((size, size), Image.LANCZOS)


def brand_path(variant=None):
    """The F as a single SVG path on a 24x24 grid.

    Derived from the same f_polys() geometry as the raster glyph. Kept for callers that
    want the bare letter; the app's rail mark uses `brand_svg()` instead, which carries the
    whole icon.
    """
    v = VARIANTS[variant or DEFAULT_VARIANT]

    def n(val):
        return f"{round(val, 2):g}"

    # The rail mark is drawn at 24px, below every variant's decor_min, so it uses the
    # centred F — the same letter the 16px and 24px icon frames use.
    polys = f_polys(24.0, **variant_fkw(v, 24))
    return "".join(
        " ".join(("M" if i == 0 else "L") + f"{n(x)} {n(y)}" for i, (x, y) in enumerate(p)) + "Z"
        for p in polys
    )


def brand_svg(variant=None):
    """The complete icon as a standalone SVG string, for brandSvg() in icons.js.

    The rail mark used to be the bare F while the installed icon was a tile with a rim and
    a perforation column — the same letter, but two obviously different pictures, which is
    what "the app icon and the in-app icon were not updated together" looks like. This
    renders the tile, the rim, the F and the hint on one 24x24 grid so the two match.

    The rim and the top wash are the parts that cannot survive as flat SVG. The rim becomes
    a 1px inset stroke, which is what a hairline is anyway; the wash is dropped, since a
    soft blur in a 24px mark is invisible at best and a smudge at worst.
    """
    v = VARIANTS[variant or DEFAULT_VARIANT]
    S = 24.0
    # The offset is applied unconditionally here, unlike in the raster path: this mark
    # always draws the decor, so it always needs the room. variant_offset() would hand
    # back a centred F for a 24px render and the two would no longer be one picture.
    kw = dict(
        mid_len=v["mid_len"],
        hint=v["hint"],
        tri_w=v.get("tri_w", TRI_W),
        tri_gap=v.get("tri_gap", 0.0),
        off=v["off"],
    )

    def n(val):
        return f"{round(val, 2):g}"

    polys = f_polys(S, **kw)
    glyph = "".join(
        " ".join(("M" if i == 0 else "L") + f"{n(x)} {n(y)}" for i, (x, y) in enumerate(p)) + "Z"
        for p in polys
    )

    r = n(S * TILE_RADIUS)
    parts = [
        # tile: the squircle the raster version fills, at full opacity
        f'<rect class="brand-tile" x="0" y="0" width="24" height="24" rx="{r}"/>',
        # rim: a hairline inset, matching the raster edge highlight
        f'<rect class="brand-rim" x="0.5" y="0.5" width="23" height="23" rx="{r}" fill="none"/>',
    ]

    # The media hint is dropped below 32px in the raster, but on a 24px grid with a
    # vector outline it costs nothing, and having it in both keeps them recognisably one
    # icon rather than two drawings of the same letter.
    if v["decor"]:
        for rect, _alpha in v["decor"](S):
            x0, y0, x1, y1 = rect
            parts.append(
                f'<rect class="brand-hint" x="{n(x0)}" y="{n(y0)}" '
                f'width="{n(x1 - x0)}" height="{n(y1 - y0)}" rx="{n(min(x1 - x0, y1 - y0) * 0.45)}"/>'
            )

    parts.append(f'<path class="brand-f" d="{glyph}"/>')
    return (
        '<svg viewBox="0 0 24 24" aria-hidden="true">'
        + "".join(parts)
        + "</svg>"
    )


# --------------------------------------------------------------------------- tile

def flat(size, rgb):
    """A single flat colour filling the whole square."""
    return Image.new("RGB", (size, size), rgb)


def _squircle_mask(s, inset=0.0):
    m = Image.new("L", (s, s), 0)
    ImageDraw.Draw(m).rounded_rectangle(
        (inset, inset, s - 1 - inset, s - 1 - inset),
        radius=int((s - 2 * inset) * TILE_RADIUS),
        fill=255,
    )
    return m


def variant_offset(v, px):
    """The F's optical offset, which only applies once the decor is actually drawn.

    The offset exists to open room for the tile decoration. Below `decor_min` the
    decoration is dropped, so keeping the offset would leave the F off-centre in an empty
    tile. Returns a centred F instead, which also keeps brandSvg() and the 24px frame
    describing the same letter.
    """
    if v["decor"] and px >= v["decor_min"]:
        return v["off"]
    return (0.0, 0.0)


def variant_fkw(v, px):
    """f_polys() keyword arguments for a variant at a given pixel size."""
    return dict(
        mid_len=v["mid_len"],
        hint=v["hint"],
        tri_w=v.get("tri_w", TRI_W),
        tri_gap=v.get("tri_gap", 0.0),
        off=variant_offset(v, px),
    )


def render(size, variant=None):
    """The finished icon at `size` px: flat dark squircle, hairline rim, flat F."""
    v = VARIANTS[variant or DEFAULT_VARIANT]
    s = size * SS

    tile_mask = _squircle_mask(s)
    out = Image.new("RGBA", (s, s), (0, 0, 0, 0))
    out.paste(flat(s, v["bg"]).convert("RGBA"), (0, 0), tile_mask)

    # A single soft top wash is the only depth cue left. It stops the tile reading
    # as a flat sticker at large sizes without turning into a gradient -- the fill
    # under it is one colour.
    top_wash = Image.new("RGBA", (s, s), (0, 0, 0, 0))
    ImageDraw.Draw(top_wash).rounded_rectangle(
        (0, 0, s - 1, int(s * 0.46)), radius=int(s * TILE_RADIUS), fill=(255, 255, 255, 14)
    )
    out = Image.alpha_composite(out, top_wash.filter(ImageFilter.GaussianBlur(s * 0.05)))

    # Hairline rim. This is the load-bearing detail for a dark icon: without it the tile
    # dissolves into a dark taskbar. Kept to a true hairline and clipped to the squircle so
    # it reads as an edge, not a bezel.
    rim = Image.new("RGBA", (s, s), (0, 0, 0, 0))
    rd = ImageDraw.Draw(rim)
    rim_w = max(1, int(round(size * 0.010 * SS / 2)))
    rd.rounded_rectangle(
        (rim_w, rim_w, s - 1 - rim_w, s - 1 - rim_w),
        radius=int(s * TILE_RADIUS - rim_w),
        outline=(255, 255, 255, 30),
        width=rim_w * 2,
    )
    out = Image.alpha_composite(out, rim)
    out.putalpha(Image.composite(out.getchannel("A"), Image.new("L", (s, s), 0), tile_mask))

    # Tile-layer media hint, when this size is big enough to carry it.
    if v["decor"] and size >= v["decor_min"]:
        deco = Image.new("RGBA", (s, s), (0, 0, 0, 0))
        dd = ImageDraw.Draw(deco)
        for rect, alpha in v["decor"](s):
            r = min(rect[2] - rect[0], rect[3] - rect[1]) * 0.45
            dd.rounded_rectangle(rect, radius=r, fill=v["accent"] + (alpha,))
        out = Image.alpha_composite(out, deco)
        out.putalpha(Image.composite(out.getchannel("A"), Image.new("L", (s, s), 0), tile_mask))

    # The F, one flat colour. A very tight dark contact shadow instead of a light
    # halo: a glow muddies the counters and makes 16px look smudged.
    fkw = variant_fkw(v, size)
    glyph = Image.new("RGBA", (s, s), (0, 0, 0, 0))
    glyph.paste(flat(s, GLYPH).convert("RGBA"), (0, 0), f_mask(size, **fkw).resize((s, s), Image.LANCZOS))

    contact = Image.new("RGBA", (s, s), (0, 0, 0, 0))
    ImageDraw.Draw(contact).polygon(
        [p for poly in f_polys(s, **fkw) for p in poly], fill=(0, 0, 0, 105)
    )
    out = Image.alpha_composite(out, contact.filter(ImageFilter.GaussianBlur(s * 0.010)))

    out = Image.alpha_composite(out, glyph)
    out.putalpha(Image.composite(out.getchannel("A"), Image.new("L", (s, s), 0), tile_mask))
    return out.resize((size, size), Image.LANCZOS)


# --------------------------------------------------------------------------- checks

def legibility(variant, n):
    """Structural read of the F at `n` px.

    An F's stem spans the full cap height, so no row is ever blank: the "counters" are
    the notches of background to the right of the stem, between and below the arms. They
    are found by banding rows on how far right the ink reaches — arm rows reach far, the
    counter rows only reach the stem. Those notches are what make an F an F; if they close
    up, the glyph silts into a solid blob.
    """
    m = f_mask(n, **variant_fkw(VARIANTS[variant], n))
    px = m.load()
    rows = [y for y in range(n) if any(px[x, y] > 127 for x in range(n))]
    ext = [max((x for x in range(n) if px[x, y] > 127), default=-1) for y in range(n)]

    stem_end = min(e for e in ext if e >= 0)          # right edge of the bare stem
    full = max(ext)                                    # right edge of the top arm
    thresh = (stem_end + full) / 2

    # Group the "reaches past the stem" rows into bands: two bands = two arms.
    bands, y = [], rows[0]
    while y <= rows[-1]:
        if ext[y] > thresh:
            y0 = y
            while y <= rows[-1] and ext[y] > thresh:
                y += 1
            bands.append((y0, y - 1))
        else:
            y += 1
    counters = [bands[i + 1][0] - bands[i][1] - 1 for i in range(len(bands) - 1)]

    stem = stem_end - min(x for x in range(n) if any(px[x, y] > 127 for y in range(n))) + 1
    return {
        "ink": sum(1 for y in range(n) for x in range(n) if px[x, y] > 127),
        "span": (rows[0], rows[-1]),
        "stem": stem,
        "arms": len(bands),
        "counters": counters,
        "ok": stem >= 2 and len(bands) == 2 and len(counters) == 1 and counters[0] >= 1,
    }


def check_variant(variant):
    """Print the legibility table for one variant and return whether it passes."""
    print(f"  {variant}")
    ok = True
    for n in ICO_SIZES:
        r = legibility(variant, n)
        decor = "  (tile decor dropped at this size)" if (
            VARIANTS[variant]["decor"] and n < VARIANTS[variant]["decor_min"]) else ""
        flag = "ok" if r["ok"] else "FAIL"
        print(f"    {n:>3}px  stem {r['stem']}px  counters {r['counters']}  "
              f"ink {r['ink']:>5}  rows {r['span'][0]}..{r['span'][1]:<3} {flag}{decor}")
        ok = ok and r["ok"]
    return ok


def rim_contrast(n):
    """Luminance gap between the hairline rim and the tile base.

    A dark tile on a dark taskbar has no silhouette without this; a delta under ~6 is
    effectively invisible.
    """
    im = render(n, DEFAULT_VARIANT).convert("RGB")
    px = im.load()
    ring = [px[x, y] for y in range(n) for x in range(n) if x < 2 or y < 2]
    base = px[n // 3, n - 2]
    lum = lambda c: 0.2126 * c[0] + 0.7152 * c[1] + 0.0722 * c[2]
    return max(lum(c) for c in ring) - lum(base)


def read_ico_sizes(path):
    """List the frame sizes actually stored in a .ico, straight from its directory."""
    with open(path, "rb") as fh:
        count = int.from_bytes(fh.read(6)[4:6], "little")
        out = []
        for i in range(count):
            fh.seek(6 + i * 16)
            e = fh.read(16)
            out.append((e[0] or 256, e[1] or 256,
                        int.from_bytes(e[8:12], "little"),
                        int.from_bytes(e[12:16], "little")))
        return out


# --------------------------------------------------------------------------- preview

DARK = (18, 20, 26)       # --surface
DEEP = (11, 12, 15)       # a darker shell, to test the silhouette
LIGHT = (243, 243, 246)   # explorer / light taskbar
LABEL = (170, 174, 184)
HEAD = (214, 216, 226)


def _on(bg, icon):
    """Paste `icon` onto a background that is exactly its own size."""
    cell = Image.new("RGB", icon.size, bg)
    cell.paste(icon, (0, 0), icon)
    return cell


def _screenshot_brand_svg(tmp_dir, variant):
    """Screenshot the real brandSvg() from icons.js with headless Chrome.

    Reads the shipped template literal rather than reimplementing it, so the sheet reports
    on the code that actually ships. Returns None when no browser is available.
    """
    import re
    import subprocess

    chrome = next((p for p in (
        r"C:/Program Files/Google/Chrome/Application/chrome.exe",
        r"C:/Program Files (x86)/Google/Chrome/Application/chrome.exe",
        r"C:/Program Files (x86)/Microsoft/Edge/Application/msedge.exe",
    ) if os.path.exists(p)), None)
    if not chrome:
        return None

    src = open(os.path.join(ROOT, "frontend", "dist", "icons.js"), encoding="utf-8").read()
    found = re.search(r"export function brandSvg\(\)\s*\{.*?`(<svg.*?)`", src, re.S)
    if not found:
        return None
    svg = found.group(1)
    q = chr(34)

    def mark(n, colour):
        sized = svg.replace("viewBox", f"width={q}{n}{q} height={q}{n}{q} viewBox")
        return (f'<div class="cell"><div class="bare" style="width:{n}px;height:{n}px;'
                f'color:{colour}">{sized}</div>{n}px</div>')

    page = os.path.join(tmp_dir, "_brand_svg.html")
    open(page, "w", encoding="utf-8").write(
        "<!DOCTYPE html><meta charset='utf-8'><style>"
        "body{margin:0;background:#12141a;font:12px/1.5 system-ui,sans-serif;color:#8e929c}"
        "h2{font:600 12px system-ui;color:#bdc1cb;margin:12px 0 2px 18px}"
        ".row{display:flex;align-items:flex-end;gap:22px;padding:6px 18px}"
        ".light{background:#f3f3f6}"
        ".tile{width:40px;height:40px;border-radius:11px;display:grid;place-items:center;"
        "background:#12141a;box-shadow:inset 0 0 0 1px rgba(255,255,255,.10)}"
        ".tile svg{width:24px;height:24px;fill:#e3e4ea}"
        ".cell{text-align:center}.bare{display:grid;place-items:center}"
        "</style>"
        "<h2>brandSvg() from frontend/dist/icons.js, in the 40px .rail__brand chip</h2>"
        f'<div class="row">{"".join(f"""<div class="tile">{svg}</div>""" for _ in range(3))}</div>'
        "<h2>bare mark at true size &mdash; dark #12141a, then light #f3f3f6</h2>"
        f'<div class="row">{"".join(mark(n, "#e3e4ea") for n in (24, 24, 32))}</div>'
        f'<div class="row light">{"".join(mark(n, "#1c1e26") for n in (24, 32, 48))}</div>'
    )

    out = os.path.join(tmp_dir, "_brand_svg.png")
    if os.path.exists(out):
        os.remove(out)
    try:
        subprocess.run(
            [chrome, "--headless=new", "--disable-gpu", "--no-sandbox", "--hide-scrollbars",
             f"--user-data-dir={os.path.join(tmp_dir, '_chrome_profile')}",
             f"--screenshot={out}", "--window-size=600,300",
             "--default-background-color=12141a",
             "file:///" + page.replace("\\", "/")],
            check=True, capture_output=True, timeout=90,
        )
    except (subprocess.SubprocessError, OSError):
        return None
    return Image.open(out).convert("RGB") if os.path.exists(out) else None


def build_preview(out_path):
    """Comparison sheet: the three candidates up front, then the chosen one's full audit.

    Laid out one size per row so each row can size its own columns; a fixed grid overflows
    as soon as the zoom factors differ.
    """
    sizes = [16, 24, 32, 48, 64, 128, 256]
    zoom = {16: 10, 24: 8, 32: 6, 48: 4, 64: 3, 128: 2, 256: 1}
    names = list(VARIANTS)
    chosen = DEFAULT_VARIANT
    pad, gap = 18, 16
    lab_w = 74

    row_h = {n: n * zoom[n] + pad * 2 for n in sizes}
    row_w = {n: lab_w + len(names) * ((n * zoom[n] + 8) * 2 + gap) for n in sizes}
    W = max(row_w.values()) + pad
    H = 300 + sum(row_h.values()) + 720
    sheet = Image.new("RGB", (W, H), (11, 12, 15))
    d = ImageDraw.Draw(sheet)

    def text(xy, s, fill=LABEL):
        d.text(xy, s, fill=fill)

    def hexs(c):
        return f"#{c[0]:02x}{c[1]:02x}{c[2]:02x}"

    # ---- 1. candidates, one big render each ----
    text((pad, 12), "CANDIDATES \u2014 flat squircle, flat F, three media hints", HEAD)
    text((pad, 30), f"F proportions measured from {os.path.basename(FONT_CANDIDATES[0])}: "
                    f"width/H {W_RATIO:.3f}  stem/H {STEM_RATIO:.3f}  arm/H {ARM_RATIO:.3f}")
    for j, nm in enumerate(names):
        v = VARIANTS[nm]
        x = pad + 100 + j * 240
        ic = render(128, nm)
        sheet.paste(ic, (x, 48), ic)
        text((x, 184), nm, HEAD if nm == chosen else LABEL)
        text((x, 200), v["blurb"], LABEL)
        text((x, 214), f'tile {hexs(v["bg"])}  ·  F {hexs(GLYPH)}  (both flat)', LABEL)
    text((pad, 240), f"shipping: {chosen}", HEAD)
    text((pad, 256), "each row: left on #12141a, right on a deeper #0b0c11 shell", LABEL)

    # ---- 2. candidate size matrix ----
    y = 286
    text((pad, y), "all three candidates at tray and desktop sizes", HEAD)
    y += 20
    for n in sizes:
        z = n * zoom[n]
        text((pad, y + z // 2 - 10), f"{n}px", HEAD)
        text((pad, y + z // 2 + 4), f"x{zoom[n]}", LABEL)
        for j, nm in enumerate(names):
            x = pad + lab_w + j * ((z + 8) * 2 + gap)
            ic = render(n, nm).resize((z, z), Image.NEAREST)
            sheet.paste(_on(DARK, ic), (x, y))
            if n <= 64:
                sheet.paste(_on(DEEP, ic), (x + z + 8, y))
        y += row_h[n]

    # ---- 3. chosen variant: full audit ----
    y += 8
    v = VARIANTS[chosen]
    text((pad, y), f"CHOSEN: {chosen} \u2014 {v['blurb']}", HEAD)
    y += 20
    big = render(256, chosen)
    sheet.paste(big, (pad, y), big)
    text((pad, y + 262), "master render (1024 on build)", LABEL)

    ax = pad + 276
    text((ax, y), "build/windows/icon.ico frames", HEAD)
    yy = y + 18
    ico = os.path.join(ROOT, "build", "windows", "icon.ico")
    if os.path.exists(ico):
        for w, h, nbytes, off in read_ico_sizes(ico):
            text((ax, yy), f"{w:>3}x{h:<3} {nbytes:>6} bytes  @0x{off:<6X}", LABEL)
            yy += 16
    yy += 10
    text((ax, yy), "F legibility: stem width + counters", HEAD)
    yy += 18
    for n in ICO_SIZES:
        r = legibility(chosen, n)
        note = "  (decor dropped here)" if (
            v["decor"] and n < v["decor_min"]) else ""
        text((ax, yy), f"{n:>3}px  stem {r['stem']:>2}px  counter {str(r['counters']):>6}  "
                       f"ink {r['ink']:>5}  {'ok' if r['ok'] else 'FAIL'}{note}", LABEL)
        yy += 16
    yy += 10
    text((ax, yy), "rim luminance delta vs tile base (dark-taskbar silhouette)", HEAD)
    yy += 18
    for n in (16, 24, 32, 48, 64):
        text((ax, yy), f"{n:>3}px  delta {rim_contrast(n):.1f}", LABEL)
        yy += 16

    # ---- 4. brand mark ----
    y = max(y + 300, yy + 24)
    shot = _screenshot_brand_svg(os.path.join(ROOT, ".tmp-logo"), chosen)
    if shot:
        text((pad, y), "brandSvg() rendered by headless Chrome from frontend/dist/icons.js",
             HEAD)
        y += 18
        sheet.paste(shot, (pad, y))
        y += shot.height + 14
    text((pad, y), "the film perforations are deliberately NOT in brandSvg(): it is a "
                   "single-colour path with no background, so it carries the F alone",
         LABEL)

    os.makedirs(os.path.dirname(out_path), exist_ok=True)
    sheet.save(out_path)
    print("wrote", out_path, sheet.size)


# --------------------------------------------------------------------------- main

def write_ico(path, sizes, variant):
    """Write a multi-size .ico, rendering every frame at its own size.

    Pillow's ICO save only resizes one master per frame, which would carry the 32px-and-up
    tile decor down into the 16px tray frame as a smudge, and `append_images` is ignored for
    ICO. So the container is assembled by hand: a directory entry plus one PNG stream per
    size. Rendering each frame at its true size keeps `decor_min` honest, so small frames
    get the plain F.
    """
    import struct

    streams = []
    for n in sizes:
        buf = io.BytesIO()
        render(n, variant).save(buf, format="PNG")
        streams.append((n, buf.getvalue()))

    offset = 6 + 16 * len(streams)
    entries, blobs = b"", b""
    for n, data in streams:
        entries += struct.pack(
            "<BBBBHHII",
            0 if n >= 256 else n, 0 if n >= 256 else n, 0, 0,
            1, 32, len(data), offset,
        )
        blobs += data
        offset += len(data)

    with open(path, "wb") as fh:
        fh.write(struct.pack("<HHH", 0, 1, len(streams)) + entries + blobs)


def main():
    variant = DEFAULT_VARIANT
    if "--variant" in sys.argv:
        variant = sys.argv[sys.argv.index("--variant") + 1]
        if variant not in VARIANTS:
            sys.exit(f"unknown variant {variant!r}; pick one of {list(VARIANTS)}")

    if "--all" in sys.argv:
        for name in VARIANTS:
            print(f"\n{name}: {VARIANTS[name]['blurb']}")
            check_variant(name)
        return

    render(1024, variant).save(os.path.join(ROOT, "build", "appicon.png"))
    ico_path = os.path.join(ROOT, "build", "windows", "icon.ico")
    os.makedirs(os.path.dirname(ico_path), exist_ok=True)
    write_ico(ico_path, ICO_SIZES, variant)
    render(64, variant).save(os.path.join(ROOT, "frontend", "dist", "favicon.png"))
    print(f"variant: {variant}")
    print("wrote build/appicon.png, build/windows/icon.ico, frontend/dist/favicon.png")

    check_variant(variant)
    print("ico frames:", [(w, h) for w, h, _, _ in read_ico_sizes(ico_path)])
    print("brandSvg() markup (must match frontend/dist/icons.js):\n ", brand_svg(variant))

    if "--preview" in sys.argv:
        build_preview(os.path.join(ROOT, ".tmp-logo", "preview.png"))


if __name__ == "__main__":
    main()
