/**
 * Shared renderers for the inheritable sections.
 *
 * "输出与命名 / 已处理过的文件 / 处理性能 / 筛选与转移 / 错误与警告" used to live
 * on the settings page. They now belong to templates, with the global template as
 * the default every other template inherits from. A regular template shows each
 * section behind a "与全局不同" switch: off means "follow", which on the wire is a
 * nil section (or, for the output naming, `outputOverride: false`).
 *
 * Being ordered by how often they are touched, the rarely-needed policy sections
 * sit at the bottom. Both editors go through these functions so a rule can never
 * be described one way on the global template and another way on a regular one.
 *
 * One rule on purpose: "与全局不同" is the *only* way to say "follow". The dropdowns
 * do not carry a "跟随全局设置" entry next to it -- two controls for one state is
 * how a panel ends up claiming to follow while the command uses something else.
 */
import { icon } from '../icons.js';
import { esc, field, selectHtml, switchRow, switchInline } from '../ui.js';

/** Section keys used for the "与全局不同" switch names, in display order. */
export const SECTIONS = ['perf', 'output', 'existing', 'filter', 'problems'];

/**
 * Merge a template with the global one, mirroring store.Template.Effective.
 *
 * 「与全局不同」是唯一的继承开关。关着时整段取全局的；打开之后字段留空就是留空
 * ——目录留空=源文件所在目录（ResolveDestDir），名称留空=源文件名（ResolveOutput）。
 * 段内没有第二层"留空即跟随"：同一件事说两遍只会让"我打开的到底是不是跟随"变成
 * 一个要靠提示文字回答的问题。
 *
 * The four pointer sections fall back whole, because 0 there means "no limit" rather
 * than "not configured".
 *
 * The UI needs this to describe what a job will actually do without asking the
 * backend, and it must stay in step with the Go side -- a preview that disagreed
 * with the plan would be worse than none.
 */
export function effective(t, g) {
  const tpl = t || {};
  const glob = g || {};
  const out = { ...tpl };
  if (!out.name) out.name = glob.name;
  if (out.outputOverride) {
    out.outDirSpec = dirSpecOf(out.outDirSpec);
    out.outPattern = out.outPattern || '';
  } else {
    // A follower ignores whatever the template still carries in the output fields.
    out.outDirSpec = dirSpecOf(glob.outDirSpec);
    out.outPattern = glob.outPattern || '';
  }
  for (const k of ['perf', 'existing', 'filter', 'problems']) {
    if (out[k] == null) out[k] = glob[k];
  }
  return out;
}

/**
 * A DirSpec as a plain object, whatever shape a draft or a payload carries.
 *
 * Mirrors store.DirSpec field for field. `mode` doubles as the blank case: an
 * unset mode writes next to the source file, which is what 「原目录」 says too.
 */
export function dirSpecOf(v) {
  const d = v || {};
  return {
    mode: String(d.mode || ''),
    dir: String(d.dir || ''),
    prefix: String(d.prefix || ''),
    suffix: String(d.suffix || ''),
    keepTree: !!d.keepTree,
  };
}

/**
 * The control names one destination is spread over.
 *
 * The five places that write a file somewhere each get a stem — `outDir`,
 * `fDir`, `exDir`, `prErrorDir`, `prWarningDir` — and every one of them answers
 * the same four questions, so the names are derived from the stem rather than
 * spelled out five times: a rule the editor and the write-back disagree about is
 * invisible until a field silently stops saving.
 */
export function dirParts(name) {
  const stem = String(name).replace(/Dir$/, '');
  return {
    mode: `${stem}Mode`, dir: name, prefix: `${stem}Prefix`,
    suffix: `${stem}Suffix`, keepTree: `${stem}KeepTree`,
  };
}

/** A blank action resolves to "leave it alone" in every section. */
function actionOr(value, blank) {
  return String(value || '').trim() ? value : blank;
}

/** 序号宽度在两组里各出现一次，说法必须一字不差。 */
const INDEX_DESC = '批次内序号；{index:3} 补零到三位（007），不写就是不补零；没有序号时留空';

