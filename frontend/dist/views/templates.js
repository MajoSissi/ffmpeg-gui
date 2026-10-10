import { icon } from '../icons.js';
import {
  esc, selectHtml, field, switchInline, toast, confirmDialog, openModal, closeModal, copyText,
  commandHtml, createListSelection,
} from '../ui.js';
import {
  SECTIONS, sectionHead, followHint, perfBody, filterBody, problemsBody, outputBody,
  containerField, existingBody, dirSpecOf, dirParts, pathCheckHtml,
} from './sections.js';

const GLOBAL_ID = 't-global';

export function createTemplatesView(ctx) {
  const el = document.createElement('section');
  el.className = 'page';
  // The list column is one piece: search, new, and rows share a single surface with no
  // rule between them. Copying a template is a row action (right-click, or the footer),
  // not a toolbar item -- having it up here made one click act on whatever happened to
  // be selected.
  //
  // The head is one row: the search box and the one button that creates things. The
  // whole-column tools (import / export / restore) used to sit here too, but two of the
  // three were one-shot file dialogs and the third deleted everything on a mis-click --
  // three permanent icons for actions nobody does twice.
  el.innerHTML = `
    <div class="split">
      <aside class="tpl-list">
        <div class="tpl-list__head">
          <div class="tpl-list__search">
            <input class="input" placeholder="搜索模板" data-role="search">
            <button class="btn btn--tonal btn--icon" data-act="new" title="新建模板">${icon('add')}</button>
          </div>
        </div>
        <div class="tpl-list__items" data-role="list"></div>
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

  /**
   * 列表多选。手势与判定都在 `ui.js` 的 `createListSelection` 里，和「过滤」页共用同一句
   * ——两页的列表长得一样，能勾选的手势不一样的话，用户得学两遍。
   *
   * 勾着的一串和"正在编辑的那一行"分开存：右侧面板显示后者，右键菜单里的复制作用在
   * 后者身上，「删除 N 项」作用在整串上。
   */
  const selection = createListSelection(() => renderList());

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
    const items = visibleTpls();

    // 勾选高亮和"正在编辑"高亮分两件事（`.is-checked` / `.is-active`）：Ctrl 点出来的
    // 一串里只有最后点的那一行在右边显示着，而整串都要能看出是选中的。
    const rowCls = (id) => [
      'tpl-row',
      selection.has(id) ? 'is-checked' : '',
      id === ctx.state.currentTemplateId ? 'is-active' : '',
    ].filter(Boolean).join(' ');

    const head = g ? `
      <div class="${rowCls(g.id)} tpl-row--global" data-id="${esc(g.id)}">
        <button class="tpl-item" data-global="${esc(g.id)}" title="处理性能配置，输出文件处理">
          <span class="tpl-item__name">${icon('tune', 'sm')}<span class="tpl-item__label">基本配置</span></span>
          <span class="tpl-item__desc">处理性能配置，输出文件处理</span>
        </button>
      </div>
      ${items.length ? '<div class="tpl-list__divider"></div>' : ''}` : '';

    listEl.innerHTML = head + (items.map((t) => `
      <div class="${rowCls(t.id)}" data-id="${esc(t.id)}"
        draggable="true" title="拖动可调整顺序">
        <button class="tpl-item">
          <span class="tpl-item__name">${esc(t.name)}${t.builtin ? '<span class="badge">内置</span>' : ''}</span>
          <span class="tpl-item__desc">${esc(t.description || summarise(t))}</span>
        </button>
        <span class="tpl-row__grip" aria-hidden="true">${icon('drag', 'sm')}</span>
      </div>`).join('')
      || (g ? '' : '<div class="hint" style="padding:14px">没有匹配的模板</div>'));
  }

  /**
   * 列表里**当前可见**的那些模板（全局那一套永远排第一，不参与搜索和勾选）。
   *
   * 搜索、勾选、Shift 的范围三处都问它 —— 各写一遍筛选条件，第三处就会和前两处不一样，
   * 而"搜完再 Shift 连选"是用户最常走的一条路。
   */
  function visibleTpls() {
    return ctx.state.templates
      .filter((t) => !t.global)
      .filter((t) => !keyword
        || t.name.toLowerCase().includes(keyword)
        || (t.description || '').toLowerCase().includes(keyword));
  }

  /** 可见行 id 的顺序。全局那一套排在最前（它在列表里就是第一行）。 */
  const visibleIds = () => {
    const g = ctx.state.templates.find((t) => t.global);
    return (g ? [g.id] : []).concat(visibleTpls().map((t) => t.id));
  };

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

    // 路径测试填的是"我正在看哪个文件"，不是模板的一部分，所以重画一遍表单不该把它
    // 丢掉 —— 切一下「与全局不同」就白填一次路径，比没有这个功能更烦人。必须在
    // innerHTML 被换掉之前读。
    const keepTest = formEl.querySelector('[data-role=path-test]')?.value || '';

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
          ${field('说明', `<input class="input" name="description" value="${esc(d.description || '')}" placeholder="一句话描述这个模板的用途">`, '会显示在队列与记录中', 'span-2')}
        </div>
      </div>`}

