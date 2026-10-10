// Bridge to the Go backend. Falls back to an in-browser mock so the UI can be
// previewed (and visually verified) without the desktop shell.

const BOUND = globalThis.go?.main?.App;
const HAS_RUNTIME = typeof globalThis.runtime?.EventsOn === 'function';

export const isMock = !BOUND;

function call(name, ...args) {
  const fn = BOUND?.[name];
  if (typeof fn === 'function') {
    return fn(...args);
  }
  const m = mock[name];
  if (typeof m === 'function') {
    return Promise.resolve(m(...args));
  }
  return Promise.reject(new Error(`后端未实现: ${name}`));
}

/* ------------------------------------------------------------------ events */

const listeners = new Map();

export function on(event, cb) {
  if (HAS_RUNTIME) {
    return globalThis.runtime.EventsOn(event, cb);
  }
  if (!listeners.has(event)) listeners.set(event, new Set());
  listeners.get(event).add(cb);
  return () => listeners.get(event)?.delete(cb);
}

function emitMock(event, payload) {
  listeners.get(event)?.forEach((cb) => cb(payload));
}

/* -------------------------------------------------------------- rich mock */

/* Mirrors the shipped global template (store.DefaultGlobalTemplate): it is the
   defaults every other template inherits, and the mock keeps one so the editor's
   "follow the global template" states can be exercised without a backend. */
const MOCK_GLOBAL = {
  id: 't-global', name: '全局模板', global: true,
  description: '所有模板的默认值。新建模板会以它为起点；模板里留空的项也跟随它。',
  outDirSpec: { mode: 'sibling', dir: '', prefix: '', suffix: '_out', keepTree: true }, outPattern: '{name}',
  outputOverride: true,
  perf: { concurrency: 2, logLevel: 'warning', retryCount: 0, threads: 0, idlePriority: false, deleteOnFail: true },
  // The global ships WITHOUT this section (store.DefaultGlobalTemplate leaves it
  // nil, and a non-nil section always skips an already-processed file). The mock
  // keeps one so the editor's follow-state can be exercised; values match what
  // seedSection produces when a user opens the section.
  existing: {
    action: 'keep',
    dir: { mode: 'sibling', dir: '', prefix: '', suffix: '_done', keepTree: true },
    pattern: '{name}', overwrite: false,
  },
  filter: {
    minSizeMB: 300, maxSizeMB: 0, minLongEdge: 0, maxLongEdge: 0, minDuration: 0, maxDuration: 0,
    includeExts: [], excludeExts: [], action: 'move',
    dir: { mode: 'custom', dir: 'D:/Media/small', prefix: '', suffix: '', keepTree: true },
    renamePattern: '{name}', overwrite: false,
  },
  problems: {
    errorAction: 'move', errorDir: { mode: 'custom', dir: 'D:/Media/error', prefix: '', suffix: '', keepTree: true },
    warningAction: 'mark', warningDir: { mode: 'sibling', dir: '', prefix: '', suffix: '_warning', keepTree: true },
  },
};

const MOCK_TEMPLATES = [
  MOCK_GLOBAL,
  // perf / existing / filter / problems are absent on purpose: nil is the "follow"
// state, and so is a missing outputOverride.
  { id: 't-remux', name: '无损转封装', description: '仅重封装，不重编码。秒级完成，画质完全无损。', builtin: true, container: '', videoMode: 'copy', audioMode: 'copy', mapAll: true, rateControl: 'crf', crf: 23, resize: { mode: 'keep', multipleOf: 2, algorithm: 'lanczos' }, maxMuxQueue: 0 },
  { id: 't-h264', name: 'H.264 通用 1080p', description: '长边压到 1080p，CRF 23 视觉无损。', builtin: true, container: 'mp4', videoMode: 'encode', videoCodec: 'libx264', rateControl: 'crf', crf: 23, preset: 'medium', pixFmt: 'yuv420p', resize: { mode: 'longedge', longEdge: 1920, onlyLarger: true, multipleOf: 2, algorithm: 'lanczos' }, audioMode: 'encode', audioCodec: 'aac', audioBitrate: '192k', audioChannels: 2, fastStart: true, maxMuxQueue: 0 },
  { id: 't-h265', name: 'H.265 高压缩', description: '同画质体积更小。', builtin: true, container: 'mp4', videoMode: 'encode', videoCodec: 'libx265', rateControl: 'crf', crf: 26, preset: 'medium', pixFmt: 'yuv420p', resize: { mode: 'longedge', longEdge: 1920, onlyLarger: true, multipleOf: 2, algorithm: 'lanczos' }, audioMode: 'encode', audioCodec: 'aac', audioBitrate: '128k', fastStart: true, maxMuxQueue: 0 },
  { id: 't-4k2k', name: '4K 长边转 2K', description: '自动识别横竖屏：长边统一到 2560，短边按比例。', builtin: true, container: 'mp4', videoMode: 'encode', videoCodec: 'libx265', rateControl: 'crf', crf: 24, preset: 'medium', pixFmt: 'yuv420p', resize: { mode: 'longedge', longEdge: 2560, onlyLarger: true, multipleOf: 2, algorithm: 'lanczos' }, audioMode: 'encode', audioCodec: 'aac', audioBitrate: '192k', fastStart: true, maxMuxQueue: 0,
    // A template that overrides one section and follows the other three.
    perf: { concurrency: 1, logLevel: 'warning', retryCount: 1, idlePriority: false, deleteOnFail: true } },
];

/* Mirrors engine.BuildPlan's argument order so the mock previews match what the
   real planner produces. Note there is no -max_muxing_queue_size: it became an
   opt-in per template, so a template that does not ask for it gets no flag. */
const FFMPEG_BIN = 'C://Users//Majo//AppData//Local//Microsoft//WinGet//Links//ffmpeg.exe';
function FFMPEG_ARGS(input, output, w, h) {
  // Only the pinned axis is written out; the other is -2 so ffmpeg keeps the
  // aspect ratio and lands on an even number, matching engine.computeScale.
  return [
    '-hide_banner', '-nostdin', '-y', '-progress', 'pipe:1',
    '-i', input,
    '-map', '0',
    '-c:v', 'libx265', '-crf', '24', '-preset', 'medium', '-pix_fmt', 'yuv420p',
    '-vf', `scale=${w}:-2:flags=lanczos`,
    '-c:a', 'copy',
    '-movflags', '+faststart',
    output,
  ];
}

function mkInfo(name, w, h, extra = {}) {
  const ext = name.split('.').pop();
  return {
    path: `D:/Media/2024/${name}`, fileName: name, ext,
    container: 'mov,mp4,m4a,3gp,3g2,mj2', containerLong: 'QuickTime / MOV',
    size: extra.size ?? 1_820_000_000, duration: extra.duration ?? 754,
    bitRate: extra.bitRate ?? 19_400_000, rotation: 0,
    width: w, height: h, displayWidth: w, displayHeight: h,
    fps: extra.fps ?? 29.97, pixFmt: 'yuv420p',
    videoCodec: extra.vc ?? 'hevc', audioCodec: 'aac',
    videoN: 1, audioN: 1, subtitleN: 0, chapters: 0,
    video: { index: 0, type: 'video', codec: extra.vc ?? 'hevc', width: w, height: h, fps: 29.97, pixFmt: 'yuv420p' },
    audio: { index: 1, type: 'audio', codec: 'aac', sampleRate: 48000, channels: 2 },
  };
}

function mkJob(id, name, w, h, status, progress, extra = {}) {
  return {
    id, input: `D:/Media/2024/${name}`, inputName: name,
    // Mirrors the default "sibling" output mode: only the top added folder gets the
    // suffix, the sub-tree is copied across and the file name is untouched.
    output: `D:/Media/2024_out/${name}`,
    outputName: name,
    sourceRoot: 'D:/Media/2024', templateId: 't-4k2k', templateName: '4K 长边转 2K',
    status, message: extra.message ?? '', error: extra.error ?? '', warnings: extra.warnings ?? [],
    // A suspended job keeps status `running` and only adds this flag -- the queue is
    // not doing anything different for it, so the badge is a separate input to
    // statusChip rather than a status of its own.
    frozen: !!extra.frozen,
    command: '', progress, speed: extra.speed ?? 0, bitrate: extra.bitrate ?? '',
    frame: 0, fps: 0, outTimeMs: 0, outBytes: 0,
    targetWidth: extra.tw ?? 2560, targetHeight: extra.th ?? 1440, resized: !!extra.tw,
    duration: extra.duration ?? 754, size: extra.size ?? 1_820_000_000,
    infoBefore: mkInfo(name, w, h, extra), infoAfter: extra.after ?? null,
    queuedAt: new Date().toISOString(), startedAt: extra.startedAt ?? '', endedAt: '',
    elapsedMs: extra.elapsedMs ?? 0, logTail: [], logLineCount: 0, outputDeleted: false, recordId: '',
    // Empty unless the 「已处理过的文件」 rule moved the source away, in which case
    // Input on its own no longer points at anything. See engine.Job.SourceMovedTo.
    sourceMovedTo: extra.sourceMovedTo ?? '',
  };
}

