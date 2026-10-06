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
	}, a.emit, a.appendRecord)

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
func (a *App) AddDroppedFiles(paths []string) AddResult {
	res := a.AddPaths(paths, false)
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
			t.UpdatedAt = time.Now().Unix()
			a.templates[i] = t
			found = true
			break
		}
	}
	if !found {
		t.ID = store.NewID()
		t.Builtin = false
		t.UpdatedAt = time.Now().Unix()
		a.templates = append(a.templates, t)
	}
	list := append([]store.Template(nil), a.templates...)
	a.mu.Unlock()

	if err := store.SaveTemplates(list); err != nil {
		return t, err
	}
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
			t.UpdatedAt = time.Now().Unix()
			a.templates[i] = t
			list := append([]store.Template(nil), a.templates...)
			a.mu.Unlock()
			if err := store.SaveTemplates(list); err != nil {
				return t, err
			}
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
	a.mu.Lock()
	if id == store.GlobalTemplateID {
		a.mu.Unlock()
		return fmt.Errorf("「%s」不能删除", store.GlobalTemplateName)
	}
	out := make([]store.Template, 0, len(a.templates))
	for _, t := range a.templates {
		if t.ID == id {
			continue
		}
		out = append(out, t)
	}
	a.templates = out
	list := append([]store.Template(nil), out...)
	a.mu.Unlock()

	if err := store.SaveTemplates(list); err != nil {
		return err
	}
	a.emit("templates:changed", list)
	return nil
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
	src.UpdatedAt = time.Now().Unix()
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

// AddFilesDialog lets the user multi-select files.
func (a *App) AddFilesDialog(recursive bool) (AddResult, error) {
	paths, err := wailsruntime.OpenMultipleFilesDialog(a.ctx, wailsruntime.OpenDialogOptions{
		Title: "选择媒体文件",
		Filters: []wailsruntime.FileFilter{
			{DisplayName: "媒体文件", Pattern: "*.mp4;*.mkv;*.mov;*.avi;*.wmv;*.flv;*.webm;*.m4v;*.mpg;*.mpeg;*.ts;*.m2ts;*.mts;*.vob;*.rmvb;*.3gp;*.ogv;*.mxf;*.mp3;*.m4a;*.aac;*.flac;*.wav;*.ogg;*.opus;*.wma;*.ac3;*.dts;*.aiff;*.ape;*.mka"},
			{DisplayName: "视频", Pattern: "*.mp4;*.mkv;*.mov;*.avi;*.wmv;*.flv;*.webm;*.m4v;*.ts;*.mts;*.mpg;*.mpeg;*.vob;*.rmvb;*.3gp;*.ogv"},
			{DisplayName: "音频", Pattern: "*.mp3;*.m4a;*.aac;*.flac;*.wav;*.ogg;*.opus;*.wma;*.ac3;*.aiff;*.ape"},
			{DisplayName: "所有文件", Pattern: "*.*"},
		},
	})
	if err != nil || len(paths) == 0 {
		return AddResult{}, err
	}
	return a.enqueue(paths, false, recursive), nil
}

// AddFolderDialog lets the user pick a directory.
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

func (a *App) enqueue(paths []string, forceDir, recursive bool) AddResult {
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
		items = append(items, engine.InputItem{Path: p, IsDir: isDir, Recursive: recursive})
	}

	a.mu.RLock()
	// A job must be bound to a template that can actually process a file. The
	// global template is pinned first in the list but holds defaults only, so
	// picking "the first one" as a fallback would hand every newly added file a
	// template that cannot run -- and the queue would then claim it was bound to
	// 「全局模板」 while the toolbar showed something else entirely.
	tplID := a.settings.LastTemplateID
	tplName := ""
	for _, t := range a.templates {
		if t.ID == tplID && !t.Global {
			tplName = t.Name
			break
		}
	}
	if tplName == "" {
		tplID = ""
		for _, t := range a.templates {
			if !t.Global {
				tplID = t.ID
				tplName = t.Name
				break
			}
		}
	}
	a.mu.RUnlock()

	added, errs := a.runner.AddInputs(items, tplID, tplName)
	if added > 0 {
		a.runner.Preprobe()
	}
	return AddResult{Added: added, Errors: errs}
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
	Offset  int    `json:"offset"`
	Limit   int    `json:"limit"`
}

// HistoryPage is a page of history records.
type HistoryPage struct {
	Total int            `json:"total"`
	Items []store.Record `json:"items"`
}

