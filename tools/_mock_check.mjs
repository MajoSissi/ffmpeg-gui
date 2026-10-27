const { api, isMock } = await import('file:///D:/Code-Project/ffmpeg-gui/frontend/dist/api.js');
const log = (...a) => process.stderr.write(a.join(' ') + '\n');
log('isMock:', isMock);

let bad = 0;
const eq = (name, got, want) => {
  const a = JSON.stringify(got), b = JSON.stringify(want);
  if (a === b) { log(`ok   ${name}`); return; }
  bad++;
  log(`FAIL ${name}\n  got  ${a}\n  want ${b}`);
};

/**
 * 断言一个纯函数的返回值，但**不让它把整个脚本带走**。
 *
 * 直接写 `eq(name, profileSummary(x), '…')` 时，函数自己抛了（`ReferenceError` 之类）
 * 就会掀掉这个模块，后面几十条断言一条都不跑，只留一段 Node 的堆栈。退出码是对的
 * （1 会拦住出包），可报告里看不到"哪一条坏了"，也看不到后面的断言结果 —— 而一个纯函数
 * 抛异常，恰恰说明它该被断言的那个形状它处理不了，后面几条多半也跟着不对。
 *
 * 所以这里接住，记成一条 FAIL，然后继续往下走。
 */
const eqCall = (name, fn, want) => {
  let got;
  try {
    got = fn();
  } catch (e) {
    bad++;
    log(`FAIL ${name}\n  threw ${(e && (e.name + ': ' + e.message)) || e}`);
    return;
  }
  eq(name, got, want);
};

/* ------------------------------------------------------------ 夹具与小工具 */

/**
 * `profileSummary` 直接断言，不开浏览器。
 *
 * 它是纯函数（`views/filters.js` 的导出），而**这里必须直接调它**而不是靠界面报不报错：
 * 任务页每次 `syncProfileOptions` 都走它，可 mock 里生效的那套（`默认`）两组条件都是空的
 * ——`groupText` 里的代码一行都不执行。2026-10-10 出过一次 `MODE_LABEL is not defined`：
 * 那个常量在 `.map` 回调里被引用，空数组压根不调用回调，于是 `node --check`、
 * `check_frontend.py`、首屏探针、整套 mock 检查全绿，只有真实用户的设置里有条件才炸。
 *
 * 所以这里喂**有条件、没说明**的方案：没有说明才会落到念条件那条路上（`description`
 * 优先返回），而"条件是空的"和"摘要是函数写错了"在界面上长得一模一样。
 */
const { profileSummary, rulesActive, draftKey } = await import(
  'file:///D:/Code-Project/ffmpeg-gui/frontend/dist/views/filters.js'
);
eqCall('摘要：没说明时念条件（排除方向 + 只收一层 + 两种匹配方式）', () => profileSummary({
  name: '排掉中间目录',
  dirs: { enabled: true, matchAll: false, exclude: true, topOnly: true,
    filters: [{ mode: 'prefix', value: 'ffmpeg__' }, { mode: 'glob', value: '*-tmp' }] },
  files: { enabled: false, matchAll: false, exclude: false, filters: [] },
}), '目录：排除 · 只收这一层 · 前缀 ffmpeg__ · 通配符 *-tmp');
eqCall('摘要：有说明时用说明，条件不念', () => profileSummary({
  name: '只收 h265', description: '转码中间产物，留着就行',
  dirs: { enabled: true, matchAll: false, exclude: false, topOnly: false,
    filters: [{ mode: 'contains', value: 'h265' }] },
  files: { enabled: true, matchAll: false, exclude: true, filters: [{ mode: 'suffix', value: '.tmp' }] },
}), '转码中间产物，留着就行');
eqCall('摘要：两组都空时说「未设置」', () => profileSummary({ name: '默认', dirs: {}, files: {} }), '未设置');
// 不认识的 mode：老设置文件里可能有现在删掉过的匹配方式，摘要要还能念出来，不能是空串。
eqCall('摘要：不认识的 mode 也念得出来', () => profileSummary({
  name: '老方案',
  dirs: { enabled: true, matchAll: false, exclude: false, topOnly: false,
    filters: [{ mode: 'regex', value: 'foo' }] },
  files: { enabled: false, matchAll: false, exclude: false, filters: [] },
}), '目录：条件 foo');
// 关掉的那组也念条件：只写"未启用"的话，用户记不住自己设过什么。
// 那一档的字是**开关自己的反面**（「启用」→「未启用」），不是「已暂停」：后者这一批刚
// 成为队列状态的说法，而这只 chip 就印在任务页工具栏上。
eqCall('摘要：关掉的那组也念', () => profileSummary({
  name: '关着的那套',
  dirs: { enabled: false, matchAll: false, exclude: false, topOnly: false,
    filters: [{ mode: 'contains', value: 'cache' }] },
  files: { enabled: false, matchAll: false, exclude: false, filters: [] },
}), '目录：未启用 · 包含 cache');
eqCall('两组空数组不算启用', () => rulesActive({ enabled: true, filters: [] }), false);
eqCall('有条件且开着才算启用', () => rulesActive({ enabled: true, filters: [{ mode: 'contains', value: 'a' }] }), true);

/* ------------------------------------------- 草稿快照：什么才算"有未保存的改动" */
/*
 * 用户报的：「我修改后点击保存了，点其他页面依旧弹出"放弃未保存的修改"」。
 *
 * 根因是**空条件行**。`paintGroup` 在一组一条条件都没有时补一行空的（那是"可以打字的
 * 地方"，后端 `Normalize` 读进去也会丢掉），而预览那条路（`scanOf()` → `readForm()`）
 * 会把页面读回草稿 —— 那个空行于是被当成了一条真条件，草稿凭空比后端那一份多出一行，
 * `dirty()` 就永远是脏的，而且脏是保存**之后**那一次预览读回来的，所以"刚存过"也一样。
 *
 * 这里断言的是那个判据本身：空行不算改动，而有值的行被增删、名称被改都还是要算。
 */
const snapshotBase = {
  name: '默认', description: '',
  dirs: { enabled: true, filters: [{ mode: 'contains', value: 'FFMPEG' }], matchAll: false, exclude: false, topOnly: false },
  files: { enabled: false, filters: [], matchAll: false, exclude: false },
};
const withRows = (group, rows) => ({ ...snapshotBase, [group]: { ...snapshotBase[group], filters: rows } });
eqCall('快照：补出来的那一行空条件不算改动',
  () => draftKey(withRows('files', [{ mode: 'contains', value: '' }])), draftKey(snapshotBase));
eqCall('快照：只有空白的行也不算',
  () => draftKey(withRows('dirs', [...snapshotBase.dirs.filters, { mode: 'glob', value: '   ' }])), draftKey(snapshotBase));
eqCall('快照：清空一条有值的条件算改动',
  () => draftKey(withRows('dirs', [])) !== draftKey(snapshotBase), true);
eqCall('快照：多加一条有值的条件算改动',
  () => draftKey(withRows('dirs', [...snapshotBase.dirs.filters, { mode: 'suffix', value: '__1' }])) !== draftKey(snapshotBase), true);
eqCall('快照：名称多打一个空格算改动（不 trim）',
  () => draftKey({ ...snapshotBase, name: '默认 ' }) !== draftKey(snapshotBase), true);
eqCall('快照：字段顺序不同不算改动（后端结构体的顺序和页面读回来的不一定一样）',
  () => draftKey({ files: snapshotBase.files, dirs: snapshotBase.dirs, description: '', name: '默认' }), draftKey(snapshotBase));

/**
 * 「匹配条件」的动词只能是「跳过」，而且只有这一个出处。
 *
 * 同一句话有两个渲染点：任务页工具栏那颗 chip（`tasks.js`）和模板页「跟随全局」时那行
 * 说明（这里）。两处对不上（一处"跳过 <300MB"一处"排除 <300MB"），用户会以为是两个设置。
 * 「排除」还是过滤页那个双向开关的一侧（收 ↔ 排除目录名）—— 同一个词指两件事，
 * 用户得先猜这里说的是哪一个。
 *
 * 模板页那条只能这样验：`followHint` 是纯函数，可「跟随全局」的分支只在**非全局模板的
 * 某一段跟着全局**时才上屏，而 mock 里那个模板的那一段有自己的值 —— 界面探针走不到这里。
 */
