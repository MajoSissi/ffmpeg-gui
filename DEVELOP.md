# 开发文档

面向要改这份代码的人。面向使用者的说明在 [README.md](README.md)。

## 技术栈

- **Go 1.25+ / Wails v2.16.0** — 桌面外壳（WebView2）与其绑定层。
- **纯静态 ESM 前端** — 无构建步骤、无 npm 依赖。`wails.json` 的
  `frontend:install` / `frontend:build` 都留空，资源通过
  `//go:embed all:frontend/dist` 内嵌进 exe。
- **fyne.io/systray** — 系统托盘。
- **github.com/wailsapp/go-webview2 v1.0.22** — 间接依赖。

## 目录结构

```
app.go                    Wails 绑定层：所有前端可调用的方法都在这里
main.go                   入口，wails.Run
frontend/dist/            前端（纯 ESM）
  index.html  app.js  api.js  ui.js  icons.js  styles.css
  views/       tasks / templates / history / settings / sections
internal/
  store/    配置与模板的持久化
  media/    ffmpeg / ffprobe 的定位与探测
  engine/   命令构造与执行流水线
  sysx/     平台相关（shell、电源、进程）
  tray/     托盘
tools/                 图标生成、截图驱动页等开发工具
```

## 构建

```bash
# Wails CLI 必须 >= v2.16.0
"C:/Users/Majo/go/bin/wails.exe" build          # 增量
"C:/Users/Majo/go/bin/wails.exe" build -clean   # 全量
```

产物：`build/bin/FFmpegGUI.exe`。

校验：

```bash
gofmt -l . && go vet ./... && go test ./... -count=1
```

> 旧版 Wails CLI 内嵌的 x/tools 无法解析新的 Go 标准库，会报
> `package "sync" without types`。升级 CLI 而不是降 Go。

## 前端验证不需要 wails build

`frontend/dist/api.js` 在检测不到 Wails 绑定时会**回退到一份 mock 数据**，
所以改前端时不必每次都重新构建 exe：

```bash
# 服务项目根目录，不是 dist —— 否则驱动页与产物不同源
python -m http.server 8731 --bind 127.0.0.1
```

然后用无头 Chrome：

```bash
chrome --headless=new --disable-gpu --no-sandbox \
  --user-data-dir="$(mktemp -d)" \
  --virtual-time-budget=12000 --window-size=1440,1400 \
  --screenshot="D:\\...\\out.png" \
  "http://127.0.0.1:8731/tools/_drive.html?src=/frontend/dist/index.html&page=templates&tpl=global&h=1380"
```

`tools/_drive.html` 的参数：

| 参数 | 作用 |
| --- | --- |
| `page=tasks\|templates\|history\|settings` | 打开哪个页面（也可写 `p=`） |
| `cat=<id>` | 设置页的分类 |
| `tpl=global` / `tpl=<id>` / `tplIndex=<n>` | 选中哪个模板 |
| `sec=perf\|output\|filter\|problems` | 打开某一段的「与全局不同」开关 |
| `modal=cmd\|command\|tplpreview\|detail\|histdetail` | 打开哪个弹层 |
| `scroll=<选择器>&to=<px>` | 滚动到某处 |
| `probe=1` | 打印选择器命中数与最终落点，不截图 |

两条纪律：

1. **先跑 `?probe=1`**，确认选择器真的命中，再相信截图。探针跑在**导航之后**，
   所以它报的是目标页面的命中数；驱动页也会在左下角标出最终落在哪个页面 ——
   静默 miss 会给你一张「看起来正常」的错误截图。
2. **每次换一个新的 `--user-data-dir`**，否则缓存会让改动看起来"没生效"。

## 架构要点

### 窗口：自绘标题栏

`main.go` 里 `Frameless: true`。系统标题栏被去掉，程序自己画（`app.js` 的
`.titlebar`），好处是那条浅色 Windows 边框不再压在一个深色应用上面。

四个必须知道的细节：