const MOCK_STATE = {
  jobs: [
    mkJob('j1', 'DJI_0042.MP4', 3840, 2160, 'running', 0.62, { speed: 3.4, bitrate: '4128.5', message: '62.0% · 7:48 · 3.4x' }),
    mkJob('j2', 'DJI_0043.MP4', 3840, 2160, 'pending', 0),
    mkJob('j3', '旅行vlog-竖屏.mp4', 2160, 3840, 'pending', 0, { tw: 1440, th: 2560 }),
    mkJob('j4', '会议录屏_2024.mkv', 1920, 1080, 'pending', 0),
    mkJob('j5', '宣传片_最终版.mov', 4096, 2304, 'done', 1, { elapsedMs: 412_000, speed: 3.1, tw: 2560, th: 1440, size: 2_940_000_000, after: mkInfo('宣传片_最终版.mp4', 2560, 1440, { size: 462_000_000, duration: 212, bitRate: 17_400_000, vc: 'hevc' }) }),
    mkJob('j6', '手机拍摄-慢动作.mp4', 3840, 2160, 'warning', 1, { elapsedMs: 96_400, message: '已完成（2 条警告）', warnings: ['输出文件复核失败: Invalid data found when processing input', '源文件已移动到 D:/Media/error'], tw: 2560, th: 1440, size: 1_240_000_000, sourceMovedTo: 'D:/Media/error/手机拍摄-慢动作.mp4', after: mkInfo('手机拍摄-慢动作.mp4', 2560, 1440, { size: 188_000_000, duration: 96, bitRate: 15_600_000, vc: 'hevc' }) }),
    mkJob('j7', '古老素材.avi', 720, 576, 'failed', 0.14, { elapsedMs: 31_000, error: 'av_interleaved_write_frame(): Invalid argument' }),
    mkJob('j8', '超短视频.mp4', 1280, 720, 'filtered', 0, { message: '体积 18.4 MB 小于下限 300 MB；已移动到 D:/Media/small', sourceMovedTo: 'D:/Media/small/超短视频.mp4' }),
    mkJob('j9', '损坏文件.mp4', 0, 0, 'skipped', 0, { message: '输出文件已存在，已跳过' }),
  ],
  stats: { total: 9, pending: 3, running: 1, done: 1, warning: 1, failed: 1, canceled: 0, skipped: 1, filtered: 1, paused: false, started: true, workers: 2, progress: 0.44 },
  log: [
    ['j1', '$ "C:\\Users\\Majo\\...\\ffmpeg.exe" -hide_banner -nostdin -y -progress pipe:1 -i "D:\\Media\\2024\\DJI_0042.MP4" -map 0 -c:v libx265 -crf 24 -preset medium -pix_fmt yuv420p -vf scale=2560:-2:flags=lanczos -c:a copy -movflags +faststart "D:\\Media\\2024_out\\DJI_0042.MP4"'],
    ['j1', 'Input #0, mov,mp4,m4a,3gp,3g2,mj2, from \'D:\\Media\\2024\\DJI_0042.MP4\':'],
    ['j1', '[warn] deprecated pixel format used, make sure you did set range correctly'],
    ['j1', 'Stream #0:0: Video: hevc (Main), yuv420p(tv), 3840x2160, 29.97 fps'],
    ['j1', '[hevc @ 000001] Using auto wpp threads'],
    ['j1', 'frame= 1420 fps= 101 q=28.0 size= 512000kB time=00:00:47.35 bitrate=88560.1kbits/s speed=3.37x'],
  ],
};

const MOCK_HISTORY_BASE = [
  {
    id: 'r1', input: 'D:/Media/2024/宣传片_最终版.mov', output: 'D:/Media/2024_out/宣传片_最终版.mp4',
    templateId: 't-4k2k', templateName: '4K 长边转 2K', status: '已完成', note: '', error: '', warnings: [],
    startedAt: new Date(Date.now() - 3600e3).toISOString(), endedAt: new Date(Date.now() - 3200e3).toISOString(),
    elapsedMs: 412_000, speed: 3.1,
    before: { exists: true, path: '', size: 2_940_000_000, container: 'mov', duration: 212, videoCodec: 'prores', width: 4096, height: 2304, fps: 25, pixFmt: 'yuv422p10le', audioCodec: 'pcm_s16le', sampleRate: 48000, channels: 2, bitRate: 110_900_000 },
    after: { exists: true, path: '', size: 462_000_000, container: 'mp4', duration: 212, videoCodec: 'hevc', width: 2560, height: 1440, fps: 25, pixFmt: 'yuv420p', audioCodec: 'aac', sampleRate: 48000, channels: 2, bitRate: 17_400_000 },
  },
  {
    id: 'r2', input: 'D:/Media/2024/手机拍摄-慢动作.mp4', output: 'D:/Media/2024_out/手机拍摄-慢动作.mp4',
    templateId: 't-h265', templateName: 'H.265 高压缩', status: '完成(警告)', note: '已完成（2 条警告）',
    error: '', warnings: ['输出文件复核失败: Invalid data found'], startedAt: new Date(Date.now() - 7200e3).toISOString(),
    endedAt: new Date(Date.now() - 7100e3).toISOString(), elapsedMs: 96_400, speed: 12.4,
    before: { exists: true, path: '', size: 1_240_000_000, container: 'mov', duration: 96, videoCodec: 'h264', width: 3840, height: 2160, fps: 120, pixFmt: 'yuv420p', audioCodec: 'aac', sampleRate: 48000, channels: 2, bitRate: 103_000_000 },
    after: { exists: true, path: '', size: 188_000_000, container: 'mp4', duration: 96, videoCodec: 'hevc', width: 2560, height: 1440, fps: 120, pixFmt: 'yuv420p', audioCodec: 'aac', sampleRate: 48000, channels: 2, bitRate: 15_600_000 },
  },
  {
    id: 'r3', input: 'D:/Media/2024/古老素材.avi', output: 'D:/Media/2024_out/古老素材.mp4',
    templateId: 't-h264', templateName: 'H.264 通用 1080p', status: '失败', note: '', error: 'av_interleaved_write_frame(): Invalid argument',
    warnings: [], startedAt: new Date(Date.now() - 9000e3).toISOString(), endedAt: new Date(Date.now() - 8960e3).toISOString(),
    elapsedMs: 31_000, speed: 0,
    before: { exists: true, path: '', size: 684_000_000, container: 'avi', duration: 1840, videoCodec: 'mpeg4', width: 720, height: 576, fps: 25, pixFmt: 'yuv420p', audioCodec: 'mp3', sampleRate: 44100, channels: 2, bitRate: 2_970_000 },
    after: { exists: false, path: '', size: 0, container: '', duration: 0, videoCodec: '', width: 0, height: 0, fps: 0, pixFmt: '', audioCodec: '', sampleRate: 0, channels: 0, bitRate: 0 },
  },
  {
    id: 'r4', input: 'D:/Media/2024/超短视频.mp4', output: 'D:/Media/small/超短视频.mp4',
    templateId: 't-h264', templateName: 'H.264 通用 1080p', status: '已排除', note: '体积 18.4 MB 小于下限 300 MB；已移动到 D:/Media/small',
    sourceMovedTo: 'D:/Media/small/超短视频.mp4',
    error: '', warnings: [], startedAt: new Date(Date.now() - 12000e3).toISOString(), endedAt: new Date(Date.now() - 11990e3).toISOString(),
    elapsedMs: 420, speed: 0,
    before: { exists: true, path: '', size: 19_300_000, container: 'mp4', duration: 12, videoCodec: 'h264', width: 1280, height: 720, fps: 30, pixFmt: 'yuv420p', audioCodec: 'aac', sampleRate: 48000, channels: 2, bitRate: 12_800_000 },
    after: { exists: false, path: '', size: 0, container: '', duration: 0, videoCodec: '', width: 0, height: 0, fps: 0, pixFmt: '', audioCodec: '', sampleRate: 0, channels: 0, bitRate: 0 },
  },
];

/* 四条手写的记录，再照它们的形状铺成一叠。
   记录页默认每页 50 条，四条数据永远翻不出第二页 —— 分页、日期区间、翻页之后
   的勾选都因此没法在预览里看。生成的那些只改文件名和时间，其余字段照抄。 */
const MOCK_HISTORY = (() => {
  const out = [];
  for (let i = 0; i < 137; i++) {
    const b = MOCK_HISTORY_BASE[i % MOCK_HISTORY_BASE.length];
    const serial = String(i + 1).padStart(3, '0');
    const stamped = (p) => {
      const dir = p.slice(0, p.lastIndexOf('/'));
      const file = p.slice(p.lastIndexOf('/') + 1);
      return `${dir}/${file.replace(/\.[^.]+$/, '')}_${serial}${file.slice(file.lastIndexOf('.'))}`;
    };
    // 从 1.5 小时前起，每条往前推 1.5 小时：最新的那条也留在过去。
    const started = Date.now() - (i + 1) * 5400e3;
    const input = stamped(b.input);
    const output = stamped(b.output);
    out.push({
      ...b,
      id: `r${i + 1}`,
      input,
      output,
      // 搬走源文件的那条规则留下的落点也一起改名，否则预览里源文件和它的落点
      // 会是两个对不上的名字。
      sourceMovedTo: b.sourceMovedTo ? stamped(b.sourceMovedTo) : '',
      before: { ...b.before, path: input },
      after: { ...b.after, path: output },
      startedAt: new Date(started).toISOString(),
      endedAt: new Date(started + Math.max(1000, b.elapsedMs)).toISOString(),
    });
  }
  return out;
})();

