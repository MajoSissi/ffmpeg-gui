/**
 * Shared renderers for the inheritable sections.
 *
 * "输出与命名 / 已处理过的源文件 / 处理性能 / 筛选与转移 / 错误与警告" used to live
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
import { esc, field, selectHtml } from '../ui.js';

/** Section keys used for the "与全局不同" switch names, in display order. */
export const SECTIONS = ['perf', 'output', 'existing', 'filter', 'problems'];

/**
 * Merge a template with the global one, mirroring store.Template.Effective.
 * The output naming follows the switch first, then falls back field by field; the
 * four pointer sections fall back whole, because 0 there means "no limit" rather
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
  // A follower ignores whatever the template still carries in the output fields.
  if (!out.outputOverride) {
    for (const k of ['outMode', 'outDir', 'outSuffix', 'outPattern']) out[k] = '';
  }
  for (const k of ['outMode', 'outDir', 'outSuffix', 'outPattern']) {
    if (!String(out[k] || '').trim()) out[k] = glob[k];
  }
  for (const k of ['perf', 'existing', 'filter', 'problems']) {
    if (out[k] == null) out[k] = glob[k];
  }
  return out;
}

/**
 * What a blank dropdown value actually means, so the editor can show it.
 *
 * It is NOT the global template's value: only 输出与命名 falls back field by field
 * (Go: Template.Effective). The four pointer sections are inherited whole, so a
 * blank there stays blank -- and each blank has a literal meaning, spelled out by
 * the code that resolves it:
 *
 *   - a blank dest mode  -> store.ResolveDestDir's default branch -> the source dir
 *   - a blank action     -> Handles()/Archives() are false -> the file stays put
 *   - a blank output mode-> global.OutMode (the output section DOES fall back)
 *
 * Showing the global value instead would put the select on an option the plan
 * never reads: the draft still holds "", the user sees "sibling", and the two
 * quietly disagree.
 */
function orGlobal(value, fallback) {
  return String(value || '').trim() ? value : (fallback || '');
}

/** A blank action resolves to "leave it alone" in every section. */
function actionOr(value, blank) {
  return String(value || '').trim() ? value : blank;
}

const SECTION_META = {
  perf: { icon: 'speed', title: '处理性能' },
  output: { icon: 'folder', title: '输出与命名' },
  existing: { icon: 'history', title: '已处理过的源文件' },
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
    ${global ? '' : `<label class="inline-toggle" title="关闭时跟随「全局模板」">
      <input type="checkbox" data-sec="${key}"${active ? ' checked' : ''}>
      <span>与全局不同</span>
    </label>`}
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
    return `跟随全局：输出${outputVerb(s.outMode, s.outDir, s.outSuffix)}，命名 ${s.outPattern || '{name}'}`;
  }
  if (key === 'existing') {
    const e = s.existing || {};
    return `跟随全局：已处理过的源文件${relocatePhrase(e.action, e.dest)}`;
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
    return `跟随全局：${bits.join('、') || '没有排除条件'}；被排除的文件${relocatePhrase(f.action, f.dest)}`;
  }
  const p = s.problems || {};
  const ep = p.errorPattern ? `并改名为 ${p.errorPattern}` : '';
  const wp = p.warningPattern ? `并改名为 ${p.warningPattern}` : '';
  return `跟随全局：错误文件${relocatePhrase(p.errorAction, p.errorDest, true)}${ep}；`
    + `警告文件${relocatePhrase(p.warningAction, p.warningDest, true)}${wp}`;
}

/**
 * "移动到同级目录 + _out" in one phrase.
 *
 * The verb and the destination are glued together here rather than at each call
 * site: destLabel already starts with 到 for the modes that name a place, so a
 * caller that writes "移动到" in front of it produces "移动到到 …". `mark` is the
 * one problem-file action that is not a relocation, hence the flag.
 */
function relocatePhrase(action, dest, allowMark = false) {
  if (allowMark && action === 'mark') return '仅在记录中标记';
  const verb = action === 'copy' ? '复制' : action === 'move' ? '移动' : '';
  if (!verb) return '留在原处';
  return verb + destLabel(dest);
}

/** The main output rule in words, for the follow summary and the global template. */
function outputVerb(mode, dir, suffix) {
  switch (mode) {
    case 'same': return '到源文件所在目录';
    case 'custom': return dir ? `到 ${dir}` : '到指定目录（未填写）';
    case 'mirror': return dir ? `到 ${dir}（源目录结构）` : '到指定目录（源目录结构，未填写）';
    case 'sibling':
    default: return `到源目录的同级顶层目录 + ${suffix || '_out'}（源目录结构）`;
  }
}

