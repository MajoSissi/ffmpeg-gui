import {
  esc, field, switchInline, toast, confirmDialog, createListSelection,
} from '../ui.js';
import { icon } from '../icons.js';

/**
 * 「过滤」页。
 *
 * 它编辑的是**常驻设置**里那一套套方案（`store.FilterProfile`），不是"这一次添加要用
 * 的条件"：设一次之后，「添加文件」「添加文件夹」和拖入窗口三条路都会自动套用它。
 *
 * 一套方案里有**两组各自独立**的规则：
 *
 *  - **目录规则**按目录名挑要收哪几个目录（前缀 / 后缀 / 包含 / 通配符 + 收或排 +
 *    满足全部 + 含子目录）。
 *  - **文件规则**按文件名挑要收哪几个文件。形状一样，但没有「含子目录」—— 文件名
 *    没有"往下收多深"这回事。
 *
 * 分开是因为它们回答的是两个问题："这个文件夹里往下收哪几个目录"和"收进来的文件里再
 * 要哪几个"。合成一份条件会让两边都写不准，而用户没法从界面上看出是哪一边错了。
 *
 * 版面上左边是方案表（搜索 / 新建 / 多选删除），右边是「基本信息」+ 两组规则 + 一个
 * "试一个文件夹"。名称和说明**在右边**而不是在左边那一行上：它们是这份方案自己的
 * 字段，和模板那边同一个位置；列表那一行只负责"哪一套"和"它在做什么"。
 * 判定规则**全部在后端**（`engine.ScanFolder` → `store.TakeName`）：页面自己再实现
 * 一套，就是"页面上说的"和"真跑一遍会做的"分家的第一步。
 *
 * 任务页上那个下拉换的就是此刻在用的那一套（选中就落盘，下次打开从它开始）；那一列
 * 最上面还有一颗「不使用过滤」，它只在本次运行里 —— 见 `App.SetActiveFilter`。
 */

const MODES = [
  { value: 'prefix', label: '前缀' },
  { value: 'suffix', label: '后缀' },
  { value: 'contains', label: '包含' },
  { value: 'glob', label: '通配符' },
];

// 摘要里念条件的那个词，只能出自上面这张表：下拉里的选项和摘要里的说法要是各写一份，
// 有一项改漏了，界面上就会出现一个念不出来的条件（"通配符 ffmpeg__*" 变成 "包含 …"）。
// 查不到给「条件」而不是空串 —— 老设置文件里出现一个现在不认识的 mode 时，摘要要还能念。
const MODE_LABEL = Object.fromEntries(MODES.map((m) => [m.value, m.label]));

const PLACEHOLDER = {
  prefix: '例如 ffmpeg__',
  suffix: '例如 __h265',
  contains: '例如 h265',
  glob: '例如 ffmpeg__*',
};

/**
 * 一组规则上的开关：标题、`data-role` 后缀、读值时的键名全出自这类表。
 *
 * 标题只写"这一项是什么"（完整意思在 title 里），所以是「排除」而不是"排除符合条件的
 * 目录"：一排五六个字的东西挤在一行里，谁也不会读完，反而看不出哪一颗是哪一颗。
 *
 * `flip` 是**控件方向**和**字段方向**不一致的那一颗：字段叫 `topOnly`（为了让老设置
 * 文件的零值等于"含子目录"，见 `store.DirFilter`），而页面上照用户的说法写「含子目录」
 * —— 开 = 往下收。两边各写一套必然有一边反着，所以放在同一张表里一个 `flip` 说清。
 */
const DIR_SWITCHES = [
  { key: 'enabled', label: '启用', tip: '关掉就整个文件夹照收，条件还留着' },
  { key: 'exclude', label: '排除', tip: '命中的目录不要，连它下面的子目录一起' },
  { key: 'matchAll', label: '满足全部', tip: '关掉 = 命中任意一条就算' },
  { key: 'topOnly', label: '含子目录', tip: '关掉只收你添加的那一层', flip: true, free: true },
];

const FILE_SWITCHES = [
  { key: 'enabled', label: '启用', tip: '关掉就所有文件照收，条件还留着' },
  { key: 'exclude', label: '排除', tip: '命中的文件不要' },
  { key: 'matchAll', label: '满足全部', tip: '关掉 = 命中任意一条就算' },
];

/** 字段值 → 控件上的勾。 */
const swChecked = (s, v) => (s.flip ? !v : v);

/** 控件上的勾 → 字段值。 */
const swField = (s, on) => (s.flip ? !on : on);

/** 不跟「启用」走的开关：「含子目录」管的是往下收多深，过滤关掉时同样算数。 */
const freeSwitch = (key) => DIR_SWITCHES.some((s) => s.key === key && s.free);