const MOCK_SETTINGS = {
  ffmpegPath: 'C:/Users/Majo/AppData/Local/Microsoft/WinGet/Links/ffmpeg.exe',
  ffprobePath: 'C:/Users/Majo/AppData/Local/Microsoft/WinGet/Links/ffprobe.exe',
  globalInArgs: '', globalOutArgs: '', hardwareDecode: false,
  preventSleep: true, enableTray: true, closeToTray: true, startMinimized: false, confirmExit: true,
  keepLogLines: 2000, saveRunLog: true, logMaxSizeMB: 50, logKeepDays: 7,
  lastTemplateId: 't-4k2k', showLogPanel: true, logPanelHeight: 0, logPanelSized: false, logDir: '',
  // 过滤方案：一套套命名的规则，每套里目录规则和文件规则各一组，两组互不相干（形状
  // 同 `store.FilterProfile`）。第一套是空的那套 —— 和 Go 侧 `Normalize` 补上的一样，
  // 任务页那个下拉总要有东西可选；第二套能看出效果，下拉里也才像"多个选项"。
  //
  // **第三套是「有条件、没说明」的，而且它是默认生效的那套。** 这不是凑数：
  //
  //   - 「说明」是第 47 批才加的字段，**所有老设置文件里的方案都没有它** —— 而真实用户
  //     打开程序看到的正是这些方案，不是这里的示例数据。
  //   - `profileSummary()` 有说明就直接返回说明，**没有说明才念条件**（`groupText`）。
  //     所以"每套都有说明"的示例数据让 `groupText` 一行都不执行。
  //   - `groupText` 里正好有个 `MODE_LABEL` 当时还没定义（2026-10-10 出包即崩：
  //     `ReferenceError: MODE_LABEL is not defined`）。它写在 `.map` 的回调里，而空数组
  //     压根不调用回调 —— 于是 `node --check`、`check_frontend.py`、探针、`_mock_check`
  //     全绿，只有真实用户的设置里有条件时才炸。
  //
  // mock 的一条铁律：**照的是规则，不是好看的程度**；这里更具体 —— 照的是用户真会有的
  // 数据形状。一份"每套都有说明"的示例数据比没有数据更危险，它让那条唯一会崩的分支
  // 永远走不到。
  filterProfiles: [
    {
      name: '默认',
      dirs: { enabled: false, filters: [], matchAll: false, exclude: false, topOnly: false },
      files: { enabled: false, filters: [], matchAll: false, exclude: false },
    },
    {
      name: '只收 h265',
      // 说明是列表里那一行字，也是右侧「基本信息」里的第二个框 —— 不参与任何判断。
      description: '转码中间产物，留着就行',
      dirs: {
        enabled: true, matchAll: false, exclude: false, topOnly: false,
        filters: [{ mode: 'contains', value: 'h265' }],
      },
      files: {
        enabled: true, matchAll: false, exclude: true,
        filters: [{ mode: 'suffix', value: '.tmp' }],
      },
    },
    {
      name: '排掉中间目录',
      dirs: {
        enabled: true, matchAll: false, exclude: true, topOnly: true,
        filters: [
          { mode: 'prefix', value: 'ffmpeg__' },
          { mode: 'glob', value: '*-tmp' },
        ],
      },
      files: {
        enabled: true, matchAll: false, exclude: false,
        filters: [{ mode: 'suffix', value: '.tmp' }],
      },
    },
  ],
  // 生效的那套是**没说明**的：任务页每次 `syncProfileOptions` 都要念一次摘要，而这是唯一
  // 一条会走到 `groupText` 的路。
  activeFilter: '排掉中间目录',
};

// Shared by the lists below so one wording fix lands everywhere at once. They
// mirror app.go's relocateActions verbatim -- these used to be near-copies whose
// wording had already drifted from the real thing. Where a file goes is a
// directory expression rather than a pick from a list, so there is no matching
// list for that.
const MOCK_RELOCATE = [
  { value: 'keep', label: '不处理，留在原处' },
  { value: 'move', label: '移动到目标目录' },
  { value: 'copy', label: '复制到目标目录' },
];

const OPTIONS = {
  videoCodecs: [
    { value: 'copy', label: '复制原编码' },
    { value: 'libx264', label: 'H.264 · libx264（推荐）' },
    { value: 'libx265', label: 'H.265 · libx265（更小）' },
    { value: 'libsvtav1', label: 'AV1 · SVT-AV1（快）' },
    { value: 'libvpx-vp9', label: 'VP9 · libvpx（WebM）' },
    { value: 'h264_nvenc', label: 'H.264 · NVENC' },
    { value: 'hevc_nvenc', label: 'H.265 · NVENC' },
    { value: 'h264_qsv', label: 'H.264 · QuickSync' },
    { value: 'h264_amf', label: 'H.264 · AMF' },
    { value: 'h264_videotoolbox', label: 'H.264 · VideoToolbox' },
    { value: 'gif', label: 'GIF 动图' },
  ],
  audioCodecs: [
    { value: 'copy', label: '复制原编码' }, { value: 'aac', label: 'AAC（通用）' },
    { value: 'libmp3lame', label: 'MP3' }, { value: 'libopus', label: 'Opus' },
    { value: 'flac', label: 'FLAC 无损' },
  ],
  containers: [
    { value: '', label: '沿用源文件的格式' }, { value: 'mp4', label: 'MP4' }, { value: 'mkv', label: 'MKV（Matroska）' },
    { value: 'mov', label: 'MOV' }, { value: 'webm', label: 'WebM' }, { value: 'm4a', label: 'M4A（仅音频）' },
    { value: 'mp3', label: 'MP3（仅音频）' },
  ],
  presets: [{ value: '', label: '默认（由 ffmpeg 决定）' }]
    .concat(['ultrafast', 'superfast', 'veryfast', 'faster', 'fast', 'medium', 'slow', 'slower', 'veryslow']
      .map((v) => ({ value: v, label: v }))),
  resizeModes: [
    { value: 'keep', label: '保持原分辨率' },
    { value: 'longedge', label: '锁定长边（自动识别横竖屏）' },
    { value: 'shortedge', label: '锁定短边（自动识别横竖屏）' },
    { value: 'exact', label: '指定宽 × 高' },
    { value: 'fit', label: '限制在矩形范围内（只缩不放）' },
    { value: 'percent', label: '按百分比缩放' },
  ],
  scaleAlgorithms: [
    { value: '', label: '默认（由 ffmpeg 决定）' },
    { value: 'lanczos', label: 'lanczos — 画质最好（推荐）' },
    { value: 'bicubic', label: 'bicubic — 均衡' },
    { value: 'bilinear', label: 'bilinear — 更快' },
  ],
  logLevels: [
    { value: 'quiet', label: 'quiet — 不输出任何信息' }, { value: 'error', label: 'error — 仅错误' },
    { value: 'warning', label: 'warning — 错误与警告（推荐）' }, { value: 'info', label: 'info — 常规信息' },
    { value: 'debug', label: 'debug — 调试信息' },
  ],
  rateControls: [
    { value: 'crf', label: 'CRF / 恒定质量（推荐）' }, { value: 'bitrate', label: '目标码率' }, { value: 'qp', label: 'QP / 固定量化' },
  ],
  padColors: [{ value: 'black', label: '黑色' }, { value: 'white', label: '白色' }, { value: '#101014', label: '深灰' }],
  // These lists mirror app.go's buildOptions() verbatim. They used to drift
  // (「只标记，不动文件」 vs 「不处理，留在原处」), which made a mock-driven look
  // differ from the real thing -- so the wording has to match, not just the values.
  destModes: [
    { value: 'same', label: '原目录' }, { value: 'custom', label: '自定义目录' },
    { value: 'sibling', label: '同级目录' },
    { value: 'siblingTop', label: '同级顶层目录' },
  ],
  defaultOutputSuffix: '_out',
  existingActions: MOCK_RELOCATE,
  filterActions: MOCK_RELOCATE,
  problemActions: [...MOCK_RELOCATE, { value: 'mark', label: '仅在结果中标记' }],
};

/* --------------------------------------------------- 按目录名筛选添加文件夹 */

/* 浏览器里没有文件系统，所以这里拿一棵固定的树当素材。规则本身和 Go 侧
   internal/engine/folder_scan.go 一套：比的是**名字**（目录名、文件名）、大小写不敏感、
   前缀 / 后缀 / 包含里出现 * 或 ? 时自动按通配符比。规则分家过一次（`{ext}` 那次），
   症状是界面上成了唯一看不出问题的地方，所以这里照着实现而不是"大概像"。

   每个目录列的是**文件名**而不是个数：文件规则按文件名筛，只存一个数字的话，"文件
   那一半"在浏览器里就永远没法验证 —— mock 存在的意义正是让界面上看得见的数和真跑
   的结果对得上。 */
const MOCK_SCAN_DIRS = [
  {
    rel: 'ffmpeg__batch01',
    files: ['b01-a.mp4', 'b01-b.mp4', 'b01-c.mp4', 'b01-d.mp4', 'b01-e.mkv', 'b01-f.mkv', 'b01-g.mov', 'b01-h.mp4'],
  },
  { rel: 'ffmpeg__batch01/h265', files: ['h265-1.mp4', 'h265-2.mp4', 'h265-3.mkv'] },
  { rel: 'ffmpeg__batch02', files: ['b02-a.mp4', 'b02-b.mp4', 'b02-c.mp4', 'b02-d.mp4', 'b02-e.mkv'] },
  { rel: 'h265-archive', files: ['arc-1.mp4', 'arc-2.mp4', 'arc-3.mp4', 'arc-4.mkv', 'arc-5.mkv', 'arc-6.mp4'] },
  // 中间层也列出来（0 个文件）：真跑的遍历会走到它们，靠名字判断的过滤条件也在它们
  // 身上过一遍。"只列叶子"的树看着更短，但 `scanned` 会凭空少几个，而且 `other` 这
  // 一层本来就是用户会想排除的那一层。
  { rel: 'other', files: [] },
  { rel: 'other/deep', files: [] },
  { rel: 'other/deep/h265', files: ['deep-1.mp4', 'deep-2.mkv'] },
  { rel: 'other/misc', files: ['misc-1.mp4', 'misc-2.mp4', 'misc-3.mp4', 'misc-4.avi'] },
];

/** `engine.maxPreviewDirs` 的等价物：页面返回的目录列表是有上限的。 */
const MAX_PREVIEW_DIRS = 200;

/** 直接放在所选目录里的文件。只看一层时收的就是它们（`mediaUnder` 的不递归分支）。 */
const MOCK_SCAN_ROOT_FILES = ['root-1.mp4', 'root-2.mp4', 'root-3.mkv'];

/** 「添加文件」在 mock 里挑到的那些。 */
const MOCK_PICKED_FILES = ['D:/Media/small/root-1.mp4', 'D:/Media/small/root-2.mp4', 'D:/Media/small/root-3.mkv'];

/** 拖进窗口的那几个。 */
const MOCK_DROPPED_FILES = ['D:/Media/drop/a.mp4', 'D:/Media/drop/b.mkv'];

const hasWildcard = (s) => s.includes('*') || s.includes('?');

