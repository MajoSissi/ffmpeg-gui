import { icon } from './icons.js';

/** Escape text for safe interpolation into HTML. */
export function esc(v) {
  if (v === null || v === undefined) return '';
  return String(v)
    .replaceAll('&', '&amp;').replaceAll('<', '&lt;').replaceAll('>', '&gt;')
    .replaceAll('"', '&quot;').replaceAll("'", '&#39;');
}

/** Render a <select> from [{value,label}]. */
export function selectHtml(name, options, value, extra = '') {
  return `<select class="select" name="${esc(name)}" ${extra}>${options
    .map((o) => {
      const v = typeof o === 'string' ? o : o.value;
      const l = typeof o === 'string' ? o : o.label;
      return `<option value="${esc(v)}"${String(v) === String(value) ? ' selected' : ''}>${esc(l)}</option>`;
    })
    .join('')}</select>`;
}

export function field(label, control, hint = '', cls = '') {
  return `<div class="field ${cls}"><label>${esc(label)}</label>${control}${hint ? `<span class="hint">${esc(hint)}</span>` : ''}</div>`;
}

export function switchRow(title, hint, name, checked) {
  return `<div class="switch-row">
    <div class="switch-row__text"><b>${esc(title)}</b><span>${esc(hint)}</span></div>
    <label class="switch"><input type="checkbox" name="${esc(name)}"${checked ? ' checked' : ''}></label>
  </div>`;
}

/* -------------------------------------------------------------- formatting */

export function humanSize(n) {
  if (!n || n <= 0) return '—';
  const units = ['B', 'KB', 'MB', 'GB', 'TB'];
  let v = n, i = 0;
  while (v >= 1024 && i < units.length - 1) { v /= 1024; i++; }
  return `${i === 0 ? Math.round(v) : v.toFixed(2).replace(/\.?0+$/, '')} ${units[i]}`;
}

export function humanDuration(sec) {
  if (!sec || sec <= 0) return '—';
  const s = Math.round(sec);
  const h = Math.floor(s / 3600);
  const m = Math.floor((s % 3600) / 60);
  const ss = s % 60;
  return h > 0
    ? `${h}:${String(m).padStart(2, '0')}:${String(ss).padStart(2, '0')}`
    : `${m}:${String(ss).padStart(2, '0')}`;
}

export function humanElapsed(ms) {
  if (!ms || ms <= 0) return '—';
  const s = ms / 1000;
  if (s < 60) return `${s.toFixed(1)} 秒`;
  return humanDuration(s);
}

export function resolution(w, h) {
  return w > 0 && h > 0 ? `${w}×${h}` : '—';
}

export function pct(v) {
  return `${(Math.max(0, Math.min(1, v || 0)) * 100).toFixed(v >= 0.995 ? 0 : 1)}%`;
}

export function num(v, digits = 2) {
  if (!v) return '—';
  const s = Number(v).toFixed(digits);
  // Only trim the fraction. Stripping bare trailing zeros would turn 300 into 3
  // when digits is 0 -- a silent 100x error in anything labelled as a limit.
  return digits > 0 ? s.replace(/\.?0+$/, '') : s;
}

export function bitrateText(bps) {
  if (!bps || bps <= 0) return '—';
  return `${Math.round(bps / 1000)} kbps`;
}

export function secondsText(sec) {
  if (!sec || sec <= 0) return '—';
  return sec.toFixed(2);
}

