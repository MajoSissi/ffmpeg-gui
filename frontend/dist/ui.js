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

/**
 * 参数的开关，两种密度，一个控件。
 *
 * 界面上只该有一种"打开 / 关闭"的样子。以前参数框有两种：一部分是左右开关，另一
 * 部分是原生方框勾选 —— 同一个界面里两种语言，用户得先分辨"这个勾选框是不是也是
 * 开关"。现在原生勾选框只剩列表的多选（选中文件不是设置），其余全走这里。
 *
 * `switchRow`：整行，标题在左、开关在右，用于段里独立的一条设置。
 * `switchInline`：紧凑，几个可以并排（`.switch-strip`），用于一组相关的参数。
 *
 * `hint` 可空 —— 空就不出 `<span>`，免得空元素把行高撑起来。`name` 也可以空：段头
 * 那个「与全局不同」用的是 `data-sec`，多一个 name 会让表单绑定把它当普通字段读。
 */
export function switchRow(title, hint, name, checked) {
  return `<div class="switch-row">
    <div class="switch-row__text"><b>${esc(title)}</b>${hint ? `<span>${esc(hint)}</span>` : ''}</div>
    <label class="switch"><input type="checkbox" name="${esc(name)}"${checked ? ' checked' : ''}></label>
  </div>`;
}

