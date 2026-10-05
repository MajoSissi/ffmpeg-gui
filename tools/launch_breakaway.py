"""Launch a Windows GUI app fully detached from any inherited Job Object.

Rationale: Chromium's process sandbox cannot create its child processes when the
browser process itself sits inside another Job Object that forbids nested jobs.
This launcher uses CREATE_BREAKAWAY_FROM_JOB so the target behaves exactly like a
user double-clicking the exe from Explorer (Explorer's processes are not in a
restrictive job), which lets us tell a real WebView2 failure apart from an
artifact of how our shell spawned the process.
"""
import os
import subprocess
import sys

DETACHED_PROCESS = 0x00000008
CREATE_NEW_PROCESS_GROUP = 0x00000200
CREATE_BREAKAWAY_FROM_JOB = 0x01000000

exe = sys.argv[1]
log_file = sys.argv[2] if len(sys.argv) > 2 else None
extra = sys.argv[3] if len(sys.argv) > 3 else ""

env = os.environ.copy()
if log_file:
    args = "--enable-logging --v=1 --log-file=%s" % log_file.replace("/", "\\")
    if extra:
        args += " " + extra
    env["WVPROBE_ARGS"] = args

flags = DETACHED_PROCESS | CREATE_NEW_PROCESS_GROUP | CREATE_BREAKAWAY_FROM_JOB

try:
    p = subprocess.Popen([exe], env=env, creationflags=flags, close_fds=True)
except OSError as exc:
    print("BREAKAWAY FAILED (%s), retrying without the breakaway flag" % exc)
    p = subprocess.Popen(
        [exe], env=env, creationflags=DETACHED_PROCESS | CREATE_NEW_PROCESS_GROUP, close_fds=True
    )

print("launched pid=%d breakaway=%s" % (p.pid, "yes"))