const { followHint, outputBody, filterBody, existingBody, problemsBody } = await import(
  'file:///D:/Code-Project/ffmpeg-gui/frontend/dist/views/sections.js'
);
eqCall('模板页匹配条件：念的是「跳过」的边界', () => followHint('filter', {
  filter: { minSizeMB: 300, maxLongEdge: 1280, excludeExts: ['webm', 'gif'], action: 'move', dir: 'D:/Media/small' },
}), '跟随全局：跳过 <300 MB、跳过长边 >1280、跳过扩展名 webm/gif；被跳过的文件移动到源文件所在目录');
eqCall('模板页匹配条件：没有条件时也不说「排除」', () => followHint('filter', { filter: {} }),
  '跟随全局：全部文件都处理，没有跳过条件');

/**
 * 参数标题旁的说明图标：五段「输出方式」+「输出文件名称」+ 四段「源文件新名称」。
 *
 * 为什么钉在纯函数这一层：说明内容**常驻 DOM**（只是不显示），所以界面探针数得到条数
 * （`_drive.html` 的 `.infotip` / `.tip__row`）；但"某一段的说明内容本来就是另一段的"
 * 在界面上长得一模一样 —— 比如把 `{ext}` 也写进「输出文件名称」，而那里
 * `engine.ExpandOutputPattern` 会**删掉** `{ext}`，列出来就是把用户引到一个写了不生效的框。
 */
const SOPTS = {};
const hits = (s, needle) => s.split(needle).length - 1;
eqCall('说明图标：输出与命名两颗', () => hits(outputBody({}, SOPTS), 'class="infotip"'), 2);
eqCall('说明图标：被跳过那段两颗', () => hits(filterBody({}, SOPTS), 'class="infotip"'), 2);
eqCall('说明图标：已处理过的那段两颗', () => hits(existingBody({}, SOPTS), 'class="infotip"'), 2);
eqCall('说明图标：错误 / 警告各两颗', () => hits(problemsBody({}, SOPTS), 'class="infotip"'), 4);
// 五段共用同一句三档说明：同一个下拉在两处被解释成两个意思，比不解释更糟。
eqCall('说明图标：五段的三档说明是同一句',
  () => [outputBody, filterBody, existingBody, problemsBody]
    .reduce((n, f) => n + hits(f({}, SOPTS), '在你添加的那个目录旁边新建一个'), 0), 5);
// 顺序也是用户看到的东西：说明卡里那几行的次序就是下拉里那几项的次序，所以这里量的是
// **有序列出**，不是"名字都出现过"。行数也一起钉住 —— 删掉一档却没有同步说明，剩下的
// 三行看着照样对。
eqCall('说明图标：三档的名字按下拉的顺序列（就三行）',
  () => [...outputBody({}, SOPTS).matchAll(/class="tip__term">([^<]+)</g)].map((m) => m[1]),
  ['原目录', '自定义目录', '同级目录']);
// 第 55 批删掉的那一档不能在说明里留下名字：下拉里没有了它，说明里还列着就等于指给
// 用户一个不存在的东西。说明内容**常驻 DOM**（只是不显示），所以这一条量得到。
eqCall('说明图标：不列「同级汇总 / 同级逐层」那两个旧名',
  () => /同级汇总|同级逐层/.test(outputBody({}, SOPTS)), false);
// `{ext}` 只在搬迁侧（`ExpandPattern` 有它、`ExpandOutputPattern` 删它）。
eqCall('说明图标：输出名称那颗不列 {ext}', () => hits(outputBody({}, SOPTS), '{ext}'), 0);
eqCall('说明图标：四段搬迁名称都列了 {ext}',
  () => [filterBody, existingBody, problemsBody]
    .reduce((n, f) => n + hits(f({}, SOPTS), 'data-var="{ext}"'), 0), 4);
eqCall('说明图标：每颗变量都带 data-var（点了能复制）',
  () => hits(existingBody({}, SOPTS), 'data-var='), 5);

/**
 * 「输出方式」三档：`api.js` 的 `destModes` 是 Go 侧 `buildOptions()` 的**镜像**
 *（它上头那两行写着 mirror verbatim）。两边各写一份字面量是"必须一致"这类断言躲不掉
 * 的事（和 `ui.js` 的 `STATUS_META` ↔ `engine.Status.Label()` 一样），所以两边都钉：
 * Go 侧那一份在 `app_test.go` 的 `TestDestModesMirrorTheThreeChoices`。
 *
 * 第 55 批删掉「同级逐层」时，漏掉这里的症状是**预览里能选到第四个、真程序只有一个**：
 * 选了以后 Go 侧把它当认不出的模式，静默退回「原目录」——产物落进源文件夹里。
 */
eq('输出方式：mock 是 Go 那三档的镜像', (await api.bootstrap()).options.destModes, [
  { value: 'same', label: '原目录' },
  { value: 'custom', label: '自定义目录' },
  { value: 'sibling', label: '同级目录' },
]);

/**
 * 「同级目录」在预览里的落点也要对：`api.js` 的 `PreviewPaths` 是 Go 侧
 * `app.PreviewPaths` 的镜像，而这一档正是**出包默认值** —— 它算错一次就是整批文件跑到
 * 别处去。这一批只动了同级的**名字与那个被删的兄弟档**，所以顺带把它的两条性质钉住：
 *
 * 1. 落点 = 文件所在目录的同级 + 默认后缀（面板里没有"添加目录"这层，锚只能顶替）；
 * 2. 开不开「保留目录结构」落点都一样 —— SrcRoot 就是 srcDir，相对自己是空路径。真跑
 *    一批文件时这个开关当然有作用（`dir_test.go` 的
 *    `TestResolveDestDirSiblingAnchorsOnAddedRoot` 钉的就是那边），预览里看不出来不是
 *    bug，但**不能因为看不出来就被当成没用**。
 */
const gTpl = (await api.templates()).find((t) => t.global);
const wantMmdOut = 'D:\\video\\mmd_out';
eq('同级目录：落点 = 文件所在目录的同级 + 默认后缀',
  (await api.previewPaths(gTpl, 'D:/video/mmd/a.mp4')).outputDir, wantMmdOut);
eq('同级目录：预览里只有一个文件时，「保留目录结构」改不了落点',
  (await api.previewPaths(
    { ...gTpl, outDirSpec: { ...gTpl.outDirSpec, keepTree: false } }, 'D:/video/mmd/a.mp4',
  )).outputDir, wantMmdOut);
eq('同级目录：答案下面明说"锚只能拿文件自己的目录顶替"',
  (await api.previewPaths(gTpl, 'D:/video/mmd/a.mp4')).notices
    .some((s) => s.includes('「同级目录」')), true);

/**
 * 状态的界面说法：`filtered` 和 `skipped` 必须**同一个词**，而且和 Go 那边一字不差。
 *
 * 两个内部状态（被模板的匹配条件跳过 / 本次运行已处理过）对用户是同一件事，界面分两行
 * 就是逼用户去猜内部 enum。值本身**不合并** —— 判断、搬迁、一批测试都按状态走 —— 所以
 * "两边说法一致"这件事只能靠断言钉住，没有类型能替它说话。
 *
 * Go 侧的同一条断言在 `app_test.go` 的 `TestSkippedLabelSharedAndLegacyAdopted`：
 * 那条钉的是 `engine.Status.Label()`，这条钉的是它的界面镜像 `ui.js` 的 `STATUS_META`。
 */
const { statusLabel, statusMeta } = await import(
  'file:///D:/Code-Project/ffmpeg-gui/frontend/dist/ui.js'
);
eqCall('状态说法：filtered 叫「跳过」', () => statusLabel('filtered'), '跳过');
eqCall('状态说法：skipped 也叫「跳过」', () => statusLabel('skipped'), '跳过');
eqCall('状态说法：两个跳过状态的 chip 样式一样', () => statusMeta('filtered').chip, statusMeta('skipped').chip);
// 反证：真值来自 Go 的 `Label()`，挪到界面这层还要看得见差别 —— 一句「都一样」若是把
// 整个表都写成同一个词，上面三条照样全绿。
eqCall('状态说法：跳过和完成不是一回事', () => statusLabel('filtered') !== statusLabel('done'), true);
// **状态名一律不带「已」**（2026-10-12 统一）：整张表扫一遍，比逐条列举能漏掉的地方少。
// 「暂停」是 `statusLabel` 在看 frozen 时替换出来的那一个，也在这条约束里。
eqCall('状态说法：一个都不带「已」',
  () => ['pending', 'preparing', 'running', 'done', 'warning', 'failed', 'canceled',
    'skipped', 'filtered'].map((s) => statusLabel(s)).filter((l) => l.startsWith('已')).join(','), '');
