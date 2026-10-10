import { icon } from '../icons.js';
import { esc, selectHtml, field, switchRow, toast, confirmDialog, shellAction } from '../ui.js';

// Only machine-level setup and UI state live here. "输出与命名 / 处理性能 /
// 匹配条件 / 错误与警告" moved to the template page, because they are
// per-template decisions now: the global template holds the defaults and each
// template may override them. Nothing here points at the template page — the
// settings page is a list of what it owns, not a map of everything else.
//
// Order follows how often the entries are touched: binaries first (set up once,
// revisited when ffmpeg moves), then data/logs, then the background behaviours
// that most people configure once and forget.
const CATEGORIES = [
  { id: 'binary', label: 'FFmpeg 程序', icon: 'terminal' },
  { id: 'logs', label: '数据日志', icon: 'database' },
  { id: 'system', label: '系统后台', icon: 'power' },
];

export function createSettingsView(ctx) {
  const el = document.createElement('section');
  el.className = 'page';
  // No data-directory block in the sidebar: the "日志与数据" category already lists
  // every file and where it lives. Repeating the path at the bottom of the nav meant
  // reading the same string twice on the one page that talks about it most.
  el.innerHTML = `
    <div class="settings">
      <aside class="settings__nav">
        <div class="settings__group">
          ${CATEGORIES.map((c) => `<button class="snav${c.id === ctx.state.settingsCategory ? ' is-active' : ''}" data-cat="${c.id}">${icon(c.icon)}${c.label}</button>`).join('')}
        </div>
      </aside>
      <div class="editor">
        <div class="editor__body" data-role="body"></div>
        <div class="editor__foot">
          <span class="hint" data-role="status"></span>
          <div class="spacer"></div>
          <button class="btn btn--text" data-act="reset">${icon('refresh')}恢复默认</button>
          <button class="btn btn--filled" data-act="save">${icon('save')}保存设置</button>
        </div>
      </div>
    </div>`;

  const bodyEl = el.querySelector('[data-role=body]');
  const statusEl = el.querySelector('[data-role=status]');
  let draft = null;
  let dirty = false;

  const opt = (list) => (ctx.state.options || {})[list] || [];

  /* ------------------------------------------------------------ fragment */

  function pathRow(label, name, value, tool) {
    const h = toolHint(tool);
    return `<div class="field span-2">
      <label>${esc(label)}</label>
      <div class="input-group">
        <input class="input mono" name="${name}" value="${esc(value || '')}" placeholder="留空则自动从系统 PATH 中查找">
        <button class="btn btn--outline" data-act="pick" data-target="${name}">${icon('folderOpen')}浏览</button>
        <button class="btn btn--text" data-act="check" data-target="${name}">检测</button>
      </div>
      <span class="hint" data-role="probe-${name}" style="color:${h.color}">${esc(h.text)}</span>
    </div>`;
  }

  /** One compact line per binary: source + version + health, no duplicate cards. */
  function toolHint(tool) {
    if (!tool || !tool.path) return { text: '未检测到，请手动指定路径', color: 'var(--err)' };
    if (tool.ok === false) return { text: `来源：${sourceLabel(tool.source)} · 无法执行`, color: 'var(--err)' };
    const v = shortVersion(tool.version);
    return { text: `来源：${sourceLabel(tool.source)}${v ? ` · ${v}` : ''}`, color: 'var(--ok)' };
  }

  function shortVersion(line) {
    const m = String(line || '').match(/\bversion\s+([0-9][\w.-]*)/i);
    return m ? `version ${m[1]}` : '';
  }

  function render() {
    const s = draft;
    let html = '';

    if (ctx.state.settingsCategory === 'binary') {
      const rt = ctx.state.runtime || {};
      const enc = ctx.state.encoderSupport;
      html = `
      <div class="section">
        <div class="section__head">${icon('terminal', 'sm')}<h3>可执行文件</h3><div class="spacer"></div>
          <button class="btn btn--tonal btn--sm" data-act="autodetect">${icon('search', 'sm')}自动检测</button></div>
        <div class="grid grid--2">
          ${pathRow('ffmpeg 路径', 'ffmpegPath', s.ffmpegPath, rt.ffmpeg)}
          ${pathRow('ffprobe 路径', 'ffprobePath', s.ffprobePath, rt.ffprobe)}
        </div>
      </div>
      <div class="section">
        <div class="section__head">${icon('tune', 'sm')}<h3>全局参数</h3><div class="spacer"></div>
          <span class="hint">会追加到每一条命令上</span></div>
        <div class="grid grid--2">
          ${field('输入前参数', `<input class="input mono" name="globalInArgs" value="${esc(s.globalInArgs || '')}" placeholder="-hwaccel auto">`, '放在 -i 之前')}
          ${field('输出参数', `<input class="input mono" name="globalOutArgs" value="${esc(s.globalOutArgs || '')}" placeholder="-threads 0">`, '放在输出文件之前')}
        </div>
        <div style="margin-top:10px">
          ${switchRow('启用硬件解码', '自动尝试 GPU 解码（-hwaccel auto）', 'hardwareDecode', s.hardwareDecode)}
        </div>
      </div>
      <div class="section">
        <div class="section__head">${icon('speed', 'sm')}<h3>可用的视频编码器</h3><div class="spacer"></div>
          <span class="hint" data-role="enc-hint">${enc ? encSummary() : ''}</span>
          <button class="btn btn--text btn--sm" data-act="encoders">${icon('search', 'sm')}重新检测</button></div>
        <div class="tag-list" data-role="encoders">${enc ? encodersHtml() : '<span class="hint">正在检测…</span>'}</div>
        <div class="hint" style="margin-top:8px">灰掉的是当前 ffmpeg 构建里不存在的编码器。</div>
      </div>`;
    }

    if (ctx.state.settingsCategory === 'system') {
      html = `
      <div class="section">
        <div class="section__head">${icon('power', 'sm')}<h3>电源与后台</h3></div>
        ${switchRow('防止系统睡眠', '处理期间阻止休眠与熄屏', 'preventSleep', s.preventSleep)}
        ${switchRow('启用系统托盘', '', 'enableTray', s.enableTray)}
        ${switchRow('关闭窗口时最小化到托盘', '', 'closeToTray', s.closeToTray)}
        ${switchRow('启动后直接最小化到托盘', '需要先启用系统托盘', 'startMinimized', s.startMinimized)}
        ${switchRow('退出前二次确认', '有任务在处理时提醒一次', 'confirmExit', s.confirmExit)}
      </div>
      <div class="section">
        <div class="section__head">${icon('info', 'sm')}<h3>运行环境</h3></div>
        <div class="detail-card">
          <dl class="kv">
            <dt>系统</dt><dd>${esc(ctx.state.runtime?.os || '—')} / ${esc(ctx.state.runtime?.arch || '—')} · ${ctx.state.runtime?.cpus ?? '—'} 逻辑核心</dd>
            <dt>程序版本</dt><dd>${esc(ctx.state.runtime?.version || '—')}</dd>
            <dt>ffmpeg</dt><dd class="mono">${esc(ctx.state.runtime?.ffmpeg?.path || '未找到')}</dd>
            <dt>ffprobe</dt><dd class="mono">${esc(ctx.state.runtime?.ffprobe?.path || '未找到')}</dd>
          </dl>
        </div>
      </div>`;
    }

    if (ctx.state.settingsCategory === 'logs') {
      html = `
      <div class="section">
        <div class="section__head">${icon('terminal', 'sm')}<h3>日志</h3></div>
        <div class="grid grid--3">
          ${field('界面保留日志行数', `<input class="input" type="number" min="100" max="20000" name="keepLogLines" value="${s.keepLogLines ?? 2000}">`, '仅影响界面上显示的最新日志')}
          ${field('日志体积上限', `<input class="input" type="number" min="1" max="4096" name="logMaxSizeMB" value="${s.logMaxSizeMB ?? 50}">`, '单位 MB，超出后删除最旧的')}
          ${field('日志保留天数', `<input class="input" type="number" min="1" max="3650" name="logKeepDays" value="${s.logKeepDays ?? 7}">`, '')}
        </div>
        <div style="margin-top:12px">
          ${switchRow('把每个任务的日志写入文件', '按任务保存到数据目录的 logs/', 'saveRunLog', s.saveRunLog)}
        </div>
        <div class="hint" style="margin-top:8px">日志目录在启动时和每次任务结束后按上面两项清理。</div>
      </div>
      <div class="section">
        <div class="section__head">${icon('database', 'sm')}<h3>数据文件</h3><div class="spacer"></div>
          <button class="btn btn--text btn--sm" data-act="open-data">${icon('folderOpen', 'sm')}打开目录</button></div>
        <div class="detail-card">
          <h4>存放位置</h4>
          <dl class="kv">
            <dt>数据目录</dt><dd>${esc(ctx.state.runtime?.dataDir || '')}</dd>
            <dt>设置</dt><dd>settings.json</dd>
            <dt>模板</dt><dd>templates.json</dd>
            <dt>处理记录</dt><dd>history.json</dd>
            <dt>运行日志</dt><dd>logs/</dd>
          </dl>
          <div class="hint" style="margin-top:8px">数据全部保存在程序同目录下的 data 文件夹，拷贝程序时可一并带走。</div>
        </div>
      </div>`;
    }

    bodyEl.innerHTML = html;
    bind();
    updateStatus();
    if (ctx.state.settingsCategory === 'binary') loadEncoders(false);
  }

  /* ------------------------------------------------------- encoder support */

  function encodersHtml() {
    const list = ctx.state.encoderSupport;
    if (!list || !list.length) return '<span class="hint">没有可检测的编码器</span>';
    return list.map((e) => `<span class="chip ${e.ok ? 'chip--ok' : 'chip--muted'}" title="${esc(e.name)}"
      style="${e.ok ? '' : 'opacity:.5'}">${icon(e.ok ? 'checkCircle' : 'close', 'sm')}${esc(e.label)}</span>`).join('');
  }

  function encSummary() {
    const list = ctx.state.encoderSupport || [];
    return `共 ${list.length} 个 · 本机支持 ${list.filter((e) => e.ok).length} 个`;
  }

  async function loadEncoders(force) {
    if (!force && ctx.state.encoderSupport) return;
    const box = bodyEl.querySelector('[data-role=encoders]');
    const hint = bodyEl.querySelector('[data-role=enc-hint]');
    if (box) box.innerHTML = '<span class="hint">正在检测…</span>';
    if (hint) hint.textContent = '';
    try {
      ctx.state.encoderSupport = (await ctx.api.detectEncoders()) || [];
    } catch (err) {
      ctx.state.encoderSupport = [];
      if (box) box.innerHTML = `<span class="hint" style="color:var(--err)">${esc(err.message || String(err))}</span>`;
      return;
    }
    if (hint) hint.textContent = encSummary();
    if (box) box.innerHTML = encodersHtml();
  }

  function sourceLabel(src) {
    return { settings: '手动指定', path: '系统 PATH', missing: '未找到' }[src] || '未知';
  }

  /* --------------------------------------------------------------- binding */

  function bind() {
    bodyEl.querySelectorAll('input[name],select[name]').forEach((c) => {
      c.addEventListener('input', onField);
      c.addEventListener('change', onField);
    });
    bodyEl.querySelectorAll('[data-act]').forEach((b) => b.addEventListener('click', onAction));
  }

  function onField(e) {
    const n = e.target.name;
    if (!n) return;
    const v = e.target.type === 'checkbox' ? e.target.checked : e.target.value;
    const numFields = ['keepLogLines', 'logMaxSizeMB', 'logKeepDays'];
    draft[n] = numFields.includes(n) ? Number(v || 0) : v;
    markDirty();
  }

  async function onAction(e) {
    const act = e.currentTarget.dataset.act;
    const target = e.currentTarget.dataset.target;
    if (act === 'pick') {
      const p = await ctx.api.pickBinary();
      if (p) {
        draft[target] = p.replaceAll('\\', '\\\\');
        render();
        markDirty();
      }
    } else if (act === 'pick-dir') {
      const dir = await pickDirectory();
      if (dir) {
        draft[target] = dir;
        render();
        markDirty();
      }
    } else if (act === 'check') {
      const p = draft[target];
      if (!p) { toast('请先填写路径', 'warning'); return; }
      const info = await ctx.api.checkBinary(p);
      const hint = bodyEl.querySelector(`[data-role=probe-${target}]`);
      if (info.ok) {
        const v = shortVersion(info.version);
        if (hint) {
          hint.textContent = `来源：${sourceLabel(info.source)}${v ? ` · ${v}` : ''}`;
          hint.style.color = 'var(--ok)';
        }
        toast('检测通过', 'success');
      } else {
        if (hint) { hint.textContent = info.error || '无法执行'; hint.style.color = 'var(--err)'; }
        toast(info.error || '检测失败', 'error');
      }
    } else if (act === 'autodetect') {
      const b = await ctx.api.autoDetectBinaries();
      draft.ffmpegPath = b.ffmpeg || '';
      draft.ffprobePath = b.ffprobe || '';
      ctx.state.runtime.binaries = b;
      const t = await ctx.api.toolInfo();
      if (ctx.state.runtime) { ctx.state.runtime.ffmpeg = t[0]; ctx.state.runtime.ffprobe = t[1]; }
      render();
      markDirty();
      toast(b.ffmpeg ? '已从系统 PATH 中找到 ffmpeg' : '系统 PATH 中没有找到 ffmpeg，请手动指定', b.ffmpeg ? 'success' : 'warning');
      ctx.state.encoderSupport = null;
      await loadEncoders(false);
    } else if (act === 'open-data') {
      await shellAction(ctx.api.showDataDir());
    } else if (act === 'encoders') {
      await loadEncoders(true);
    }
  }

  async function pickDirectory() {
    const dir = await ctx.api.pickDirectory();
    return dir || null;
  }

  function markDirty() {
    dirty = true;
    updateStatus();
  }

  function updateStatus() {
    statusEl.innerHTML = dirty ? `${icon('dot', 'sm')} 有未保存的修改` : '所有修改已保存';
    statusEl.style.color = dirty ? 'var(--warn)' : 'var(--on-surface-dim)';
  }

  /* ------------------------------------------------------------- category */

  el.addEventListener('click', (e) => {
    const nav = e.target.closest('.snav');
    if (!nav) return;
    ctx.state.settingsCategory = nav.dataset.cat;
    el.querySelectorAll('.snav').forEach((n) => n.classList.toggle('is-active', n.dataset.cat === ctx.state.settingsCategory));
    render();
  });

el.querySelector('[data-act=save]').addEventListener('click', () => save());
  el.querySelector('[data-act=reset]').addEventListener('click', () => reset());

  async function save() {
    if (!draft) return;
    const saved = await ctx.api.saveSettings(draft);
    ctx.state.settings = saved;
    draft = JSON.parse(JSON.stringify(saved));
    dirty = false;
    updateStatus();
    toast('设置已保存', 'success');
    if (ctx.state.settingsCategory === 'binary') {
      ctx.state.encoderSupport = null;
      await loadEncoders(false);
    }
  }

  async function reset() {
    if (!(await confirmDialog('恢复默认设置', '所有设置项将回到出厂状态，模板与处理记录不受影响。', '恢复默认', true))) return;
    const s = await ctx.api.resetSettings();
    ctx.state.settings = s;
    draft = JSON.parse(JSON.stringify(s));
    dirty = false;
    render();
    toast('已恢复默认设置', 'success');
  }

  return {
    el,
    async mount() {
      // The data directory still feeds the "日志与数据" panel, which shows where every
      // file lives -- only the nav-corner duplicate is gone.
      const dir = await ctx.api.dataDir();
      if (ctx.state.runtime) ctx.state.runtime.dataDir = dir;
      draft = JSON.parse(JSON.stringify(ctx.state.settings));
      dirty = false;
      render();
    },
    onTemplatesChanged() {},
    onJobsChanged() {},
    onJobUpdate() {},
    onStats() {},
    onLog() {},
    onSelectJob() {},
    save,
    reset,
    hasUnsaved: () => dirty,
    destroy() {},
  };
}
