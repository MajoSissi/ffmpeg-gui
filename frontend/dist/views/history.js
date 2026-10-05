import { icon } from '../icons.js';
import {
  esc, humanSize, humanDuration, humanElapsed, resolution, bitrateText, dateText,
  toast, openModal, closeModal, confirmDialog, ratioText, ratioClass, num, shellAction,
} from '../ui.js';

const STATUS_ORDER = ['全部', '已完成', '完成(警告)', '失败', '已取消', '已跳过', '已排除'];

export function createHistoryView(ctx) {
  const el = document.createElement('section');
  el.className = 'page';
  el.innerHTML = `
    <div class="toolbar">
      <div class="input-group" style="max-width:300px">
        <input class="input" placeholder="搜索文件名 / 路径 / 模板 / 错误" data-role="search">
      </div>
      <select class="select" data-role="status" style="min-width:140px">
        ${STATUS_ORDER.map((s) => `<option value="${s === '全部' ? 'all' : s}">${s}</option>`).join('')}
      </select>
      <div class="toolbar__right">
        <span class="chip chip--muted" data-role="count">0 条记录</span>
        <div class="sep"></div>
        <button class="btn btn--tonal" data-act="export">${icon('download')}导出 CSV</button>
        <button class="btn btn--danger" data-act="clear">${icon('trash')}清空</button>
      </div>
    </div>
    <div class="table-wrap">
      <table class="grid-table">
        <thead>
          <tr>
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
        <h3>还没有处理记录</h3>
        <p>每完成一个文件都会记录处理前 / 处理后的格式、分辨率、大小、压缩比与耗时，可一键导出 CSV。</p>
      </div>
    </div>`;

  const rowsEl = el.querySelector('[data-role=rows]');
  const emptyEl = el.querySelector('[data-role=empty]');
  const countEl = el.querySelector('[data-role=count]');
  let query = { keyword: '', status: 'all' };

  function itemOf(r) {
    return ctx.state.history.find((x) => x.id === r.id) || r;
  }

  function render() {
    const rows = ctx.state.history;
    emptyEl.hidden = rows.length > 0;
    countEl.textContent = `${rows.length} 条记录`;
    rowsEl.innerHTML = rows.map((r) => {
      const b = r.before || {};
      const a = r.after || {};
      const changed = a.exists;
      return `<tr data-id="${esc(r.id)}">
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
          ${(r.output) ? `<button class="btn btn--text btn--icon btn--sm" data-act="reveal" data-path="${esc(r.output)}" title="显示输出文件">${icon('external', 'sm')}</button>` : ''}
          <button class="btn btn--text btn--icon btn--sm" data-act="detail" data-id="${esc(r.id)}" title="查看详情">${icon('info', 'sm')}</button>
        </td>
      </tr>`;
    }).join('');
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

  async function reload() {
    try {
      const page = await ctx.api.history({ ...query, limit: 1000 });
      ctx.state.history = page.items || [];
      render();
    } catch (err) {
      toast(err.message || String(err), 'error');
    }
  }

  el.addEventListener('click', async (e) => {
    const btn = e.target.closest('[data-act]');
    if (btn) {
      if (btn.dataset.act === 'reveal') { await shellAction(ctx.api.revealPath(btn.dataset.path)); return; }
      if (btn.dataset.act === 'detail') { showDetail(itemOf({ id: btn.dataset.id })); return; }
      if (btn.dataset.act === 'export') {
        try {
          const p = await ctx.api.exportHistoryCSV(query);
          if (p) toast(`已导出 ${ctx.state.history.length} 条记录到 ${p}`, 'success', 5000);
        } catch (err) { toast(err.message || String(err), 'error'); }
        return;
      }
      if (btn.dataset.act === 'clear') {
        if (!(await confirmDialog('清空处理记录', '所有历史记录将被删除，已导出的 CSV 不受影响。', '清空', true))) return;
        await ctx.api.clearHistory();
        ctx.state.history = [];
        render();
        toast('记录已清空', 'success');
        return;
      }
    }
    const row = e.target.closest('tr[data-id]');
    if (row) showDetail(itemOf({ id: row.dataset.id }));
  });

  el.querySelector('[data-role=search]').addEventListener('input', (e) => {
    query.keyword = e.target.value.trim();
    reload();
  });
  el.querySelector('[data-role=status]').addEventListener('change', (e) => {
    query.status = e.target.value;
    reload();
  });

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
        ${r.output ? `<button class="btn btn--tonal" data-reveal>${icon('external')}显示输出文件</button>` : ''}
        <button class="btn btn--text" data-close4>关闭</button>`,
    });
    modal.querySelector('[data-close4]').addEventListener('click', closeModal);
    modal.querySelector('[data-copy]')?.addEventListener('click', async () => {
      toast((await copyTextSafe(r.command)) ? '命令已复制' : '复制失败', 'success');
    });
    modal.querySelector('[data-reveal]')?.addEventListener('click', () => shellAction(ctx.api.revealPath(r.output)));
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