/** filepath.Match 的等价物：* 和 ? 通配，半截模式（"["）当不匹配而不是报错。 */
function globMatch(pattern, name) {
  const re = pattern.replace(/[.+^${}()|[\]\\]/g, '\\$&')
    .replace(/\*/g, '[^]*').replace(/\?/g, '[^]');
  try {
    return new RegExp(`^${re}$`).test(name);
  } catch {
    return false;
  }
}

function matchName(name, filter) {
  const raw = String(filter?.value ?? '').trim();
  if (!raw) return false;
  const value = raw.toLowerCase();
  name = name.toLowerCase();
  switch (filter.mode) {
    case 'prefix':
      return hasWildcard(value) ? globMatch(value + '*', name) : name.startsWith(value);
    case 'suffix':
      return hasWildcard(value) ? globMatch('*' + value, name) : name.endsWith(value);
    case 'glob':
      return globMatch(value, name);
    default: // contains
      return hasWildcard(value) ? globMatch('*' + value + '*', name) : name.includes(value);
  }
}

function activeFilters(filters) {
  return (filters || []).filter((f) => String(f?.value ?? '').trim() !== '');
}

function matchNameAll(name, filters, matchAll) {
  const active = activeFilters(filters);
  if (active.length === 0) return true;
  return matchAll
    ? active.every((f) => matchName(name, f))
    : active.some((f) => matchName(name, f));
}

/**
 * 把一棵固定的树按**两组规则**筛一遍，形状和 `engine.ScanFolder` 的返回值一一对应。
 *
 * 目录规则决定挑哪几个目录，文件规则决定这些目录里再收哪些文件 —— 和 Go 侧
 * `ExpandFolder` + `mediaUnder` 同一条路。方向、开关、`scanned` 的口径全都要跟着，
 * 否则界面上写的数和真跑出来的数就会分家，而那正是这套东西最不该出的错。
 */
function mockScanFolder(scan) {
  const root = String(scan?.dir || '').replace(/[\\/]+$/, '');
  const win = (p) => p.replace(/[\\/]+/g, '\\');
  const base = (p) => p.split(/[\\/]/).filter(Boolean).pop() || p;

  const dirRules = normalizeDirRules(scan?.dirs);
  const fileRules = normalizeNameRules(scan?.files);
  // 方向只在有条件时才有意义：开关关着或一条都没填，两组都是"不过滤"。
  const dirFiltering = rulesActive(dirRules);
  const exclude = dirFiltering && dirRules.exclude;
  const fileFiltering = rulesActive(fileRules);
  const recursive = scan?.recursive !== false;

  /** 这些名字里留下来几个（`mediaUnder` 里的 keepFile）。 */
  const kept = (names) => (fileFiltering ? names.filter((n) => rulesTake(n, fileRules)) : names).length;
  /** 被文件规则丢掉几个。 */
  const drop = (names) => (fileFiltering ? names.filter((n) => !rulesTake(n, fileRules)).length : 0);

  /** 一棵子树的文件（含它自己那一层）。 */
  const treeFiles = (rel) => MOCK_SCAN_DIRS
    .filter((d) => d.rel === rel || d.rel.startsWith(`${rel}/`))
    .flatMap((d) => d.files);
  const allFiles = MOCK_SCAN_ROOT_FILES.concat(MOCK_SCAN_DIRS.flatMap((d) => d.files));

  const candidates = recursive
    ? MOCK_SCAN_DIRS
    : MOCK_SCAN_DIRS.filter((d) => !d.rel.includes('/'));

  if (!dirFiltering) {
    // 没填条件 = 整个目录。含子目录时根自己就是落点、取整棵树；只看一层时它收的
    // 就只是直接放在里面的那些文件（`engine.mediaUnder` 的不递归分支）。
    const names = recursive ? allFiles : MOCK_SCAN_ROOT_FILES.slice();
    return {
      root: win(root), filtering: false, exclude: false, scanned: 0, dirsTotal: 1,
      dirs: [{ dir: win(root), name: base(root), rel: base(root), files: kept(names) }],
      totalFiles: kept(names),
      filteredFiles: drop(names),
    };
  }

  if (exclude) {
    // 排除方向：落点是用户添加的那个文件夹本身，条件是沿途跳掉的子树 —— 命中的目录
    // 连整棵子树一起不要，其余照收。和 `engine.mediaUnder` 里的 SkipDir 一样，命中的
    // 目录之下不再往里看：里面再有命中的也没有意义，它们已经一起被丢掉了。
    //
    // 所以这里列出来的是**被跳过的目录**，totalFiles 是真正会进队列的数量 —— 一个说
    // "少了什么"，一个说"还剩多少"，两个都要有才对得上"整个文件夹减去这些"。
    //
    // 只看一层时一个子目录都不会被跳过（根本没往里看），收的就是根那一层：和 Go 侧的
    // 不递归分支直接忽略条件是一样的。
    const pruned = [];
    let examined = 0;
    if (recursive) {
      for (const d of candidates) {
        if (pruned.some((p) => d.rel.startsWith(`${p}/`))) continue;
        examined++;
        if (!takeName(base(d.rel), dirRules.filters, dirRules.matchAll, true)) pruned.push(d.rel);
      }
    }
    const under = (rel) => pruned.some((p) => rel === p || rel.startsWith(`${p}/`));
    const remaining = recursive
      ? MOCK_SCAN_ROOT_FILES.concat(MOCK_SCAN_DIRS.filter((d) => !under(d.rel)).flatMap((d) => d.files))
      : MOCK_SCAN_ROOT_FILES.slice();
    const dirs = pruned.map((rel) => ({
      dir: win(`${root}/${rel}`),
      name: base(rel),
      rel: win(rel),
      // 被跳掉的子树报的是"这里有多少个文件会跟着没" —— 所以**不套**文件规则：那些
      // 文件是被目录规则带走的，拿文件规则再筛一遍会把"丢掉多少"说小。
      files: treeFiles(rel).length,
    }));
    return {
      root: win(root), filtering: true, exclude: true, scanned: examined,
      dirsTotal: dirs.length,
      dirs: dirs.slice(0, MAX_PREVIEW_DIRS),
      totalFiles: kept(remaining),
      filteredFiles: drop(remaining),
    };
  }

  // 根自己也是一个候选，而且排在第一个 —— 和 `engine.ExpandFolder` 一样。它是用户
  // 亲手挑的目录，名字命中了就是要它：含子目录时连它下面的内容一起，只看一层时只收
  // 它直接放着的那些。少了这一条，"条件 包含 1 + 挑 D:/123"会一个文件都收不到，而
  // mock 要是也只算子目录，这个错在浏览器里就永远看不见。
  const rootName = base(root);
  const rootMatched = matchNameAll(rootName, dirRules.filters, dirRules.matchAll);
  const matched = candidates.filter((d) => matchNameAll(base(d.rel), dirRules.filters, dirRules.matchAll));

  // 命中的目录是整体收进来的：嵌套命中的那些文件已经算在上一层头上了，所以它自己
  // 报 0 —— 和 Go 侧一样，每个文件只算一次，逐行相加等于总数。
  const owned = [];
  const rows = [];
  let filtered = 0;
  const addRow = (dir, name, rel, names) => {
    rows.push({ dir, name, rel, files: kept(names) });
    filtered += drop(names);
  };
  if (rootMatched) {
    const names = recursive ? allFiles : MOCK_SCAN_ROOT_FILES.slice();
    // 根自己那行的相对路径就是它的名字（`relativeTo(root, root)` 走 "." 那一档）。
    addRow(win(root), rootName, rootName, names);
    // 根整棵都收进来了，再让下面那些命中的目录各报一次就是重复计数。
    if (recursive) MOCK_SCAN_DIRS.forEach((o) => owned.push(o.rel));
  }
  for (const d of matched) {
    const mine = recursive
      ? MOCK_SCAN_DIRS.filter(
        (o) => (o.rel === d.rel || o.rel.startsWith(`${d.rel}/`)) && !owned.includes(o.rel))
      : [d];
    mine.forEach((o) => owned.push(o.rel));
    // 列表里显示的是相对所选目录的路径，不是目录名：一棵树里每个批次都有一个 h265
    // 子目录时，两行都写着 "h265" 是分不清要加哪一个的。
    addRow(win(`${root}/${d.rel}`), base(d.rel), win(d.rel), mine.flatMap((o) => o.files));
  }
  return {
    root: win(root), filtering: true, exclude: false,
    // 根也是"看过的候选"之一。
    scanned: candidates.length + 1,
    dirsTotal: rows.length,
    dirs: rows.slice(0, MAX_PREVIEW_DIRS),
    totalFiles: rows.reduce((n, r) => n + r.files, 0),
    filteredFiles: filtered,
  };
}

/** `store.TakeName` 的等价物：一个名字算不算数，包含方向。 */
function takeName(name, filters, matchAll, exclude) {
  if (activeFilters(filters).length === 0) return true;
  const matched = matchNameAll(name, filters, matchAll);
  return exclude ? !matched : matched;
}

/**
 * 一组规则要不要这个名字 —— `store.NameRules.Take` 的等价物。
 *
 * 开关和条件一起看：关着的那一组等于没在筛，哪怕条件填得满满的。分开判的写法在每个
 * 调用点各写一遍，写三遍就会有一遍忘了看开关，那时候界面上的开关是关的、文件却照旧
 * 被筛掉。
 */
function rulesTake(name, r) {
  if (!rulesActive(r)) return true;
  return takeName(name, r.filters, r.matchAll, r.exclude);
}

/** 一组规则是不是真的在筛（`store.NameRules.Active`）。 */
function rulesActive(r) {
  return !!r?.enabled && activeFilters(r?.filters).length > 0;
}

/** `store.DirFilter.Recursive` 的等价物：往下收多深，两个来源只能往更窄的方向收。 */
function recursiveDepth(f, caller = true) {
  return caller && !f?.topOnly;
}

const DEFAULT_FILTER_NAME = '默认';