const SECTION_META = {
  perf: { icon: 'speed', title: '处理性能' },
  output: { icon: 'folder', title: '输出与命名' },
  existing: { icon: 'history', title: '已处理过的文件' },
  filter: { icon: 'filter', title: '匹配条件' },
  problems: { icon: 'warning', title: '错误与警告' },
};

/**
 * A section header. On the global template the "与全局不同" switch is omitted --
 * there is nothing above it to differ from -- and `active` is irrelevant.
 */
export function sectionHead(key, active, { global = false, hint = '' } = {}) {
  const m = SECTION_META[key];
  return `<div class="section__head">${icon(m.icon, 'sm')}<h3>${m.title}</h3><div class="spacer"></div>
    ${hint ? `<span class="hint">${esc(hint)}</span>` : ''}
    ${global ? '' : switchInline('与全局不同', '', !!active, '关闭时跟随「全局模板」', `data-sec="${key}"`)}
  </div>`;
}

/** The one-line explanation shown while a section is following the global one. */
export function followHint(key, global) {
  const s = global || {};
  if (key === 'perf') {
    const c = s.perf || {};
    const th = c.threads > 0 ? ` · 每任务 ${c.threads} 核` : ' · 核数不限';
    return `跟随全局：同时 ${c.concurrency || 1} 个任务${th} · 失败重试 ${c.retryCount || 0} 次 · 日志级别 ${c.logLevel || 'warning'}`;
  }
  if (key === 'output') {
    return `跟随全局：输出${destLabel(s.outDirSpec)}，命名 ${s.outPattern || '{name}'}`;
  }
  if (key === 'existing') {
    const e = s.existing || {};
    return `跟随全局：已处理过的文件${relocatePhrase(e.action, e.dir)}`;
  }
  if (key === 'filter') {
    const f = s.filter || {};
    if (!filterActive(f)) return '跟随全局：全部文件都处理，没有排除条件';
    // Same wording rule as the toolbar chip: these bounds name what is EXCLUDED.
    // "小于 300 MB" here reads as a description of the kept files, which is the
    // opposite of what MinSizeMB does.
    const bits = [];
    if (f.minSizeMB > 0) bits.push(`排除 <${f.minSizeMB} MB`);
    if (f.maxSizeMB > 0) bits.push(`排除 >${f.maxSizeMB} MB`);
    if (f.minLongEdge > 0) bits.push(`排除长边 <${f.minLongEdge}`);
    if (f.maxLongEdge > 0) bits.push(`排除长边 >${f.maxLongEdge}`);
    if (f.minDuration > 0) bits.push(`排除时长 <${f.minDuration}s`);
    if (f.maxDuration > 0) bits.push(`排除时长 >${f.maxDuration}s`);
    if ((f.includeExts || []).length) bits.push(`仅 ${f.includeExts.join('/')}`);
    if ((f.excludeExts || []).length) bits.push(`排除扩展名 ${f.excludeExts.join('/')}`);
    return `跟随全局：${bits.join('、') || '没有排除条件'}；被排除的文件${relocatePhrase(f.action, f.dir)}`;
  }
  const p = s.problems || {};
  const ep = p.errorPattern ? `并改名为 ${p.errorPattern}` : '';
  const wp = p.warningPattern ? `并改名为 ${p.warningPattern}` : '';
  return `跟随全局：错误文件${relocatePhrase(p.errorAction, p.errorDir, true)}${ep}；`
    + `警告文件${relocatePhrase(p.warningAction, p.warningDir, true)}${wp}`;
}

/**
 * "移动到 D:\done" in one phrase.
 *
 * The verb and the destination are glued together here rather than at each call
 * site: destLabel already starts with 到, so a caller that writes "移动到" in
 * front of it produces "移动到到 …". `mark` is the one problem-file action that
 * is not a relocation, hence the flag.
 */
function relocatePhrase(action, dir, allowMark = false) {
  if (allowMark && action === 'mark') return '仅在记录中标记';
  const verb = action === 'copy' ? '复制' : action === 'move' ? '移动' : '';
  if (!verb) return '留在原处';
  return verb + destLabel(dir);
}