export function switchInline(title, name, checked, tip = '', attrs = '') {
  return `<label class="switch-inline"${tip ? ` title="${esc(tip)}"` : ''}>
    ${title ? `<span class="switch-inline__label">${esc(title)}</span>` : ''}
    <span class="switch"><input type="checkbox"${name ? ` name="${esc(name)}"` : ''}${
      attrs ? ` ${attrs}` : ''}${checked ? ' checked' : ''}></span>
  </label>`;
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

/**
 * 「定位源文件」入口的说明文字。
 *
 * 任务列表和记录列表各有一排定位入口，说法得一样；搬走过的时候还要把落点写出来
 * —— 那正是用户点它想问的事。两个视图因此共用这一份，而不是各写一句。
 *
 * @param {{sourceMovedTo?: string}} item 带 sourceMovedTo 的任务或记录
 */
export function locateSourceHint(item) {
  const moved = item?.sourceMovedTo;
  return moved ? `定位源文件（已移动到 ${moved}）` : '定位源文件';
}

/**
 * 定位文件，并在它不在列表显示的那个位置时说出来。
 *
 * 源文件会被「已处理过的文件」搬到别处，所以「定位源文件」要试两个地方：列表
 * 上那条路径，和它被搬去的位置。落到第二个位置上的时候，用户看到的是资源管理
 * 器开在一个自己没点过的目录里 —— 不说一句，这就是个解释不通的结果。定位输出
 * 没有第二个位置，所以这句话只会为源文件出现。
 *
 * @param {Promise<{path: string, moved: boolean}>} promise locate 调用的返回值
 */
export async function locateAction(promise) {
  try {
    const r = await promise;
    if (r?.moved && r.path) toast(`源文件已移动，已定位到 ${r.path}`, 'info', 6000);
    return r;
  } catch (err) {
    toast(String(err?.message || err), 'error', 5200);
    return null;
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

/* ------------------------------------------------------------- list selection */

/**
 * 左侧列表的多选。和资源管理器同一套手势：单击 = 选中并进入编辑，Ctrl 加选/减选，
 * Shift 从上一次点的那一行连选，Ctrl+A 全选。
 *
 * **选中和"正在编辑的那一行"分开存。** Ctrl 点出来的一串里只有最后点的那一行是当前
 * 编辑的对象 —— 右侧面板显示它，右键菜单里的「复制」「删除」也作用在它身上。不分成
 * 两份的话，"复制"要么作用在一整串上（用户没说要那样），要么随机挑一个。
 *
 * 只管"哪些行被勾着"，不碰 DOM：两个列表（模板、过滤方案）行高、拖拽、搜索各不
 * 相同，判定却该是同一句。返回的 `click` 只回答一件事 —— **这一行要不要变成当前
 * 正在编辑的那一行**；调用方自己决定要不要重画右侧。
 *
 * @param {(ids: string[]) => void} onChange 勾选变了就喊一声（重画高亮用）
 */
export function createListSelection(onChange) {
  const picked = new Set();
  let anchor = '';
  let current = '';
  const fire = () => onChange?.([...picked]);

  return {
    /** 当前勾着的行（按点选的先后，不按列表顺序）。 */
    get ids() { return [...picked]; },
    /** 正在编辑的那一行。它**一定**在 `ids` 里。 */
    get current() { return current; },
    has: (id) => picked.has(id),

    /** 只勾这一行。新建、删完之后落到别的行上时用 —— 不是"多选"，是换了一个。 */
    only(id) {
      picked.clear();
      if (id) picked.add(id);
      current = id;
      anchor = id;
      fire();
    },

    /**
     * 处理一次点击。
     *
     * @param {string[]} order 列表里**当前可见**的行 id，Shift 的范围按它算
     * @param {string} id 点到的那一行
     * @param {MouseEvent} [ev]
     * @returns {boolean} 这一行是不是要变成"正在编辑"的那一行
     */
    click(order, id, ev) {
      if (ev?.shiftKey && anchor) {
        const a = order.indexOf(anchor);
        const b = order.indexOf(id);
        if (a >= 0 && b >= 0) {
          // Shift 自己就是"换成这一段"，不按就永远在原来那一串上追加；Ctrl+Shift
          // 才是往现有勾选里加一段。
          if (!ev.ctrlKey && !ev.metaKey) picked.clear();
          const [lo, hi] = a < b ? [a, b] : [b, a];
          for (let i = lo; i <= hi; i += 1) picked.add(order[i]);
          current = id;
          fire();
          return true;
        }
        // 锚点已经不在可见的列表里（搜过、或者刚被删掉）：退化成一次普通单击，
        // 而不是"从列表开头连到这一行"。
      }
      if (ev?.ctrlKey || ev?.metaKey) {
        if (picked.has(id)) picked.delete(id); else picked.add(id);
        current = id;
        anchor = id;
        fire();
        return true;
      }
      picked.clear();
      picked.add(id);
      current = id;
      anchor = id;
      fire();
      return true;
    },

    /** Ctrl+A。返回 false 表示"没接管这次按键"，让调用方决定要不要拦。 */
    selectAll(order) {
      if (!order.length) return false;
      picked.clear();
      for (const id of order) picked.add(id);
      current = current || order[0];
      anchor = current;
      fire();
      return true;
    },

    /**
     * 整张表变了之后（新建、删除、改名、搜索词变了）把已经不存在的行清掉。
     *
     * 不清的话，"复制"会拿着一串看不见的行去后端，而那些行要么报错要么被静默忽略 ——
     * 两种都比"少选了一项"更难解释。
     */
    prune(order) {
      const alive = new Set(order);
      let changed = false;
      for (const id of [...picked]) {
        if (!alive.has(id)) { picked.delete(id); changed = true; }
      }
      if (!alive.has(anchor)) anchor = current && alive.has(current) ? current : '';
      if (!alive.has(current)) current = [...picked][0] || '';
      if (changed) fire();
    },

    clear() { picked.clear(); current = ''; anchor = ''; fire(); },
  };
}

/* ------------------------------------------------------------------- paging */

/**
 * 每页条数的可选值。两张列表共用一份：它们的分页条长得一样，能选的条数也该一样。
 */
export const PAGE_SIZES = [50, 100, 200, 300];

/**
 * 分页条的标记。配 bindPager 用。
 *
 * 两张列表用的是同一个条，所以标记和行为放在一起：分开写就是两次把「最后一页
 * 越界」写歪的机会。
 */
export function pagerHtml() {
  return `
    <span class="hint" data-role="pager-range"></span>
    <div class="spacer"></div>
    <label class="pager__size">
      <span>每页</span>
      <select class="select" data-role="pager-size" title="每页显示多少条">
        ${PAGE_SIZES.map((n) => `<option value="${n}">${n} 条</option>`).join('')}
      </select>
    </label>
    <button class="btn btn--text btn--icon btn--sm" data-role="pager-prev" title="上一页">${icon('chevronLeft', 'sm')}</button>
    <span class="pager__page" data-role="pager-page"></span>
    <button class="btn btn--text btn--icon btn--sm" data-role="pager-next" title="下一页">${icon('chevronRight', 'sm')}</button>`;
}

/**
 * 驱动一条分页条。
 *
 * 页码留在这里而不是调用方：调用方每次重画都得先问一句「现在显示的是第几页」，
 * 问回来的页码已经按当前的候选总数钳过边，于是页码不可能比它数的那份列表活得久
 * （删到最后一页空了、筛选之后只剩两页，都会自己退回去）。
 *
 * @param {HTMLElement} host 装着 pagerHtml() 的那个元素
 * @param {{ onChange?: () => void }} [opts] 页码或每页条数被用户改动时调用
 */
export function bindPager(host, { onChange } = {}) {
  const state = { page: 1, pageSize: PAGE_SIZES[0], total: 0 };
  const sizeEl = host.querySelector('[data-role=pager-size]');
  const rangeEl = host.querySelector('[data-role=pager-range]');
  const pageEl = host.querySelector('[data-role=pager-page]');
  const prevEl = host.querySelector('[data-role=pager-prev]');
  const nextEl = host.querySelector('[data-role=pager-next]');
  sizeEl.value = String(state.pageSize);

  const lastPage = () => Math.max(1, Math.ceil(state.total / state.pageSize));

  function go(n) {
    const p = Math.min(lastPage(), Math.max(1, n));
    if (p === state.page) return;
    state.page = p;
    onChange?.();
  }

  sizeEl.addEventListener('change', () => {
    state.pageSize = Number(sizeEl.value) || PAGE_SIZES[0];
    // 换了每页条数，「第 5 页」指的是另一段内容了，回到开头最不容易看错。
    state.page = 1;
    onChange?.();
  });
  prevEl.addEventListener('click', () => go(state.page - 1));
  nextEl.addEventListener('click', () => go(state.page + 1));

  return {
    get page() { return state.page; },
    get pageSize() { return state.pageSize; },
    /** 回到第 1 页（筛选条件变了的时候用）。 */
    reset() { state.page = 1; },
    /**
     * 把分页条指向一份 total 条内容的列表，返回它现在显示的是第几页。
     */
    render(total) {
      state.total = Math.max(0, Number(total) || 0);
      const last = lastPage();
      if (state.page > last) state.page = last;
      if (state.page < 1) state.page = 1;
      const from = (state.page - 1) * state.pageSize;
      const to = Math.min(from + state.pageSize, state.total);
      rangeEl.textContent = state.total === 0 ? '0 条' : `${from + 1}–${to} / 共 ${state.total} 条`;
      pageEl.textContent = `第 ${state.page} / ${last} 页`;
      prevEl.disabled = state.page <= 1;
      nextEl.disabled = state.page >= last;
      return state.page;
    },
  };
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
