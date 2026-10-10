package store

// ---------------------------------------------------------------------------
// Resize
// ---------------------------------------------------------------------------

// Resize modes.
const (
	ResizeKeep      = "keep"      // 保持原始分辨率
	ResizeLongEdge  = "longedge"  // 长边固定，短边按比例（自动识别横竖屏）
	ResizeShortEdge = "shortedge" // 短边固定，长边按比例
	ResizeExact     = "exact"     // 指定宽 x 高
	ResizeFit       = "fit"       // 限制在矩形范围内，只缩不放
	ResizePercent   = "percent"   // 按百分比缩放
)

// Scale algorithms understood by ffmpeg's scale filter. The empty string is a
// valid choice too: it means "emit no :flags= at all", i.e. ffmpeg's own default
// (bicubic).
const (
	ScaleLanczos  = "lanczos"
	ScaleBicubic  = "bicubic"
	ScaleBilinear = "bilinear"
	ScaleNeighbor = "neighbor"
)

// ResizeSpec describes the target geometry for a template.
type ResizeSpec struct {
	Mode        string  `json:"mode"`
	LongEdge    int     `json:"longEdge"`
	ShortEdge   int     `json:"shortEdge"`
	Width       int     `json:"width"`
	Height      int     `json:"height"`
	MaxWidth    int     `json:"maxWidth"`
	MaxHeight   int     `json:"maxHeight"`
	Percent     float64 `json:"percent"`
	OnlyLarger  bool    `json:"onlyLarger"` // 只缩小不放大
	MultipleOf  int     `json:"multipleOf"` // 宽高对齐倍数
	Algorithm   string  `json:"algorithm"`
	PadToTarget bool    `json:"padToTarget"`
	PadColor    string  `json:"padColor"`
}

// Enabled reports whether the spec actually changes anything.
func (r ResizeSpec) Enabled() bool {
	switch r.Mode {
	case "", ResizeKeep:
		return false
	case ResizeLongEdge:
		return r.LongEdge > 0
	case ResizeShortEdge:
		return r.ShortEdge > 0
	case ResizeExact:
		return r.Width > 0 || r.Height > 0
	case ResizeFit:
		return r.MaxWidth > 0 || r.MaxHeight > 0
	case ResizePercent:
		return r.Percent > 0 && r.Percent != 100
	}
	return false
}

// ---------------------------------------------------------------------------
// Extra arguments
// ---------------------------------------------------------------------------

// ArgSpec is one custom ffmpeg argument pair shown in the template editor.
type ArgSpec struct {
	Flag    string `json:"flag"`
	Value   string `json:"value"`
	Comment string `json:"comment"`
	Enabled bool   `json:"enabled"`
}

// ---------------------------------------------------------------------------
// Template
// ---------------------------------------------------------------------------

// Video / audio / container handling modes.
const (
	ModeCopy    = "copy"    // 直接复制流
	ModeEncode  = "encode"  // 重新编码
	ModeDisable = "disable" // 丢弃该类型的流
)

// Rate control modes.
const (
	RateCRF     = "crf"
	RateBitrate = "bitrate"
	RateQP      = "qp"
)

// Template is a reusable, switchable set of ffmpeg parameters.
type Template struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Builtin     bool   `json:"builtin"`
	// Global marks the one template that holds the defaults every other template
	// inherits. It is pinned at the top of the list and cannot be deleted or
	// duplicated.
	Global bool `json:"global,omitempty"`

	// 容器
	Container string `json:"container"`

	// 视频
	VideoMode    string `json:"videoMode"`
	VideoCodec   string `json:"videoCodec"`
	RateControl  string `json:"rateControl"`
	CRF          int    `json:"crf"`
	VideoBitrate string `json:"videoBitrate"`
	MaxRate      string `json:"maxRate"`
	BufSize      string `json:"bufSize"`
	Preset       string `json:"preset"`
	Tune         string `json:"tune"`
	Profile      string `json:"profile"`
	Level        string `json:"level"`
	PixFmt       string `json:"pixFmt"`

	Resize ResizeSpec `json:"resize"`
	FPS    string     `json:"fps"`

	// 音频
	AudioMode     string `json:"audioMode"`
	AudioCodec    string `json:"audioCodec"`
	AudioBitrate  string `json:"audioBitrate"`
	AudioChannels int    `json:"audioChannels"`
	SampleRate    int    `json:"sampleRate"`

	// 输出与命名。是否跟随「全局模板」只由 OutputOverride 这一个开关决定
	//（对应界面上的「与全局不同」）：为 false 时整段跟随，模板自己写的值被忽略；
	// 为 true 时字段留空各自取最朴素的默认——方式留空 = 与源文件同目录，名称留空
	// = 源文件名。段内不再有第二层「留空即跟随」。
	OutDirSpec DirSpec `json:"outDirSpec"` // 产物放哪：原目录 / 自定义目录 / 同级目录 / 同级顶层目录
	OutPattern string  `json:"outPattern"` // 输出文件名称，如 {name}
	// OutputOverride 记录这一段是不是被模板显式接管。关掉时整段跟随全局模板。
	// 之所以用布尔量而不是指针，是因为输出段里没有「0 有意义」的字段——
	// 每个字段为空就表示未设置，逐字段回落已经足够。
	OutputOverride bool `json:"outputOverride,omitempty"`

	// 以下四段整段跟随全局：指针为 nil 即表示"这一段用全局模板的值"。
	// 之所以用整段指针而不是逐字段留空，是因为 0 在这些字段里本身就有意义
	//（例如"不限制体积"和"跟随全局"必须能区分开）。
	Perf     *PerfSpec     `json:"perf,omitempty"`
	Existing *ExistingSpec `json:"existing,omitempty"`
	Filter   *FilterSpec   `json:"filter,omitempty"`
	Problems *ProblemSpec  `json:"problems,omitempty"`

	// 容器与元数据
	MapAll        bool `json:"mapAll"`
	FastStart     bool `json:"fastStart"`
	StripMetadata bool `json:"stripMetadata"`
	StripChapters bool `json:"stripChapters"`
	// MaxMuxQueue maps to -max_muxing_queue_size. 0 means "leave it to ffmpeg"
	// and the flag is omitted; it is only worth setting for inputs that burst
	// packets, which would otherwise abort the muxer.
	MaxMuxQueue int `json:"maxMuxQueue"`

	// 附加
	FilterMode   string    `json:"filterMode"` // "vf" 简单滤镜链 | "complex" 完整滤镜图
	VideoFilters string    `json:"videoFilters"`
	AudioFilters string    `json:"audioFilters"`
	InputArgs    []ArgSpec `json:"inputArgs"`
	OutputArgs   []ArgSpec `json:"outputArgs"`
}

