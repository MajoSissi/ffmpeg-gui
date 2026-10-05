/**
 * Shared renderers for the four inheritable sections.
 *
 * "输出与命名 / 处理性能 / 筛选与转移 / 错误与警告" used to live on the settings
 * page. They now belong to templates, with the global template as the default
 * every other template inherits from. A regular template shows each section
 * behind a "与全局不同" switch: off means "follow", which on the wire is a nil
 * section (or, for the output naming, `outputOverride: false`).
 *
 * All four are ordered by how often they are touched, so the rarely-needed policy
 * sections sit at the bottom. Both editors go through these functions so a rule can
 * never be described one way on the global template and another way on a regular one.
 */
import { icon } from '../icons.js';
import { esc, field, selectHtml } from '../ui.js';

/** Section keys used for the "与全局不同" switch names, in display order. */
export const SECTIONS = ['perf', 'output', 'filter', 'problems'];

/**
 * Merge a template with the global one, mirroring store.Template.Effective.
 * The output naming follows the switch first, then falls back field by field; the
 * three pointer sections fall back whole, because 0 there means "no limit" rather
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
    for (const k of ['outMode', 'outDir', 'outSuffix', 'outPattern', 'outConflict']) out[k] = '';
  }
  for (const k of ['outMode', 'outDir', 'outSuffix', 'outPattern', 'outConflict']) {
    if (!String(out[k] || '').trim()) out[k] = glob[k];
  }
  for (const k of ['perf', 'filter', 'problems']) {
    if (out[k] == null) out[k] = glob[k];
  }
  return out;
}

const SECTION_META = {
  perf: { icon: 'speed', title: '处理性能' },
  output: { icon: 'folder', title: '输出与命名' },
  filter: { icon: 'filter', title: '筛选条件' },
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
    return `跟随全局：输出${outputVerb(s.outMode, s.outDir, s.outSuffix)}，命名 ${s.outPattern || '{name}.{ext}'}`;
  }
  if (key === 'filter') {
    const f = s.filter || {};
    if (!filterActive(f)) return '跟随全局：不筛选，所有文件都会处理，被排除的文件留在原处';
    const bits = [];
    if (f.minSizeMB > 0) bits.push(`小于 ${f.minSizeMB} MB`);
    if (f.maxSizeMB > 0) bits.push(`大于 ${f.maxSizeMB} MB`);
    if (f.minLongEdge > 0) bits.push(`长边 < ${f.minLongEdge}`);
    if (f.maxLongEdge > 0) bits.push(`长边 > ${f.maxLongEdge}`);
    if (f.minDuration > 0) bits.push(`短于 ${f.minDuration}s`);
    if (f.maxDuration > 0) bits.push(`长于 ${f.maxDuration}s`);
    if ((f.includeExts || []).length) bits.push(`仅 ${f.includeExts.join('/')}`);
    if ((f.excludeExts || []).length) bits.push(`排除 ${f.excludeExts.join('/')}`);
    return `跟随全局：${bits.join('、') || '不筛选'}；被排除的文件${
      f.action === 'copy' ? '复制到' : f.action === 'move' ? '移动到' : '留在原处'}${
      destLabel(f.dest)}`;
  }
  const p = s.problems || {};
  return `跟随全局：失败文件${problemVerb(p.errorAction)}${destLabel(p.errorDest)}；`
    + `警告文件${problemVerb(p.warningAction)}${destLabel(p.warningDest)}`;
}

function problemVerb(action) {
  if (action === 'move') return '移动';
  if (action === 'copy') return '复制';
  if (action === 'mark') return '仅标记';
  return '保留原处';
}

/** The main output rule in words, for the follow summary and the global template. */
function outputVerb(mode, dir, suffix) {
  switch (mode) {
    case 'same': return '到源文件所在目录';
    case 'custom': return dir ? `到 ${dir}` : '到指定目录（未填写）';
    case 'mirror': return dir ? `到 ${dir} 并保持子目录` : '到指定目录并保持子目录（未填写）';
    case 'sibling':
    default: return `到源目录的同级 + ${suffix || '_out'}，子目录结构不变`;
  }
}

