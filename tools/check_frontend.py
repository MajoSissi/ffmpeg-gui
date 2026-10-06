"""Load every frontend ES module the way the browser does, and report what breaks.

`node --check` only parses: it happily accepts a module that references a name nobody
declared, because the reference is a runtime lookup, not a syntax error. That is not a
theoretical gap -- a MOCK_* constant that got dropped during an edit left the app
rendering nothing at all, and the symptom ("the window is a flat colour") pointed nowhere
near the file that caused it.

So this loads the real entry point with a DOM stand-in, captures every window error and
unhandled rejection, and exits non-zero if any module throws. A blank UI is a bug you
notice minutes later; this fails in a second.

    python tools/check_frontend.py

Needs no dependencies. It imports /frontend/dist/app.js via a data: URL so the browser
resolves the same relative specifiers it would in the app.
"""

import http.server
import json
import os
import shutil
import socketserver
import subprocess
import sys
import tempfile
import threading

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
PORT = 8799
ENTRY = "/frontend/dist/index.html"


def find_chrome() -> str:
    """The system browser, since this project deliberately has no automation deps."""
    for env in ("CHROME", "CHROME_PATH"):
        p = os.environ.get(env)
        if p and os.path.exists(p):
            return p
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


def serve(directory: str) -> socketserver.TCPServer:
    """A quiet static server on a fixed port, running in this process."""
    handler = type("Q", (http.server.SimpleHTTPRequestHandler,), {"log_message": lambda *a: None})
    socketserver.TCPServer.allow_reuse_address = True
    httpd = socketserver.TCPServer(("127.0.0.1", PORT), handler)
    threading.Thread(target=httpd.serve_forever, daemon=True).start()
    return httpd


def main() -> int:
    # A page that mirrors index.html's body and then reports every error to the DOM,
    # so --dump-dom is the whole output channel. Anything thrown while the modules load
    # shows up as text instead of a blank <pre>.
    probe = """<!DOCTYPE html><html><head><meta charset="utf-8">
<link rel="stylesheet" href="/frontend/dist/styles.css"></head>
<body>
<div class="app" id="app"></div>
<pre id="log">loading</pre>
<script>
window.__errs = [];
addEventListener('error', function (e) {
  window.__errs.push((e.message || e) + ' @ ' + (e.filename || '?') + ':' + (e.lineno || '?'));
});
addEventListener('unhandledrejection', function (e) {
  window.__errs.push('unhandled rejection: ' + ((e.reason && e.reason.message) || e.reason));
});
</script>
<script type="module">
try {
  await import('/frontend/dist/app.js');
  await new Promise(function (r) { setTimeout(r, 1500); });
  var app = document.getElementById('app');
  document.getElementById('log').textContent = JSON.stringify({
    errors: window.__errs,
    rendered: !!(app && app.children.length),
    chars: app ? app.innerHTML.length : 0,
  });
} catch (e) {
  document.getElementById('log').textContent = JSON.stringify({
    errors: window.__errs.concat([(e && e.message) || String(e)]),
    rendered: false, chars: 0,
  });
}
</script>
</body></html>"""
    with tempfile.TemporaryDirectory() as tmp:
        with open(os.path.join(ROOT, ".check_frontend.html"), "w", encoding="utf-8") as fh:
            fh.write(probe)
        httpd = serve(ROOT)
        try:
            out = subprocess.run(
                [
                    find_chrome(),
                    "--headless=new",
                    "--disable-gpu",
                    "--no-sandbox",
                    f"--user-data-dir={os.path.join(tmp, 'profile')}",
                    "--virtual-time-budget=8000",
                    "--dump-dom",
                    f"http://127.0.0.1:{PORT}/.check_frontend.html",
                ],
                capture_output=True,
                text=True,
                encoding="utf-8",
                errors="replace",
                timeout=120,
            ).stdout
        finally:
            httpd.shutdown()
            os.remove(os.path.join(ROOT, ".check_frontend.html"))

    start = out.find("<pre id=\"log\">")
    if start < 0:
        print("FAIL: the probe page never rendered; cannot tell load from crash")
        return 1
    end = out.find("</pre>", start)
    body = out[start + len("<pre id=\"log\">"): end]

    try:
        report = json.loads(body)
    except json.JSONDecodeError:
        print("FAIL: probe did not report (page threw before it could write)\n" + body[:400])
        return 1

    if report["errors"]:
        print("FAIL: the frontend threw while loading")
        for e in report["errors"]:
            print("  " + e)
        return 1
    if not report["rendered"] or report["chars"] < 500:
        print(f"FAIL: loaded clean but rendered nothing (chars={report['chars']})")
        return 1
    print(f"OK: no errors, {report['chars']} chars rendered")
    return 0


if __name__ == "__main__":
    sys.exit(main())