let seq = 0;

function condRow(mode, value, off) {
  const dis = off ? ' disabled' : '';
  return `
    <div class="fscan__row" data-role="cond">
      <select class="select" data-role="mode"${dis}>
        ${MODES.map((m) => `<option value="${m.value}"${m.value === mode ? ' selected' : ''}>${m.label}</option>`).join('')}
      </select>
      <input class="input mono" data-role="value" value="${esc(value)}" placeholder="${esc(PLACEHOLDER[mode] || '')}"${dis}>
      <button class="btn btn--text btn--icon" data-act="drop-cond" title="删除这条条件">${icon('close')}</button>
    </div>`;
}

/**
 * 一套方案会收什么，一句话。
 *
 * 两组分开念，中间用「＋」连起来：它们说的是两件事，用「·」连成一串会读成"这些条件
 * 同时作用在同一个东西上"。开关关掉的那组也要把条件念出来（`已暂停 · 前缀 cache`）：
 * 只写"关闭"的话，用户记不住自己是不是设过一套条件，也想不到打开就能用。
 *
 * 列表那一行的副标题优先用方案自己的**说明** —— 说明是写给人看的，条件摘要不是：
 * 两套方案只差一个条件的先后顺序时，条件摘要上看着一模一样，而那多半不是用户区分
 * 它们的方式。
 */
export function profileSummary(profile) {
  if (profile?.description) return profile.description;
  const dirs = groupText(profile?.dirs, {
    paused: '已暂停',
    reverse: '排除',
    extra: (r) => (r?.topOnly ? '只收这一层' : ''),
  });
  const files = groupText(profile?.files, { paused: '已暂停', reverse: '排除' });
  const parts = [];
  if (dirs) parts.push(`目录：${dirs}`);
  if (files) parts.push(`文件：${files}`);
  return parts.length ? parts.join('　＋　') : '未设置';
}

/** 一组规则的摘要文字。没有条件时给空串 —— 调用方按"两组都空"说「未设置」。 */
function groupText(rules, { paused, reverse, extra }) {
  if (!rules) return '';
  const items = (rules.filters || [])
    .map((c) => ({ mode: c?.mode || 'contains', value: String(c?.value ?? '').trim() }))
    .filter((c) => c.value !== '')
    .map((c) => `${MODE_LABEL[c.mode] || '条件'} ${c.value}`);
  if (!items.length) return '';
  const on = !!rules.enabled;
  const text = items.join(' · ');
  const extraText = extra ? extra(rules) : '';
  const head = (on ? '' : `${paused} · `)
    + (rules.exclude ? `${reverse} · ` : '')
    + (extraText ? `${extraText} · ` : '');
  return head + (text.length > 44 ? `${text.slice(0, 43)}…` : text);
}

/**
 * 一组规则现在会不会真的收窄东西 —— 镜像 `store.NameRules.Active`。
 *
 * 开关和"有非空条件"缺一不可：两边各判一次的话，页面上说着"已启用"而引擎按"不过滤"
 * 跑，是最难解释的一种不一致。
 */
export function rulesActive(r) {
  return !!r?.enabled
    && (r?.filters || []).some((c) => String(c?.value ?? '').trim() !== '');
}

