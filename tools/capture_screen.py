"""Bring a native window to the front and grab its on-screen pixels.

PrintWindow() cannot capture WebView2 / DirectComposition surfaces, so this uses
a plain desktop BitBlt (Pillow ImageGrab) after focusing the window.
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

user32.ShowWindow(hwnd, 9)  # SW_RESTORE
user32.SetForegroundWindow(hwnd)
time.sleep(1.5)

rect = wintypes.RECT()
user32.GetWindowRect(hwnd, ctypes.byref(rect))
box = (rect.left, rect.top, rect.right, rect.bottom)
print("hwnd=%s box=%s" % (hwnd, box))

ImageGrab.grab(bbox=box, all_screens=True).save(out)
print("saved", out)
