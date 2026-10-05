import { api, on, EVENTS, isMock } from './api.js';
import { icon, brandSvg } from './icons.js';
import { esc, toast, confirmDialog } from './ui.js';
import { createTasksView } from './views/tasks.js';
import { createTemplatesView } from './views/templates.js';
import { createHistoryView } from './views/history.js';
import { createSettingsView } from './views/settings.js';

/* ------------------------------------------------------------------- state */

const state = {
  page: 'tasks',
  settingsCategory: 'binary',
  runtime: {},
  settings: {},
  options: {},
  templates: [],
  currentTemplateId: '',
  jobs: [],
  stats: {},
  history: [],
  selectedJobId: '',
  recursive: true,
  loaded: false,
};

const ctx = {
  state, api, toast,
  notifyTemplatesChanged: () => views.templates?.onTemplatesChanged?.(),
  // Assigned once showContextMenu exists; views need it for row-level actions.
  showContextMenu: (...a) => showContextMenu(...a),
};

// `title` is only the rail item's tooltip now. It used to head a page banner as well,
// which put the same sentence on screen twice; the rail's own label names the page.
const PAGES = [
  { id: 'tasks', label: '任务', icon: 'queue', title: '任务队列' },
  { id: 'templates', label: '模板', icon: 'layers', title: '参数模板' },
  { id: 'history', label: '记录', icon: 'history', title: '处理记录' },
  { id: 'settings', label: '设置', icon: 'settings', title: '设置' },
];

const views = {
  tasks: createTasksView(ctx),
  templates: createTemplatesView(ctx),
  history: createHistoryView(ctx),
  settings: createSettingsView(ctx),
};

/* --------------------------------------------------------------------- DOM */

const app = document.getElementById('app');
// One frame, two columns. The rail runs from the very top of the window to the bottom --
// logo, four destinations, and then straight into the content -- so nothing is boxed in.
// The window controls and status chips sit in the content column's own top row, on the
// same line as the logo rather than in a band above it.
//
// No titles anywhere on that row. "FFmpeg GUI" in the bar and a page title underneath it
// said the same thing twice, sixty pixels apart, and the rail's own label already names
// the page. What is left of the bar is the part that earns its place: the drag strip,
// the window's state, and the three buttons.
//
// The rail keeps the only logo. It used to sit next to an 18px copy in the title bar,
// and its plate was a rounded box around the rounded tile -- a frame inside a frame.
app.innerHTML = `
  <nav class="rail">
    <div class="rail__brand" title="FFmpeg GUI">${brandSvg()}</div>
    <div class="rail__items">
      ${PAGES.map((p) => `
        <button class="rail__item" data-page="${p.id}" title="${esc(p.title)}">
          ${icon(p.icon)}
          <span>${p.label}</span>
          <span class="rail__badge" data-badge="${p.id}" hidden></span>
        </button>`).join('')}
    </div>
  </nav>
  <main class="main">
    <div class="titlebar">
      <div class="titlebar__chips" data-role="chips"></div>
      <div class="spacer"></div>
      <div class="titlebar__btns">
        <button class="winbtn" data-win="min" title="最小化">${winIcon('min')}</button>
        <button class="winbtn" data-win="max" title="最大化">${winIcon('max')}</button>
        <button class="winbtn winbtn--close" data-win="close" title="关闭">${winIcon('close')}</button>
      </div>
    </div>
    <div data-role="pagehost" style="flex:1;min-height:0;display:flex"></div>
  </main>
  <div class="context-menu" data-role="ctxmenu" hidden></div>
  <div class="drop-overlay"><div class="drop-overlay__box">${icon('download', 'lg')}松开即可添加到队列</div></div>`;