/** `store.NameRules.Normalize` 的等价物：丢掉空行、认不出的模式退回包含。 */
function normalizeNameRules(r) {
  return {
    enabled: !!r?.enabled,
    filters: activeFilters(r?.filters).map((c) => ({
      mode: ['prefix', 'suffix', 'glob'].includes(c.mode) ? c.mode : 'contains',
      value: String(c.value).trim(),
    })),
    matchAll: !!r?.matchAll,
    exclude: !!r?.exclude,
  };
}

/** `store.DirFilter` 的等价物：一组规则 + 「含子目录」的反面。 */
function normalizeDirRules(r) {
  return { ...normalizeNameRules(r), topOnly: !!r?.topOnly };
}

/** `store.normalizeProfiles` 的等价物：名字去空白、空名字与重名丢掉、至少留一套。 */
function normalizeProfiles(list) {
  const out = [];
  const seen = new Set();
  for (const p of (list || [])) {
    const name = String(p?.name || '').trim();
    if (!name) continue;
    const key = name.toLowerCase();
    if (seen.has(key)) continue;
    seen.add(key);
    out.push({
      name,
      description: String(p?.description ?? '').trim(),
      dirs: normalizeDirRules(p?.dirs),
      files: normalizeNameRules(p?.files),
    });
  }
  if (!out.length) {
    out.push({ name: DEFAULT_FILTER_NAME, description: '', dirs: normalizeDirRules(), files: normalizeNameRules() });
  }
  return out;
}

/**
 * `store.PickProfile` 的等价物：按名字找，找不到给第一套。
 *
 * 找不到时给第一套而不是零值：名字指着一套不存在的方案时，静默退成"不过滤"会让用户
 * 以为刚才那套还在生效。
 */
function pickProfile(name, list) {
  const key = String(name ?? '').trim().toLowerCase();
  const all = list || [];
  return all.find((p) => String(p?.name || '').toLowerCase() === key)
    || all[0]
    || { name: DEFAULT_FILTER_NAME, dirs: normalizeDirRules(), files: normalizeNameRules() };
}

/**
 * 任务页上那个「不使用过滤」。**只活在内存里**（见 `App.SetActiveFilter`）。
 *
 * 它和 `MOCK_SETTINGS.activeFilter` 不是一回事：后者是"我在用哪套"，落盘；这个是
 * "这一次我不要过滤"，关掉程序就没了。
 */
let mockFilterOff = false;
/**
 * `app.putProfile` 的等价物：把一套方案放进方案表里，位置只有一个答案。
 *
 *   - 同名的那一套被换掉（大小写不敏感）；
 *   - 改名（`oldName` 非空）时**旧名字那一格**就是它的新家，位置不动；
 *   - 都没有就追加到末尾。
 *
 * 名字是方案唯一的标识，所以"改的是哪一套"和"新的叫什么"必须一起说 —— 前端拿到的整
 * 表跟真跑出来的一样，靠的就是这一段。
 */
function putProfile(list, oldName, p) {
  const from = String(oldName ?? '').trim().toLowerCase();
  const key = p.name.toLowerCase();
  const skip = from && from !== key ? new Set([key, from]) : new Set([key]);
  const out = [];
  let placed = false;
  for (const old of list) {
    if (skip.has(old.name.toLowerCase())) {
      if (!placed) { out.push(p); placed = true; }
      continue;
    }
    out.push(old);
  }
  if (!placed) out.push(p);
  return out;
}

/**
 * 此刻真正生效的那一套。
 *
 * 「不使用过滤」给**零值**方案（两组都没开 → `Take` 一律放行），和 Go 侧
 * `App.effectiveFilter` 同一句：判据只有"两组开没开"，禁用不另开一条代码路径。
 */
function activeProfile() {
  if (mockFilterOff) return { name: '', description: '', dirs: normalizeDirRules(), files: normalizeNameRules() };
  return pickProfile(MOCK_SETTINGS.activeFilter, normalizeProfiles(MOCK_SETTINGS.filterProfiles));
}

/** `App.FilterState` 的等价物。顺带把方案表归一化，等价于 Go 侧 Normalize 那一趟。 */
function mockFilterState() {
  const profiles = normalizeProfiles(MOCK_SETTINGS.filterProfiles);
  MOCK_SETTINGS.filterProfiles = profiles;
  if (!profiles.some((p) => p.name === MOCK_SETTINGS.activeFilter)) {
    MOCK_SETTINGS.activeFilter = profiles[0].name;
  }
  return {
    profiles,
    active: MOCK_SETTINGS.activeFilter,
    off: mockFilterOff,
    profile: mockFilterOff
      ? { name: '', description: '', dirs: normalizeDirRules(), files: normalizeNameRules() }
      : pickProfile(MOCK_SETTINGS.activeFilter, profiles),
  };
}

/** 把路径最后一段拿出来，只为拼一条说得清是哪个文件夹的出错信息。 */
const baseName = (p) => String(p).split(/[\\/]/).filter(Boolean).pop() || String(p);

/**
 * 一次「添加文件夹」在 mock 里长什么样。
 *
 * 和 Go 侧同一条路（`engine.ExpandFolder` + `mediaUnder`）：规则为空（或关着）就是整个
 * 文件夹，收的方向下按条件挑子目录、一个都没挑着就把话说出来，排除方向下整个文件夹
 * 减去被跳掉的子树 —— 静默地什么都不加是最难查的一种。所以这里不写 {}，而是照着
 * `mockScanFolder` 的数报出来。
 *
 * 读的是**当前生效的那套方案**（`App.enqueue` 读的是同一个东西），所以任务页换个方案，
 * 下一次添加就该按新的走。
 */
function mockEnqueueFolder(dir, recursive = true) {
  const p = activeProfile();
  const dirs = p.dirs;
  const pv = mockScanFolder({
    dir,
    dirs,
    files: p.files,
    // 和 `App.enqueue` 同一句：方案说只看这一层就只看这一层，和过滤开不开无关。
    recursive: recursiveDepth(dirs, recursive),
  });
  const errors = [];
  const name = baseName(dir);
  if (pv.dirsTotal === 0 && rulesActive(dirs) && !dirs.exclude) {
    errors.push(`${name}: 没有子目录符合过滤条件`);
  } else if (pv.totalFiles === 0 && rulesActive(dirs) && dirs.exclude) {
    errors.push(`${name}: 全部文件都被过滤条件排除了`);
  } else if (pv.totalFiles === 0 && pv.filteredFiles > 0) {
    errors.push(`${name}: ${pv.filteredFiles} 个文件被文件过滤条件排除`);
  }
  return { added: pv.totalFiles, errors };
}

/**
 * 点名的那些文件在 mock 里长什么样。
 *
 * 走的是 `AddFilesDialog` / `AddPaths` / `AddDroppedFiles` 三条路 —— Go 侧它们都汇到
 * `runner.AddInputs` 里那段"文件也要过文件规则"，所以这里也只有这一份实现：两条路各
 * 判一次的话，"拖进来一个文件"和"拖进来一个文件夹、里面正是同一个文件"会有两种结果。
 */
function mockEnqueueFiles(paths) {
  const files = normalizeNameRules(activeProfile().files);
  const errors = [];
  let added = 0;
  for (const path of paths) {
    if (rulesTake(baseName(path), files)) added++;
    else errors.push(`${baseName(path)}: 被文件过滤条件排除`);
  }
  return { added, errors };
}

