package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v2/pkg/options"
	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"ffmpeggui/internal/engine"
	"ffmpeggui/internal/media"
	"ffmpeggui/internal/store"
	"ffmpeggui/internal/sysx"
	"ffmpeggui/internal/tray"
)

// AppVersion is stamped into the UI and the CSV export.
const AppVersion = "1.0.0"

// App is the Wails-bound application object.
type App struct {
	ctx context.Context

	mu        sync.RWMutex
	settings  store.Settings
	templates []store.Template
	binaries  media.Binaries
	ffmpegTI  media.ToolInfo
	ffprobeTI media.ToolInfo

	histMu    sync.Mutex
	history   []store.Record
	histDirty bool

	// filterOff 是任务页上那个「不使用过滤」。**只在内存里**：它说的不是"我这套规则
	// 怎么用"，而是"这一次我不要过滤"，所以关掉程序再打开就回到上次选的那套。
	// 它和 settings 共用一把锁读写。
	filterOff bool

	runner   *engine.Runner
	tray     *tray.Controller
	trayIcon []byte
	quitting bool

	lastOutputDir string
}

// NewApp builds the application object.
func NewApp(trayIcon []byte) *App {
	return &App{
		runner:   engine.NewRunner(),
		trayIcon: trayIcon,
	}
}

// ---------------------------------------------------------------------------
// Lifecycle
// ---------------------------------------------------------------------------

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx

	a.mu.Lock()
	a.settings = store.LoadSettings()
	a.templates = store.LoadTemplates()
	a.refreshBinariesLocked()
	logCfg := a.settings
	a.mu.Unlock()

	// Sweep the log folder once per launch. Doing it here (and not only after a
	// task finishes) means the limits still hold for someone who opens the app
	// rarely and never runs anything.
	_ = os.MkdirAll(logCfg.LogDir, 0o755)
	store.PruneLogs(logCfg.LogDir, int64(logCfg.LogMaxSizeMB)*1024*1024, logCfg.LogKeepDays)

	a.histMu.Lock()
	a.history = store.LoadHistory()
	a.histMu.Unlock()

	a.runner.Configure(engine.Providers{
		Settings: a.Settings,
		Binaries: a.Binaries,
		Template: a.templateByID,
		// The engine reads the defaults straight off the global template, so
		// editing it takes effect without any extra plumbing.
		GlobalTemplate: a.GlobalTemplate,
	}, a.emitEngine, a.appendRecord)

	a.startTrayIfEnabled()
	go a.historySaver()
	// No WindowHide here on purpose. Wails runs OnStartup in a goroutine *after*
	// it has already put the window on screen, so hiding it from here can only
	// ever be a race the user watches happen. "Start hidden" is decided before
	// the window exists instead — see StartHidden in main.go.
}

// showWindow brings the main window back, whether it was minimised or hidden to
// the tray. Wails' WindowShow covers both: un-minimise if it was minimised,
// show if it was hidden, then foreground it.
func (a *App) showWindow() {
	if a.ctx == nil {
		return
	}
	wailsruntime.WindowShow(a.ctx)
	wailsruntime.WindowUnminimise(a.ctx)
}

// onSecondInstance runs in the copy that was already running when a second one
// was launched. The new process has sent us its arguments and exited before it
// ever created a window, so all we owe the user is their window back.
func (a *App) onSecondInstance(_ options.SecondInstanceData) {
	a.showWindow()
}

// AddDroppedFiles is called by the frontend's drag & drop handler.
// AddDroppedFiles queues what was dragged onto the window.
//
// 递归扫描：README 一直是这么写的（"拖入文件或整个目录，递归扫描子目录"），而这里
// 传的是 false —— 拖进来的文件夹只收第一层，和「添加文件夹」不是同一个答案。过滤
// 更把这件事放大了：条件要在整棵树上找子目录，只看一层就会静默漏掉更深的那几个。
func (a *App) AddDroppedFiles(paths []string) AddResult {
	res := a.AddPaths(paths, true)
	if res.Added > 0 {
		a.emitToastKind("success", fmt.Sprintf("已拖入 %d 个文件", res.Added))
	}
	for _, e := range res.Errors {
		a.emitToastKind("error", e)
	}
	return res
}

func (a *App) shutdown(ctx context.Context) {
	a.runner.Shutdown()
	if a.tray != nil {
		a.tray.Stop()
	}
	a.flushHistory()
}

func (a *App) beforeClose(ctx context.Context) bool {
	a.mu.RLock()
	quitting := a.quitting
	closeToTray := a.settings.CloseToTray
	trayOn := a.settings.EnableTray
	a.mu.RUnlock()

	if !quitting && closeToTray && trayOn {
		wailsruntime.WindowHide(ctx)
		return true // stay alive in the tray
	}
	return false
}

// Quit exits the application for real.
func (a *App) Quit() {
	a.mu.Lock()
	a.quitting = true
	a.mu.Unlock()
	if a.tray != nil {
		a.tray.Stop()
	}
	if a.ctx != nil {
		wailsruntime.Quit(a.ctx)
	} else {
		os.Exit(0)
	}
}

// ---------------------------------------------------------------------------
// Tray
// ---------------------------------------------------------------------------

func (a *App) startTrayIfEnabled() {
	a.mu.RLock()
	enabled := a.settings.EnableTray
	a.mu.RUnlock()
	if !enabled {
		return
	}
	if a.tray == nil {
		a.tray = tray.New(tray.Callbacks{
			OnShow: a.showWindow,
			OnQuit: func() { a.Quit() },
			// The icon ignores writes before it exists, so the queue the user
			// already has is pushed once the tray is really up.
			OnReady: func() { a.refreshTrayTooltip(a.runner.Stats()) },
		}, a.trayIcon, "", "FFmpeg GUI — 媒体批量处理")
	}
	a.tray.Start()
}

// ---------------------------------------------------------------------------
// Settings & templates
// ---------------------------------------------------------------------------

// Settings returns a copy of the current configuration.
func (a *App) Settings() store.Settings {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.settings
}

// Binaries returns the resolved executable paths.
func (a *App) Binaries() media.Binaries {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.binaries
}

func (a *App) templateByID(id string) (store.Template, bool) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	for _, t := range a.templates {
		if t.ID == id {
			return t, true
		}
	}
	return store.Template{}, false
}

// GlobalTemplate returns the template that holds every default. It is the single
// source the UI's "跟随全局" placeholders resolve against.
func (a *App) GlobalTemplate() store.Template {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return store.GlobalOrDefault(a.templates)
}

func (a *App) refreshBinariesLocked() {
	a.binaries = media.Resolve(a.settings.FFmpegPath, a.settings.FFprobePath)
	a.ffmpegTI, a.ffprobeTI = media.Inspect(a.binaries)
}

// SaveSettings persists configuration and applies side effects immediately.
func (a *App) SaveSettings(s store.Settings) (store.Settings, error) {
	s.Normalize()

	a.mu.Lock()
	prev := a.settings
	a.settings = s
	a.refreshBinariesLocked()
	// The worker ceiling now lives on the global template, so read it back
	// rather than from the (removed) settings field.
	prevWorkers := globalConcurrency(store.GlobalOrDefault(a.templates))
	nextWorkers := globalConcurrency(store.GlobalOrDefault(a.templates))
	a.mu.Unlock()

	if err := store.SaveSettings(s); err != nil {
		return s, err
	}

	if nextWorkers != prevWorkers {
		a.runner.EnsureWorkers(nextWorkers)
	}
	if !s.EnableTray && a.tray != nil {
		a.tray.Stop()
		a.tray = nil
	}
	if s.EnableTray && a.tray == nil {
		a.startTrayIfEnabled()
	}
	if s.LogDir == "" || s.LogDir != prev.LogDir {
		_ = os.MkdirAll(s.LogDir, 0o755)
	}
	a.emitState()
	return s, nil
}

func globalConcurrency(t store.Template) int {
	if t.Perf == nil || t.Perf.Concurrency < 1 {
		return 1
	}
	return t.Perf.Concurrency
}

// ResetSettings restores the factory configuration.
func (a *App) ResetSettings() (store.Settings, error) {
	return a.SaveSettings(store.DefaultSettings())
}

// Templates returns every template.
func (a *App) Templates() []store.Template {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return append([]store.Template(nil), a.templates...)
}

// NewTemplate returns a blank template seeded from the global defaults, so the
// editor opens on values that already make sense.
func (a *App) NewTemplate() store.Template {
	return store.NewFromGlobal(a.GlobalTemplate())
}