/** The three window glyphs, drawn as paths so they inherit the button's colour. */
function winIcon(kind) {
  const d = {
    min: 'M0 5h10v1.4H0z',
    max: 'M0.6 0.6h8.8v8.8H0.6zm1.4 1.4v6h6V2z',
    close: 'M0.7 0.7 5 5l4.3-4.3 1 1L6 6l4.3 4.3-1 1L5 7l-4.3 4.3-1-1L4 6-.3 1.7z',
  }[kind];
  return `<svg viewBox="0 0 10 10" aria-hidden="true"><path d="${d}"/></svg>`;
}

const pageHost = app.querySelector('[data-role=pagehost]');
const overlay = document.querySelector('.drop-overlay');
const ctxMenu = app.querySelector('[data-role=ctxmenu]');

/* ---------------------------------------------------------- context menu */

/**
 * A shared right-click menu. The window's native menu is gone with the frame, so a
 * long-press / right-click inside the app needs somewhere to go. It is deliberately
 * minimal: only the actions that are useful *at that row*, never a copy of the whole
 * toolbar, and never anything destructive without a confirmation behind it.
 *
 * @param {number} x client coordinates
 * @param {number} y client coordinates
 * @param {{label:string, icon?:string, danger?:boolean, onClick:Function}[]} items
 */
function showContextMenu(x, y, items) {
  ctxMenu.innerHTML = items.map((it, i) => `
    <button class="ctxmenu__item${it.danger ? ' is-danger' : ''}" data-ctx="${i}">
      ${it.icon ? icon(it.icon, 'sm') : ''}<span>${esc(it.label)}</span>
    </button>`).join('');
  ctxMenu.hidden = false;
  // Measure before clamping: the menu is anchored to the click, and near the right or
  // bottom edge it has to grow the other way or part of it lands off-screen.
  const r = ctxMenu.getBoundingClientRect();
  ctxMenu.style.left = `${Math.min(x, window.innerWidth - r.width - 8)}px`;
  ctxMenu.style.top = `${Math.min(y, window.innerHeight - r.height - 8)}px`;
  ctxMenu.querySelectorAll('[data-ctx]').forEach((b) => {
    b.addEventListener('click', () => {
      hideContextMenu();
      items[Number(b.dataset.ctx)].onClick();
    });
  });
}

function hideContextMenu() {
  ctxMenu.hidden = true;
  ctxMenu.innerHTML = '';
}

// Any click elsewhere, or Escape, closes it. The capture phase matters: a right-click
// that opens the menu must not immediately close it again.
window.addEventListener('pointerdown', (e) => {
  if (!ctxMenu.hidden && !e.target.closest('.context-menu')) hideContextMenu();
}, true);
window.addEventListener('blur', hideContextMenu);

/* --------------------------------------------------------- window controls */

// The window is frameless, so the app owns its own buttons. They go through the Wails
// runtime directly; close routes to the app's QuitApp so the tray / running-jobs checks
// still run instead of the window vanishing.
const rt = globalThis.runtime;
app.querySelector('.titlebar__btns').addEventListener('click', (e) => {
  const b = e.target.closest('[data-win]');
  if (!b || !rt) return;
  const kind = b.dataset.win;
  if (kind === 'min') rt.WindowMinimise();
  else if (kind === 'max') rt.WindowToggleMaximise();
  else if (kind === 'close') api.quitApp();
});
// The bar is the title bar, so dragging its empty area moves the window and
// double-clicking it maximises, the way a native one does. The rail is NOT part of it:
// the logo and the destinations live up there, and making that strip draggable would
// mean a click that missed a button dragged the window instead.
const titlebar = app.querySelector('.titlebar');
titlebar.addEventListener('dblclick', (e) => {
  if (e.target.closest('.winbtn')) return;
  rt?.WindowToggleMaximise();
});

Object.values(views).forEach((v) => {
  v.el.style.display = 'none';
  pageHost.appendChild(v.el);
});

/* ------------------------------------------------------------- navigation */