${g ? '' : `      <div class="section">
        <div class="section__head">${icon('movie', 'sm')}<h3>视频</h3><div class="spacer"></div>
          <span class="hint">留空的项不会写进命令，交给 ffmpeg 用默认值</span></div>
        <div class="grid grid--4">
          ${field('处理方式', selectHtml('videoMode', [
            { value: 'encode', label: '重新编码' },
            { value: 'copy', label: '直接复制（不重编码）' },
            { value: 'disable', label: '丢弃视频流' },
          ], d.videoMode || 'encode'))}
          ${field('编码器', selectHtml('videoCodec', o.videoCodecs || [], d.videoCodec || ''), '', 'span-2')}
          ${field('码率控制', selectHtml('rateControl', o.rateControls || [], d.rateControl || 'crf'))}
          ${field('CRF / 质量值', `<input class="input" type="number" min="0" max="51" name="crf" value="${d.crf > 0 ? d.crf : ''}" placeholder="留空 = 默认">`, '常用 18~28')}
          ${field('目标码率', `<input class="input" name="videoBitrate" value="${esc(d.videoBitrate || '')}" placeholder="如 4000k">`)}
          ${field('峰值码率', `<input class="input" name="maxRate" value="${esc(d.maxRate || '')}" placeholder="如 5000k">`)}
          ${field('缓冲大小', `<input class="input" name="bufSize" value="${esc(d.bufSize || '')}" placeholder="如 8000k">`)}
          ${field('预设 preset', selectHtml('preset', o.presets || [], d.preset || ''))}
          ${field('tune', `<input class="input" name="tune" value="${esc(d.tune || '')}" placeholder="film / animation">`)}
          ${field('Profile', `<input class="input" name="profile" value="${esc(d.profile || '')}" placeholder="high / main">`)}
          ${field('Level', `<input class="input" name="level" value="${esc(d.level || '')}" placeholder="4.1">`)}
          ${field('像素格式', `<input class="input" name="pixFmt" value="${esc(d.pixFmt || '')}" placeholder="yuv420p">`, '', 'span-2')}
          ${field('帧率 fps', `<input class="input" name="fps" value="${esc(d.fps || '')}" placeholder="留空 = 保持原样">`, '例如 30 / 24000/1001', 'span-2')}
        </div>
      </div>

      <div class="section">
        <div class="section__head">${icon('crop', 'sm')}<h3>分辨率</h3><div class="spacer"></div>
          <span class="hint">另一边由 ffmpeg 按比例算出</span></div>
        <div class="grid grid--4">
          ${field('缩放方式', selectHtml('resizeMode', o.resizeModes || [], r.mode || 'keep'))}
          ${field('长边', `<input class="input" type="number" name="longEdge" value="${r.longEdge ?? 2560}" placeholder="2560 = 2K">`)}
          ${field('短边', `<input class="input" type="number" name="shortEdge" value="${r.shortEdge ?? 1080}" placeholder="1080">`)}
          ${field('缩放算法', selectHtml('algorithm', o.scaleAlgorithms || [], r.algorithm || ''), '留空交给 ffmpeg 决定')}
          ${field('宽', `<input class="input" type="number" name="width" value="${r.width ?? 0}" placeholder="0 = 自动（按比例）">`)}
          ${field('高', `<input class="input" type="number" name="height" value="${r.height ?? 0}" placeholder="0 = 自动（按比例）">`)}
          ${field('最大宽', `<input class="input" type="number" name="maxWidth" value="${r.maxWidth ?? 0}" placeholder="0 = 不限">`)}
          ${field('最大高', `<input class="input" type="number" name="maxHeight" value="${r.maxHeight ?? 0}" placeholder="0 = 不限">`)}
          ${field('缩放比例 %', `<input class="input" type="number" name="percent" value="${r.percent ?? 50}" placeholder="50">`)}
          ${field('对齐倍数', `<input class="input" type="number" name="multipleOf" value="${r.multipleOf ?? 2}" placeholder="2">`, '宽高自动对齐到该倍数')}
          ${field('补边颜色', selectHtml('padColor', o.padColors || [], r.padColor || 'black'))}
        </div>
        <div class="switch-strip" style="margin-top:12px">
          ${switchInline('只缩小，不放大', 'onlyLarger', !!r.onlyLarger)}
          ${switchInline('补边到目标尺寸（不裁切）', 'padToTarget', !!r.padToTarget)}
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
          ${field('声道数', `<input class="input" type="number" name="audioChannels" value="${d.audioChannels ?? 0}" placeholder="0 = 保持原样">`)}
          ${field('采样率', `<input class="input" type="number" name="sampleRate" value="${d.sampleRate ?? 0}" placeholder="0 = 保持原样">`)}
        </div>
      </div>

      <div class="section">
        <div class="section__head">${icon('queue', 'sm')}<h3>容器与流</h3><div class="spacer"></div>
          <span class="hint">只有打开的项才会写进命令</span></div>
        <div class="switch-strip">
          ${switchInline('保留全部流（字幕、多音轨、附件）', 'mapAll', !!d.mapAll, '关闭时命令里会出现 -sn')}
          ${switchInline('faststart（MP4 网页快速起播）', 'fastStart', !!d.fastStart)}
          ${switchInline('清除元数据', 'stripMetadata', !!d.stripMetadata)}
          ${switchInline('清除章节', 'stripChapters', !!d.stripChapters)}
        </div>
        <div class="grid grid--3" style="margin-top:10px">
          ${field('混流队列上限', `<input class="input" type="number" min="0" name="maxMuxQueue" value="${d.maxMuxQueue > 0 ? d.maxMuxQueue : ''}" placeholder="留空 = 默认">`,
            '仅在报「Too many packets buffered」时填写')}
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
${inheritableSection('perf', d, g, o, (s) => perfBody(s, o, ctx.state.runtime?.cpus || 0))}
${inheritableSection('output', d, g, o, outputBody)}
${inheritableSection('existing', d, g, o, existingBody)}
${inheritableSection('filter', d, g, o, filterBody)}
${inheritableSection('problems', d, g, o, problemsBody)}
`;

    // 路径测试填的是"我正在看哪个文件"，不是模板的一部分，所以重画一遍表单不该把它
    // 丢掉 —— 切一下「与全局不同」就白填一次路径，比没有这个功能更烦人。规则可能刚
    // 被改过，所以值还要重算一遍。
    bindForm();
    restorePathTest(keepTest);
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
    // 输出格式 is decided per template rather than inherited, so it stays editable
    // even while the rest of the section is following the global one. Hiding it
    // behind the switch would take away the one control most templates need. When
    // the section IS expanded the body renders it next to 输出文件名称, so the
    // standalone row only exists in the follow state -- otherwise the same select
    // would appear twice and the two copies would drift apart.
    const own = key === 'output' && !g && !active
      ? `<div class="grid grid--2">${containerField(d, o)}</div>`
      : '';
    return `<div class="section">
      ${sectionHead(key, active, { global: g })}
      ${own}
      ${active
        ? body(sectionValue(key, d), o)
        : `<div class="follow">${icon('swap', 'sm')}${esc(followHint(key, globalTemplate()))}</div>`}
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
        <td style="width:46px">${switchInline('', '', a.enabled, '这一行参数是否写进命令', 'data-arg="enabled"')}</td>
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
            draft.outDirSpec = dirSpecOf(g.outDirSpec);
            draft.outPattern = g.outPattern || '';
          } else {
            draft.outputOverride = false;
            for (const k of ['outDirSpec', 'outPattern']) delete draft[k];
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
    // 变量表里的每一颗按钮点一下就复制它自己。这里**不**往输入框里插：光标在哪、
    // 要不要覆盖选中的一段，只有用户知道；复制给他，他自己决定贴哪里。
    formEl.querySelectorAll('[data-act=copy-var]').forEach((b) => b.addEventListener('click', async () => {
      const token = b.dataset.var || '';
      const ok = await copyText(token);
      toast(ok ? `已复制 ${token}` : `复制失败：${token}`, ok ? 'success' : 'error', 1600);
    }));
    formEl.querySelectorAll('[data-act=pick-dir]').forEach((b) => b.addEventListener('click', async () => {
      const input = formEl.querySelector(`[name="${b.dataset.target}"]`);
      if (!input) return;
      const dir = await ctx.api.pickDirectory();
      if (!dir) return;
      input.value = dir;
      input.dispatchEvent(new Event('input', { bubbles: true }));
    }));
    // 路径测试。输入框改一下就重算（防抖，不必每敲一个字符问一次后端），选完文件
    // 立刻算。它读的是 draft，所以看到的就是屏幕上这套还没保存的规则。
    const testInput = formEl.querySelector('[data-role=path-test]');
    if (testInput) {
      testInput.addEventListener('input', schedulePathTest);
      // Enter / 失焦不等防抖：用户已经说"就是它"了。
      testInput.addEventListener('change', () => {
        clearTimeout(pathTestTimer);
        runPathTest();
      });
    }
    formEl.querySelectorAll('[data-act=pick-test-file]').forEach((b) => b.addEventListener('click', async () => {
      const p = await ctx.api.pickFile();
      if (!p) return;
      const input = formEl.querySelector('[data-role=path-test]');
      if (!input) return;
      input.value = p;
      runPathTest();
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
    refreshEnabled();
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
        outDirSpec: dirSpecOf(src.outDirSpec),
        outPattern: src.outPattern || '',
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
    if (key === 'existing') {
      const e = (src.existing || {});
      return {
        action: e.action || 'keep',
        dir: dirSpecOf(e.dir),
        pattern: e.pattern || '',
        overwrite: !!e.overwrite,
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
        dir: dirSpecOf(f.dir),
        renamePattern: f.renamePattern || '',
        overwrite: !!f.overwrite,
      };
    }
    const p = (src.problems || {});
    return {
      errorAction: p.errorAction || 'keep',
      errorDir: dirSpecOf(p.errorDir),
      errorPattern: p.errorPattern || '',
      warningAction: p.warningAction || 'mark',
      warningDir: dirSpecOf(p.warningDir),
      warningPattern: p.warningPattern || '',
    };
  }

  /** Every field name that belongs to one of the inheritable sections. */
  const SECTION_FIELDS = {
    perf: ['pConcurrency', 'pThreads', 'pRetryCount', 'pLogLevel', 'pIdlePriority', 'pDeleteOnFail'],
    output: ['outMode', 'outDir', 'outPrefix', 'outSuffix', 'outKeepTree', 'outPattern'],
    existing: ['exAction', 'exMode', 'exDir', 'exPrefix', 'exSuffix', 'exKeepTree', 'exPattern', 'exOverwrite'],
    filter: ['fMinSizeMB', 'fMaxSizeMB', 'fMinDuration', 'fMaxDuration', 'fMinLongEdge', 'fMaxLongEdge',
      'fIncludeExts', 'fExcludeExts', 'fAction', 'fMode', 'fDir', 'fPrefix', 'fSuffix', 'fKeepTree',
      'fRenamePattern', 'fOverwrite'],
    problems: ['prErrorAction', 'prErrorMode', 'prErrorDir', 'prErrorPrefix', 'prErrorSuffix',
      'prErrorKeepTree', 'prErrorPattern',
      'prWarningAction', 'prWarningMode', 'prWarningDir', 'prWarningPrefix', 'prWarningSuffix',
      'prWarningKeepTree', 'prWarningPattern'],
  };

  function sectionOf(name) {
    for (const k of SECTIONS) {
      if (SECTION_FIELDS[k].includes(name)) return k;
    }
    return null;
  }

  // The resize fields Go unmarshals into numbers. Every control hands over a
  // string, and sending "2560" for longEdge fails to unmarshal into an int --
  // that is exactly how saving a template came to report
  // `cannot unmarshal string into Go struct field Template.resize.longEdge`.
  const RESIZE_NUMBERS = new Set(['longEdge', 'shortEdge', 'width', 'height',
    'maxWidth', 'maxHeight', 'percent', 'multipleOf']);

  function resizeValue(key, v) {
    return RESIZE_NUMBERS.has(key) ? Number(v || 0) : v;
  }

  /**
   * Which fields a selector turns on.
   *
   * A resize mode that only reads 长边 should not offer six more boxes that do
   * nothing -- they look usable and are silently ignored, which is how a template
   * ends up "configured" with values that never reach the command. Each rule maps
   * the controlling field's value to the fields it enables; a field named by more
   * than one rule is enabled only when every rule that mentions it allows it.
   *
   * refreshEnabled() applies these by toggling `disabled` rather than re-rendering
   * the form: a full re-render on every mode switch would steal the focus and cut
   * off whatever was being typed.
   */
  const ENABLED_BY = {
    resizeMode: {
      keep: ['algorithm', 'multipleOf'],
      longedge: ['longEdge', 'onlyLarger', 'algorithm', 'multipleOf'],
      shortedge: ['shortEdge', 'onlyLarger', 'algorithm', 'multipleOf'],
      exact: ['width', 'height', 'padToTarget', 'padColor', 'algorithm', 'multipleOf'],
      fit: ['maxWidth', 'maxHeight', 'algorithm', 'multipleOf'],
      percent: ['percent', 'onlyLarger', 'algorithm', 'multipleOf'],
    },
    videoMode: {
      encode: ['videoCodec', 'rateControl', 'crf', 'videoBitrate', 'maxRate', 'bufSize',
        'preset', 'tune', 'profile', 'level', 'pixFmt', 'fps'],
      // Copying or dropping the video stream leaves nothing to configure: every
      // encoder option is skipped, and the -vf chain (fps included) never runs.
      copy: [],
      disable: [],
    },
    rateControl: {
      crf: ['crf', 'maxRate', 'bufSize'],
      qp: ['crf', 'maxRate', 'bufSize'],
      bitrate: ['videoBitrate', 'maxRate', 'bufSize'],
    },
    audioMode: {
      encode: ['audioCodec', 'audioBitrate', 'audioChannels', 'sampleRate'],
      copy: [],
      disable: [],
    },
    // The destination only means anything once the action moves or copies the
    // file; 「留在原处」 would leave every one of these fields silently ignored.
    exAction: {
      keep: [],
      move: ['exMode', 'exDir', 'exPrefix', 'exSuffix', 'exKeepTree', 'exPattern', 'exOverwrite'],
      copy: ['exMode', 'exDir', 'exPrefix', 'exSuffix', 'exKeepTree', 'exPattern', 'exOverwrite'],
    },
    fAction: {
      keep: [],
      move: ['fMode', 'fDir', 'fPrefix', 'fSuffix', 'fKeepTree', 'fRenamePattern', 'fOverwrite'],
      copy: ['fMode', 'fDir', 'fPrefix', 'fSuffix', 'fKeepTree', 'fRenamePattern', 'fOverwrite'],
    },
    prErrorAction: {
      keep: [], mark: [],
      move: ['prErrorMode', 'prErrorDir', 'prErrorPrefix', 'prErrorSuffix', 'prErrorKeepTree', 'prErrorPattern'],
      copy: ['prErrorMode', 'prErrorDir', 'prErrorPrefix', 'prErrorSuffix', 'prErrorKeepTree', 'prErrorPattern'],
    },
    prWarningAction: {
      keep: [], mark: [],
      move: ['prWarningMode', 'prWarningDir', 'prWarningPrefix', 'prWarningSuffix',
        'prWarningKeepTree', 'prWarningPattern'],
      copy: ['prWarningMode', 'prWarningDir', 'prWarningPrefix', 'prWarningSuffix',
        'prWarningKeepTree', 'prWarningPattern'],
    },
    // 一段目录的哪几个框有意义，由「输出方式」决定：目录只对「自定义目录」有用，
    // 前缀 / 后缀只对「同级目录」有用，「原目录」一个都用不上。灰掉而不是藏起来
    // —— 藏起来的话用户得先猜"我这一版还有没有别的东西可填"。
    //
    // 主输出段没有动作开关在上面，所以这里只列出它那一份；`allowed` 里没有的字段
    // 谁都不去动它。
    //
    // 「保留目录结构」在自定义目录和「同级目录」下都有意义：这两种方式的落点在源
    // 目录**上方**，文件所在的那几层要靠它接回去。「同级顶层目录」的锚就是文件自己
    // （相对自己是空路径），「原目录」的落点也是源目录本身 —— 两者都靠
    // dirSpecHasRoom 判掉，不必在这里各写一遍。
    //
    // 两个同级的前缀 / 后缀字段是一样的（只有锚不同），所以下面每个 sibling 都
    // 跟着配一份 siblingTop，漏一个就是那一段的两个框在该方式下灰着。
    outMode: {
      same: [],
      custom: ['outDir', 'outKeepTree'],
      sibling: ['outPrefix', 'outSuffix', 'outKeepTree'],
      siblingTop: ['outPrefix', 'outSuffix'],
    },
    exMode: {
      same: [],
      custom: ['exDir', 'exKeepTree'],
      sibling: ['exPrefix', 'exSuffix', 'exKeepTree'],
      siblingTop: ['exPrefix', 'exSuffix'],
    },
    fMode: {
      same: [],
      custom: ['fDir', 'fKeepTree'],
      sibling: ['fPrefix', 'fSuffix', 'fKeepTree'],
      siblingTop: ['fPrefix', 'fSuffix'],
    },
    prErrorMode: {
      same: [],
      custom: ['prErrorDir', 'prErrorKeepTree'],
      sibling: ['prErrorPrefix', 'prErrorSuffix', 'prErrorKeepTree'],
      siblingTop: ['prErrorPrefix', 'prErrorSuffix'],
    },
    prWarningMode: {
      same: [],
      custom: ['prWarningDir', 'prWarningKeepTree'],
      sibling: ['prWarningPrefix', 'prWarningSuffix', 'prWarningKeepTree'],
      siblingTop: ['prWarningPrefix', 'prWarningSuffix'],
    },
  };

  /** Grey out everything the current selections make irrelevant. */
  function refreshEnabled() {
    const allowed = new Map();
    for (const [ctrl, map] of Object.entries(ENABLED_BY)) {
      const box = formEl.querySelector(`[name="${ctrl}"]`);
      if (!box) continue;
      const on = new Set(Object.hasOwn(map, box.value) ? map[box.value] : []);
      for (const n of new Set([].concat(...Object.values(map)))) {
        allowed.set(n, allowed.has(n) ? allowed.get(n) && on.has(n) : on.has(n));
      }
    }
    for (const [n, ok] of allowed) {
      const c = formEl.querySelector(`[name="${n}"]`);
      if (!c) continue;
      c.disabled = !ok;
      const box = c.closest('.field') || c.closest('.check') || c.closest('.switch-row');
      if (box) box.classList.toggle('is-disabled', !ok);
    }
    // 「保留目录结构」是两个条件都得满足：动作得真的搬文件（allowed），而方式得
    // 不是「原目录」—— 那种方式的落点就是源目录本身，子树已经在那里了。
    // `allowed` 里没有这一项说明上面的动作开关没管它（主输出段），undefined 就是
    // "没人禁止"。
    for (const stem of ['out', 'f', 'ex', 'prError', 'prWarning']) {
      const tree = formEl.querySelector(`[name="${stem}KeepTree"]`);
      if (!tree) continue;
      const ok = allowed.get(`${stem}KeepTree`) !== false && !!dirSpecHasRoom(stem);
      tree.disabled = !ok;
      const row = tree.closest('.switch-row');
      if (row) row.classList.toggle('is-disabled', !ok);
    }
  }

  /**
   * 这一段的方式能不能长出子树。
   *
   * 「原目录」和「同级顶层目录」都不能：前者的落点就是源目录本身，后者的锚也是文件
   * 自己所在的目录（相对自己是空路径）—— 结构要么已经在那里，要么压根没有可以
   * 往回接的一段。另外两种方式的落点在源目录**上方**，文件所在的那几层要靠这个开关
   * 接回去；关掉的话整棵树的文件贴平到同一个目录里，重名就得靠 _1、_2 挡。
   */
  function dirSpecHasRoom(stem) {
    const box = formEl.querySelector(`[name="${stem}Mode"]`);
    return !!box && box.value !== 'same' && box.value !== 'siblingTop';
  }

  function onFieldChange(e) {
    applyFieldChange(e);
    refreshEnabled();
    schedulePathTest();
  }

  /* ------------------------------------------------------------ 路径测试 */

  // 每次请求一个序号：输入框是防抖的，用户改得快时可能有两条在路上，先发的那条
  // 后回来就会把新结果盖掉。
  let pathTestSeq = 0;
  let pathTestTimer = 0;

  /**
   * 改了规则就把测试重算一遍。
   *
   * 不重算的话，下面那两条路径是上一版规则的答案 —— 而它存在的全部意义就是"面板
   * 说的和真跑一遍一致"，一个会过期的答案比没有这个面板更糟。
   */
  function schedulePathTest() {
    clearTimeout(pathTestTimer);
    pathTestTimer = setTimeout(runPathTest, 250);
  }

  /**
   * 按屏幕上的规则算一遍那个输入文件会写到哪。
   *
   * 后端只按路径推算、不读文件，所以这件事可以跟着打字跑。空路径不是错误：用户刚
   * 清空输入框，结果区回到"等你填"就好 —— 弹一条红字只会像出了毛病。
   */
  async function runPathTest() {
    const out = formEl.querySelector('[data-role=path-test-out]');
    const input = formEl.querySelector('[data-role=path-test]');
    if (!out || !input) return;
    const path = input.value.trim();
    const seq = ++pathTestSeq;
    if (!path) {
      out.innerHTML = '<div class="pathtest__note">填一个输入文件，这里给出输出目录和输出文件。</div>';
      return;
    }
    try {
      const check = await ctx.api.previewPaths(draft || {}, path);
      if (seq === pathTestSeq) out.innerHTML = pathCheckHtml(check);
    } catch (e) {
      if (seq === pathTestSeq) out.innerHTML = pathCheckHtml(null, e?.message || String(e));
    }
  }

  /** 重画表单之后把测试路径放回去，并按新规则重算一遍。 */
  function restorePathTest(path) {
    const input = formEl.querySelector('[data-role=path-test]');
    if (!input || !path) return;
    input.value = path;
    runPathTest();
  }

  function applyFieldChange(e) {
    const n = e.target.name;
    const v = e.target.type === 'checkbox' ? e.target.checked : e.target.value;
    if (!n || !draft) return;

    const resizeFields = ['resizeMode', 'longEdge', 'shortEdge', 'width', 'height', 'maxWidth', 'maxHeight',
      'percent', 'onlyLarger', 'multipleOf', 'algorithm', 'padToTarget', 'padColor'];
    if (resizeFields.includes(n)) {
      draft.resize = draft.resize || {};
      const key = n === 'resizeMode' ? 'mode' : n;
      // Everything off a control arrives as a string, and Go's ResizeSpec wants
      // real numbers -- sending "2560" fails to unmarshal into an int, which is
      // how saving a template came to report `cannot unmarshal string into
      // Template.resize.longEdge`. Checkboxes already give a bool.
      draft.resize[key] = e.target.type === 'checkbox' ? v : resizeValue(key, v);
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
    // 一段目录的五个控件写的是同一段数据，所以合成一个入口：分成五支的话，
    // "改了前缀但方式没跟上"就成了只有用户能发现的 bug。控件名 -> DirSpec 字段的
    // 对应关系是把 sections.js 的 dirParts 反过来查，两边不可能各写一份。
    const stem = name.replace(/(Mode|Dir|Prefix|Suffix|KeepTree)$/, '');
    const dirKey = Object.fromEntries(Object.entries(dirParts(`${stem}Dir`)).map(([k, v]) => [v, k]));
    const withDir = (spec) => ({ ...dirSpecOf(spec), [dirKey[name]]: v });

    if (sec === 'output') {
      // Flat fields on the draft itself, named the same as the template's JSON keys.
      if (name in dirKey) draft.outDirSpec = withDir(draft.outDirSpec);
      else draft[name] = v;
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
    if (sec === 'existing') {
      switch (name) {
        case 'exAction': s.action = v; break;
        case 'exPattern': s.pattern = v; break;
        case 'exOverwrite': s.overwrite = v; break;
        default: s.dir = withDir(s.dir);
      }
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
        default: s.dir = withDir(s.dir);
      }
      return;
    }
    if (name === 'prErrorAction') s.errorAction = v;
    else if (name === 'prWarningAction') s.warningAction = v;
    else if (name === 'prErrorPattern') s.errorPattern = v;
    else if (name === 'prWarningPattern') s.warningPattern = v;
    else if (name.startsWith('prError')) s.errorDir = withDir(s.errorDir);
    else s.warningDir = withDir(s.warningDir);
  }

  function markDirty() {
    dirty = true;
    updateStatus();
  }

  /**
   * 左下角只说"有没有未保存的改动"。
   *
   * 原来这里还挂着"更新于 ……"的时间戳：保存的时间戳存在模板里，但除了这里没有第二个
   * 地方会显示它 —— 列表里看的是名字和摘要，右边看的是值。删掉之后 statusEl 只剩
   * 脏标记这一个职责，也就只需要在 markDirty 和保存成功时各写一次。
   */
  function updateStatus() {
    statusEl.innerHTML = dirty
      ? `${icon('dot', 'sm')} 有未保存的修改`
      : `${icon('checkCircle', 'sm')} 已保存`;
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
   * Right-click on a row. The native menu is gone with the frame, so the actions that
   * belong to *a* template rather than to the page live here: duplicate and delete.
   * Right-clicking a row that is not part of the current selection selects it first --
   * acting on a template the user has not highlighted is the fastest way to delete the
   * wrong one. Right-clicking **inside** the selection keeps the whole selection, so a
   * multi-row delete does not collapse to one row first.
   *
   * 多选时菜单只剩「删除 N 项」：勾一串出来不是为了"复制"—— 一个模板复制一份就多一份，
   * 复制一串只会得到一串几乎一样的模板，而"一次删掉它们"才是勾这一串的用途。
   */
  listEl.addEventListener('contextmenu', async (e) => {
    const row = e.target.closest('.tpl-row');
    if (!row) return;
    e.preventDefault();
    const t = ctx.state.templates.find((x) => x.id === row.dataset.id);
    if (!t) return;
    if (!selection.has(t.id)) {
      if (ctx.state.currentTemplateId !== t.id
        && dirty
        && !(await confirmDialog('放弃修改？', '当前模板有未保存的修改，切换后将丢失。', '放弃修改', true))) return;
      selection.click(visibleIds(), t.id, e);
      selectTemplate(t.id);
    }
    if (t.global) return;   // neither duplicating nor deleting the defaults is meaningful
    const many = deletableIds().length > 1;
    ctx.showContextMenu(e.clientX, e.clientY, [
      ...(many ? [{
        label: `删除 ${deletableIds().length} 个模板`,
        icon: 'trash',
        danger: true,
        onClick: () => deleteSelected(),
      }] : [
        { label: '复制模板', icon: 'copy', onClick: () => duplicateTemplate(t.id) },
        {
          label: `删除模板「${t.name}」`,
          icon: 'trash',
          danger: true,
          onClick: () => deleteSelected(),
        },
      ]),
    ]);
  });

  /** 勾着的那些里真正删得掉的（全局那一套删不掉，它在多选里只是这一项留着）。 */
  function deletableIds() {
    return selection.ids.filter((id) => id !== GLOBAL_ID);
  }

  /**
   * 删掉勾着的那些（只勾了一行就是删那一行）。
   *
   * 一次确认、一次往返：勾五下删五次会在列表上闪五轮，而用户要的是"这五个都没了"。
   * 勾选中混进「基本配置」那一套时只删其余的 —— 它是所有模板的默认值来源，删掉的话
   * 剩下的模板全都没有兜底值了。
   */
  async function deleteSelected() {
    const ids = deletableIds();
    if (!ids.length) {
      toast('「基本配置」不能删除', 'warning');
      return;
    }
    const one = ids.length === 1;
    const names = ids.map((id) => ctx.state.templates.find((t) => t.id === id)?.name).filter(Boolean);
    const title = one ? `删除模板「${names[0]}」？` : `删除 ${ids.length} 个模板？`;
    const text = one
      ? '此操作不可撤销。'
      : `此操作不可撤销：${names.slice(0, 6).join('、')}${names.length > 6 ? ' 等' : ''}`;
    if (!(await confirmDialog(title, text, '删除', true))) return;
    try {
      const gone = await ctx.api.deleteTemplates(ids);
      const rest = ctx.state.templates.filter((t) => !ids.includes(t.id));
      ctx.state.templates = rest;
      selection.prune(rest.map((t) => t.id));
      // 落在删掉的那一格上：用户刚删的是哪几个，它旁边的就是最该接着看的那一个。
      const at = ctx.state.templates.findIndex((t) => t.id === ctx.state.currentTemplateId);
      if (at < 0) {
        ctx.state.currentTemplateId = ctx.state.templates.find((t) => !t.global)?.id || '';
        draft = null; dirty = false;
        selectTemplate(ctx.state.currentTemplateId);
      } else {
        renderList();
      }
      ctx.notifyTemplatesChanged();
      toast(one ? `已删除「${names[0]}」` : `已删除 ${gone} 个模板`, 'success');
    } catch (err) {
      toast(err?.message || '删除失败', 'error');
    }
  }

  async function duplicateTemplate(id) {
    const t = await ctx.api.duplicateTemplate(id);
    // The backend broadcasts the whole list through templates:changed, so appending
    // here too would leave two copies of the same template in the sidebar.
    if (!ctx.state.templates.some((x) => x.id === t.id)) ctx.state.templates.push(t);
    ctx.state.currentTemplateId = t.id;
    selectTemplate(t.id);
    toast(`已复制为「${t.name}」`, 'success');
  }

  el.addEventListener('click', async (e) => {
    // 「基本配置」那一行和普通行长得不一样，它自己带 data-global；两行都要参与多选，
    // 所以先问"点在不在勾选里"，再决定是换行还是只改勾选。
    const row = e.target.closest('.tpl-row');
    if (row) {
      const id = row.dataset.id;
      if (id === ctx.state.currentTemplateId) {
        selection.click(visibleIds(), id, e);
        return;
      }
      if (dirty && !(await confirmDialog('放弃修改？', '当前模板有未保存的修改，切换后将丢失。', '放弃修改', true))) return;
      selection.click(visibleIds(), id, e);
      selectTemplate(id);
      return;
    }
    const gcard = e.target.closest('[data-global]');
    if (gcard) {
      if (dirty && !(await confirmDialog('放弃修改？', '当前模板有未保存的修改，切换后将丢失。', '放弃修改', true))) return;
      selectTemplate(gcard.dataset.global);
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
        resize: { mode: 'keep', multipleOf: 2, algorithm: '' },
        // Copy the audio by default. Most jobs only want to re-encode the video;
        // re-encoding audio as well costs time and cannot make it sound better.
        audioMode: 'copy', audioCodec: '', audioBitrate: '', fastStart: true,
        perf: null, existing: null, filter: null, problems: null, outputOverride: false,
      });
      // Same as duplicate: the backend broadcasts the new list, so only fall back to
      // pushing when the broadcast has not landed yet.
      if (!ctx.state.templates.some((x) => x.id === t.id)) ctx.state.templates.push(t);
      ctx.state.currentTemplateId = t.id;
      selectTemplate(t.id);
      toast('已新建模板', 'success');
    } else if (act === 'duplicate') {
      if (!draft) return;
      if (isGlobal()) { toast('全局模板是默认值来源，请直接新建模板', 'warning'); return; }
      await duplicateTemplate(draft.id);
    } else if (act === 'delete') {
      if (!draft) return;
      if (isGlobal()) { toast('「基本配置」不能删除', 'warning'); return; }
      selection.only(draft.id);
      await deleteSelected();
    } else if (act === 'save' || act === 'save-as') {
      if (!draft) return;
      if (act === 'save-as' && isGlobal()) { toast('全局模板不能另存为，请直接新建模板', 'warning'); return; }
      collectArgs('in'); collectArgs('out');
      const payload = { ...draft };
      // A section that is following the global template is saved empty, so the file
      // cannot disagree with the switch: turn the switch on later and the old values
      // would otherwise silently come back.
      for (const k of ['perf', 'existing', 'filter', 'problems']) {
        if (payload[k] == null) delete payload[k];
      }
      if (!isGlobal() && !payload.outputOverride) {
        for (const k of ['outDirSpec', 'outPattern']) delete payload[k];
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
        toast(payload.global
          ? '「基本配置」已保存，所有模板的默认值已更新'
          // 另存为报的是"多了一套"，保存报的是"这一套存好了"。两件事的落点不同，说成
          // 同一句的话，用户会以为「另存为新模板」只是把当前这套覆盖了。
          : act === 'save-as' ? `已另存为「${saved.name}」` : `模板已保存「${saved.name}」`, 'success');
      } catch (err) { toast(err.message || String(err), 'error'); }
    } else if (act === 'preview') {
      await previewCommand();
    }
  });

  // 搜完要把看不见的勾去掉：不剪的话，Shift 的范围和「删除 N 项」会拿着一串
  // 用户根本没看见、也没法再取消勾的行去操作。
  el.querySelector('[data-role=search]').addEventListener('input', (e) => {
    keyword = e.target.value.trim().toLowerCase();
    selection.prune(visibleIds());
    renderList();
  });

  el.addEventListener('keydown', (e) => {
    if (!(e.ctrlKey || e.metaKey) || e.key.toLowerCase() !== 'a') return;
    const tag = document.activeElement?.tagName;
    if (tag === 'INPUT' || tag === 'TEXTAREA' || tag === 'SELECT') return;
    e.preventDefault();
    selection.selectAll(visibleIds());
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
    selection.prune(ctx.state.templates.map((t) => t.id));
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
    // Send the draft itself, not its id: the id would come back with the *saved*
    // copy of the template, so every unsaved edit in the form would be missing
    // from the command the preview claims to describe.
    let plan;
    try {
      plan = await ctx.api.previewTemplate(draft, '');
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