export function createFiltersView(ctx) {
  const el = document.createElement('section');
  el.className = 'page';
  el.innerHTML = `
    <div class="split fscan-page">
      <aside class="tpl-list">
        <div class="tpl-list__head">
          <div class="tpl-list__search">
            <input class="input" placeholder="搜索方案" data-role="search">
            <button class="btn btn--tonal btn--icon" data-act="new-profile" title="新建方案">${icon('add')}</button>
          </div>
        </div>
        <div class="tpl-list__items" data-role="list"></div>
      </aside>
      <div class="editor">
        <div class="editor__body" data-role="body"></div>
        <div class="editor__foot">
          <span class="hint" data-role="status"></span>
          <div class="spacer"></div>
          <button class="btn btn--text" data-act="duplicate-profile" data-role="copy-btn">${icon('copy')}复制</button>
          <button class="btn btn--text btn--danger" data-act="drop-profile" data-role="del-btn">${icon('trash')}删除</button>
          <button class="btn btn--tonal" data-act="save-as" data-role="saveas-btn">${icon('copy')}另存为新方案</button>
          <button class="btn btn--filled" data-act="save" data-role="save-btn">${icon('save')}保存</button>
        </div>
      </div>
    </div>`;

  const listEl = el.querySelector('[data-role=list]');
  const searchEl = el.querySelector('[data-role=search]');
  const bodyEl = el.querySelector('[data-role=body]');
  const statusEl = el.querySelector('[data-role=status]');

  let list = [];       // 方案表，来自后端
  let keyword = '';    // 搜索框里的字，小写
  let inUse = '';      // 此刻在用的是哪套（任务页那个下拉选的）
  let draft = null;    // 页面上正在编辑的那一份
  let selected = '';   // 它叫什么（改之前）
  let testDir = '';
  let latest = null;
  // 打开/切换时的那份快照。改动判断靠它，所以"取消"永远是"回到进来时的样子"，不是
  // "回到上一次保存时的样子"。
  let originalKey = '';

  const dirty = () => !!draft && JSON.stringify(draft) !== originalKey;

  /**
   * 列表多选。勾着的和"正在编辑的"分开存：右键菜单里的「复制」「删除」作用在后者身上，
   * 「删除 N 项」作用在整串上。手势和判定都在 `ui.js` 的 `createListSelection` 里，
   * 和模板页共用同一句。
   */
  const selection = createListSelection(() => renderList());

  /** 当前可见的行 id，Shift 的范围按它算（搜过的时候按筛出来的那些）。 */
  const visibleNames = () => list
    .filter((p) => !keyword
      || p.name.toLowerCase().includes(keyword)
      || profileSummary(p).toLowerCase().includes(keyword))
    .map((p) => p.name);

  /* -------------------------------------------------------------- 画方案表 */

  /**
   * 方案表。一行一套：名字 + 说明（没写说明时退回条件摘要）。
   *
   * 用的是模板页那套行（`.tpl-row` / `.tpl-item`），不是侧栏那种导航项：两页做的是同一
   * 件事 —— 左列挑一个、右列编辑它 —— 行长得不一样只会让人以为"过滤方案"是另一种东西。
   *
   * 勾选高亮和"正在编辑"高亮**分两件事**（`.is-checked` / `.is-active`）：多选出来的
   * 一串里只有最后点的那一行在右边显示着，而整串都要能被看出是选中的。
   */
  function renderList() {
    const items = visibleNames();
    listEl.innerHTML = items.map((name) => {
      const p = list.find((x) => x.name === name);
      const cls = [
        'tpl-row',
        selection.has(name) ? 'is-checked' : '',
        p.name === selected ? 'is-active' : '',
      ].filter(Boolean).join(' ');
      return `<div class="${cls}" data-name="${esc(name)}">
        <button class="tpl-item">
          <span class="tpl-item__name">${esc(p.name)}</span>
          <span class="tpl-item__desc" title="${esc(profileSummary(p))}">${esc(profileSummary(p))}</span>
        </button>
      </div>`;
    }).join('') || '<div class="hint" style="padding:14px">没有匹配的方案</div>';
  }

  /* ---------------------------------------------------------------- 一组规则 */

  /**
   * 画一组规则：开关一排、条件若干、右边一颗「添加」。
   *
   * @param {'dirs'|'files'} role 这一组在方案里的字段名，同时是 `data-group`
   * @param {string} title
   * @param {string} desc 这一组管什么，一句话
   * @param {Array} switches
   */
  function groupHtml(role, title, desc, switches) {
    return `
      <div class="section" data-group="${role}">
        <div class="section__head">${icon('filter', 'sm')}<h3>${esc(title)}</h3>
          <div class="spacer"></div>
          <span class="hint">${esc(desc)}</span></div>
        <div class="switch-strip" data-role="opts"></div>
        <div class="fscan__condsec">
          <div class="fscan__conds" data-role="conds"></div>
          <button class="btn btn--outline" data-act="add-cond" title="再加一条条件">${icon('add')}添加</button>
        </div>
      </div>`;
  }

  function paintBody() {
    const dirs = draft.dirs || {};
    const dEnough = rulesActive(dirs);
    // 「含子目录」不跟「启用」灰掉：它管的是往下收多深，过滤关掉时照样算数。
    bodyEl.innerHTML = `
      <div class="section">
        <div class="section__head">${icon('layers', 'sm')}<h3>基本信息</h3>
          <div class="spacer"></div>
          <span class="hint">名称就是方案的标识，改名按「保存」</span></div>
        <div class="grid grid--2">
          ${field('方案名称', `<input class="input" data-role="pname" value="${esc(draft.name || '')}" placeholder="例如：只收转码中间产物" spellcheck="false">`)}
          ${field('说明', `<input class="input" data-role="pdesc" value="${esc(draft.description || '')}" placeholder="一句话说明这套在做什么">`, '会显示在左侧列表里；不参与筛选判断', 'span-2')}
        </div>
      </div>
      ${groupHtml('dirs', '目录规则', '挑要收哪几个文件夹', DIR_SWITCHES)}
      ${groupHtml('files', '文件规则', '挑要收哪几个文件', FILE_SWITCHES)}
      <div class="section">
        <div class="section__head">${icon('search', 'sm')}<h3>试一个文件夹</h3>
          <div class="spacer"></div>
          <span class="hint">只推算，不动任何文件</span></div>
        <div class="fscan__test">
          <input class="input mono" data-role="testdir" spellcheck="false"
            placeholder="目录路径" autocomplete="off" value="${esc(testDir)}">
          <button class="btn btn--outline" data-act="pick-test" title="从资源管理器里选择文件夹">选择</button>
        </div>
        <div class="fscan__out" data-role="out"></div>
      </div>`;

    paintGroup('dirs', DIR_SWITCHES);
    paintGroup('files', FILE_SWITCHES);
    if (!dEnough && draft.dirs?.exclude) {
      // 方向留着但条件不生效时说一句：不然页面上那颗「排除」看起来正在起作用。
      statusEl.textContent = '目录规则没在筛（开关关着或没填条件），方向不参与。';
    } else {
      statusEl.textContent = dirty() ? '有未保存的改动' : '';
    }
    schedule(0);
  }

  function groupEl(role) {
    return bodyEl.querySelector(`[data-group=${role}]`);
  }

  function paintGroup(role, switches) {
    const g = groupEl(role);
    if (!g) return;
    const rules = draft[role] || {};
    const off = !rules.enabled;

    g.querySelector('[data-role=opts]').innerHTML = switches
      .map((s) => switchInline(s.label, '', swChecked(s, rules[s.key]), s.tip, `data-role="sw-${s.key}"`))
      .join('');

    const rows = (rules.filters || [])
      .map((f) => ({ mode: f?.mode || 'contains', value: String(f?.value ?? '') }));
    // 一条条件都没有时给一条空行：页面打开就该有个可以打字的地方，而不是零行 + 一颗
    // 「添加」。
    if (!rows.length) rows.push({ mode: 'contains', value: '' });
    const conds = g.querySelector('[data-role=conds]');
    conds.innerHTML = rows.map((r) => condRow(r.mode, r.value, off)).join('');
    // 只剩一条时不显示删除按钮：删到最后一条就没有条件，"空面板"该由那一条空行表示，
    // 而不是由零条表示。
    conds.querySelectorAll('[data-act=drop-cond]').forEach((b) => { b.hidden = rows.length === 1; });

    g.querySelector('[data-act=add-cond]').disabled = off;
    // 除「启用」和「含子目录」之外的开关依赖"有条件"：条件一条都不填时，方向和满足度
    // 没有意义。
    g.querySelectorAll('[data-role^=sw-]').forEach((box) => {
      const key = box.dataset.role.slice(3);
      if (key === 'enabled' || freeSwitch(key)) return;
      box.disabled = off;
      box.closest('.switch-inline').classList.toggle('is-disabled', off);
    });
  }

  /** 把一组规则从页面上读回来。行只在增删时重画，输入走事件。 */
  function readGroup(role) {
    const g = groupEl(role);
    const rules = { ...(draft[role] || {}) };
    if (!g) return rules;
    rules.filters = [...g.querySelectorAll('[data-role=cond]')].map((row) => ({
      mode: row.querySelector('[data-role=mode]').value,
      value: row.querySelector('[data-role=value]').value,
    }));
    return rules;
  }

  /**
   * 把整张表单读回草稿。
   *
   * 名称和说明**原样**收进来（不 trim）：`dirty()` 拿它和进来时的快照比，trim 过就等于
   * "多打了一个空格也算改动"；真正要提交的那一份由后端 `Normalize` 收拾。
   */
  function readForm() {
    draft.dirs = readGroup('dirs');
    draft.files = readGroup('files');
    draft.name = bodyEl.querySelector('[data-role=pname]')?.value ?? draft.name;
    draft.description = bodyEl.querySelector('[data-role=pdesc]')?.value ?? draft.description;
    return draft;
  }

  /* ---------------------------------------------------------------- 试目录 */

  function paintOut(pv, err = '') {
    const out = bodyEl.querySelector('[data-role=out]');
    if (!out) return;
    out.classList.remove('is-stale');
    latest = pv && pv.totalFiles > 0 ? pv : null;
    const box = err ? `<div class="fscan__err">${icon('warning')}<span>${esc(err)}</span></div>` : '';
    if (!pv) {
      out.innerHTML = box;
      return;
    }

    const total = pv.dirsTotal || 0;
    const head = pv.exclude
      ? `排除 <b>${total}</b> 个目录 · 收 <b>${pv.totalFiles}</b> 个文件`
      : pv.filtering
        ? `收 <b>${total}</b> 个目录 · <b>${pv.totalFiles}</b> 个文件`
        : `<b>${pv.totalFiles}</b> 个文件`;
    // 文件规则筛掉了多少。没有这一行，"规则写着排除、文件数却没变"和"规则根本没生效"
    // 在页面上长得一模一样 —— 而这个数字把两件事分开。
    const dropped = pv.filteredFiles > 0
      ? `<span class="hint">文件规则筛掉 ${pv.filteredFiles} 个</span>`
      : '';

    const list = pv.dirs.length
      ? `<div class="fscan__list">${pv.dirs.map((d) => `
          <div class="fscan__item">
            <span class="mono fscan__name" title="${esc(d.dir)}">${esc(d.rel || d.name)}</span>
            <span class="fscan__count">${d.files} 个文件</span>
          </div>`).join('')}
          ${total > pv.dirs.length ? `<div class="fscan__more">还有 ${total - pv.dirs.length} 个目录未列出</div>` : ''}
        </div>`
      : `<div class="fscan__empty">${pv.filtering
        ? (pv.exclude ? '没有被排除的目录。' : '没有符合条件的目录。') : ''}</div>`;

    out.innerHTML = `${box}<div class="fscan__head">${head}${dropped}</div>${list}`;
  }

  function paintLoading() {
    const out = bodyEl.querySelector('[data-role=out]');
    if (!out) return;
    if (!out.innerHTML.trim()) out.innerHTML = '<div class="fscan__empty">正在查找…</div>';
    out.classList.add('is-stale');
  }

  let timer = 0;
  function schedule(delay = 180) {
    clearTimeout(timer);
    const out = bodyEl.querySelector('[data-role=out]');
    // 没有可预览的东西就什么都不显示：没有目录时无从算起。
    if (!testDir || !out) {
      latest = null;
      if (out) {
        out.classList.remove('is-stale');
        out.innerHTML = '';
      }
      return;
    }
    paintLoading();
    timer = setTimeout(async () => {
      const mine = ++seq;
      try {
        const pv = await ctx.api.previewFolderScan(scanOf());
        // 打字快的时候会有两条在路上，先发的后回来就会把新结果盖掉。
        if (mine !== seq) return;
        paintOut(pv);
      } catch (e) {
        if (mine !== seq) return;
        paintOut(null, e?.message || '预览失败');
      }
    }, delay);
  }

  /**
   * 拿页面上这套去问后端"加进来会收什么"。
   *
   * `recursive` 是**已经算完的**往下收多深（`!topOnly`）：预览没有"调用方"这一层，所以
   * 按"用户是递归添加的"算 —— 真加入时调用方给的是不是递归，只会让结果更窄。
   */
  const scanOf = () => {
    const d = readForm();
    return {
      dir: testDir,
      dirs: d.dirs,
      files: d.files,
      recursive: !d.dirs?.topOnly,
    };
  };

  /* ---------------------------------------------------------------- 存取 */

  function adopt(st) {
    if (!st) return;
    list = st.profiles || [];
    inUse = st.active || '';
    ctx.applyFilterState?.(st);
  }

  function pick(name) {
    const p = list.find((x) => x.name === name);
    if (!p) {
      // 一套都没有（列表被搜索词筛空不算，这是真的一套都没有）：右边得说清楚，而不是
      // 停在上一次编辑的那套的条件上 —— 那些条件属于一套已经不存在的方案。
      selected = ''; draft = null; originalKey = '';
      renderList();
      bodyEl.innerHTML = '<div class="empty">从左侧选择一个方案开始编辑</div>';
      statusEl.textContent = '';
      paintFoot();
      return;
    }
    selected = p.name;
    // 深拷一份当草稿：直接改后端交回来的那份，会让"取消/放弃"变成不可能，也会让
    // `dirty()` 永远为假。键的顺序和后端结构体一致，`dirty()` 才不会平白报脏。
    draft = JSON.parse(JSON.stringify({
      name: p.name, description: p.description || '', dirs: p.dirs || {}, files: p.files || {},
    }));
    originalKey = JSON.stringify(draft);
    if (!ctx.state.filter) ctx.state.filter = {};
    renderList();
    paintBody();
    statusEl.textContent = '';
    paintFoot();
  }

  /** 勾上并编辑这一行。新建、删除、切换都走它，多选里"当前"那一行也归它管。 */
  function jumpTo(name) {
    selection.only(name);
    pick(name);
  }

  /**
   * 换到另一套去编辑。
   *
   * **确认放在动多选之前**：用户答"不放弃"的时候，勾选必须原样留着 —— 先勾上再问，
   * 问完发现不让切，列表上就多出一串他没打算留下的高亮。
   */
  async function leaveFor(name) {
    if (dirty() && !(await confirmDialog('放弃未保存的改动？', `「${selected}」上有改动还没保存。`, '放弃并切换', true))) return false;
    pick(name);
    return true;
  }

  /* ---------------------------------------------------------------- 事件 */

  listEl.addEventListener('click', async (e) => {
    const row = e.target.closest('.tpl-row');
    if (!row) return;
    const name = row.dataset.name;
    // 换行要问"要不要放弃改动"，而点**已经选中**的那一行不换行 —— 只改勾选。
    if (name === selected) {
      selection.click(visibleNames(), name, e);
      return;
    }
    if (dirty() && !(await confirmDialog('放弃未保存的改动？', `「${selected}」上有改动还没保存。`, '放弃并切换', true))) return;
    selection.click(visibleNames(), name, e);
    pick(name);
  });

  /** Ctrl+A 全选可见的那些行。焦点不在输入框里时才接管。 */
  el.addEventListener('keydown', (e) => {
    if (!(e.ctrlKey || e.metaKey) || e.key.toLowerCase() !== 'a') return;
    const tag = document.activeElement?.tagName;
    if (tag === 'INPUT' || tag === 'TEXTAREA' || tag === 'SELECT') return;
    e.preventDefault();
    selection.selectAll(visibleNames());
  });

  /**
   * 右键一套方案。
   *
   * 多选时菜单只剩「删除 N 项」：勾一串出来不是为了"复制"—— 一串规则各自复制一份
   * 会得到一串几乎一样的方案，而"一次删掉它们"才是勾这一串的用途。
   *
   * 和模板页同一张菜单的形状、同一套做法：先切换（切换会问"要不要放弃改动"），用户
   * 在那里点了取消就什么都不弹 —— 弹一张菜单、点下去才发现是作用在另一套上，比不弹更糟。
   */
  listEl.addEventListener('contextmenu', async (e) => {
    const row = e.target.closest('.tpl-row');
    if (!row) return;
    e.preventDefault();
    const name = row.dataset.name;
    if (!selection.has(name)) {
      if (name !== selected && !(await leaveFor(name))) return;
      selection.click(visibleNames(), name, e);
      pick(name);
    }
    const many = selection.ids.length > 1;
    ctx.showContextMenu(e.clientX, e.clientY, [
      ...(many ? [] : [
        { label: '复制方案', icon: 'copy', onClick: () => duplicateProfile() },
        { label: `删除方案「${name}」`, icon: 'trash', danger: true, onClick: () => deleteSelected() },
      ]),
      ...(many ? [{
        label: `删除 ${selection.ids.length} 套方案`,
        icon: 'trash',
        danger: true,
        onClick: () => deleteSelected(),
      }] : []),
    ]);
  });

  searchEl.addEventListener('input', (e) => {
    keyword = e.target.value.trim().toLowerCase();
    selection.prune(visibleNames());
    renderList();
  });

  bodyEl.addEventListener('input', (e) => {
    // 试目录那一段不在任何一组规则里（它自己单独一段），所以先单独认它：让它落到下面
    // 那个 `[data-group]` 判据上，它就永远进不来 —— 输入框里打得再热闹，结果区也一直
    // 是空的，而"空的"和"还没查"长得一模一样。
    if (e.target.dataset.role === 'testdir') {
      testDir = e.target.value.trim();
      schedule();
      return;
    }
    // 名称和说明是这份方案自己的字段，改它们和改条件一样是"在编辑"。
    if (e.target.dataset.role === 'pname' || e.target.dataset.role === 'pdesc') {
      readForm();
      statusEl.textContent = dirty() ? '有未保存的改动' : '';
      return;
    }
    const role = e.target.closest('[data-group]')?.dataset.group;
    if (!role) return;
    if (e.target.dataset.role === 'mode') {
      const row = e.target.closest('[data-role=cond]');
      row.querySelector('[data-role=value]').placeholder = PLACEHOLDER[e.target.value] || '';
    }
    if (draft) draft[role] = readGroup(role);
    statusEl.textContent = dirty() ? '有未保存的改动' : '';
    schedule();
  });
  bodyEl.addEventListener('change', (e) => {
    if (e.target.dataset.role === 'testdir') {
      testDir = e.target.value.trim();
      // Enter / 失焦不等防抖：用户已经说"就是它"了。
      schedule(0);
      return;
    }
    const role = e.target.closest('[data-group]')?.dataset.group;
    if (!role) return;
    if (e.target.dataset.role?.startsWith('sw-')) {
      const key = e.target.dataset.role.slice(3);
      const table = role === 'dirs' ? DIR_SWITCHES : FILE_SWITCHES;
      const s = table.find((x) => x.key === key);
      if (!s) return;
      const rules = readGroup(role);
      rules[key] = swField(s, e.target.checked);
      draft[role] = rules;
      // 开关变了整组条件的可用性跟着变 —— 直接重画，不用等防抖（用户点的是开关）。
      paintGroup(role, table);
      statusEl.textContent = dirty() ? '有未保存的改动' : '';
      schedule(0);
      return;
    }
    if (e.target.dataset.role === 'mode') schedule(0);
  });

  bodyEl.addEventListener('click', async (e) => {
    const btn = e.target.closest('[data-act]');
    if (!btn) return;
    const role = btn.closest('[data-group]')?.dataset.group;
    const act = btn.dataset.act;
    if (act === 'add-cond') {
      draft[role] = readGroup(role);
      draft[role].filters.push({ mode: 'contains', value: '' });
      paintGroup(role, role === 'dirs' ? DIR_SWITCHES : FILE_SWITCHES);
      groupEl(role).querySelector('[data-role=cond]:last-child [data-role=value]').focus();
      statusEl.textContent = '有未保存的改动';
    } else if (act === 'drop-cond') {
      const all = [...btn.closest('[data-role=conds]').querySelectorAll('[data-role=cond]')];
      const idx = all.indexOf(btn.closest('[data-role=cond]'));
      const rules = readGroup(role);
      if (idx >= 0) rules.filters.splice(idx, 1);
      draft[role] = rules;
      paintGroup(role, role === 'dirs' ? DIR_SWITCHES : FILE_SWITCHES);
      statusEl.textContent = '有未保存的改动';
      schedule(0);
    } else if (act === 'pick-test') {
      // 选了就立刻算：用户已经点完目录了，没有"还要再确认一下"的意思。
      btn.disabled = true;
      try {
        const dir = await ctx.api.pickDirectory();
        if (dir) {
          testDir = dir.trim();
          const input = bodyEl.querySelector('[data-role=testdir]');
          if (input) input.value = dir;
          schedule(0);
        }
      } catch (err) {
        paintOut(latest, err?.message || '选择目录失败');
      }
      btn.disabled = false;
    }
  });

  /**
   * 存这一套。
   *
   * 旧名字（`selected`）一定要一起交出去：名字是方案唯一的标识，后端只收到新名字的话，
   * "改名"在它眼里就是"多一套"—— 旧的还在，表里出现两行分不清谁是谁的东西。
   */
  async function saveProfile(extra = {}) {
    readForm();
    const name = String(extra.name ?? draft.name).trim();
    if (!name) {
      toast('方案要有名字', 'warning');
      return false;
    }
    try {
      const st = await ctx.api.saveFilterProfile(selected, { ...draft, name });
      adopt(st);
      jumpTo(name);
      toast(`方案已保存「${name}」`, 'success');
      return true;
    } catch (e) {
      statusEl.textContent = e?.message || '保存失败';
      toast(e?.message || '保存失败', 'error');
      return false;
    }
  }

  el.addEventListener('click', async (e) => {
    const btn = e.target.closest('[data-act]');
    if (!btn) return;
    const act = btn.dataset.act;
    if (act === 'save') {
      await saveProfile();
    } else if (act === 'save-as') {
      await saveAsNew();
    } else if (act === 'duplicate-profile') {
      await duplicateProfile();
    } else if (act === 'new-profile') {
      await createProfile();
    } else if (act === 'drop-profile') {
      await deleteSelected();
    }
  });

  /* ---------------------------------------------------------------- 增删改 */

  /**
   * 新建一套，直接落库、不弹窗。
   *
   * 和模板页的「新建」同一个逻辑：名字先给一个「新方案」，右侧面板打开、焦点落在名称
   * 框上，用户想改就改，不改直接按保存也说得通。弹窗会先把"我刚按了新建"这件事从屏幕
   * 上抹掉，而用户还在想叫什么。
   *
   * 从**空**开始，不是从当前这套复制：复制出来的那套看起来和原来一模一样，用户会以为
   * "新建"没生效。想复制有右键里的「复制方案」和页脚的「另存为新方案」。
   */
  async function createProfile() {
    const name = freeName('新方案');
    try {
      const st = await ctx.api.saveFilterProfile('', {
        name,
        description: '',
        dirs: { enabled: false, filters: [], matchAll: false, exclude: false, topOnly: false },
        files: { enabled: false, filters: [], matchAll: false, exclude: false },
      });
      adopt(st);
      jumpTo(name);
      toast('已新建方案', 'success');
      bodyEl.querySelector('[data-role=pname]')?.select();
    } catch (e) {
      toast(e?.message || '新建失败', 'error');
    }
  }

  /**
   * 复制**已保存**的那一份。
   *
   * 和「另存为新方案」不是同一件事：这一颗复制的是表里那份，右边还没保存的改动不算；
   * 那一颗存的是眼前这份。两个入口差在"我现在改的要不要一起带走"，所以都留着。
   *
   * 名字后面加「副本」而不是加序号：用户刚点完复制，下一步多半是改名，给他一个能直接
   * 认的名字比给他一个没重复的编号更有用 —— 撞名了才补序号。
   */
  async function duplicateProfile() {
    const src = list.find((p) => p.name === selected);
    if (!src) return;
    const name = freeName(`${src.name} 副本`);
    try {
      const st = await ctx.api.saveFilterProfile('', JSON.parse(JSON.stringify({ ...src, name })));
      adopt(st);
      jumpTo(name);
      toast(`已复制为「${name}」`, 'success');
    } catch (e) {
      toast(e?.message || '复制失败', 'error');
    }
  }

  /** 另存为新方案：把**眼前这份**（含没保存的改动）存成一个新的一套。 */
  async function saveAsNew() {
    if (!draft) return;
    readForm();
    const name = freeName(`${draft.name || '方案'} 副本`);
    try {
      const st = await ctx.api.saveFilterProfile('', JSON.parse(JSON.stringify({ ...draft, name })));
      adopt(st);
      jumpTo(name);
      toast(`已另存为「${name}」`, 'success');
    } catch (e) {
      toast(e?.message || '另存为失败', 'error');
    }
  }

  /**
   * 删掉勾着的那些（只勾了一行就是删那一行）。
   *
   * 一次确认、一次往返：勾五下删五次会在列表上闪五轮，而用户要的是"这五个都没了"。
   * 删完落在**此刻在用**的那一套上 —— 它一定还在（后端至少留一套），所以不用在剩下的
   * 里挑一条规则出来。
   */
  async function deleteSelected() {
    const names = selection.ids.filter(Boolean);
    if (!names.length) return;
    const one = names.length === 1;
    const title = one ? `删除方案「${names[0]}」？` : `删除 ${names.length} 套方案？`;
    const text = one
      ? '这一套的条件会一起删掉，其他方案不受影响。'
      : '这些方案的条件会一起删掉，别的方案不受影响。';
    if (!(await confirmDialog(title, text, '删除', true))) return;
    try {
      const st = await ctx.api.deleteFilterProfiles(names);
      adopt(st);
      const alive = st.profiles.map((p) => p.name);
      selection.prune(alive);
      jumpTo(alive.includes(st.active) ? st.active : alive[0] || '');
      toast(one ? `已删除「${names[0]}」` : `已删除 ${names.length} 套方案`, 'success');
    } catch (err) {
      toast(err?.message || '删除失败', 'error');
    }
  }

  /** 一个还没被占用的名字。副本撞名才补序号 —— 序号是兜底，不是常态。 */
  function freeName(base) {
    if (!list.some((p) => p.name === base)) return base;
    for (let i = 2; ; i += 1) {
      const n = `${base} ${i}`;
      if (!list.some((p) => p.name === n)) return n;
    }
  }

  /**
   * 页脚那几颗按钮都作用在"右边正在编辑的这一套"上，一套都没选中时它们没有对象 ——
   * 灰着比藏起来好：按钮的位置不会跳，而灰着的按钮本身就是在说"先选一套"。
   */
  function paintFoot() {
    const on = !!draft;
    for (const role of ['copy-btn', 'del-btn', 'saveas-btn', 'save-btn']) {
      const b = el.querySelector(`[data-role=${role}]`);
      if (b) b.disabled = !on;
    }
  }

  /* ---------------------------------------------------------------- 对外 */

  return {
    el,
    async mount() {
      if (!ctx.state.filter?.profiles?.length) {
        try {
          adopt(await ctx.api.filterState());
        } catch (e) {
          console.error(e);
        }
      } else {
        adopt(ctx.state.filter);
      }
      // 每次进来都从方案表重新拿一份草稿：离开这一页时那个确认框答应的是"放弃未保存的
      // 改动"，草稿留着的话，回来时那些"已放弃"的改动又原封不动地摆在那儿，看起来就像
      // 放弃没生效。没改过的草稿重拿一份是同样的内容，所以没有副作用。
      //
      // 选中的还是**上一次在编辑的那一套**（还在的话）：用户离开是想去别处看一眼，回来
      // 应该回到他刚站着的地方，而不是被丢回另一套。
      const names = list.map((p) => p.name);
      selection.prune(names);
      const fallback = names.includes(inUse) ? inUse : names[0] || '';
      jumpTo(names.includes(selected) ? selected : fallback);
    },
    /** 任务页换了方案：只更新列表上的那一个记号，不碰正在编辑的草稿。 */
    onFilterChanged() {
      const st = ctx.state.filter;
      if (!st) return;
      inUse = st.active || '';
      list = st.profiles || list;
      renderList();
    },
    hasUnsaved: () => dirty(),
  };
}