/**
 * 「到 <哪里>」 in words, for the follow summary and the toolbar chip.
 *
 * 同级目录 has no single path to print — the folder's own name is not known until
 * a file goes through it — so it is spelled out the way the rule is built instead
 * of guessing a name.
 */
function destLabel(spec) {
  const d = dirSpecOf(spec);
  if (d.mode === 'siblingTop') {
    return `到同级目录「${d.prefix}所在目录名${d.suffix}」（每个子目录各一个）`;
  }
  const keep = d.keepTree ? '（保留目录结构）' : '';
  if (d.mode === 'sibling') {
    return `到同级目录「${d.prefix}所添加的目录名${d.suffix}」${keep}`;
  }
  if (d.mode === 'custom' && d.dir) return `到 ${d.dir}${keep}`;
  return '到源文件所在目录';
}

function filterActive(f) {
  if (!f) return false;
  return (f.minSizeMB > 0) || (f.maxSizeMB > 0) || (f.minLongEdge > 0) || (f.maxLongEdge > 0)
    || (f.minDuration > 0) || (f.maxDuration > 0)
    || (f.includeExts || []).length > 0 || (f.excludeExts || []).length > 0;
}

/* ------------------------------------------------------------------ bodies */

/** 处理性能 — concurrency, CPU budget, retries, log level, priority, cleanup. */
export function perfBody(p, opts, cpus = 0) {
  const d = p || {};
  // cpus comes from runtime.NumCPU(). It is only 0 while the bootstrap payload
  // has not arrived yet; in that case leave the number out rather than print a
  // guess that looks authoritative.
  const autoCores = cpus > 0 ? `（本机 ${cpus} 核）` : '';
  return `
  <div class="grid grid--3">
    ${field('同时处理任务数', `<input class="input" type="number" min="1" max="16" name="pConcurrency" value="${d.concurrency ?? 1}">`,
      '显卡编码建议 1~2')}
    ${field('每个任务使用的 CPU 核心数', `<input class="input" type="number" min="0" max="64" name="pThreads" value="${d.threads ?? 0}">`,
      `0 = 交给 ffmpeg 自己决定${autoCores}`)}
    ${field('日志级别', selectHtml('pLogLevel', opts.logLevels || [], d.logLevel || 'warning'))}
    ${field('失败重试次数', `<input class="input" type="number" min="0" max="5" name="pRetryCount" value="${d.retryCount ?? 0}">`, '0 = 不重试')}
  </div>
  <div style="margin-top:12px">
    ${switchRow('降低进程优先级', '', 'pIdlePriority', !!d.idlePriority)}
    ${switchRow('失败时删除残缺输出', '', 'pDeleteOnFail', !!d.deleteOnFail)}
  </div>`;
}

/**
 * 匹配条件 — which files to skip, and what happens to the ones that are.
 *
 * The six bounds read as two rows of one scale rather than three pairs: 最小/最短/下限
 * on top, 最大/最长/上限 below, each column a range.
 *
 * Which way the numbers point is stated once, above the grid, instead of six
 * times in the field hints. Written per field it kept coming out as 「小于 100 MB」
 * under a bound that means 「排除 <100 MB」 -- the same words, the opposite
 * meaning, and the reader has no way to tell which one was meant.
 */
