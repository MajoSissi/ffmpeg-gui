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
  outMode: 'sibling', outDir: '', outSuffix: '_out', outPattern: '{name}',
  outputOverride: true,
  perf: { concurrency: 2, logLevel: 'warning', retryCount: 0, threads: 0, idlePriority: false, deleteOnFail: true },
  // The global ships WITHOUT this section (store.DefaultGlobalTemplate leaves it
  // nil, and a non-nil section always skips an already-processed file). The mock
  // keeps one so the editor's follow-state can be exercised; values match what
  // seedSection produces when a user opens the section.
  existing: {
    action: 'keep',
    dest: { mode: 'sibling', dir: '', suffix: '_done' },
    pattern: '{name}', overwrite: false,
  },
  filter: {
    minSizeMB: 300, maxSizeMB: 0, minLongEdge: 0, maxLongEdge: 0, minDuration: 0, maxDuration: 0,
    includeExts: [], excludeExts: [], action: 'move',
    dest: { mode: 'mirror', dir: 'D:/Media/small', suffix: '_out' },
    renamePattern: '{name}', overwrite: false,
  },
  problems: {
    errorAction: 'move', errorDest: { mode: 'mirror', dir: 'D:/Media/error', suffix: '_out' },
    warningAction: 'mark', warningDest: { mode: 'mirror', dir: '', suffix: '_out' },
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
  };
}

const MOCK_STATE = {
  jobs: [
    mkJob('j1', 'DJI_0042.MP4', 3840, 2160, 'running', 0.62, { speed: 3.4, bitrate: '4128.5', message: '62.0% · 7:48 · 3.4x' }),
    mkJob('j2', 'DJI_0043.MP4', 3840, 2160, 'pending', 0),
    mkJob('j3', '旅行vlog-竖屏.mp4', 2160, 3840, 'pending', 0, { tw: 1440, th: 2560 }),
    mkJob('j4', '会议录屏_2024.mkv', 1920, 1080, 'pending', 0),
    mkJob('j5', '宣传片_最终版.mov', 4096, 2304, 'done', 1, { elapsedMs: 412_000, speed: 3.1, tw: 2560, th: 1440, size: 2_940_000_000, after: mkInfo('宣传片_最终版.mp4', 2560, 1440, { size: 462_000_000, duration: 212, bitRate: 17_400_000, vc: 'hevc' }) }),
    mkJob('j6', '手机拍摄-慢动作.mp4', 3840, 2160, 'warning', 1, { elapsedMs: 96_400, message: '已完成（2 条警告）', warnings: ['输出文件复核失败: Invalid data found when processing input', '源文件已移动到 D:/Media/error'], tw: 2560, th: 1440, size: 1_240_000_000, after: mkInfo('手机拍摄-慢动作.mp4', 2560, 1440, { size: 188_000_000, duration: 96, bitRate: 15_600_000, vc: 'hevc' }) }),
    mkJob('j7', '古老素材.avi', 720, 576, 'failed', 0.14, { elapsedMs: 31_000, error: 'av_interleaved_write_frame(): Invalid argument' }),
    mkJob('j8', '超短视频.mp4', 1280, 720, 'filtered', 0, { message: '体积 18.4 MB 小于下限 300 MB；已移动到 D:/Media/small' }),
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

const MOCK_HISTORY = [
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
    error: '', warnings: [], startedAt: new Date(Date.now() - 12000e3).toISOString(), endedAt: new Date(Date.now() - 11990e3).toISOString(),
    elapsedMs: 420, speed: 0,
    before: { exists: true, path: '', size: 19_300_000, container: 'mp4', duration: 12, videoCodec: 'h264', width: 1280, height: 720, fps: 30, pixFmt: 'yuv420p', audioCodec: 'aac', sampleRate: 48000, channels: 2, bitRate: 12_800_000 },
    after: { exists: false, path: '', size: 0, container: '', duration: 0, videoCodec: '', width: 0, height: 0, fps: 0, pixFmt: '', audioCodec: '', sampleRate: 0, channels: 0, bitRate: 0 },
  },
];

const MOCK_SETTINGS = {
  ffmpegPath: 'C:/Users/Majo/AppData/Local/Microsoft/WinGet/Links/ffmpeg.exe',
  ffprobePath: 'C:/Users/Majo/AppData/Local/Microsoft/WinGet/Links/ffprobe.exe',
  globalInArgs: '', globalOutArgs: '', hardwareDecode: false,
  preventSleep: true, enableTray: true, closeToTray: true, startMinimized: false, confirmExit: true,
  keepLogLines: 2000, saveRunLog: true, logMaxSizeMB: 50, logKeepDays: 7,
  lastTemplateId: 't-4k2k', showLogPanel: true, logPanelHeight: 0, logPanelSized: false, logDir: '',
};

// Shared by the lists at the bottom so one wording fix lands everywhere at once.
// They mirror app.go's destModes / relocateActions verbatim -- these used to be
// four near-copies whose wording had already drifted from the real thing.
const MOCK_DEST_MODES = [
  { value: 'same', label: '与源文件同目录' },
  { value: 'sibling', label: '同级顶层目录 + 后缀（源目录结构）' },
  { value: 'custom', label: '指定目录' },
  { value: 'mirror', label: '指定目录（源目录结构）' },
];
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
  outputModes: MOCK_DEST_MODES,
  // The same four rules reach every section that moves a file somewhere.
  destModes: MOCK_DEST_MODES,
  existingActions: MOCK_RELOCATE,
  filterActions: MOCK_RELOCATE,
  problemActions: [...MOCK_RELOCATE, { value: 'mark', label: '仅在结果中标记' }],
};

