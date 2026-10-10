import { icon } from '../icons.js';
import {
  esc, humanSize, humanDuration, humanElapsed, resolution, pct, statusChip, statusMeta, statusLabel,
  toast, confirmDialog, copyText, dateText, num, bitrateText,
  commandHtml, pagerHtml, bindPager, locateAction, locateSourceHint,
} from '../ui.js';
import { effective } from './sections.js';
import { profileSummary } from './filters.js';

export function createTasksView(ctx) {
  const el = document.createElement('section');
  el.className = 'page';

  /**
   * The status filter's choices.
   *
   * Grouped by the question being asked rather than one entry per internal status:
   * 已完成 and 完成(警告) are the same answer to "which of these actually got
   * encoded", and 已跳过 / 已排除 both mean the file was deliberately left alone.
   * `match` takes a job, so this one list drives both the dropdown and the filter.
   */
  const STATUS_FILTERS = [
    { value: '', label: '全部状态', match: () => true },
    { value: 'pending', label: '排队中', match: (j) => j.status === 'pending' },
    { value: 'active', label: '处理中', match: (j) => j.status === 'running' || j.status === 'preparing' },
    { value: 'done', label: '已完成', match: (j) => j.status === 'done' || j.status === 'warning' },
    { value: 'failed', label: '失败', match: (j) => j.status === 'failed' },
    { value: 'canceled', label: '已取消', match: (j) => j.status === 'canceled' },
    { value: 'skipped', label: '已跳过 / 已排除', match: (j) => j.status === 'skipped' || j.status === 'filtered' },
  ];

  el.innerHTML = `
    <div class="toolbar toolbar--rows">
      <div class="toolbar__row">
        <button class="btn btn--tonal" data-act="add-files">${icon('add')}添加文件</button>
        <button class="btn btn--tonal" data-act="add-folder">${icon('folderOpen')}添加文件夹</button>
        <select class="select" data-role="status-filter" title="只显示某一类状态的任务">
          ${STATUS_FILTERS.map((f) => `<option value="${f.value}">${f.label}</option>`).join('')}
        </select>
        <button class="btn btn--text" data-act="retry">${icon('refresh')}重试失败</button>
        <button class="btn btn--text" data-act="clear-finished">${icon('trash')}清理已完成</button>
        <div class="toolbar__right">
          <button class="btn" data-act="stop">${icon('stop')}停止</button>
          <button class="btn" data-act="pause">${icon('pause')}暂停</button>
          <button class="btn btn--filled" data-act="start">${icon('play')}开始</button>
        </div>
      </div>
      <div class="toolbar__row">
        <div class="toolbar__field">
          <span class="toolbar__label">${icon('layers', 'sm')}模板</span>
          <select class="select" data-role="template" title="用哪套参数处理这批文件"></select>
        </div>
        <div class="toolbar__field">
          <span class="toolbar__label">${icon('filter', 'sm')}过滤</span>
          <select class="select select--profile" data-role="profile" title="本次运行按哪套过滤规则收文件"></select>
        </div>
        <div class="toolbar__right">
          <span class="chip chip--muted" data-role="stat-total">0 个任务</span>
          <span class="chip chip--ok" data-role="stat-done" hidden></span>
          <span class="chip chip--err" data-role="stat-failed" hidden></span>
          <span class="chip chip--warn" data-role="stat-filter">${icon('filter')}<span data-role="filter-text">未启用过滤</span></span>
        </div>
      </div>
    </div>

    <div class="bulkbar" data-role="bulkbar" hidden>
      <b data-role="bulk-label">已选 0 项</b>
      <div class="spacer"></div>
      <button class="btn btn--text btn--sm" data-act="select-all">全选</button>
      <button class="btn btn--text btn--sm" data-act="select-none">取消选择</button>
      <button class="btn btn--tonal btn--sm" data-act="bulk-delete">${icon('trash', 'sm')}删除输出</button>
      <button class="btn btn--text btn--sm" data-act="bulk-remove">${icon('close', 'sm')}移除所选</button>
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
        <h3 data-role="empty-title">队列里还没有文件</h3>
        <p data-role="empty-text">点击「添加文件」或「添加文件夹」，也可以把文件直接拖进窗口。支持多选文件与多选目录。</p>
      </div>
    </div>
    <div class="pager" data-role="pager">${pagerHtml()}</div>
    <div class="panel" data-role="panel">
      <div class="panel__grip" data-role="grip" title="上下拖动可调整面板高度"></div>
      <div class="panel__head">
        <button class="tab is-active" data-tab="log">${icon('terminal')}输出日志</button>
        <button class="tab" data-tab="running">${icon('gauge')}处理详情</button>
        <button class="tab" data-tab="command">${icon('terminal')}命令预览</button>
        <div class="spacer"></div>
        <span class="hint" data-role="panel-hint"></span>
        <button class="btn btn--tonal btn--sm" data-act="copy-command" title="复制这条命令" hidden>${icon('copy', 'sm')}复制</button>
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
  const copyCmdBtn = el.querySelector('[data-act=copy-command]');
  const panelEl = el.querySelector('[data-role=panel]');
  const gripEl = el.querySelector('[data-role=grip]');
  const tplSelect = el.querySelector('[data-role=template]');
  const statusSelect = el.querySelector('[data-role=status-filter]');
  const profileSelect = el.querySelector('[data-role=profile]');
  const bulkbarEl = el.querySelector('[data-role=bulkbar]');

  const local = {
    tab: 'log',
    checked: new Set(),
    // The rows on the current page, and the rows the filter lets through. Keeping
    // the two apart is what lets a queue update tell "this row is off screen"
    // (leave it alone) from "this row has just entered or left the filter"
    // (rebuild) -- see onJobUpdate.
    renderedIds: [],
    filteredIds: [],
    followLog: true,
    collapsed: false,
    // '' = 全部状态. A view setting, not a queue one: it never reaches the backend,
    // and a hidden job is still queued, still processed and still counted.
    statusFilter: '',
    // Anchor for shift-click range selection: the last row picked without a modifier.
    anchorId: '',
    // The command currently rendered in the preview, kept so 复制 has something
    // to copy without walking the rendered markup.
    lastCommand: '',
  };

  /**
   * 队列自己分页，不经过后端。
   *
   * 队列最多几百条，而且每一条都还是活的：不在屏幕上的行照样有进度进来，那些行
   * 是就地打补丁的，不重建。分页在这里的作用是把 DOM 压小——几千个 <tr> 一起
   * 存在，每一次结构变化都要重排一整张表，这才是长队列卡顿的来源。
   */
  const pager = bindPager(el.querySelector('[data-role=pager]'), {
    onChange: () => {
      renderJobs();
      // 换页之后，上一页勾过的行已经不在屏幕上了：裁剪放在 paintSelection 里，
      // 和筛选走的是同一条规则。
      paintSelection();
    },
  });

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
      // The backend reads settings.lastTemplateId when files are added. Leaving
      // it empty (first run, or after the template it pointed at was deleted)
      // makes the backend guess on its own, and the queue can end up bound to a
      // different template than the one the toolbar shows.
      if (ctx.state.currentTemplateId) {
        ctx.state.settings = { ...ctx.state.settings, lastTemplateId: ctx.state.currentTemplateId };
        ctx.api.saveSettings(ctx.state.settings).catch(() => {});
      }
    }
    renderToolbar();
  }

  /**
   * The toolbar binds the whole queue to one template, so switching it has to
   * move every row at once -- the backend re-points them and sends the finished
   * ones back to 排队中. It only broadcasts stats, though, so the rows and the
   * command preview have to be pulled again here or they would keep showing the
   * previous template until some unrelated event happened to repaint.
   */
  tplSelect.addEventListener('change', async () => {
    ctx.state.currentTemplateId = tplSelect.value;
    const s = { ...ctx.state.settings, lastTemplateId: tplSelect.value };
    ctx.state.settings = s;
    await ctx.api.saveSettings(s);
    const name = tplSelect.selectedOptions[0]?.textContent || '';
    const r = await ctx.api.setAllTemplates(tplSelect.value);
    const applied = r?.applied || 0;
    await refreshJobs();
    if (local.tab === 'command') await refreshCommand();
    if (applied > 0) {
      const back = r?.requeued || 0;
      toast(`已对 ${applied} 个任务应用「${name}」`
        + (back > 0 ? `，其中 ${back} 个已重新排队` : ''), 'success');
    } else {
      toast('模板已切换', 'info', 1600);
    }
  });

  /**
   * The filter is a view setting, so it changes nothing but what is rendered:
   * hidden jobs are still queued, still processed and still counted by the toolbar.
   * The direction of the mistake matters -- a filter that also stopped the queue
   * would be a trap.
   */
  statusSelect.addEventListener('change', () => {
    local.statusFilter = statusSelect.value;
    // The rows that just left the screen cannot stay checked: 全选 and 删除输出 both
    // read the checked set, and acting on a row nobody can see is how a filter turns
    // into a way to delete the wrong files.
    local.checked.clear();
    // 筛选变了，第 3 页指的是另一份列表了。
    pager.reset();
    renderJobs();
    paintSelection();
  });

  /**
   * 任务页上换方案就是**换掉此刻在用的那套**，并且记住它：这里选哪套，下次打开就从它
   * 开始。「不使用过滤」也在这颗下拉里，值是空串。
   *
   * 换的是"往下收的时候按哪套规则"，所以队列里已经在的任务一个都不动：过滤发生在加入
   * 的那一刻，不是跑到一半再筛。把已经在的任务也筛一遍，才是真的把人坑了。
   */
  profileSelect.addEventListener('change', async () => {
    const name = profileSelect.value;
    try {
      ctx.applyFilterState(await ctx.api.setActiveFilter(name, name === ''));
    } catch (e) {
      toast(e?.message || '切换过滤方案失败', 'error');
    }
    renderToolbar();
  });

  /* -------------------------------------------------------------- actions */

  el.addEventListener('click', async (e) => {
    const btn = e.target.closest('[data-act]');
    if (btn) {
      const act = btn.dataset.act;
      if (act === 'add-files') {
        const r = await ctx.api.addFilesDialog(ctx.state.recursive);
        await reportAdd(r);
      } else if (act === 'add-folder') {
        // 选目录，然后按**当前生效的那套方案**收，不再弹面板。规则归「过滤」页管 ——
        // 一批目录连着加好几次的时候，每次都要重新填一遍同样的条件才是真的烦。
        await reportAdd(await ctx.api.addFolderDialog(ctx.state.recursive));
      } else if (act === 'start') {
        ctx.state.autoStarted = true;
        await ctx.api.startQueue();
        toast('开始处理队列', 'success', 1800);
      } else if (act === 'pause') {
        const paused = await ctx.api.togglePause();
        // The button's own label is the only feedback, and it comes from the stats
        // broadcast rather than from this call -- which may not arrive if nothing
        // else changes. Pull it so the label is right immediately. The rows are
        // pulled too: freezing a process flips each running job's Frozen flag, and
        // that is a per-row badge the toolbar event does not carry.
        ctx.state.stats = await ctx.api.stats();
        await refreshJobs();
        renderToolbar();
        toast(paused ? '已暂停，进行中的任务会保留进度' : '队列已继续', 'info', 2200);
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
      } else if (act === 'bulk-remove') {
        await removeChecked();
      } else if (act === 'bulk-delete') {
        await deleteOutputs([...local.checked]);
      } else if (act === 'select-all') {
        local.checked = new Set(local.renderedIds);
        paintSelection();
      } else if (act === 'select-none') {
        local.checked.clear();
        paintSelection();
      } else if (act === 'copy-command') {
        if (!local.lastCommand) return;
        toast((await copyText(local.lastCommand)) ? '命令已复制' : '复制失败', 'success', 1600);
      } else if (act === 'panel-toggle') {
        local.collapsed = !local.collapsed;
        applyPanel();
      } else if (act === 'locate-source') {
        await locateAction(ctx.api.locate(btn.dataset.path || '', btn.dataset.fallback || ''));
      } else if (act === 'locate-output') {
        await locateAction(ctx.api.locate(btn.dataset.path || '', ''));
      } else if (act === 'delete-output') {
        await deleteOutputs([btn.dataset.id]);
      } else if (act === 'remove') {
        await removeOne(btn.dataset.id);
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
      // Coming back to the log means showing what arrived while you were away --
      // and landing on the newest line again rather than wherever the frozen
      // scrollbar happened to be.
      if (local.tab === 'log') showHeldLog();
      if (local.tab === 'command') await refreshCommand();
      return;
    }

    const row = e.target.closest('tr[data-id]');
    if (row && !e.target.closest('input,button,a')) {
      rowPicked(row.dataset.id, e);
      await reflectSelection();
    }
  });

  /**
   * Point the panel below at whatever row is focused now.
   *
   * The outlined row and the panel are one selection seen twice, so every way of
   * moving the focus has to move both. Ticking a row's checkbox used to move only the
   * outline -- which left one file's log sitting under another file's name.
   */
  async function reflectSelection() {
    const id = ctx.state.selectedJobId;
    if (!id) {
      clearLog();
      renderDetails();
      return;
    }
    if (local.tab === 'log') await loadLog(id);
    else if (local.tab === 'command') await refreshCommand();
    else renderDetails();
  }

  /**
   * Clicking a row focuses it; it does not select it.
   *
   * The two used to be the same thing, so a plain click on a row ticked its
   * checkbox and opened the bulk bar -- reading a log meant arming a batch
   * operation. Now a plain click only moves the focus (the outlined row, which is
   * what the panel below shows), and ticking is something the checkbox column does.
   * Ctrl and Shift stay on the row because extending a selection across a range is
   * the one gesture a checkbox cannot express.
   *
   * @param {string} id job id of the clicked row
   * @param {MouseEvent} e
   */
  function rowPicked(id, e) {
    const ids = local.renderedIds;
    ctx.state.selectedJobId = id;
    if (e.ctrlKey || e.metaKey) {
      if (local.checked.has(id)) local.checked.delete(id); else local.checked.add(id);
      local.anchorId = id;
    } else if (e.shiftKey && local.anchorId && ids.includes(local.anchorId)) {
      const a = ids.indexOf(local.anchorId);
      const b = ids.indexOf(id);
      local.checked = new Set(ids.slice(Math.min(a, b), Math.max(a, b) + 1));
    } else {
      local.anchorId = id;
    }
    paintSelection();
  }

  el.addEventListener('change', async (e) => {
    if (e.target.matches('[data-role=check-all]')) {
      local.checked = e.target.checked ? new Set(local.renderedIds) : new Set();
      paintSelection();
      return;
    }
    if (e.target.matches('tr[data-id] input[type=checkbox]')) {
      const id = e.target.closest('tr').dataset.id;
      if (e.target.checked) local.checked.add(id); else local.checked.delete(id);
      ctx.state.selectedJobId = id;
      local.anchorId = id;
      paintSelection();
      // Ticking a row focuses it, so the panel has to follow: the outline and the log
      // below it are the same selection, and moving only one of them shows the wrong
      // file's output under the right file's name.
      await reflectSelection();
    }
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

  /**
   * Remove one row.
   *
   * The repaint is unconditional, and that is the whole fix here. It used to run
   * only on the way back from the call -- but the call *throws* when the job is
   * already gone, so the second click on a row that had just been removed rejected
   * and skipped the repaint entirely: the row stayed, and the button did nothing
   * that could be seen.
   */
  async function removeOne(id) {
    try {
      await ctx.api.removeJob(id);
    } catch (err) {
      // Already gone is the one case worth carrying on from: the desired end state
      // is a row that is not there, and it is not there.
      if (!/任务不存在/.test(String(err?.message || err))) {
        toast(String(err?.message || err), 'error', 5000);
      }
    }
    if (ctx.state.selectedJobId === id) {
      ctx.state.selectedJobId = '';
      clearLog();
    }
    local.checked.delete(id);
    await refreshJobs();
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
    if (ids.includes(ctx.state.selectedJobId)) {
      ctx.state.selectedJobId = '';
      clearLog();
    }
    await refreshJobs();
    toast(n > 0 ? `已移除 ${n} 个任务` : '所选任务已被移除', 'success');
  }

  /**
   * Delete the files behind the given jobs, and keep every row.
   *
   * Deliberately its own button rather than a rider on 移除: throwing away a file
   * that may have taken an hour to encode is not the same decision as clearing a
   * line off a list, and after a removal the row -- the only thing that still says
   * what the file was -- is gone as well. Nothing here removes a job.
   */
  async function deleteOutputs(ids) {
    const jobs = ctx.state.jobs.filter((j) => ids.includes(j.id) && canDeleteOutput(j));
    if (!jobs.length) {
      toast('所选任务没有可删除的输出文件', 'info', 2400);
      return;
    }
    const names = jobs.slice(0, 3).map((j) => j.outputName || j.inputName).join('、');
    const extra = jobs.length > 3 ? ` 等 ${jobs.length} 个文件` : '';
    const running = jobs.length - jobs.filter((j) => !['running', 'preparing'].includes(j.status)).length;
    const ok = await confirmDialog('删除输出文件',
      `将删除 ${names}${extra}${running ? `（其中 ${running} 个仍在处理，会被跳过）` : ''}。`
      + '任务会留在列表里，但文件无法恢复。',
      '删除', true);
    if (!ok) return;
    const res = await ctx.api.deleteOutputs(jobs.map((j) => j.id));
    const failed = (res?.errors || []).length;
    if (res?.deleted) toast(`已删除 ${res.deleted} 个输出文件`, 'success');
    else if (!failed) toast('没有文件被删除', 'info', 2400);
    (res?.errors || []).slice(0, 3).forEach((m) => toast(m, 'error', 6000));
    await refreshJobs();
  }

  /**
   * 双击一行 = 定位这一行的文件。
   *
   * 做完的行去输出，其余的去源文件 —— 这是一条独立规则，不是「点第一颗按钮」：
   * 两个按钮都在，而双击只该有一个答案。源文件那一支要带上落点，否则双击一行被
   * 搬走过的任务，开出来的是「文件不在这里了」。
   */
  rowsEl.addEventListener('dblclick', async (e) => {
    const row = e.target.closest('tr[data-id]');
    if (!row) return;
    const job = ctx.state.jobs.find((j) => j.id === row.dataset.id);
    if (!job) return;
    const hasOutput = !!job.output && !job.outputDeleted
      && (job.status === 'done' || job.status === 'warning');
    if (hasOutput) await locateAction(ctx.api.locate(job.output, ''));
    else await locateAction(ctx.api.locate(job.input, job.sourceMovedTo));
  });

  /**
   * Adding files only broadcasts the queue counters, never the rows, so the
   * list has to be pulled here: otherwise the total chip goes up while the
   * table stays empty until some unrelated event happens to repaint it.
   */
  async function reportAdd(r) {
    if (!r) return;
    if (r.added > 0) {
      await refreshJobs();
      toast(`已添加 ${r.added} 个文件`, 'success');
    } else {
      toast('没有添加任何文件', 'warning');
    }
    (r.errors || []).slice(0, 3).forEach((m) => toast(m, 'error', 5000));
  }

  /* ---------------------------------------------------------- panel layout */

  // Panel height = the body the user sized plus the fixed chrome above it: the
  // panel's 1px top rule, the 6px drag grip and the 34px tab bar.
  const PANEL_CHROME = 41;

  /**
   * The page's own height, used to size the log panel. Falls back to the window
   * while the view is still detached and has no box yet.
   */
  function pageHeight() {
    const h = el.getBoundingClientRect().height;
    return h > 80 ? h : window.innerHeight;
  }

  /**
   * Default log-panel body: half the page.
   *
   * The panel is a peer of the queue, not a footnote under it -- you read a
   * command or a log line by looking at it, not by scrolling a slot. The number
   * is worked out from the page because a pixel default cannot be half of a
   * window it has never seen.
   */
  function panelAuto() {
    return Math.max(160, Math.round(pageHeight() * 0.5) - PANEL_CHROME);
  }

  /** Ceiling: three quarters of the page, so the queue always keeps a quarter. */
  function panelCap() {
    return Math.max(160, Math.round(pageHeight() * 0.75) - PANEL_CHROME);
  }

  function applyPanel() {
    // The stored pixel value only counts once it came from the grip. Otherwise a
    // height carried over from another window size would decide the layout here.
    const sized = !!ctx.state.settings.logPanelSized;
    const want = sized ? Math.max(120, ctx.state.settings.logPanelHeight || 0) : panelAuto();
    const h = local.collapsed ? 0 : Math.min(panelCap(), want);
    panelEl.style.height = local.collapsed ? '36px' : `${h + PANEL_CHROME}px`;
    el.querySelector('[data-role=panel-hint]').textContent = local.collapsed ? '' : hintForTab();
    const btn = el.querySelector('[data-act=panel-toggle]');
    // One chevron, flipped. It used to ask for icon('remove'), which does not
    // exist -- icon() handed back an empty path and the button sat there blank
    // until the pointer happened to land on it.
    btn.innerHTML = icon('chevronDown', 'sm');
    btn.style.transform = local.collapsed ? 'rotate(180deg)' : '';
    gripEl.style.display = local.collapsed ? 'none' : '';
    copyCmdBtn.hidden = local.tab !== 'command' || local.collapsed;
  }

  function hintForTab() {
    const job = currentJob();
    // The command preview names the file in the command itself, so repeating it
    // here only pushed 复制 further from the corner it belongs in.
    if (local.tab === 'command') return '';
    if (!job) return local.tab === 'log' ? '' : '未选择任务';
    if (local.tab === 'log') return `${job.inputName} · ${job.logLineCount || 0} 行`;
    return `${job.inputName} · ${statusMeta(job.status).label}`;
  }

  function applyTab() {
    logEl.hidden = local.tab !== 'log';
    detailsEl.hidden = local.tab !== 'running';
    commandEl.hidden = local.tab !== 'command';
    // 复制 belongs to the head, not to a row of its own above the command: a
    // bar holding nothing but a small button read as a blank first line that
    // the preview was mysteriously indenting around.
    copyCmdBtn.hidden = local.tab !== 'command' || local.collapsed;
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
      // Same ceiling as applyPanel, or a drag past it would be saved and then
      // silently snap back on the next render. Dragging also flips the height
      // from "half the page" to a number the user actually chose.
      const h = Math.min(panelCap(), Math.round(panelEl.getBoundingClientRect().height) - PANEL_CHROME);
      ctx.state.settings = {
        ...ctx.state.settings,
        logPanelHeight: h,
        logPanelSized: true,
        showLogPanel: true,
      };
      await ctx.api.saveSettings(ctx.state.settings);
    };
    document.addEventListener('mousemove', move);
    document.addEventListener('mouseup', up);
  });

  /* ------------------------------------------------------------- rendering */

  function currentJob() {
    return ctx.state.jobs.find((j) => j.id === ctx.state.selectedJobId) || null;
  }

  /** The active filter entry; '' is the 全部状态 entry, so this is never undefined. */
  function activeFilter() {
    return STATUS_FILTERS.find((f) => f.value === local.statusFilter) || STATUS_FILTERS[0];
  }

  function matchesFilter(job) {
    return activeFilter().match(job);
  }

  /** Only the rows the filter lets through. The queue itself is untouched. */
  function visibleJobs() {
    return ctx.state.jobs.filter(matchesFilter);
  }

  /**
   * The slice of the filtered queue that is on screen.
   *
   * pager.render is what clamps the page number, and it is asked on every repaint
   * with the length of the list it is about to count -- so removing rows, or
   * filtering them away, cannot leave the table showing a page past the end.
   */
  function pageJobs() {
    const all = visibleJobs();
    const page = pager.render(all.length);
    const from = (page - 1) * pager.pageSize;
    return all.slice(from, from + pager.pageSize);
  }

  /**
   * The queue keeps working on a job the filter is hiding, so the counter has to
   * show both numbers: a total that silently dropped to the filtered count would
   * say the other files were gone. It counts the filtered list, not the page --
   * "50 / 300 个任务" would say the other 250 had been filtered away.
   */
  function filterSummary(shown, total) {
    return local.statusFilter ? `${shown} / ${total} 个任务` : `${total} 个任务`;
  }

  /**
   * 填工具栏上那个方案下拉。
   *
   * 选项从 `ctx.state.filter.profiles` 来（后端已经保证了至少有一套），选中的是
   * `filter.profile.name` —— 后端算出来的"此刻真正生效的那一套"。前端不按
   * `active` 自己推一遍：那等于把"名字找不到时退回第一套"再写一遍，而
   * 页面上说"当前是这套"、引擎按另一套跑，是最难查的一类不一致。
   *
   * 第一项是「不使用过滤」，值为空串 —— 下拉里只有这个值代表"不选哪套"，所以
   * 禁用过一轮之后还能转回来选某一套，它自己没有被顶掉。`off` 时选中这一项，
   * 标题写成"什么都不按"而不是拿某一套的条件去骗人。
   *
   * 当前这套说了什么放在 title 里：工具栏那一行已经有六个控件，再塞一整句条件进去会
   * 把它挤成两行，而"这套里到底写了什么"是偶尔才要确认一次的事。
   */
  function syncProfileOptions() {
    const st = ctx.state.filter || {};
    const list = st.profiles || [];
    if (!list.length) {
      // 没有方案可列，但「不使用过滤」仍是一个真实的选择 —— 队列照收不误。
      profileSelect.innerHTML = '<option value="">不使用过滤</option>';
      profileSelect.disabled = false;
      profileSelect.value = '';
      profileSelect.title = '添加文件夹、拖入文件夹时不做任何筛选';
      profileSelect.classList.remove('is-off');
      return;
    }
    profileSelect.disabled = false;
    const name = st.off ? '' : (st.profile?.name || list[0].name);
    profileSelect.innerHTML = '<option value="">不使用过滤</option>' + list
      .map((p) => `<option value="${esc(p.name)}">${esc(p.name)}</option>`)
      .join('');
    profileSelect.value = name;
    profileSelect.title = st.off
      ? '添加文件夹、拖入文件夹时不做任何筛选'
      : `本次运行按「${name}」收\n${profileSummary(st.profile)}`;
    // 「禁用」和「选中某一套」长得不一样：前者不做任何筛选，说错了的代价是收进来一堆
    // 本该跳过的文件，得一眼看得出"这批是没过滤的"。
    profileSelect.classList.toggle('is-off', !!st.off);
  }

  function renderToolbar() {
    const s = ctx.state.stats || {};
    el.querySelector('[data-role=stat-total]').textContent
      = filterSummary(local.filteredIds.length, ctx.state.jobs.length);
    const done = (s.done || 0) + (s.warning || 0);
    const doneChip = el.querySelector('[data-role=stat-done]');
    doneChip.hidden = done === 0;
    doneChip.textContent = `完成 ${done}`;
    const failChip = el.querySelector('[data-role=stat-failed]');
    const bad = (s.failed || 0) + (s.canceled || 0);
    failChip.hidden = bad === 0;
    failChip.textContent = `失败 ${bad}`;

    // 过滤方案：这颗下拉说的是"添加文件夹 / 拖入文件夹时按哪一套规则收"。规则本身
    // 归「过滤」页管，这里只选一套 —— 一批目录连着加好几次的时候，每次都要重新填一遍
    // 同样的条件才是真的烦。
    //
    // 用词：工具栏第二行那颗 chip 说的是模板的**匹配条件**（按体积、时长、扩展名排除
    // 文件），所以这一颗叫「过滤」—— 两件事都会让文件不进队列，名字必须分得开。
    syncProfileOptions();

    // The filter rules live on the template (or on the global one, for a template
    // that follows it), so the chip describes whatever this queue is bound to right
    // now. Switching templates has to update it, hence renderToolbar on change.
    //
    // Wording: these bounds say what gets EXCLUDED, not what gets processed. The old
    // phrasing ("小于 100MB") read as a description of the kept files, so a
    // minSizeMB of 100 looked like "process everything from 100MB up" -- the exact
    // opposite of what it does. Naming the excluded side removes the ambiguity.
    const f = (currentTemplate() && currentTemplate().filter) || {};
    const parts = [];
    const n = (v) => num(v, 0);
    if (f.minSizeMB > 0) parts.push(`排除 <${n(f.minSizeMB)}MB`);
    if (f.maxSizeMB > 0) parts.push(`排除 >${n(f.maxSizeMB)}MB`);
    if (f.minLongEdge > 0) parts.push(`排除长边 <${n(f.minLongEdge)}`);
    if (f.maxLongEdge > 0) parts.push(`排除长边 >${n(f.maxLongEdge)}`);
    if (f.minDuration > 0) parts.push(`排除时长 <${n(f.minDuration)}s`);
    if (f.maxDuration > 0) parts.push(`排除时长 >${n(f.maxDuration)}s`);
    if (f.includeExts?.length) parts.push(`仅 ${f.includeExts.join('/')}`);
    if (f.excludeExts?.length) parts.push(`排除 ${f.excludeExts.join('/')}`);
    const tag = el.querySelector('[data-role=filter-text]');
    if (parts.length) {
      const act = f.action === 'move' ? '移动到' : f.action === 'copy' ? '复制到' : '留在原处';
      tag.textContent = parts.join(' · ')
        + (f.action && f.action !== 'keep' ? ` → ${act}` : '');
    } else {
      tag.textContent = '未启用匹配条件';
    }
    el.querySelector('[data-role=stat-filter]').classList.toggle('chip--accent', parts.length > 0);

    const running = (s.running || 0) > 0;
    // Three distinct states, not two: a queue nobody has started must not wear the
    // 「暂停」label, because 暂停 on a queue that never began is a no-op the user
    // cannot tell from a broken button. It also stays disabled until there is
    // something to pause.
    const started = s.started !== false;
    const startBtn = el.querySelector('[data-act=start]');
    const pauseBtn = el.querySelector('[data-act=pause]');
    if (!started) {
      pauseBtn.innerHTML = `${icon('pause')}暂停`;
      pauseBtn.disabled = true;
      pauseBtn.classList.remove('btn--tonal');
    } else {
      pauseBtn.innerHTML = s.paused ? `${icon('play')}继续` : `${icon('pause')}暂停`;
      pauseBtn.disabled = false;
      pauseBtn.classList.toggle('btn--tonal', !!s.paused);
    }
    // 开始 is the only way in, so it stays available while jobs are waiting.
    startBtn.disabled = started && running && (s.pending || 0) === 0;
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
    if (job.frozen) {
      // No throughput while the process is suspended: ffmpeg is not reading
      // anything, so the last reported speed describes work that is not happening.
      // Saying so is also what stops a stopped bar from reading as a hang. The
      // percentage is deliberately left out -- the right-hand column already shows
      // it, and saying it twice reads as a rendering bug.
      left = '已暂停，进度保留';
    } else if (job.status === 'running') {
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

  /**
   * Is there a produced file behind this row that 删除 could still remove?
   *
   * Only jobs that got far enough to name an output are offered: a pending job has
   * nothing, and a running one is holding its output open -- the backend refuses
   * those, and a button that always fails is worse than a greyed-out one.
   */
  function canDeleteOutput(job) {
    return !!job.output && !job.outputDeleted
      && !['pending', 'preparing', 'running'].includes(job.status);
  }

  /**
   * 定位源文件要试的两条路径：列表上那条，和它被搬走之后的落点。
   *
   * 顺序不能反。落点只在「已处理过的文件」真的搬过之后才有，而没搬过的行上列表
   * 上那条路径本身就是答案；把落点放前面，等于让每一行都先去开一个多数时候不存在
   * 的地方。
   */
  function sourcePaths(job) {
    return { path: job.input, fallback: job.sourceMovedTo };
  }

  /**
   * 是否已经有输出文件可以定位。
   *
   * 和 删除 用的是同一道门槛：还在排队或者正在写的行，输出路径只是一个打算写的
   * 地方，点开只会落到「文件不在这里了」。
   */
  function canLocateOutput(job) {
    return !!job.output && !job.outputDeleted
      && !['pending', 'preparing', 'running'].includes(job.status);
  }

  function outputHint(job) {
    if (job.outputDeleted) return '输出文件已删除';
    if (!job.output) return '还没有输出文件';
    if (['pending', 'preparing', 'running'].includes(job.status)) return '输出文件还没有生成';
    return '定位输出文件';
  }

  /**
   * The row's buttons.
   *
   * 定位两个 —— 源文件与输出 —— 各占一个，而不是共用一颗。共用的时候只能二选
   * 一：原来那颗在状态允许时指向输出、否则指向源文件，于是一行做完之后就再也定
   * 位不到源文件，而源文件恰恰是被「已处理过的文件」搬走的那一个。
   *
   * Shared with patchJob, because all of these depend on the status and the fast path
   * only writes the cells it knows about. A finished row still offering 定位源文件,
   * or a 删除 that stays grey after the output appeared, is a row that lies about what
   * can be done with it.
   */
  function actionsCell(job) {
    const src = sourcePaths(job);
    const hasSource = !!(src.path || src.fallback);
    return `
        <button class="btn btn--text btn--icon btn--sm" data-act="locate-source"
          data-path="${esc(src.path)}" data-fallback="${esc(src.fallback)}"
          title="${esc(locateSourceHint(job))}" ${hasSource ? '' : 'disabled'}>${icon('folderOpen', 'sm')}</button>
        <button class="btn btn--text btn--icon btn--sm" data-act="locate-output" data-path="${esc(job.output)}"
          title="${esc(outputHint(job))}" ${canLocateOutput(job) ? '' : 'disabled'}>${icon('external', 'sm')}</button>
        <button class="btn btn--text btn--icon btn--sm" data-act="delete-output" data-id="${esc(job.id)}"
          title="${job.outputDeleted ? '输出文件已删除' : canDeleteOutput(job) ? '删除输出文件（任务保留在列表里）' : '没有可删除的输出文件'}"
          ${canDeleteOutput(job) ? '' : 'disabled'}>${icon('trash', 'sm')}</button>
        <button class="btn btn--text btn--icon btn--sm" data-act="remove" data-id="${esc(job.id)}" title="从列表移除（不动文件）">${icon('close', 'sm')}</button>`;
  }

  function rowHtml(job) {
    const cls = [local.checked.has(job.id) ? 'is-checked' : '', job.status === 'running' && !job.frozen ? 'is-running' : ''].join(' ');
    const [res, dur, size] = measureCells(job);
    // 输出已删除 is a state the row has to admit, not a note to bury: an 打开输出
    // button that opens nothing is worse than no button, and the queue keeps the row
    // on purpose, so it is the only place that can say the file is gone.
    const sub = job.status === 'filtered' || job.status === 'skipped'
      ? esc(truncate(job.message || (job.output || job.input), 76))
      : esc(truncate(folderOf(job.output || job.input), 76));
    const subLine = job.outputDeleted
      ? `<span style="color:var(--warn)">输出已删除</span>${sub ? ` · ${sub}` : ''}`
      : sub;
    return `<tr data-id="${esc(job.id)}" class="${cls}">
      <td class="col-check"><label class="check"><input type="checkbox"${local.checked.has(job.id) ? ' checked' : ''}></label></td>
      <td class="name">
        <div class="nowrap" title="${esc(job.input)}">${esc(job.inputName)}</div>
        <div class="cell-sub nowrap" title="${esc(job.output || job.input)}">${subLine}</div>
      </td>
      <td class="num">${res}</td>
      <td class="num">${dur}</td>
      <td class="num">${size}</td>
      <td>${statusChip(job.status, job.frozen)}</td>
      <td>${progressCell(job)}</td>
      <td class="actions">${actionsCell(job)}</td>
    </tr>`;
  }

  function folderOf(p) {
    if (!p) return '';
    const i = Math.max(p.lastIndexOf('\\'), p.lastIndexOf('/'));
    return i > 0 ? p.slice(0, i) : p;
  }

  function renderJobs() {
    // The toolbar acts on the queue, the table shows the filter's slice of it. Keeping
    // the two apart is the point: hiding the failed rows must not disable 重试.
    const all = ctx.state.jobs;
    const filtered = visibleJobs();
    local.filteredIds = filtered.map((j) => j.id);
    const jobs = pageJobs();
    emptyEl.hidden = filtered.length > 0;
    if (!filtered.length) paintEmptyState(all.length);
    const hasFinished = all.some((j) => ['done', 'warning', 'failed', 'canceled', 'skipped', 'filtered'].includes(j.status));
    const canStart = all.some((j) => j.status === 'pending');
    el.querySelector('[data-act=start]').disabled = !canStart;
    el.querySelector('[data-act=stop]').disabled = !all.some((j) => j.status === 'running' || j.status === 'preparing' || j.status === 'pending');
    el.querySelector('[data-act=retry]').disabled = !all.some((j) => ['failed', 'canceled', 'skipped'].includes(j.status));
    el.querySelector('[data-act=clear-finished]').disabled = !hasFinished;

    rowsEl.innerHTML = jobs.map(rowHtml).join('');
    local.renderedIds = jobs.map((j) => j.id);
    // 全选 means the rows on screen. Anything else and the header checkbox would tick
    // itself for rows the user cannot see, and 删除输出 would then act on them.
    const ids = new Set(local.renderedIds);
    el.querySelector('[data-role=check-all]').checked = jobs.length > 0
      && [...local.checked].every((id) => ids.has(id)) && local.checked.size === jobs.length;
    renderToolbar();
    renderDetails();
  }

  /**
   * The queue is empty, or the filter is hiding all of it -- two different nothings.
   * "队列里还没有文件" shown under a 失败 filter would just be untrue.
   */
  function paintEmptyState(total) {
    const title = emptyEl.querySelector('[data-role=empty-title]');
    const text = emptyEl.querySelector('[data-role=empty-text]');
    if (total > 0) {
      title.textContent = '没有符合这个筛选的任务';
      text.textContent = `队列里有 ${total} 个任务，但没有一个属于「${activeFilter().label}」。换一个状态，或者切回「全部状态」。`;
      return;
    }
    title.textContent = '队列里还没有文件';
    text.textContent = '点击「添加文件」或「添加文件夹」，也可以把文件直接拖进窗口。支持多选文件与多选目录。';
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
    cells[5].innerHTML = statusChip(job.status, job.frozen);
    cells[6].innerHTML = progressCell(job);
    if (cells[7]) cells[7].innerHTML = actionsCell(job);
    row.classList.toggle('is-running', job.status === 'running' && !job.frozen);
    // A finished row is the one whose actions change, so the toolbar has to be told
    // even though nothing about the queue did.
    if (job.status !== 'running') renderToolbar();
    return true;
  }

  /**
   * 卡片标题行右端的定位按钮。
   *
   * 和行里那两颗共用同一组 data-act：面板和表格说的是同一件事，处理点击的地方
   * 只能有一个。这里多出来的只是位置 —— 标题行比表格宽，说明文字能写完整。
   */
  function locateBtn(act, path, fallback, title, enabled) {
    const ic = act === 'locate-source' ? 'folderOpen' : 'external';
    return `<span class="spacer"></span>
      <button class="btn btn--text btn--icon btn--sm" data-act="${act}"
        data-path="${esc(path || '')}" data-fallback="${esc(fallback || '')}"
        title="${esc(title)}" ${enabled ? '' : 'disabled'}>${icon(ic, 'sm')}</button>`;
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
    const src = sourcePaths(job);
    const hasSource = !!(src.path || src.fallback);
    detailsEl.innerHTML = `
      <div class="detail-card">
        <h4>源文件${locateBtn('locate-source', src.path, src.fallback, locateSourceHint(job), hasSource)}</h4>
        <dl class="kv">
          <dt>文件</dt><dd>${esc(job.inputName)}</dd>
          <dt>格式</dt><dd>${esc(b?.container || '—')}</dd>
          <dt>分辨率</dt><dd>${esc(b ? resolution(b.displayWidth, b.displayHeight) : '—')}</dd>
          <dt>视频</dt><dd>${esc(b?.videoCodec || '—')}${b?.fps ? ` @ ${num(b.fps, 2)} fps` : ''}</dd>
          <dt>音频</dt><dd>${esc(b?.audioCodec || '—')}</dd>
          <dt>时长</dt><dd>${esc(b ? humanDuration(b.duration) : '—')}</dd>
          <dt>大小</dt><dd>${esc(b ? humanSize(b.size) : '—')}</dd>
          <dt>码率</dt><dd>${esc(b ? bitrateText(b.bitRate) : '—')}</dd>
          <dt>路径</dt><dd>${esc(job.input || '—')}</dd>
          ${job.sourceMovedTo ? `<dt>已移动</dt><dd>${esc(job.sourceMovedTo)}</dd>` : ''}
        </dl>
      </div>
      <div class="detail-card">
        <h4>输出${locateBtn('locate-output', job.output, '', outputHint(job), canLocateOutput(job))}</h4>
        <dl class="kv">
          <dt>文件</dt><dd>${esc(job.outputName || '—')}</dd>
          <dt>模板</dt><dd>${esc(job.templateName || '—')}</dd>
          <dt>目标分辨率</dt><dd>${esc(job.resized ? `${job.targetWidth}×${job.targetHeight}` : '保持原始')}</dd>
          <dt>格式</dt><dd>${esc(a?.container || '—')}</dd>
          <dt>大小</dt><dd>${esc(a ? humanSize(a.size) : '—')}</dd>
          <dt>码率</dt><dd>${esc(a ? bitrateText(a.bitRate) : '—')}</dd>
          <dt>压缩比</dt><dd>${b && a ? esc(`${(a.size / b.size * 100).toFixed(1)}%`) : '—'}</dd>
          <dt>耗时</dt><dd>${esc(humanElapsed(job.elapsedMs))}</dd>
          <dt>路径</dt><dd>${esc(job.output || '—')}</dd>
        </dl>
      </div>
      <div class="detail-card">
        <h4>实时状态</h4>
        <dl class="kv">
          <dt>状态</dt><dd>${esc(statusLabel(job.status, job.frozen))}</dd>
          <dt>进度</dt><dd>${esc(live ? pct(job.progress) : job.status === 'done' || job.status === 'warning' ? '100%' : '—')}</dd>
          <dt>速度</dt><dd>${job.speed && !job.frozen ? `${num(job.speed, 2)}x` : '—'}</dd>
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
    // "Alive" means rendered, not merely queued: behind a filter, or on another
    // page, a checked row the user cannot see would still be counted in the bulk bar
    // and would still be deleted by 删除输出. Dropping it keeps the count and the
    // table telling the same story. 全选 follows the same rule -- it means this page.
    const alive = new Set(local.renderedIds);
    for (const id of [...local.checked]) if (!alive.has(id)) local.checked.delete(id);
    const all = el.querySelector('[data-role=check-all]');
    all.checked = local.renderedIds.length > 0 && local.checked.size === local.renderedIds.length;
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
    // 删除输出 is only worth offering when at least one checked row has a file behind
    // it; greyed out it still says the action exists, which is the point of showing it.
    const deletable = ctx.state.jobs.filter((j) => local.checked.has(j.id) && canDeleteOutput(j)).length;
    const delBtn = el.querySelector('[data-act=bulk-delete]');
    delBtn.disabled = deletable === 0;
    delBtn.title = deletable === 0
      ? '所选任务没有可删除的输出文件'
      : `删除 ${deletable} 个输出文件（任务保留在列表里）`;
  }

  /* ----------------------------------------------------------------- logs */

  /**
   * Colour class for one raw ffmpeg line.
   *
   * One copy, shared by the append path and the reload path. The two used to carry
   * their own regex chains, and they had already drifted: the same line could come
   * out red as it streamed past and grey again after a reload.
   */
  function logLineCls(line) {
    return /\[error\]|error:|failed|invalid|no such file/i.test(line) ? 'l-err'
      : /\[warn\]|warning|deprecated|not supported/i.test(line) ? 'l-warn'
        : line.startsWith('$') ? 'l-cmd'
          : /^\[(done|policy|filter|retry|cancel)\]/i.test(line) ? 'l-info' : '';
  }

  function logLineHtml(line) {
    return `<span class="${logLineCls(line)}">${esc(line)}</span>`;
  }

  /**
   * Lines that arrived for the job on screen while another tab was showing.
   *
   * The log can only be painted while its own tab is up, so without somewhere to
   * put them these were simply dropped: spend a minute on 处理详情 and the tail of
   * the run was gone for good, which reads as "the encoder stopped".
   */
  let heldLog = [];

  /**
   * Should the next batch drag the panel down with it?
   *
   * Asked of the scrollbar itself, not of a remembered flag: a scroll event is
   * delivered on the next rendering step, so a reader who scrolled up a moment ago is
   * already out of position even though the event has not landed yet. Following a
   * stale flag there is exactly what used to yank the panel back to the end while
   * somebody was reading twenty lines up.
   *
   * Measured before the new lines go in. Afterwards every append would look like a
   * reader who had scrolled away, because the content just grew past the scrollbar.
   */
  function logAtBottom() {
    return logEl.scrollTop + logEl.clientHeight >= logEl.scrollHeight - 12;
  }

  /**
   * Whether the panel chases the newest line.
   *
   * This is the reader's expressed intent, and only a scroll event changes it. It is
   * kept as state -- rather than read off the scrollbar -- because the panel has to
   * survive being hidden: a view that is `display:none` has no box, so "is it at the
   * end?" has no answer while the reader is on another page, and the intent is all
   * that is left to come back to.
   */
  logEl.addEventListener('scroll', () => {
    local.followLog = logAtBottom();
  });

  /** Jump to the newest line. */
  function scrollLogToEnd() {
    logEl.scrollTop = logEl.scrollHeight;
  }

  function paintPanelHint() {
    if (local.collapsed) return;
    el.querySelector('[data-role=panel-hint]').textContent = hintForTab();
  }

  /**
   * Append freshly streamed lines to the panel.
   *
   * Only the job the panel is showing is written; lines for another job are not
   * this log. While the log tab is elsewhere the lines are held rather than
   * dropped, and the panel chases the newest line only if the reader has not
   * scrolled away from it.
   *
   * @param {string} jobId
   * @param {string[]} lines
   */
  function appendLog(jobId, lines) {
    if (!lines.length) return;
    const selected = ctx.state.selectedJobId;
    if (selected && selected !== jobId) return;
    if (local.tab !== 'log' || local.collapsed) {
      heldLog.push(...lines);
      return;
    }
    const follow = logAtBottom();
    logEl.insertAdjacentHTML('beforeend', `\n${lines.map(logLineHtml).join('\n')}`);
    if (follow) scrollLogToEnd();
    paintPanelHint();
  }

  /**
   * Paint whatever was held while another tab was up, then chase the newest line.
   *
   * Also the way back from a page switch: a hidden view has no box to scroll, so a
   * jump to the end taken then silently does nothing and the panel comes back
   * parked on the top of a log the reader never touched.
   */
  function showHeldLog() {
    if (local.tab !== 'log' || local.collapsed) return;
    if (heldLog.length) {
      logEl.insertAdjacentHTML('beforeend', `\n${heldLog.map(logLineHtml).join('\n')}`);
      heldLog = [];
    }
    // The intent, not the position: while the page was hidden the element had no box
    // to measure, so the reader's own choice is the only thing left to restore.
    if (local.followLog) scrollLogToEnd();
    paintPanelHint();
  }

  /**
   * Replace the panel with one job's whole log.
   *
   * The reply is tagged with a ticket: clicking two rows in quick succession let
   * the slower reply land last, so the panel showed the file you had just clicked
   * away from.
   */
  let logTicket = 0;

  async function loadLog(jobId) {
    const ticket = ++logTicket;
    const lines = await ctx.api.jobLogs(jobId);
    if (ticket !== logTicket) return; // a newer selection already won
    heldLog = [];
    local.followLog = true; // opening a log means "show me the newest line"
    logEl.innerHTML = (lines || []).map(logLineHtml).join('\n');
    scrollLogToEnd();
    paintPanelHint();
  }

  /**
   * Forget the job on screen.
   *
   * A removed row has to take its log with it: the panel would otherwise keep
   * showing the output of a file that is no longer in the queue, with a header
   * naming a row that is not there any more.
   */
  function clearLog() {
    logTicket++; // a reply still in flight must not repaint what was just cleared
    heldLog = [];
    logEl.innerHTML = '';
    paintPanelHint();
  }

  /* ---------------------------------------------------------------- command */

  /**
   * The command preview for the selected job.
   *
   * Built from the template *this job is bound to* -- which, now that the
   * toolbar applies to the whole queue, is the toolbar's template too. The one
   * case where they differ is a job already on the CPU: it keeps whatever it
   * started with. Nothing is said about it in the panel: the command is simply
   * the one that ran / is running, and a note explaining which template it came
   * from was read as "something is wrong" even when nothing was.
   */
  async function refreshCommand() {
    const job = currentJob();
    if (!job) {
      local.lastCommand = '';
      copyCmdBtn.disabled = true;
      commandTextEl.innerHTML = '<div class="cmd"><div class="cmd__line"><span class="cmd__val">选择一条任务查看它的 ffmpeg 命令</span></div></div>';
      return;
    }
    const tplId = job.templateId || ctx.state.currentTemplateId;
    try {
      const plan = await ctx.api.previewCommand(tplId, job.input);
      local.lastCommand = plan.command || '';
      copyCmdBtn.disabled = !local.lastCommand;
      commandTextEl.innerHTML = commandHtml(plan.bin, plan.args, '（无）');
    } catch (err) {
      local.lastCommand = '';
      copyCmdBtn.disabled = true;
      commandTextEl.innerHTML = `<div class="cmd"><div class="cmd__line"><span class="cmd__val">${esc(`无法生成命令：${err.message || err}`)}</span></div></div>`;
    }
  }

  /* ------------------------------------------------------------------ mount */

  syncTemplateOptions();
  applyPanel();
  applyTab();

  return {
    el,
    async mount() {
      syncTemplateOptions();
      renderJobs();
      // Again now that the page has a box: at construction time el is detached
      // and panelAuto() had to fall back to the window height, which is a
      // titlebar taller than the page actually is.
      applyPanel();
      if (local.tab !== 'log' || !ctx.state.selectedJobId) return;
      // The page was off screen until now, so everything appended in the meantime
      // went into a box with no height and the scroll jump was a no-op. Reload a
      // log that was never painted, otherwise put the scrollbar back at the end.
      if (!logEl.textContent.trim()) await loadLog(ctx.state.selectedJobId);
      else showHeldLog();
    },
    onTemplatesChanged() { syncTemplateOptions(); },
    // 「过滤」页存了方案、删了方案、或者把某套设成了默认：工具栏那个下拉的选项和
    // 摘要都得跟着变，否则它会一直列着上一次打开时的方案名。
    onFilterChanged() { renderToolbar(); },
    onJobsChanged() {
      renderJobs();
      if (local.tab === 'log' && ctx.state.selectedJobId && !logEl.textContent.trim()) loadLog(ctx.state.selectedJobId);
    },
    onJobUpdate(job) {
      const onPage = local.renderedIds.includes(job.id);
      const wanted = matchesFilter(job);
      const wasFiltered = local.filteredIds.includes(job.id);
      // A status change can move a row in or out of the current filter, and only a
      // repaint can express that. Everything else is patched in place.
      if (wanted !== wasFiltered) { renderJobs(); return; }
      // Filtered out, or simply on another page: either way there is no row to
      // patch, and rebuilding the table for a row nobody can see is what made a
      // long queue crawl -- progress arrives for every running job, on screen or
      // not.
      if (!onPage) return;
      if (!patchJob(job)) renderJobs();
    },
    onStats() { renderToolbar(); },
    onLog(batch) { appendLog(batch.jobId, batch.lines || []); },
    async onSelectJob(id) {
      ctx.state.selectedJobId = id;
      paintSelection();
      await reflectSelection();
    },
    destroy() {},
  };
}