// SaveTemplate creates or updates a template.
func (a *App) SaveTemplate(t store.Template) (store.Template, error) {
	// The global template is the defaults every other template inherits, so it
	// is never copied or renamed; ignore whatever the editor sent back.
	t.Global = false
	if t.ID == store.GlobalTemplateID {
		return t, fmt.Errorf("「%s」是内置的默认值模板，不能作为普通模板保存", store.GlobalTemplateName)
	}
	t.Normalize()
	if strings.TrimSpace(t.Name) == "" {
		return t, fmt.Errorf("模板名称不能为空")
	}

	a.mu.Lock()
	found := false
	for i := range a.templates {
		if a.templates[i].ID == t.ID {
			if a.templates[i].Global {
				a.mu.Unlock()
				return t, fmt.Errorf("「%s」不能被覆盖", store.GlobalTemplateName)
			}
			t.Builtin = a.templates[i].Builtin
			a.templates[i] = t
			found = true
			break
		}
	}
	if !found {
		t.ID = store.NewID()
		t.Builtin = false
		a.templates = append(a.templates, t)
	}
	list := append([]store.Template(nil), a.templates...)
	a.mu.Unlock()

	if err := store.SaveTemplates(list); err != nil {
		return t, err
	}
	// The 「已处理过的文件」record means "this file came out of this template
	// already". Editing the template changes what it would produce, so the record
	// no longer describes anything: drop it, otherwise re-queueing the same
	// folder after a change would report 已跳过 for every file and quietly not
	// apply the change.
	a.runner.ForgetTemplate(t.ID)
	a.emit("templates:changed", list)
	return t, nil
}

// SaveGlobalTemplate updates the defaults template in place. It is a separate
// entry point so the editor can save it without the copy guard above.
func (a *App) SaveGlobalTemplate(t store.Template) (store.Template, error) {
	t.ID = store.GlobalTemplateID
	t.Global = true
	t.Builtin = false
	t.Normalize()
	if strings.TrimSpace(t.Name) == "" {
		t.Name = store.GlobalTemplateName
	}

	a.mu.Lock()
	for i := range a.templates {
		if a.templates[i].Global {
			a.templates[i] = t
			list := append([]store.Template(nil), a.templates...)
			a.mu.Unlock()
			if err := store.SaveTemplates(list); err != nil {
				return t, err
			}
			// Every template inherits from this one, so a change here changes
			// what all of them produce. Drop every record for the same reason
			// SaveTemplate drops one template's.
			a.runner.ForgetAllProcessed()
			a.applyConcurrency()
			a.emit("templates:changed", list)
			return t, nil
		}
	}
	a.mu.Unlock()
	return t, fmt.Errorf("「%s」不存在", store.GlobalTemplateName)
}

// applyConcurrency resizes the worker pool after the global template changed.
func (a *App) applyConcurrency() {
	a.runner.EnsureWorkers(globalConcurrency(a.GlobalTemplate()))
}

// DeleteTemplate removes a template.
func (a *App) DeleteTemplate(id string) error {
	if _, err := a.DeleteTemplates([]string{id}); err != nil {
		return err
	}
	return nil
}

// DeleteTemplates removes every template in ids and reports how many went away.
//
// 批量删是**同一个出口**：单删只是 ids 只有一个时的样子。判据（全局模板删不掉）留在
// 这里一处，所以"勾五行删五下"和"删一行"永远不会给出两种结果 —— 而它们在界面上是
// 同一颗按钮。
//
// 全局模板是**整批跳过**，不是整批失败：多选里混进它只是这一项留着，其余照删。
func (a *App) DeleteTemplates(ids []string) (int, error) {
	want := map[string]bool{}
	for _, id := range ids {
		if id != "" && id != store.GlobalTemplateID {
			want[id] = true
		}
	}

	a.mu.Lock()
	out := make([]store.Template, 0, len(a.templates))
	for _, t := range a.templates {
		if want[t.ID] {
			continue
		}
		out = append(out, t)
	}
	removed := len(a.templates) - len(out)
	if removed == 0 {
		a.mu.Unlock()
		return 0, fmt.Errorf("要删的模板已经不在了")
	}
	a.templates = out
	list := append([]store.Template(nil), out...)
	a.mu.Unlock()

	if err := store.SaveTemplates(list); err != nil {
		return removed, err
	}
	a.emit("templates:changed", list)
	return removed, nil
}

// DuplicateTemplate copies a template under a new name.
func (a *App) DuplicateTemplate(id string) (store.Template, error) {
	src, ok := a.templateByID(id)
	if !ok {
		return store.Template{}, fmt.Errorf("模板不存在")
	}
	if src.Global {
		return store.Template{}, fmt.Errorf("「%s」是默认值来源，请直接新建模板", store.GlobalTemplateName)
	}
	src.ID = store.NewID()
	src.Name = uniqueName(src.Name, a.templateNames())
	src.Builtin = false
	src.Global = false
	// The section pointers would be shared with the source; copy them so editing
	// the duplicate cannot rewrite the original.
	src.Perf = store.ClonePerf(src.Perf)
	src.Existing = store.CloneExisting(src.Existing)
	src.Filter = store.CloneFilter(src.Filter)
	src.Problems = store.CloneProblems(src.Problems)

	a.mu.Lock()
	a.templates = append(a.templates, src)
	list := append([]store.Template(nil), a.templates...)
	a.mu.Unlock()

	if err := store.SaveTemplates(list); err != nil {
		return src, err
	}
	a.emit("templates:changed", list)
	return src, nil
}

// ReorderTemplates persists a new display order. The frontend drags the list
// around, so the order it sends is the order to keep. The global template is
// pinned first and any id that is unknown (or missing from ids) keeps its
// relative position at the end, so a stale UI can never drop a template.
func (a *App) ReorderTemplates(ids []string) ([]store.Template, error) {
	a.mu.Lock()
	byID := make(map[string]store.Template, len(a.templates))
	for _, t := range a.templates {
		byID[t.ID] = t
	}
	out := make([]store.Template, 0, len(a.templates))
	seen := make(map[string]bool, len(a.templates))
	// The defaults template always leads, whatever the caller sent.
	if g, ok := byID[store.GlobalTemplateID]; ok {
		out = append(out, g)
		seen[g.ID] = true
	}
	for _, id := range ids {
		t, ok := byID[id]
		if !ok || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, t)
	}
	// Anything the caller did not mention keeps its old relative order.
	rest := make([]store.Template, 0, len(a.templates))
	for _, t := range a.templates {
		if !seen[t.ID] {
			rest = append(rest, t)
		}
	}
	a.templates = append(out, rest...)
	list := append([]store.Template(nil), a.templates...)
	a.mu.Unlock()

	if err := store.SaveTemplates(list); err != nil {
		return nil, err
	}
	return list, nil
}

// ExportTemplates writes every template to a JSON file chosen by the user.
func (a *App) ExportTemplates() (string, error) {
	path, err := wailsruntime.SaveFileDialog(a.ctx, wailsruntime.SaveDialogOptions{
		Title:           "导出参数模板",
		DefaultFilename: "ffmpeg-gui-templates.json",
		Filters:         []wailsruntime.FileFilter{{DisplayName: "JSON", Pattern: "*.json"}},
	})
	if err != nil || path == "" {
		return "", err
	}
	a.mu.RLock()
	defer a.mu.RUnlock()
	return path, store.WriteJSON(path, a.templates)
}

// ImportTemplatesFromFile loads templates from a JSON file chosen by the user.
func (a *App) ImportTemplatesFromFile() (int, error) {
	path, err := wailsruntime.OpenFileDialog(a.ctx, wailsruntime.OpenDialogOptions{
		Title:   "导入参数模板",
		Filters: []wailsruntime.FileFilter{{DisplayName: "JSON", Pattern: "*.json"}},
	})
	if err != nil || path == "" {
		return 0, err
	}
	var incoming []store.Template
	if _, err := store.ReadJSON(path, &incoming); err != nil {
		return 0, fmt.Errorf("无法解析模板文件: %w", err)
	}
	if len(incoming) == 0 {
		return 0, fmt.Errorf("文件中没有模板")
	}

	a.mu.Lock()
	existing := map[string]bool{}
	for _, t := range a.templates {
		existing[t.ID] = true
	}
	names := a.templateNames()
	n := 0
	for _, t := range incoming {
		if existing[t.ID] {
			t.ID = store.NewID()
		}
		t.Builtin = false
		// An imported file may carry its own global template; ours is the only
		// one that counts, so demote it to a regular template.
		if t.Global {
			t.Global = false
			if t.ID == store.GlobalTemplateID {
				t.ID = store.NewID()
			}
			if t.Name == store.GlobalTemplateName {
				t.Name = uniqueName(t.Name, names)
			}
		}
		if _, dup := names[t.Name]; dup {
			t.Name = uniqueName(t.Name, names)
		}
		names[t.Name] = true
		t.Normalize()
		a.templates = append(a.templates, t)
		n++
	}
	list := append([]store.Template(nil), a.templates...)
	a.mu.Unlock()

	if err := store.SaveTemplates(list); err != nil {
		return n, err
	}
	a.emit("templates:changed", list)
	return n, nil
}

func (a *App) templateNames() map[string]bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	m := map[string]bool{}
	for _, t := range a.templates {
		m[t.Name] = true
	}
	return m
}

func uniqueName(base string, taken map[string]bool) string {
	for i := 2; i < 1000; i++ {
		cand := fmt.Sprintf("%s (%d)", base, i)
		if !taken[cand] {
			return cand
		}
	}
	return base
}

// ---------------------------------------------------------------------------
// Binary selection
// ---------------------------------------------------------------------------

// AutoDetectBinaries re-resolves ffmpeg/ffprobe from PATH.
func (a *App) AutoDetectBinaries() (media.Binaries, media.ToolInfo, media.ToolInfo) {
	b := media.Resolve("", "")
	ff, fp := media.Inspect(b)
	a.mu.Lock()
	a.binaries, a.ffmpegTI, a.ffprobeTI = b, ff, fp
	a.mu.Unlock()
	return b, ff, fp
}