- 拖动靠 CSS：可拖区域标 `--wails-draggable: drag`，**按钮要标
  `--wails-draggable: no-drag` 显式退出**，否则点按钮会变成拖窗口。
- 装饰不能关。`DisableFramelessWindowDecorations` 保持默认 false —— 那层
  "装饰"就是阴影和 resize 边框，关掉窗口就没有影子、也拖不动边缘了。
- 关闭按钮走 `api.quitApp()`（Go 侧 `QuitApp`）。**`QuitApp` 必须先过
  `beforeClose`**：无边框窗口里这个按钮就是大多数人唯一的关闭入口，
  直接调 `Quit()` 会先置 `quitting=true`，「关闭时最小化到托盘」就永远不生效
  ——设置打开着、进程照样死。曾经就是这个 bug。
- **「启动后隐藏」必须在建窗口之前决定**（`main.go` 的 `StartHidden`），
  不能在 `OnStartup` 里 `WindowHide`。Wails 把 `OnStartup` 丢进 goroutine 跑，
  而窗口已经在 `setupChromium` 里显示了 —— 从那里藏永远是用户看得见的一场竞态
  （表现为"启动闪一下才到后台"）。`StartHidden` 让 `navigationCompleted`
  直接 return，窗口**从未出现**；`hasBeenShown` 因此保持 false，之后
  `WindowShow()` 走 default 分支照样能显示出来，托盘单击不受影响。
  前置条件：隐藏必须有托盘兜底，`StartMinimized && !EnableTray` 时**不隐藏**，
  否则是个找不回来的程序。
- **单实例靠 Wails 的 `SingleInstanceLock`**（`UniqueId` 常量不要随版本改）。
  第二份进程在 `frontend.Run` 开头就命中已存在的互斥量，把参数用 `WM_COPYDATA`
  发给第一份然后 `os.Exit(0)` —— **在建窗口之前**就退了，所以不会闪、也不会
  多一个托盘图标。第一份在 `onSecondInstance` 里调 `showWindow()` 把窗口叫出来。
- **托盘左键打开主窗口靠子类化**（`internal/tray/tray_windows.go`）。fyne.io/systray
  的 Windows wndProc 把**左键弹起也当弹菜单**（`WM_RBUTTONUP, WM_LBUTTONUP:
  showMenu()`），且库没留钩子。库有两个恰好固定的值可用：图标回调消息 =
  `WM_USER+1 (0x0401)`，窗口类名 = `"SystrayClass"`。于是找到窗口后
  `SetWindowLongPtrW(GWLP_WNDPROC)` 装自己的过程：
  拦下 `0x0401` 且 lParam 为 `WM_LBUTTONUP/WM_LBUTTONDBLCLK` 的消息 → 显示主窗口，
  其余全部 `CallWindowProcW` 原样转发（右键菜单、explorer 重启重建等不受影响）。
  三个不能省的细节：
  1. **安装点在 `onReady` 里，不是 `Start` 里** —— 窗口是 `systray.Run` 内部建的，
     `Start` 之后还得轮询一个尚未存在的窗口。
  2. **不能用 `FindWindowW`，要 `EnumWindows` + 比对进程 id** —— `FindWindowW`
     会返回**别的进程**的 SystrayClass 窗口（上一个还没退出的实例就够），
     子类化到别人窗口上等于钩子装在一个永远收不到点击的地方。
     实测抓到过 `pid=15376` 的陌生窗口被排在前面。
  3. **回调要 `go fn()` 而不是直接调** —— 显示窗口走 Wails，会 `LockOSThread`
     并跟主窗口消息循环来回一趟，在 wndProc 里同步做会把托盘自己的消息循环卡住。
  旧过程用 `atomic.Uintptr` 存——安装与记录之间有一条消息的竞态窗口，丢了就丢
  （最坏是重建消息），不能为它加锁进消息路径。`syscall.NewCallback` 整个包只建一次。