eqCall('状态说法：暂停的徽标也不带「已」', () => statusLabel('running', true), '暂停');
// 记录页那张状态表按**字面量**取色调，键必须和后端 `Label()` 同词 —— 差一个字，
// 那一行的 chip 就会悄悄退回默认灰（不会报错，只是颜色不对）。
eqCall('状态说法：取消不是跳过那个色调',
  () => statusMeta('canceled').chip !== statusMeta('skipped').chip, true);

/**
 * 队列那一颗按钮的状态机：**开始 → 暂停 → 继续**。回到「开始」有两条路：「停止」，
 * 以及**一批跑完**（后端 `emitState()` 里的 `settleLocked` 把 `armed` 清掉）。
 *
 * 为什么钉在这里：三态全是**看后端两个布尔量**推出来的，界面探针只能量到"此刻"那一个
 * （mock 开局就是 `started=true`），量不到另外两条支路；而支路写错的表现是"按钮写着
 * 暂停、按下去却在开始队列"—— 这种错在截图里一个像素都看不出来。
 *
 * 「停止之后」和「批次跑完」这两条必须都钉：前者靠 `CancelAll`，后者靠 `settleLocked`，
 * 漏掉任何一条，跑完之后右上角就会挂着两颗按了没反应的按钮 —— 那就是用户报的问题。
 */
const { runButton } = await import(
  'file:///D:/Code-Project/ffmpeg-gui/frontend/dist/views/tasks.js'
);
eqCall('队列按钮：没开始过 = 开始', () => runButton({ started: false, pending: 3 }).label, '开始');
eqCall('队列按钮：开始过、跑着 = 暂停', () => runButton({ started: true, paused: false, running: 1 }).label, '暂停');
eqCall('队列按钮：开始过、按住了 = 继续', () => runButton({ started: true, paused: true, running: 1 }).label, '继续');
// 停止（后端把 `armed` 清掉）是回到「开始」的第一条路。
eqCall('队列按钮：停止之后回到开始', () => runButton({ started: false, paused: false }).label, '开始');
// **批次跑完这一档不由前端改写**：队列跑空之后后端会先把 `armed` 清掉，所以
// `started:true` 配 `pending:0 / running:0` 在真程序里不会存在。纯函数仍按表返回「暂停」
// —— 判据留在后端一处，按钮这边不偷看 `pending` 自己"修正"成开始，否则就成了"按钮写着
// 开始、后端还活着"：再加一个文件它会自己跑。
eqCall('队列按钮：批次跑完这一档不由前端改写（后端会先退回未开始）',
  () => runButton({ started: true, paused: false, running: 0, pending: 0 }).label, '暂停');
// 动作和标签出自同一个函数：动作错位就是"写着暂停、按下去在开始"。
eqCall('队列按钮：只有未开始那一态的动作才是 start',
  () => ['start', 'toggle', 'toggle'].map((_, i) => runButton(
    [{ started: false }, { started: true, paused: false }, { started: true, paused: true }][i]
  ).act).join(','), 'start,toggle,toggle');
// 重按一次的颜色：开始和继续是实底（把活推起来的那一下），暂停是普通（活正在跑）。
eqCall('队列按钮：开始是实底', () => runButton({ started: false }).filled, true);
eqCall('队列按钮：暂停不是实底', () => runButton({ started: true, paused: false }).filled, false);
eqCall('队列按钮：继续是实底', () => runButton({ started: true, paused: true }).filled, true);
// 「开始」灰不灰：判据是 `pending`（有没有一行等着跑），**不是** `total`（列表里有几行）。
// 空队列上按它只是把队列武装起来，屏幕上一行都不会动 —— 那和程序坏了长得一样。
eqCall('队列按钮：空队列的「开始」灰着', () => runButton({ started: false, pending: 0, total: 0 }).disabled, true);
eqCall('队列按钮：有排队中的就能按', () => runButton({ started: false, pending: 3, total: 9 }).disabled, false);
// 非空列表但一行可跑的都没有（全是完成 / 取消 / 跳过）—— 按「开始」同样没反应，
// 这时候旁边那颗「重试失败」是亮的，想接着跑走那条路。所以判据不能写成 `total === 0`。
eqCall('队列按钮：列表非空但没得跑，一样灰着',
  () => runButton({ started: false, pending: 0, total: 5, done: 5 }).disabled, true);
// 「暂停」「继续」看的是队列这个开关本身，和列表里有几行无关：队列活着的时候把之后加进来
// 的文件按住是一件真事，哪怕此刻一行都没有。
eqCall('队列按钮：暂停不灰', () => runButton({ started: true, paused: false, pending: 0, total: 0 }).disabled, false);
eqCall('队列按钮：继续不灰', () => runButton({ started: true, paused: true, pending: 0, total: 0 }).disabled, false);
// 反证：三态里只有「开始」有灰的余地，另外两态永远可点。
eqCall('队列按钮：只有开始那一态会灰',
  () => [[false, false], [true, false], [true, true]].map(([started, paused]) => runButton({
    started, paused, pending: 0, total: 0,
  }).disabled).join(','), 'true,false,false');

/**
 * 上面钉的是纯函数，下面走一遍 **mock 自己的**那三个队列接口。
 *
 * mock 是镜子：`StopQueue` 要是没把 `started` 清掉，浏览器预览里那颗按钮停止之后就会
 * 继续显示「暂停」，而真程序里它是「开始」—— 预览比真实现好看，正是最坏的一种偏差。
 *
 * `eqCall` 只收同步函数而 `api.stats()` 是异步的，所以先取快照再断言。
 */
const eqRunLabel = async (name, want) => {
  const snap = await api.stats();
  eqCall(name, () => runButton(snap).label, want);
};

// 先备份：`StopQueue` 会把排队中的状态改成「取消」（照 `Runner.CancelAll`），下面要
// 按 id 还回去 —— 否则后面几十条断言看到的是另一个世界（`pending` 变 0、`canceled` 变 4）。
const keepJobs = (await api.jobs()).slice();
const statBackup = new Map(keepJobs.map((j) => [j.id, { status: j.status, progress: j.progress, message: j.message }]));

await api.startQueue();
await eqRunLabel('mock 队列：开始之后是暂停', '暂停');
await api.togglePause();
await eqRunLabel('mock 队列：按一下暂停之后是继续', '继续');
await api.togglePause();
await eqRunLabel('mock 队列：再按一下回到暂停', '暂停');
const beforeStop = await api.stats();
await api.stopQueue();
await eqRunLabel('mock 队列：停止之后是开始', '开始');
// 停止 = 把排队中的全丢成「取消」（`Runner.CancelAll`），所以此刻一行可跑的都没有 ——
// 那颗「开始」是灰的，旁边「重试失败」是亮的（它管 failed/canceled/skipped）。
// 这两条同时钉住 mock 的 `StopQueue` 有没有照抄那一步：漏掉的话预览里 pending 还是 3、
// 按钮还亮着，而真程序里已经是灰的。
const stoppedSnap = await api.stats();
eqCall('mock 队列：停止之后没有排队中的了', () => stoppedSnap.pending, 0);
// 丢掉的不是消失：它们排进了「取消」，数目正好是刚才还在排队 / 在跑的那些（不写死，
// mock 加一行也不会假红）。
eqCall('mock 队列：丢掉的那些进了「取消」',
  () => stoppedSnap.canceled - beforeStop.canceled, beforeStop.pending + beforeStop.running);
eqCall('mock 队列：停止之后「开始」灰着', () => runButton(stoppedSnap).disabled, true);
// 再把行全移掉（用户真的会这么干）：还是灰的，total 归零。
await api.removeJobs(keepJobs.map((j) => j.id));
const emptySnap = await api.stats();
eqCall('mock 队列：清空之后「开始」灰着', () => runButton(emptySnap).disabled, true);
eqCall('mock 队列：清空之后 total 归零', () => emptySnap.total, 0);
// **队列跑空了就是「未开始」**：Go 侧那句在 `emitState()` 里的 `settleLocked`（跑完一批、
// 移走最后一行都走它），mock 在 `mockQueueStats()` 里照抄。少了这一条，预览里把行全移完
// 之后按钮会停在「暂停」—— 真程序里它已经是「开始」了，而预览比真实现好看正是最坏的一种偏差。
eqCall('mock 队列：清空之后回到未开始', () => emptySnap.started, false);
// 还原：行塞回去、状态按 id 还回去，后面的断言看到的是同一个世界。
(await api.jobs()).push(...keepJobs);
for (const j of await api.jobs()) Object.assign(j, statBackup.get(j.id) || {});
await api.startQueue();