function destLabel(d) {
  const dir = (d && d.dir) || '';
  switch (d && d.mode) {
    case 'same': return '在源目录';
    case 'sibling': return `到同级顶层目录 + ${(d && d.suffix) || '_out'}`;
    case 'custom': return dir ? `到 ${dir}` : '（未指定目录）';
    case 'mirror': return dir ? `到 ${dir}（源目录结构）` : '（未指定目录）';
    default: return '在原处';
  }
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
  const cores = cpus > 0 ? `本机 ${cpus} 个逻辑核心；` : '';
  const autoCores = cpus > 0 ? `（本机 ${cpus} 核）` : '';
  return `
  <div class="grid grid--3">
    ${field('同时处理任务数', `<input class="input" type="number" min="1" max="16" name="pConcurrency" value="${d.concurrency ?? 1}">`,
      `${cores}显卡编码建议 1~2`)}
    ${field('每个任务使用的 CPU 核心数', `<input class="input" type="number" min="0" max="64" name="pThreads" value="${d.threads ?? 0}">`,
      `给 ffmpeg 传 -threads；0 = 交给 ffmpeg 自己决定${autoCores}。同时跑多个任务时要往下压`)}
    ${field('日志级别', selectHtml('pLogLevel', opts.logLevels || [], d.logLevel || 'warning'), '级别越高输出越详细')}
    ${field('失败重试次数', `<input class="input" type="number" min="0" max="5" name="pRetryCount" value="${d.retryCount ?? 0}">`, '失败后自动重跑，0 表示不重试')}
  </div>
  <div style="margin-top:12px">
    ${switchRowInline('降低进程优先级', '以低优先级运行 ffmpeg，避免影响日常使用', 'pIdlePriority', !!d.idlePriority)}
    ${switchRowInline('失败时删除残缺输出', '转码中断后清理半成品文件', 'pDeleteOnFail', !!d.deleteOnFail)}
  </div>`;
}

/**
 * 匹配条件 — which files to skip, and what happens to the ones that are.
 *
 * The section header already says "匹配条件", so this body opens straight into the
 * fields. It used to repeat the title as a sub-heading, which stacked two headings
 * with the same words inside one card.
 *
 * The six bounds read as two rows of one scale rather than three pairs: 最小/最短/下限
 * on top, 最大/最长/上限 below, each column a range. The old order interleaved them
 * (最小体积, 最大体积, 最短时长, ...) so scanning a row gave you two halves of three
 * unrelated scales.
 */
export function filterBody(f, opts) {
  const d = f || {};
  return `
  <div class="grid grid--3">
    ${field('最小体积 (MB)', `<input class="input" type="number" min="0" step="1" name="fMinSizeMB" value="${d.minSizeMB ?? 0}">`, '留 0 表示不限制。小于该体积的文件不处理')}
    ${field('最短时长 (秒)', `<input class="input" type="number" min="0" name="fMinDuration" value="${d.minDuration ?? 0}">`, '留 0 表示不限制。短于该时长的文件不处理')}
    ${field('长边下限 (px)', `<input class="input" type="number" min="0" name="fMinLongEdge" value="${d.minLongEdge ?? 0}">`, '留 0 表示不限制。横竖屏都取较长的一边')}
  </div>
  <div class="grid grid--3" style="margin-top:10px">
    ${field('最大体积 (MB)', `<input class="input" type="number" min="0" step="1" name="fMaxSizeMB" value="${d.maxSizeMB ?? 0}">`, '留 0 表示不限制。超过该体积的文件不处理')}
    ${field('最长时长 (秒)', `<input class="input" type="number" min="0" name="fMaxDuration" value="${d.maxDuration ?? 0}">`, '留 0 表示不限制。超过该时长的文件不处理')}
    ${field('长边上限 (px)', `<input class="input" type="number" min="0" name="fMaxLongEdge" value="${d.maxLongEdge ?? 0}">`, '留 0 表示不限制。超过该长边的文件不处理')}
  </div>
  <div class="grid grid--2" style="margin-top:10px">
    ${field('仅处理这些扩展名', `<input class="input mono" name="fIncludeExts" value="${esc((d.includeExts || []).join(','))}" placeholder="mp4,mkv,mov">`, '逗号分隔，留空表示全部')}
    ${field('排除这些扩展名', `<input class="input mono" name="fExcludeExts" value="${esc((d.excludeExts || []).join(','))}" placeholder="webm,gif">`)}
  </div>
  <div class="section__head" style="border:0;padding:0;margin:18px 0 10px">
    ${icon('route', 'sm')}<h3 style="font-size:13px">被排除文件的处理</h3>
  </div>
  <div class="grid grid--2">
    ${field('处理方式', selectHtml('fAction', opts.filterActions || [], actionOr(d.action, 'keep')))}
    ${field('输出方式', selectHtml('fDestMode', opts.destModes || [], orGlobal((d.dest || {}).mode, 'same')), '与「输出与命名」相同的四种方式')}
    ${field('目录后缀', `<input class="input mono" name="fDestSuffix" value="${esc((d.dest || {}).suffix || '')}" placeholder="留空用 _out">`, '「同级顶层目录 + 后缀」模式下使用')}
    ${field('指定目录', `<div class="input-group">
      <input class="input mono" name="fDestDir" value="${esc((d.dest || {}).dir || '')}" placeholder="例如 D:\\Media\\small">
      <button class="btn btn--outline btn--icon" data-act="pick-dir" data-target="fDestDir">${icon('folderOpen')}</button>
    </div>`, '「指定目录」与「指定目录（源目录结构）」使用')}
    ${field('重命名模板', `<input class="input mono" name="fRenamePattern" value="${esc(d.renamePattern || '')}" placeholder="{name}">`, '变量与「输出与命名」的命名模板相同，说明见该节', 'span-2')}
  </div>
  <div style="margin-top:12px">
    ${switchRowInline('覆盖同名文件', '关闭时自动追加 _1、_2 避免覆盖', 'fOverwrite', !!d.overwrite)}
  </div>`;
}