export function filterBody(f, opts) {
  const d = f || {};
  return `
  <p class="hint" style="margin:0 0 10px">下面填的都是<b>排除</b>条件：不满足的文件不处理。留 0 表示不限制。</p>
  <div class="grid grid--3">
    ${field('最小体积 (MB)', `<input class="input" type="number" min="0" step="1" name="fMinSizeMB" value="${d.minSizeMB ?? 0}">`)}
    ${field('最短时长 (秒)', `<input class="input" type="number" min="0" name="fMinDuration" value="${d.minDuration ?? 0}">`)}
    ${field('长边下限 (px)', `<input class="input" type="number" min="0" name="fMinLongEdge" value="${d.minLongEdge ?? 0}">`, '横竖屏都取较长的一边')}
  </div>
  <div class="grid grid--3" style="margin-top:10px">
    ${field('最大体积 (MB)', `<input class="input" type="number" min="0" step="1" name="fMaxSizeMB" value="${d.maxSizeMB ?? 0}">`)}
    ${field('最长时长 (秒)', `<input class="input" type="number" min="0" name="fMaxDuration" value="${d.maxDuration ?? 0}">`)}
    ${field('长边上限 (px)', `<input class="input" type="number" min="0" name="fMaxLongEdge" value="${d.maxLongEdge ?? 0}">`)}
  </div>
  <div class="grid grid--2" style="margin-top:10px">
    ${field('仅处理这些扩展名', `<input class="input mono" name="fIncludeExts" value="${esc((d.includeExts || []).join(','))}" placeholder="mp4,mkv,mov">`)}
    ${field('排除这些扩展名', `<input class="input mono" name="fExcludeExts" value="${esc((d.excludeExts || []).join(','))}" placeholder="webm,gif">`)}
  </div>
  <div class="section__head" style="border:0;padding:0;margin:18px 0 10px">
    ${icon('route', 'sm')}<h3 style="font-size:13px">被排除文件的处理</h3>
  </div>
  <div class="grid grid--2">
    ${field('处理方式', selectHtml('fAction', opts.filterActions || [], actionOr(d.action, 'keep')))}
    ${field('源文件新名称', `<input class="input mono" name="fRenamePattern" value="${esc(d.renamePattern || '')}" placeholder="{name}">`, '', 'span-2')}
  </div>
  ${dirField('fDir', d.dir, opts)}
  ${switchRow('覆盖同名文件', '关闭时自动追加 _1、_2', 'fOverwrite', !!d.overwrite)}`;
}

/**
 * 已处理过的文件 — what to do with a source this template has already processed.
 * Running the same template over the same folder twice is the case it exists for,
 * and re-encoding finished files is the thing worth avoiding.
 *
 * Deliberately the same shape as 「被排除文件的处理」, down to the wording:
 * 处理方式 + 输出目录 + 保留目录结构 + 源文件新名称 + 覆盖开关. The two differ only in
 * which file gets moved, and a rule that reads identically in both places is one
 * less thing to misremember.
 */
export function existingBody(e, opts) {
  const d = e || {};
  // A blank action is the plain "leave it where it is" case, so that -- not some
  // archive verb -- is what the select has to show.
  const action = actionOr(d.action, 'keep');
  return `
  <p class="hint" style="margin:0 0 12px">
    每处理完一个文件，都按下面的方式安置它的<b>源文件</b>（不用等第二次）。
    本次运行内再遇到同一个文件、同一个模板，就不再重新编码，只安置源文件。
  </p>
  <div class="grid grid--2">
    ${field('处理方式', selectHtml('exAction', opts.existingActions || [], action))}
    ${field('源文件新名称', `<input class="input mono" name="exPattern" value="${esc(d.pattern || '')}" placeholder="{name}">`, '移动 / 复制时保持原扩展名', 'span-2')}
  </div>
  ${dirField('exDir', d.dir, opts, '移走后队列里的路径不变')}
  ${switchRow('覆盖目标目录里的同名文件', '关闭时自动追加 _1、_2', 'exOverwrite', !!d.overwrite)}`;
}

/** 错误与警告 — the policy for files that failed or finished with complaints. */
export function problemsBody(p, opts) {
  const d = p || {};
  return `
  <div class="section__head" style="border:0;padding:0;margin:0 0 10px">
    ${icon('error', 'sm')}<h3 style="font-size:13px">错误文件</h3>
  </div>
  <div class="grid grid--2">
    ${field('处理方式', selectHtml('prErrorAction', opts.problemActions || [], actionOr(d.errorAction, 'keep')))}
    ${field('源文件新名称', `<input class="input mono" name="prErrorPattern" value="${esc(d.errorPattern || '')}" placeholder="{name}">`, '移动 / 复制时保持原扩展名', 'span-2')}
  </div>
  ${dirField('prErrorDir', d.errorDir, opts, '移走后队列里的路径不变')}
  <div class="section__head" style="border:0;padding:0;margin:18px 0 10px">
    ${icon('warning', 'sm')}<h3 style="font-size:13px">警告文件</h3>
  </div>
  <div class="grid grid--2">
    ${field('处理方式', selectHtml('prWarningAction', opts.problemActions || [], actionOr(d.warningAction, 'keep')))}
    ${field('源文件新名称', `<input class="input mono" name="prWarningPattern" value="${esc(d.warningPattern || '')}" placeholder="{name}">`, '移动 / 复制时保持原扩展名', 'span-2')}
  </div>
  ${dirField('prWarningDir', d.warningDir, opts, '移走后队列里的路径不变')}`;
}

