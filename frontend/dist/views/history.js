import { icon } from '../icons.js';
import {
  esc, humanSize, humanDuration, humanElapsed, resolution, bitrateText, dateText,
  toast, openModal, closeModal, confirmDialog, ratioText, ratioClass, num,
  pagerHtml, bindPager, locateAction, locateSourceHint,
} from '../ui.js';

const STATUS_ORDER = ['全部', '已完成', '完成(警告)', '失败', '已取消', '已跳过', '已排除'];

const SORTS = [
  { value: 'newest', label: '最新在前' },
  { value: 'oldest', label: '最早在前' },
];

export function createHistoryView(ctx) {
  const el = document.createElement('section');
  el.className = 'page';
  el.innerHTML = `
    <div class="toolbar">
      <div class="input-group" style="max-width:280px">
        <input class="input" placeholder="搜索文件名 / 路径 / 模板 / 错误" data-role="search">
      </div>
      <select class="select" data-role="status" style="min-width:140px">
        ${STATUS_ORDER.map((s) => `<option value="${s === '全部' ? 'all' : s}">${s}</option>`).join('')}
      </select>
      <div class="input-group">
        <input class="input" type="date" data-role="from" title="起始日期" style="width:148px">
        <span class="hint">至</span>
        <input class="input" type="date" data-role="to" title="结束日期" style="width:148px">
        <button class="btn btn--text btn--icon btn--sm" data-act="reset-filter" title="清除筛选条件">${icon('close', 'sm')}</button>
      </div>
      <select class="select" data-role="sort" style="min-width:136px">
        ${SORTS.map((s) => `<option value="${s.value}">${s.label}</option>`).join('')}
      </select>
      <div class="toolbar__right">
        <span class="chip chip--muted" data-role="count">0 条记录</span>
        <div class="sep"></div>
        <button class="btn btn--tonal" data-act="export">${icon('download')}导出 CSV</button>
        <button class="btn btn--danger" data-act="clear">${icon('trash')}清空</button>
      </div>
    </div>
    <div class="bulkbar" data-role="bulkbar" hidden>
      <b data-role="bulk-label">已选 0 项</b>
      <div class="spacer"></div>
      <button class="btn btn--text btn--sm" data-act="select-all">全选</button>
      <button class="btn btn--text btn--sm" data-act="select-none">取消选择</button>
      <button class="btn btn--tonal btn--sm" data-act="bulk-remove">${icon('trash', 'sm')}移除所选</button>
    </div>
    <div class="table-wrap">
      <table class="grid-table">
        <thead>
          <tr>
            <th class="col-check"><label class="check"><input type="checkbox" data-role="check-all"></label></th>
            <th>文件</th>
            <th>处理前</th>
            <th>处理后</th>
            <th>压缩比</th>
            <th>耗时</th>
            <th>模板</th>
            <th>状态</th>
            <th>完成时间</th>
            <th class="actions"></th>
          </tr>
        </thead>
        <tbody data-role="rows"></tbody>
      </table>
      <div class="empty" data-role="empty">
        ${icon('history')}
        <h3 data-role="empty-title">还没有处理记录</h3>
        <p data-role="empty-text">每完成一个文件都会记录处理前 / 处理后的格式、分辨率、大小、压缩比与耗时，可一键导出 CSV。</p>
      </div>
    </div>
    <div class="pager" data-role="pager">${pagerHtml()}</div>`;

  const rowsEl = el.querySelector('[data-role=rows]');
  const emptyEl = el.querySelector('[data-role=empty]');
  const countEl = el.querySelector('[data-role=count]');
  const bulkbarEl = el.querySelector('[data-role=bulkbar]');
  const searchEl = el.querySelector('[data-role=search]');
  const statusEl = el.querySelector('[data-role=status]');
  const fromEl = el.querySelector('[data-role=from]');
  const toEl = el.querySelector('[data-role=to]');
  const sortEl = el.querySelector('[data-role=sort]');

  // 送给后端的筛选条件。两个日期是空字符串表示这一端不设边界。
  const query = { keyword: '', status: 'all', from: '', to: '', sort: 'newest' };

  const local = {
    checked: new Set(),
    // 当前这一页的行。ctx.state.history 装的也是这一页。
    renderedIds: [],
    total: 0,
  };

  const pager = bindPager(el.querySelector('[data-role=pager]'), { onChange: () => reload() });

  function itemOf(r) {
    return ctx.state.history.find((x) => x.id === r.id) || r;
  }

  /* --------------------------------------------------------------- loading */

  /**
   * 取一页记录。
   *
   * 分页在后端做：记录可以存到两万条，全拉过来再切片只是把卡顿从 DOM 挪到了
   * 传输和解析上 —— 表格里的行少了，几次序列化一条不少。
   *
   * 票号是给快速连点用的：两次请求先发后到的时候，旧的那一份不能盖掉新的。
   */
  let ticket = 0;
  let searchTimer = 0;

  async function reload() {
    const t = ++ticket;
    try {
      const page = await ctx.api.history({
        ...query,
        offset: (pager.page - 1) * pager.pageSize,
        limit: pager.pageSize,
      });
      if (t !== ticket) return; // 更新的一次已经赢了
      const asked = pager.page;
      ctx.state.history = page.items || [];
      local.total = page.total || 0;
      // 在最后一页上删完、清空之后，页码会越过末尾：退到还存在的最后一页再取。
      if (pager.render(local.total) !== asked) return reload();
      render();
    } catch (err) {
      if (t !== ticket) return;
      toast(err.message || String(err), 'error');
    }
  }

  /** 筛选条件变了：回到第 1 页，扔下勾选，重新取数。 */
  function refilter(delay = 0) {
    pager.reset();
    local.checked.clear();
    clearTimeout(searchTimer);
    if (delay > 0) searchTimer = setTimeout(reload, delay);
    else reload();
  }

  /* -------------------------------------------------------------- rendering */

  function render() {
    const rows = ctx.state.history;
    local.renderedIds = rows.map((r) => r.id);
    // 换页、换筛选之后，看不见的行不能继续留在勾选集里：批量移除会照着它动手。
    const alive = new Set(local.renderedIds);
    for (const id of [...local.checked]) if (!alive.has(id)) local.checked.delete(id);

    emptyEl.hidden = rows.length > 0;
    if (!rows.length) paintEmptyState();
    countEl.textContent = local.total > 0 ? `共 ${local.total} 条记录` : '0 条记录';
    rowsEl.innerHTML = rows.map(rowHtml).join('');
    paintSelection();
  }

  /** 「没有记录」和「没有符合条件的记录」是两件事。 */
  function paintEmptyState() {
    const filtered = query.keyword || query.status !== 'all' || query.from || query.to;
    emptyEl.querySelector('[data-role=empty-title]').textContent
      = filtered ? '没有符合这些条件的记录' : '还没有处理记录';
    emptyEl.querySelector('[data-role=empty-text]').textContent = filtered
      ? '记录是有的，只是都不满足当前的搜索、状态或日期区间。清除筛选条件就能看到全部。'
      : '每完成一个文件都会记录处理前 / 处理后的格式、分辨率、大小、压缩比与耗时，可一键导出 CSV。';
  }

  function rowHtml(r) {
    const b = r.before || {};
    const a = r.after || {};
    const changed = a.exists;
    const checked = local.checked.has(r.id);
    return `<tr data-id="${esc(r.id)}"${checked ? ' class="is-checked"' : ''}>
      <td class="col-check"><label class="check"><input type="checkbox"${checked ? ' checked' : ''}></label></td>
      <td class="name">
        <div class="nowrap" title="${esc(r.input)}">${esc(r.input.split(/[\\/]/).pop())}</div>
        <div class="cell-sub nowrap" title="${esc(r.note || r.error || '')}">${esc(shorten(r.note || r.error || r.output))}</div>
      </td>
      <td class="num">${esc(resolution(b.width, b.height))}<div class="cell-sub">${esc(humanSize(b.size))}${b.videoCodec ? ` · ${esc(b.videoCodec)}` : ''}</div></td>
      <td class="num">${changed ? `${esc(resolution(a.width, a.height))}<div class="cell-sub">${esc(humanSize(a.size))}${a.videoCodec ? ` · ${esc(a.videoCodec)}` : ''}</div>` : '<span style="color:var(--on-surface-dim)">—</span>'}</td>
      <td class="num ${ratioClass(b, a)}">${changed ? esc(ratioText(b, a)) : '—'}</td>
      <td class="num">${esc(humanElapsed(r.elapsedMs))}</td>
      <td>${esc(r.templateName || '—')}</td>
      <td>${statusChipFor(r.status)}</td>
      <td class="num">${esc(dateText(r.endedAt || r.startedAt))}</td>
      <td class="actions">
        <button class="btn btn--text btn--icon btn--sm" data-act="locate-source"
          data-path="${esc(r.input)}" data-fallback="${esc(r.sourceMovedTo || '')}"
          title="${esc(locateSourceHint(r))}" ${(r.input || r.sourceMovedTo) ? '' : 'disabled'}>${icon('folderOpen', 'sm')}</button>
        <button class="btn btn--text btn--icon btn--sm" data-act="locate-output" data-path="${esc(r.output)}"
          title="${r.output ? '定位输出文件' : '这条记录没有输出文件'}" ${r.output ? '' : 'disabled'}>${icon('external', 'sm')}</button>
        <button class="btn btn--text btn--icon btn--sm" data-act="detail" data-id="${esc(r.id)}" title="查看详情">${icon('info', 'sm')}</button>
      </td>
    </tr>`;
  }

  /** 勾选、焦点、批量条 —— 表头那个「全选」永远只指这一页。 */
  function paintSelection() {
    rowsEl.querySelectorAll('tr[data-id]').forEach((tr) => {
      const on = local.checked.has(tr.dataset.id);
      tr.classList.toggle('is-checked', on);
      const cb = tr.querySelector('input[type=checkbox]');
      if (cb) cb.checked = on;
    });
    const all = el.querySelector('[data-role=check-all]');
    all.checked = local.renderedIds.length > 0 && local.checked.size === local.renderedIds.length;
    const n = local.checked.size;
    bulkbarEl.hidden = n === 0;
    if (n > 0) el.querySelector('[data-role=bulk-label]').textContent = `已选 ${n} 项`;
  }

  function shorten(s) {
    s = String(s || '');
    return s.length > 70 ? `${s.slice(0, 69)}…` : s;
  }

  function statusChipFor(label) {
    const map = {
      已完成: 'chip--ok', '完成(警告)': 'chip--warn', 失败: 'chip--err',
      已取消: 'chip--muted', 已跳过: 'chip--muted', 已排除: 'chip--muted',
    };
    return `<span class="chip ${map[label] || 'chip--muted'}">${esc(label)}</span>`;
  }

  /* -------------------------------------------------------------- actions */

  /**
   * 移除选中的记录。
   *
   * 走确认框，并且说清楚动的是记录不是文件：记录一没，「这个文件被处理过、
   * 产物在哪」就再也查不到了，而产物本身还躺在磁盘上。
   */
  async function removeChecked() {
    const ids = [...local.checked];
    if (!ids.length) return;
    const ok = await confirmDialog('移除处理记录',
      `将从记录中删掉 ${ids.length} 条。输出文件不受影响，但删掉的记录无法恢复。`,
      '移除', true);
    if (!ok) return;
    try {
      const n = await ctx.api.deleteRecords(ids);
      local.checked.clear();
      await reload();
      toast(n > 0 ? `已移除 ${n} 条记录` : '所选记录已经不在列表里了', 'success');
    } catch (err) {
      toast(err.message || String(err), 'error');
    }
  }

  el.addEventListener('click', async (e) => {
    const btn = e.target.closest('[data-act]');
    if (btn) {
      const act = btn.dataset.act;
      if (act === 'locate-source') {
        await locateAction(ctx.api.locate(btn.dataset.path || '', btn.dataset.fallback || ''));
        return;
      }
      if (act === 'locate-output') {
        await locateAction(ctx.api.locate(btn.dataset.path || '', ''));
        return;
      }
      if (act === 'detail') { showDetail(itemOf({ id: btn.dataset.id })); return; }
      if (act === 'select-all') { local.checked = new Set(local.renderedIds); paintSelection(); return; }
      if (act === 'select-none') { local.checked.clear(); paintSelection(); return; }
      if (act === 'bulk-remove') { await removeChecked(); return; }
      if (act === 'reset-filter') {
        Object.assign(query, { keyword: '', status: 'all', from: '', to: '' });
        searchEl.value = '';
        statusEl.value = 'all';
        fromEl.value = '';
        toEl.value = '';
        refilter();
        return;
      }
      if (act === 'export') {
        try {
          const p = await ctx.api.exportHistoryCSV(query);
          if (p) toast(`已导出 ${local.total} 条记录到 ${p}`, 'success', 5000);
        } catch (err) { toast(err.message || String(err), 'error'); }
        return;
      }
      if (act === 'clear') {
        if (!(await confirmDialog('清空处理记录', '所有历史记录将被删除，已导出的 CSV 不受影响。', '清空', true))) return;
        local.checked.clear();
        await ctx.api.clearHistory();
        await reload();
        toast('记录已清空', 'success');
        return;
      }
    }
    // 行的空白处打开详情。复选框和按钮各有各的事，点它们不该顺带弹出详情。
    const row = e.target.closest('tr[data-id]');
    if (row && !e.target.closest('input,button,a,label')) showDetail(itemOf({ id: row.dataset.id }));
  });

  el.addEventListener('change', (e) => {
    if (e.target.matches('[data-role=check-all]')) {
      local.checked = e.target.checked ? new Set(local.renderedIds) : new Set();
      paintSelection();
      return;
    }
    if (e.target.matches('tr[data-id] input[type=checkbox]')) {
      const id = e.target.closest('tr').dataset.id;
      if (e.target.checked) local.checked.add(id); else local.checked.delete(id);
      paintSelection();
    }
  });

  // 搜索是每敲一个字都改条件：打字期间攒一下再发，免得一个词发五次请求。
  searchEl.addEventListener('input', () => {
    query.keyword = searchEl.value.trim();
    refilter(220);
  });
  statusEl.addEventListener('change', () => {
    query.status = statusEl.value;
    refilter();
  });
  fromEl.addEventListener('change', () => {
    query.from = fromEl.value;
    refilter();
  });
  toEl.addEventListener('change', () => {
    query.to = toEl.value;
    refilter();
  });
  sortEl.addEventListener('change', () => {
    query.sort = sortEl.value;
    refilter();
  });

  /* --------------------------------------------------------------- detail */

  function showDetail(r) {
    if (!r) return;
    const b = r.before || {};
    const a = r.after || {};
    const body = `
      <div style="padding:16px 16px 0">
        <div class="row row--wrap" style="gap:8px;margin-bottom:14px">
          ${statusChipFor(r.status)}
          <span class="chip chip--muted">${icon('layers')}${esc(r.templateName || '—')}</span>
          <span class="chip chip--muted">${icon('schedule')}${esc(humanElapsed(r.elapsedMs))}</span>
          ${r.speed ? `<span class="chip chip--muted">${icon('speed')}${esc(num(r.speed, 2))}x</span>` : ''}
          ${a.size ? `<span class="chip ${(a.size < b.size) ? 'chip--ok' : 'chip--warn'}">压缩比 ${esc(ratioText(b, a))}</span>` : ''}
        </div>
        <div class="compare" style="margin-bottom:16px">
          <div class="detail-card" style="margin:0">
            <h4>处理前</h4>
            ${kv(b, false)}
          </div>
          <div class="compare__arrow">${icon('chevronRight', 'lg')}</div>
          <div class="detail-card" style="margin:0">
            <h4>处理后</h4>
            ${a.exists ? kv(a, true) : '<div class="hint">没有产出文件</div>'}
          </div>
        </div>
        ${r.error ? `<div class="detail-card" style="margin-bottom:12px"><h4>错误</h4><div style="color:var(--err);word-break:break-all;font-family:var(--mono);font-size:11.5px">${esc(r.error)}</div></div>` : ''}
        ${(r.warnings || []).length ? `<div class="detail-card" style="margin-bottom:12px"><h4>警告</h4>${r.warnings.map((w) => `<div style="color:var(--warn);margin-bottom:4px;word-break:break-all">${esc(w)}</div>`).join('')}</div>` : ''}
        ${r.note ? `<div class="detail-card" style="margin-bottom:12px"><h4>备注</h4><div style="word-break:break-all">${esc(r.note)}</div></div>` : ''}
        <div class="detail-card" style="margin-bottom:16px">
          <h4>路径</h4>
          <dl class="kv">
            <dt>源文件</dt><dd>${esc(r.input)}</dd>
            ${r.sourceMovedTo ? `<dt>已移动</dt><dd>${esc(r.sourceMovedTo)}</dd>` : ''}
            <dt>输出</dt><dd>${esc(r.output || '—')}</dd>
            <dt>开始</dt><dd>${esc(dateText(r.startedAt))}</dd>
            <dt>结束</dt><dd>${esc(dateText(r.endedAt))}</dd>
          </dl>
        </div>
        ${r.command ? `<div class="card" style="overflow:hidden;margin-bottom:16px">
          <div class="row" style="padding:9px 12px;border-bottom:1px solid var(--outline)">
            <b style="font-size:12px">执行的命令</b><div class="spacer"></div>
            <button class="btn btn--text btn--sm" data-copy>${icon('copy', 'sm')}复制</button>
          </div>
          <pre class="code-block" style="max-height:220px">${esc(r.command)}</pre>
        </div>` : ''}
      </div>`;
    const { modal } = openModal({
      title: '处理详情',
      size: 'modal--wide',
      body,
      footer: `<span class="hint">${esc(r.input.split(/[\\/]/).pop())}</span><div class="spacer"></div>
        <button class="btn btn--tonal" data-locate-source title="${esc(locateSourceHint(r))}">${icon('folderOpen')}定位源文件</button>
        ${r.output ? `<button class="btn btn--tonal" data-locate-output title="定位输出文件">${icon('external')}定位输出文件</button>` : ''}
        <button class="btn btn--text" data-close4>关闭</button>`,
    });
    modal.querySelector('[data-close4]').addEventListener('click', closeModal);
    modal.querySelector('[data-copy]')?.addEventListener('click', async () => {
      toast((await copyTextSafe(r.command)) ? '命令已复制' : '复制失败', 'success');
    });
    modal.querySelector('[data-locate-source]')?.addEventListener('click',
      () => locateAction(ctx.api.locate(r.input, r.sourceMovedTo)));
    modal.querySelector('[data-locate-output]')?.addEventListener('click',
      () => locateAction(ctx.api.locate(r.output, '')));
  }

  function kv(m, isAfter) {
    return `<dl class="kv">
      <dt>格式</dt><dd>${esc(m.container || '—')}</dd>
      <dt>分辨率</dt><dd>${esc(resolution(m.width, m.height))}</dd>
      <dt>视频</dt><dd>${esc(m.videoCodec || '—')}${m.fps ? ` @ ${num(m.fps, 2)}` : ''}${m.pixFmt ? ` · ${esc(m.pixFmt)}` : ''}</dd>
      <dt>音频</dt><dd>${esc(m.audioCodec || '—')}${m.channels ? ` · ${m.channels}ch` : ''}${m.sampleRate ? ` · ${m.sampleRate}Hz` : ''}</dd>
      <dt>时长</dt><dd>${esc(humanDuration(m.duration))}</dd>
      <dt>大小</dt><dd>${esc(humanSize(m.size))}</dd>
      <dt>码率</dt><dd>${esc(bitrateText(m.bitRate))}</dd>
    </dl>`;
  }

  async function copyTextSafe(t) {
    try { await navigator.clipboard.writeText(t); return true; } catch { return false; }
  }

  return {
    el,
    mount() { reload(); },
    onTemplatesChanged() {},
    onJobsChanged() {},
    onJobUpdate() {},
    onStats() {},
    onLog() {},
    onSelectJob() {},
    onRecord() { reload(); },
    destroy() {},
  };
}
