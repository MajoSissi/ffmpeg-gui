import { icon } from '../icons.js';
import {
  esc, selectHtml, field, switchRow, toast, confirmDialog, openModal, closeModal, copyText, shellAction,
  commandHtml,
} from '../ui.js';
import {
  SECTIONS, sectionHead, followHint, perfBody, filterBody, problemsBody, outputBody,
} from './sections.js';

const GLOBAL_ID = 't-global';

export function createTemplatesView(ctx) {
  const el = document.createElement('section');
  el.className = 'page';
  // The list column is one piece: title, search, and rows share a single surface with
  // no rule between them. Copying a template is a row action (right-click, or the
  // footer), not a toolbar item -- having it up here made one click act on whatever
  // happened to be selected.
  el.innerHTML = `
    <div class="split">
      <aside class="tpl-list">
        <div class="tpl-list__head">
          <div class="tpl-list__bar">
            <button class="btn btn--text btn--icon btn--sm" data-act="import" title="从文件导入模板">${icon('upload', 'sm')}</button>
            <button class="btn btn--text btn--icon btn--sm" data-act="export" title="导出全部模板">${icon('download', 'sm')}</button>
            <button class="btn btn--text btn--icon btn--sm" data-act="reset-builtin" title="恢复内置模板">${icon('refresh', 'sm')}</button>
            <div class="spacer"></div>
            <button class="btn btn--tonal btn--sm" data-act="new">${icon('add', 'sm')}新建模板</button>
          </div>
          <input class="input" placeholder="搜索模板" data-role="search">
        </div>
        <div class="tpl-list__items" data-role="list"></div>
        <div class="hint tpl-list__tip">拖动可排序 · 右键更多操作</div>
      </aside>
      <div class="editor">
        <div class="editor__body" data-role="form"></div>
        <div class="editor__foot">
          <span class="hint" data-role="status"></span>
          <div class="spacer"></div>
          <button class="btn btn--text" data-act="preview" data-role="preview-btn">${icon('terminal')}命令预览</button>
          <button class="btn btn--text" data-act="duplicate" data-role="dup-btn">${icon('copy')}复制</button>
          <button class="btn btn--text btn--danger" data-act="delete" data-role="delete-btn">${icon('trash')}删除</button>
          <button class="btn btn--tonal" data-act="save-as" data-role="saveas-btn">${icon('copy')}另存为新模板</button>
          <button class="btn btn--filled" data-act="save">${icon('save')}保存</button>
        </div>
      </div>
    </div>`;

  const listEl = el.querySelector('[data-role=list]');
  const formEl = el.querySelector('[data-role=form]');
  const statusEl = el.querySelector('[data-role=status]');

  let draft = null;       // working copy of the selected template
  let dirty = false;
  let keyword = '';
  let dragId = '';        // template being dragged, for the reorder affordance

  const opts = () => ctx.state.options || {};

  /* ------------------------------------------------------------- list */

  /**
   * One list, one column. The global template leads it and cannot be dragged or
   * reordered -- it is the defaults every other template inherits, not a peer preset.
   * It sits inside the same scroll area (rather than in a separate card above it) so
   * the column reads as one list instead of a card plus a list.
   *
   * Each row summarises itself with the encode/resize facts that matter when picking a
   * preset, so the right pane does not have to be open to tell two templates apart.
   */
  function renderList() {
    const g = ctx.state.templates.find((t) => t.global);
    const items = ctx.state.templates
      .filter((t) => !t.global)
      .filter((t) => !keyword
        || t.name.toLowerCase().includes(keyword)
        || (t.description || '').toLowerCase().includes(keyword));

    const head = g ? `
      <div class="tpl-row tpl-row--global ${g.id === ctx.state.currentTemplateId ? 'is-active' : ''}" data-id="${esc(g.id)}">
        <button class="tpl-item" data-global="${esc(g.id)}" title="处理性能配置，输出文件处理">
          <span class="tpl-item__name">${icon('tune', 'sm')}<span class="tpl-item__label">基本配置</span></span>
          <span class="tpl-item__desc">处理性能配置，输出文件处理</span>
        </button>
      </div>
      ${items.length ? '<div class="tpl-list__divider"></div>' : ''}` : '';

    listEl.innerHTML = head + (items.map((t) => `
      <div class="tpl-row ${t.id === ctx.state.currentTemplateId ? 'is-active' : ''}" data-id="${esc(t.id)}"
        draggable="true" title="拖动可调整顺序">
        <button class="tpl-item">
          <span class="tpl-item__name">${esc(t.name)}${t.builtin ? '<span class="badge">内置</span>' : ''}</span>
          <span class="tpl-item__desc">${esc(t.description || summarise(t))}</span>
        </button>
        <span class="tpl-row__grip" aria-hidden="true">${icon('drag', 'sm')}</span>
      </div>`).join('')
      || (g ? '' : '<div class="hint" style="padding:14px">没有匹配的模板</div>'));
  }

  /** The template whose values fill in every blank. Never null. */
  function globalTemplate() {
    return ctx.state.templates.find((t) => t.global) || ctx.state.templates[0] || {};
  }

  function isGlobal() {
    return !!draft?.global;
  }

  function summarise(t) {
    const bits = [];
    bits.push(t.videoMode === 'copy' ? '不重编码' : `视频 ${t.videoCodec || '默认'}`);
    const r = t.resize || {};
    if (r.mode === 'longedge') bits.push(`长边 ${r.longEdge}`);
    else if (r.mode === 'shortedge') bits.push(`短边 ${r.shortEdge}`);
    else if (r.mode === 'exact') bits.push(`${r.width}×${r.height}`);
    else if (r.mode === 'percent') bits.push(`${r.percent}%`);
    if (t.container) bits.push(`.${t.container}`);
    return bits.join(' · ');
  }

  /* ------------------------------------------------------------- form */

  function renderForm() {
    if (!draft) {
      formEl.innerHTML = '<div class="empty">从左侧选择一个模板开始编辑</div>';
      statusEl.textContent = '';
      return;
    }
    const d = draft;
    const r = d.resize || {};
    const o = opts();
    // The global template owns the defaults; on a regular template the three
    // pointer sections may be nil, which is the "follow" state.
    const g = isGlobal();

    // The basic-configuration template has no "基本信息" block: it has no name to
    // edit, no container of its own, and no per-template description -- the list
    // already introduces it. What used to be there was a form with one field in it
    // and a paragraph apologising for being a form.
    formEl.innerHTML = `
      ${g ? '' : `<div class="section">
        <div class="section__head">${icon('layers', 'sm')}<h3>基本信息</h3><div class="spacer"></div>
          <span class="hint">${d.builtin ? '内置模板，可直接修改后保存' : '自定义模板'}</span></div>
        <div class="grid grid--2">
          ${field('模板名称', `<input class="input" name="name" value="${esc(d.name || '')}" placeholder="例如：4K 长边转 2K">`)}
          ${field('输出容器', selectHtml('container', o.containers || [], d.container || ''), '留空表示沿用源文件的容器')}
          ${field('说明', `<input class="input" name="description" value="${esc(d.description || '')}" placeholder="一句话描述这个模板的用途">`, '会显示在队列与记录中', 'span-2')}
        </div>
      </div>`}

${g ? '' : `      <div class="section">
        <div class="section__head">${icon('movie', 'sm')}<h3>视频</h3><div class="spacer"></div>
          <span class="hint">直接复制最快；重新编码才能改分辨率。留空的项不会写进命令，交由 ffmpeg 用默认值</span></div>
        <div class="grid grid--4">
          ${field('处理方式', selectHtml('videoMode', [
            { value: 'encode', label: '重新编码' },
            { value: 'copy', label: '直接复制（不重编码）' },
            { value: 'disable', label: '丢弃视频流' },
          ], d.videoMode || 'encode'))}
          ${field('编码器', selectHtml('videoCodec', o.videoCodecs || [], d.videoCodec || ''), '', 'span-2')}
          ${field('码率控制', selectHtml('rateControl', o.rateControls || [], d.rateControl || 'crf'))}
          ${field('CRF / 质量值', `<input class="input" type="number" min="0" max="51" name="crf" value="${d.crf > 0 ? d.crf : ''}" placeholder="留空 = ffmpeg 默认">`, '常用 18~28；留空则不传 -crf，libx264/x265 默认 23')}
          ${field('目标码率', `<input class="input" name="videoBitrate" value="${esc(d.videoBitrate || '')}" placeholder="如 4000k">`)}
          ${field('峰值码率', `<input class="input" name="maxRate" value="${esc(d.maxRate || '')}" placeholder="如 5000k">`)}
          ${field('缓冲大小', `<input class="input" name="bufSize" value="${esc(d.bufSize || '')}" placeholder="如 8000k">`)}
          ${field('预设 preset', selectHtml('preset', o.presets || [], d.preset || ''))}
          ${field('tune', `<input class="input" name="tune" value="${esc(d.tune || '')}" placeholder="film / animation">`)}
          ${field('Profile', `<input class="input" name="profile" value="${esc(d.profile || '')}" placeholder="high / main">`)}
          ${field('Level', `<input class="input" name="level" value="${esc(d.level || '')}" placeholder="4.1">`)}
          ${field('像素格式', `<input class="input" name="pixFmt" value="${esc(d.pixFmt || '')}" placeholder="yuv420p">`, '兼容性优先用 yuv420p', 'span-2')}
          ${field('帧率 fps', `<input class="input" name="fps" value="${esc(d.fps || '')}" placeholder="留空保持原始">`, '例如 30 / 24000/1001', 'span-2')}
        </div>
      </div>

      <div class="section">
        <div class="section__head">${icon('crop', 'sm')}<h3>分辨率</h3><div class="spacer"></div>
          <span class="hint">锁定长边可自动适配横屏与竖屏</span></div>
        <div class="grid grid--4">
          ${field('缩放方式', selectHtml('resizeMode', o.resizeModes || [], r.mode || 'keep'))}
          ${field('长边', `<input class="input" type="number" name="longEdge" value="${r.longEdge ?? 2560}" placeholder="2560 = 2K">`, '较长的一边固定为该值')}
          ${field('短边', `<input class="input" type="number" name="shortEdge" value="${r.shortEdge ?? 1080}" placeholder="1080">`)}
          ${field('缩放算法', selectHtml('algorithm', o.scaleAlgorithms || [], r.algorithm || 'lanczos'))}
          ${field('宽', `<input class="input" type="number" name="width" value="${r.width ?? 0}" placeholder="0 = 自动">`)}
          ${field('高', `<input class="input" type="number" name="height" value="${r.height ?? 0}" placeholder="0 = 自动">`)}
          ${field('最大宽', `<input class="input" type="number" name="maxWidth" value="${r.maxWidth ?? 0}" placeholder="0 = 不限">`)}
          ${field('最大高', `<input class="input" type="number" name="maxHeight" value="${r.maxHeight ?? 0}" placeholder="0 = 不限">`)}
          ${field('缩放比例 %', `<input class="input" type="number" name="percent" value="${r.percent ?? 50}" placeholder="50">`)}
          ${field('对齐倍数', `<input class="input" type="number" name="multipleOf" value="${r.multipleOf ?? 2}" placeholder="2">`, '宽高自动取整到该倍数')}
          ${field('补边颜色', selectHtml('padColor', o.padColors || [], r.padColor || 'black'))}
        </div>
        <div class="grid grid--3" style="margin-top:10px">
          <label class="check"><input type="checkbox" name="onlyLarger"${r.onlyLarger ? ' checked' : ''}>只缩小，不放大</label>
          <label class="check"><input type="checkbox" name="padToTarget"${r.padToTarget ? ' checked' : ''}>补边到目标尺寸（不裁切）</label>
        </div>
      </div>

      <div class="section">
        <div class="section__head">${icon('bell', 'sm')}<h3>音频</h3></div>
        <div class="grid grid--4">
          ${field('处理方式', selectHtml('audioMode', [
            { value: 'encode', label: '重新编码' },
            { value: 'copy', label: '直接复制（不重编码）' },
            { value: 'disable', label: '移除音频' },
          ], d.audioMode || 'encode'))}
          ${field('编码器', selectHtml('audioCodec', o.audioCodecs || [], d.audioCodec || ''))}
          ${field('码率', `<input class="input" name="audioBitrate" value="${esc(d.audioBitrate || '')}" placeholder="192k">`)}
          ${field('声道数', `<input class="input" type="number" name="audioChannels" value="${d.audioChannels ?? 0}" placeholder="0 = 保持">`)}
          ${field('采样率', `<input class="input" type="number" name="sampleRate" value="${d.sampleRate ?? 0}" placeholder="0 = 保持">`)}
        </div>
      </div>

      <div class="section">
        <div class="section__head">${icon('queue', 'sm')}<h3>容器与流</h3><div class="spacer"></div>
          <span class="hint">勾选项才会写进命令</span></div>
        <div class="grid grid--3">
          <label class="check"><input type="checkbox" name="mapAll"${d.mapAll ? ' checked' : ''}>保留全部流（含字幕、多音轨）</label>
          <label class="check"><input type="checkbox" name="fastStart"${d.fastStart ? ' checked' : ''}>faststart（MP4 网页快速起播）</label>
          <label class="check"><input type="checkbox" name="stripMetadata"${d.stripMetadata ? ' checked' : ''}>清除元数据</label>
          <label class="check"><input type="checkbox" name="stripChapters"${d.stripChapters ? ' checked' : ''}>清除章节</label>
        </div>
        <div class="grid grid--3" style="margin-top:10px">
          ${field('混流队列上限', `<input class="input" type="number" min="0" name="maxMuxQueue" value="${d.maxMuxQueue > 0 ? d.maxMuxQueue : ''}" placeholder="留空 = ffmpeg 默认">`,
            '仅在报「Too many packets buffered」时填写，例如 2048')}
        </div>
      </div>

      <div class="section">
        <div class="section__head">${icon('tune', 'sm')}<h3>滤镜</h3><div class="spacer"></div>
          <span class="hint">滤镜链会自动与分辨率、帧率设置合并</span></div>
        <div class="grid grid--2">
          ${field('滤镜模式', selectHtml('filterMode', [
            { value: 'vf', label: '简单滤镜（追加到 -vf 之后）' },
            { value: 'complex', label: '复杂滤镜图（完整 -filter_complex）' },
          ], d.filterMode || 'vf'))}
          ${field('视频滤镜', `<input class="input mono" name="videoFilters" value="${esc(d.videoFilters || '')}" placeholder="如 hqdn3d=1.5:1.5:6:6">`)}
        </div>
      </div>

      <div class="section">
        <div class="section__head">${icon('plus', 'sm')}<h3>自定义参数</h3><div class="spacer"></div>
          <button class="btn btn--text btn--sm" data-act="add-in">${icon('add', 'sm')}输入参数</button>
          <button class="btn btn--text btn--sm" data-act="add-out">${icon('add', 'sm')}输出参数</button>
        </div>
        <div class="grid grid--2">
          <div>
            <div class="hint" style="margin-bottom:6px">输入前参数（放在 -i 之前）</div>
            ${argTable('inputArgs', d.inputArgs || [], 'in')}
          </div>
          <div>
            <div class="hint" style="margin-bottom:6px">输出参数（放在输出文件之前）</div>
            ${argTable('outputArgs', d.outputArgs || [], 'out')}
          </div>
        </div>
      </div>

`}
${inheritableSection('perf', d, g, o, (s) => perfBody(s, o, ctx.state.runtime?.cpus || 8))}
${inheritableSection('output', d, g, o, (t) => outputBody(t, o))}
${inheritableSection('filter', d, g, o, filterBody)}
${inheritableSection('problems', d, g, o, problemsBody)}
`;

    bindForm();
    updateStatus();
  }

  /**
   * One of the four inheritable sections. On the global template it is always
   * editable; on a regular template the switch decides between the editor and a
   * one-line summary of what is being inherited.
   */
  // No "所有模板的默认值" caption on every section head: the list entry already
  // says what this template is, and four more copies of the same sentence turned
  // the caption into decoration nobody read twice.
  function inheritableSection(key, d, g, o, body) {
    const active = g || !!d[key] || (key === 'output' && !!d.outputOverride);
    return `<div class="section">
      ${sectionHead(key, active, { global: g })}
      ${active ? body(sectionValue(key, d), o) : `<div class="follow">${icon('swap', 'sm')}${esc(followHint(key, globalTemplate()))}</div>`}
    </div>`;
  }

  /** Where a section's editor reads its values from on the draft. */
  function sectionValue(key, d) {
    return key === 'output' ? d : d[key];
  }

  function argTable(name, list, kind) {
    if (!list.length) return `<div class="hint" style="padding:8px 0">还没有额外参数</div>`;
    return `<table class="arg-table" style="width:100%">
      ${list.map((a, i) => `<tr data-kind="${kind}" data-i="${i}">
        <td style="width:20px"><label class="check"><input type="checkbox" data-arg="enabled"${a.enabled ? ' checked' : ''}></label></td>
        <td style="width:34%"><input class="input mono" data-arg="flag" value="${esc(a.flag || '')}" placeholder="-ss"></td>
        <td><input class="input mono" data-arg="value" value="${esc(a.value || '')}" placeholder="值"></td>
        <td><input class="input" data-arg="comment" value="${esc(a.comment || '')}" placeholder="备注"></td>
        <td><button class="btn btn--text btn--icon btn--sm" data-arg="del">${icon('close', 'sm')}</button></td>
      </tr>`).join('')}
    </table>`;
  }

  function collectArgs(kind) {
    const key = kind === 'in' ? 'inputArgs' : 'outputArgs';
    const out = [];
    formEl.querySelectorAll(`tr[data-kind="${kind}"]`).forEach((tr) => {
      const flag = tr.querySelector('[data-arg=flag]').value.trim();
      const value = tr.querySelector('[data-arg=value]').value;
      const comment = tr.querySelector('[data-arg=comment]').value.trim();
      const enabled = tr.querySelector('[data-arg=enabled]').checked;
      if (!flag && !value) return;
      out.push({ flag, value, comment, enabled });
    });
    draft[key] = out;
    return out;
  }

  /* ---------------------------------------------------------- form binding */

  function bindForm() {
    formEl.querySelectorAll('input[name],select[name]').forEach((c) => {
      c.addEventListener('input', onFieldChange);
      c.addEventListener('change', onFieldChange);
    });
    // "与全局不同" switches. Turning one on materialises the section seeded from
    // the global values, so the user edits their own copy rather than blanks.
    // Turning one off clears it: leaving stale values behind would mean the switch
    // says "following" while the command still uses them.
    formEl.querySelectorAll('[data-sec]').forEach((c) => {
      c.addEventListener('change', () => {
        const key = c.dataset.sec;
        if (key === 'output') {
          if (c.checked) {
            const g = globalTemplate();
            draft.outputOverride = true;
            draft.outMode = g.outMode || '';
            draft.outDir = g.outDir || '';
            draft.outSuffix = g.outSuffix || '';
            draft.outPattern = g.outPattern || '';
            draft.outConflict = g.outConflict || '';
          } else {
            draft.outputOverride = false;
            for (const k of ['outMode', 'outDir', 'outSuffix', 'outPattern', 'outConflict']) delete draft[k];
          }
        } else if (c.checked) {
          draft[key] = seedSection(key, globalTemplate());
        } else {
          draft[key] = null;
        }
        markDirty();
        renderForm();
      });
    });
    formEl.querySelectorAll('[data-act=pick-dir]').forEach((b) => b.addEventListener('click', async () => {
      const input = formEl.querySelector(`[name="${b.dataset.target}"]`);
      if (!input) return;
      const dir = await ctx.api.pickDirectory();
      if (!dir) return;
      input.value = dir;
      input.dispatchEvent(new Event('input', { bubbles: true }));
    }));
    formEl.querySelectorAll('[data-act=add-in]').forEach((b) => b.addEventListener('click', () => {
      collectArgs('in'); collectArgs('out');
      draft.inputArgs = [...(draft.inputArgs || []), { flag: '', value: '', comment: '', enabled: true }];
      renderForm();
    }));
    formEl.querySelectorAll('[data-act=add-out]').forEach((b) => b.addEventListener('click', () => {
      collectArgs('in'); collectArgs('out');
      draft.outputArgs = [...(draft.outputArgs || []), { flag: '', value: '', comment: '', enabled: true }];
      renderForm();
    }));
    formEl.querySelectorAll('[data-arg=del]').forEach((b) => b.addEventListener('click', () => {
      const tr = b.closest('tr');
      const kind = tr.dataset.kind;
      const i = Number(tr.dataset.i);
      collectArgs('in'); collectArgs('out');
      const key = kind === 'in' ? 'inputArgs' : 'outputArgs';
      draft[key] = (draft[key] || []).filter((_, idx) => idx !== i);
      renderForm();
    }));
    formEl.querySelectorAll('[data-arg]').forEach((c) => {
      if (c.dataset.arg === 'del') return;
      c.addEventListener('input', () => {
        markDirty();
        collectArgs('in'); collectArgs('out');
      });
      c.addEventListener('change', () => {
        markDirty();
        collectArgs('in'); collectArgs('out');
      });
    });
  }

  /**
   * Seed a freshly enabled section from the global template's values. Starting
   * from the defaults (rather than from zeros) means switching a section on and
   * saving changes nothing until the user actually edits a field.
   *
   * The output section is not copied into the draft at all: it lives in flat fields,
   * and the switch itself is what marks it as overridden.
   */
  function seedSection(key, g) {
    const src = g || {};
    if (key === 'output') {
      return {
        outputOverride: true,
        outMode: src.outMode || '', outDir: src.outDir || '',
        outSuffix: src.outSuffix || '', outPattern: src.outPattern || '',
        outConflict: src.outConflict || '',
      };
    }
    if (key === 'perf') {
      const p = (src.perf || {});
      return {
        concurrency: p.concurrency || 1,
        logLevel: p.logLevel || 'warning',
        retryCount: p.retryCount || 0,
        idlePriority: !!p.idlePriority,
        deleteOnFail: !!p.deleteOnFail,
      };
    }
    if (key === 'filter') {
      const f = (src.filter || {});
      return {
        minSizeMB: f.minSizeMB || 0, maxSizeMB: f.maxSizeMB || 0,
        minLongEdge: f.minLongEdge || 0, maxLongEdge: f.maxLongEdge || 0,
        minDuration: f.minDuration || 0, maxDuration: f.maxDuration || 0,
        includeExts: [...(f.includeExts || [])], excludeExts: [...(f.excludeExts || [])],
        action: f.action || 'keep',
        dest: { ...(f.dest || { mode: '', dir: '', suffix: '' }) },
        renamePattern: f.renamePattern || '',
        overwrite: !!f.overwrite,
      };
    }
    const p = (src.problems || {});
    return {
      errorAction: p.errorAction || 'keep',
      errorDest: { ...(p.errorDest || { mode: '', dir: '', suffix: '' }) },
      warningAction: p.warningAction || 'mark',
      warningDest: { ...(p.warningDest || { mode: '', dir: '', suffix: '' }) },
    };
  }

  /** Every field name that belongs to one of the four inheritable sections. */
  const SECTION_FIELDS = {
    perf: ['pConcurrency', 'pThreads', 'pRetryCount', 'pLogLevel', 'pIdlePriority', 'pDeleteOnFail'],
    output: ['outMode', 'outDir', 'outSuffix', 'outPattern', 'outConflict'],
    filter: ['fMinSizeMB', 'fMaxSizeMB', 'fMinDuration', 'fMaxDuration', 'fMinLongEdge', 'fMaxLongEdge',
      'fIncludeExts', 'fExcludeExts', 'fAction', 'fDestMode', 'fDestDir', 'fDestSuffix',
      'fRenamePattern', 'fOverwrite'],
    problems: ['prErrorAction', 'prErrorDestMode', 'prErrorDestDir', 'prErrorDestSuffix',
      'prWarningAction', 'prWarningDestMode', 'prWarningDestDir', 'prWarningDestSuffix'],
  };

  function sectionOf(name) {
    for (const k of SECTIONS) {
      if (SECTION_FIELDS[k].includes(name)) return k;
    }
    return null;
  }

  function onFieldChange(e) {
    const n = e.target.name;
    const v = e.target.type === 'checkbox' ? e.target.checked : e.target.value;
    if (!n || !draft) return;

    const resizeFields = ['resizeMode', 'longEdge', 'shortEdge', 'width', 'height', 'maxWidth', 'maxHeight',
      'percent', 'onlyLarger', 'multipleOf', 'algorithm', 'padToTarget', 'padColor'];
    if (resizeFields.includes(n)) {
      draft.resize = draft.resize || {};
      const key = n === 'resizeMode' ? 'mode' : n;
      draft.resize[key] = v;
      if (!draft.resize.multipleOf) draft.resize.multipleOf = 2;
      markDirty();
      return;
    }
    const sec = sectionOf(n);
    if (sec) {
      applySectionField(sec, n, v);
      markDirty();
      return;
    }
    const numFields = ['crf', 'audioChannels', 'sampleRate', 'maxMuxQueue'];
    draft[n] = numFields.includes(n) ? Number(v || 0) : v;
    markDirty();
  }

  /**
   * Write one control back into its section. The section is created on demand so
   * a form that predates the switch (or a reset) cannot throw on a nil pointer.
   */
  function applySectionField(sec, name, v) {
    const s = draft[sec] || (draft[sec] = seedSection(sec, globalTemplate()));
    const num = (x) => Number(x || 0);
    const exts = (x) => String(x).split(',').map((y) => y.trim()).filter(Boolean);

    if (sec === 'output') {
      // Flat fields on the draft itself, named the same as the template's JSON keys.
      draft[name] = v;
      draft.outputOverride = true;
      return;
    }
    if (sec === 'perf') {
      if (name === 'pConcurrency') s.concurrency = num(v);
      else if (name === 'pThreads') s.threads = num(v);
      else if (name === 'pRetryCount') s.retryCount = num(v);
      else if (name === 'pLogLevel') s.logLevel = v;
      else if (name === 'pIdlePriority') s.idlePriority = v;
      else if (name === 'pDeleteOnFail') s.deleteOnFail = v;
      return;
    }
    if (sec === 'filter') {
      switch (name) {
        case 'fMinSizeMB': s.minSizeMB = num(v); break;
        case 'fMaxSizeMB': s.maxSizeMB = num(v); break;
        case 'fMinDuration': s.minDuration = num(v); break;
        case 'fMaxDuration': s.maxDuration = num(v); break;
        case 'fMinLongEdge': s.minLongEdge = num(v); break;
        case 'fMaxLongEdge': s.maxLongEdge = num(v); break;
        case 'fIncludeExts': s.includeExts = exts(v); break;
        case 'fExcludeExts': s.excludeExts = exts(v); break;
        case 'fAction': s.action = v; break;
        case 'fRenamePattern': s.renamePattern = v; break;
        case 'fOverwrite': s.overwrite = v; break;
        default: s.dest = { ...(s.dest || {}), [destKey(name)]: v };
      }
      return;
    }
    const isError = name.startsWith('prError');
    const d = isError ? (s.errorDest = s.errorDest || {}) : (s.warningDest = s.warningDest || {});
    if (name === 'prErrorAction') s.errorAction = v;
    else if (name === 'prWarningAction') s.warningAction = v;
    else d[destKey(name)] = v;
  }

  /** prErrorDestMode -> mode, fDestSuffix -> suffix, ... */
  function destKey(name) {
    return name.slice(name.lastIndexOf('Dest') + 4).toLowerCase();
  }

  function markDirty() {
    dirty = true;
    updateStatus();
  }

  function updateStatus() {
    const when = draft?.updatedAt ? ` · 更新于 ${new Date(draft.updatedAt * 1000).toLocaleString('zh-CN')}` : '';
    statusEl.innerHTML = dirty
      ? `${icon('dot', 'sm')} 有未保存的修改`
      : `${icon('checkCircle', 'sm')} 已保存${when}`;
    statusEl.style.color = dirty ? 'var(--warn)' : 'var(--on-surface-dim)';
    // The global template produces no command of its own and is neither
    // deletable nor duplicable, so those controls do not apply to it.
    const g = isGlobal();
    el.querySelector('[data-role=preview-btn]').style.display = g ? 'none' : '';
    el.querySelector('[data-role=delete-btn]').style.display = g ? 'none' : '';
    el.querySelector('[data-role=saveas-btn]').style.display = g ? 'none' : '';
    el.querySelector('[data-role=dup-btn]').style.display = g ? 'none' : '';
  }

  /* -------------------------------------------------------------- actions */

  /**
   * Right-click on a row. The native menu is gone with the frame, so the two actions
   * that belong to *a* template rather than to the page live here: duplicate and
   * delete. Right-clicking selects the row first -- acting on a template the user has
   * not highlighted is the fastest way to delete the wrong one.
   */
  listEl.addEventListener('contextmenu', async (e) => {
    const row = e.target.closest('.tpl-row');
    if (!row) return;
    e.preventDefault();
    const t = ctx.state.templates.find((x) => x.id === row.dataset.id);
    if (!t) return;
    if (ctx.state.currentTemplateId !== t.id) {
      if (dirty && !(await confirmDialog('放弃修改？', '当前模板有未保存的修改，切换后将丢失。', '放弃修改', true))) return;
      selectTemplate(t.id);
    }
    if (t.global) return;   // neither duplicating nor deleting the defaults is meaningful
    ctx.showContextMenu(e.clientX, e.clientY, [
      { label: '复制模板', icon: 'copy', onClick: () => duplicateTemplate(t.id) },
      {
        label: '删除模板',
        icon: 'trash',
        danger: true,
        onClick: () => deleteTemplate(t.id, t.name),
      },
    ]);
  });

  async function duplicateTemplate(id) {
    const t = await ctx.api.duplicateTemplate(id);
    // The backend broadcasts the whole list through templates:changed, so appending
    // here too would leave two copies of the same template in the sidebar.
    if (!ctx.state.templates.some((x) => x.id === t.id)) ctx.state.templates.push(t);
    ctx.state.currentTemplateId = t.id;
    selectTemplate(t.id);
    toast(`已复制为「${t.name}」`, 'success');
  }

  async function deleteTemplate(id, name) {
    if (!(await confirmDialog('删除模板', `确定删除「${name}」吗？此操作不可撤销。`, '删除', true))) return;
    await ctx.api.deleteTemplate(id);
    ctx.state.templates = ctx.state.templates.filter((t) => t.id !== id);
    if (ctx.state.currentTemplateId === id) {
      draft = null; dirty = false;
      selectTemplate(ctx.state.templates.find((t) => !t.global)?.id);
    } else {
      renderList();
    }
    toast('模板已删除', 'success');
  }

  el.addEventListener('click', async (e) => {
    const gcard = e.target.closest('[data-global]');
    if (gcard) {
      if (dirty && !(await confirmDialog('放弃修改？', '当前模板有未保存的修改，切换后将丢失。', '放弃修改', true))) return;
      selectTemplate(gcard.dataset.global);
      return;
    }
    const item = e.target.closest('.tpl-row');
    if (item) {
      if (dirty && !(await confirmDialog('放弃修改？', '当前模板有未保存的修改，切换后将丢失。', '放弃修改', true))) return;
      selectTemplate(item.dataset.id);
      return;
    }
    const btn = e.target.closest('[data-act]');
    if (!btn) return;
    const act = btn.dataset.act;

    if (act === 'new') {
      // Seeded from the global template so the new preset starts on values that
      // already make sense instead of a pile of blanks. The four policy sections stay
      // switched off: a fresh template follows the defaults until it has a reason not to.
      const t = await ctx.api.saveTemplate({
        ...(await ctx.api.newTemplate()),
        name: '新模板', description: '', container: '', videoMode: 'encode', videoCodec: 'libx264',
        rateControl: 'crf', crf: 23, preset: 'medium', pixFmt: 'yuv420p',
        resize: { mode: 'keep', multipleOf: 2, algorithm: 'lanczos' },
        audioMode: 'encode', audioCodec: 'aac', audioBitrate: '192k', fastStart: true,
        perf: null, filter: null, problems: null, outputOverride: false,
      });
      // Same as duplicate: the backend broadcasts the new list, so only fall back to
      // pushing when the broadcast has not landed yet.
      if (!ctx.state.templates.some((x) => x.id === t.id)) ctx.state.templates.push(t);
      ctx.state.currentTemplateId = t.id;
      selectTemplate(t.id);
      toast('已创建模板', 'success');
    } else if (act === 'duplicate') {
      if (!draft) return;
      if (isGlobal()) { toast('全局模板是默认值来源，请直接新建模板', 'warning'); return; }
      await duplicateTemplate(draft.id);
    } else if (act === 'import') {
      try {
        const n = await ctx.api.importTemplatesFromFile();
        if (n > 0) { ctx.state.templates = await ctx.api.templates(); renderList(); toast(`已导入 ${n} 个模板`, 'success'); }
      } catch (err) { toast(err.message || String(err), 'error'); }
    } else if (act === 'export') {
      try {
        const p = await ctx.api.exportTemplates();
        if (p) toast(`已导出到 ${p}`, 'success', 4200);
      } catch (err) { toast(err.message || String(err), 'error'); }
    } else if (act === 'reset-builtin') {
      if (!(await confirmDialog('恢复内置模板', '将删除全部模板并重新写入内置模板集，自定义模板会丢失。', '恢复', true))) return;
      try {
        await ctx.api.invalidateTemplates?.();
      } catch { /* optional */ }
      const dataDir = await ctx.api.dataDir();
      await shellAction(ctx.api.openPath(dataDir));
      toast('请删除 data/templates.json 后重启，即可恢复内置模板', 'info', 6000);
    } else if (act === 'delete') {
      if (!draft) return;
      if (isGlobal()) { toast('全局模板不能删除', 'warning'); return; }
      await deleteTemplate(draft.id, draft.name);
    } else if (act === 'save' || act === 'save-as') {
      if (!draft) return;
      if (act === 'save-as' && isGlobal()) { toast('全局模板不能另存为，请直接新建模板', 'warning'); return; }
      collectArgs('in'); collectArgs('out');
      const payload = { ...draft };
      // A section that is following the global template is saved empty, so the file
      // cannot disagree with the switch: turn the switch on later and the old values
      // would otherwise silently come back.
      for (const k of ['perf', 'filter', 'problems']) {
        if (payload[k] == null) delete payload[k];
      }
      if (!isGlobal() && !payload.outputOverride) {
        for (const k of ['outMode', 'outDir', 'outSuffix', 'outPattern', 'outConflict']) delete payload[k];
      }
      if (act === 'save-as') {
        payload.id = '';
        payload.builtin = false;
        payload.global = false;
        payload.name = `${payload.name} 副本`;
      }
      try {
        // The global template has its own entry point: it is the defaults source,
        // so it can never be created, renamed away or replaced by an import.
        const saved = payload.global
          ? await ctx.api.saveGlobalTemplate(payload)
          : await ctx.api.saveTemplate(payload);
        const i = ctx.state.templates.findIndex((t) => t.id === saved.id);
        if (i >= 0) ctx.state.templates[i] = saved; else ctx.state.templates.push(saved);
        draft = { ...saved };
        dirty = false;
        ctx.state.currentTemplateId = saved.id;
        renderList(); renderForm(); ctx.notifyTemplatesChanged();
        toast(payload.global ? '基本配置已保存，所有模板的默认值已更新' : '模板已保存', 'success');
      } catch (err) { toast(err.message || String(err), 'error'); }
    } else if (act === 'preview') {
      await previewCommand();
    }
  });

  el.querySelector('[data-role=search]').addEventListener('input', (e) => {
    keyword = e.target.value.trim().toLowerCase();
    renderList();
  });

  /* --------------------------------------------------------- reordering */

  /**
   * Drag to reorder. HTML5 DnD rather than a pointer implementation: the rows are a
   * plain vertical list, and the native drag image plus the "you cannot drop here"
   * cursor come for free. The order is written straight through on drop -- a template
   * order is not worth an undo stack, and the list is the only thing it affects.
   */
  listEl.addEventListener('dragstart', (e) => {
    const row = e.target.closest('.tpl-row');
    if (!row) return;
    dragId = row.dataset.id;
    row.classList.add('is-dragging');
    e.dataTransfer.effectAllowed = 'move';
    // Firefox refuses to start a drag without payload.
    e.dataTransfer.setData('text/plain', dragId);
  });

  listEl.addEventListener('dragend', () => {
    dragId = '';
    listEl.querySelectorAll('.is-dragging,.is-over').forEach((n) => n.classList.remove('is-dragging', 'is-over'));
  });

  listEl.addEventListener('dragover', (e) => {
    if (!dragId) return;
    e.preventDefault();
    e.dataTransfer.dropEffect = 'move';
    const row = e.target.closest('.tpl-row');
    listEl.querySelectorAll('.is-over').forEach((n) => n.classList.remove('is-over'));
    if (row && row.dataset.id !== dragId) row.classList.add('is-over');
  });

  listEl.addEventListener('drop', async (e) => {
    if (!dragId) return;
    e.preventDefault();
    const row = e.target.closest('.tpl-row');
    const toId = row?.dataset.id || '';
    const fromId = dragId;
    dragId = '';
    listEl.querySelectorAll('.is-dragging,.is-over').forEach((n) => n.classList.remove('is-dragging', 'is-over'));
    if (!toId || toId === fromId) return;

    // Reorder locally first so the list reacts immediately, then persist. A rejected
    // save puts the old order back rather than leaving the UI lying about what is saved.
    const order = ctx.state.templates.map((t) => t.id);
    const from = order.indexOf(fromId);
    const to = order.indexOf(toId);
    if (from < 0 || to < 0) return;
    order.splice(to, 0, ...order.splice(from, 1));

    const byId = new Map(ctx.state.templates.map((t) => [t.id, t]));
    const previous = ctx.state.templates;
    ctx.state.templates = order.map((id) => byId.get(id)).filter(Boolean);
    renderList();
    // No success toast: the reordered list *is* the confirmation, and a toast that
    // fires on every drop trains the user to dismiss it. A rejected save still has to
    // be loud, because then the list on screen is lying about what is stored.
    try {
      const saved = await ctx.api.reorderTemplates(order);
      if (saved?.length) ctx.state.templates = saved;
    } catch (err) {
      ctx.state.templates = previous;
      renderList();
      toast(err.message || String(err), 'error');
    }
  });

  function selectTemplate(id) {
    const t = ctx.state.templates.find((x) => x.id === id);
    if (!t) { draft = null; renderForm(); renderList(); return; }
    draft = JSON.parse(JSON.stringify(t));
    draft.resize = draft.resize || { mode: 'keep', multipleOf: 2 };
    draft.inputArgs = draft.inputArgs || [];
    draft.outputArgs = draft.outputArgs || [];
    dirty = false;
    ctx.state.currentTemplateId = id;
    renderList();
    renderForm();
  }

  async function previewCommand() {
    if (!draft) return;
    // Preview with variables instead of a sample path: this page describes the
    // template, and both the input and the output are decided per file. The task
    // page has its own preview that resolves real paths for the current queue.
    let plan;
    try {
      plan = await ctx.api.previewCommand(draft.id, '');
    } catch (err) {
      plan = { command: '', warnings: [err.message || String(err)] };
    }
    const body = `
      <div style="padding:14px 16px 0">
        <div class="row row--wrap" style="gap:8px;margin-bottom:10px">
          <span class="chip chip--accent">${icon('layers')}${esc(draft.name)}</span>
          <span class="chip chip--muted mono">{输入}</span>
          <span class="chip chip--muted mono">{输出}</span>
          ${plan.resized ? `<span class="chip chip--muted">缩放按 4K 示例算得 ${plan.targetW}×${plan.targetH}</span>` : ''}
        </div>
        ${(plan.warnings || []).map((w) => `<div class="chip chip--warn" style="margin:0 6px 8px 0">${esc(w)}</div>`).join('')}
        <div class="hint" style="margin-bottom:10px">
          <span class="mono">{输入}</span> 与 <span class="mono">{输出}</span> 会在运行时替换为真实路径
          ${plan.resized ? '；缩放数值按 4K 示例计算，实际以源文件为准' : ''}。
          队列中已有文件时，任务页的「命令预览」会显示替换后的完整命令。
        </div>
      </div>
      <div class="code-block" style="margin:0 16px 16px;padding:0;border:1px solid var(--outline);border-radius:12px;max-height:340px">${commandHtml(plan.bin, plan.args)}</div>`;
    const { modal } = openModal({
      title: '命令预览',
      size: 'modal--wide',
      body,
      footer: `<span class="hint">未保存的修改也会体现在这里</span><div class="spacer"></div>
        <button class="btn btn--tonal" data-copy>${icon('copy')}复制命令</button>
        <button class="btn btn--text" data-close3>关闭</button>`,
    });
    modal.querySelector('[data-copy]').addEventListener('click', async () => {
      toast((await copyText(plan.command)) ? '命令已复制' : '复制失败', 'success');
    });
    modal.querySelector('[data-close3]').addEventListener('click', closeModal);
  }

  /* ---------------------------------------------------------------- mount */

  function mount() {
    renderList();
    // Never land on the global template: it holds no processing settings of its own, so
    // opening the page on it would show an editor that cannot produce a command.
    const list = ctx.state.templates;
    const wanted = ctx.state.currentTemplateId;
    const first = list.find((t) => !t.global && t.id === wanted)
      || list.find((t) => !t.global)
      || list.find((t) => t.global)
      || list[0];
    selectTemplate(first?.id);
  }

  return {
    el,
    mount,
    onTemplatesChanged() { renderList(); },
    onJobsChanged() {},
    onJobUpdate() {},
    onStats() {},
    onLog() {},
    onSelectJob() {},
    destroy() {},
    hasUnsaved: () => dirty,
  };
}