// History returns a filtered page of records (newest first).
func (a *App) History(q HistoryQuery) HistoryPage {
	a.histMu.Lock()
	list := append([]store.Record(nil), a.history...)
	a.histMu.Unlock()

	kw := strings.ToLower(strings.TrimSpace(q.Keyword))
	filtered := make([]store.Record, 0, len(list))
	for _, r := range list {
		if q.Status != "" && q.Status != "all" && r.Status != q.Status {
			continue
		}
		if kw != "" {
			hay := strings.ToLower(r.Input + " " + r.Output + " " + r.TemplateName + " " + r.Error + " " + r.Note)
			if !strings.Contains(hay, kw) {
				continue
			}
		}
		filtered = append(filtered, r)
	}
	sort.SliceStable(filtered, func(i, j int) bool {
		return filtered[i].StartedAt.After(filtered[j].StartedAt)
	})

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

// ExportHistoryCSV writes the (optionally filtered) records to a CSV file.
func (a *App) ExportHistoryCSV(query HistoryQuery) (string, error) {
	page := a.History(HistoryQuery{Keyword: query.Keyword, Status: query.Status, Limit: 0})
	if len(page.Items) == 0 {
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
	if err := store.ExportCSV(path, page.Items); err != nil {
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

// OpenOutputDir opens the folder that will receive finished files. The rule now
// lives on the global template, so a "same directory" rule has no fixed target and
// falls back to wherever the last finished file actually went.
func (a *App) OpenOutputDir() error {
	a.mu.RLock()
	global := store.GlobalOrDefault(a.templates)
	last := a.lastOutputDir
	a.mu.RUnlock()

	dir := strings.TrimSpace(global.OutDir)
	if global.OutMode == store.OutputSame || global.OutMode == store.OutputSibling || global.OutMode == "" {
		if last != "" {
			dir = last
		}
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
	OutputModes     []Option `json:"outputModes"`
	// FilterActions is the shared 处理方式 list for the three sections that move a
	// file somewhere: 已处理过的源文件, 被排除的文件 and the error / warning
	// policies. They all read the same way on purpose -- a rule you learned in one
	// place works in the other two.
	//
	// ProblemActions is FilterActions plus 「仅在结果中标记」, which only makes sense
	// for a problem file: there is nothing to mark about a source you simply skip.
	FilterActions   []Option `json:"filterActions"`
	ProblemActions  []Option `json:"problemActions"`
	ExistingActions []Option `json:"existingActions"`
	// DestModes is the four-way output rule every stage gets. None of these lists
	// carries a "follow the global template" entry: the section's 与全局不同 switch
	// is the single way to say that, and offering both let a panel claim to follow
	// while the command used something else.
	DestModes []Option `json:"destModes"`
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
	Runtime   RuntimeInfo      `json:"runtime"`
	Settings  store.Settings   `json:"settings"`
	Templates []store.Template `json:"templates"`
	Jobs      []engine.Job     `json:"jobs"`
	Stats     engine.Stats     `json:"stats"`
	History   []store.Record   `json:"history"`
	Options   Options          `json:"options"`
}

// Bootstrap returns everything the UI needs for a cold start.
func (a *App) Bootstrap() Bootstrap {
	a.mu.RLock()
	s := a.settings
	tpls := append([]store.Template(nil), a.templates...)
	bins := a.binaries
	ff, fp := a.ffmpegTI, a.ffprobeTI
	a.mu.RUnlock()

	a.histMu.Lock()
	hist := append([]store.Record(nil), a.history...)
	a.histMu.Unlock()
	if len(hist) > 500 {
		hist = hist[:500]
	}

	return Bootstrap{
		Runtime: RuntimeInfo{
			OS: runtime.GOOS, Arch: runtime.GOARCH, CPUs: runtime.NumCPU(),
			DataDir: store.DataDir(), Binaries: bins,
			FFmpeg: ff, FFprobe: fp, Version: AppVersion,
		},
		Settings:  s,
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

func (a *App) emitState() {
	a.emit(engine.EventQueue, a.runner.Stats())
	if a.tray != nil {
		st := a.runner.Stats()
		if st.Total > 0 {
			a.tray.SetTooltip(fmt.Sprintf("FFmpeg GUI — %d/%d 完成", st.Done+st.Warning+st.Failed, st.Total))
		}
	}
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

// destModes is the four-way output rule, shared by the main output and by every
// section that relocates a file. One list rather than four near-copies: the four
// reads alike on purpose, and a wording fix that misses one of them is a bug the
// user has to find.
var destModes = []Option{
	{"same", "与源文件同目录"},
	{"sibling", "同级顶层目录 + 后缀（源目录结构）"},
	{"custom", "指定目录"},
	{"mirror", "指定目录（源目录结构）"},
}

// relocateActions is the 处理方式 list for the three sections that move a file:
// 已处理过的源文件, 被排除的文件, and the error / warning policies.
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
		OutputModes:   destModes,
		DestModes:     destModes,
		FilterActions: relocateActions,
		// The 「已处理过的源文件」 section reads exactly like the filter's, so it
		// gets the same list rather than a near-copy that can drift.
		ExistingActions: relocateActions,
		ProblemActions: append(append([]Option{}, relocateActions...),
			Option{"mark", "仅在结果中标记"}),
	}
}