// IsRemux reports whether the template only rewraps the streams.
func (t Template) IsRemux() bool {
	return t.VideoMode == ModeCopy && (t.AudioMode == ModeCopy || t.AudioMode == ModeDisable) && !t.Resize.Enabled()
}

// Normalize fills safe defaults for enum-ish fields.
func (t *Template) Normalize() {
	if t.ID == "" {
		t.ID = NewID()
	}
	switch t.VideoMode {
	case ModeCopy, ModeEncode, ModeDisable:
	default:
		t.VideoMode = ModeEncode
	}
	switch t.AudioMode {
	case ModeCopy, ModeEncode, ModeDisable:
	default:
		t.AudioMode = ModeEncode
	}
	switch t.RateControl {
	case RateCRF, RateBitrate, RateQP:
	default:
		t.RateControl = RateCRF
	}
	switch t.Resize.Mode {
	case "", ResizeKeep, ResizeLongEdge, ResizeShortEdge, ResizeExact, ResizeFit, ResizePercent:
	default:
		t.Resize.Mode = ResizeKeep
	}
	if t.Resize.MultipleOf <= 0 {
		t.Resize.MultipleOf = 2
	}
	// The scale algorithm stays "" when the user picked 默认: that is the difference
	// between "not set" and "lanczos", and filling lanczos in here would add
	// :flags=lanczos to the command of a template that deliberately left it alone.
	if t.Resize.PadColor == "" {
		t.Resize.PadColor = "black"
	}
	switch t.FilterMode {
	case "", "vf", "complex":
	default:
		t.FilterMode = "vf"
	}
	// 一个模式管四个字段的哪些有意义，认不出的模式宁可退回"原目录"：那是唯一不
	// 会写到别处去的答案。
	switch t.OutDirSpec.Mode {
	case "", OutputSame, OutputSibling, OutputSiblingTop, OutputCustom:
	default:
		t.OutDirSpec.Mode = ""
	}
	// The four inheritable sections are only normalised when present; a nil
	// section is the "follow the global template" state and must stay nil.
	if t.Perf != nil {
		t.Perf.Normalize()
	}
	if t.Existing != nil {
		switch t.Existing.Action {
		case "", ActionMove, ActionCopy:
		default:
			t.Existing.Action = ""
		}
	}
	if t.Filter != nil {
		switch t.Filter.Action {
		case "", ActionKeep, ActionMove, ActionCopy:
		default:
			t.Filter.Action = ""
		}
		t.Filter.IncludeExts = normalizeExts(t.Filter.IncludeExts)
		t.Filter.ExcludeExts = normalizeExts(t.Filter.ExcludeExts)
	}
	if t.Problems != nil {
		switch t.Problems.ErrorAction {
		case "", ActionKeep, ActionMove, ActionCopy:
		default:
			t.Problems.ErrorAction = ""
		}
		switch t.Problems.WarningAction {
		case "", ActionKeep, ActionMove, ActionCopy, ActionMark:
		default:
			t.Problems.WarningAction = ""
		}
	}
	// CRF stays 0 when unset, and 0 means "let ffmpeg decide" -- the flag is then
	// omitted entirely. Defaulting it to 23 here would put a -crf into the command
	// of every template that never asked for one, and would also mislabel the
	// quality for non-lib* encoders (NVENC, QSV and AMF do not read -crf at all).
	if t.CRF < 0 {
		t.CRF = 0
	}
	if t.CRF > 51 {
		t.CRF = 51
	}
}

// TemplatesPath is the template file location.
func TemplatesPath() string { return Path("templates.json") }

// LoadTemplates reads templates.json, seeding the built-in set on first run.
//
// It also guarantees a global template is present and pinned first.
func LoadTemplates() []Template {
	var list []Template
	ok, err := ReadJSON(TemplatesPath(), &list)
	if !ok || err != nil || len(list) == 0 {
		list = BuiltinTemplates()
	} else {
		// 老文件里的 outMode/dest 或 path 只在这里读一次，翻译成"模式 + 字段"
		//（见 legacy_dir.go）。读文件而不是读结构体：那些键已经不在 Template 上了。
		adoptLegacyDirs(list)
	}
	for i := range list {
		list[i].Normalize()
	}

	list, changed := EnsureGlobal(list)
	if !ok || err != nil || changed {
		_ = SaveTemplates(list)
	}
	return list
}

// SaveTemplates writes templates.json.
func SaveTemplates(list []Template) error {
	return WriteJSON(TemplatesPath(), list)
}