/**
 * 已处理过的源文件 — what to do with a source this template has already produced
 * output for. Running the same template over the same folder twice is the case it
 * exists for, and re-encoding finished files is the thing worth avoiding.
 *
 * Deliberately the same shape as 「被排除文件的处理」, down to the wording:
 * 处理方式 + 四种输出方式 + 目录后缀 / 指定目录 + 命名模板 + 覆盖开关. The two
 * differ only in which file gets moved, and a rule that reads identically in both
 * places is one less thing to misremember.
 */
export function existingBody(e, opts) {
  const d = e || {};
  // A blank action is the plain "leave it where it is" case, so that -- not some
  // archive verb -- is what the select has to show.
  const action = actionOr(d.action, 'keep');
  return `
  <div class="grid grid--2">
    ${field('处理方式', selectHtml('exAction', opts.existingActions || [], action),
      '只对已经处理过的文件生效：本次不再编码它，按上面的方式安置源文件')}
    ${field('输出方式', selectHtml('exDestMode', opts.destModes || [], orGlobal((d.dest || {}).mode, 'same')),
      '仅「移动 / 复制」时使用，四种方式与「输出与命名」相同')}
    ${field('目录后缀', `<input class="input mono" name="exDestSuffix" value="${esc((d.dest || {}).suffix || '')}" placeholder="留空用 _out">`)}
    ${field('指定目录', `<div class="input-group">
      <input class="input mono" name="exDestDir" value="${esc((d.dest || {}).dir || '')}" placeholder="例如 D:\\Media\\done">
      <button class="btn btn--outline btn--icon" data-act="pick-dir" data-target="exDestDir">${icon('folderOpen')}</button>
    </div>`, '源文件被移动后，队列中的路径不会改变；如需重新处理请重新添加')}
    ${field('命名模板', `<input class="input mono" name="exPattern" value="${esc(d.pattern || '')}" placeholder="{name}">`,
      '留空保持原文件名', 'span-2')}
  </div>
  <div style="margin-top:12px">
    ${switchRowInline('覆盖目标目录里的同名文件', '关闭时自动追加 _1、_2，避免上一次移走的文件被这一次顶掉', 'exOverwrite', !!d.overwrite)}
  </div>`;
}

