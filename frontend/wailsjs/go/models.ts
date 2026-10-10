export namespace engine {
	
	export class ApplyResult {
	    applied: number;
	    requeued: number;
	
	    static createFrom(source: any = {}) {
	        return new ApplyResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.applied = source["applied"];
	        this.requeued = source["requeued"];
	    }
	}
	export class DeleteResult {
	    deleted: number;
	    skipped: number;
	    paths: string[];
	    errors: string[];
	
	    static createFrom(source: any = {}) {
	        return new DeleteResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.deleted = source["deleted"];
	        this.skipped = source["skipped"];
	        this.paths = source["paths"];
	        this.errors = source["errors"];
	    }
	}
	export class DirMatch {
	    dir: string;
	    name: string;
	    rel: string;
	    files: number;
	
	    static createFrom(source: any = {}) {
	        return new DirMatch(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.dir = source["dir"];
	        this.name = source["name"];
	        this.rel = source["rel"];
	        this.files = source["files"];
	    }
	}
	export class FolderPreview {
	    root: string;
	    filtering: boolean;
	    exclude: boolean;
	    dirs: DirMatch[];
	    dirsTotal: number;
	    totalFiles: number;
	    filteredFiles: number;
	    scanned: number;
	
	    static createFrom(source: any = {}) {
	        return new FolderPreview(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.root = source["root"];
	        this.filtering = source["filtering"];
	        this.exclude = source["exclude"];
	        this.dirs = this.convertValues(source["dirs"], DirMatch);
	        this.dirsTotal = source["dirsTotal"];
	        this.totalFiles = source["totalFiles"];
	        this.filteredFiles = source["filteredFiles"];
	        this.scanned = source["scanned"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class FolderScan {
	    dir: string;
	    dirs: store.NameRules;
	    files: store.NameRules;
	    recursive: boolean;
	
	    static createFrom(source: any = {}) {
	        return new FolderScan(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.dir = source["dir"];
	        this.dirs = this.convertValues(source["dirs"], store.NameRules);
	        this.files = this.convertValues(source["files"], store.NameRules);
	        this.recursive = source["recursive"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class Job {
	    id: string;
	    input: string;
	    inputName: string;
	    output: string;
	    outputName: string;
	    sourceRoot: string;
	    templateId: string;
	    templateName: string;
	    index: number;
	    status: string;
	    message: string;
	    error: string;
	    warnings: string[];
	    frozen: boolean;
	    command: string;
	    progress: number;
	    speed: number;
	    bitrate: string;
	    frame: number;
	    fps: number;
	    outTimeMs: number;
	    outBytes: number;
	    targetWidth: number;
	    targetHeight: number;
	    resized: boolean;
	    duration: number;
	    size: number;
	    infoBefore?: media.Info;
	    infoAfter?: media.Info;
	    // Go type: time
	    queuedAt: any;
	    // Go type: time
	    startedAt: any;
	    // Go type: time
	    endedAt: any;
	    elapsedMs: number;
	    logTail: string[];
	    logLineCount: number;
	    outputDeleted: boolean;
	    sourceMovedTo: string;
	    recordId: string;
	
	    static createFrom(source: any = {}) {
	        return new Job(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.input = source["input"];
	        this.inputName = source["inputName"];
	        this.output = source["output"];
	        this.outputName = source["outputName"];
	        this.sourceRoot = source["sourceRoot"];
	        this.templateId = source["templateId"];
	        this.templateName = source["templateName"];
	        this.index = source["index"];
	        this.status = source["status"];
	        this.message = source["message"];
	        this.error = source["error"];
	        this.warnings = source["warnings"];
	        this.frozen = source["frozen"];
	        this.command = source["command"];
	        this.progress = source["progress"];
	        this.speed = source["speed"];
	        this.bitrate = source["bitrate"];
	        this.frame = source["frame"];
	        this.fps = source["fps"];
	        this.outTimeMs = source["outTimeMs"];
	        this.outBytes = source["outBytes"];
	        this.targetWidth = source["targetWidth"];
	        this.targetHeight = source["targetHeight"];
	        this.resized = source["resized"];
	        this.duration = source["duration"];
	        this.size = source["size"];
	        this.infoBefore = this.convertValues(source["infoBefore"], media.Info);
	        this.infoAfter = this.convertValues(source["infoAfter"], media.Info);
	        this.queuedAt = this.convertValues(source["queuedAt"], null);
	        this.startedAt = this.convertValues(source["startedAt"], null);
	        this.endedAt = this.convertValues(source["endedAt"], null);
	        this.elapsedMs = source["elapsedMs"];
	        this.logTail = source["logTail"];
	        this.logLineCount = source["logLineCount"];
	        this.outputDeleted = source["outputDeleted"];
	        this.sourceMovedTo = source["sourceMovedTo"];
	        this.recordId = source["recordId"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class Plan {
	    bin: string;
	    args: string[];
	    command: string;
	    warnings: string[];
	    targetW: number;
	    targetH: number;
	    resized: boolean;
	    outputExt: string;
	
	    static createFrom(source: any = {}) {
	        return new Plan(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.bin = source["bin"];
	        this.args = source["args"];
	        this.command = source["command"];
	        this.warnings = source["warnings"];
	        this.targetW = source["targetW"];
	        this.targetH = source["targetH"];
	        this.resized = source["resized"];
	        this.outputExt = source["outputExt"];
	    }
	}
	export class Stats {
	    total: number;
	    pending: number;
	    running: number;
	    done: number;
	    warning: number;
	    failed: number;
	    canceled: number;
	    skipped: number;
	    filtered: number;
	    paused: boolean;
	    started: boolean;
	    workers: number;
	    progress: number;
	
	    static createFrom(source: any = {}) {
	        return new Stats(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.total = source["total"];
	        this.pending = source["pending"];
	        this.running = source["running"];
	        this.done = source["done"];
	        this.warning = source["warning"];
	        this.failed = source["failed"];
	        this.canceled = source["canceled"];
	        this.skipped = source["skipped"];
	        this.filtered = source["filtered"];
	        this.paused = source["paused"];
	        this.started = source["started"];
	        this.workers = source["workers"];
	        this.progress = source["progress"];
	    }
	}

}

export namespace main {
	
	export class AddResult {
	    added: number;
	    errors: string[];
	
	    static createFrom(source: any = {}) {
	        return new AddResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.added = source["added"];
	        this.errors = source["errors"];
	    }
	}
	export class Option {
	    value: string;
	    label: string;
	
	    static createFrom(source: any = {}) {
	        return new Option(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.value = source["value"];
	        this.label = source["label"];
	    }
	}
	export class Options {
	    videoCodecs: Option[];
	    audioCodecs: Option[];
	    containers: Option[];
	    presets: Option[];
	    resizeModes: Option[];
	    scaleAlgorithms: Option[];
	    logLevels: Option[];
	    rateControls: Option[];
	    padColors: Option[];
	    destModes: Option[];
	    defaultOutputSuffix: string;
	    filterActions: Option[];
	    problemActions: Option[];
	    existingActions: Option[];
	
	    static createFrom(source: any = {}) {
	        return new Options(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.videoCodecs = this.convertValues(source["videoCodecs"], Option);
	        this.audioCodecs = this.convertValues(source["audioCodecs"], Option);
	        this.containers = this.convertValues(source["containers"], Option);
	        this.presets = this.convertValues(source["presets"], Option);
	        this.resizeModes = this.convertValues(source["resizeModes"], Option);
	        this.scaleAlgorithms = this.convertValues(source["scaleAlgorithms"], Option);
	        this.logLevels = this.convertValues(source["logLevels"], Option);
	        this.rateControls = this.convertValues(source["rateControls"], Option);
	        this.padColors = this.convertValues(source["padColors"], Option);
	        this.destModes = this.convertValues(source["destModes"], Option);
	        this.defaultOutputSuffix = source["defaultOutputSuffix"];
	        this.filterActions = this.convertValues(source["filterActions"], Option);
	        this.problemActions = this.convertValues(source["problemActions"], Option);
	        this.existingActions = this.convertValues(source["existingActions"], Option);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class FilterState {
	    profiles: store.FilterProfile[];
	    active: string;
	    off: boolean;
	    profile: store.FilterProfile;
	
	    static createFrom(source: any = {}) {
	        return new FilterState(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.profiles = this.convertValues(source["profiles"], store.FilterProfile);
	        this.active = source["active"];
	        this.off = source["off"];
	        this.profile = this.convertValues(source["profile"], store.FilterProfile);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class RuntimeInfo {
	    os: string;
	    arch: string;
	    cpus: number;
	    dataDir: string;
	    binaries: media.Binaries;
	    ffmpeg: media.ToolInfo;
	    ffprobe: media.ToolInfo;
	    version: string;
	
	    static createFrom(source: any = {}) {
	        return new RuntimeInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.os = source["os"];
	        this.arch = source["arch"];
	        this.cpus = source["cpus"];
	        this.dataDir = source["dataDir"];
	        this.binaries = this.convertValues(source["binaries"], media.Binaries);
	        this.ffmpeg = this.convertValues(source["ffmpeg"], media.ToolInfo);
	        this.ffprobe = this.convertValues(source["ffprobe"], media.ToolInfo);
	        this.version = source["version"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class Bootstrap {
	    runtime: RuntimeInfo;
	    settings: store.Settings;
	    filter: FilterState;
	    templates: store.Template[];
	    jobs: engine.Job[];
	    stats: engine.Stats;
	    history: store.Record[];
	    options: Options;
	
	    static createFrom(source: any = {}) {
	        return new Bootstrap(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.runtime = this.convertValues(source["runtime"], RuntimeInfo);
	        this.settings = this.convertValues(source["settings"], store.Settings);
	        this.filter = this.convertValues(source["filter"], FilterState);
	        this.templates = this.convertValues(source["templates"], store.Template);
	        this.jobs = this.convertValues(source["jobs"], engine.Job);
	        this.stats = this.convertValues(source["stats"], engine.Stats);
	        this.history = this.convertValues(source["history"], store.Record);
	        this.options = this.convertValues(source["options"], Options);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class EncoderSupport {
	    name: string;
	    label: string;
	    ok: boolean;
	
	    static createFrom(source: any = {}) {
	        return new EncoderSupport(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.label = source["label"];
	        this.ok = source["ok"];
	    }
	}
	
	export class HistoryPage {
	    total: number;
	    items: store.Record[];
	
	    static createFrom(source: any = {}) {
	        return new HistoryPage(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.total = source["total"];
	        this.items = this.convertValues(source["items"], store.Record);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class HistoryQuery {
	    keyword: string;
	    status: string;
	    from: string;
	    to: string;
	    sort: string;
	    offset: number;
	    limit: number;
	
	    static createFrom(source: any = {}) {
	        return new HistoryQuery(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.keyword = source["keyword"];
	        this.status = source["status"];
	        this.from = source["from"];
	        this.to = source["to"];
	        this.sort = source["sort"];
	        this.offset = source["offset"];
	        this.limit = source["limit"];
	    }
	}
	export class LocatedFile {
	    path: string;
	    moved: boolean;
	
	    static createFrom(source: any = {}) {
	        return new LocatedFile(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.moved = source["moved"];
	    }
	}
	
	
	export class PathCheck {
	    srcDir: string;
	    outputDir: string;
	    outputPath: string;
	    notices?: string[];
	
	    static createFrom(source: any = {}) {
	        return new PathCheck(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.srcDir = source["srcDir"];
	        this.outputDir = source["outputDir"];
	        this.outputPath = source["outputPath"];
	        this.notices = source["notices"];
	    }
	}

}

export namespace media {
	
	export class Binaries {
	    ffmpeg: string;
	    ffprobe: string;
	    ffmpegFrom: string;
	    ffprobeFrom: string;
	
	    static createFrom(source: any = {}) {
	        return new Binaries(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.ffmpeg = source["ffmpeg"];
	        this.ffprobe = source["ffprobe"];
	        this.ffmpegFrom = source["ffmpegFrom"];
	        this.ffprobeFrom = source["ffprobeFrom"];
	    }
	}
	export class Stream {
	    index: number;
	    type: string;
	    codec: string;
	    codecLong: string;
	    profile: string;
	    width: number;
	    height: number;
	    fps: number;
	    bitRate: number;
	    pixFmt: string;
	    sampleRate: number;
	    channels: number;
	    channelLayout: string;
	    language: string;
	    title: string;
	    default: boolean;
	
	    static createFrom(source: any = {}) {
	        return new Stream(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.index = source["index"];
	        this.type = source["type"];
	        this.codec = source["codec"];
	        this.codecLong = source["codecLong"];
	        this.profile = source["profile"];
	        this.width = source["width"];
	        this.height = source["height"];
	        this.fps = source["fps"];
	        this.bitRate = source["bitRate"];
	        this.pixFmt = source["pixFmt"];
	        this.sampleRate = source["sampleRate"];
	        this.channels = source["channels"];
	        this.channelLayout = source["channelLayout"];
	        this.language = source["language"];
	        this.title = source["title"];
	        this.default = source["default"];
	    }
	}
	export class Info {
	    path: string;
	    fileName: string;
	    ext: string;
	    size: number;
	    container: string;
	    containerLong: string;
	    duration: number;
	    bitRate: number;
	    rotation: number;
	    width: number;
	    height: number;
	    displayWidth: number;
	    displayHeight: number;
	    fps: number;
	    pixFmt: string;
	    videoCodec: string;
	    audioCodec: string;
	    video?: Stream;
	    audio?: Stream;
	    videoN: number;
	    audioN: number;
	    subtitleN: number;
	    chapters: number;
	
	    static createFrom(source: any = {}) {
	        return new Info(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.fileName = source["fileName"];
	        this.ext = source["ext"];
	        this.size = source["size"];
	        this.container = source["container"];
	        this.containerLong = source["containerLong"];
	        this.duration = source["duration"];
	        this.bitRate = source["bitRate"];
	        this.rotation = source["rotation"];
	        this.width = source["width"];
	        this.height = source["height"];
	        this.displayWidth = source["displayWidth"];
	        this.displayHeight = source["displayHeight"];
	        this.fps = source["fps"];
	        this.pixFmt = source["pixFmt"];
	        this.videoCodec = source["videoCodec"];
	        this.audioCodec = source["audioCodec"];
	        this.video = this.convertValues(source["video"], Stream);
	        this.audio = this.convertValues(source["audio"], Stream);
	        this.videoN = source["videoN"];
	        this.audioN = source["audioN"];
	        this.subtitleN = source["subtitleN"];
	        this.chapters = source["chapters"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	
	export class ToolInfo {
	    name: string;
	    path: string;
	    source: string;
	    version: string;
	    ok: boolean;
	    error: string;
	
	    static createFrom(source: any = {}) {
	        return new ToolInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.path = source["path"];
	        this.source = source["source"];
	        this.version = source["version"];
	        this.ok = source["ok"];
	        this.error = source["error"];
	    }
	}

}

export namespace store {
	
	export class ArgSpec {
	    flag: string;
	    value: string;
	    comment: string;
	    enabled: boolean;
	
	    static createFrom(source: any = {}) {
	        return new ArgSpec(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.flag = source["flag"];
	        this.value = source["value"];
	        this.comment = source["comment"];
	        this.enabled = source["enabled"];
	    }
	}
	export class NameFilter {
	    mode: string;
	    value: string;
	
	    static createFrom(source: any = {}) {
	        return new NameFilter(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.mode = source["mode"];
	        this.value = source["value"];
	    }
	}
	export class DirFilter {
	    enabled: boolean;
	    filters: NameFilter[];
	    matchAll: boolean;
	    exclude: boolean;
	    topOnly: boolean;
	
	    static createFrom(source: any = {}) {
	        return new DirFilter(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.enabled = source["enabled"];
	        this.filters = this.convertValues(source["filters"], NameFilter);
	        this.matchAll = source["matchAll"];
	        this.exclude = source["exclude"];
	        this.topOnly = source["topOnly"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class DirSpec {
	    mode: string;
	    dir: string;
	    prefix: string;
	    suffix: string;
	    keepTree: boolean;
	
	    static createFrom(source: any = {}) {
	        return new DirSpec(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.mode = source["mode"];
	        this.dir = source["dir"];
	        this.prefix = source["prefix"];
	        this.suffix = source["suffix"];
	        this.keepTree = source["keepTree"];
	    }
	}
	export class ExistingSpec {
	    action: string;
	    dir: DirSpec;
	    pattern?: string;
	    overwrite: boolean;
	
	    static createFrom(source: any = {}) {
	        return new ExistingSpec(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.action = source["action"];
	        this.dir = this.convertValues(source["dir"], DirSpec);
	        this.pattern = source["pattern"];
	        this.overwrite = source["overwrite"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class NameRules {
	    enabled: boolean;
	    filters: NameFilter[];
	    matchAll: boolean;
	    exclude: boolean;
	
	    static createFrom(source: any = {}) {
	        return new NameRules(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.enabled = source["enabled"];
	        this.filters = this.convertValues(source["filters"], NameFilter);
	        this.matchAll = source["matchAll"];
	        this.exclude = source["exclude"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class FilterProfile {
	    name: string;
	    description: string;
	    dirs: DirFilter;
	    files: NameRules;
	
	    static createFrom(source: any = {}) {
	        return new FilterProfile(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.description = source["description"];
	        this.dirs = this.convertValues(source["dirs"], DirFilter);
	        this.files = this.convertValues(source["files"], NameRules);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class FilterSpec {
	    minSizeMB: number;
	    maxSizeMB: number;
	    minLongEdge: number;
	    maxLongEdge: number;
	    minDuration: number;
	    maxDuration: number;
	    includeExts: string[];
	    excludeExts: string[];
	    action: string;
	    dir: DirSpec;
	    renamePattern: string;
	    overwrite: boolean;
	
	    static createFrom(source: any = {}) {
	        return new FilterSpec(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.minSizeMB = source["minSizeMB"];
	        this.maxSizeMB = source["maxSizeMB"];
	        this.minLongEdge = source["minLongEdge"];
	        this.maxLongEdge = source["maxLongEdge"];
	        this.minDuration = source["minDuration"];
	        this.maxDuration = source["maxDuration"];
	        this.includeExts = source["includeExts"];
	        this.excludeExts = source["excludeExts"];
	        this.action = source["action"];
	        this.dir = this.convertValues(source["dir"], DirSpec);
	        this.renamePattern = source["renamePattern"];
	        this.overwrite = source["overwrite"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class MediaSummary {
	    exists: boolean;
	    path: string;
	    size: number;
	    container: string;
	    duration: number;
	    videoCodec: string;
	    width: number;
	    height: number;
	    fps: number;
	    pixFmt: string;
	    audioCodec: string;
	    sampleRate: number;
	    channels: number;
	    bitRate: number;
	
	    static createFrom(source: any = {}) {
	        return new MediaSummary(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.exists = source["exists"];
	        this.path = source["path"];
	        this.size = source["size"];
	        this.container = source["container"];
	        this.duration = source["duration"];
	        this.videoCodec = source["videoCodec"];
	        this.width = source["width"];
	        this.height = source["height"];
	        this.fps = source["fps"];
	        this.pixFmt = source["pixFmt"];
	        this.audioCodec = source["audioCodec"];
	        this.sampleRate = source["sampleRate"];
	        this.channels = source["channels"];
	        this.bitRate = source["bitRate"];
	    }
	}
	
	
	export class PerfSpec {
	    concurrency: number;
	    logLevel: string;
	    retryCount: number;
	    threads: number;
	    idlePriority: boolean;
	    deleteOnFail: boolean;
	
	    static createFrom(source: any = {}) {
	        return new PerfSpec(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.concurrency = source["concurrency"];
	        this.logLevel = source["logLevel"];
	        this.retryCount = source["retryCount"];
	        this.threads = source["threads"];
	        this.idlePriority = source["idlePriority"];
	        this.deleteOnFail = source["deleteOnFail"];
	    }
	}
	export class ProblemSpec {
	    errorAction: string;
	    errorDir: DirSpec;
	    errorPattern?: string;
	    warningAction: string;
	    warningDir: DirSpec;
	    warningPattern?: string;
	
	    static createFrom(source: any = {}) {
	        return new ProblemSpec(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.errorAction = source["errorAction"];
	        this.errorDir = this.convertValues(source["errorDir"], DirSpec);
	        this.errorPattern = source["errorPattern"];
	        this.warningAction = source["warningAction"];
	        this.warningDir = this.convertValues(source["warningDir"], DirSpec);
	        this.warningPattern = source["warningPattern"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class Record {
	    id: string;
	    input: string;
	    output: string;
	    templateId: string;
	    sourceMovedTo: string;
	    templateName: string;
	    command: string;
	    status: string;
	    note: string;
	    error: string;
	    warnings: string[];
	    // Go type: time
	    startedAt: any;
	    // Go type: time
	    endedAt: any;
	    elapsedMs: number;
	    speed: number;
	    before: MediaSummary;
	    after: MediaSummary;
	
	    static createFrom(source: any = {}) {
	        return new Record(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.input = source["input"];
	        this.output = source["output"];
	        this.templateId = source["templateId"];
	        this.sourceMovedTo = source["sourceMovedTo"];
	        this.templateName = source["templateName"];
	        this.command = source["command"];
	        this.status = source["status"];
	        this.note = source["note"];
	        this.error = source["error"];
	        this.warnings = source["warnings"];
	        this.startedAt = this.convertValues(source["startedAt"], null);
	        this.endedAt = this.convertValues(source["endedAt"], null);
	        this.elapsedMs = source["elapsedMs"];
	        this.speed = source["speed"];
	        this.before = this.convertValues(source["before"], MediaSummary);
	        this.after = this.convertValues(source["after"], MediaSummary);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class ResizeSpec {
	    mode: string;
	    longEdge: number;
	    shortEdge: number;
	    width: number;
	    height: number;
	    maxWidth: number;
	    maxHeight: number;
	    percent: number;
	    onlyLarger: boolean;
	    multipleOf: number;
	    algorithm: string;
	    padToTarget: boolean;
	    padColor: string;
	
	    static createFrom(source: any = {}) {
	        return new ResizeSpec(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.mode = source["mode"];
	        this.longEdge = source["longEdge"];
	        this.shortEdge = source["shortEdge"];
	        this.width = source["width"];
	        this.height = source["height"];
	        this.maxWidth = source["maxWidth"];
	        this.maxHeight = source["maxHeight"];
	        this.percent = source["percent"];
	        this.onlyLarger = source["onlyLarger"];
	        this.multipleOf = source["multipleOf"];
	        this.algorithm = source["algorithm"];
	        this.padToTarget = source["padToTarget"];
	        this.padColor = source["padColor"];
	    }
	}
	export class Settings {
	    ffmpegPath: string;
	    ffprobePath: string;
	    globalInArgs: string;
	    globalOutArgs: string;
	    hardwareDecode: boolean;
	    preventSleep: boolean;
	    enableTray: boolean;
	    closeToTray: boolean;
	    startMinimized: boolean;
	    confirmExit: boolean;
	    keepLogLines: number;
	    saveRunLog: boolean;
	    logMaxSizeMB: number;
	    logKeepDays: number;
	    filterProfiles: FilterProfile[];
	    activeFilter: string;
	    lastTemplateId: string;
	    showLogPanel: boolean;
	    logDir: string;
	    logPanelHeight: number;
	    logPanelSized: boolean;
	
	    static createFrom(source: any = {}) {
	        return new Settings(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.ffmpegPath = source["ffmpegPath"];
	        this.ffprobePath = source["ffprobePath"];
	        this.globalInArgs = source["globalInArgs"];
	        this.globalOutArgs = source["globalOutArgs"];
	        this.hardwareDecode = source["hardwareDecode"];
	        this.preventSleep = source["preventSleep"];
	        this.enableTray = source["enableTray"];
	        this.closeToTray = source["closeToTray"];
	        this.startMinimized = source["startMinimized"];
	        this.confirmExit = source["confirmExit"];
	        this.keepLogLines = source["keepLogLines"];
	        this.saveRunLog = source["saveRunLog"];
	        this.logMaxSizeMB = source["logMaxSizeMB"];
	        this.logKeepDays = source["logKeepDays"];
	        this.filterProfiles = this.convertValues(source["filterProfiles"], FilterProfile);
	        this.activeFilter = source["activeFilter"];
	        this.lastTemplateId = source["lastTemplateId"];
	        this.showLogPanel = source["showLogPanel"];
	        this.logDir = source["logDir"];
	        this.logPanelHeight = source["logPanelHeight"];
	        this.logPanelSized = source["logPanelSized"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class Template {
	    id: string;
	    name: string;
	    description: string;
	    builtin: boolean;
	    global?: boolean;
	    container: string;
	    videoMode: string;
	    videoCodec: string;
	    rateControl: string;
	    crf: number;
	    videoBitrate: string;
	    maxRate: string;
	    bufSize: string;
	    preset: string;
	    tune: string;
	    profile: string;
	    level: string;
	    pixFmt: string;
	    resize: ResizeSpec;
	    fps: string;
	    audioMode: string;
	    audioCodec: string;
	    audioBitrate: string;
	    audioChannels: number;
	    sampleRate: number;
	    outDirSpec: DirSpec;
	    outPattern: string;
	    outputOverride?: boolean;
	    perf?: PerfSpec;
	    existing?: ExistingSpec;
	    filter?: FilterSpec;
	    problems?: ProblemSpec;
	    mapAll: boolean;
	    fastStart: boolean;
	    stripMetadata: boolean;
	    stripChapters: boolean;
	    maxMuxQueue: number;
	    filterMode: string;
	    videoFilters: string;
	    audioFilters: string;
	    inputArgs: ArgSpec[];
	    outputArgs: ArgSpec[];
	
	    static createFrom(source: any = {}) {
	        return new Template(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.description = source["description"];
	        this.builtin = source["builtin"];
	        this.global = source["global"];
	        this.container = source["container"];
	        this.videoMode = source["videoMode"];
	        this.videoCodec = source["videoCodec"];
	        this.rateControl = source["rateControl"];
	        this.crf = source["crf"];
	        this.videoBitrate = source["videoBitrate"];
	        this.maxRate = source["maxRate"];
	        this.bufSize = source["bufSize"];
	        this.preset = source["preset"];
	        this.tune = source["tune"];
	        this.profile = source["profile"];
	        this.level = source["level"];
	        this.pixFmt = source["pixFmt"];
	        this.resize = this.convertValues(source["resize"], ResizeSpec);
	        this.fps = source["fps"];
	        this.audioMode = source["audioMode"];
	        this.audioCodec = source["audioCodec"];
	        this.audioBitrate = source["audioBitrate"];
	        this.audioChannels = source["audioChannels"];
	        this.sampleRate = source["sampleRate"];
	        this.outDirSpec = this.convertValues(source["outDirSpec"], DirSpec);
	        this.outPattern = source["outPattern"];
	        this.outputOverride = source["outputOverride"];
	        this.perf = this.convertValues(source["perf"], PerfSpec);
	        this.existing = this.convertValues(source["existing"], ExistingSpec);
	        this.filter = this.convertValues(source["filter"], FilterSpec);
	        this.problems = this.convertValues(source["problems"], ProblemSpec);
	        this.mapAll = source["mapAll"];
	        this.fastStart = source["fastStart"];
	        this.stripMetadata = source["stripMetadata"];
	        this.stripChapters = source["stripChapters"];
	        this.maxMuxQueue = source["maxMuxQueue"];
	        this.filterMode = source["filterMode"];
	        this.videoFilters = source["videoFilters"];
	        this.audioFilters = source["audioFilters"];
	        this.inputArgs = this.convertValues(source["inputArgs"], ArgSpec);
	        this.outputArgs = this.convertValues(source["outputArgs"], ArgSpec);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