- **标题栏里只有一个 logo**（准确说一个都没有，logo 在 rail 顶部）。曾经在标题栏
  放 18px 的、rail 放 30px 的，两个相距 40px 明显重复；现在 rail 是唯一的品牌锚点，
  并且去掉了外面那圈圆角底板 —— tile 自带圆角，再套一个框就是"框中框"。

### 顶部：没有标题，只有状态和窗口按钮

三层顶部被逐层删空了，这是刻意的 —— 同一个信息出现三次：

1. `.titlebar` 里的 "FFmpeg GUI"（窗口自己的标题）
2. `.pagehead` 的页面标题（"任务队列" / "参数模板"…）
3. rail 上那一项的标签（"任务" / "模板"…）

**现在 `.titlebar` 里只剩拖动区、状态 chips 和三个窗口按钮**，`.pagetitle`
整块删除，页面直接顶在标题栏下面。`.titlebar` 的 `border-bottom` 因此变成
必需的：拖动区和页面自己的 `.toolbar` 都是 `--surface`，没有这条线就糊成一块。

`.PAGES[].title` 只剩 tooltip 用途，`.sub` 整个删了。改 rail 标签时记得
tooltip 也跟着改。

历史包袱：`.topbar`（60px，含标题 + 副标题 + chips）→ `.pagehead`（一行，
chips 搬进标题栏）→ 没有了。别再把它加回来。

### 表格：勾选不许改变行高

`.check input:checked::after` 会给复选框加一个对勾伪元素，这会改变
inline-flex 的 `.check` 的baseline。在表头里它表现为**点「全选」时整张表的内容
上跳约 3.4px**（实测 thead 36.94px → 33.50px，而所有行高不变）。

修法是给 `table.grid-table th .check` 定死 `height` + `vertical-align: middle`，
让表头行高与勾选状态无关。**这类"某个状态改变 baseline"的 bug 靠肉眼很难定位，
要量**：`getBoundingClientRect().height` 逐步对比 before/after 即可。

### 前后对比单元格（`.cmp`）

分辨率 / 时长 / 大小三列在任务完成后显示"处理前 → 处理后"。做法是**两个格子叠在
一个单元格里**：上行是划掉的源值（`.cmp__from`），下行是新值加百分比
（`.cmp__to` + `.cmp__pct`）。以前是把 `源 → 目标` 拼在一行，两个数字糊在一起，
读者要猜哪个是结果。

`measureCells(job)` 返回的是**三段 innerHTML 字符串**，不是 `<td>` 标签。原因：
浏览器会把裸 `<td>` 从任何赋值容器里提出来，所以"拼一段 `<td>…</td>` 塞进
`createElement('tbody')` 再读 `children[i]`"这种写法**静默拿到 undefined**，
`patchJob` 会持续抛 `Cannot read properties of undefined`。返回字符串最省事也最稳。

### 队列级筛选覆盖（`queueFilter`）

模板的筛选规则是"这类文件怎么处理"，而"这一批只要这些文件"是**队列的决定**，
改模板会连带影响所有用它的队列。所以在 `Runner` 上加了一个运行时覆盖：

- `Runner.SetQueueFilter(*store.FilterSpec)`，nil = 不覆盖
- 覆盖**整段替换**模板规则，不做合并 —— 面板上只有一组数字，半合并的规则集
  描述不了实际会发生什么
- 每个 job 在阶段 2 读一次（`queueFilterSpec()`），批次中途改设置不会让同一批里
  两个文件遵守不同规则
- 持久化在 `Settings.QueueFilter` + `QueueFilterSet`。**两者都要**：
  `QueueFilter == nil` 表示"交回模板"，而 `QueueFilterSet` 记录面板是否被显式打开，
  否则关掉面板和"清空条件"混为一谈

### 批量移除

`Runner.RemoveJobs(ids)` **一把锁删完**，而不是循环调 `RemoveJob`：后者每次都
`emitState()`，勾 50 行就会重绘 50 次，中间还会闪出半空的列表。未知 id 直接跳过
而不是报错——队列在跑的时候行可能自己就没了。回归测试见 `queuefilter_test.go`。

