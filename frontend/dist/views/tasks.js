import { icon } from '../icons.js';
import {
  esc, humanSize, humanDuration, humanElapsed, resolution, pct, statusChip, statusMeta,
  toast, openModal, closeModal, confirmDialog, copyText, dateText, num, bitrateText,
  shellAction, commandHtml, field, selectHtml,
} from '../ui.js';
import { effective } from './sections.js';

export function createTasksView(ctx) {
  const el = document.createElement('section');
  el.className = 'page';
  el.innerHTML = `
    <div class="toolbar">
      <button class="btn btn--tonal" data-act="add-files">${icon('add')}添加文件</button>
      <button class="btn btn--tonal" data-act="add-folder">${icon('folderOpen')}添加文件夹</button>
      <div class="sep"></div>
      <select class="select" data-role="template" title="处理模板"></select>
      <button class="btn btn--outline" data-act="preview-command">${icon('terminal')}命令预览</button>
      <button class="btn btn--outline" data-act="toggle-filter" data-role="filter-btn"
        title="只对当前队列生效，优先于模板里的筛选条件">${icon('filter')}<span data-role="filter-btn-text">筛选条件</span></button>
      <div class="sep"></div>
      <button class="btn btn--filled" data-act="start">${icon('play')}开始</button>
      <button class="btn" data-act="pause">${icon('pause')}暂停</button>
      <button class="btn" data-act="stop">${icon('stop')}停止</button>
      <div class="toolbar__right">
        <span class="chip chip--muted" data-role="stat-total">0 个任务</span>
        <span class="chip chip--ok" data-role="stat-done" hidden></span>
        <span class="chip chip--err" data-role="stat-failed" hidden></span>
        <span class="chip chip--warn" data-role="stat-filter">${icon('filter')}<span data-role="filter-text">未启用过滤</span></span>
        <div class="sep"></div>
        <button class="btn btn--text btn--icon" data-act="retry" title="重试失败的任务">${icon('refresh')}</button>
        <button class="btn btn--text btn--icon" data-act="clear-finished" title="清理已完成">${icon('trash')}</button>
      </div>
    </div>

    <div class="qfilter" data-role="qfilter" hidden>
      <div class="qfilter__head">
        <b>${icon('filter', 'sm')}本批次的筛选条件</b>
        <span class="hint">优先于模板与全局模板里的筛选，只影响当前队列</span>
        <div class="spacer"></div>
        <button class="btn btn--text btn--sm" data-act="qfilter-clear">${icon('close', 'sm')}不用筛选</button>
      </div>
      <div class="grid grid--4" data-role="qfilter-fields"></div>
    </div>

    <div class="bulkbar" data-role="bulkbar" hidden>
      <b data-role="bulk-label">已选 0 项</b>
      <div class="spacer"></div>
      <button class="btn btn--text btn--sm" data-act="select-all">全选</button>
      <button class="btn btn--text btn--sm" data-act="select-none">取消选择</button>
      <button class="btn btn--tonal btn--sm" data-act="bulk-remove">${icon('trash', 'sm')}移除所选</button>
    </div>

    <div class="table-wrap" data-role="table-wrap">
      <table class="grid-table">
        <thead>
          <tr>
            <th class="col-check"><label class="check"><input type="checkbox" data-role="check-all"></label></th>
            <th>文件</th>
            <th>分辨率</th>
            <th>时长</th>
            <th>大小</th>
            <th>状态</th>
            <th style="width:190px">进度</th>
            <th class="actions">操作</th>
          </tr>
        </thead>
        <tbody data-role="rows"></tbody>
      </table>
      <div class="empty" data-role="empty">
        ${icon('movie')}
        <h3>队列里还没有文件</h3>
        <p>点击「添加文件」或「添加文件夹」，也可以把文件直接拖进窗口。支持多选文件与多选目录。</p>
      </div>
    </div>
    <div class="panel" data-role="panel">
      <div class="panel__grip" data-role="grip"></div>
      <div class="panel__head">
        <button class="tab is-active" data-tab="log">${icon('terminal')}输出日志</button>
        <button class="tab" data-tab="running">${icon('gauge')}处理详情</button>
        <button class="tab" data-tab="command">${icon('chevronRight')}命令</button>
        <div class="spacer"></div>
        <span class="hint" data-role="panel-hint"></span>
        <button class="btn btn--text btn--icon btn--sm" data-act="panel-toggle" title="折叠/展开">${icon('chevronDown')}</button>
      </div>
      <div class="panel__body">
        <pre class="log-view" data-role="log"></pre>
        <div class="detail-grid" data-role="details" hidden></div>
        <div class="panel__body" data-role="command" hidden style="overflow:auto">
          <div class="code-block" data-role="command-text" style="padding:0"></div>
        </div>
      </div>
    </div>`;

  const rowsEl = el.querySelector('[data-role=rows]');
  const emptyEl = el.querySelector('[data-role=empty]');
  const tableWrap = el.querySelector('[data-role=table-wrap]');
  const logEl = el.querySelector('[data-role=log]');
  const detailsEl = el.querySelector('[data-role=details]');
  const commandEl = el.querySelector('[data-role=command]');
  const commandTextEl = el.querySelector('[data-role=command-text]');
  const panelEl = el.querySelector('[data-role=panel]');
  const gripEl = el.querySelector('[data-role=grip]');
  const tplSelect = el.querySelector('[data-role=template]');
  const bulkbarEl = el.querySelector('[data-role=bulkbar]');
  const qfilterEl = el.querySelector('[data-role=qfilter]');
  const qfilterFields = el.querySelector('[data-role=qfilter-fields]');

  const local = {
    tab: 'log',
    checked: new Set(),
    renderedIds: [],
    followLog: true,
    collapsed: false,
    // Anchor for shift-click range selection: the last row picked without a modifier.
    anchorId: '',
    // The queue filter panel is its own working copy; it edits live on input so the
    // chip above the table keeps telling the truth about what will run.
    qfilter: null,
  };

  /* ------------------------------------------------------------ template */

  const opts = () => ctx.state.options || {};

  /** The global template holds defaults only; it can never process a file. */
  function globalTemplate() {
    return ctx.state.templates.find((t) => t.global) || {};
  }

  /** The template the queue is bound to, merged with the global defaults. */
  function currentTemplate() {
    const all = ctx.state.templates;
    const t = all.find((x) => x.id === ctx.state.currentTemplateId && !x.global)
      || all.find((x) => !x.global);
    return effective(t, globalTemplate());
  }

  function syncTemplateOptions() {
    const tpls = ctx.state.templates.filter((t) => !t.global);
    tplSelect.innerHTML = tpls
      .map((t) => `<option value="${esc(t.id)}"${t.id === ctx.state.currentTemplateId ? ' selected' : ''}>${esc(t.name)}</option>`)
      .join('');
    // A selection that pointed at the global template (or a deleted one) has to
    // fall back to something processable, or the queue would have no template.
    if (!tpls.some((t) => t.id === ctx.state.currentTemplateId)) {
      ctx.state.currentTemplateId = tpls[0]?.id || '';
    }
    renderToolbar();
  }

  tplSelect.addEventListener('change', async () => {
    ctx.state.currentTemplateId = tplSelect.value;
    const s = { ...ctx.state.settings, lastTemplateId: tplSelect.value };
    ctx.state.settings = s;
    await ctx.api.saveSettings(s);
    const n = await ctx.api.setAllTemplates(tplSelect.value);
    if (n > 0) toast(`已对 ${n} 个排队任务应用新模板`, 'success');
    else toast('模板已切换', 'info', 1600);
  });

  /* -------------------------------------------------------------- actions */

  el.addEventListener('click', async (e) => {
    const btn = e.target.closest('[data-act]');
    if (btn) {
      const act = btn.dataset.act;
      if (act === 'add-files') {
        const r = await ctx.api.addFilesDialog(ctx.state.recursive);
        reportAdd(r);
      } else if (act === 'add-folder') {
        const r = await ctx.api.addFolderDialog(ctx.state.recursive);
        reportAdd(r);
      } else if (act === 'start') {
        ctx.state.autoStarted = true;
        await ctx.api.startQueue();
        toast('开始处理队列', 'success', 1800);
      } else if (act === 'pause') {
        const paused = await ctx.api.togglePause();
        toast(paused ? '队列已暂停（当前任务继续完成）' : '队列已继续', 'info', 2200);
      } else if (act === 'stop') {
        if (await confirmDialog('停止处理', '将取消正在运行的任务，并把排队中的任务标记为已取消。', '停止', true)) {
          await ctx.api.stopQueue();
          toast('已停止', 'warning');
        }
      } else if (act === 'retry') {
        const n = await ctx.api.retryFailed();
        toast(n > 0 ? `已重新排队 ${n} 个任务` : '没有需要重试的任务', n > 0 ? 'success' : 'info');
      } else if (act === 'clear-finished') {
        const n = await ctx.api.removeFinished();
        if (n > 0) await refreshJobs();
        toast(n > 0 ? `已清理 ${n} 个任务` : '没有可清理的任务', 'info');
      } else if (act === 'toggle-filter') {
        toggleQueueFilter();
      } else if (act === 'qfilter-clear') {
        // "Not filtering" is a distinct state from "filtering with no rules": the
        // first hands the decision back to the template, the second would silently
        // drop the template's own rules.
        local.qfilter = null;
        await pushQueueFilter();
        renderQueueFilter();
      } else if (act === 'bulk-remove') {
        await removeChecked();
      } else if (act === 'select-all') {
        local.checked = new Set(ctx.state.jobs.map((j) => j.id));
        paintSelection();
      } else if (act === 'select-none') {
        local.checked.clear();
        paintSelection();
      } else if (act === 'preview-command') {
        await showCommandPreview();
      } else if (act === 'panel-toggle') {
        local.collapsed = !local.collapsed;
        applyPanel();
      } else if (act === 'reveal') {
        await shellAction(ctx.api.revealPath(btn.dataset.path));
      } else if (act === 'remove') {
        await ctx.api.removeJob(btn.dataset.id);
        if (ctx.state.selectedJobId === btn.dataset.id) ctx.state.selectedJobId = '';
        local.checked.delete(btn.dataset.id);
        await refreshJobs();
      } else if (act === 'play-one') {
        ctx.state.currentTemplateId = tplSelect.value;
        await ctx.api.startQueue();
        toast('开始处理队列', 'success', 1800);
      }
      return;
    }

    const tab = e.target.closest('[data-tab]');
    if (tab) {
      local.tab = tab.dataset.tab;
      el.querySelectorAll('[data-tab]').forEach((t) => t.classList.toggle('is-active', t.dataset.tab === local.tab));
      applyTab();
      if (local.tab === 'command') await refreshCommand();
      return;
    }

    const row = e.target.closest('tr[data-id]');
    if (row && !e.target.closest('input,button,a')) {
      rowPicked(row.dataset.id, e);
      if (local.tab === 'command') await refreshCommand();
      else renderDetails();
    }
  });

  /**
   * Explorer-style row picking. A plain click focuses one row and makes it the whole
   * selection; Ctrl toggles a row in place; Shift takes everything from the anchor to
   * here. The checkbox column mirrors the same set, so there is exactly one selection
   * and the bulk bar always describes it truthfully.
   *
   * @param {string} id job id of the clicked row
   * @param {MouseEvent} e
   */
  function rowPicked(id, e) {
    const ids = local.renderedIds;
    if (e.ctrlKey || e.metaKey) {
      if (local.checked.has(id)) local.checked.delete(id); else local.checked.add(id);
      ctx.state.selectedJobId = id;
      local.anchorId = id;
    } else if (e.shiftKey && local.anchorId && ids.includes(local.anchorId)) {
      const a = ids.indexOf(local.anchorId);
      const b = ids.indexOf(id);
      local.checked = new Set(ids.slice(Math.min(a, b), Math.max(a, b) + 1));
      ctx.state.selectedJobId = id;
    } else {
      local.checked = new Set([id]);
      ctx.state.selectedJobId = id;
      local.anchorId = id;
    }
    paintSelection();
  }

  el.addEventListener('change', (e) => {
    if (e.target.matches('[data-role=check-all]')) {
      local.checked = e.target.checked ? new Set(ctx.state.jobs.map((j) => j.id)) : new Set();
      paintSelection();
      return;
    }
    if (e.target.matches('tr[data-id] input[type=checkbox]')) {
      const id = e.target.closest('tr').dataset.id;
      if (e.target.checked) local.checked.add(id); else local.checked.delete(id);
      ctx.state.selectedJobId = id;
      local.anchorId = id;
      paintSelection();
    }
  });

  // The queue filter edits live: every keystroke updates the backend so the chip and
  // the command preview describe the rules that will actually run, not the ones that
  // were there when the panel opened.
  qfilterFields.addEventListener('input', async () => {
    local.qfilter = readQueueFilter();
    await pushQueueFilter();
  });
  qfilterFields.addEventListener('change', async () => {
    local.qfilter = readQueueFilter();
    await pushQueueFilter();
  });

  /**
   * Re-pull the queue from the backend and repaint. Removals never arrive as a
   * `job:update` -- the backend only broadcasts stats for them -- so without this
   * the deleted rows would sit on screen until some other event happened to repaint.
   */
  async function refreshJobs() {
    ctx.state.jobs = (await ctx.api.jobs()) || [];
    renderJobs();
    paintSelection();
  }

  async function removeChecked() {
    const ids = [...local.checked];
    if (!ids.length) return;
    const running = ctx.state.jobs.filter((j) => local.checked.has(j.id)
      && ['running', 'preparing'].includes(j.status)).length;
    const msg = running
      ? `将移除 ${ids.length} 个任务，其中 ${running} 个正在处理（会被取消）。`
      : `将移除 ${ids.length} 个任务。已生成的文件不受影响。`;
    if (!(await confirmDialog('移除所选任务', msg, '移除', true))) return;
    const n = await ctx.api.removeJobs(ids);
    local.checked.clear();
    ctx.state.selectedJobId = '';
    await refreshJobs();
    toast(n > 0 ? `已移除 ${n} 个任务` : '所选任务已被移除', 'success');
  }

  // double-click a row -> open the file
  rowsEl.addEventListener('dblclick', async (e) => {
    const row = e.target.closest('tr[data-id]');
    if (!row) return;
    const job = ctx.state.jobs.find((j) => j.id === row.dataset.id);
    if (!job) return;
    if (job.output && job.status === 'done') await shellAction(ctx.api.revealPath(job.output));
    else await shellAction(ctx.api.revealPath(job.input));
  });

  function reportAdd(r) {
    if (!r) return;
    if (r.added > 0) toast(`已添加 ${r.added} 个文件`, 'success');
    else toast('没有添加任何文件', 'warning');
    (r.errors || []).slice(0, 3).forEach((m) => toast(m, 'error', 5000));
  }

  /* --------------------------------------------------------- queue filter */

  /**
   * The queue filter: a filter that outranks the template's, for "this batch, these
   * files only". It is deliberately NOT part of the template -- changing the template
   * to suit one batch would silently change every other batch bound to it.
   */
  function seedQueueFilter() {
    const s = ctx.state.settings || {};
    if (s.queueFilter && typeof s.queueFilter === 'object') return { ...s.queueFilter };
    // Start from what the template would do, so "open the panel and change one number"
    // does not wipe the rules the user cannot see.
    const f = currentTemplate().filter || {};
    return {
      minSizeMB: 0, maxSizeMB: 0, minLongEdge: 0, maxLongEdge: 0, minDuration: 0, maxDuration: 0,
      includeExts: [], excludeExts: [],
      action: f.action || 'keep',
      dest: { ...(f.dest || {}) },
      renamePattern: f.renamePattern || '',
      overwrite: !!f.overwrite,
    };
  }

  function readQueueFilter() {
    const get = (n) => el.querySelector(`[data-role=qfilter-fields] [name=qf_${n}]`);
    const numOf = (n) => Number(get(n)?.value || 0) || 0;
    const exts = (n) => (get(n)?.value || '').split(',').map((s) => s.trim()).filter(Boolean);
    return {
      minSizeMB: numOf('minSizeMB'),
      maxSizeMB: numOf('maxSizeMB'),
      minLongEdge: numOf('minLongEdge'),
      maxLongEdge: numOf('maxLongEdge'),
      minDuration: numOf('minDuration'),
      maxDuration: numOf('maxDuration'),
      includeExts: exts('includeExts'),
      excludeExts: exts('excludeExts'),
      action: get('action')?.value || 'keep',
      dest: {
        mode: get('destMode')?.value || '',
        dir: get('destDir')?.value || '',
        suffix: get('destSuffix')?.value || '',
      },
      renamePattern: get('renamePattern')?.value || '',
      overwrite: !!get('overwrite')?.checked,
    };
  }

  /** Push the panel's values to the backend. A nil filter means "use the template's". */
  async function pushQueueFilter() {
    const active = !!local.qfilter && filterActive(local.qfilter);
    await ctx.api.setQueueFilter(active ? local.qfilter : null);
    // Re-render so the button label and tint follow the rules that are actually in
    // effect -- an empty panel means "no override", not "an override that matches nothing".
    renderQueueFilter();
    renderToolbar();
  }

  function toggleQueueFilter() {
    if (local.qfilter) {
      local.qfilter = null;
    } else {
      local.qfilter = seedQueueFilter();
      qfilterEl.hidden = false;
    }
    renderQueueFilter();
    pushQueueFilter();
  }

  function filterActive(f) {
    return f && (f.minSizeMB > 0 || f.maxSizeMB > 0 || f.minLongEdge > 0 || f.maxLongEdge > 0
      || f.minDuration > 0 || f.maxDuration > 0
      || (f.includeExts || []).length > 0 || (f.excludeExts || []).length > 0);
  }

  function renderQueueFilter() {
    qfilterEl.hidden = !local.qfilter;
    const btnText = el.querySelector('[data-role=filter-btn-text]');
    if (local.qfilter && filterActive(local.qfilter)) {
      btnText.textContent = '筛选中';
      el.querySelector('[data-act=toggle-filter]').classList.add('btn--tonal');
      el.querySelector('[data-act=toggle-filter]').classList.remove('btn--outline');
    } else {
      btnText.textContent = '筛选条件';
      el.querySelector('[data-act=toggle-filter]').classList.remove('btn--tonal');
      el.querySelector('[data-act=toggle-filter]').classList.add('btn--outline');
    }
    if (!local.qfilter) {
      // Drop the built fields so reopening the panel starts from the template again
      // instead of showing whatever was typed last time.
      qfilterFields.innerHTML = '';
      return;
    }
    // Rebuilding the inputs while one of them has focus would drop the caret on every
    // keystroke, because the panel re-renders on each input event. So the fields are
    // only built when the panel opens; after that they are left alone.
    if (qfilterFields.childElementCount) return;
    const o = opts();
    const d = local.qfilter;
    const dest = d.dest || {};
    qfilterFields.innerHTML = `
      ${field('最小体积 (MB)', `<input class="input" type="number" min="0" step="1" name="qf_minSizeMB" value="${d.minSizeMB ?? 0}">`, '留 0 表示不限制')}
      ${field('最大体积 (MB)', `<input class="input" type="number" min="0" step="1" name="qf_maxSizeMB" value="${d.maxSizeMB ?? 0}">`)}
      ${field('最短时长 (秒)', `<input class="input" type="number" min="0" name="qf_minDuration" value="${d.minDuration ?? 0}">`)}
      ${field('最长时长 (秒)', `<input class="input" type="number" min="0" name="qf_maxDuration" value="${d.maxDuration ?? 0}">`)}
      ${field('长边下限 (px)', `<input class="input" type="number" min="0" name="qf_minLongEdge" value="${d.minLongEdge ?? 0}">`, '横竖屏都取较长的一边')}
      ${field('长边上限 (px)', `<input class="input" type="number" min="0" name="qf_maxLongEdge" value="${d.maxLongEdge ?? 0}">`)}
      ${field('仅处理这些扩展名', `<input class="input mono" name="qf_includeExts" value="${esc((d.includeExts || []).join(','))}" placeholder="mp4,mkv">`, '逗号分隔，留空表示全部')}
      ${field('排除这些扩展名', `<input class="input mono" name="qf_excludeExts" value="${esc((d.excludeExts || []).join(','))}" placeholder="webm,gif">`)}
      ${field('被排除的文件', selectHtml('qf_action', (o.filterActions || []).filter((x) => x.value !== ''), d.action || 'keep'))}
      ${field('输出方式', selectHtml('qf_destMode', o.destModes || [], dest.mode || ''), '与「输出与命名」相同的四种方式')}
      ${field('目录后缀', `<input class="input mono" name="qf_destSuffix" value="${esc(dest.suffix || '')}" placeholder="_out">`, '「同级目录 + 后缀」模式使用')}
      ${field('指定目录', `<input class="input mono" name="qf_destDir" value="${esc(dest.dir || '')}" placeholder="例如 D:\\Media\\small">`, 'custom / mirror 模式使用')}
      ${field('重命名模板', `<input class="input mono" name="qf_renamePattern" value="${esc(d.renamePattern || '')}" placeholder="{name}.{ext}">`, '可用变量：{name} {ext} {template} {dir}')}
      ${field('覆盖同名文件', `<label class="check" style="height:34px"><input type="checkbox" name="qf_overwrite"${d.overwrite ? ' checked' : ''}><span class="hint">关闭时自动追加 _1</span></label>`)}`;
  }

  /* ---------------------------------------------------------- panel layout */

  function applyPanel() {
    const h = local.collapsed ? 0 : Math.max(120, ctx.state.settings.logPanelHeight || 220);
    panelEl.style.height = local.collapsed ? '36px' : `${h + 34}px`;
    el.querySelector('[data-role=panel-hint]').textContent = local.collapsed ? '' : hintForTab();
    const btn = el.querySelector('[data-act=panel-toggle]');
    btn.innerHTML = icon(local.collapsed ? 'chevronDown' : 'remove');
    btn.style.transform = local.collapsed ? 'rotate(-90deg)' : '';
    gripEl.style.display = local.collapsed ? 'none' : '';
  }

  function hintForTab() {
    const job = currentJob();
    if (!job) return local.tab === 'log' ? '' : '未选择任务';
    if (local.tab === 'log') return `${job.inputName} · ${job.logLineCount || 0} 行`;
    if (local.tab === 'command') return `${job.inputName}`;
    return `${job.inputName} · ${statusMeta(job.status).label}`;
  }

  function applyTab() {
    logEl.hidden = local.tab !== 'log';
    detailsEl.hidden = local.tab !== 'running';
    commandEl.hidden = local.tab !== 'command';
    el.querySelector('[data-role=panel-hint]').textContent = local.collapsed ? '' : hintForTab();
    if (local.tab === 'running') renderDetails();
  }

  let dragStart = null;
  gripEl.addEventListener('mousedown', (e) => {
    dragStart = { y: e.clientY, h: panelEl.getBoundingClientRect().height };
    document.body.style.cursor = 'ns-resize';
    const move = (ev) => {
      const h = Math.min(760, Math.max(150, dragStart.h - (ev.clientY - dragStart.y)));
      panelEl.style.height = `${h}px`;
    };
    const up = async () => {
      document.removeEventListener('mousemove', move);
      document.removeEventListener('mouseup', up);
      document.body.style.cursor = '';
      const h = Math.round(panelEl.getBoundingClientRect().height) - 34;
      ctx.state.settings = { ...ctx.state.settings, logPanelHeight: h, showLogPanel: true };
      await ctx.api.saveSettings(ctx.state.settings);
    };
    document.addEventListener('mousemove', move);
    document.addEventListener('mouseup', up);
  });

  /* ------------------------------------------------------------- rendering */

  function currentJob() {
    return ctx.state.jobs.find((j) => j.id === ctx.state.selectedJobId) || null;
  }

  function renderToolbar() {
    const s = ctx.state.stats || {};
    el.querySelector('[data-role=stat-total]').textContent = `${s.total || 0} 个任务`;
    const done = (s.done || 0) + (s.warning || 0);
    const doneChip = el.querySelector('[data-role=stat-done]');
    doneChip.hidden = done === 0;
    doneChip.textContent = `完成 ${done}`;
    const failChip = el.querySelector('[data-role=stat-failed]');
    const bad = (s.failed || 0) + (s.canceled || 0);
    failChip.hidden = bad === 0;
    failChip.textContent = `失败 ${bad}`;

    // The filter rules now belong to the template, so the chip has to describe the
    // rules that will actually apply to this queue. The queue-level panel outranks
    // them, and the chip has to say so -- otherwise a tightened batch filter would
    // look like it had been ignored.
    const qf = local.qfilter;
    const tpl = currentTemplate();
    const f = qf && filterActive(qf) ? qf : (tpl && tpl.filter) || {};
    const fromQueue = !!(qf && filterActive(qf));
    const parts = [];
    if (f.minSizeMB > 0) parts.push(`小于 ${num(f.minSizeMB, 0)}MB`);
    if (f.maxSizeMB > 0) parts.push(`大于 ${num(f.maxSizeMB, 0)}MB`);
    if (f.minLongEdge > 0) parts.push(`长边小于 ${num(f.minLongEdge, 0)}`);
    if (f.maxLongEdge > 0) parts.push(`长边大于 ${num(f.maxLongEdge, 0)}`);
    if (f.minDuration > 0) parts.push(`时长小于 ${num(f.minDuration, 0)}s`);
    if (f.maxDuration > 0) parts.push(`时长大于 ${num(f.maxDuration, 0)}s`);
    if (f.includeExts?.length) parts.push(`仅 ${f.includeExts.join('/')}`);
    if (f.excludeExts?.length) parts.push(`排除 ${f.excludeExts.join('/')}`);
    const tag = el.querySelector('[data-role=filter-text]');
    if (parts.length) {
      const act = f.action === 'move' ? '移动到' : f.action === 'copy' ? '复制到' : '仅标记';
      tag.textContent = (fromQueue ? '本批次 · ' : '') + parts.join(' · ')
        + (f.action && f.action !== 'keep' ? ` → ${act}` : '');
    } else {
      tag.textContent = fromQueue ? '本批次 · 不筛选' : '未启用过滤';
    }
    el.querySelector('[data-role=stat-filter]').classList.toggle('chip--accent', parts.length > 0);

    const running = (s.running || 0) > 0;
    const pauseBtn = el.querySelector('[data-act=pause]');
    pauseBtn.innerHTML = s.paused ? `${icon('play')}继续` : `${icon('pause')}暂停`;
    pauseBtn.classList.toggle('btn--tonal', !!s.paused);
    el.querySelector('[data-act=start]').disabled = s.paused === false && running && (s.pending || 0) === 0;
  }

  /**
   * The status chip column already shows the state, so the meta line under the
   * bar carries *extra* information only: throughput while running, elapsed time
   * when finished, and the reason when a file was rejected. The compression ratio
   * used to be repeated here even though the size column now shows before -> after
   * with the percentage on it.
   */
  function progressCell(job) {
    const m = statusMeta(job.status);
    const doneish = job.status === 'done' || job.status === 'warning';
    const width = doneish ? 100 : Math.round((job.progress || 0) * 100);

    let left = '';
    if (job.status === 'running') {
      const bits = [];
      if (job.speed > 0) bits.push(`${num(job.speed, 1)}x`);
      if (job.bitrate) bits.push(`${esc(job.bitrate)} kbps`);
      left = bits.join(' · ');
    } else if (doneish) {
      const bits = [];
      if (job.elapsedMs > 0) bits.push(`用时 ${humanElapsed(job.elapsedMs)}`);
      if (job.warnings?.length) bits.push(`${job.warnings.length} 条警告`);
      left = bits.join(' · ');
    } else if (job.error) {
      left = truncate(job.error, 48);
    } else if (job.message && job.status !== 'pending' && job.status !== 'preparing') {
      left = truncate(job.message, 48);
    }

    const right = job.status === 'running' ? pct(job.progress)
      : job.status === 'pending' || job.status === 'preparing' ? '—' : '';
    return `<div class="progress-cell">
      <div class="progress ${m.bar}"><i style="width:${width}%"></i></div>
      <div class="progress-meta"><span>${left}</span><span>${right}</span></div>
    </div>`;
  }

  function truncate(s, n) {
    s = String(s || '');
    return s.length > n ? `${s.slice(0, n - 1)}…` : s;
  }

  /**
   * Before/after for one measured property, in a single cell.
   *
   * The columns used to show the source value with an inline "-> 1920x1080" appended,
   * which ran the two numbers together and left the reader guessing which was the
   * result. Two lines -- source struck through above, result below -- reads as a
   * transformation, and the delta chip on the right answers "was it worth it" without
   * a second glance.
   */
  function deltaCell(srcText, outText, ratio) {
    if (!outText || outText === srcText) {
      return `<span class="cmp__same">${esc(srcText)}</span>`;
    }
    const pctTxt = Number.isFinite(ratio) ? `${Math.round(ratio * 100)}%` : '';
    const cls = ratio < 0.98 ? 'delta-down' : ratio > 1.02 ? 'delta-up' : '';
    return `<span class="cmp">
      <span class="cmp__from">${esc(srcText)}</span>
      <span class="cmp__to">${esc(outText)}${pctTxt ? `<span class="cmp__pct ${cls}">${esc(pctTxt)}</span>` : ''}</span>
    </span>`;
  }

  /**
   * The three measured columns as ready-to-use cell bodies.
   *
   * Returned as an array of innerHTML strings rather than a "<td>...</td>" blob so the
   * fast path (patchJob) can drop them into existing cells without re-parsing markup.
   * A pending row falls back to the plain source value -- there is nothing to compare
   * against yet, and an empty "after" line would only add noise.
   */
  function measureCells(job) {
    const b = job.infoBefore;
    const a = job.infoAfter;
    if (!b) return ['—', '—', '—'];
    const finished = (job.status === 'done' || job.status === 'warning') && a;
    if (!finished) {
      return [
        esc(resolution(b.displayWidth, b.displayHeight)),
        esc(humanDuration(b.duration)),
        esc(humanSize(b.size)),
      ];
    }
    const srcRes = resolution(b.displayWidth, b.displayHeight);
    const outRes = resolution(a.displayWidth || a.width, a.displayHeight || a.height);
    // Resolution is compared pixel-for-pixel; size and duration as ratios.
    const resRatio = a.width && b.displayWidth
      ? (a.width * a.height) / (b.displayWidth * b.displayHeight) : NaN;
    const sizeRatio = a.size && b.size ? a.size / b.size : NaN;
    const durRatio = a.duration && b.duration ? a.duration / b.duration : NaN;
    const tgt = job.resized && job.targetWidth
      ? resolution(job.targetWidth, job.targetHeight) : outRes;
    return [
      deltaCell(srcRes, tgt, resRatio),
      deltaCell(humanDuration(b.duration), humanDuration(a.duration), durRatio),
      deltaCell(humanSize(b.size), humanSize(a.size), sizeRatio),
    ];
  }

  function rowHtml(job) {
    const cls = [local.checked.has(job.id) ? 'is-checked' : '', job.status === 'running' ? 'is-running' : ''].join(' ');
    const [res, dur, size] = measureCells(job);
    return `<tr data-id="${esc(job.id)}" class="${cls}">
      <td class="col-check"><label class="check"><input type="checkbox"${local.checked.has(job.id) ? ' checked' : ''}></label></td>
      <td class="name">
        <div class="nowrap" title="${esc(job.input)}">${esc(job.inputName)}</div>
        <div class="cell-sub nowrap" title="${esc(job.output || job.input)}">${
          job.status === 'filtered' || job.status === 'skipped'
            ? esc(truncate(job.message || (job.output || job.input), 76))
            : esc(truncate(folderOf(job.output || job.input), 76))
        }</div>
      </td>
      <td class="num">${res}</td>
      <td class="num">${dur}</td>
      <td class="num">${size}</td>
      <td>${statusChip(job.status)}</td>
      <td>${progressCell(job)}</td>
      <td class="actions">
        ${job.status === 'done' || job.status === 'warning'
          ? `<button class="btn btn--text btn--icon btn--sm" data-act="reveal" data-path="${esc(job.output)}" title="在资源管理器中显示">${icon('external', 'sm')}</button>`
          : `<button class="btn btn--text btn--icon btn--sm" data-act="reveal" data-path="${esc(job.input)}" title="定位源文件">${icon('folderOpen', 'sm')}</button>`}
        <button class="btn btn--text btn--icon btn--sm" data-act="remove" data-id="${esc(job.id)}" title="移除">${icon('close', 'sm')}</button>
      </td>
    </tr>`;
  }

  function folderOf(p) {
    if (!p) return '';
    const i = Math.max(p.lastIndexOf('\\'), p.lastIndexOf('/'));
    return i > 0 ? p.slice(0, i) : p;
  }

  function renderJobs() {
    const jobs = ctx.state.jobs;
    emptyEl.hidden = jobs.length > 0;
    const hasFinished = jobs.some((j) => ['done', 'warning', 'failed', 'canceled', 'skipped', 'filtered'].includes(j.status));
    const canStart = jobs.some((j) => j.status === 'pending');
    el.querySelector('[data-act=start]').disabled = !canStart;
    el.querySelector('[data-act=stop]').disabled = !jobs.some((j) => j.status === 'running' || j.status === 'preparing' || j.status === 'pending');
    el.querySelector('[data-act=retry]').disabled = !jobs.some((j) => ['failed', 'canceled', 'skipped'].includes(j.status));
    el.querySelector('[data-act=clear-finished]').disabled = !hasFinished;
    el.querySelector('[data-role=check-all]').checked = jobs.length > 0 && local.checked.size === jobs.length;

    rowsEl.innerHTML = jobs.map(rowHtml).join('');
    local.renderedIds = jobs.map((j) => j.id);
    renderToolbar();
    renderDetails();
  }

  /** Fast path: patch only the volatile cells of a single row. */
  function patchJob(job) {
    const row = rowsEl.querySelector(`tr[data-id="${CSS.escape(job.id)}"]`);
    if (!row) return false;
    const cells = row.children;
    // measureCells returns the three measured columns as innerHTML strings, written in
    // place rather than rebuilding the row -- that keeps the progress bar of a running
    // job from being torn down several times a second.
    const [res, dur, size] = measureCells(job);
    cells[2].innerHTML = res;
    cells[3].innerHTML = dur;
    cells[4].innerHTML = size;
    cells[5].innerHTML = statusChip(job.status);
    cells[6].innerHTML = progressCell(job);
    row.classList.toggle('is-running', job.status === 'running');
    if (job.status !== 'running') { renderToolbar(); return true; }
    return true;
  }

  function renderDetails() {
    const job = currentJob();
    if (!job) {
      detailsEl.innerHTML = '<div class="empty" style="grid-column:1/-1">选择一条任务查看详细信息</div>';
      return;
    }
    const b = job.infoBefore;
    const a = job.infoAfter;
    const live = job.status === 'running' || job.status === 'preparing';
    detailsEl.innerHTML = `
      <div class="detail-card">
        <h4>源文件</h4>
        <dl class="kv">
          <dt>文件</dt><dd>${esc(job.inputName)}</dd>
          <dt>格式</dt><dd>${esc(b?.container || '—')}</dd>
          <dt>分辨率</dt><dd>${esc(b ? resolution(b.displayWidth, b.displayHeight) : '—')}</dd>
          <dt>视频</dt><dd>${esc(b?.videoCodec || '—')}${b?.fps ? ` @ ${num(b.fps, 2)} fps` : ''}</dd>
          <dt>音频</dt><dd>${esc(b?.audioCodec || '—')}</dd>
          <dt>时长</dt><dd>${esc(b ? humanDuration(b.duration) : '—')}</dd>
          <dt>大小</dt><dd>${esc(b ? humanSize(b.size) : '—')}</dd>
          <dt>码率</dt><dd>${esc(b ? bitrateText(b.bitRate) : '—')}</dd>
        </dl>
      </div>
      <div class="detail-card">
        <h4>输出</h4>
        <dl class="kv">
          <dt>文件</dt><dd>${esc(job.outputName || '—')}</dd>
          <dt>模板</dt><dd>${esc(job.templateName || '—')}</dd>
          <dt>目标分辨率</dt><dd>${esc(job.resized ? `${job.targetWidth}×${job.targetHeight}` : '保持原始')}</dd>
          <dt>格式</dt><dd>${esc(a?.container || '—')}</dd>
          <dt>大小</dt><dd>${esc(a ? humanSize(a.size) : '—')}</dd>
          <dt>码率</dt><dd>${esc(a ? bitrateText(a.bitRate) : '—')}</dd>
          <dt>压缩比</dt><dd>${b && a ? esc(`${(a.size / b.size * 100).toFixed(1)}%`) : '—'}</dd>
          <dt>耗时</dt><dd>${esc(humanElapsed(job.elapsedMs))}</dd>
        </dl>
      </div>
      <div class="detail-card">
        <h4>实时状态</h4>
        <dl class="kv">
          <dt>状态</dt><dd>${esc(statusMeta(job.status).label)}</dd>
          <dt>进度</dt><dd>${esc(live ? pct(job.progress) : job.status === 'done' || job.status === 'warning' ? '100%' : '—')}</dd>
          <dt>速度</dt><dd>${job.speed ? `${num(job.speed, 2)}x` : '—'}</dd>
          <dt>码率</dt><dd>${esc(job.bitrate ? `${job.bitrate} kbps` : '—')}</dd>
          <dt>已处理</dt><dd>${job.outTimeMs ? esc(humanDuration(job.outTimeMs / 1000)) : '—'}</dd>
          <dt>帧</dt><dd>${job.frame ? `${job.frame}${job.fps ? ` @ ${num(job.fps, 1)}` : ''}` : '—'}</dd>
          <dt>输出体积</dt><dd>${esc(job.outBytes ? humanSize(job.outBytes) : '—')}</dd>
          <dt>队列时间</dt><dd>${esc(dateText(job.queuedAt))}</dd>
        </dl>
      </div>
      ${(job.error || (job.warnings && job.warnings.length)) ? `
      <div class="detail-card" style="grid-column:1/-1">
        <h4>问题记录</h4>
        ${job.error ? `<div class="row" style="align-items:flex-start;gap:8px;margin-bottom:8px">${icon('error', 'sm')}<span style="color:var(--err);word-break:break-all">${esc(job.error)}</span></div>` : ''}
        ${(job.warnings || []).map((w) => `<div class="row" style="align-items:flex-start;gap:8px;margin-bottom:6px">${icon('warning', 'sm')}<span style="color:var(--warn);word-break:break-all">${esc(w)}</span></div>`).join('')}
      </div>` : ''}`;
  }

  function paintSelection() {
    rowsEl.querySelectorAll('tr[data-id]').forEach((tr) => {
      tr.classList.toggle('is-selected', ctx.state.selectedJobId === tr.dataset.id);
      const cb = tr.querySelector('input[type=checkbox]');
      if (cb) cb.checked = local.checked.has(tr.dataset.id);
    });
    // Drop ids that no longer exist so the count in the bulk bar cannot drift away
    // from what the table shows after jobs are removed elsewhere.
    const alive = new Set(ctx.state.jobs.map((j) => j.id));
    for (const id of [...local.checked]) if (!alive.has(id)) local.checked.delete(id);
    const all = el.querySelector('[data-role=check-all]');
    all.checked = ctx.state.jobs.length > 0 && local.checked.size === ctx.state.jobs.length;
    renderBulkbar();
    renderToolbar();
  }

  /** The bulk bar only exists while something is checked, so it cannot push the table
   *  around when it is not needed. It has no checkbox of its own: the header's
   *  select-all is the only "everything" control, so the two can never disagree. */
  function renderBulkbar() {
    const n = local.checked.size;
    bulkbarEl.hidden = n === 0;
    if (n === 0) return;
    el.querySelector('[data-role=bulk-label]').textContent = `已选 ${n} 项`;
  }

  /* ----------------------------------------------------------------- logs */

  function appendLog(jobId, lines) {
    const selected = ctx.state.selectedJobId;
    if (local.tab !== 'log' || (selected && selected !== jobId) || local.collapsed) {
      // still keep the panel hint fresh
      return;
    }
    const frag = lines.map((line) => {
      const cls = /\[error\]|Error|error:|failed/i.test(line) ? 'l-err'
        : /\[warn\]|Warning|deprecated/i.test(line) ? 'l-warn'
          : line.startsWith('$') ? 'l-cmd' : /^\[(done|policy|filter|retry|cancel)\]/i.test(line) ? 'l-info' : '';
      return `<span class="${cls}">${esc(line)}</span>`;
    }).join('\n');
    const atBottom = logEl.scrollTop + logEl.clientHeight >= logEl.scrollHeight - 24;
    logEl.insertAdjacentHTML('beforeend', `\n${frag}`);
    if (atBottom) logEl.scrollTop = logEl.scrollHeight;
    const job = currentJob();
    if (job) el.querySelector('[data-role=panel-hint]').textContent = `${job.inputName} · ${job.logLineCount || 0} 行`;
  }

  async function loadLog(jobId) {
    const lines = await ctx.api.jobLogs(jobId);
    logEl.innerHTML = (lines || []).map((line) => {
      const cls = /\[error\]|failed|Invalid/i.test(line) ? 'l-err'
        : /\[warn\]|Warning|deprecated/i.test(line) ? 'l-warn'
          : line.startsWith('$') ? 'l-cmd' : /^\[(done|policy|filter|retry|cancel)\]/i.test(line) ? 'l-info' : '';
      return `<span class="${cls}">${esc(line)}</span>`;
    }).join('\n');
    logEl.scrollTop = logEl.scrollHeight;
  }

  /* ---------------------------------------------------------------- command */

  async function refreshCommand() {
    const job = currentJob();
    if (!job) {
      commandTextEl.innerHTML = '<div class="cmd"><div class="cmd__line"><span class="cmd__val">选择一条任务查看它的 ffmpeg 命令</span></div></div>';
      return;
    }
    const tplId = job.templateId || ctx.state.currentTemplateId;
    try {
      const plan = await ctx.api.previewCommand(tplId, job.input);
      commandTextEl.innerHTML = commandHtml(plan.bin, plan.args, '（无）');
    } catch (err) {
      commandTextEl.innerHTML = `<div class="cmd"><div class="cmd__line"><span class="cmd__val">${esc(`无法生成命令：${err.message || err}`)}</span></div></div>`;
    }
  }

  async function showCommandPreview() {
    const jobs = ctx.state.jobs.filter((j) => !['done', 'warning', 'failed', 'filtered', 'skipped'].includes(j.status));
    let items = [];
    let warning = '';
    try {
      items = await ctx.api.previewQueue(tplSelect.value);
    } catch (e) {
      warning = e.message || String(e);
    }
    const body = `
      <div style="padding:12px 16px 0">
        <div class="row" style="gap:8px;margin-bottom:10px">
          <span class="chip chip--accent">${icon('layers')}${esc(tplSelect.selectedOptions[0]?.textContent || '当前模板')}</span>
          <span class="chip chip--muted">${items.length || jobs.length} 个待处理任务</span>
          <div class="spacer"></div>
          <span class="hint">命令随源文件分辨率实时变化，这里按当前队列逐条生成</span>
        </div>
        ${warning ? `<div class="chip chip--err" style="margin-bottom:10px">${esc(warning)}</div>` : ''}
      </div>
      <div class="scroll-y" style="padding:0 16px 16px;display:flex;flex-direction:column;gap:10px">
        ${(items.length ? items : jobs.map((j) => ({ jobId: j.id, input: j.input, output: j.output, bin: '', args: [], command: '', notes: [] })))
          .map((it, i) => `
          <div class="card" style="overflow:hidden">
            <div class="row" style="padding:8px 12px;border-bottom:1px solid var(--outline);gap:8px">
              <span class="badge">${i + 1}</span>
              <span class="nowrap" style="flex:1;font-size:12.5px" title="${esc(it.input)}">${esc(it.input)}</span>
              <button class="btn btn--text btn--sm" data-copy="${i}">${icon('copy', 'sm')}复制</button>
            </div>
            <div class="code-block" style="max-height:320px;padding:0">${commandHtml(it.bin, it.args, '（无法生成）')}</div>
            ${(it.notes || []).length ? `<div style="padding:0 12px 10px">${it.notes.map((n) => `<div class="chip chip--warn" style="margin:3px 4px 0 0">${esc(n)}</div>`).join('')}</div>` : ''}
          </div>`).join('')}
      </div>`;
    const { modal } = openModal({
      title: '命令预览',
      size: 'modal--wide',
      body,
      footer: `<span class="hint">共 ${items.length} 条命令</span><div class="spacer"></div>
        <button class="btn btn--tonal" data-copy-all>${icon('copy')}复制全部</button>
        <button class="btn btn--text" data-close2>关闭</button>`,
    });
    const all = items.map((it) => it.command).filter(Boolean).join('\n\n');
    modal.querySelector('[data-copy-all]').addEventListener('click', async () => {
      toast((await copyText(all)) ? '已复制全部命令' : '复制失败', 'success');
    });
    modal.querySelector('[data-close2]')?.addEventListener('click', closeModal);
    modal.querySelectorAll('[data-copy]').forEach((b) => b.addEventListener('click', async () => {
      const it = items[Number(b.dataset.copy)];
      toast((await copyText(it.command)) ? '命令已复制' : '复制失败', 'success', 1600);
    }));
  }

  /* ------------------------------------------------------------------ mount */

  syncTemplateOptions();
  applyPanel();
  applyTab();

  return {
    el,
    mount() {
      syncTemplateOptions();
      // Restore the queue filter only when it was left switched on. Reloading the
      // page should not silently re-apply rules the user had turned off.
      if (ctx.state.settings?.queueFilterSet && ctx.state.settings?.queueFilter) {
        local.qfilter = { ...ctx.state.settings.queueFilter };
      }
      renderQueueFilter();
      renderJobs();
      if (ctx.state.selectedJobId) loadLog(ctx.state.selectedJobId);
    },
    onTemplatesChanged() { syncTemplateOptions(); },
    onJobsChanged() {
      renderJobs();
      if (local.tab === 'log' && ctx.state.selectedJobId && !logEl.textContent.trim()) loadLog(ctx.state.selectedJobId);
    },
    onJobUpdate(job) {
      const existed = local.renderedIds.includes(job.id);
      if (!existed) { renderJobs(); return; }
      if (!patchJob(job)) renderJobs();
    },
    onStats() { renderToolbar(); },
    onLog(batch) { appendLog(batch.jobId, batch.lines || []); },
    onSelectJob(id) {
      ctx.state.selectedJobId = id;
      paintSelection();
      if (local.tab === 'log') loadLog(id);
      else if (local.tab === 'command') refreshCommand();
      else renderDetails();
    },
    destroy() {},
  };
}
