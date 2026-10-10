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

import functools
import http.server
import json
import os
import shutil
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


def serve(directory: str) -> http.server.ThreadingHTTPServer:
    """A quiet static server on a fixed port, running in this process.

    One thread per request, not the single-threaded `TCPServer`: the browser asks
    for a dozen modules at once, and a serialized server drops one of them often
    enough to be mistaken for a broken build ("Failed to fetch dynamically
    imported module: .../app.js" -- reported against a file that is fine).
    """
    handler = type("Q", (http.server.SimpleHTTPRequestHandler,), {"log_message": lambda *a: None})
    # `directory=` rather than relying on the process cwd: the script is documented
    # to run from anywhere, and a server rooted elsewhere answers 404 for app.js.
    handler = functools.partial(handler, directory=directory)
    httpd = http.server.ThreadingHTTPServer(("127.0.0.1", PORT), handler)
    httpd.daemon_threads = True
    threading.Thread(target=httpd.serve_forever, daemon=True).start()
    return httpd


def main() -> int:
    # A page that mirrors index.html's body and then reports every error to the DOM,
    # so --dump-dom is the whole output channel. Anything thrown while the modules load
    # shows up as text instead of a blank <pre>.
    # A RAW triple-quoted string, and that `r` is load-bearing.
    #
    # The probe below is JavaScript, and it contains `stack.split('\n')`. In a plain
    # Python string literal Python eats that backslash before the browser ever sees
    # it, so the JS ends up with a bare newline inside a single-quoted literal --
    # a SyntaxError, which kills the whole module script. Nothing runs, the module
    # never imports, and `<pre id="log">` stays at "loading" forever. The symptom
    # ("probe did not report") points at the checker, not at the checker being fed
    # a broken page, which is why this one cost an afternoon. Nothing else in here
    # has a backslash, so raw is safe; if you add one, read this again first.
    probe = r"""<!DOCTYPE html><html><head><meta charset="utf-8">
<link rel="stylesheet" href="/frontend/dist/styles.css"></head>
<body>
<div class="app" id="app"></div>
<script>
// The report node is a direct child of <html>, NOT of <body>.
//
// app.js's boot fallback does `document.body.innerHTML = ...` and paints the
// "启动失败" card. That wipes every node inside body -- including a probe
// <pre> living there -- so a checker that reports through the body reports
// "the page never rendered" precisely when the app is most broken. On
// 2026-10-10 that turned a precise `ReferenceError: MODE_LABEL is not
// defined` into a useless "cannot tell load from crash". Nothing that renders
// inside body can outlive a boot failure, so the report lives outside it.
var pre = document.createElement('pre');
pre.id = 'log';
pre.textContent = 'loading';
document.documentElement.appendChild(pre);

window.__errs = [];
window.__phase = 'booting';
window.__report = function (extra) {
  // Write the verdict in a classic script, not from inside the module: a
  // rejected `import()` kills the module, so nothing in it can report on its own
  // behalf. `onunhandledrejection` below is the only listener that still runs.
  var el = document.getElementById('log');
  if (!el) return;
  el.textContent = JSON.stringify(Object.assign({
    errors: window.__errs, rendered: false, chars: 0, pages: {},
  }, extra || {}));
};
addEventListener('error', function (e) {
  window.__errs.push((e.message || e) + ' @ ' + (e.filename || '?') + ':' + (e.lineno || '?'));
  window.__phase = 'threw in: ' + (e.filename || '?') + ':' + (e.lineno || '?');
});
// `preventDefault` stops Chrome from ALSO logging it to stderr, which is noise
// next to the report we are about to print anyway.
addEventListener('unhandledrejection', function (e) {
  var r = e.reason;
  window.__errs.push('unhandled rejection: ' + ((r && (r.message || r.name)) || r));
  window.__phase = 'rejected in: ' + ((r && r.stack) || '').split('\n').slice(0, 2).join(' | ');
  e.preventDefault();
  window.__report({ phase: window.__phase });
});
</script>
<script type="module">
// Every page, not just the first one.
//
// All five views are constructed at boot, but `paint()` shows one of them: the
// others stay hidden, and code inside a view's `mount()` never runs. That is
// exactly how `MODE_LABEL is not defined` shipped (2026-10-10) -- the bug was
// in views/filters.js, reached only from the filter dropdown's tooltip, and the
// toolbar path that calls it also only runs for a profile that actually has
// conditions. Rendering the task page proves nothing about the other four.
//
// So: load once, then click every rail item and let each page settle. Anything
// thrown on the way lands in __errs and fails the run.
var PAGES = ['tasks', 'templates', 'filters', 'history', 'settings'];

// Why the indirection: an exception inside a top-level `await` in a module
// script rejects the *module*, and nothing in this page catches that -- the
// #probeout node just stays at "loading". The failure mode that produces is
// indistinguishable from "the page never loaded". Running the work inside an
// async function called with .catch() puts every throw on a path that reaches
// the report.
const run = async () => {
  await import('/frontend/dist/app.js');

  // app.js swallows a boot failure into its own "启动失败" card, so no window
  // error ever fires and __errs stays empty. Without this the run would go on to
  // report five "no rail item" lines -- technically true, completely useless,
  // and it hides the one line that matters. Read the stack the app printed.
  var fail = document.querySelector('body > div > h2');
  if (fail && fail.textContent.indexOf('启动失败') >= 0) {
    var pre = document.querySelector('body > div > pre');
    window.__errs.push('boot() failed: ' + ((pre && pre.textContent) || '(no stack)').trim());
    window.__phase = 'app painted its 启动失败 card';
    window.__report({ errors: window.__errs, phase: window.__phase, bootFailed: true });
    return;
  }

  var app = document.getElementById('app');
  var perPage = {};

  // Also switch the filter dropdown to every option it offers.
  //
  // Rendering a page only proves the *default* path works. `profileSummary()` is
  // called from the dropdown's tooltip, and its interesting branch (念条件 rather
  // than 念说明) only runs for a profile that has conditions -- so the crash that
  // shipped on 2026-10-10 (`MODE_LABEL is not defined`) was invisible here: the
  // mock's active profile has none, and `Array.map` over an empty array never
  // invokes the callback that referenced the missing name. Selecting each option
  // in turn is what walks that branch.
  var sel = document.querySelector('[data-role="profile"]');
  var filterOpts = [];
  if (sel) {
    for (const o of Array.from(sel.options)) {
      sel.value = o.value;
      sel.dispatchEvent(new Event('change', { bubbles: true }));
      await new Promise(function (r) { setTimeout(r, 250); });
      filterOpts.push(o.value || '(off)');
    }
  }

  for (const p of PAGES) {
    var before = window.__errs.length;
    var item = document.querySelector('.rail__item[data-page="' + p + '"]');
    if (!item) { window.__errs.push('no rail item for page ' + p); continue; }
    item.click();
    await new Promise(function (r) { setTimeout(r, 400); });
    var live = document.querySelector('.page:not([hidden])');
    perPage[p] = {
      shown: live ? live.className : '(nothing shown)',
      chars: live ? live.innerHTML.length : 0,
      errors: window.__errs.length - before,
    };
    // A page that rendered nothing is as broken as one that threw: "空白" and
    // "有内容但少了一块" look the same in every other check.
    if (!live || live.innerHTML.length < 200) {
      window.__errs.push('page ' + p + ' rendered almost nothing (chars=' + perPage[p].chars + ')');
    }
  }
  window.__phase = 'done';
  window.__report({
    errors: window.__errs,
    rendered: !!(app && app.children.length),
    chars: app ? app.innerHTML.length : 0,
    pages: perPage,
    filterOpts: filterOpts,
  });
};
run().catch((e) => {
  var msg = (e && (e.message || e.name)) || String(e);
  window.__errs.push(msg);
  window.__report({ phase: window.__phase });
});
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
                    # Virtual time fast-forwards setTimeout, so the waits below are
                    # nearly free -- but the budget must still cover them. Clicking
                    # five rail items at 400ms plus every filter option at 250ms is
                    # ~3.6s of scheduled time, and the page is cut off mid-report
                    # (leaving <pre> at "loading", which reads as "never rendered")
                    # if the budget runs out first.
                    "--virtual-time-budget=30000",
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
    if report.get("filterOpts") is not None:
        print("   过滤下拉逐项切换: " + " | ".join(report["filterOpts"]))
    for name, info in (report.get("pages") or {}).items():
        print(f"   {name:10} {info['chars']:7d} chars  errors={info['errors']}")
    print(f"OK: no errors on {len(report.get('pages') or {})} pages, "
          f"{report['chars']} chars rendered")
    return 0


if __name__ == "__main__":
    sys.exit(main())