export function dateText(iso) {
  if (!iso) return '—';
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return '—';
  const p = (n) => String(n).padStart(2, '0');
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}:${p(d.getSeconds())}`;
}

export function ratioText(before, after) {
  if (!before?.size || !after?.size) return '—';
  return `${(after.size / before.size * 100).toFixed(1)}%`;
}

export function ratioClass(before, after) {
  if (!before?.size || !after?.size) return '';
  const r = after.size / before.size;
  if (r < 0.98) return 'delta-down';
  if (r > 1.02) return 'delta-up';
  return '';
}

/* ------------------------------------------------------------------- status */

const STATUS_META = {
  pending: { label: '排队中', chip: 'chip--muted', bar: '' },
  preparing: { label: '分析中', chip: 'chip--accent', bar: '' },
  running: { label: '处理中', chip: 'chip--accent', bar: '' },
  done: { label: '已完成', chip: 'chip--ok', bar: 'is-done' },
  warning: { label: '完成(警告)', chip: 'chip--warn', bar: 'is-warn' },
  failed: { label: '失败', chip: 'chip--err', bar: 'is-err' },
  canceled: { label: '已取消', chip: 'chip--muted', bar: '' },
  skipped: { label: '已跳过', chip: 'chip--muted', bar: '' },
  filtered: { label: '已排除', chip: 'chip--muted', bar: '' },
};

export function statusMeta(s) {
  return STATUS_META[s] || { label: s || '—', chip: 'chip--muted', bar: '' };
}

/**
 * The chip for a job's row.
 *
 * `frozen` is separate from the status on purpose: a suspended job is still
 * `running` as far as the pipeline is concerned, but showing 「处理中」 next to a
 * progress bar that has stopped moving reads as a hang. The queue keeps working on
 * a paused job's terms, so the state is passed alongside rather than folded into
 * STATUS_META -- there is no `paused` status to key off.
 */
export function statusChip(s, frozen) {
  if (frozen) return `<span class="chip chip--warn">已暂停</span>`;
  const m = statusMeta(s);
  return `<span class="chip ${m.chip}">${esc(m.label)}</span>`;
}

/** The label a status column should read, honouring the frozen flag. */
export function statusLabel(s, frozen) {
  return frozen ? '已暂停' : statusMeta(s).label;
}

/* -------------------------------------------------------------------- toast */

let toastHost;

/**
 * A transient notice in the top-right corner. It sits under the window's own
 * button row rather than the bottom-right: the bottom-right corner is where the
 * mouse already is for every action that raises a toast, and a stack of messages
 * there covered the log panel. Top-right is next to the window controls, so it
 * reads as "the app telling you something" rather than "something you triggered".
 *
 * Clicking a toast dismisses it. Messages that ask the user to read something
 * long stay long enough on their own, and the timeout is the fallback, not the
 * only exit.
 *
 * @param {string} message text to show
 * @param {'info'|'success'|'warning'|'error'} kind drives the accent colour
 * @param {number} ms how long it stays before fading on its own
 */
export function toast(message, kind = 'info', ms = 3600) {
  if (!toastHost) {
    toastHost = document.createElement('div');
    toastHost.className = 'toasts';
    document.body.appendChild(toastHost);
  }
  const el = document.createElement('div');
  el.className = `toast ${kind === 'success' ? 'ok' : kind}`;
  el.title = '点击关闭';
  const ic = kind === 'error' ? 'error' : kind === 'warning' ? 'warning' : kind === 'success' ? 'checkCircle' : 'info';
  el.innerHTML = `${icon(ic, 'sm')}<span>${esc(message)}</span>`;

  let timer = 0;
  const dismiss = () => {
    if (!el.isConnected) return;
    clearTimeout(timer);
    el.style.transition = 'opacity .18s, transform .18s';
    el.style.opacity = '0';
    el.style.transform = 'translateX(10px)';
    setTimeout(() => el.remove(), 200);
  };
  el.addEventListener('click', dismiss);
  toastHost.appendChild(el);
  timer = setTimeout(dismiss, ms);
}

/**
 * Await a shell action (open / reveal) and report failure as a toast.
 *
 * The OS shell can legitimately refuse -- the file was moved or deleted since the
 * queue was built, or we lack permission. Without this the rejection is simply
 * dropped and the button appears to do nothing, which is exactly the confusion
 * that made the old "reveal source file" look broken. The backend already phrases
 * the message for display.
 *
 * @param {Promise<any>} promise result of a reveal/open API call
 */
export async function shellAction(promise) {
  try {
    await promise;
  } catch (err) {
    toast(String(err?.message || err), 'error', 5200);
  }
}

/* ------------------------------------------------------------------ command */

// A token starts a new argument line when it looks like a flag. Requiring a
// letter after the dashes matters: `-0:v?` is a *value* of `-map`, and treating
// it as a flag would split that pair across two lines.
const CMD_FLAG = /^-{1,2}[A-Za-z]/;

/**
 * Lay out a flat ffmpeg argv as one flag per line, keeping each flag with its
 * value.
 *
 * The preview exists to show exactly which flags are being passed, and a single
 * wrapped line hides that. Pairing is inferred from the token shapes rather than
 * from a table of flags: a flag starts a line, following non-flag tokens join it,
 * and the last token is always the output path so it gets its own line.
 *
 * @param {string} bin ffmpeg executable, shown on the first line
 * @param {string[]} args flat argument list
 * @returns {string[]} display lines
 */
export function commandLines(bin, args) {
  const all = [bin || 'ffmpeg', ...args];
  const lines = [];
  for (let i = 0; i < all.length; i += 1) {
    const tok = all[i];
    // The output path is the last argument and never belongs to the flag in
    // front of it, however that flag's value happens to look.
    if (i === all.length - 1 && i > 0) {
      lines.push(tok);
      break;
    }
    if (!CMD_FLAG.test(tok)) {
      lines.push(tok);
      continue;
    }
    let line = tok;
    while (i + 1 < all.length - 1 && !CMD_FLAG.test(all[i + 1])) {
      i += 1;
      line += ' ' + all[i];
    }
    lines.push(line);
  }
  return lines;
}

/**
 * Render a command for display. Falls back to `fallback` when there is nothing to
 * show, so callers do not need to branch.
 */
export function commandHtml(bin, args, fallback = '（无法生成命令）') {
  const list = Array.isArray(args) ? args : [];
  if (!list.length) {
    return `<div class="cmd"><div class="cmd__line"><span class="cmd__val">${esc(fallback)}</span></div></div>`;
  }
  const body = commandLines(bin, list).map((line, i) => {
    const cls = i === 0 ? 'cmd__bin' : CMD_FLAG.test(line) ? 'cmd__flag' : 'cmd__val';
    return `<div class="cmd__line"><span class="${cls}">${esc(line)}</span></div>`;
  }).join('');
  return `<div class="cmd">${body}</div>`;
}

/* -------------------------------------------------------------------- modal */

let modalHost;
let modalCloser = null;

function ensureModalHost() {
  if (!modalHost) {
    modalHost = document.createElement('div');
    modalHost.className = 'modal-root';
    modalHost.innerHTML = '<div class="scrim"></div><div class="modal" role="dialog"></div>';
    document.body.appendChild(modalHost);
    modalHost.querySelector('.scrim').addEventListener('click', () => closeModal());
    document.addEventListener('keydown', (e) => {
      if (e.key === 'Escape' && modalHost.classList.contains('is-open')) closeModal();
    });
  }
  return modalHost;
}

/** Open a modal. Returns { root, modal, close }. */
export function openModal({ title, body, footer = '', size = '' }) {
  const host = ensureModalHost();
  const modal = host.querySelector('.modal');
  modal.className = `modal ${size}`;
  modal.innerHTML = `
    <div class="modal__head">
      <h2>${esc(title)}</h2>
      <div class="spacer"></div>
      <button class="btn btn--text btn--icon" data-close aria-label="关闭">${icon('close')}</button>
    </div>
    <div class="modal__body"></div>
    ${footer ? `<div class="modal__foot">${footer}</div>` : ''}`;
  modal.querySelector('.modal__body').innerHTML = body;
  modal.querySelector('[data-close]').addEventListener('click', () => closeModal());
  host.classList.add('is-open');

  const onKey = (e) => { if (e.key === 'Escape') closeModal(); };
  modalCloser = () => document.removeEventListener('keydown', onKey);
  document.addEventListener('keydown', onKey);
  return { root: host, modal, close: closeModal };
}

export function closeModal() {
  if (!modalHost) return;
  modalHost.classList.remove('is-open');
  modalCloser?.();
  modalCloser = null;
}

export function confirmDialog(title, message, confirmLabel = '确定', danger = false) {
  return new Promise((resolve) => {
    const { modal, close } = openModal({
      title,
      // Its own size class, not modal--narrow: the generic chrome (a 63px head
      // and a 59px foot around a 43px body) squeezed the sentence into a thin
      // band that read as off-centre. See .modal--dialog in styles.css.
      size: 'modal--dialog',
      body: `<p class="dialog__text">${esc(message)}</p>`,
      footer: `<div class="spacer"></div>
        <button class="btn btn--sm ${danger ? 'btn--danger' : 'btn--filled'}" data-yes>${esc(confirmLabel)}</button>
        <button class="btn btn--sm btn--tonal" data-no>取消</button>`,
    });
    modal.querySelector('[data-no]').addEventListener('click', () => { close(); resolve(false); });
    modal.querySelector('[data-yes]').addEventListener('click', () => { close(); resolve(true); });
  });
}

/* ------------------------------------------------------------------ helpers */

export async function copyText(text) {
  try {
    if (globalThis.runtime?.ClipboardSetText) {
      globalThis.runtime.ClipboardSetText(text);
      return true;
    }
    await navigator.clipboard.writeText(text);
    return true;
  } catch {
    return false;
  }
}

/** Read a form into a plain object, coercing numbers where the field says so. */
export function readForm(root, numberFields = []) {
  const out = {};
  const nums = new Set(numberFields);
  root.querySelectorAll('[name]').forEach((el) => {
    const n = el.name;
    if (el.type === 'checkbox') out[n] = el.checked;
    else if (nums.has(n)) out[n] = el.value === '' ? 0 : Number(el.value);
    else out[n] = el.value;
  });
  return out;
}