**但后端只广播 stats（`queue:state`），不广播"行没了"**。前端 `on(EVENTS.queue)`
只刷新工具栏——所以删除曾经"成功了但界面不动"。修复在发起方：
`removeChecked` / 单行 `remove` / `clear-finished` 删除后调 `refreshJobs()`
重新拉 `Jobs()` 再 `renderJobs()`。**谁发起结构变更，谁负责重拉**；
不要指望 stats 事件替你重绘列表。

### 行选中（文件管理器语义）

一套选中集（`local.checked`）驱动两样东西：复选框列和批量条。三种点击：

- **普通点击**：只选这一行，并把它设为焦点行（详情面板跟着走）
- **Ctrl+点击**：把该行加入/移出选中集，不改其他行
- **Shift+点击**：从锚点（上一次无修饰键点击的行）到该行整段选中

锚点是 `local.anchorId`，复选框的 change 事件也要更新它，否则"先勾 A、
Shift 勾 C"会从过期的锚点起算。焦点行样式是**描边**（`is-selected`），
批量选中是**填充**（`is-checked`）——两个状态叠在一条行上时都要看得出来。

### 批量条没有自己的复选框

表头的全选是唯一的"全选"控件。批量条里曾放第二个复选框，勾它和勾表头
效果完全一样——两个控件管同一份状态，用户会疑惑哪个才是"真的"。

### 模板继承：全局默认值

→「输出与命名 / 处理性能 / 筛选条件 / 错误与警告」曾经是全局设置，现在全部
挂在模板上。`store.GlobalTemplateID`（`t-global`）那个模板只提供默认值，
**它就是列表的第一项**（`.tpl-row--global`），和其余模板共用一个滚动区与
一套行样式 —— 曾经给它单独做了一张虚线卡片（`.tpl-global`），那是"面板叠面板"，
后来连卡片一起删了。它不可拖动、不可排序、不可删除，这一点靠 `global` 标记
在 `renderList()` 里过滤，样式只是把这件事再显式一遍。

列表头现在只有两行：第一行左边三个整列工具（导入 / 导出 / 恢复内置），右边
「新建模板」；第二行是搜索框。曾经有个"模板"标题，加下面一行分隔线里还有
一个"模板"，加上页面标题 —— 同一个词在屏幕上出现三次。

四段都有「与全局不同」开关，继承有两套机制，因为字段语义不同：

- **处理性能 / 筛选条件 / 错误与警告** 整段回落（`*Spec` 指针，nil = 跟随）。
  因为 `0` 在这些字段里本身有意义 ——「不限制体积」和「跟随全局」必须能区分开。
- **输出与命名** 由 `Template.OutputOverride` 决定：开关关着就**整段丢弃**模板
  自己写的值，开着才逐字段留空回落（`Template.Effective`）。段内没有「0 有意义」
  的字段，所以不需要指针。

`OutputOverride` 一定要在 `Effective` 里**先清空再回落**。只做逐字段回落的话，
一个关着开关却残留旧值的模板，界面写着「跟随全局」而命令用的是旧值 ——
两边说法不一致。`Normalize` 里有一段兼容：老文件里有输出值但没这个标记的，
自动当作「开关已开」，否则升级会把它们的输出规则悄悄退回全局。

`Effective` 返回的模板**共享全局模板的段指针**，只读。不要对它调 `Normalize`，
那会写穿到全局模板上。要改就 `ClonePerf` / `CloneFilter` / `CloneProblems`。

前端镜像：`views/sections.js` 的 `effective(t, g)`，与 Go 侧必须同步
（任务页的筛选 chip 要在本地描述"这条任务实际会怎样"）。段的显示顺序也只在
`sections.js` 的 `SECTIONS` 里定义一次。

### DestRule：四段输出共用一套规则

主输出、筛选排除的文件、问题文件都用 `store.DestRule{Mode,Dir,Suffix}`，
由 `store.ResolveDestDir` 统一解析成目录。四种模式：