const mock = {
  Bootstrap() {
    return {
      runtime: {
        os: 'windows', arch: 'amd64', cpus: 16, dataDir: 'D:/Code-Project/ffmpeg-gui/build/bin/data', version: '1.0.0',
        binaries: { ffmpeg: MOCK_SETTINGS.ffmpegPath, ffprobe: MOCK_SETTINGS.ffprobePath, ffmpegFrom: 'path', ffprobeFrom: 'path' },
        ffmpeg: { name: 'ffmpeg', path: MOCK_SETTINGS.ffmpegPath, source: 'path', version: 'ffmpeg version 7.1.1 Copyright (c) 2000-2025 the FFmpeg developers', ok: true },
        ffprobe: { name: 'ffprobe', path: MOCK_SETTINGS.ffprobePath, source: 'path', version: 'ffprobe version 7.1.1 Copyright (c) 2000-2025 the FFmpeg developers', ok: true },
      },
      settings: MOCK_SETTINGS, templates: MOCK_TEMPLATES, jobs: MOCK_STATE.jobs,
      stats: MOCK_STATE.stats, history: MOCK_HISTORY, options: OPTIONS,
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
  DeleteTemplate: (id) => {
    if (id === 't-global') throw new Error('「全局模板」不能删除');
    const i = MOCK_TEMPLATES.findIndex((x) => x.id === id);
    if (i >= 0) MOCK_TEMPLATES.splice(i, 1);
    emitMock('templates:changed', MOCK_TEMPLATES);
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
    if (t.filter) c.filter = { ...t.filter, dest: { ...t.filter.dest } };
    if (t.problems) c.problems = { ...t.problems, errorDest: { ...t.problems.errorDest }, warningDest: { ...t.problems.warningDest } };
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
  ToolInfo: () => ([
    { name: 'ffmpeg', path: MOCK_SETTINGS.ffmpegPath, source: 'path', version: 'ffmpeg version 7.1.1 Copyright (c) 2000-2025 the FFmpeg developers', ok: true },
    { name: 'ffprobe', path: MOCK_SETTINGS.ffprobePath, source: 'path', version: 'ffprobe version 7.1.1 Copyright (c) 2000-2025 the FFmpeg developers', ok: true },
  ]),
  DetectEncoders: () => {
    const ok = new Set(['libx264', 'libx265', 'libsvtav1', 'libvpx-vp9', 'mpeg4', 'gif', 'webp', 'h264_nvenc', 'hevc_nvenc', 'av1_nvenc']);
    return OPTIONS.videoCodecs.filter((o) => o.value !== 'copy')
      .map((o) => ({ name: o.value, label: o.label, ok: ok.has(o.value) }));
  },
  AddFilesDialog: () => ({ added: 3, errors: [] }),
  AddFolderDialog: () => ({ added: 12, errors: ['D:/Media/broken: 拒绝访问'] }),
  AddPaths: () => ({ added: 2, errors: [] }),
  AddDroppedFiles: () => ({ added: 2, errors: [] }),
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
  History: (q) => ({ total: MOCK_HISTORY.length, items: MOCK_HISTORY }),
  ClearHistory: () => {}, ExportHistoryCSV: () => 'D:/Code-Project/ffmpeg-gui/build/bin/data/ffmpeg-gui-处理记录-20261005.csv',
  OpenPath: () => {}, RevealPath: () => {}, OpenOutputDir: () => {}, ShowDataDir: () => {},
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
  deleteTemplate: (id) => call('DeleteTemplate', id),
  duplicateTemplate: (id) => call('DuplicateTemplate', id),
  reorderTemplates: (ids) => call('ReorderTemplates', ids),
  exportTemplates: () => call('ExportTemplates'),
  importTemplatesFromFile: () => call('ImportTemplatesFromFile'),
  autoDetectBinaries: () => call('AutoDetectBinaries'),
  checkBinary: (p) => call('CheckBinary', p),
  pickBinary: () => call('PickBinary'),
  pickDirectory: () => call('PickDirectory'),
  toolInfo: () => call('ToolInfo'),
  detectEncoders: () => call('DetectEncoders'),
  addFilesDialog: (recursive) => call('AddFilesDialog', recursive),
  addFolderDialog: (recursive) => call('AddFolderDialog', recursive),
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
  history: (q) => call('History', q),
  clearHistory: () => call('ClearHistory'),
  exportHistoryCSV: (q) => call('ExportHistoryCSV', q),
  openPath: (p) => call('OpenPath', p),
  revealPath: (p) => call('RevealPath', p),
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