// CheckBinary validates a user supplied path.
func (a *App) CheckBinary(path string) media.ToolInfo {
	if strings.TrimSpace(path) == "" {
		return media.ToolInfo{Error: "路径为空"}
	}
	if err := media.LooksLikeFFmpeg(path); err != nil {
		return media.ToolInfo{Path: path, Error: err.Error()}
	}
	ff, fp := media.Inspect(media.Binaries{FFmpeg: path, FFprobe: path})
	if ff.OK {
		return ff
	}
	return fp
}

// PickBinary opens a file dialog to choose ffmpeg or ffprobe.
func (a *App) PickBinary() (string, error) {
	return wailsruntime.OpenFileDialog(a.ctx, wailsruntime.OpenDialogOptions{
		Title: "选择可执行文件",
		Filters: []wailsruntime.FileFilter{
			{DisplayName: "可执行文件", Pattern: "*.exe"},
			{DisplayName: "所有文件", Pattern: "*.*"},
		},
	})
}

// PickDirectory opens a folder chooser and returns the selected path.
func (a *App) PickDirectory() (string, error) {
	return wailsruntime.OpenDirectoryDialog(a.ctx, wailsruntime.OpenDialogOptions{Title: "选择目录"})
}

// ToolInfo returns the cached -version output for both tools.
func (a *App) ToolInfo() (media.ToolInfo, media.ToolInfo) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.ffmpegTI, a.ffprobeTI
}

// DetectEncoders asks the resolved ffmpeg which encoders it actually ships with.
// Hardware encoders are build dependent, so the template editor should not offer
// NVENC / QSV / AMF blindly.
func (a *App) DetectEncoders() ([]EncoderSupport, error) {
	a.mu.RLock()
	ff := a.binaries.FFmpeg
	cfgFF, cfgFP := a.settings.FFmpegPath, a.settings.FFprobePath
	a.mu.RUnlock()

	if ff == "" {
		ff = media.Resolve(cfgFF, cfgFP).FFmpeg
	}
	if ff == "" {
		return nil, fmt.Errorf("未找到 ffmpeg，请先在「二进制与路径」中指定")
	}

	have, err := media.Encoders(ff)
	if err != nil {
		return nil, err
	}
	out := make([]EncoderSupport, 0, len(videoEncoderCatalog))
	for _, e := range videoEncoderCatalog {
		out = append(out, EncoderSupport{Name: e.Value, Label: e.Label, OK: have[e.Value]})
	}
	return out, nil
}

// ---------------------------------------------------------------------------
// Inputs & queue
// ---------------------------------------------------------------------------

// AddResult reports what happened when inputs were queued.
type AddResult struct {
	Added  int      `json:"added"`
	Errors []string `json:"errors"`
}

// The file-type filter both choosers offer. Defined once because a picker that
// hides a format the queue happily accepts reads as "this format is unsupported".
const (
	mediaFilterAll   = "*.mp4;*.mkv;*.mov;*.avi;*.wmv;*.flv;*.webm;*.m4v;*.mpg;*.mpeg;*.ts;*.m2ts;*.mts;*.vob;*.rmvb;*.3gp;*.ogv;*.mxf;*.mp3;*.m4a;*.aac;*.flac;*.wav;*.ogg;*.opus;*.wma;*.ac3;*.dts;*.aiff;*.ape;*.mka"
	mediaFilterVideo = "*.mp4;*.mkv;*.mov;*.avi;*.wmv;*.flv;*.webm;*.m4v;*.ts;*.mts;*.mpg;*.mpeg;*.vob;*.rmvb;*.3gp;*.ogv"
	mediaFilterAudio = "*.mp3;*.m4a;*.aac;*.flac;*.wav;*.ogg;*.opus;*.wma;*.ac3;*.aiff;*.ape"
)

// AddFilesDialog lets the user multi-select files.
func (a *App) AddFilesDialog(recursive bool) (AddResult, error) {
	paths, err := wailsruntime.OpenMultipleFilesDialog(a.ctx, wailsruntime.OpenDialogOptions{
		Title: "选择媒体文件",
		Filters: []wailsruntime.FileFilter{
			{DisplayName: "媒体文件", Pattern: mediaFilterAll},
			{DisplayName: "视频", Pattern: mediaFilterVideo},
			{DisplayName: "音频", Pattern: mediaFilterAudio},
			{DisplayName: "所有文件", Pattern: "*.*"},
		},
	})
	if err != nil || len(paths) == 0 {
		return AddResult{}, err
	}
	return a.enqueue(paths, false, recursive), nil
}

// PickFile opens a file chooser and returns the chosen path, without queueing it.
//
// AddFilesDialog also picks, but its whole job is to put what it picked into the
// queue. The template editor's 路径测试 wants a path to look at -- reaching for the
// queueing dialog there would add a job as a side effect of asking a question.
func (a *App) PickFile() (string, error) {
	return wailsruntime.OpenFileDialog(a.ctx, wailsruntime.OpenDialogOptions{
		Title: "选择输入文件",
		Filters: []wailsruntime.FileFilter{
			{DisplayName: "媒体文件", Pattern: mediaFilterAll},
			{DisplayName: "所有文件", Pattern: "*.*"},
		},
	})
}

// AddFolderDialog lets the user pick a directory and queues what is in it.
//
// 它**不**弹过滤面板：规则已经设好了（「过滤」页里那套当前生效的方案），添加的时候
// 只按它收。想改规则就去改那一个地方，而不是每加一次问一遍 —— 一批目录要连着加好几次
// 的时候，每次都要重新填一遍同样的条件。
func (a *App) AddFolderDialog(recursive bool) (AddResult, error) {
	path, err := wailsruntime.OpenDirectoryDialog(a.ctx, wailsruntime.OpenDialogOptions{Title: "选择目录"})
	if err != nil || path == "" {
		return AddResult{}, err
	}
	return a.enqueue([]string{path}, true, recursive), nil
}

// AddPaths queues explicit paths (used by drag & drop).
func (a *App) AddPaths(paths []string, recursive bool) AddResult {
	return a.enqueue(paths, false, recursive)
}

// enqueue is the only way into the queue. Every caller comes through here so that
// "what happens when a file or a folder is added" has exactly one answer -- the
// filter rules included.
func (a *App) enqueue(paths []string, forceDir, recursive bool) AddResult {
	// 当前生效的那一套方案，两条规则一起带上：目录规则决定"这个文件夹里收哪几个子
	// 目录"，文件规则决定"收进来的文件里再要哪几个"。开关是不是开着由 `store.NameRules`
	// 自己带（`Active()`），这里不替它判断 —— 判断写两遍，就会有一遍忘了看开关。
	profile := a.effectiveFilter()

	items := make([]engine.InputItem, 0, len(paths))
	for _, p := range paths {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		isDir := forceDir
		if st, err := os.Stat(p); err == nil {
			isDir = st.IsDir()
		}
		item := engine.InputItem{
			Path: p, IsDir: isDir, Recursive: recursive,
			Dirs: profile.Dirs.NameRules, Files: profile.Files,
		}
		// 「含子目录」跟着这套方案走，和规则是不是开着无关：它管的是"这个文件夹往下
		// 收多深"，而过滤整个关掉时同样要看它（那时收的就是"这一层"或"整棵子树"）。
		if isDir {
			item.Recursive = profile.Dirs.Recursive(recursive)
		}
		items = append(items, item)
	}

	tplID, tplName := a.currentTemplate()
	added, errs := a.runner.AddInputs(items, tplID, tplName)
	return a.finishAdd(added, errs)
}

// ---------------------------------------------------------------------------
// 过滤方案
// ---------------------------------------------------------------------------

// FilterState is everything the 「过滤」页 and the task toolbar need to paint
// themselves, in one shape.
//
// 三样东西必须一起交出去：有哪些方案、**此刻真正会生效的那一套**、以及此刻真正生效的
// 规则本身。少最后一样，前端就得自己在列表里按名字再找一遍 —— 而"名字找不到时退回第一
// 套"这条规则就有了第二份实现，界面上的"当前"和引擎跑的迟早会对不上。
type FilterState struct {
	// Profiles 是全部方案，任务页那个下拉直接列它。
	Profiles []store.FilterProfile `json:"profiles"`
	// Active 是**此刻生效**那套的名字。它写进设置文件：任务页上选的就是在用的，
	// 没有"默认那套 / 本次这套"两份 —— 界面上分不出来的东西，模型里也不该有两份。
	Active string `json:"active"`
	// Off 是任务页上那个「不使用过滤」。只在内存里（见 App.filterOff）。
	Off bool `json:"off"`
	// Profile 是当前生效的规则本身；Off 时给**零值**那一套（两组都没开 = 不过滤），
	// 而不是"名字指向的那一套"—— 界面上正显示着「不使用过滤」，规则却还在生效，
	// 是最难解释的一种不一致。
	Profile store.FilterProfile `json:"profile"`
}

// FilterState returns the whole filter state.
func (a *App) FilterState() FilterState {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.filterStateLocked()
}