const mock = {
  Bootstrap() {
    return {
      runtime: {
        os: 'windows', arch: 'amd64', cpus: 16, dataDir: 'D:/Code-Project/ffmpeg-gui/build/bin/data', version: '1.0.0',
        binaries: { ffmpeg: MOCK_SETTINGS.ffmpegPath, ffprobe: MOCK_SETTINGS.ffprobePath, ffmpegFrom: 'path', ffprobeFrom: 'path' },
        ffmpeg: { name: 'ffmpeg', path: MOCK_SETTINGS.ffmpegPath, source: 'path', version: 'ffmpeg version 7.1.1 Copyright (c) 2000-2025 the FFmpeg developers', ok: true },
        ffprobe: { name: 'ffprobe', path: MOCK_SETTINGS.ffprobePath, source: 'path', version: 'ffprobe version 7.1.1 Copyright (c) 2000-2025 the FFmpeg developers', ok: true },
      },
      settings: MOCK_SETTINGS, filter: mockFilterState(),
      templates: MOCK_TEMPLATES, jobs: MOCK_STATE.jobs,
      stats: MOCK_STATE.stats, history: MOCK_HISTORY.slice(0, 50), options: OPTIONS,
    };
  },
  Settings: () => MOCK_SETTINGS,
  SaveSettings: (s) => Object.assign(MOCK_SETTINGS, s),
  ResetSettings: () => MOCK_SETTINGS,
  Templates: () => MOCK_TEMPLATES,
  GlobalTemplate: () => MOCK_TEMPLATES.find((t) => t.global) || MOCK_GLOBAL,
  NewTemplate: () => ({
    id: 'tmp-' + Date.now(), name: '', description: '', container: '', videoMode: 'encode',
    // Audio starts on copy on the Go side too (store.NewFromGlobal): most jobs only
    // re-encode the video.
    audioMode: 'copy',
    // Every policy section starts switched off, i.e. following the global template.
    outputOverride: false,
    resize: { mode: 'keep', multipleOf: 2, algorithm: '' },
  }),
  SaveTemplate: (t) => {
    // The defaults template is never created or overwritten through this path.
    const i = MOCK_TEMPLATES.findIndex((x) => x.id === t.id);
    const next = { ...t, global: false };
    if (i >= 0) MOCK_TEMPLATES[i] = next;
    else MOCK_TEMPLATES.push({ ...next, id: 't-' + Date.now() });
    return next;
  },
  SaveGlobalTemplate: (t) => {
    const i = MOCK_TEMPLATES.findIndex((x) => x.global);
    const next = { ...t, id: 't-global', global: true };
    if (i >= 0) MOCK_TEMPLATES[i] = next;
    emitMock('templates:changed', MOCK_TEMPLATES);
    return next;
  },
  // 批量删走同一个出口（单删是 ids 只有一个时的样子）。全局模板整批跳过：多选里混进
  // 它只是这一项留着，其余照删 —— 和 Go 侧 App.DeleteTemplates 同一句。
  DeleteTemplates: (ids) => {
    const want = new Set((ids || []).filter((id) => id && id !== 't-global'));
    const before = MOCK_TEMPLATES.length;
    const kept = MOCK_TEMPLATES.filter((t) => !want.has(t.id));
    if (kept.length === before) throw new Error('要删的模板已经不在了');
    MOCK_TEMPLATES.length = 0;
    MOCK_TEMPLATES.push(...kept);
    emitMock('templates:changed', MOCK_TEMPLATES);
    return before - kept.length;
  },
  ReorderTemplates: (ids) => {
    // Mirrors the Go side: the global template leads, unknown ids are ignored, and
    // anything the caller left out keeps its old relative position at the end.
    const byId = new Map(MOCK_TEMPLATES.map((t) => [t.id, t]));
    const out = [];
    const seen = new Set();
    const g = byId.get('t-global');
    if (g) { out.push(g); seen.add(g.id); }
    for (const id of ids) {
      const t = byId.get(id);
      if (!t || seen.has(id)) continue;
      seen.add(id);
      out.push(t);
    }
    for (const t of MOCK_TEMPLATES) if (!seen.has(t.id)) out.push(t);
    MOCK_TEMPLATES.length = 0;
    MOCK_TEMPLATES.push(...out);
    return MOCK_TEMPLATES;
  },
  DuplicateTemplate: (id) => {
    const t = MOCK_TEMPLATES.find((x) => x.id === id);
    if (!t) throw new Error('模板不存在');
    if (t.global) throw new Error('「全局模板」是默认值来源，请直接新建模板');
    // Deep-copy the three sections: sharing the pointers would let an edit of the
    // copy reach back into the original.
    const c = { ...t, id: 't-' + Date.now(), name: t.name + ' 副本', builtin: false, global: false };
    if (t.perf) c.perf = { ...t.perf };
    if (t.filter) c.filter = { ...t.filter, dir: { ...t.filter.dir } };
    if (t.problems) c.problems = { ...t.problems, errorDir: { ...t.problems.errorDir }, warningDir: { ...t.problems.warningDir } };
    MOCK_TEMPLATES.push(c);
    emitMock('templates:changed', MOCK_TEMPLATES);
    return c;
  },
  ExportTemplates: () => 'D:/Code-Project/ffmpeg-gui/build/bin/data/ffmpeg-gui-templates.json',
  ImportTemplatesFromFile: () => 0,
  AutoDetectBinaries: () => ({ ffmpeg: MOCK_SETTINGS.ffmpegPath, ffprobe: MOCK_SETTINGS.ffprobePath, ffmpegFrom: 'path', ffprobeFrom: 'path' }),
  CheckBinary: (p) => ({ name: 'ffmpeg', path: p, source: 'settings', version: 'ffmpeg version 7.1.1', ok: true }),
  PickBinary: () => 'C:/ffmpeg/bin/ffmpeg.exe',
  PickDirectory: () => 'D:/Media/small',
  PickFile: () => 'D:/video/mmd/sub/b.mp4',
  ToolInfo: () => ([
    { name: 'ffmpeg', path: MOCK_SETTINGS.ffmpegPath, source: 'path', version: 'ffmpeg version 7.1.1 Copyright (c) 2000-2025 the FFmpeg developers', ok: true },
    { name: 'ffprobe', path: MOCK_SETTINGS.ffprobePath, source: 'path', version: 'ffprobe version 7.1.1 Copyright (c) 2000-2025 the FFmpeg developers', ok: true },
  ]),
  DetectEncoders: () => {
    const ok = new Set(['libx264', 'libx265', 'libsvtav1', 'libvpx-vp9', 'mpeg4', 'gif', 'webp', 'h264_nvenc', 'hevc_nvenc', 'av1_nvenc']);
    return OPTIONS.videoCodecs.filter((o) => o.value !== 'copy')
      .map((o) => ({ name: o.value, label: o.label, ok: ok.has(o.value) }));
  },
  AddFilesDialog: () => mockEnqueueFiles(MOCK_PICKED_FILES),
  PreviewFolderScan: (scan) => mockScanFolder(scan),
  // 「添加文件夹」按当前生效的那套方案收。
  AddFolderDialog: (recursive) => mockEnqueueFolder('D:/Media/small', recursive),
  FilterState: () => mockFilterState(),
  // 整份状态交回，和 Go 侧一样：让前端自己猜列表变成什么样，就是把排序/去重规则写两遍。
  // 第一个参数是**改名前**的名字（新建时是空串）—— 名字就是方案的标识，见 `putProfile`。
  SaveFilterProfile: (oldName, p) => {
    const name = String(p?.name ?? '').trim();
    if (!name) throw new Error('方案要有名字');
    const profiles = normalizeProfiles(MOCK_SETTINGS.filterProfiles);
    const next = {
      name,
      description: String(p?.description ?? '').trim(),
      dirs: normalizeDirRules(p?.dirs),
      files: normalizeNameRules(p?.files),
    };
    MOCK_SETTINGS.filterProfiles = putProfile(profiles, oldName, next);
    // 正在用的那套被改名了，设置里记的名字跟着走（同 App.SaveFilterProfile）：否则它指着
    // 一个已经不存在的名字，下一次取生效方案时会静默退回第一套，而用户手里什么都没换。
    const key = String(oldName || '').trim().toLowerCase();
    if (MOCK_SETTINGS.activeFilter && MOCK_SETTINGS.activeFilter.toLowerCase() === key) {
      MOCK_SETTINGS.activeFilter = name;
    }
    return mockFilterState();
  },
  // 批量删走同一个出口（单删是 names 只有一个时的样子）。
  DeleteFilterProfiles: (names) => {
    const want = new Set((names || []).map((n) => String(n ?? '').trim().toLowerCase()).filter(Boolean));
    // 删完至少要留一套（`normalizeProfiles` 兜底），否则任务页那个下拉是空的。
    MOCK_SETTINGS.filterProfiles = normalizeProfiles(
      normalizeProfiles(MOCK_SETTINGS.filterProfiles).filter((p) => !want.has(p.name.toLowerCase())),
    );
    if (want.has(MOCK_SETTINGS.activeFilter.toLowerCase())) {
      MOCK_SETTINGS.activeFilter = normalizeProfiles(MOCK_SETTINGS.filterProfiles)[0].name;
    }
    return mockFilterState();
  },
  // 选哪套就写 MOCK_SETTINGS.activeFilter（下次打开从它开始）。`off` 那颗「不使用过滤」
  // 只翻内存里的开关，一个字都不写进设置 —— 和 App.SetActiveFilter 同一句。
  SetActiveFilter: (name, off) => {
    const profiles = normalizeProfiles(MOCK_SETTINGS.filterProfiles);
    MOCK_SETTINGS.filterProfiles = profiles;
    mockFilterOff = !!off;
    if (!off) MOCK_SETTINGS.activeFilter = pickProfile(name, profiles).name;
    return mockFilterState();
  },
  AddPaths: () => mockEnqueueFiles(MOCK_DROPPED_FILES),
  AddDroppedFiles: () => mockEnqueueFiles(MOCK_DROPPED_FILES),
  Jobs: () => MOCK_STATE.jobs,
  Stats: () => MOCK_STATE.stats,
  StartQueue: () => {}, PauseQueue: () => {}, ResumeQueue: () => {},
  TogglePause: () => false, StopQueue: () => {},
  RemoveJob: (id) => { MOCK_STATE.jobs = MOCK_STATE.jobs.filter((j) => j.id !== id); },
  RemoveJobs: (ids) => {
    // One shot for the whole batch, matching the Go side: the list has to shrink once,
    // not once per clicked row.
    const want = new Set(ids || []);
    const before = MOCK_STATE.jobs.length;
    MOCK_STATE.jobs = MOCK_STATE.jobs.filter((j) => !want.has(j.id));
    return before - MOCK_STATE.jobs.length;
  },
  RemoveFinished: () => 0, ClearQueue: () => 0, RetryFailed: () => 0,
  // Mirrors the real SetAllTemplates: every row that is not on the CPU takes the
  // new template, and the finished ones go back to 排队中 so the counters the
  // toast shows are not made up.
  SetJobTemplate: (jobId, tid) => {
    const t = MOCK_TEMPLATES.find((x) => x.id === tid);
    const j = MOCK_STATE.jobs.find((x) => x.id === jobId);
    if (t && j) { j.templateId = t.id; j.templateName = t.name; }
  },
  SetAllTemplates: (tid) => {
    const t = MOCK_TEMPLATES.find((x) => x.id === tid);
    if (!t) return { applied: 0, requeued: 0 };
    let applied = 0; let requeued = 0;
    MOCK_STATE.jobs.forEach((j) => {
      if (['running', 'preparing'].includes(j.status)) return;
      j.templateId = t.id; j.templateName = t.name;
      applied++;
      if (['done', 'warning', 'failed', 'canceled', 'skipped', 'filtered'].includes(j.status)) {
        j.status = 'pending'; j.progress = 0; j.message = '排队中'; j.error = '';
        requeued++;
      }
    });
    emitMock('queue:state', MOCK_STATE.stats);
    return { applied, requeued };
  },
  JobLogs: (id) => MOCK_STATE.log.filter(([j]) => j === id).map(([, l]) => l),
  Probe: () => mkInfo('DJI_0042.MP4', 3840, 2160),
  // Mirrors the real PreviewCommand: with a real input path the resolved paths are
  // shown, and with an empty one the {输入}/{输出} variables stand in (that is what
  // the templates page asks for, since it describes the template rather than a file).
  PreviewCommand: (tid, path) => {
    const job = MOCK_STATE.jobs.find((j) => j.input === path);
    const input = job ? job.input : '{输入}';
    const output = job ? job.output : '{输出}';
    const args = FFMPEG_ARGS(input, output, 2560, 1440);
    return {
      bin: FFMPEG_BIN, args, command: [FFMPEG_BIN, ...args].join(' '),
      warnings: [], targetW: 2560, targetH: 1440, resized: true, outputExt: 'mp4',
    };
  },
  // Same as PreviewCommand but fed the editor's draft, so an unsaved edit is
  // visible in the preview. The mock returns the same shape either way.
  PreviewTemplate: (tpl, path) => mock.PreviewCommand(tpl?.id, path),
  /* 「路径测试」：和 Go 侧的 store.ResolveDestDir + engine.ResolveOutput 同一条规则。
     mock 和真实现一分家，这个面板就成了唯一看不出问题的地方 —— 它显示的路径本来
     就是拿来核对真跑一遍的，自己错就毫无意义。

     文件存在与否在浏览器里无从判断，所以 Notices 里那条"文件当前不存在"这里不会有；
     其余（没有扩展名、路径是目录、空输入）都照搬。 */
  PreviewPaths: (tpl, srcPath) => {
    const path = String(srcPath || '').trim();
    if (!path) throw new Error('先填一个输入文件');
    if (/[\\/]$/.test(path)) throw new Error('这是一个目录，请填一个文件的完整路径');

    // Go 侧一律经 filepath.Clean / filepath.Join，出来的分隔符是反斜杠，所以这里
    // 也收成同一种写法 —— 两边显示不同样子会让"和真跑一遍一致"这句话打折扣。
    // 盘根（"E:\"）要留着那个分隔符：filepath.Clean 也留着，去掉它就变成相对路径了。
    const toWin = (p) => {
      const s = String(p).replace(/[\\/]+/g, '\\');
      if (/^[A-Za-z]:$/.test(s)) return `${s}\\`;
      if (/^[A-Za-z]:\\$/.test(s)) return s;
      return s.replace(/\\+$/, '');
    };
    const cut = Math.max(path.lastIndexOf('/'), path.lastIndexOf('\\'));
    const srcDir = toWin(cut > 0 ? path.slice(0, cut) : path);
    const base = cut > 0 ? path.slice(cut + 1) : path;
    const dot = base.lastIndexOf('.');
    const stem = dot > 0 ? base.slice(0, dot) : base;
    const ext = dot > 0 ? base.slice(dot + 1) : '';
    // filepath.Dir：去掉最后一段。盘根没有最后一段，原样返回。
    const upOne = (d) => {
      const m = /^(.*?)[\\/][^\\/]+$/.exec(d);
      if (!m) return d;
      return /^[A-Za-z]:$/.test(m[1]) ? `${m[1]}\\` : m[1];
    };
    // 名字就是最后一段；盘根退化成卷名（filepath.Base("E:\") 在 Windows 上是 `\`）。
    const leafName = (d) => {
      const m = /[\\/]([^\\/]+)$/.exec(d);
      return m ? m[1] : (d.replace(/\\+$/, '') || d);
    };

    // {token} / {token:N} —— 和 Go 侧 store.ReplaceTokens 同一套扫描：单趟从左到右，
    // 替换结果不会再被扫第二遍；认不出的 token（含参数不是整数的）连花括号一起留着。
    const tokens = (expr, resolve) => {
      const s = String(expr);
      let out = '';
      for (let i = 0; i < s.length;) {
        if (s[i] !== '{') { out += s[i++]; continue; }
        const end = s.indexOf('}', i);
        if (end < 0) { out += s.slice(i); break; }
        const inner = s.slice(i + 1, end);
        const c = inner.indexOf(':');
        // arg === null 是"没写参数"，NaN 是"写了但不是整数" —— 后者整段当普通文字。
        const tail = c < 0 ? '' : inner.slice(c + 1);
        const arg = c < 0 ? null : (/^-?\d+$/.test(tail) ? Number(tail) : NaN);
        const name = c < 0 ? inner : inner.slice(0, c);
        const val = name && !Number.isNaN(arg) ? resolve(name, arg) : undefined;
        out += val === undefined ? s.slice(i, end + 1) : val;
        i = end + 1;
      }
      return out;
    };
    // 名字类的 token 不收参数：它没法回答"哪一层"，留着花括号比猜一个好。
    const plain = (val, n) => (n === null ? val : undefined);
    // 这里只有一个文件，没有"批次内序号"，所以 {index} 各种宽度都留空 —— 和 Go 侧
    // 一致：没有序号就是空，补零不会把它变成一个 0。
    const index = () => '';

    const g = MOCK_TEMPLATES.find((t) => t.global) || MOCK_GLOBAL;
    // 「与全局不同」是唯一的继承开关：关着时整段取全局的，打开后字段留空就是留空。
    const own = !!tpl?.outputOverride;
    const spec = (own ? tpl.outDirSpec : g.outDirSpec) || {};
    const pattern = String((own ? tpl.outPattern : g.outPattern) || '').trim();
    // 「输出方式」四种：原目录 / 自定义目录 / 同级目录 / 同级顶层目录
    //（Go 侧 store.ResolveDestDir）。两个同级在这个面板里落点相同：Go 侧
    // PreviewPaths 传的 SrcRoot 就是文件自己所在的目录，而同级顶层目录量的正是它
    // —— 所以区别（添加目录 vs 所在目录）要等真的跑一批文件才看得出来，这里看不出
    // 来不是 bug。保留目录结构在两种同级下都加不出东西，同理。
    const ds = {
      mode: String(spec.mode || ''),
      dir: String(spec.dir || ''),
      prefix: String(spec.prefix || ''),
      suffix: String(spec.suffix || ''),
    };
    // 两边都留空才补默认后缀：那是拼出来等于源目录名的唯一一种输入。
    // 盘根自带一个尾分隔符，直接加会多出一个（Go 侧 filepath.Join 会折掉）。
    const winJoin = (a, b) => {
      const d = toWin(a);
      return `${d}${d.endsWith('\\') ? '' : '\\'}${b}`;
    };
    let outDir = srcDir;
    if (ds.mode === 'sibling' || ds.mode === 'siblingTop') {
      const pre = ds.prefix.trim();
      const suf = ds.suffix.trim();
      outDir = winJoin(upOne(srcDir),
        `${pre}${leafName(srcDir)}${pre === '' && suf === '' ? '_out' : suf}`);
    } else if (ds.mode === 'custom' && ds.dir.trim()) {
      outDir = toWin(ds.dir.trim());
    }
    // 「输出文件名称」只有文件名，扩展名由「输出格式」决定 —— 所以这一侧没有 {ext}，
    // Go 侧也是直接把它去掉（留着的话 "clip.{ext}" 会变成 "clip..mp4"）。
    const nameVars = {
      name: (n) => plain(stem, n),
      template: (n) => plain(String(tpl?.name || ''), n),
      dir: (n) => plain(leafName(srcDir), n),
      ext: (n) => plain('', n),
      index,
    };
    const fill = (expr, vars) => tokens(expr, (name, arg) => vars[name]?.(arg));
    const outExt = String(tpl?.container || '').trim() || ext;

    let name = fill(pattern || '{name}', nameVars);
    // 只剩一个点（老模板写的 "{name}.{ext}"，{ext} 被去掉之后留下的）不能直接接扩展名，
    // 否则成了 "clip..mp4" —— Go 侧 ResolveOutput 也是在补后缀之前先去掉这个点。
    if (name.endsWith('.')) name = name.slice(0, -1);
    if (outExt && !name.toLowerCase().endsWith(`.${outExt.toLowerCase()}`)) name += `.${outExt}`;

    const join = (n) => winJoin(outDir, n);
    const eq = (a, b) => toWin(a).toLowerCase() === toWin(b).toLowerCase();
    let outPath = join(name);
    // 绝不悄悄覆盖源文件：先退回"源文件名+新扩展名"，还撞就加 _out。
    if (eq(outPath, path)) {
      const plain = `${stem}${outExt ? `.${outExt}` : ''}`;
      const bump = eq(join(plain), path) || outExt === ext;
      outPath = join(bump ? `${stem}_out${outExt ? `.${outExt}` : ''}` : plain);
    }

    // notices 和 Go 侧 app.PreviewPaths 一一对应：没有扩展名那条，以及「同级目录」
    // 那条（面板里没有"添加目录"这层，只能拿文件自己的目录顶上去）。
    const notices = [];
    if (!ext) notices.push('这个路径没有扩展名，「输出格式」留空时就定不下扩展名');
    if (ds.mode === 'sibling') {
      notices.push('「同级目录」按你添加的那个目录算，这里只能拿这个文件自己的目录代替 —— '
        + '它真的从上一级目录添加的话，产物会落在更靠上一层的地方');
    }

    return {
      srcDir, outputDir: outDir, outputPath: outPath,
      notices,
    };
  },
  /* 和 Go 侧的 History 同一套语义：关键字、状态、日期区间过滤，再排序、切片。
     日期两端都含当天 —— 结束日期取的是那一天的最后一毫秒。 */
  History: (q) => {
    const query = q || {};
    const kw = String(query.keyword || '').toLowerCase();
    const bound = (s, end) => {
      const t = Date.parse(`${s}T00:00:00`);
      if (Number.isNaN(t)) return null; // 填不成日期就当没有这个边界
      return end ? t + 86399999 : t;
    };
    const from = query.from ? bound(query.from, false) : null;
    const to = query.to ? bound(query.to, true) : null;
    const when = (r) => Date.parse(r.endedAt || r.startedAt);
    const items = MOCK_HISTORY.filter((r) => {
      if (query.status && query.status !== 'all' && r.status !== query.status) return false;
      const t = when(r);
      if (from !== null && t < from) return false;
      if (to !== null && t > to) return false;
      const hay = `${r.input} ${r.output} ${r.templateName} ${r.error} ${r.note}`.toLowerCase();
      return !kw || hay.includes(kw);
    }).sort((a, b) => (query.sort === 'oldest' ? when(a) - when(b) : when(b) - when(a)));
    const offset = Math.max(0, query.offset || 0);
    const limit = query.limit > 0 ? query.limit : 500;
    return { total: items.length, items: items.slice(offset, offset + limit) };
  },
  DeleteRecords: (ids) => {
    const want = new Set(ids || []);
    let removed = 0;
    for (let i = MOCK_HISTORY.length - 1; i >= 0; i--) {
      if (want.has(MOCK_HISTORY[i].id)) { MOCK_HISTORY.splice(i, 1); removed++; }
    }
    return removed;
  },
  ClearHistory: () => { MOCK_HISTORY.length = 0; },
  ExportHistoryCSV: () => 'D:/Code-Project/ffmpeg-gui/build/bin/data/ffmpeg-gui-处理记录-20261005.csv',
  OpenPath: () => {}, RevealPath: () => {}, OpenOutputDir: () => {}, ShowDataDir: () => {},
  /* 浏览器预览里没有磁盘可查，所以把真机的判据换成一个看得出来的约定：给了
     fallback 就说明这条路径是被搬走过的，定位落在 fallback 上，和真机上
     「原路径已经不在了」那一支长得一样。都不给就是真机那句「没有可定位的路径」。 */
  Locate: (path, fallback) => {
    const first = String(path || '').trim();
    const alt = String(fallback || '').trim();
    if (!first && !alt) throw new Error('没有可定位的路径');
    if (alt) return { path: alt, moved: true };
    return { path: first, moved: false };
  },
  DataDir: () => 'D:/Code-Project/ffmpeg-gui/build/bin/data',
  AppVersion: () => '1.0.0', QuitApp: () => {}, ShowWindow: () => {},

  // Queue control. These were missing entirely, so every 暂停 / 继续 / 停止 click in
  // a mock-driven preview threw and the button looked broken -- which is exactly the
  // bug report that sent me looking at the real implementation first.
  Stats: () => ({ ...MOCK_STATE.stats }),
  StartQueue: () => {
    MOCK_STATE.stats.started = true;
    MOCK_STATE.stats.paused = false;
    emitMock('queue:state', { ...MOCK_STATE.stats });
  },
  PauseQueue: () => {
    MOCK_STATE.stats.paused = true;
    emitMock('queue:state', { ...MOCK_STATE.stats });
  },
  ResumeQueue: () => {
    MOCK_STATE.stats.paused = false;
    emitMock('queue:state', { ...MOCK_STATE.stats });
  },
  TogglePause: () => {
    MOCK_STATE.stats.paused = !MOCK_STATE.stats.paused;
    emitMock('queue:state', { ...MOCK_STATE.stats });
    return MOCK_STATE.stats.paused;
  },
  StopQueue: () => {
    MOCK_STATE.stats.started = false;
    MOCK_STATE.stats.paused = false;
    emitMock('queue:state', { ...MOCK_STATE.stats });
  },
  // RemoveJob / RemoveJobs / RemoveFinished / ClearQueue / RetryFailed are defined
  // further up in this same literal. They must not be repeated here: a duplicate key
  // silently wins, and these five used to reappear as `() => 0` no-ops -- so every
  // 移除 in a browser preview did nothing at all, and the row that stayed put looked
  // like a rendering bug rather than a mock one.
  //
  // The browser preview has no disk to delete from, so 删除 only marks the rows. That
  // is enough to exercise the paths that matter for the UI -- the confirm dialog, the
  // toast, the row's 输出已删除 state -- without pretending to have removed a file.
  DeleteOutput: (id) => mock.DeleteOutputs([id]),
  DeleteOutputs: (ids) => {
    const want = new Set(ids || []);
    let deleted = 0;
    MOCK_STATE.jobs.forEach((j) => {
      if (!want.has(j.id) || j.outputDeleted || !j.output) return;
      if (['pending', 'preparing', 'running'].includes(j.status)) return;
      j.outputDeleted = true;
      deleted++;
      emitMock('job:update', { ...j });
    });
    return { deleted, skipped: want.size - deleted, paths: [], errors: [] };
  },
};