```
same    与源文件同目录
sibling 源目录的同级 + 后缀，子目录结构保留   ← 默认
custom  全部平铺到指定目录
mirror  指定根目录 + 保持子目录结构
```

`sibling` 是默认值，也是主推方案：`/video/mmd` → `/video/mmd_out`。
做成**同级**而不是"改名放旁边"，是为了让结果落在扫描树之外 ——
重跑同一目录不会把已生成的产物再吃一遍。

注意 `sibling` 改的只是**最上层那个目录名**，文件本身不动：

```
D:\video\mmd\a.mp4        ->  D:\video\mmd_out\a.mp4
D:\video\mmd\sub\b.mp4    ->  D:\video\mmd_out\sub\b.mp4
```

所以默认命名模板是 `{name}.{ext}` 而不是 `{name}_out` —— 目录已经带了标记，
文件名再加一遍纯属噪音。`{ext}` 取的是**输出**扩展名（`ResolveOutput` 里传
`outExt`），否则选了别的容器会得到一个 MP4 内容却叫 `.mkv` 的文件。

`custom` / `mirror` 的 `Dir` 为空是配置错误，`Validate` 会明确报错，
而不是静默退回"写在源旁边"。

### 空值即省略

数值 0 / 字符串 `""` = 未设置 = **不传该 flag**。唯一例外是
`-hide_banner -nostdin -y -progress pipe:1`（批量必需，`-progress` 是速度 /
码率 / 进度读数的数据来源）。

推论：编码器留空 → 完全不传 `-c:v`，并连带跳过 `-crf` / `-b:v` / `-preset`。
编码器家族不同，质量参数写法也不同（libx264/x265 读 `-crf`，
NVENC / QSV / AMF 读 `-cq`），**宁缺勿猜**。只在真的丢弃了用户填过的项时才警告。

`Template.Normalize` 因此**不能**把 `CRF` 默认成 23 —— 那会给每个从没要求过
CRF 的模板都塞一个 `-crf`，而且对非 lib\* 编码器来说还会标错质量。

### ffmpeg 参数顺序

只有**全局选项**可以出现在第一个 `-i` 之前。`-max_muxing_queue_size` 之类的
输出选项放前面会报 `Option b:v cannot be applied to input url`。

### 平台相关

- **`internal/sysx/shell_windows.go`** — 「定位源文件」用
  `ILCreateFromPathW` + `SHOpenFolderAndSelectItems`，**不要**退回
  `explorer.exe /select,` 子进程：`exec.Command` 的 Windows 参数转义会把整个
  argv 元素包进引号（`"/select,C:\Program Files\x.mp4"`），explorer 自己的
  命令行解析器不接受且**不报错**，只是默默打开「文档」。
- 取 HRESULT 必须 `uint32(ret)` 截断 —— Win64 ABI 下 32 位返回类型的高 32 位
  未定义。
- `CoInitializeEx` 返回 S_OK(0) / S_FALSE(1) 都要配对 `CoUninitialize`；
  `RPC_E_CHANGED_MODE`（线程已在别的 apartment，WebView2 占 STA）**不能**
  uninit，但 shell API 仍可用。
- **`CREATE_NO_WINDOW` / `HideWindow` 绝不能用于 GUI 程序**：它设
  `STARTF_USESHOWWINDOW` + `SW_HIDE`，GUI 进程会据此把**首个窗口**创建为隐藏，
  于是「打开文件夹」既不报错也不出窗口。只给 ffmpeg / ffprobe 用
  `sysx.NoWindow()`。
- **`PrintWindow` 抓不到 WebView2 窗口**（DirectComposition 表面），只能桌面
  BitBlt，且被抓窗口被遮挡时会抓到遮挡物。

### 并发

工作池大小 = 全局模板的 `Perf.Concurrency`。单个模板可以调低，不能调高 ——
池子只有这么大。

