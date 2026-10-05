"""Grab a window's pixels straight off the desktop (no focus stealing).

WebView2 renders through DirectComposition, so PrintWindow() returns a blank
surface; a plain desktop BitBlt is the only way to see what the user sees.
"""
import ctypes
import sys
import time
from ctypes import wintypes

from PIL import ImageGrab

user32 = ctypes.windll.user32
user32.SetProcessDPIAware()

title = sys.argv[1] if len(sys.argv) > 1 else "FFmpeg GUI"
out = sys.argv[2] if len(sys.argv) > 2 else "screen.png"

hwnd = user32.FindWindowW(None, title)
if not hwnd:
    print("window not found:", title)
    sys.exit(1)

if len(sys.argv) > 3 and sys.argv[3] == "raise":
    SWP_NOMOVE, SWP_NOSIZE, SWP_SHOWWINDOW = 0x0002, 0x0001, 0x0040
    user32.BringWindowToTop(hwnd)
    user32.SetWindowPos(hwnd, 0, 0, 0, 0, 0, SWP_NOMOVE | SWP_NOSIZE | SWP_SHOWWINDOW)
    time.sleep(1.2)

rect = wintypes.RECT()
user32.GetWindowRect(hwnd, ctypes.byref(rect))
box = (rect.left, rect.top, rect.right, rect.bottom)
print("hwnd=%s box=%s" % (hwnd, box))
if rect.right - rect.left <= 0:
    sys.exit("window has no size (already closed?)")

ImageGrab.grab(bbox=box, all_screens=True).save(out)
print("saved", out)