async function go(page) {
  if (page === state.page) return;
  const current = views[state.page];
  if (current?.hasUnsaved?.()) {
    const ok = await confirmDialog('放弃未保存的修改？', '离开当前页面会丢失尚未保存的改动。', '放弃并离开', true);
    if (!ok) return;
  }
  state.page = page;
  await paint();
}

async function paint() {
  PAGES.forEach((p) => {
    const item = app.querySelector(`[data-page=${p.id}]`);
    item.classList.toggle('is-active', p.id === state.page);
    item.setAttribute('aria-current', p.id === state.page ? 'page' : 'false');
  });
  // Nothing to write at the top of the page any more: no window title, no page heading.
  // The rail's active label is the only place the current page is named.

  Object.entries(views).forEach(([id, v]) => {
    v.el.style.display = id === state.page ? 'flex' : 'none';
  });
  await views[state.page]?.mount?.();
}

app.querySelector('.rail__items').addEventListener('click', (e) => {
  const b = e.target.closest('[data-page]');
  if (b) go(b.dataset.page);
});

/* --------------------------------------------------------------- top chips */

function paintChips() {
  const host = app.querySelector('[data-role=chips]');
  const rt = state.runtime || {};
  const ok = rt.ffmpeg?.ok && rt.ffprobe?.ok;
  const s = state.stats || {};
  const running = (s.running || 0) > 0;
  host.innerHTML = `
    ${running ? `<span class="chip chip--accent">${icon('speed')}${s.running} 个任务处理中</span>` : ''}
    ${s.paused ? `<span class="chip chip--warn">${icon('pause')}队列已暂停</span>` : ''}
    ${state.settings.preventSleep && running ? `<span class="chip chip--ok">${icon('power')}已阻止休眠</span>` : ''}
    <span class="chip ${ok ? 'chip--muted' : 'chip--err'} chip--click" data-role="binchip" title="${esc(rt.ffmpeg?.path || '')}">
      ${icon(ok ? 'check' : 'warning')}${ok ? 'ffmpeg 就绪' : '未找到 ffmpeg'}
    </span>`;
  host.querySelector('[data-role=binchip]').addEventListener('click', () => {
    state.settingsCategory = 'binary';
    go('settings');
  });
}

function paintBadges() {
  const s = state.stats || {};
  const running = (s.running || 0) + (s.pending || 0);
  const failed = (s.failed || 0) + (s.warning || 0);
  const set = (id, n, cls) => {
    const el = app.querySelector(`[data-badge=${id}]`);
    if (!el) return;
    el.hidden = !n;
    el.textContent = n > 99 ? '99+' : n;
    el.style.background = cls || '';
  };
  set('tasks', running);
  set('history', failed, failed ? 'var(--warn)' : '');
}

/* ------------------------------------------------------------------ events */

on(EVENTS.jobUpdate, (job) => {
  const i = state.jobs.findIndex((j) => j.id === job.id);
  if (i >= 0) state.jobs[i] = job; else state.jobs.push(job);
  views.tasks.onJobUpdate(job);
});

on(EVENTS.jobLog, (batch) => views.tasks.onLog(batch));

on(EVENTS.queue, (stats) => {
  state.stats = stats || {};
  views.tasks.onStats(state.stats);
  paintChips();
  paintBadges();
});

on(EVENTS.toast, (t) => {
  const kind = t.kind === 'error' ? 'error' : t.kind === 'warning' ? 'warning' : t.kind === 'success' ? 'success' : 'info';
  toast(t.message, kind);
});

on(EVENTS.record, () => {
  if (state.page === 'history') views.history.onRecord?.();
  else views.history.onRecord?.();
});

on(EVENTS.templatesChanged, (list) => {
  state.templates = list || state.templates;
  views.tasks.onTemplatesChanged();
  views.templates.onTemplatesChanged();
});

/* --------------------------------------------------------------- drag drop */