/**
 * 右上角那五颗计数：**总任务进度 / 完成 / 失败 / 取消 / 跳过**。
 *
 * 第一条是这条改动的核心约定：**百分比取后端的 `progress`、前端不自己除一遍** ——
 * 托盘悬停文字念的是同一个字段，两处不一致的表现是"悬停说 89%、工具栏说 88%"。
 * 所以这里故意喂一个和计数器**对不上**的 `progress`（0.44 配 5/9）：真值若改回
 * `finished / total`，这条立刻报红 —— 而它报红是对的，那时候"谁说了算"就变了。
 *
 * 另外几条钉的是分组口径：同一类任务在下拉里算一类、在计数里也该算一类（筛出 5 条、
 * chip 说 3 条是最难解释的一类不一致）。**取消是唯一不合的那一个** —— 状态筛选里它
 * 本来就单占一项，所以这里也单占一颗，而且**不能算进「失败」**：用户按一下「停止」，
 * 排队中和在跑的整批都会变成它，算成失败就是把自己按的那一下报成程序出错。
 *
 * 但"不算失败"不等于"不算数"：它仍然是一行的终态，仍然要进进度那个分子。所以这里两条
 * 一起钉 —— 只钉前者的话，把 canceled 从 `finished` 里一起摘掉也能全绿，而那会让
 * `进度 12 / 12` 和四颗 chip 对不上账。
 */
const { queueCounts, queueIdle } = await import(
  'file:///D:/Code-Project/ffmpeg-gui/frontend/dist/views/tasks.js'
);
/** 一份固定的 stats 底子，只改被测的那一项。 */
const counts = (over) => queueCounts({
  total: 9, done: 1, warning: 1, failed: 1, canceled: 0, skipped: 1, filtered: 1, progress: 0.44, ...over,
});
eqCall('计数：百分比取后端的 progress，不在前端除一遍', () => counts({}).progressText, '进度 5 / 9 · 44%');
eqCall('计数：分子是四个分组之和', () => counts({}).finished, 5);
eqCall('计数：完成含「完成(警告)」', () => counts({}).doneText, '完成 2');
eqCall('计数：取消不算在「失败」里', () => counts({ canceled: 3 }).failedText, '失败 1');
// 反证：真失败还照旧算 —— 上面那条只看 `canceled` 的话，把 failed 整个漏掉也能全绿。
eqCall('计数：「失败」只数真失败', () => counts({ failed: 4 }).failedText, '失败 4');
eqCall('计数：取消单占一颗', () => counts({ canceled: 3 }).canceledText, '取消 3');
// 不算失败，但**仍然算数**：多出来的那一项是 progress 的分子里的一部分。
eqCall('计数：取消仍然进进度分子', () => counts({ canceled: 3 }).finished, 8);
// 四组之和必须正好是进度分子，一颗不多一颗不少 —— 这是"加了第五颗 chip"能有的唯一前提。
eqCall('计数：四颗分组之和 = 进度分子',
  () => {
    const c = counts({ canceled: 3 });
    return c.done + c.failed + c.canceled + c.skipped === c.finished;
  }, true);
eqCall('计数：被匹配条件跳过的算在「跳过」里', () => counts({ skipped: 0, filtered: 4 }).skippedText, '跳过 4');
eqCall('计数：空队列不写「0 / 0 · 0%」', () => queueCounts({ total: 0 }).progressText, '还没有任务');
eqCall('计数：空队列里没有 NaN', () => /NaN/.test(queueCounts({ total: 0 }).progressText), false);
// 反证：一整排字段缺席时也要念出五个 0，而不是把 undefined 拼进字符串里。
eqCall('计数：字段缺席当 0',
  () => ['doneText', 'failedText', 'canceledText', 'skippedText'].map((k) => queueCounts({})[k]).join(' '),
  '完成 0 失败 0 取消 0 跳过 0');

/**
 * 「清空」那颗按钮亮不亮（`queueIdle`）。
 *
 * 判据只有一条：后端的 `armed` 是空的 —— 「未开始」「停止之后」「一批跑完」落在同一个
 * 值上（`Runner.Disarm` / `CancelAll` 都清 `armed`，跑空一批则由 `emitState()` 里的
 * `settleLocked` 清），所以这里量"停止之后"和量"从没开始过"是同一个断言。
 *
 * 反过来那两条才是重点：**队列真的还在转的时候它必须是灰的**。跑着、按着暂停都算活着；
 * 写漏任何一条，用户就能在一片"还在转"的任务上按「清空」。
 */
eqCall('清空：还没开始过 = 可点', () => queueIdle({ started: false, total: 3 }), true);
eqCall('清空：停止之后 = 可点', () => queueIdle({ started: false, paused: false, total: 3 }), true);
eqCall('清空：跑着 = 灰着', () => queueIdle({ started: true, paused: false, running: 2 }), false);
eqCall('清空：按着暂停 = 灰着', () => queueIdle({ started: true, paused: true }), false);
// `started:true` 配"没有一行待跑也没有一行在跑"这一档，后端不会让它停在那儿（`settleLocked`
// 会先退回未开始），留着这条是钉住 `queueIdle` **只读 `started`**、不偷看计数 —— 判据一散
// 成两处，"灰着还是亮着"就又有两个出处了。
eqCall('清空：一批跑完（后端会先退回未开始）',
  () => queueIdle({ started: true, paused: false, running: 0, pending: 0, total: 9 }), false);
// 反证：三态里只有 `started === false` 那一态亮着 —— 上面四条若都写成同一个词，这条报红。
eqCall('清空：只有"没开始"那一态可点',
  () => [[false, false], [true, false], [true, true]].map(([started, paused]) => queueIdle({
    started, paused, total: 3,
  })).join(','), 'true,false,false');
// 载荷里没有这个键时按"已经开始了"处理：这颗按钮管的是不可撤销的清空，**灰着比亮着安全**。
// （读法和 `runButton()` 的 `s.started !== false` 一致 —— 两处对同一个字段的解释必须一样。）
eqCall('清空：stats 里没有 started 时灰着', () => queueIdle({ total: 3 }), false);
eqCall('清空：没有 stats 时灰着', () => queueIdle(undefined), false);

/**
 * mock 那份 stats 现在是**现算**的（`api.js` 的 `mockQueueStats()`），所以"进度"和
 * 计数器必须自洽 —— 它手写过 `progress: 0.44` 配 5/9（差 12 个百分点），而工具栏把
 * 这两个数印在同一颗 chip 上。手写的那份还不会跟着 `RemoveJob` 走：`total` 会留在 9。
 */
const LIVE = await api.stats();
const JOBS = await api.jobs();
eqCall('mock：total = jobs 行数', () => LIVE.total, JOBS.length);
eqCall('mock：进度和计数器自洽', () => {
  const fin = LIVE.done + LIVE.warning + LIVE.failed + LIVE.canceled + LIVE.skipped + LIVE.filtered;
  return Math.round((fin / LIVE.total) * 100);
}, Math.round(LIVE.progress * 100));
// 拿最后一行试一次移除：手写的那份 stats 纹丝不动，现算的这份必须跟着掉。
// （`eqCall` 只收同步函数，所以先把快照 await 出来再断言。）
const lastJob = JOBS[JOBS.length - 1];
await api.removeJobs([lastJob.id]);
const afterRemove = await api.stats();
eqCall('mock：total 跟着移除走', () => afterRemove.total, JOBS.length - 1);
// 塞回去 —— `RemoveJobs` 是**重新赋值**数组，所以得重新取一份引用，塞回末尾正好还原顺序。
(await api.jobs()).push(lastJob);
const afterRestore = await api.stats();
eqCall('mock：塞回之后 total 复原', () => afterRestore.total, JOBS.length);

/**
 * 移除必须**广播** `queue:state`，和 Go 里那三行 `r.emitState()` 一一对应。
 *
 * 这条漏了在界面上看不出来，而且症状很具体：前端 `removeOne()` 自己会重拉 `Jobs()`，
 * 所以**行**照样消失；但工具栏那几颗计数 chip 是 `onStats()` 画的，只有广播叫得动它
 * —— 于是「移除 3 行之后进度还写着 9」。数出来的东西和数它的那一列打架，比两个都停在
 * 旧值更像"这程序算错了"。
 *
 * 两次移除分别对应 Go 的两个分支：`RemoveJob` 无条件发，`RemoveJobs` 只在真删掉东西
 * 时发（空批次什么都不发）。所以这里也照一次空批次，钉住"不是有调用就发"。
 */