/* A tiny live ticker so the mock shows moving progress in previews -- and a log
   that actually streams, which is the only way to see how the log panel follows
   without a real encode running. */
if (isMock) {
  setInterval(() => {
    const running = MOCK_STATE.jobs.filter((j) => j.status === 'running');
    if (!running.length) return;
    running.forEach((j) => {
      j.progress = Math.min(0.99, j.progress + 0.006);
      j.outTimeMs += 4200;
      j.speed = 3.2 + Math.sin(Date.now() / 4000) * 0.5;
      j.message = `${(j.progress * 100).toFixed(1)}% · ${fmtClock(j.outTimeMs / 1000)} · ${j.speed.toFixed(1)}x`;
      emitMock('job:update', { ...j });
      const line = `frame=${Math.round(j.outTimeMs / 42)} fps=${(29.97 * j.speed).toFixed(0)}`
        + ` size=${Math.round(j.outTimeMs * 1.1)}kB time=${fmtClock(j.outTimeMs / 1000)}.00`
        + ` bitrate=88560.1kbits/s speed=${j.speed.toFixed(2)}x`;
      MOCK_STATE.log.push([j.id, line]);
      j.logLineCount = (j.logLineCount || 0) + 1;
      emitMock('job:log', { jobId: j.id, lines: [line] });
    });
  }, 1000);
}