/** 输出与命名 — the main output rule. */
export function outputBody(t, opts) {
  const d = t || {};
  return `
  <div class="grid grid--2">
    ${field('输出文件名称', `<input class="input mono" name="outPattern" value="${esc(d.outPattern || '')}" placeholder="{name}">`)}
    ${containerField(d, opts)}
  </div>
  ${dirField('outDir', d.outDirSpec, opts)}
  ${variablesNote()}`;
}

/**
 * 产物放哪 — one mode plus the fields that mode reads.
 *
 * 四种方式回答的是同一个问题（这个文件写到哪），所以它们共用一组控件和同一段
 * 说明，五处（主输出 + 四段搬迁）长得一模一样。哪种字段眼下有用由
 * refreshEnabled()（templates.js）按方式灰掉，不在这里判断：一个渲染时就写死的
 * disabled 没法发现上面那个下拉刚刚改过。
 *
 * 两个同级只差**锚**：sibling 量你添加的那个目录（整棵树的产物汇到一处），
 * siblingTop 量文件自己所在的那一层（每个子目录各出一个）。前缀 / 后缀两个框在
 * 两种方式下都是同一套拼法，所以它们一起亮、一起灰。
 *
 * 「留空」在这几个框里只有一个意思：最朴素的默认。段头那个「与全局不同」开关才是
 * 唯一的继承开关，字段级的回落等于在自己的段里又悄悄跟着全局，用户只能靠一行提示
 * 文字知道。
 */
function dirField(name, spec, opts, hint = '') {
  const p = dirParts(name);
  const d = dirSpecOf(spec);
  const text = ['留空 = 源文件同目录', hint].filter(Boolean).join(' · ');
  return `
  <div class="grid grid--3" style="margin-top:12px">
    ${field('输出方式', selectHtml(p.mode, opts.destModes || [], d.mode || 'same'))}
    ${field('前缀', `<input class="input mono" name="${p.prefix}" value="${esc(d.prefix)}" placeholder="例如 123_">`)}
    ${field('后缀', `<input class="input mono" name="${p.suffix}" value="${esc(d.suffix)}"
      placeholder="${esc(opts.defaultOutputSuffix || '_out')}">`)}
  </div>
  <div style="margin-top:10px">
    ${field('目录', `<div class="input-group">
      <input class="input mono" name="${p.dir}" value="${esc(d.dir)}" placeholder="例如 D:////Media">
      <button class="btn btn--outline btn--icon" data-act="pick-dir" data-target="${p.dir}">${icon('folderOpen')}</button>
    </div>`, text, 'span-2')}
  </div>
  <div style="margin-top:12px">${keepTreeSwitch(p.keepTree, spec)}</div>`;
}

/**
 * 保留目录结构 — rebuild the source's sub-directories under the resolved
 * directory. The sub-tree is the one relative to the directory the user added, so
 * a folder dropped in whole comes back out with the shape it went in.
 */
function keepTreeSwitch(name, spec) {
  return switchRow('保留目录结构', '补上源文件相对「添加目录」的子目录', name, dirSpecOf(spec).keepTree);
}

/** 输出格式 — the container the result is wrapped in, which is what decides the
 * output file's extension. It belongs next to 输出文件名称 because together they are
 * the whole file name: the pattern produces the stem, the container the suffix.
 *
 * Unlike the rest of this section it is not inherited from the global template --
 * every template picks its own container -- so the editor keeps it editable even
 * while the section is set to follow.
 */
export function containerField(t, opts) {
  const d = t || {};
  return field('输出格式', selectHtml('container', opts.containers || [], d.container || ''),
    '留空沿用源文件的格式');
}