// The overlay is about dropping *files*, so it must only appear for that. A plain
// dragenter also fires when the user selects text and drags it, or drags an image out of
// a page — and preventDefault on those is what makes text unselectable inside the app.
// Checking the type list is the only reliable discriminator: "Files" is present exactly
// when the drag carries files from outside the window.
const carriesFiles = (e) => Array.from(e.dataTransfer?.types || []).includes('Files');

let dragDepth = 0;
window.addEventListener('dragenter', (e) => {
  if (!carriesFiles(e)) return;
  e.preventDefault();
  dragDepth++;
  overlay.classList.add('is-on');
});
window.addEventListener('dragover', (e) => {
  if (!carriesFiles(e)) return;
  e.preventDefault();
  if (e.dataTransfer) e.dataTransfer.dropEffect = 'copy';
});
window.addEventListener('dragleave', () => {
  // Unconditional: the depth counter is only ever incremented for a file drag, and some
  // browsers empty dataTransfer.types on the leave event, so gating this on the type
  // list could strand the overlay on screen.
  dragDepth = Math.max(0, dragDepth - 1);
  if (dragDepth === 0) overlay.classList.remove('is-on');
});
window.addEventListener('drop', (e) => {
  if (!carriesFiles(e)) return;
  e.preventDefault();
  dragDepth = 0;
  overlay.classList.remove('is-on');
});

if (globalThis.runtime?.OnFileDrop) {
  // useDropTarget = false so a drop anywhere in the window is accepted.
  globalThis.runtime.OnFileDrop(async (x, y, paths) => {
    if (!paths?.length) return;
    const res = await api.addDroppedFiles(paths);
    if (res?.added) {
      if (state.page !== 'tasks') go('tasks');
      else views.tasks.mount();
    }
  }, false);
}

/* ------------------------------------------------------------- shortcuts */

window.addEventListener('keydown', async (e) => {
  const mod = e.ctrlKey || e.metaKey;
  if (mod && e.key.toLowerCase() === 'o') {
    e.preventDefault();
    const r = await api.addFilesDialog(state.recursive);
    if (r?.added) { views.tasks.mount(); toast(`已添加 ${r.added} 个文件`, 'success'); }
  } else if (mod && e.key.toLowerCase() === 'k') {
    e.preventDefault();
    go('tasks');
  } else if (mod && e.key === 'Enter') {
    e.preventDefault();
    await api.startQueue();
  } else if (e.key === 'F5') {
    e.preventDefault();
    toast('界面已刷新', 'info', 1200);
    await boot(true);
  }
});

/* ------------------------------------------------------------------- boot */

async function boot(silent = false) {
  const data = await api.bootstrap();
  state.runtime = data.runtime || {};
  state.settings = data.settings || {};
  state.options = data.options || {};
  state.templates = data.templates || [];
  state.jobs = data.jobs || [];
  state.stats = data.stats || {};
  state.history = data.history || [];
  state.recursive = true;
  // The global template is pinned first in the list but holds only defaults, so
  // the queue must never default to it.
  const processable = state.templates.filter((t) => !t.global);
  state.currentTemplateId = processable.some((t) => t.id === state.settings.lastTemplateId)
    ? state.settings.lastTemplateId
    : processable[0]?.id || '';
  if (!state.selectedJobId && state.jobs.length) state.selectedJobId = state.jobs[0].id;

  // window title also carries the version
  document.title = `FFmpeg GUI ${state.runtime.version || ''}`.trim();

  await paint();
  views.tasks.onJobsChanged();
  paintChips();
  paintBadges();
  if (!silent && isMock) toast('浏览器预览模式：数据为演示内容', 'info', 4200);
}

boot().catch((err) => {
  console.error(err);
  document.body.innerHTML = `<div style="padding:40px;font-family:system-ui;color:#e3e4ea">
    <h2>启动失败</h2><pre style="color:#ffb3ac;white-space:pre-wrap">${esc(err?.stack || err?.message || String(err))}</pre>
  </div>`;
});