function fmtClock(sec) {
  const s = Math.round(sec);
  return `${Math.floor(s / 60)}:${String(s % 60).padStart(2, '0')}`;
}

/* ------------------------------------------------------------------ export */

export const api = {
  bootstrap: () => call('Bootstrap'),
  settings: () => call('Settings'),
  saveSettings: (s) => call('SaveSettings', s),
  resetSettings: () => call('ResetSettings'),
  templates: () => call('Templates'),
  globalTemplate: () => call('GlobalTemplate'),
  newTemplate: () => call('NewTemplate'),
  saveTemplate: (t) => call('SaveTemplate', t),
  saveGlobalTemplate: (t) => call('SaveGlobalTemplate', t),
  deleteTemplates: (ids) => call('DeleteTemplates', ids),
  duplicateTemplate: (id) => call('DuplicateTemplate', id),
  reorderTemplates: (ids) => call('ReorderTemplates', ids),
  exportTemplates: () => call('ExportTemplates'),
  importTemplatesFromFile: () => call('ImportTemplatesFromFile'),
  autoDetectBinaries: () => call('AutoDetectBinaries'),
  checkBinary: (p) => call('CheckBinary', p),
  pickBinary: () => call('PickBinary'),
  pickDirectory: () => call('PickDirectory'),
  pickFile: () => call('PickFile'),
  toolInfo: () => call('ToolInfo'),
  detectEncoders: () => call('DetectEncoders'),
  addFilesDialog: (recursive) => call('AddFilesDialog', recursive),
  addFolderDialog: (recursive) => call('AddFolderDialog', recursive),
  previewFolderScan: (scan) => call('PreviewFolderScan', scan),
  filterState: () => call('FilterState'),
  // 第一个参数是这套方案**改名前**的名字（新建时传空串）：名字就是方案的标识，只给新
  // 名字的话"重命名"在后端眼里是"多一套"。
  saveFilterProfile: (oldName, p) => call('SaveFilterProfile', oldName, p),
  deleteFilterProfiles: (names) => call('DeleteFilterProfiles', names),
  // 任务页临时换一套：只改内存，不写设置文件。
  setActiveFilter: (name, off) => call('SetActiveFilter', name, off),
  // 「过滤」页上的「设为默认」：这个才落盘。
  addPaths: (paths, recursive) => call('AddPaths', paths, recursive),
  addDroppedFiles: (paths) => call('AddDroppedFiles', paths),
  jobs: () => call('Jobs'),
  stats: () => call('Stats'),
  startQueue: () => call('StartQueue'),
  pauseQueue: () => call('PauseQueue'),
  resumeQueue: () => call('ResumeQueue'),
  togglePause: () => call('TogglePause'),
  stopQueue: () => call('StopQueue'),
  removeJob: (id) => call('RemoveJob', id),
  removeJobs: (ids) => call('RemoveJobs', ids),
  removeFinished: () => call('RemoveFinished'),
  clearQueue: () => call('ClearQueue'),
  retryFailed: () => call('RetryFailed'),
  jobLogs: (id) => call('JobLogs', id),
  deleteOutput: (id) => call('DeleteOutput', id),
  deleteOutputs: (ids) => call('DeleteOutputs', ids),
  setJobTemplate: (jobId, tid) => call('SetJobTemplate', jobId, tid),
  setAllTemplates: (tid) => call('SetAllTemplates', tid),
  probe: (p) => call('Probe', p),
  previewCommand: (tid, path) => call('PreviewCommand', tid, path),
  previewTemplate: (tpl, path) => call('PreviewTemplate', tpl, path),
  previewPaths: (tpl, path) => call('PreviewPaths', tpl, path),
  history: (q) => call('History', q),
  deleteRecords: (ids) => call('DeleteRecords', ids),
  clearHistory: () => call('ClearHistory'),
  exportHistoryCSV: (q) => call('ExportHistoryCSV', q),
  openPath: (p) => call('OpenPath', p),
  revealPath: (p) => call('RevealPath', p),
  locate: (path, fallback) => call('Locate', path, fallback),
  openOutputDir: () => call('OpenOutputDir'),
  dataDir: () => call('DataDir'),
  showDataDir: () => call('ShowDataDir'),
  appVersion: () => call('AppVersion'),
  quitApp: () => call('QuitApp'),
  showWindow: () => call('ShowWindow'),
};

export const EVENTS = {
  jobUpdate: 'job:update',
  jobLog: 'job:log',
  queue: 'queue:state',
  toast: 'app:toast',
  record: 'record:new',
  templatesChanged: 'templates:changed',
};