const { on: onEvent } = await import('file:///D:/Code-Project/ffmpeg-gui/frontend/dist/api.js');
const seen = [];
const offEvents = onEvent('queue:state', (s) => seen.push(s));
const victim = (await api.jobs()).slice(-1)[0];
await api.removeJob(victim.id);
eqCall('mock：移除之后广播了 stats', () => seen.length, 1);
eqCall('mock：广播的就是移除后的那一份', () => seen[0] && seen[0].total, JOBS.length - 1);
await api.removeJobs([]);
eqCall('mock：空批次不广播（照 Go 的 removed > 0）', () => seen.length, 1);
// 塞回去，别让后面的断言看到少一行的队列。
(await api.jobs()).push(victim);
offEvents();

/** 一组目录规则。字段顺序和 `normalizeDirRules` 一致，好让整条规则能直接比。 */
const dirs = (filters, extra = {}) => ({
  enabled: true, filters, matchAll: false, exclude: false, topOnly: false, ...extra,
});
/** 一组文件规则：形状和目录规则一样，但没有「含子目录」。 */
const files = (filters, extra = {}) => ({
  enabled: true, filters, matchAll: false, exclude: false, ...extra,
});
const contains = (value) => [{ mode: 'contains', value }];
const suffix = (value) => [{ mode: 'suffix', value }];
const NONE = files([]);

/** 存一套叫「测试」的方案并切到它 —— 之后"真跑一次"的行为都按它走。 */
const useProfile = async (d, f) => {
  await api.saveFilterProfile('', { name: '测试', dirs: d, files: f });
  await api.setActiveFilter('测试', false);
};

/** 按页面上那套去问后端。预览的 `recursive` 是**已经算完的**往下收多深。 */
const scan = (d, f, recursive = true, dir = 'D:/Media/big') =>
  api.previewFolderScan({ dir, dirs: d, files: f, recursive });

/* ------------------------------------------------------------------ 方案表 */
await api.deleteFilterProfiles(['测试']);

// 基线从 mock 自己读，不在断言里抄一份名单。
//
// 抄名单等于把"mock 开局长什么样"复制到第二个地方，于是往 mock 里加一套方案（比如为了
// 走出某条分支）会让十几条断言一起红，而红的原因跟被测的行为无关 —— 是断言过期了。
// 该问的从来是"相对开局那张表发生了什么"，所以基准就该是开局那张表本身。
const s0 = await api.filterState();
const BASE = s0.profiles.map((p) => p.name);
const BASE_PROFILES = s0.profiles.map((p) => JSON.parse(JSON.stringify(p)));
const BASE_ACTIVE = s0.active;
const BASE0 = BASE[0];
/** 基线 + 追加的名字，用来断言"表里现在有哪些"。 */
const withNames = (...extra) => [...BASE, ...extra];

// 在用的那套必须是表里真实存在的一套，且没停在「不使用过滤」上。
// mock 偏偏把**有条件、没说明**的那套设成 `activeFilter`（理由见 api.js 里那段注释）：
// 那正是 `MODE_LABEL is not defined` 会炸的形状，而默认那套两组都是空的，永远走不到。
eq('开局在用的是表里那套、没禁用', [s0.active, s0.off, s0.profile.name],
  [BASE_ACTIVE, false, BASE_ACTIVE]);
eq('开局方案名不重复', new Set(BASE).size, BASE.length);
// 下面所有"某一套的形状"断言都指着它，所以它必须真的有条件（不然整个文件测不到那条分支）。
eq('开局在用的那套有条件', s0.profile.dirs.filters.length > 0 || s0.profile.files.filters.length > 0, true);

const saved = await api.saveFilterProfile('', { name: ' 批次 ', dirs: dirs(contains('ffmpeg__'), { topOnly: true }), files: NONE });
eq('名字去空白', saved.profiles.map((p) => p.name), withNames('批次'));
eq('新方案追加在末尾', saved.profiles[saved.profiles.length - 1].name, '批次');
eq('存一套新方案不动当前生效的那套', saved.profile.name, BASE_ACTIVE);

const overwrite = await api.saveFilterProfile('', { name: '批次', dirs: dirs(suffix('__h265'), { exclude: true }), files: NONE });
eq('同名覆盖在原位置', overwrite.profiles.map((p) => p.name), withNames('批次'));
eq('覆盖后条件跟着换', overwrite.profiles[BASE.length].dirs.filters, [{ mode: 'suffix', value: '__h265' }]);
eq('覆盖后方向也跟着换', overwrite.profiles[BASE.length].dirs.exclude, true);

const ci = await api.saveFilterProfile('', { name: '批次', dirs: dirs([]), files: NONE });
eq('大小写不敏感也算同名', ci.profiles.length, BASE.length + 1);
eq('同名覆盖不会留下旧的那份', ci.profiles[BASE.length].dirs.filters, []);

const dropped = await api.deleteFilterProfiles(['批次']);
eq('删掉一套之后回到开局那张表', dropped.profiles.map((p) => p.name), BASE);

// 空名字必须被拒：放进去就是方案表里两行一样的名字，用户再也分不清选的是哪一行。
let emptyErr = '';
try {
  await api.saveFilterProfile('', { name: '   ', dirs: dirs([]), files: NONE });
} catch (e) { emptyErr = e.message; }
eq('空名字被拒', emptyErr, '方案要有名字');
eq('被拒之后方案表没变', (await api.filterState()).profiles.map((p) => p.name), BASE);

/* ------------------------------------------------- 目录规则：往下收多深 */
const H265 = dirs(contains('h265'));
const all = await scan(H265, NONE, true);
const top = await scan(H265, NONE, false);
log('h265 含子目录:', JSON.stringify(all));
log('h265 只看一层:', JSON.stringify(top));
// 含子目录：ffmpeg__batch01/h265(3) + h265-archive(6) + other/deep/h265(2)
eq('含子目录收整棵树里的三处', all.totalFiles, 11);
eq('只看一层只认直接子目录里的那一个', top.totalFiles, 6);
eq('含子目录看过的候选多', all.scanned > top.scanned, true);
eq('只看一层不会挑出更深的那两个', top.dirs.map((d) => d.rel), ['h265-archive']);
eq('没开文件规则时一个文件都没被筛掉', [all.filteredFiles, top.filteredFiles], [0, 0]);

/* --------------------------------------------------------- 排除方向 */
const ex = dirs(contains('h265'), { exclude: true });
const exAll = await scan(ex, NONE, true);
const exTop = await scan(ex, NONE, false);
log('排除 + 含子目录:', JSON.stringify(exAll));
log('排除 + 只看一层:', JSON.stringify(exTop));
eq('排除方向列的是被跳过的目录', exAll.dirsTotal, 3);
eq('排除方向总数 = 全部 - 跳过的', exAll.totalFiles, 31 - 11);
eq('排除方向的结果顶上写着"排除"', [exAll.filtering, exAll.exclude], [true, true]);
eq('只看一层时一个子目录都不跳（根本没往里看）', [exTop.dirsTotal, exTop.totalFiles], [0, 3]);
eq('总数不为负', exAll.totalFiles >= 0 && exTop.totalFiles >= 0, true);

/* ----------------------- 排除方向：用户挑的那个文件夹自己的名字也过条件 -------------- */
/*
 * 用户报的第二条：条件「包含 FFMPEG + 排除」，在「试一个文件夹」里挑了一个自己就叫
 * `FFMPEG__1__H265_Slow_21_2560` 的目录（里面正好 48 个文件），面板上却是
 * 「排除 0 个目录 · 收 48 个文件」—— 按条件读，那个文件夹显然是"整个都不要"，而这个
 * 结果和"排除根本没生效"长得一模一样。
 *
 * 夹具里根叫 big，所以拿"条件包含 big + 排除"复现同一件事：整个文件夹都不要，而列表里
 * 那一行就是它自己（还要报出会跟着没的文件数）。这一判据在"往下收多深"之前，含子目录和
 * 只看一层都给"一个都不收"，差别只在那一行数的是整棵还是一层。
 */
const exSelf = dirs(contains('big'), { exclude: true });
const exSelfAll = await scan(exSelf, NONE, true);
const exSelfTop = await scan(exSelf, NONE, false);
log('排除 + 挑中的文件夹自己命中（含子目录）:', JSON.stringify(exSelfAll));
log('排除 + 挑中的文件夹自己命中（只看一层）:', JSON.stringify(exSelfTop));
eq('挑中的文件夹自己命中 → 一个文件都不收', [exSelfAll.dirsTotal, exSelfAll.totalFiles], [1, 0]);
eq('那一行就是它自己，并报出会跟着没的文件数',
  exSelfAll.dirs.map((d) => [d.dir, d.name, d.rel, d.files]),
  [['D:\\Media\\big', 'big', 'big', 31]]);