function destLabel(d) {
  const dir = (d && d.dir) || '';
  switch (d && d.mode) {
    case 'same': return '在源目录';
    case 'sibling': return `到同级目录 + ${(d && d.suffix) || '_out'}`;
    case 'custom': return dir ? `到 ${dir}` : '（未指定目录）';
    case 'mirror': return dir ? `到 ${dir} 并保持子目录` : '（未指定目录）';
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
export function perfBody(p, opts, cpus) {
  const d = p || {};
  return `
  <div class="grid grid--3">
    ${field('同时处理任务数', `<input class="input" type="number" min="1" max="16" name="pConcurrency" value="${d.concurrency ?? 1}">`,
      `本机 ${cpus} 个逻辑核心；显卡编码建议 1~2`)}
    ${field('每个任务使用的 CPU 核心数', `<input class="input" type="number" min="0" max="64" name="pThreads" value="${d.threads ?? 0}">`,
      `给 ffmpeg 传 -threads；0 = 交给 ffmpeg 自己决定（本机 ${cpus} 核）。同时跑多个任务时要往下压`)}
    ${field('日志级别', selectHtml('pLogLevel', opts.logLevels || [], d.logLevel || 'warning'), '级别越高输出越详细')}
    ${field('失败重试次数', `<input class="input" type="number" min="0" max="5" name="pRetryCount" value="${d.retryCount ?? 0}">`, '失败后自动重跑，0 表示不重试')}
  </div>
  <div style="margin-top:12px">
    ${switchRowInline('降低进程优先级', '以低优先级运行 ffmpeg，避免影响日常使用', 'pIdlePriority', !!d.idlePriority)}
    ${switchRowInline('失败时删除残缺输出', '转码中断后清理半成品文件', 'pDeleteOnFail', !!d.deleteOnFail)}
  </div>`;
}

/**
 * 筛选条件 — which files to skip, and what happens to the ones that are.
 *
 * The section header already says "筛选条件", so this body opens straight into the
 * fields. It used to repeat the title as a sub-heading, which stacked two headings
 * with the same words inside one card.
 */
export function filterBody(f, opts) {
  const d = f || {};
  return `
  <div class="grid grid--3">
    ${field('最小体积 (MB)', `<input class="input" type="number" min="0" step="1" name="fMinSizeMB" value="${d.minSizeMB ?? 0}">`, '留 0 表示不限制。小于该体积的文件不处理')}
    ${field('最大体积 (MB)', `<input class="input" type="number" min="0" step="1" name="fMaxSizeMB" value="${d.maxSizeMB ?? 0}">`)}
    ${field('最短时长 (秒)', `<input class="input" type="number" min="0" name="fMinDuration" value="${d.minDuration ?? 0}">`)}
    ${field('最长时长 (秒)', `<input class="input" type="number" min="0" name="fMaxDuration" value="${d.maxDuration ?? 0}">`)}
    ${field('长边下限 (px)', `<input class="input" type="number" min="0" name="fMinLongEdge" value="${d.minLongEdge ?? 0}">`, '横竖屏都会取较长的一边')}
    ${field('长边上限 (px)', `<input class="input" type="number" min="0" name="fMaxLongEdge" value="${d.maxLongEdge ?? 0}">`)}
  </div>
  <div class="grid grid--2" style="margin-top:10px">
    ${field('仅处理这些扩展名', `<input class="input mono" name="fIncludeExts" value="${esc((d.includeExts || []).join(','))}" placeholder="mp4,mkv,mov">`, '逗号分隔，留空表示全部')}
    ${field('排除这些扩展名', `<input class="input mono" name="fExcludeExts" value="${esc((d.excludeExts || []).join(','))}" placeholder="webm,gif">`)}
  </div>
  <div class="section__head" style="border:0;padding:0;margin:18px 0 10px">
    ${icon('route', 'sm')}<h3 style="font-size:13px">被排除文件的处理</h3>
  </div>
  <div class="grid grid--2">
    ${field('处理方式', selectHtml('fAction', opts.filterActions || [], d.action || 'keep'))}
    ${field('输出方式', selectHtml('fDestMode', opts.destModes || [], (d.dest || {}).mode || ''), '与「输出与命名」相同的四种方式')}
    ${field('目录后缀', `<input class="input mono" name="fDestSuffix" value="${esc((d.dest || {}).suffix || '')}" placeholder="留空用 _out">`, '「同级目录 + 后缀」模式下使用')}
    ${field('指定目录', `<div class="input-group">
      <input class="input mono" name="fDestDir" value="${esc((d.dest || {}).dir || '')}" placeholder="例如 D:\\Media\\small">
      <button class="btn btn--outline btn--icon" data-act="pick-dir" data-target="fDestDir">${icon('folderOpen')}</button>
    </div>`, '「全部输出到指定目录」与「指定目录 + 保持子目录结构」使用')}
    ${field('重命名模板', `<input class="input mono" name="fRenamePattern" value="${esc(d.renamePattern || '')}" placeholder="{name}.{ext}">`, '变量与「输出与命名」的命名模板相同，说明见该节', 'span-2')}
  </div>
  <div style="margin-top:12px">
    ${switchRowInline('覆盖同名文件', '关闭时自动追加 _1、_2 避免覆盖', 'fOverwrite', !!d.overwrite)}
  </div>`;
}

/** 错误与警告 — the policy for files that failed or finished with complaints. */
export function problemsBody(p, opts) {
  const d = p || {};
  return `
  <div class="grid grid--2">
    ${field('失败文件 · 处理方式', selectHtml('prErrorAction', opts.problemActions || [], d.errorAction || 'keep'), '便于事后统一排查')}
    ${field('失败文件 · 输出方式', selectHtml('prErrorDestMode', opts.destModes || [], (d.errorDest || {}).mode || ''))}
    ${field('失败文件 · 目录后缀', `<input class="input mono" name="prErrorDestSuffix" value="${esc((d.errorDest || {}).suffix || '')}" placeholder="留空用 _out">`)}
    ${field('失败文件 · 指定目录', `<div class="input-group">
      <input class="input mono" name="prErrorDestDir" value="${esc((d.errorDest || {}).dir || '')}" placeholder="例如 D:\\Media\\failed">
      <button class="btn btn--outline btn--icon" data-act="pick-dir" data-target="prErrorDestDir">${icon('folderOpen')}</button>
    </div>`)}
  </div>
  <div class="section__head" style="border:0;padding:0;margin:18px 0 10px">
    ${icon('warning', 'sm')}<h3 style="font-size:13px">带警告完成的文件</h3>
  </div>
  <div class="grid grid--2">
    ${field('警告文件 · 处理方式', selectHtml('prWarningAction', opts.problemActions || [], d.warningAction || 'mark'))}
    ${field('警告文件 · 输出方式', selectHtml('prWarningDestMode', opts.destModes || [], (d.warningDest || {}).mode || ''))}
    ${field('警告文件 · 目录后缀', `<input class="input mono" name="prWarningDestSuffix" value="${esc((d.warningDest || {}).suffix || '')}" placeholder="留空用 _out">`)}
    ${field('警告文件 · 指定目录', `<div class="input-group">
      <input class="input mono" name="prWarningDestDir" value="${esc((d.warningDest || {}).dir || '')}" placeholder="例如 D:\\Media\\warnings">
      <button class="btn btn--outline btn--icon" data-act="pick-dir" data-target="prWarningDestDir">${icon('folderOpen')}</button>
    </div>`, '源文件被移动后，队列中的路径不会改变；如需重新处理请重新添加')}
  </div>
  <div class="hint" style="margin-top:12px">${icon('info', 'sm')} 这里的移动 / 复制<strong>保持原文件名</strong>，不套用「输出与命名」的命名模板；
    只决定文件<strong>去哪个目录</strong>。要达到改名效果请用「筛选条件」里的重命名模板。</div>`;
}

/** 输出与命名 — the main output rule. */
export function outputBody(t, opts) {
  const d = t || {};
  return `
  <div class="grid grid--2">
    ${field('输出方式', selectHtml('outMode', opts.destModes || opts.outputModes || [], d.outMode || ''))}
    ${field('目录后缀', `<input class="input mono" name="outSuffix" value="${esc(d.outSuffix || '')}" placeholder="_out">`,
      '「同级目录 + 后缀」模式下，追加到最上层那个目录名之后')}
    ${field('指定目录', `<div class="input-group">
      <input class="input mono" name="outDir" value="${esc(d.outDir || '')}" placeholder="例如 D:\\Media\\out">
      <button class="btn btn--outline btn--icon" data-act="pick-dir" data-target="outDir">${icon('folderOpen')}</button>
    </div>`, '「全部输出到指定目录」与「指定目录 + 保持子目录结构」使用', 'span-2')}
    ${field('命名模板', `<input class="input mono" name="outPattern" value="${esc(d.outPattern || '')}" placeholder="{name}.{ext}">`,
      '留空使用 {name}.{ext}', 'span-2')}
    ${field('重名处理', selectHtml('outConflict', opts.conflictModes || [], d.outConflict || ''), '输出文件已存在时怎么办', 'span-2')}
  </div>
  ${variablesNote()}`;
}

/**
 * The one place the naming variables are documented. The pattern fields used to carry
 * nothing but a flat "可用变量：{name} {ext} …" hint, which names the tokens without
 * saying what any of them expand to. Values match engine.ExpandPattern exactly.
 *
 * It lives here, next to the pattern it describes, rather than repeated under every
 * field that takes one -- the same set drives the filter's rename template, and a
 * footnote says so instead of duplicating the table.
 */
export function variablesNote() {
  return `
  <div class="varlist">
    <span class="mono">{name}</span><span>源文件名，不含扩展名</span>
    <span class="mono">{ext}</span><span>输出文件的扩展名——换了容器就跟着变，不会保留 mkv 这种源扩展名</span>
    <span class="mono">{template}</span><span>模板名</span>
    <span class="mono">{dir}</span><span>源文件所在的目录名</span>
    <span class="mono">{index}</span><span>批次内序号，不足三位补零（第 7 个是 007；序号为 0 时空白）</span>
  </div>
  <div class="varlist__foot">「筛选条件」里被排除文件的重命名模板用的是同一套变量。
    「错误与警告」的文件移动 / 复制不套用命名模板，保持原文件名。</div>`;
}

function switchRowInline(title, hint, name, checked) {
  return `<div class="switch-row">
    <div class="switch-row__text"><b>${esc(title)}</b><span>${esc(hint)}</span></div>
    <label class="switch"><input type="checkbox" name="${esc(name)}"${checked ? ' checked' : ''}></label>
  </div>`;
}
