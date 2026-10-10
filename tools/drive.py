"""Drive the app in a headless browser and print what the driver reports.

One entry point for `tools/_drive.html`: serve the project root, load the driver
with a query string, and print `#probeout` -- which is where the probe and the
`expr=` expression both write. Everything the driver needs to say goes through
that one place because `--dump-dom` does not serialize an iframe.

    python tools/drive.py "page=templates&probe=1"
    python tools/drive.py "page=templates&tpl=global&sec=filter&expr=<js>"
    python tools/drive.py "page=templates&tpl=t-4k2k&sec=output" shot.png
    python tools/drive.py "page=tasks&acts=%5Bdata-act%3Dadd-folder%5D&type=...&probe=1"

`expr=`, `set=`, `acts=` and `type=` values are URL-encoded by the caller. Order is
`set=` → `acts=` → `type=` → probe/expr: `set=` fires `change` and lands before the
clicks, `type=` fires `input` and lands after them, so it can reach a box inside a
panel that a click has just opened. An optional second argument is a png to write:
same query, same page state, plus pixels.

A step inside `acts=` may itself be `type:sel@text` or `set:sel@text`, which puts one
value assignment in the middle of the click sequence -- the two whole-parameter forms
can only sit before or after all of it, so a "type, then click save" flow cannot be
expressed with them.

Run one probe at a time: the port is fixed and Windows lets a second process bind it
too, after which the two runs interleave and both chains miss everything.

Two Windows traps this exists to avoid: PowerShell's `& chrome ... --dump-dom`
produces no output at all here (neither file nor stdout), and a single-threaded
`TCPServer` drops one page load out of a run, which reads as "the app is broken".
So: subprocess from Python, and a thread per request.
"""
import http.server
import os
import re
import shutil
import subprocess
import sys
import tempfile
import threading

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
PORT = 8731

# The window the capture uses. The default 800x600 cuts the action column off the
# task list, which looks exactly like "the buttons were never rendered".
WINDOW = "1720,1000"


def find_chrome() -> str:
    for rel in (
        r"C:\Program Files\Google\Chrome\Application\chrome.exe",
        r"C:\Program Files (x86)\Google\Chrome\Application\chrome.exe",
        r"C:\Program Files (x86)\Microsoft\Edge\Application\msedge.exe",
        r"C:\Program Files\Microsoft\Edge\Application\msedge.exe",
    ):
        if os.path.exists(rel):
            return rel
    found = shutil.which("chrome") or shutil.which("google-chrome") or shutil.which("chromium")
    if not found:
        sys.exit("no Chrome/Edge found; set CHROME to its path")
    return found


def main() -> int:
    query = sys.argv[1] if len(sys.argv) > 1 else "probe=1"
    # A second argument turns the same navigation into a screenshot, so the picture
    # and the probe always describe the same page state -- a capture taken through a
    # second script is a second chance to get the query string wrong.
    shot = sys.argv[2] if len(sys.argv) > 2 else ""
    handler = type("Q", (http.server.SimpleHTTPRequestHandler,), {"log_message": lambda *a: None})
    httpd = http.server.ThreadingHTTPServer(("127.0.0.1", PORT), handler)
    httpd.daemon_threads = True
    threading.Thread(target=httpd.serve_forever, daemon=True).start()

    url = f"http://127.0.0.1:{PORT}/tools/_drive.html?{query}"
    with tempfile.TemporaryDirectory() as tmp:
        args = [
            find_chrome(), "--headless=new", "--disable-gpu", "--no-sandbox",
            f"--user-data-dir={os.path.join(tmp, 'profile')}",
            f"--window-size={WINDOW}",
            "--virtual-time-budget=15000",
            "--dump-dom", url,
        ]
        if shot:
            args.append(f"--screenshot={os.path.abspath(shot)}")
        proc = subprocess.run(
            args,
            capture_output=True, text=True, encoding="utf-8", errors="replace", timeout=180,
        )
    dom = proc.stdout or ""
    if shot:
        print(f"saved {os.path.abspath(shot)}")
    m = re.search(r'<pre id="probeout">(.*?)</pre>', dom, re.S)
    if not m:
        # A screenshot-only run writes no probe output, and that is not a failure:
        # only complain when the query actually asked for a probe or an expression.
        if shot and not re.search(r'(?:^|&)(?:probe|expr)=', query):
            return 0
        print("no #probeout in the dump -- the driver never reached the end of its chain")
        print(dom[:2000])
        return 1
    print(m.group(1)
          .replace("&lt;", "<").replace("&gt;", ">").replace("&amp;", "&").replace("&quot;", '"'))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