eq('只看一层同样整个不要（那一行数的是一层的文件）',
  [exSelfTop.dirsTotal, exSelfTop.totalFiles, exSelfTop.dirs[0].files], [1, 0, 3]);
eq('名字不命中时排除方向照旧（整个文件夹减去命中的子树）', exAll.totalFiles, 31 - 11);

/* ------------------------------- 两组规则互不相干：文件规则只管文件 -------------- */
// 夹具里没有 .tmp，所以"只收 h265"那套方案自带的文件规则（排除 .tmp）在夹具上是一条
// 都不命中的 —— 夹具得挑一条真的会命中的规则，否则这一段测的是"什么都没发生"。
const NO_MKV = files(suffix('.mkv'), { exclude: true });
const dAndF = await scan(H265, NO_MKV, true);
eq('文件规则不改挑中的目录', dAndF.dirs.map((d) => d.rel), all.dirs.map((d) => d.rel));
eq('目录规则那一组不受文件规则影响', dAndF.dirsTotal, all.dirsTotal);
eq('文件规则把 .mkv 从结果里去掉', dAndF.totalFiles, 11 - 4);
eq('被筛掉几个也报出来', dAndF.filteredFiles, 4);

// 反过来：换一套目录规则，文件规则照样在它挑中的那几个目录里生效。
const onlyArchive = await scan(dirs(contains('archive')), NO_MKV, true);
eq('换一套目录规则后文件规则照旧生效', onlyArchive.totalFiles, 6 - 2);
eq('换一套目录规则后筛掉的数也跟着换', onlyArchive.filteredFiles, 2);

// 收的方向：只留命中的那些。
const onlyMkv = await scan(H265, files(suffix('.mkv')), true);
eq('文件规则也能反过来只留命中的', onlyMkv.totalFiles, 4);
eq('只留的时候"筛掉"报的是没留下的', onlyMkv.filteredFiles, 7);

/* ------------------------- 命中所选目录自己（用户报的那条） ------------------------- */
// 用户原话："条件包含 1，测试 D:\123，这样这个应该是符合条件的，里面的视频文件要收
// 录进来，但是实际并没有"。夹具里根叫 big，所以拿"条件包含 big"复现同一件事。
const selfAll = await scan(dirs(contains('big')), NONE, true);
const selfTop = await scan(dirs(contains('big')), NONE, false);
log('命中根（含子目录）:', JSON.stringify(selfAll));
log('命中根（只看一层）:', JSON.stringify(selfTop));
eq('根自己命中 → 含子目录收整棵', selfAll.totalFiles, 31);
eq('根自己命中 → 只看一层收它那一层', selfTop.totalFiles, 3);
eq('第一行就是用户挑的那个目录', [selfAll.dirs[0].rel, selfAll.dirs[0].name], ['big', 'big']);
eq('根也计入看过的候选', selfAll.scanned, 9);

// 根和子目录同时命中：根整棵都收进来了，嵌套的报 0（每个文件只算一次，逐行相加
// 等于总数）。条件 包含 "b"：根 big 命中，直接子目录里只有 ffmpeg__batch01 /
// ffmpeg__batch02 命中（比的是目录名，`ffmpeg__batch01/h265` 那一段是 "h265"，
// 而 h265-archive 里没有 b）。
const both = await scan(dirs(contains('b')), NONE, true);
log('根与子目录同时命中:', JSON.stringify(both));
eq('根 + 两个子目录 = 三行', both.dirsTotal, 3);
eq('嵌套命中的报 0', both.dirs.slice(1).map((d) => d.files), [0, 0]);
eq('逐行相加等于总数', both.dirs.reduce((n, r) => n + r.files, 0), both.totalFiles);

/* ------------------------------------------------------ 两种名字、四种方式 */
// 目录名。后缀和其它三种的差别在夹具里看得很清楚：`h265-archive` 是 h 开头、e 结尾 ——
// 前缀/包含/通配符都命中它，后缀不该。
const dirsBy = async (mode, value) => (await scan(dirs([{ mode, value }]), NONE, true)).dirs.map((d) => d.rel);
eq('目录名 包含 h265', await dirsBy('contains', 'h265'), ['ffmpeg__batch01\\h265', 'h265-archive', 'other\\deep\\h265']);
eq('目录名 前缀 h265', await dirsBy('prefix', 'h265'), ['ffmpeg__batch01\\h265', 'h265-archive', 'other\\deep\\h265']);
eq('目录名 后缀 h265', await dirsBy('suffix', 'h265'), ['ffmpeg__batch01\\h265', 'other\\deep\\h265']);
eq('目录名 通配符 h265*', await dirsBy('glob', 'h265*'), ['ffmpeg__batch01\\h265', 'h265-archive', 'other\\deep\\h265']);
eq('目录名 大小写不敏感', await dirsBy('contains', 'H265'), ['ffmpeg__batch01\\h265', 'h265-archive', 'other\\deep\\h265']);

// 文件名。这两组走的是同一段比较函数，但它是**两组规则**里各调一次 —— 一边换掉实现
// 另一边没跟上，就是"目录筛对了、文件筛错了"，而界面上两边长得一模一样。
const fileTotal = async (r, recursive = true) => (await scan(NONE, r, recursive)).totalFiles;
eq('文件名 包含 h265', await fileTotal(files(contains('h265'))), 3);
eq('文件名 前缀 arc-', await fileTotal(files([{ mode: 'prefix', value: 'arc-' }])), 6);
eq('文件名 后缀 .mkv', await fileTotal(files(suffix('.mkv'))), 8);
eq('文件名 通配符 b01-*', await fileTotal(files([{ mode: 'glob', value: 'b01-*' }])), 8);
eq('文件名 排除 .mp4 之后剩下别的', await fileTotal(files(suffix('.mp4'), { exclude: true })), 31 - 21);
// 只看一层时，文件规则能看到的就只有直接放在所选目录里的那三个（root-1.mp4、
// root-2.mp4、root-3.mkv）—— 子目录里的文件根本还没被收进来，谈不上筛。
eq('只看一层时文件规则只在根那一层里筛（后缀 .mkv → root-3.mkv）', await fileTotal(files(suffix('.mkv')), false), 1);
eq('只看一层时排除 .mp4 也是同一个口径', await fileTotal(files(suffix('.mp4'), { exclude: true }), false), 1);
eq('两组都空时"命中的文件数"就是整棵树', await fileTotal(NONE), 31);

/* ------------------------------------------------- 真加入与预览对得上 */
await useProfile(H265, NONE);
const addedAll = await api.addFolderDialog(true);
const addedTop = await api.addFolderDialog(false);
log('加一次（含子目录）:', JSON.stringify(addedAll));
log('加一次（只看一层）:', JSON.stringify(addedTop));
// 「添加文件夹」在 mock 里用的是 `D:/Media/small`（和 `PickDirectory` 同一个目录），
// 而 `small` 不命中 "h265"，所以这两个数和上面按 big 算出来的一模一样。
eq('真加入 == 预览（含子目录）', addedAll.added, all.totalFiles);
eq('真加入 == 预览（只看一层）', addedTop.added, top.totalFiles);
// 方案说只看这一层时，调用方给的"递归"被收窄成一层。
await useProfile(dirs(contains('h265'), { topOnly: true }), NONE);
eq('方案说只看这一层时，连"拖进来递归"也收窄', (await api.addFolderDialog(true)).added, 6);

// 命中所选目录自己时，真加入也要跟着走（这一段是上面那条用户反馈的另一半）。
await useProfile(dirs(contains('small')), NONE);
const addedSelf = await api.addFolderDialog(true);
log('命中所选目录 + 真加入:', JSON.stringify(addedSelf));
eq('命中所选目录 → 含子目录收整棵', addedSelf.added, 31);
eq('没有"没有子目录符合"这种反话', addedSelf.errors, []);
await useProfile(dirs(contains('small'), { topOnly: true }), NONE);
eq('命中所选目录 → 只看一层只收它那一层', (await api.addFolderDialog(true)).added, 3);

/* ------------------------------------------------ 点名的文件也要过文件规则 */
// 「添加文件」和「拖进来」在 Go 侧汇到同一个落点（`runner.AddInputs` 里那段），所以
// 这里也只有一份实现：两条路各判一次的话，"拖进来一个文件"和"拖进来一个文件夹、里面
// 正是同一个文件"会有两种结果。
await useProfile(NONE, NO_MKV);
const picked = await api.addFilesDialog(true);
eq('点名的文件一样过文件规则', picked.added, 2);
eq('被排掉的那个把话说出来', picked.errors, ['root-3.mkv: 被文件过滤条件排除']);

