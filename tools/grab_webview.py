"""PrintWindow() a WebView2 child HWND to check whether content actually renders.

The Wails main window is a plain HWND wrapping a WebView2 composition surface,
so PrintWindow on the top-level window yields the background only. The child
Chrome_WidgetWin_1 HWND does respond to PrintWindow with real pixels.
"""
import ctypes
import sys
from ctypes import wintypes

from PIL import Image

user32 = ctypes.windll.user32
gdi32 = ctypes.windll.gdi32
user32.SetProcessDPIAware()

title = sys.argv[1] if len(sys.argv) > 1 else "FFmpeg GUI"
out = sys.argv[2] if len(sys.argv) > 2 else "child.png"

parent = user32.FindWindowW(None, title)
if not parent:
    sys.exit("window not found: " + title)

children = []


def enum_proc(hwnd, _):
    buf = ctypes.create_unicode_buffer(256)
    user32.GetClassNameW(hwnd, buf, 256)
    rect = wintypes.RECT()
    user32.GetWindowRect(hwnd, ctypes.byref(rect))
    children.append((hwnd, buf.value, rect.right - rect.left, rect.bottom - rect.top))
    return True


WNDENUMPROC = ctypes.WINFUNCTYPE(ctypes.c_bool, wintypes.HWND, wintypes.LPARAM)
user32.EnumChildWindows(parent, WNDENUMPROC(enum_proc), 0)
for h, cls, w, hh in children:
    print(f"child hwnd={h} class={cls} size={w}x{hh}")

chrome = [(h, w, hh) for h, cls, w, hh in children if "Chrome" in cls and w > 200 and hh > 200]
if not chrome:
    sys.exit("no sized Chrome child window found (nothing rendered?)")

hwnd, w, h = chrome[0]
hdc = user32.GetWindowDC(hwnd)
memdc = gdi32.CreateCompatibleDC(hdc)
bmp = gdi32.CreateCompatibleBitmap(hdc, w, h)
gdi32.SelectObject(memdc, bmp)
user32.PrintWindow(hwnd, memdc, 2)


class BMIH(ctypes.Structure):
    _fields_ = [
        ("biSize", wintypes.DWORD), ("biWidth", wintypes.LONG), ("biHeight", wintypes.LONG),
        ("biPlanes", wintypes.WORD), ("biBitCount", wintypes.WORD), ("biCompression", wintypes.DWORD),
        ("biSizeImage", wintypes.DWORD), ("biXPelsPerMeter", wintypes.LONG),
        ("biYPelsPerMeter", wintypes.LONG), ("biClrUsed", wintypes.DWORD),
        ("biClrImportant", wintypes.DWORD),
    ]


bi = BMIH()
bi.biSize = ctypes.sizeof(BMIH)
bi.biWidth, bi.biHeight = w, -h
bi.biPlanes, bi.biBitCount, bi.biCompression = 1, 32, 0

buf = ctypes.create_string_buffer(w * h * 4)
gdi32.GetDIBits(memdc, bmp, 0, h, buf, ctypes.byref(bi), 0)
img = Image.frombuffer("RGBA", (w, h), buf, "raw", "BGRA", 0, 1).convert("RGB")
img.save(out)
colors = img.getcolors(maxcolors=1 << 24) or []
print("distinct colors:", len(colors))
print("saved", out)