/**
 * 变量表 + 路径测试 — the one place the variables are documented.
 *
 * 变量**全部**列出来，按用途分两组。分组不是排版偏好：输出文件名称说*叫什么*，
 * 移动时的新名称说*搬走的那个叫什么*（它的 `{ext}` 是源扩展名，因为没有东西重新
 * 封装它），两个 token 集合不能混用，排成一长条的话用户没法知道该抄哪几个。
 * `{template}` 和 `{index}` 在两组里各出现一次是有意的 —— 每组都要能单独照着抄。
 *
 * 目录不在这张表里：它现在只有四种方式和两个填写框，没有可替换的东西。
 *
 * 每个变量都是一颗可以点的按钮：表达式是要**打进输入框**的东西，让用户照着屏幕
 * 手打一遍花括号和大小写，就是在制造拼错的机会。点一下拿到的就是原样的 token。
 */
function variablesNote() {
  const groups = [
    ['输出文件名称', [
      ['{name}', '源文件名，不含扩展名'],
      ['{template}', '模板名'],
      ['{dir}', '源文件所在目录的名字'],
      ['{index}', INDEX_DESC],
    ]],
    ['移动 / 复制源文件时的「源文件新名称」', [
      ['{ext}', '源文件的扩展名，不含点'],
    ]],
  ];
  return `
  <div class="vargrid">
    ${groups.map(([title, vars]) => `<div class="vargrid__group">${esc(title)}</div>
      ${vars.map(([token, desc]) => `<div class="vargrid__cell">
        ${varChip(token)}<span>${esc(desc)}</span></div>`).join('')}`).join('')}
  </div>
  ${pathTest()}`;
}

/**
 * 路径测试 — one input file, the rules above, the two resolved paths.
 *
 * 变量说明单独看是有歧义的：四种方式分别落在哪？两个框都留空会去哪？把
 * 一个真实路径代进去看结果，比读十行说明快。它和 runner 走同一对函数
 * （engine.OutputRoot / engine.ResolveOutput），所以这里说的就是真跑一遍会做的。
 *
 * 面板里只有**一个**输入文件，没有「添加目录」这一层，所以两个同级在这里给出同一个
 * 答案（「同级目录」只能拿文件自己的目录顶替那个锚）。这一点写在答案下面，不靠猜。
 *
 * 输入框**故意不带 `name`**：带 name 的控件在模板编辑器里就是模板字段，会写进
 * draft、会把表单标脏 —— 而这里填的只是一个用来试算的路径，不是模板的一部分。
 */
function pathTest() {
  return `
  <div class="pathtest">
    <div class="pathtest__head">${icon('route', 'sm')}<b>路径测试</b>
      <span class="hint">按上面的规则算出这个文件会写到哪；「添加目录」按它所在的目录算</span></div>
    <div class="input-group">
      <input class="input mono" data-role="path-test" placeholder="输入文件的完整路径，例如 D:/video/mmd/a.mp4">
      <button class="btn btn--outline" data-act="pick-test-file">${icon('folderOpen')}选择文件</button>
    </div>
    <div class="pathtest__out" data-role="path-test-out">
      <div class="pathtest__note">填一个输入文件，这里给出输出目录和输出文件。</div>
    </div>
  </div>`;
}

/**
 * 路径测试的结果。出错也走同一个位置 —— 两种状态长在同一个地方，用户不用去找
 * "这次为什么什么都没有"。
 */
export function pathCheckHtml(check, err) {
  if (err) return `<div class="pathtest__line pathtest__line--bad">${esc(err)}</div>`;
  const c = check || {};
  const rows = [
    ['源文件目录', c.srcDir],
    ['输出目录', c.outputDir],
    ['输出文件', c.outputPath],
  ];
  return rows.map(([k, v]) => `<div class="pathtest__line">
      <span>${k}</span><code>${esc(v || '—')}</code></div>`).join('')
    + (c.notices || []).map((n) => `<div class="pathtest__note">${esc(n)}</div>`).join('');
}

/** 一个可复制的变量。点击的处理在 templates.js 里（它是模板编辑器的表单）。 */
function varChip(token) {
  return `<button type="button" class="var-chip" data-act="copy-var" data-var="${esc(token)}"
    title="点击复制 ${esc(token)}">${esc(token)}</button>`;
}