// mock 的拖入用的是固定那几个文件（`MOCK_DROPPED_FILES`：a.mp4 + b.mkv），参数不参与
// —— 浏览器里没有真实拖放，这里要验的是"这批文件过不过同一套判据"。
const droppedFiles = await api.addDroppedFiles(['D:/Media/drop/a.mp4']);
eq('拖进来的文件走同一条判据', [droppedFiles.added, droppedFiles.errors], [1, ['b.mkv: 被文件过滤条件排除']]);

/* ------------------------------------------- 关掉过滤时两个方向都是整棵树 */
const offRules = dirs(contains('h265'), { enabled: false, exclude: true });
await useProfile(offRules, NONE);
log('过滤关掉 + 拖进来:', JSON.stringify(await api.addFolderDialog(true)));
eq('过滤关掉 = 整个文件夹照收', (await api.addFolderDialog(true)).added, 31);
eq('关掉时"排除"方向也不参与', (await scan(offRules, NONE, true)).exclude, false);
eq('关掉时含子目录照旧算数（它不是过滤的一部分）', (await scan(offRules, NONE, false)).totalFiles, 3);

/* ------------------------------- 任务页下拉：选哪套就落盘 + 「不使用过滤」只在本次 */
// 「不要用」的那套得挑个真的没条件的，才能量出"选它 = 一个都不筛"。基线里第一套
// 未必没条件（mock 有意让生效那套带条件，见上面），所以按形状找，不按位置猜。
const INERT = BASE_PROFILES.find((p) => !rulesActive(p.dirs) && !rulesActive(p.files)) || BASE_PROFILES[0];
const COND = BASE_PROFILES.find((p) => p.name === BASE_ACTIVE) || BASE_PROFILES[0];
await api.setActiveFilter(INERT.name, false);
const chosen = await api.setActiveFilter(COND.name, false);
eq('下拉选的那套就是此刻生效的', [chosen.active, chosen.profile.name], [COND.name, COND.name]);
eq('没有第二份"默认那套"要分庭抗礼', chosen.off, false);
// 引擎读的是同一份判据：下拉选中哪套，真跑加进去的个数就得等于**预览**扫出来的
// `totalFiles`（`mockEnqueueFolder` 返回的就是它，不是另算一遍）。
//
// 基准取自预览而不是写死一个数字：写死的话，mock 换一套生效方案（或者那套的规则改了）
// 这条就红，而红的原因跟"下拉有没有真的生效"无关 —— 它量的其实是"mock 那套规则算出来
// 是不是恰好等于 11"。收的方向要收满整个树、排除的方向会把命中项全减掉，两种都照抄 11
// 就等于假设生效的那套永远是收的方向，而 mock 有意让它不是（见 api.js 里 activeFilter）。
const pv = await scan(COND.dirs, COND.files, true);
eq('真跑一次按选中的那套走', (await api.addFolderDialog(true)).added, pv.totalFiles);
eq('选中的那套写进设置文件（下次打开从它开始）', (await api.filterState()).active, COND.name);

const bogus = await api.setActiveFilter('查无此套', false);
eq('对不上的名字按第一套处理', bogus.profile.name, BASE0);

// 「禁用」：两组都不参与，而且它**落盘** —— 关掉程序再打开还是禁用的。
//
// 这条以前反着写（"禁用不落盘，设置文件里还是那套"）。那是当时的决定，用户用下来否掉了：
// 他拿这颗当"把这个功能关掉"用，关了程序又被自动接上，等于每开一次都要手动关一次。
await api.setActiveFilter(COND.name, false);
const off = await api.setActiveFilter(COND.name, true);
eq('状态里报出禁用', off.off, true);
eq('禁用时交出去的规则不筛', [off.profile.dirs.enabled, off.profile.files.enabled], [false, false]);
eq('禁用 = 整个文件夹照收', (await api.addFolderDialog(true)).added, 31);
// 落盘了没有 —— 这才是用户报的那件事（"重新打开程序会变成方案的"）。
// 从 `api.settings()` 读，而不是去够 mock 内部的 `MOCK_SETTINGS`：这条问的正是"设置文件里
// 写了什么"，而 `Settings` 就是那件事的出口（设置页也靠它整体回写，所以走它还能顺带验证
// 那颗开关不会被一次保存抹掉）。
eq('禁用落盘了', (await api.settings()).filterOff, true);
// 同时**不动**「上次用的是哪套」：那颗下拉里"禁用"和"某一套"是并列选项，用户切回某一套
// 时要回到他原来挑的那套。
eq('禁用不动「上次用的是哪套」', (await api.filterState()).active, COND.name);
const offBack = await api.setActiveFilter(COND.name, false);
eq('关掉禁用回到那套', [offBack.off, offBack.profile.name], [false, COND.name]);
eq('关掉禁用也落盘了', (await api.settings()).filterOff, false);
// 设置页保存是**整体**替换 `Settings`：它交回去的那一份必须带着 filterOff，否则用户在设置页
// 点一次保存就把禁用状态悄悄打开了。这里模拟一次"原样交回"。
await api.setActiveFilter(COND.name, true);
await api.saveSettings(await api.settings());
eq('设置页整体保存不会把禁用抹掉', (await api.settings()).filterOff, true);
await api.setActiveFilter(COND.name, false);

// 正在用的那套被改名时，设置里记的名字跟着走：否则它指着一个已经不存在的名字，下一次
// 取生效方案时会静默退回第一套，而用户手里什么都没换。
//
// 旧名字一定要一起交出去：名字是方案唯一的标识，后端只收到新名字的话，"改名"在它眼里
// 就是"多一套"—— 旧的还在，表里出现两行分不清谁是谁的东西。
const RENAMED = '改名过的';
await api.setActiveFilter(COND.name, false);
const renamed = await api.saveFilterProfile(COND.name, { name: RENAMED, dirs: H265, files: NONE });
eq('改名时正在用的那套跟着走', [renamed.active, renamed.profile.name], [RENAMED, RENAMED]);
// 位置按**下标**断言，不按"名字集合对得上"：后者分不出"原地改名"和"删了再加"，
// 而那正是这个参数存在的理由（下面 `不给旧名字` 那条量的是反面）。
const at = (list, name) => list.indexOf(name);
eq('改名不改变它排在列表里的位置', [at(renamed.profiles.map((p) => p.name), RENAMED),
  renamed.profiles.length], [at(BASE, COND.name), BASE.length + 1]);
const renamedBack = await api.saveFilterProfile(RENAMED, { name: COND.name, dirs: H265, files: NONE });
eq('再改回来还在同一个位置', [at(renamedBack.profiles.map((p) => p.name), COND.name),
  renamedBack.profiles.length], [at(BASE, COND.name), BASE.length + 1]);

// 反过来量一次"不给旧名字"会怎样：这就是上面那个参数存在的理由。
const extra = await api.saveFilterProfile('', { name: RENAMED, dirs: H265, files: NONE });
eq('不给旧名字 = 又多出一套（不是改名）', [extra.profiles.length,
  at(extra.profiles.map((p) => p.name), RENAMED)], [BASE.length + 2, BASE.length + 1]);
await api.deleteFilterProfiles([RENAMED]);

/* ------------------------------------------------------------------- 收尾 */
const cleaned = await api.deleteFilterProfiles(['测试']);
eq('删掉「测试」之后回到开局那张表', cleaned.profiles.map((p) => p.name), BASE);

/* ------------------------------------------------- 批量删（列表多选走的那一个出口） */
// 单删是"列表里只勾了一行"的特例，不是另一条路：勾五下删五次会在列表上闪五轮，而用户
// 要的是"这五个都没了"。所以两边都问同一个出口，这里量它一次删一串。
await api.saveFilterProfile('', { name: '甲', dirs: dirs(contains('a')), files: NONE });
await api.saveFilterProfile('', { name: '乙', dirs: dirs(contains('b')), files: NONE });
await api.setActiveFilter('甲', false);
const goneMany = await api.deleteFilterProfiles(['甲', '乙', '查无此套']);
eq('一次删掉两套（表里没有的那个不算）', goneMany.profiles.map((p) => p.name), BASE);
eq('删掉正在用的那套会落到还剩的那套', [goneMany.active, goneMany.profile.name], [BASE0, BASE0]);

