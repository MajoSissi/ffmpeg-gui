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
const { profileSummary, rulesActive } = await import(
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
// 关掉的那组也念条件：只写"已暂停"的话，用户记不住自己设过什么。
eqCall('摘要：关掉的那组也念', () => profileSummary({
  name: '暂停着的那套',
  dirs: { enabled: false, matchAll: false, exclude: false, topOnly: false,
    filters: [{ mode: 'contains', value: 'cache' }] },
  files: { enabled: false, matchAll: false, exclude: false, filters: [] },
}), '目录：已暂停 · 包含 cache');
eqCall('两组空数组不算启用', () => rulesActive({ enabled: true, filters: [] }), false);
eqCall('有条件且开着才算启用', () => rulesActive({ enabled: true, filters: [{ mode: 'contains', value: 'a' }] }), true);

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

// 「不使用过滤」：两组都不参与，但设置文件里记的还是选中的那套。
await api.setActiveFilter(COND.name, false);
const off = await api.setActiveFilter(COND.name, true);
eq('状态里报出禁用', off.off, true);
eq('禁用时交出去的规则不筛', [off.profile.dirs.enabled, off.profile.files.enabled], [false, false]);
eq('禁用 = 整个文件夹照收', (await api.addFolderDialog(true)).added, 31);
eq('禁用不落盘（设置文件里还是那套）', (await api.filterState()).active, COND.name);
const offBack = await api.setActiveFilter(COND.name, false);
eq('关掉禁用回到那套', [offBack.off, offBack.profile.name], [false, COND.name]);

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

log(bad ? `\n${bad} FAILED` : '\nall mock checks passed');
process.exit(bad ? 1 : 0);
