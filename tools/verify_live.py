"""Verify the app as the user would actually run it: launched by Explorer.

Why Explorer: our shell spawns children inside a Job Object that forbids
breakaway (CREATE_BREAKAWAY_FROM_JOB fails with ERROR_ACCESS_DENIED). Chromium's
process sandbox needs to build its own job hierarchy, so every WebView2 child
process dies instantly under that job. Explorer is a normal desktop process with
no such restriction, so this reproduces a real double-click.

The script also calls IsProcessInJob on the window's owning process to prove
whether the job is present, and probes responsiveness with
SendMessageTimeout(WM_NULL) -- a hung window fails to answer.
"""
import ctypes
import subprocess
import sys
import time
from ctypes import wintypes as wt

from PIL import ImageGrab

user32 = ctypes.windll.user32
kernel32 = ctypes.windll.kernel32
user32.SetProcessDPIAware()

EnumWindowsProc = ctypes.WINFUNCTYPE(ctypes.c_bool, wt.HWND, wt.LPARAM)

WM_NULL = 0x0000
SMTO_ABORTIFHUNG = 0x0002
PROCESS_QUERY_LIMITED_INFORMATION = 0x1000


def list_windows(substr):
    hits = []

    def cb(hwnd, lparam):
        n = user32.GetWindowTextLengthW(hwnd)
        if n and user32.IsWindowVisible(hwnd):
            buf = ctypes.create_unicode_buffer(n + 1)
            user32.GetWindowTextW(hwnd, buf, n + 1)
            if substr.lower() in buf.value.lower():
                hits.append((hwnd, buf.value))
        return True

    user32.EnumWindows(EnumWindowsProc(cb), 0)
    return hits


def responsive(hwnd):
    res = ctypes.c_size_t()
    r = user32.SendMessageTimeoutW(
        hwnd, WM_NULL, 0, 0, SMTO_ABORTIFHUNG, 3000, ctypes.byref(res)
    )
    return bool(r)


def in_job(pid):
    h = kernel32.OpenProcess(PROCESS_QUERY_LIMITED_INFORMATION, False, pid)
    if not h:
        return None
    flag = wt.BOOL()
    ok = kernel32.IsProcessInJob(h, None, ctypes.byref(flag))
    kernel32.CloseHandle(h)
    return bool(flag.value) if ok else None


exe = sys.argv[1]
out = sys.argv[2] if len(sys.argv) > 2 else None
wait = float(sys.argv[3]) if len(sys.argv) > 3 else 14.0
needle = sys.argv[4] if len(sys.argv) > 4 else "FFmpeg GUI"

if exe == "--attach":
    print("attaching to an already running process")
else:
    subprocess.Popen(["explorer.exe", exe])
    print("launched via explorer:", exe)

deadline = time.time() + wait
seen = []
while time.time() < deadline:
    hits = list_windows(needle)
    if hits:
        seen = hits
    time.sleep(1.0)

if not seen:
    print("RESULT: no window matching %r appeared" % needle)
    sys.exit(2)

hwnd, title = seen[0]
pid = wt.DWORD()
user32.GetWindowThreadProcessId(hwnd, ctypes.byref(pid))
alive = responsive(hwnd)
print("window   : hwnd=%s" % hwnd)
print("title    : %r" % title)
print("pid      : %d" % pid.value)
print("in_job   : %s" % in_job(pid.value))
print("responds : %s" % alive)
print("hung     : %s" % ("(未响应)" in title))

if out:
    SWP_NOMOVE, SWP_NOSIZE, SWP_SHOWWINDOW = 0x0002, 0x0001, 0x0040
    user32.SetWindowPos(hwnd, -1, 0, 0, 0, 0, SWP_NOMOVE | SWP_NOSIZE | SWP_SHOWWINDOW)
    time.sleep(1.5)
    rect = wt.RECT()
    user32.GetWindowRect(hwnd, ctypes.byref(rect))
    ImageGrab.grab(
        bbox=(rect.left, rect.top, rect.right, rect.bottom), all_screens=True
    ).save(out)
    print("saved    :", out)