func (a *App) filterStateLocked() FilterState {
	profiles := append([]store.FilterProfile(nil), a.settings.FilterProfiles...)
	if len(profiles) == 0 {
		// 正常不会走到：Normalize 保证至少有一套。空列表会让任务页那个下拉没有任何
		// 选项可选，所以这里给一份空的兜住，而不是让界面上出现一排空控件。
		profiles = []store.FilterProfile{{Name: store.DefaultFilterName}}
	}
	profile := store.PickProfile(a.settings.ActiveFilter, profiles)
	if a.filterOff {
		profile = store.FilterProfile{}
	}
	return FilterState{
		Profiles: profiles,
		Active:   a.settings.ActiveFilter,
		Off:      a.filterOff,
		Profile:  profile,
	}
}

// effectiveFilter is the profile in force right now, for the queue to read.
//
// 「不使用过滤」在这里给**零值**方案：两组规则都没开，`NameRules.Take` 因此一律放行 ——
// 判据只有这一个函数，禁用不需要另开一条代码路径（另开一条，就会有"关掉开关"和
// "选中禁用"两种说法，而它们必须永远等价）。
func (a *App) effectiveFilter() store.FilterProfile {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.filterOff {
		return store.FilterProfile{}
	}
	return store.PickProfile(a.settings.ActiveFilter, a.settings.FilterProfiles)
}

// SaveFilterProfile stores one profile, overwriting the one it is named after,
// and hands back the whole state.
//
// 整表交回给前端，是因为下拉里要立刻反映出"多了这一项"或者"这一项被覆盖了"：让前端
// 自己猜列表变成什么样，等于把同一个排序/去重规则写两遍。
//
// `oldName` 是这套方案**改名前**叫什么（新建时是空串）。方案没有 id，名字就是它的标识，
// 所以"改名"必须把旧名字一起说出来 —— 只给新名字的话，改名会变成"多一套、旧的那套还
// 在"，而用户以为自己只是改了个拼错的字。
func (a *App) SaveFilterProfile(oldName string, p store.FilterProfile) (FilterState, error) {
	p.Name = strings.TrimSpace(p.Name)
	if p.Name == "" {
		return FilterState{}, fmt.Errorf("方案要有名字")
	}
	p.Normalize()

	a.mu.Lock()
	a.settings.FilterProfiles = putProfile(a.settings.FilterProfiles, oldName, p)
	// 正在用的那套被改名了（或者只是大小写变了），设置里记的名字跟着走：否则它指向一个
	// 已经不存在的老名字，下一次取生效方案时会静默退回第一套 —— 而用户手里什么都没换。
	if a.settings.ActiveFilter != "" &&
		(strings.EqualFold(a.settings.ActiveFilter, oldName) || strings.EqualFold(a.settings.ActiveFilter, p.Name)) {
		a.settings.ActiveFilter = p.Name
	}
	st := a.filterStateLocked()
	s := a.settings
	a.mu.Unlock()

	if err := store.SaveSettings(s); err != nil {
		return st, err
	}
	return st, nil
}

// DeleteFilterProfile removes the profile with this name and hands back the state.
//
// 删掉的是一整套规则。正在用的那套被删掉时，`Normalize` 会把它拉回剩下里的第一套 ——
// 留着"当前指向一套不存在的方案"这种状态，只会让下一次添加的行为无法解释。
func (a *App) DeleteFilterProfile(name string) (FilterState, error) {
	return a.DeleteFilterProfiles([]string{name})
}

// DeleteFilterProfiles removes every named profile and hands back the state.
//
// 批量删走的是**同一个出口**（单删只是 names 只有一个时的样子）。删完之后至少要留一套，
// 否则任务页那个下拉是空的；正在用的那套被删掉时落到剩下里的第一套。
func (a *App) DeleteFilterProfiles(names []string) (FilterState, error) {
	want := map[string]bool{}
	for _, n := range names {
		if k := strings.ToLower(strings.TrimSpace(n)); k != "" {
			want[k] = true
		}
	}

	a.mu.Lock()
	list := make([]store.FilterProfile, 0, len(a.settings.FilterProfiles))
	for _, p := range a.settings.FilterProfiles {
		if want[strings.ToLower(p.Name)] {
			continue
		}
		list = append(list, p)
	}
	a.settings.FilterProfiles = list
	// 删完之后至少要留一套，否则任务页那个下拉是空的。名字沿用第一套。
	if len(a.settings.FilterProfiles) == 0 {
		a.settings.FilterProfiles = []store.FilterProfile{{Name: store.DefaultFilterName}}
	}
	if want[strings.ToLower(a.settings.ActiveFilter)] {
		a.settings.ActiveFilter = a.settings.FilterProfiles[0].Name
	}
	a.settings.Normalize()
	st := a.filterStateLocked()
	s := a.settings
	a.mu.Unlock()

	if err := store.SaveSettings(s); err != nil {
		return st, err
	}
	return st, nil
}

// SetActiveFilter picks the profile the queue uses, and persists that.
//
// 它写设置文件：任务页上那个下拉是"我在用哪套"，没有第二份"默认那套"和它分庭抗礼 ——
// 界面上分不出来的东西，模型里就不该有两份。所以关掉程序再打开，回到的是上次选的那套。
//
// `off` 是那颗「不使用过滤」：它**不写**设置文件（`filterOff` 只在内存里）。理由是它
// 说的不是"这套规则怎么用"，而是"这一次我不要过滤"；落盘的话，忘了关回来就得每次
// 手动切回去，而"忘了"正是这类开关最常见的用法。
//
// 名字对不上任何一套时按"没选"处理：记着一个不存在的名字，会让下拉显示的和引擎跑的
// 看起来在说两件互相矛盾的事。
func (a *App) SetActiveFilter(name string, off bool) (FilterState, error) {
	a.mu.Lock()
	a.filterOff = off
	if !off {
		a.settings.ActiveFilter = store.PickProfile(name, a.settings.FilterProfiles).Name
	}
	st := a.filterStateLocked()
	s := a.settings
	a.mu.Unlock()

	if !off {
		if err := store.SaveSettings(s); err != nil {
			return st, err
		}
	}
	return st, nil
}

// putProfile 把一套方案放进方案表里，位置只有一个答案：
//
//   - 同名的那一套被换掉（大小写不敏感：下拉里 "CACHE" 和 "cache" 是同一个名字）。
//   - 改名（`oldName` 非空）：**旧名字那一格**就是它的新家，位置不动。一套方案在列表里
//     跳到末尾，用户会以为自己又建了一套，而刚刚那套不见了。
//   - 都没有：追加到末尾，已有顺序不动。
//
// 名字是方案唯一的标识（没有 id），所以"改的是哪一套"和"新的叫什么"必须在同一次调用里
// 说清 —— 拆成"先删旧的、再存新的"两步的话，第二步失败时旧的那套已经没了。
// 改成一个已经存在的名字时两边合成一套，留下的还是旧名字那一格：表里出现两行一模一样
// 的名字，用户就再也分不清选的是哪一行。
func putProfile(list []store.FilterProfile, oldName string, p store.FilterProfile) []store.FilterProfile {
	from := strings.ToLower(strings.TrimSpace(oldName))
	key := strings.ToLower(p.Name)
	if from == key {
		from = ""
	}

	out := make([]store.FilterProfile, 0, len(list)+1)
	placed := false
	for _, old := range list {
		k := strings.ToLower(old.Name)
		if k == key || (from != "" && k == from) {
			if !placed {
				out = append(out, p)
				placed = true
			}
			continue
		}
		out = append(out, old)
	}
	if !placed {
		out = append(out, p)
	}
	return out
}

// finishAdd is what every path into the queue ends with: preroll the probes when
// something actually landed, and report the same shape of result.
func (a *App) finishAdd(added int, errs []string) AddResult {
	if added > 0 {
		a.runner.Preprobe()
	}
	return AddResult{Added: added, Errors: errs}
}

// currentTemplate picks the template a newly added file should be bound to.
//
// A job must be bound to a template that can actually process a file. The global
// template is pinned first in the list but holds defaults only, so picking "the
// first one" as a fallback would hand every newly added file a template that
// cannot run -- and the queue would then claim it was bound to 「全局模板」 while
// the toolbar showed something else entirely.
func (a *App) currentTemplate() (string, string) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	tplID := a.settings.LastTemplateID
	for _, t := range a.templates {
		if t.ID == tplID && !t.Global {
			return t.ID, t.Name
		}
	}
	for _, t := range a.templates {
		if !t.Global {
			return t.ID, t.Name
		}
	}
	return "", ""
}

// PreviewFolderScan is the folder filter panel's dry run: which directories the
// conditions pick out and how many files each brings. Nothing is queued.
//
// 这里兜一层 recover，是因为它遍历的是**用户随手挑的任意目录**，而且是打字时不停
// 重跑的那一个（每改一个字都来一次）。遍历里任何一种意外——路径长到 Windows 都
// 不认、权限在中途变了、盘符在走的过程中被拔了——在面板上只该是一句"预览失败"，
// 不该把整个进程带走：Go 的 panic 在 goroutine 里没人接就是进程退出，而 GUI 程序
// 没有控制台，用户看到的只有"点了一下，程序没了"。真正的崩溃原因（跟在目录联接
// 后面无限递归）已经在 engine 的遍历里修掉，这层是留给下一个的。
func (a *App) PreviewFolderScan(scan engine.FolderScan) (pv engine.FolderPreview, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("扫描这个目录时出错: %v", r)
		}
	}()
	pv, _ = engine.ScanFolder(scan)
	return pv, nil
}