/** 错误与警告 — the policy for files that failed or finished with complaints. */
export function problemsBody(p, opts) {
  const d = p || {};
  return `
  <div class="section__head" style="border:0;padding:0;margin:0 0 10px">
    ${icon('error', 'sm')}<h3 style="font-size:13px">错误文件</h3>
  </div>
  <div class="grid grid--2">
    ${field('处理方式', selectHtml('prErrorAction', opts.problemActions || [], actionOr(d.errorAction, 'keep')), '便于事后统一排查')}
    ${field('输出方式', selectHtml('prErrorDestMode', opts.destModes || [], orGlobal((d.errorDest || {}).mode, 'same')))}
    ${field('目录后缀', `<input class="input mono" name="prErrorDestSuffix" value="${esc((d.errorDest || {}).suffix || '')}" placeholder="留空用 _out">`)}
    ${field('指定目录', `<div class="input-group">
      <input class="input mono" name="prErrorDestDir" value="${esc((d.errorDest || {}).dir || '')}" placeholder="例如 D:\\Media\\failed">
      <button class="btn btn--outline btn--icon" data-act="pick-dir" data-target="prErrorDestDir">${icon('folderOpen')}</button>
    </div>`)}
    ${field('命名模板', `<input class="input mono" name="prErrorPattern" value="${esc(d.errorPattern || '')}" placeholder="{name}">`,
      '留空保持原文件名', 'span-2')}
  </div>
  <div class="section__head" style="border:0;padding:0;margin:18px 0 10px">
    ${icon('warning', 'sm')}<h3 style="font-size:13px">警告文件</h3>
  </div>
  <div class="grid grid--2">
    ${field('处理方式', selectHtml('prWarningAction', opts.problemActions || [], actionOr(d.warningAction, 'keep')), '便于事后统一排查')}
    ${field('输出方式', selectHtml('prWarningDestMode', opts.destModes || [], orGlobal((d.warningDest || {}).mode, 'same')))}
    ${field('目录后缀', `<input class="input mono" name="prWarningDestSuffix" value="${esc((d.warningDest || {}).suffix || '')}" placeholder="留空用 _out">`)}
    ${field('指定目录', `<div class="input-group">
      <input class="input mono" name="prWarningDestDir" value="${esc((d.warningDest || {}).dir || '')}" placeholder="例如 D:\\Media\\warnings">
      <button class="btn btn--outline btn--icon" data-act="pick-dir" data-target="prWarningDestDir">${icon('folderOpen')}</button>
    </div>`, '源文件被移动后，队列中的路径不会改变；如需重新处理请重新添加')}
    ${field('命名模板', `<input class="input mono" name="prWarningPattern" value="${esc(d.warningPattern || '')}" placeholder="{name}">`,
      '留空保持原文件名', 'span-2')}
  </div>`;
}

/** 输出与命名 — the main output rule. */
export function outputBody(t, opts, g) {
  const d = t || {};
  const gd = g || {};
  return `
  <div class="grid grid--2">
    ${field('输出方式', selectHtml('outMode', opts.destModes || opts.outputModes || [], orGlobal(d.outMode, gd.outMode)))}
    ${field('目录后缀', `<input class="input mono" name="outSuffix" value="${esc(d.outSuffix || '')}" placeholder="_out">`,
      '「同级顶层目录 + 后缀」模式下，追加到最上层那个目录名之后')}
    ${field('指定目录', `<div class="input-group">
      <input class="input mono" name="outDir" value="${esc(d.outDir || '')}" placeholder="例如 D:\\Media\\out">
      <button class="btn btn--outline btn--icon" data-act="pick-dir" data-target="outDir">${icon('folderOpen')}</button>
    </div>`, '「指定目录」与「指定目录（源目录结构）」使用', 'span-2')}
    ${field('命名模板', `<input class="input mono" name="outPattern" value="${esc(d.outPattern || '')}" placeholder="{name}">`,
      '只写文件名，不含扩展名；留空使用 {name}')}
    ${containerField(d, opts)}
  </div>
  ${variablesNote()}`;
}

/**
 * 输出格式 — the container the result is wrapped in, which is what decides the
 * output file's extension. It belongs next to 命名模板 because together they are
 * the whole file name: the pattern produces the stem, the container the suffix.
 *
 * Unlike the rest of this section it is not inherited from the global template --
 * every template picks its own container -- so the editor keeps it editable even
 * while the section is set to follow.
 */
export function containerField(t, opts) {
  const d = t || {};
  return field('输出格式', selectHtml('container', opts.containers || [], d.container || ''),
    '决定输出文件的扩展名；留空沿用源文件的格式');
}

/**
 * 可用变量 — the one place the naming variables are documented, next to the field
 * they describe instead of repeated under all four that take one. Values match
 * engine.ExpandPattern exactly.
 *
 * Wording rule learned the hard way: one line per variable, saying what it
 * expands to and nothing else. The earlier version explained {ext}'s two
 * meanings in a footnote nobody read twice, and the exception turned out to be
 * the part that mattered.
 */
export function variablesNote() {
  return `
  <div class="varlist">
    <span class="mono">{name}</span><span>源文件名，不含扩展名</span>
    <span class="mono">{template}</span><span>模板名</span>
    <span class="mono">{dir}</span><span>源文件所在的目录名</span>
    <span class="mono">{index}</span><span>批次内序号，补零三位（007）；没有序号时留空</span>
  </div>
  <div class="varlist__foot">「命名模板」只写<strong>文件名</strong>，扩展名由「输出格式」决定并自动补上，
    所以没有 <span class="mono">{ext}</span>。<strong>被移走的文件</strong>（匹配条件排除的、已处理过的、
    出错 / 出警告的）不重新编码，保持原扩展名，那里额外可以用
    <span class="mono">{ext}</span> 拿到源扩展名。</div>`;
}

function switchRowInline(title, hint, name, checked) {
  return `<div class="switch-row">
    <div class="switch-row__text"><b>${esc(title)}</b><span>${esc(hint)}</span></div>
    <label class="switch"><input type="checkbox" name="${esc(name)}"${checked ? ' checked' : ''}></label>
  </div>`;
}