per-template 的并发门在 `internal/engine/throttle.go`，它**故意不用** runner 的
`sync.Cond`：那个 cond 守的是队列，槽位释放唤醒队列等待者后再阻塞，
一次漏唤醒就能拖垮整批。这里用独立的 `wake` channel，等待者 select 在
`wake` 和 `ctx.Done()` 上 —— 因为还在等槽位的任务根本没启动，没有 ffmpeg 进程
可取消，只能靠 context。

### Go 1.25+ 的 copylocks

`go vet` 会把任何带 `Lock()/Unlock()` 方法的类型判为锁。所以结构体自用锁时
方法名用小写：`Job.lock()` / `Job.unlock()`。

### `-progress pipe:1`

`out_time_ms` 的实际单位是**微秒**（ffmpeg 的历史遗留）。`out_time_us` 同理。

## 数据文件

都在 `data/`（程序同目录，`-data` 可覆盖）：

| 文件 | 内容 |
| --- | --- |
| `settings.json` | 二进制路径、系统与日志选项、界面记忆 |
| `templates.json` | 模板列表，第一项是全局模板 |
| `history.json` | 处理记录（26 列中文表头 CSV 可导出） |
| `logs/` | 运行日志，按天龄 + 体积双限制轮转 |

### 旧配置迁移

迁移到全局模板之前，`settings.json` 里那些字段是扁平的（`concurrency`、
`outputDirMode`…），`filters` 是个嵌套对象。它们现在保留为
`Legacy*` 字段，配 `omitempty`，由 `NewGlobalFromLegacy` 读一次后
`ClearLegacy` 清空。

注意 `filters` 必须是**嵌套结构**：用点号键（`json:"filters.minSizeMB"`）
`encoding/json` 并不支持，会静默读不到，迁移就成了空操作。

## 图标

`tools/genicon.py` 是图标的唯一真相来源：`f_polys()` 给出几何，同时喂给
Pillow（栅格）和 SVG。

**全部是纯色**（`TILE` / `GLYPH`），没有渐变。深色圆角方块加竖向渐变正是
Windows 11 的默认图标长相，自定义图标要避开的就是这个；而且渐变没法在
rail 那个自有背景上复现，两边一比就露馅。

`brand_svg()` 输出的是**整个图标**（方块 + 描边 + F + 胶片齿孔），
`frontend/dist/icons.js` 的 `brandSvg()` 逐字照抄它。改几何要重新跑脚本并把
打印出来的那一行贴回去 —— 之前 `brandSvg()` 只画裸 F，于是界面图标和 exe 图标
是两张不同的图。颜色走 CSS class（`.brand-tile` / `.brand-f` / `.brand-hint`），
好跟随主题。

`.ico` 是**逐尺寸独立渲染**的，不要用 Pillow 的 `save(sizes=)` 从 256 master
缩放 —— 那样 16px 会把胶片齿孔混进笔画里。脚本里的 `legibility()` 与
`rim_contrast()` 是自检，改完几何要跑一遍。

## 代码约定

- 注释写**为什么**，不写是什么。解释一段反直觉的代码背后的取舍，而不是复述
  它在做什么。
- 中文注释用于面向用户的规则说明（错误文案、UI 提示），英文注释用于内部机制。
- 新增枚举值要同时加进 `app.go` 的 `buildOptions()` 和 `api.js` 的 mock，
  否则前端会拿到一个空下拉框。
- 前端改完至少 `node --check` 一遍；`api.js` 的 mock 与 Go 侧的字段名要同步。
- **「跟随全局」类字段要双向一致**：界面说跟随，`Effective` 就必须真的丢弃模板
  自己的值。只做单向回落会产生"界面说 A、命令做 B"的静默分歧。

## 已知环境限制

在受限的 Job Object 中运行时，WebView2 的所有子进程会以 `exit_code=7` 崩溃
（Chromium 拒绝嵌套 / 脱离）。这是执行环境的限制，不是程序缺陷 ——
用户手动启动一切正常。`--no-sandbox` 可以绕过，但**不要**写进产品代码。