// Jobs returns the queue snapshot.
func (a *App) Jobs() []engine.Job { return a.runner.Jobs() }

// Stats returns queue counters.
func (a *App) Stats() engine.Stats { return a.runner.Stats() }

// SetJobTemplate re-points a single queued job at another template.
func (a *App) SetJobTemplate(jobID, templateID string) error {
	t, ok := a.templateByID(templateID)
	if !ok {
		return fmt.Errorf("模板不存在")
	}
	jobs := a.runner.Jobs()
	for _, j := range jobs {
		if j.ID == jobID {
			return a.runner.UpdateJobTemplate(jobID, t.ID, t.Name)
		}
	}
	return fmt.Errorf("任务不存在")
}

// SetAllTemplates re-points the whole queue at another template and reports how
// many rows moved. Finished rows are re-queued, hence the second counter.
func (a *App) SetAllTemplates(templateID string) engine.ApplyResult {
	t, ok := a.templateByID(templateID)
	if !ok {
		return engine.ApplyResult{}
	}
	return a.runner.UpdateAllTemplates(t.ID, t.Name)
}

// StartQueue begins processing.
func (a *App) StartQueue() {
	a.runner.Start()
	a.emitState()
}

// PauseQueue pauses the queue.
func (a *App) PauseQueue() { a.runner.Pause() }

// ResumeQueue continues the queue.
func (a *App) ResumeQueue() { a.runner.Resume() }

// TogglePause flips pause and returns the new state.
func (a *App) TogglePause() bool { return a.runner.TogglePause() }

// StopQueue cancels running jobs and unqueues pending ones.
func (a *App) StopQueue() { a.runner.CancelAll() }

// RemoveJob drops one job.
func (a *App) RemoveJob(id string) error { return a.runner.RemoveJob(id) }

// RemoveJobs drops every job in ids and returns how many were actually removed.
func (a *App) RemoveJobs(ids []string) int { return a.runner.RemoveJobs(ids) }

// RemoveFinished clears every completed job.
func (a *App) RemoveFinished() int { return a.runner.ClearFinished() }

// DeleteOutput deletes the file one job produced, leaving the row in place.
func (a *App) DeleteOutput(id string) engine.DeleteResult { return a.runner.DeleteOutput(id) }

// DeleteOutputs deletes the files several jobs produced, leaving the rows in place.
func (a *App) DeleteOutputs(ids []string) engine.DeleteResult { return a.runner.DeleteOutputs(ids) }

// ClearQueue empties the queue.
func (a *App) ClearQueue() int { return a.runner.ClearAll() }

// RetryFailed re-queues failed and cancelled jobs.
func (a *App) RetryFailed() int { return a.runner.ResetFailed() }

// JobLogs returns the buffered log for one job.
func (a *App) JobLogs(id string) []string { return a.runner.Logs(id) }

// ---------------------------------------------------------------------------
// Probing & preview
// ---------------------------------------------------------------------------