// 删到一套都不剩时必须补一套出来，否则任务页那颗下拉是空的。补出来的那套叫「默认」
// （`store.DefaultFilterName`）且**没有条件** —— 它就是"不过滤"这件事的一个名字。
// 删的是**整张基线表**，不是当初那两套的名字：留着第三套的话删完还剩一套，这一整段
// 就测不到"补一套"那个出口了（2026-10-10 就是这么被漏掉的）。
const lastOne = await api.deleteFilterProfiles(BASE);
eq('一套都不剩时补一套出来', lastOne.profiles.length, 1);
eq('补出来的那套没有条件', [lastOne.profiles[0].name, lastOne.profiles[0].dirs.enabled, lastOne.profiles[0].files.enabled],
  ['默认', false, false]);
eq('补出来的空方案 = 不过滤', (await api.addFolderDialog(true)).added, 31);
// 恢复开局那整张表（逐套按原样写回，顺序也跟着回去），免得这一段之后还有别的段要拿它们当基准。
for (const p of BASE_PROFILES) {
  await api.saveFilterProfile('', { name: p.name, dirs: p.dirs, files: p.files });
}
await api.setActiveFilter(BASE_ACTIVE, false);

/* ------------------------------------------------------- 模板批量删（同一形状） */
const tpls0 = (await api.templates()).map((t) => t.id);
const dupe = await api.duplicateTemplate(tpls0[2]);
const tplsAfterDupe = (await api.templates()).map((t) => t.id);
eq('复制的那套追加在末尾', tplsAfterDupe[tplsAfterDupe.length - 1], dupe.id);
const nRemoved = await api.deleteTemplates([tpls0[2], dupe.id]);
eq('一次删掉两个模板', nRemoved, 2);
eq('剩下的还是原来那几套', (await api.templates()).map((t) => t.id), tplsAfterDupe.filter((id) => id !== tpls0[2] && id !== dupe.id));

// 「基本配置」是所有模板的默认值来源，删掉的话剩下的模板全都没有兜底值了。整批一起交上
// 来时它要**被跳过**，而不是让这一批全部失败 —— 用户勾的是另外三行。
const onlyGlobal = await api.deleteTemplates(['t-global', tplsAfterDupe[1]]);
eq('全局那一套带着一起交：只删掉另一个', onlyGlobal, 1);
eq('「基本配置」还在', (await api.templates())[0].id, 't-global');
let goneErr = '';
try { await api.deleteTemplates(['t-global']); } catch (e) { goneErr = e.message; }
eq('只交全局那一套 = 一个都没删掉', goneErr, '要删的模板已经不在了');

/**
 * 「清空」（`ClearQueue`）在 mock 里也照抄一遍 Go 的 `Runner.ClearAll`。
 *
 * 这一颗**只清列表，不动文件**，而且它和 `armed` 无关 —— 后端不判，判的是前端那颗按钮
 * 灰不灰（`queueIdle`，上面钉过了）。所以这里不去摆 `armed`，只量清空本身做了什么。
 *
 * 桩成 `() => 0` 的话，"清空"在浏览器预览里就是一颗按下去什么都不动的按钮。**行**是
 * 前端自己重拉 `Jobs()` 画的，所以桩着也看不出来；看得出来的是工具栏那几颗计数 chip
 * （`onStats()` 画的），它们只有广播才叫得动 —— 少了这一句，清完之后列表空了、进度还
 * 写着 9。所以这里数的是广播。
 */
const keepForClear = (await api.jobs()).slice();
const seenClear = [];
const offClear = onEvent('queue:state', (s) => seenClear.push(s));
const cleared = await api.clearQueue();
eq('清空：返回清掉的行数', cleared, keepForClear.length);
eq('清空：列表真的空了', (await api.jobs()).length, 0);
eq('清空：广播了新的 stats', seenClear.length, 1);
eq('清空：广播的那份 total 归零', seenClear[0] && seenClear[0].total, 0);
// 空列表上再清一次：返回 0，但**照样广播** —— 照 Go 的 `ClearAll` 那句无条件的
// `r.emitState()`（同一个文件里 `RemoveJobs` 是有条件的那种，两个不一样，别对齐错了）。
const beforeSecond = seenClear.length;
eq('清空：空列表返回 0', await api.clearQueue(), 0);
eq('清空：空列表也广播（照 Go 的无条件 emit）', seenClear.length, beforeSecond + 1);
offClear();
// 塞回去，别让这一段的最后一步改动落到"脚本跑完之后"的状态上（虽然是最后一段）。
(await api.jobs()).push(...keepForClear);
eq('清空：还原之后 total 回到原样', (await api.stats()).total, keepForClear.length);

/* ------------------------------------------------ 记录页的时间区间（精确到秒） */

/**
 * 记录页那两个框是 `datetime-local step="1"`，也就是**能填到秒**；规矩是一句话：
 * **填到哪一格，就包含那一整格。**
 *
 * 这条为什么要钉在 mock 上：真正做筛的是 Go 的 `parseBound`，mock 这边是它的**镜子**
 * （`api.js` 的 `bound`），而镜子错一个等号（`t > to` vs `t >= to`）或者少一档写法，
 * 表现是"预览里多含 / 少含整整一格"—— 和真机长得一模一样，肉眼永远看不出来。
 *
 * 判据挑的是**用户看得见的那件事**：把列表上「完成时间」那一列的字（`2026-05-02 15:04:33`）
 * 抄进筛选框，那条记录就必须在里面；而结束端往回挪一秒，它就必须出去。两句话合起来正好
 * 钉住"那一格是闭的、下一格是开的"。
 */
const pad2 = (n) => String(n).padStart(2, '0');
/** 把一条记录的 `endedAt`（ISO / UTC）转成框里那种**本地**写法，精确到秒。 */
const localStamp = (iso, back = 0) => {
  const d = new Date(Date.parse(iso) - back);
  return `${d.getFullYear()}-${pad2(d.getMonth() + 1)}-${pad2(d.getDate())}`
    + `T${pad2(d.getHours())}:${pad2(d.getMinutes())}:${pad2(d.getSeconds())}`;
};
const allRecords = (await api.history({ limit: 500 })).items;
const probe = allRecords[0];
const secs = localStamp(probe.endedAt);
const gotIds = async (q) => (await api.history({ ...q, limit: 500 })).items.map((r) => r.id);
const has = (ids) => ids.includes(probe.id);

eq('时间区间：抄那一列的字（到秒）能筛到这一条', has(await gotIds({ from: secs, to: secs })), true);
eq('时间区间：结束端往回挪一秒就筛不到（那一格是闭的）',
  has(await gotIds({ from: secs, to: localStamp(probe.endedAt, 1000) })), false);
eq('时间区间：只填到分钟 = 含那一整分钟（不是含那一刻）',
  has(await gotIds({ from: secs.slice(0, 16), to: secs.slice(0, 16) })), true);
eq('时间区间：只填日期 = 含那一整天（界面给不出这一档，手输 / 后端调用走这条）',
  has(await gotIds({ from: secs.slice(0, 10), to: secs.slice(0, 10) })), true);
// 精度由**写法**决定：补上 `:00` 是那一"秒"那一格，和只写到分钟的"那一整分钟"差一个结果。
// 界面走不到这一档（`datetime-local` 在秒为 0 时会把秒那一段省掉，实测过），但判据在这里，
// 镜子写错了就没人拦得住。
//
// 基准要挑一条**秒数非 0** 的记录：mock 的时间戳跟着当前时间走，撞上整分钟时"补 :00"
// 和"只到分钟"就是同一个区间，这条会假红 —— 60 次里撞一次，2026-10-14 撞过一次。
const tick = allRecords.find((r) => localStamp(r.endedAt).slice(17) !== '00') || probe;
const tickMin = localStamp(tick.endedAt).slice(0, 16);
eq('时间区间：补上 :00 是那一秒，不是那一分钟',
  (await gotIds({ from: `${tickMin}:00`, to: `${tickMin}:00` })).includes(tick.id), false);
// 手输一半的串（`2026-5-2` 那种）当作**这一端不设边界**：不清空整张表。
eq('时间区间：填不成时间就当没有这个边界',
  (await gotIds({ from: '2026-5-2', to: '不是日期' })).length,
  (await gotIds({})).length);
// 反证：上面那几条"能筛到"对一个"永远返回全部"的桩**也全绿**，所以两头各往外挪一格再量
// 一次 ——「从」这一端往后挪一秒，它就必须消失。
eq('时间区间：起始端往后挪一秒就筛不到', has(await gotIds({ from: localStamp(probe.endedAt, -1000) })), false);

log(bad ? `\n${bad} FAILED` : '\nall mock checks passed');
process.exit(bad ? 1 : 0);