// Probe returns the media description of a single file.
func (a *App) Probe(path string) (*media.Info, error) {
	b := a.Binaries()
	if !b.Ready() {
		return nil, fmt.Errorf("未找到 ffprobe，请在设置中指定路径")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	return media.ProbeFile(ctx, b.FFprobe, path)
}

// Variables standing in for the values that are only known per file. The
// template page previews commands with these, so the preview never implies a
// concrete path the user has not chosen yet.
const (
	inputVar  = "{输入}"
	outputVar = "{输出}"
)

// PreviewCommand renders the ffmpeg command a stored template would produce.
//
// Given a real inputPath the whole command resolves, output path included.
// Without one the two runtime values stay as {输入} / {输出}; everything else is
// decided by the template and the global settings, so the preview is still
// exact where it can be.
func (a *App) PreviewCommand(templateID, inputPath string) (*engine.Plan, error) {
	t, ok := a.templateByID(templateID)
	if !ok {
		return nil, fmt.Errorf("模板不存在")
	}
	return a.buildPreview(t, inputPath)
}

// PreviewTemplate renders the command for a template the editor hands over
// wholesale. PreviewCommand looks its template up by id, so it can only show the
// *saved* copy -- an editor that called it while claiming
// "未保存的修改也会体现在这里" was lying. This is the entry point that makes the
// preview follow the draft on screen instead.
func (a *App) PreviewTemplate(t store.Template, inputPath string) (*engine.Plan, error) {
	return a.buildPreview(t, inputPath)
}

// PathCheck is what the template editor's 路径测试 answers with: for one concrete
// input path, the directory each stage would use and the file that would be
// written. `Notices` carries the things the reader should know about the answer
// rather than reasons to refuse it -- the paths are always produced.
type PathCheck struct {
	SrcDir     string   `json:"srcDir"`     // 源文件所在目录
	OutputDir  string   `json:"outputDir"`  // 输出目录
	OutputPath string   `json:"outputPath"` // 输出文件
	Notices    []string `json:"notices,omitempty"`
}

// PreviewPaths resolves one input path against a template the editor hands over,
// so the test describes the draft on screen rather than the saved copy.
//
// It goes through the same two functions the runner uses (engine.OutputRoot and
// engine.ResolveOutput), so the answer cannot drift from what a real job does.
// Nothing is probed and nothing is read: the naming rules only ever need the path
// -- the extension is the one piece of the file that is in its name -- and waiting
// on ffprobe to learn a directory would make a live test impossible.
func (a *App) PreviewPaths(t store.Template, srcPath string) (PathCheck, error) {
	srcPath = strings.TrimSpace(srcPath)
	if srcPath == "" {
		return PathCheck{}, fmt.Errorf("先填一个输入文件")
	}
	// A directory has no file name to build an output name from, and the answer
	// would be a plausible-looking path assembled from the folder's own name.
	// Saying so is more useful than printing it.
	if strings.HasSuffix(srcPath, "/") || strings.HasSuffix(srcPath, `\`) {
		return PathCheck{}, fmt.Errorf("这是一个目录，请填一个文件的完整路径")
	}

	eff := t.Effective(a.GlobalTemplate())
	// A single file has no added folder above it, so its own directory plays that
	// part -- the same rule store.ResolveDestDir applies to 同级目录.
	root := filepath.Dir(srcPath)
	info := &media.Info{
		Path:     srcPath,
		FileName: filepath.Base(srcPath),
		Ext:      strings.TrimPrefix(filepath.Ext(srcPath), "."),
	}
	req := engine.OutputRequest{Info: info, Tpl: eff, SrcRoot: root}

	dir, err := engine.OutputRoot(req)
	if err != nil {
		return PathCheck{}, err
	}
	dest, err := engine.ResolveOutput(req)
	if err != nil {
		return PathCheck{}, err
	}

	check := PathCheck{SrcDir: root, OutputDir: dir, OutputPath: dest}
	if info.Ext == "" {
		check.Notices = append(check.Notices, "这个路径没有扩展名，「输出格式」留空时就定不下扩展名")
	}
	// 两个同级在这个面板里给出同一个答案，而它们本来是不同的规则 —— 不说出来就
	// 像是算错了。「同级目录」量的是**添加的那个目录**，面板里没有这层，只能拿文件
	// 自己所在的目录顶上去：文件真是从它的上级目录添加的时候，真实落点比这里显示的
	// 更靠上一层。哪一种面板都说不准，所以把这句话留给用户自己判断。
	if eff.OutDirSpec.Mode == store.OutputSibling {
		check.Notices = append(check.Notices,
			"「同级目录」按你添加的那个目录算，这里只能拿这个文件自己的目录代替 —— "+
				"它真的从上一级目录添加的话，产物会落在更靠上一层的地方")
	}
	if !fileExists(srcPath) {
		check.Notices = append(check.Notices, "文件当前不存在，这里只按路径推算")
	}
	return check, nil
}

func (a *App) buildPreview(t store.Template, inputPath string) (*engine.Plan, error) {
	s := a.Settings()
	b := a.Binaries()
	// Resolve against the merged template so the preview shows the values that
	// will really be used, not the blanks a follower leaves behind.
	eff := t.Effective(a.GlobalTemplate())

	var info *media.Info
	fromFile := false
	standIn := false
	if strings.TrimSpace(inputPath) != "" && fileExists(inputPath) {
		if p, err := a.Probe(inputPath); err == nil {
			info, fromFile = p, true
		}
	}
	if info == nil {
		// A 4K sample keeps the resolution maths visible; only the path is
		// replaced by a variable.
		info = sampleInfo()
		info.Path = inputVar
		info.FileName = inputVar
		// Never substitute silently. A stand-in produces a perfectly plausible
		// command whose numbers have nothing to do with the real file, which is
		// exactly how one panel comes to disagree with another and neither looks
		// wrong.
		standIn = true
	}

	// The output path goes through ResolveOutput either way. A stand-in used to
	// skip it and print a bare {输出}, which meant the naming template and the
	// output container were both invisible in the preview: "{name}" showed up with
	// no extension at all, and the only way to see the real result was to spell
	// the extension out in the pattern. The file name is real either way; only
	// the directory depends on the actual input, so that is what gets folded back
	// into a placeholder.
	out := outputVar
	if dest, err := engine.ResolveOutput(engine.OutputRequest{
		Info: info, Tpl: eff, SrcRoot: filepath.Dir(info.Path),
	}); err == nil {
		out = dest
	}
	if !fromFile {
		out = previewOutputName(out)
	}

	plan, err := engine.BuildPlan(engine.PlanInput{
		Info: info, Tpl: eff, Settings: s, Binaries: b, Output: out,
		LogLevel: templateLogLevel(eff), Threads: templateThreads(eff),
	})
	if err != nil {
		return nil, err
	}
	if standIn {
		plan.Warnings = append(plan.Warnings,
			"读不到源文件（可能已被移动或删除），这条命令按 3840×2160 的示例计算，分辨率相关参数不是真实值")
	}
	return plan, nil
}

// previewOutputName folds a stand-in's resolved output path back into something
// readable: the file name is exactly what a real run would produce (naming template
// applied, output container's extension appended), and the directory -- which only
// exists because a sample path was made up -- goes back to a placeholder.
func previewOutputName(dest string) string {
	name := filepath.Base(dest)
	if strings.TrimSpace(name) == "" || name == "." {
		return outputVar
	}
	return outputVar + string(filepath.Separator) + name
}

// templateLogLevel reads the merged -loglevel, tolerating a follower whose
// section the caller did not fill in.
func templateLogLevel(t store.Template) string {
	if t.Perf == nil {
		return ""
	}
	return t.Perf.LogLevel
}

// templateThreads is templateLogLevel's counterpart for -threads.
func templateThreads(t store.Template) int {
	if t.Perf == nil {
		return 0
	}
	return t.Perf.Threads
}

func sampleInfo() *media.Info {
	return sampleInfoFor("sample-4k.mp4")
}

func sampleInfoFor(path string) *media.Info {
	ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(path)), ".")
	if ext == "" {
		ext = "mp4"
	}
	return &media.Info{
		Path: path, FileName: filepath.Base(path), Ext: ext,
		Container: "mov,mp4,m4a,3gp,3g2,mj2", ContainerLong: "QuickTime / MOV",
		Size: 2 << 30, Duration: 180, BitRate: 95_000_000,
		Width: 3840, Height: 2160, DisplayWidth: 3840, DisplayHeight: 2160,
		FPS: 30, PixFmt: "yuv420p", VideoCodec: "hevc", AudioCodec: "aac",
		VideoN: 1, AudioN: 1,
		Video: &media.Stream{Index: 0, Type: "video", Codec: "hevc", Width: 3840, Height: 2160, FPS: 30, PixFmt: "yuv420p"},
		Audio: &media.Stream{Index: 1, Type: "audio", Codec: "aac", SampleRate: 48000, Channels: 2},
	}
}

func fileExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}

// ---------------------------------------------------------------------------
// History
// ---------------------------------------------------------------------------

// HistoryQuery filters the history list.
type HistoryQuery struct {
	Keyword string `json:"keyword"`
	Status  string `json:"status"`
	// From / To are inclusive local dates (YYYY-MM-DD) matched against the moment a
	// record is dated by -- the same one the list's 完成时间 column shows.
	From string `json:"from"`
	To   string `json:"to"`
	// Sort is "" / "newest" for 最新在前, "oldest" for 最早在前.
	Sort   string `json:"sort"`
	Offset int    `json:"offset"`
	Limit  int    `json:"limit"`
}

// HistoryPage is a page of history records.
type HistoryPage struct {
	Total int            `json:"total"`
	Items []store.Record `json:"items"`
}

// recordTime is the moment a record is dated by: when it finished, or when it
// started for the ones that never finished (a rejected file, a cancellation).
// It is exactly what the 完成时间 column shows, so filtering by date can never
// disagree with the timestamps the list puts on screen.
func recordTime(r store.Record) time.Time {
	if !r.EndedAt.IsZero() {
		return r.EndedAt
	}
	return r.StartedAt
}

// parseDay reads a YYYY-MM-DD date in the local zone. An unusable string simply
// has no bound: the value comes from a date picker that can also be cleared.
func parseDay(s string) (time.Time, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, false
	}
	t, err := time.ParseInLocation("2006-01-02", s, time.Local)
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}

// filterHistory returns the records matching q, newest first unless q.Sort asks
// otherwise. Pure, so the rules above can be pinned without an App.
func filterHistory(list []store.Record, q HistoryQuery) []store.Record {
	kw := strings.ToLower(strings.TrimSpace(q.Keyword))
	from, hasFrom := parseDay(q.From)
	to, hasTo := parseDay(q.To)
	if hasTo {
		// To is inclusive: something finished at 23:50 on the end date belongs to
		// that date, so the bound is the start of the following one.
		to = to.AddDate(0, 0, 1)
	}

	out := make([]store.Record, 0, len(list))
	for _, r := range list {
		if q.Status != "" && q.Status != "all" && r.Status != q.Status {
			continue
		}
		if hasFrom || hasTo {
			t := recordTime(r)
			if hasFrom && t.Before(from) {
				continue
			}
			if hasTo && !t.Before(to) {
				continue
			}
		}
		if kw != "" {
			hay := strings.ToLower(r.Input + " " + r.Output + " " + r.TemplateName + " " + r.Error + " " + r.Note)
			if !strings.Contains(hay, kw) {
				continue
			}
		}
		out = append(out, r)
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := recordTime(out[i]), recordTime(out[j])
		if q.Sort == "oldest" {
			return a.Before(b)
		}
		return a.After(b)
	})
	return out
}

// History returns a filtered page of records.
func (a *App) History(q HistoryQuery) HistoryPage {
	a.histMu.Lock()
	list := append([]store.Record(nil), a.history...)
	a.histMu.Unlock()

	filtered := filterHistory(list, q)

	page := HistoryPage{Total: len(filtered)}
	from := q.Offset
	if from < 0 {
		from = 0
	}
	if from > len(filtered) {
		from = len(filtered)
	}
	limit := q.Limit
	if limit <= 0 {
		limit = 500
	}
	to := from + limit
	if to > len(filtered) {
		to = len(filtered)
	}
	page.Items = filtered[from:to]
	return page
}

// ClearHistory wipes every record.
func (a *App) ClearHistory() error {
	a.histMu.Lock()
	a.history = nil
	a.histDirty = true
	a.histMu.Unlock()
	return a.flushHistory()
}

// DeleteRecords drops the given records and reports how many were actually there.
//
// A count rather than an error when nothing matched: the ids come from the list
// the user was looking at, and a row that moved out of the current filter in the
// meantime must not fail the whole batch.
func (a *App) DeleteRecords(ids []string) (int, error) {
	want := make(map[string]bool, len(ids))
	for _, id := range ids {
		if id != "" {
			want[id] = true
		}
	}
	if len(want) == 0 {
		return 0, nil
	}

	a.histMu.Lock()
	kept := make([]store.Record, 0, len(a.history))
	removed := 0
	for _, r := range a.history {
		if want[r.ID] {
			removed++
			continue
		}
		kept = append(kept, r)
	}
	if removed > 0 {
		a.history = kept
		a.histDirty = true
	}
	a.histMu.Unlock()

	if removed == 0 {
		return 0, nil
	}
	return removed, a.flushHistory()
}

// ExportHistoryCSV writes the (optionally filtered) records to a CSV file.
func (a *App) ExportHistoryCSV(query HistoryQuery) (string, error) {
	// Not a page: the file gets every record the current filter selects. Going
	// through History here meant quietly exporting at most its 500-row default.
	a.histMu.Lock()
	list := append([]store.Record(nil), a.history...)
	a.histMu.Unlock()
	records := filterHistory(list, query)
	if len(records) == 0 {
		return "", fmt.Errorf("没有可导出的记录")
	}
	defaultName := fmt.Sprintf("ffmpeg-gui-处理记录-%s.csv", time.Now().Format("20060102-150405"))
	path, err := wailsruntime.SaveFileDialog(a.ctx, wailsruntime.SaveDialogOptions{
		Title:           "导出处理记录",
		DefaultFilename: defaultName,
		Filters:         []wailsruntime.FileFilter{{DisplayName: "CSV", Pattern: "*.csv"}},
	})
	if err != nil || path == "" {
		return "", err
	}
	if err := store.ExportCSV(path, records); err != nil {
		return "", err
	}
	return path, nil
}

func (a *App) appendRecord(r store.Record) {
	a.histMu.Lock()
	// newest first
	a.history = append([]store.Record{r}, a.history...)
	if len(a.history) > 20000 {
		a.history = a.history[:20000]
	}
	a.histDirty = true
	a.histMu.Unlock()

	if r.Output != "" {
		a.mu.Lock()
		a.lastOutputDir = filepath.Dir(r.Output)
		a.mu.Unlock()
	}
}

func (a *App) historySaver() {
	t := time.NewTicker(3 * time.Second)
	defer t.Stop()
	for range t.C {
		a.flushHistory()
	}
}

func (a *App) flushHistory() error {
	a.histMu.Lock()
	if !a.histDirty {
		a.histMu.Unlock()
		return nil
	}
	list := append([]store.Record(nil), a.history...)
	a.histDirty = false
	a.histMu.Unlock()
	return store.SaveHistory(list)
}

// ---------------------------------------------------------------------------
// Shell helpers
// ---------------------------------------------------------------------------

// OpenPath opens a file or folder with the OS handler.
func (a *App) OpenPath(path string) error { return sysx.Open(path) }

// RevealPath selects a file in the file manager.
func (a *App) RevealPath(path string) error { return sysx.Reveal(path) }

// LocatedFile reports which path a 定位 request ended up opening.
type LocatedFile struct {
	Path string `json:"path"`
	// Moved is true when the file was no longer at the path the list shows and the
	// second location had to be used.
	Moved bool `json:"moved"`
}

// locateCandidates is the list of places to look for a file, nearest first and
// without repeats. Pure, so the rule can be pinned without opening Explorer.
func locateCandidates(primary, fallback string) []string {
	first := strings.TrimSpace(primary)
	second := strings.TrimSpace(fallback)
	if second == first {
		second = ""
	}
	out := make([]string, 0, 2)
	if first != "" {
		out = append(out, first)
	}
	if second != "" {
		out = append(out, second)
	}
	return out
}

// Locate reveals a file in the file manager, falling back to the place it was
// moved to.
//
// `fallback` is the source's other home: the 「已处理过的文件」 rule moved it there,
// so the path the list shows is empty on disk. Existence is decided here instead
// of being left to the shell -- Explorer does not report an error for a path that
// is not there, it quietly opens 文档, which looks exactly like a successful locate.
func (a *App) Locate(path, fallback string) (LocatedFile, error) {
	tried := locateCandidates(path, fallback)
	for i, p := range tried {
		if _, err := os.Stat(p); err != nil {
			continue
		}
		if err := sysx.Reveal(p); err != nil {
			return LocatedFile{}, err
		}
		return LocatedFile{Path: p, Moved: i > 0}, nil
	}
	switch len(tried) {
	case 0:
		return LocatedFile{}, fmt.Errorf("没有可定位的路径")
	case 1:
		return LocatedFile{}, fmt.Errorf("文件不在这里了: %s", tried[0])
	default:
		return LocatedFile{}, fmt.Errorf("文件不在 %s，也不在 %s", tried[0], tried[1])
	}
}

// OpenOutputDir opens the folder that will receive finished files.
//
// Only「自定义目录」names a folder that exists before any file does. 原目录 and
// 同级目录 are answers about a *file* — where it sits decides the answer — so there
// is nothing to open, and the last finished job's directory is the closest thing
// to what the user meant.
func (a *App) OpenOutputDir() error {
	a.mu.RLock()
	global := store.GlobalOrDefault(a.templates)
	last := a.lastOutputDir
	a.mu.RUnlock()

	dir := ""
	if global.OutDirSpec.Mode == store.OutputCustom {
		dir = strings.TrimSpace(global.OutDirSpec.Dir)
	}
	if dir == "" {
		dir = last
	}
	if dir == "" {
		dir = store.DataDir()
	}
	if _, err := os.Stat(dir); err != nil {
		return fmt.Errorf("目录不存在: %s", dir)
	}
	return sysx.Open(dir)
}

// DataDir exposes the data directory to the UI.
func (a *App) DataDir() string { return store.DataDir() }

// ShowDataDir reveals the data directory.
func (a *App) ShowDataDir() error { return sysx.Open(store.DataDir()) }

// AppVersion returns the build version.
func (a *App) AppVersion() string { return AppVersion }

// QuitApp is the frontend's close button. With a frameless window this button IS the
// close button for nearly everyone, so it must honour the same rules as the native
// close: when "close to tray" is on, hide instead of quitting. Routing it straight to
// Quit is why the setting appeared to do nothing -- Quit sets `quitting` first, which
// makes beforeClose stand down, and the process died no matter what the switch said.
func (a *App) QuitApp() {
	if a.ctx != nil && a.beforeClose(a.ctx) {
		return
	}
	a.Quit()
}

// ShowWindow brings the main window back from the tray.
func (a *App) ShowWindow() {
	if a.ctx != nil {
		wailsruntime.WindowShow(a.ctx)
		wailsruntime.WindowUnminimise(a.ctx)
	}
}

// ---------------------------------------------------------------------------
// Bootstrap payload
// ---------------------------------------------------------------------------

// Option is a labelled choice for the UI selects.
type Option struct {
	Value string `json:"value"`
	Label string `json:"label"`
}

// videoEncoderCatalog is the single source of truth for the video encoder list:
// it feeds the template editor dropdown *and* the capability probe, so the two
// can never drift apart.
var videoEncoderCatalog = []Option{
	{"libx264", "H.264 · libx264（推荐）"},
	{"libx265", "H.265 · libx265（更小）"},
	{"libsvtav1", "AV1 · SVT-AV1（快）"},
	{"libaom-av1", "AV1 · libaom（慢）"},
	{"libvpx-vp9", "VP9 · libvpx（WebM）"},
	{"h264_nvenc", "H.264 · NVENC"},
	{"hevc_nvenc", "H.265 · NVENC"},
	{"av1_nvenc", "AV1 · NVENC"},
	{"h264_qsv", "H.264 · QuickSync"},
	{"hevc_qsv", "H.265 · QuickSync"},
	{"av1_qsv", "AV1 · QuickSync"},
	{"h264_amf", "H.264 · AMF"},
	{"hevc_amf", "H.265 · AMF"},
	{"h264_videotoolbox", "H.264 · VideoToolbox"},
	{"hevc_videotoolbox", "H.265 · VideoToolbox"},
	{"mpeg4", "MPEG-4"},
	{"gif", "GIF 动图"},
	{"webp", "WebP 动图"},
}

// EncoderSupport reports whether one encoder of the catalogue is usable.
type EncoderSupport struct {
	Name  string `json:"name"`
	Label string `json:"label"`
	OK    bool   `json:"ok"`
}

// Options holds every enumerated choice the template editor needs.
type Options struct {
	VideoCodecs     []Option `json:"videoCodecs"`
	AudioCodecs     []Option `json:"audioCodecs"`
	Containers      []Option `json:"containers"`
	Presets         []Option `json:"presets"`
	ResizeModes     []Option `json:"resizeModes"`
	ScaleAlgorithms []Option `json:"scaleAlgorithms"`
	LogLevels       []Option `json:"logLevels"`
	RateControls    []Option `json:"rateControls"`
	PadColors       []Option `json:"padColors"`
	// DestModes is the 「输出方式」 list: 原目录 / 自定义目录 / 同级目录 / 同级顶层目录.
	// It is the same choices in all four places that write a file somewhere, so it is
	// defined once here rather than once per editor.
	//
	// DefaultOutputSuffix is the folder-name suffix the two sibling modes fall back to
	// when the editor's 前缀 / 后缀 are both blank. The frontend shows it as the
	// suffix field's placeholder, so what the field says it will do and what the
	// engine does are the same string.
	DestModes           []Option `json:"destModes"`
	DefaultOutputSuffix string   `json:"defaultOutputSuffix"`
	// Where a file goes is a mode plus its fields, not a pick from a list of
	// directories -- so there is no destination list here. What the three sections
	// that relocate a file need from this struct is their 处理方式 action: 已处理过的
	// 文件, 被排除的文件 and the error / warning policies. They all read the same way
	// on purpose -- a rule you learned in one place works in the other two.
	//
	// ProblemActions is FilterActions plus 「仅在结果中标记」, which only makes sense
	// for a problem file: there is nothing to mark about a source you simply skip.
	FilterActions   []Option `json:"filterActions"`
	ProblemActions  []Option `json:"problemActions"`
	ExistingActions []Option `json:"existingActions"`
}

// RuntimeInfo describes the environment.
type RuntimeInfo struct {
	OS       string         `json:"os"`
	Arch     string         `json:"arch"`
	CPUs     int            `json:"cpus"`
	DataDir  string         `json:"dataDir"`
	Binaries media.Binaries `json:"binaries"`
	FFmpeg   media.ToolInfo `json:"ffmpeg"`
	FFprobe  media.ToolInfo `json:"ffprobe"`
	Version  string         `json:"version"`
}

// Bootstrap is the single payload the frontend loads on start-up.
type Bootstrap struct {
	Runtime  RuntimeInfo    `json:"runtime"`
	Settings store.Settings `json:"settings"`
	// Filter 单独一份而不是让前端从 Settings 里挑：`settings.filterProfiles` 里没有
	// "此刻生效的是哪一套"（那还取决于本次运行有没有临时换过），而那正是任务页那个
	// 下拉要显示的东西。
	Filter    FilterState      `json:"filter"`
	Templates []store.Template `json:"templates"`
	Jobs      []engine.Job     `json:"jobs"`
	Stats     engine.Stats     `json:"stats"`
	History   []store.Record   `json:"history"`
	Options   Options          `json:"options"`
}

// Bootstrap returns everything the UI needs for a cold start.
// historySeed is how many records Bootstrap hands over: one page, so the record
// list has something to draw on the way in. It has to match the frontend's default
// page size (ui.js PAGE_SIZES[0]); anything more is thrown away on mount.
const historySeed = 50

func (a *App) Bootstrap() Bootstrap {
	a.mu.RLock()
	s := a.settings
	tpls := append([]store.Template(nil), a.templates...)
	bins := a.binaries
	flt := a.filterStateLocked()
	ff, fp := a.ffmpegTI, a.ffprobeTI
	a.mu.RUnlock()

	a.histMu.Lock()
	hist := append([]store.Record(nil), a.history...)
	a.histMu.Unlock()
	// Only the first page. The record page asks for its own page on mount, so this
	// is just enough to fill the list the first time it is opened -- sending 500
	// records meant serialising 500 media summaries and 500 command lines into the
	// startup reply for rows that were about to be thrown away.
	if len(hist) > historySeed {
		hist = hist[:historySeed]
	}

	return Bootstrap{
		Runtime: RuntimeInfo{
			OS: runtime.GOOS, Arch: runtime.GOARCH, CPUs: runtime.NumCPU(),
			DataDir: store.DataDir(), Binaries: bins,
			FFmpeg: ff, FFprobe: fp, Version: AppVersion,
		},
		Settings:  s,
		Filter:    flt,
		Templates: tpls,
		Jobs:      a.runner.Jobs(),
		Stats:     a.runner.Stats(),
		History:   hist,
		Options:   buildOptions(),
	}
}

// ---------------------------------------------------------------------------
// Events
// ---------------------------------------------------------------------------

// Toast is a transient notification shown in the UI.
type Toast struct {
	Kind    string `json:"kind"`
	Message string `json:"message"`
}

func (a *App) emit(name string, payload any) {
	if a.ctx == nil {
		return
	}
	wailsruntime.EventsEmit(a.ctx, name, payload)
}

func (a *App) emitState() { a.emitEngine(engine.EventQueue, a.runner.Stats()) }

// emitEngine is the engine's event sink.
//
// The tray tooltip is refreshed here, on every queue event, rather than in
// emitState: emitState only ever ran for 开始 and 保存设置, so pausing, stopping,
// clearing or removing rows left the hover text describing a queue that no
// longer existed -- the tooltip simply stopped changing for the rest of the
// session. Every mutation ends in a queue event, so this is the one place that
// cannot miss one.
func (a *App) emitEngine(name string, payload any) {
	if name == engine.EventQueue {
		if st, ok := payload.(engine.Stats); ok {
			a.refreshTrayTooltip(st)
		}
	}
	a.emit(name, payload)
}

// refreshTrayTooltip puts the queue state on the icon.
func (a *App) refreshTrayTooltip(st engine.Stats) {
	if a.tray == nil {
		return
	}
	a.tray.SetTooltip(trayTooltip(st))
}

// trayTooltip renders a queue state as hover text, or "" for "nothing to report",
// which the tray turns back into its idle title.
//
// An empty queue has to fall back rather than keep the last count: "3/9 完成" over
// an icon whose queue was cleared is worse than no information at all, and it is
// exactly what the old code did -- it only ever wrote the tooltip when Total > 0.
func trayTooltip(st engine.Stats) string {
	if st.Total == 0 {
		return ""
	}
	parts := []string{fmt.Sprintf("%d/%d 完成", st.Finished(), st.Total)}
	switch {
	case st.Paused:
		parts = append(parts, "已暂停")
	case !st.Started:
		parts = append(parts, "未开始")
	case st.Running > 0:
		parts = append(parts, fmt.Sprintf("%d 处理中", st.Running))
	}
	return "FFmpeg GUI — " + strings.Join(parts, " · ")
}

func (a *App) emitToastKind(kind, msg string) {
	a.emit(engine.EventToast, Toast{Kind: kind, Message: msg})
}

// ---------------------------------------------------------------------------
// Static option tables
// ---------------------------------------------------------------------------

// DefaultOptionLabel is what every dropdown calls the "not set" choice.
//
// The wording for "the user did not configure this" used to be invented per list:
// "保持原样", "留空 = ffmpeg 默认", "0 = 自动", "默认（由 ffmpeg 决定）"... four
// spellings for one idea, which reads as four different behaviours. One phrase
// now covers all of them, in this vocabulary:
//
//   - dropdowns lead with DefaultOptionLabel
//   - placeholders say "留空 = 默认"
//   - numeric fields say "0 = 保持原样" or "0 = 不限", whichever is true
const DefaultOptionLabel = "默认（由 ffmpeg 决定）"

// DefaultPlaceholder is the placeholder counterpart of DefaultOptionLabel.
const DefaultPlaceholder = "留空 = 默认"

// relocateActions is the 处理方式 list for the three sections that move a file:
// 已处理过的文件, 被排除的文件, and the error / warning policies.
var relocateActions = []Option{
	{"keep", "不处理，留在原处"},
	{"move", "移动到目标目录"},
	{"copy", "复制到目标目录"},
}

func buildOptions() Options {
	return Options{
		// An empty value is a first-class choice, not a missing one: it means "do
		// not pass -c:v / -c:a at all and let ffmpeg pick its default encoder".
		VideoCodecs: append([]Option{{"", DefaultOptionLabel}, {"copy", "复制原编码"}}, videoEncoderCatalog...),
		AudioCodecs: []Option{
			{"", DefaultOptionLabel},
			{"copy", "复制原编码"},
			{"aac", "AAC（通用）"},
			{"libmp3lame", "MP3"},
			{"libopus", "Opus"},
			{"libvorbis", "Vorbis"},
			{"flac", "FLAC 无损"},
			{"alac", "ALAC 无损"},
			{"ac3", "AC-3"},
			{"eac3", "E-AC-3"},
			{"pcm_s16le", "PCM 16-bit"},
		},
		Containers: []Option{
			{"", "沿用源文件的格式"},
			{"mp4", "MP4"},
			{"mkv", "MKV（Matroska）"},
			{"mov", "MOV"},
			{"webm", "WebM"},
			{"avi", "AVI"},
			{"m4v", "M4V"},
			{"ts", "MPEG-TS"},
			{"flv", "FLV"},
			{"m4a", "M4A（仅音频）"},
			{"mp3", "MP3（仅音频）"},
			{"aac", "AAC（仅音频）"},
			{"flac", "FLAC（仅音频）"},
			{"wav", "WAV（仅音频）"},
			{"mka", "MKA（仅音频）"},
			{"ogg", "OGG（仅音频）"},
			{"gif", "GIF"},
		},
		Presets: []Option{
			{"", DefaultOptionLabel},
			{"ultrafast", "ultrafast — 最快，体积最大"},
			{"superfast", "superfast"},
			{"veryfast", "veryfast"},
			{"faster", "faster"},
			{"fast", "fast"},
			{"medium", "medium — 均衡（推荐）"},
			{"slow", "slow"},
			{"slower", "slower"},
			{"veryslow", "veryslow — 最慢，体积最小"},
		},
		ResizeModes: []Option{
			{"keep", "保持原分辨率"},
			{"longedge", "锁定长边（自动识别横竖屏）"},
			{"shortedge", "锁定短边（自动识别横竖屏）"},
			{"exact", "指定宽 × 高"},
			{"fit", "限制在矩形范围内（只缩不放）"},
			{"percent", "按百分比缩放"},
		},
		ScaleAlgorithms: []Option{
			{"", DefaultOptionLabel},
			{"lanczos", "lanczos — 画质最好（推荐）"},
			{"bicubic", "bicubic — 均衡"},
			{"bilinear", "bilinear — 更快"},
			{"spline", "spline"},
			{"area", "area — 缩小专用"},
			{"neighbor", "neighbor — 像素风"},
		},
		LogLevels: []Option{
			{"quiet", "quiet — 不输出任何信息"},
			{"error", "error — 仅错误"},
			{"warning", "warning — 错误与警告（推荐）"},
			{"info", "info — 常规信息"},
			{"verbose", "verbose — 详细信息"},
			{"debug", "debug — 调试信息"},
		},
		RateControls: []Option{
			{"crf", "CRF / 恒定质量（推荐）"},
			{"bitrate", "目标码率"},
			{"qp", "QP / 固定量化"},
		},
		PadColors: []Option{
			{"black", "黑色"},
			{"white", "白色"},
			{"#101014", "深灰"},
		},
		DestModes: []Option{
			// 原目录 is the explicit form of the blank mode; both resolve to "next to
			// the source file", and the blank one is what an unset section carries.
			{store.OutputSame, "原目录"},
			{store.OutputCustom, "自定义目录"},
			// 两个同级只差锚：sibling 量**添加的那个目录**（整棵树的产物汇到一处），
			// siblingTop 量**文件自己所在的目录**（每个子目录各出一个）。
			{store.OutputSibling, "同级目录"},
			{store.OutputSiblingTop, "同级顶层目录"},
		},
		DefaultOutputSuffix: store.DefaultOutputSuffix,

		FilterActions: relocateActions,
		// The 「已处理过的文件」 section reads exactly like the filter's, so it
		// gets the same list rather than a near-copy that can drift.
		ExistingActions: relocateActions,
		ProblemActions: append(append([]Option{}, relocateActions...),
			Option{"mark", "仅在结果中标记"}),
	}
}
