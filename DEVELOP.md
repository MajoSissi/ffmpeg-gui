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
  回调本身存 `atomic.Pointer[func()]`：消息线程读、`onReady` 写，而且 `Stop` 必须能
  把它清掉。

- **右键菜单「用一次灵、隔一次失效」是 TrackPopupMenu 的档案缺陷，缺 `WM_NULL`。**
  MSDN 的 TrackPopupMenu 备注（原 KB135788）写明：给通知区图标弹菜单，除了
  `SetForegroundWindow`，还必须在 `TrackPopupMenu` **返回之后**往宿主窗口
  `PostMessage(hWnd, WM_NULL, 0, 0)`；少了这一步，「菜单第二次显示时会一闪即消失」。
  fyne.io/systray 只做了前一半。子类化正好是从 lib 手里接过这条消息之后的落脚点：

  ```
  r := CallWindowProcW(prev, ...)              // 里面阻塞在 TrackPopupMenu，直到菜单关掉
  if msg == 0x0401 && lParam == WM_RBUTTONUP { PostMessageW(hWnd, WM_NULL, 0, 0) }
  ```

- **`Stop` 里要 `uninstallLeftClick()`，否则托盘开关一关一开，左键就废了。**
  systray 每次 `Run` 都建新窗口，而钩子的安装状态是包级变量：第二次
  `installLeftClick` 看到 `trayPrevProc != 0` 就直接返回，新窗口根本没被子类化，
  旧过程还指着那个已经不存在的窗口。要把原过程还回去并清掉状态。

- **托盘 tooltip 必须挂在引擎的事件出口上，不能挂在 `App.emitState` 上。**
  `runner.Configure(prov, emit, ...)` 传的是 `a.emit`；而更新 tooltip 的代码写在
  `a.emitState` 里，那个函数只有「开始」和「保存设置」会调 —— 暂停、停止、清空、移除
  全都绕过它，于是悬停文字从用户按下暂停那一刻起就再没变过，一直描述着一个不存在的
  队列。现在传 `a.emitEngine`，它对每个 `EventQueue` 都刷一次悬浮文字。
  顺带两条：**空队列要回落到空闲标题**（`SetTooltip("")`），"3/9 完成"挂在一个已经清空
  的队列上比没有信息更糟；`SetTooltip` 内部**相同文本直接丢弃**，因为每次调用都是一次
  同步的 `Shell_NotifyIcon` 到 explorer。
  另外 `Stats.Finished()` 是「已完成」的唯一算法：进度条和悬浮文字读同一个和，
  否则一个说 4/9、一个说 5/9，够查半小时。
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

### 底部面板的标签栏：等高、铺满、居中

`.panel__head` 用 `align-items: stretch` + 固定 `height: 34px`，`.tab` 自身
`padding: 0 12px` + `margin-bottom: -1px`（下划线压在 head 的 1px 边框上，而不是
浮在它上面 1px）。**不是 tab 的子元素必须 `align-self: center`**，否则 28px 的按钮
会被拉成 34px、hint 会变成整条高的块。以前是 `align-items:center` + 上下不对称的
`7px/9px` 内边距，三个标签各自为政地浮在一条更高的横条里。

面板高度 = 正文高度 + `PANEL_CHROME`（面板顶边 1px + 抓手 6px + 标签栏 34px = 41）。
改任一项都要同步这个常量，否则正文会比设置值少/多几像素。

正文高默认 **页面高度的一半**（`panelAuto()`，再减去 `PANEL_CHROME`，所以"一半"
指的是整个面板而不是正文），上限 3/4（`panelCap()`）。

像素值 `LogPanelHeight` **只在 `LogPanelSized` 为真时才生效** —— 也就是用户拖过
抓手之后。否则换一台机器、换个窗口尺寸打开，一个"在别处很合适"的数字会钉死这里的
布局。像素默认值不能是"一半"，因为它没见过那个窗口。

`mount()` 里要再调一次 `applyPanel()`：构造时 `el` 还没进文档、`getBoundingClientRect()`
是 0，`pageHeight()` 会退回 `window.innerHeight`，比真实页面高一个标题栏。

### 入队：点名文件和扫目录同一道闸

`AddInputs` 里**直接点名的文件以前不过 `isMediaFile()`**，只有扫目录才过 —— 于是
把一个 .zip 拖进窗口会真的建出一个 ffmpeg 永远打不开的任务：计数涨了、列表却没行
（它只广播 stats，不广播 job:update）。按用户的要求是不支持的文件根本不进队列，
并给出「xxx.zip: 不是媒体文件，已跳过」。

**添加文件后前端必须自己 `api.jobs()` 重拉**（`reportAdd` 与 app.js 的拖放回调都要），
否则那次新增只有计数变化、表格要等到别的事件顺带重绘才出现。
回归：`TestAddInputsRejectsFilesFFmpegCannotOpen`。

### 原生控件要跟着主题走

`html` 上必须写 `color-scheme: dark`。`.select` 的框是我们自己画的，但**弹出来的
列表、它的滚动条、焦点环是 WebView2 画的**，没有这一句它们按浅色渲染 —— 深色应用
里长出一个白下拉框。

`.input:focus, .select:focus` 里用的是 `background-color` 而不是 `background`：
简写会把 `.select` 的 `background-image`（下拉箭头）重置掉，聚焦时箭头就没了。

### 确认弹窗用 `.modal--dialog`，不要用通用 modal 的 chrome

通用 modal 的头 63px、脚 59px（头里那个 34px 的图标按钮撑的），正文只有 43px，
上下两条占了 73% 的高度；正文又是 `18px 上 / 6px 下` 的内边距 —— 合起来让那句话
比弹窗中心低了 8.6px，看着就是"不居中、偏下"。

`confirmDialog` 因此走自己的尺寸类 **`.modal--dialog`**：上面一行标题（无下边框，
关闭按钮 `position:absolute` 移出流以便标题居中 —— 注意 `spacer` 要 `display:none`，
否则它会把标题往左挤 14px）；**下面正文与按钮栏共用 `surface-2`、中间没有分隔线**，
读起来是一整块；两个按钮都是 82px 起的实心/tonal 药丸，右对齐，删除按钮用填充的
error 色突出。宽度 440px。

改任何一条都会把版面推回原样 —— 靠量（`getBoundingClientRect`），别靠眼睛：
现在的数字是「文字中心与弹窗中心差 0.2px」「标题盒中心与弹窗中心重合」。

### 前后对比单元格（`.cmp`）

分辨率 / 时长 / 大小三列在任务完成后显示"处理前 → 处理后"。做法是**两个格子叠在
一个单元格里**：上行是划掉的源值（`.cmp__from`），下行是新值加百分比
（`.cmp__to` + `.cmp__pct`）。以前是把 `源 → 目标` 拼在一行，两个数字糊在一起，
读者要猜哪个是结果。

`measureCells(job)` 返回的是**三段 innerHTML 字符串**，不是 `<td>` 标签。原因：
浏览器会把裸 `<td>` 从任何赋值容器里提出来，所以"拼一段 `<td>…</td>` 塞进
`createElement('tbody')` 再读 `children[i]`"这种写法**静默拿到 undefined**，
`patchJob` 会持续抛 `Cannot read properties of undefined`。返回字符串最省事也最稳。

### 筛选只在模板上（已删掉的队列级覆盖）

曾经在 `Runner` 上挂过一个"本批次筛选"覆盖（`SetQueueFilter` + `Settings.QueueFilter`
/ `QueueFilterSet`），理由是"这一批只要这些文件"看起来是队列的决定。**实际用起来是
第二份筛选面板**：它整段压过模板，而界面上只有一个 chip 提示，用户很难意识到自己
填的模板规则没生效。已整体删除——筛选规则只有一处，就是模板（跟随的模板则用全局的）。

### 批量移除

`Runner.RemoveJobs(ids)` **一把锁删完**，而不是循环调 `RemoveJob`：后者每次都
`emitState()`，勾 50 行就会重绘 50 次，中间还会闪出半空的列表。未知 id 直接跳过
而不是报错——队列在跑的时候行可能自己就没了。回归测试见 `jobs_test.go`。

**但后端只广播 stats（`queue:state`），不广播"行没了"**。前端 `on(EVENTS.queue)`
只刷新工具栏——所以删除曾经"成功了但界面不动"。修复在发起方：
`removeChecked` / 单行 `removeOne` / `clear-finished` 删除后调 `refreshJobs()`
重新拉 `Jobs()` 再 `renderJobs()`。**谁发起结构变更，谁负责重拉**；
不要指望 stats 事件替你重绘列表。

`removeOne` 的 `refreshJobs()` 还必须**无条件执行**：任务已经不在时后端会返回
错误，`await` 直接抛出，重绘就被跳过了——那正是"再点移除没反应"的来源。
"任务不存在"是唯一可以从宽处理的情况（想要的结果就是那行不在，它确实不在）。

### 行选中（文件管理器语义）

一套选中集（`local.checked`）驱动两样东西：复选框列和批量条。但**焦点是一个
独立的状态**（`ctx.state.selectedJobId`），普通点击只动它：

- **普通点击**：只把焦点移到这一行（下方日志/详情/命令跟着走），**不勾选**
- **勾选复选框**：加入/移出选中集，同时把焦点移到该行
- **Ctrl+点击 / Shift+点击**：加入移出一行 / 从锚点整段选中；这两个留在行上，
  因为"跨一段范围"是复选框表达不了的手势

锚点是 `local.anchorId`，复选框的 change 事件也要更新它，否则"先勾 A、
Shift 勾 C"会从过期的锚点起算。焦点行样式是**描边**（`is-selected`），
批量选中是**填充**（`is-checked`）——两个状态叠在一条行上时都要看得出来。

**普通点击曾经=勾选**，于是"看一眼日志"就变成"准备一次批量操作"，谁都不敢点。
两件事拆开后，勾选只剩复选框列在做。

**焦点变了，面板必须跟着变**：`reflectSelection()` 是所有移动焦点的地方
（点行、勾选、`onSelectJob`）共用的出口。复选框那条曾经只改 `selectedJobId`、
不刷面板，于是**一行文件的日志挂在另一行的名字下面**。

### 批量条没有自己的复选框

表头的全选是唯一的"全选"控件。批量条里曾放第二个复选框，勾它和勾表头
效果完全一样——两个控件管同一份状态，用户会疑惑哪个才是"真的"。

### 状态筛选

工具栏右侧的 `data-role=status-filter`，选项表在 `STATUS_FILTERS`
（`views/tasks.js`）里，每条带一个 `match(job)`，**同一个表既生成下拉也做过滤**。
分组按用户问的问题分（「哪些真的转了」= 完成+完成(警告)），而不是一个内部
状态一条。

几条必须守住的边界：

- **筛选是纯视图**：不进后端，不改变队列。工具栏的 `开始/停止/重试/清理` 一律看
  `ctx.state.jobs` 全量，只有表格和计数看筛选结果。隐藏了失败行还把「重试」变灰
  就完全说不过去。
- **计数写成 `3 / 9 个任务`**：只显示筛出来的数会让人以为别的文件没了。
- **全选 / 批量删除只作用于屏幕上能看到的行**（`local.renderedIds`，不是
  `ctx.state.jobs`）。"已选 N 项"里的行必须是看得见的行，否则「删除输出」会删掉
  用户根本不知道选中的东西。
- **换筛选条件时清空选中集**：上一条的第二道保险。
- **筛完一行不剩时要说实话**：队列非空就说「没有符合这个筛选的任务」，
  别说「队列里还没有文件」（`paintEmptyState`）。
- **`onJobUpdate` 要把状态变化当成结构性变更**：一行进了或出了筛选范围，只有整体
  重绘能表达，`patchJob` 做不到。判据是 `visible !== wanted`。

### 删除输出文件（`Runner.DeleteOutputs`）

**「移除」和「删除」是两件事，不能合成一个按钮**：丢掉一份可能跑了一小时才转出来
的文件，和把一行从列表上抹掉，不是同一个决定；而移除之后，那行——唯一还说得清
"这是什么、花了多久、用的哪条命令"的东西——也没了。所以 `DeleteOutputs` 只碰文件，
**一行都不删**。

- 行的留白由 `Job.OutputDeleted` 补上：一个打不开任何东西的「打开输出」按钮比没有
  按钮更糟，所以行要能承认文件已经不在。它和状态无关，是另一维的事实。
- **正在处理的任务一律拒绝**，返回一句能照做的中文（先暂停或移除）。ffmpeg 还开着
  那个输出，中途删掉等于让它往一个再也叫不出名字的路径上继续写；Windows 上多半
  直接删不掉。
- **源文件有独立的一道闸**：`samePath(out, input)` 就跳过。`ResolveOutput` 已经不会
  把源文件命名为自己的输出，但这里是**唯一真正删文件的地方**——守卫要守在动手的
  地方，不能只守在取名字的地方。
- **在内存里的那条记录也跟着走**（`dropProcessed`）。它是一句「有产物」的断言，产物
  没了它就不成立；留着会让之后的重跑被跳过。
- **"没东西可删"不是失败**：没有输出、文件已经不在、未知 id，全部计入
  `Skipped`；`Errors` 只放真的没办成的（忙、路径是目录、系统拒绝）。
- 删完 `r.logLine(job, "已删除输出文件: "+out)`：日志里留一条，和产出它的那次运行
  记在一起（`logLine` 顺带 `emitJob`，所以行上的 `OutputDeleted` 也一起广播出去）。
- 回归测试见 `internal/engine/delete_test.go`。

### 输出日志面板

底部面板的「输出日志」有三件事必须同时成立，缺一个都像坏了：

1. **跟着最新一行走**。判据是 `logAtBottom()`——**直接问滚动条**，不查记忆里的
   标志位。滚动事件要到下一个渲染步才派发，所以"刚往上翻了一点"的人**已经**不在
   底部了，可事件还没到；照着过期的标志位决定，就会把正在读二十行前的人拽回末尾。
   这里曾经有两个自造标志位的版本，都在这一点上翻车：一个 `selfScroll` 用 rAF
   清标志，正好把同一帧内到达的用户滚动吞掉；一个"意图 + 位置"的双判据，又在合成
   测试里被事件顺序打乱。**结论：位置直接量，别缓存。**
2. **测量必须在插入之前**。插进去之后 `scrollHeight` 已经涨了，任何追加看起来都
   像"用户翻走了"。
3. **看不见的日志不能丢**。日志只能在它的标签页上绘制，所以切到「处理详情」期间的
   行先攒在 `heldLog` 里，切回来（或整页切回来，见 `mount()`）再补上。
   `local.followLog` 只服务于这一条：**一个 `display:none` 的视图没有盒子，
   "在不在底部"无从谈起**，回来时只剩"用户的意图"可以依据。

另外两条：

- **`loadLog` 用 ticket 防竞态**：连点两行，慢的那个回复后到，面板会显示你已经
  点走的那份日志。
- **颜色分类只有一份实现**（`logLineCls`）。追加和重载曾经各带一套正则，早就漂了：
  同一行流过去时是红的，重新加载后是灰的。
- **删掉焦点行要清面板**（`clearLog()`，同时把 ticket 加一，让在途的回复作废）。

回归：`tools/_drive_ui.html` 式的无头探针（mock 的 `job:log` 定时器会真的流日志，
真实后端 ≥120ms 一批）。看 `top / clientHeight / scrollHeight` 三个数，不要靠截图。

### app.js 的 `job:update` 只改不加

`on(EVENTS.jobUpdate)` **不得**在列表里找不到这个 id 时 push 进去。行只会因为
"重新拉了一次列表"而增加，而每一次结构变更（boot、添加文件、拖入）都会拉。

这条曾经是 `if (i >= 0) 更新 else push`，后果是一整类"移除之后又回来了"的怪事：
后端删除只广播 stats，前端自己重拉列表；此时**还在路上的**一条 `job:update`
落到一个已经没有这个 id 的列表上，就悄悄把那行加了回来——之后再点「移除」毫无反应，
因为后端早就不认识这个任务了。

同一个坑还有一半在 mock 里：`api.js` 的 `mock` 对象**被重复定义过五个键**
（`RemoveJob` / `RemoveJobs` / `RemoveFinished` / `ClearQueue` / `RetryFailed`
先是真实现、后面又出现 `() => 0`），**对象字面量的同名键后者胜**，于是浏览器预览里
的「移除」一直是空操作——**这正好把上面那个 bug 挡住了，靠 mock 截图永远看不见它**。
往 mock 里加键之前先搜一遍有没有同名。

### 模板继承：全局默认值

→「输出与命名 / 处理性能 / 匹配条件 / 错误与警告」曾经是全局设置，现在全部
挂在模板上。`store.GlobalTemplateID`（`t-global`）那个模板只提供默认值，
**它就是列表的第一项**（`.tpl-row--global`），和其余模板共用一个滚动区与
一套行样式 —— 曾经给它单独做了一张虚线卡片（`.tpl-global`），那是"面板叠面板"，
后来连卡片一起删了。它不可拖动、不可排序、不可删除，这一点靠 `global` 标记
在 `renderList()` 里过滤，样式只是把这件事再显式一遍。

列表头现在只有两行：第一行左边三个整列工具（导入 / 导出 / 恢复内置），右边
「新建模板」；第二行是搜索框。曾经有个"模板"标题，加下面一行分隔线里还有
一个"模板"，加上页面标题 —— 同一个词在屏幕上出现三次。

五段都有「与全局不同」开关，继承有两套机制，因为字段语义不同：

- **处理性能 / 已处理过的文件 / 匹配条件 / 错误与警告** 整段回落（`*Spec` 指针，
  nil = 跟随）。因为 `0` 在这些字段里本身有意义 ——「不限制体积」和「跟随全局」
  必须能区分开。
- **输出与命名** 由 `Template.OutputOverride` 决定：开关关着就**整段丢弃**模板
  自己写的值，开着才逐字段留空回落（`Template.Effective`）。段内没有「0 有意义」
  的字段，所以不需要指针。

**「与全局不同」是唯一的跟随开关**，下拉框里不再有「跟随全局设置」那一项。一个状态
给两个控件，界面就会出现「看着在跟随、命令里却是别的值」。因此段内字段在渲染时要用
`orGlobal(value, globalValue)` 兜底：Go 侧把空值当回落处理，前端就得显示回落后
那个真实值，否则 `<select>` 会停在第一个选项上，和命令对不上。

`OutputOverride` 一定要在 `Effective` 里**先清空再回落**。只做逐字段回落的话，
一个关着开关却残留旧值的模板，界面写着「跟随全局」而命令用的是旧值 ——
两边说法不一致。`Normalize` 里有一段兼容：老文件里有输出值但没这个标记的，
自动当作「开关已开」，否则升级会把它们的输出规则悄悄退回全局。

`Effective` 返回的模板**共享全局模板的段指针**，只读。不要对它调 `Normalize`，
那会写穿到全局模板上。要改就 `ClonePerf` / `CloneExisting` / `CloneFilter` /
`CloneProblems`。

前端镜像：`views/sections.js` 的 `effective(t, g)`，与 Go 侧必须同步
（任务页的筛选 chip 要在本地描述"这条任务实际会怎样"）。段的显示顺序也只在
`sections.js` 的 `SECTIONS` 里定义一次。

### 已处理过的文件（`ExistingSpec`）

**判据是本次运行自己的记录，不是「输出位置有个文件」。** 记录就是 `Runner` 上的一个
map（`processedThisRun`，键 = `(输入路径小写, 模板 id)`，见 `processedKey`），在 step 6
验证产物之后写入，`handleProcessed` 只读它。除此之外什么都不看。

**记录不落盘，这是刻意的。** 磁盘上的记录会活得比它的理由更久：文件夹整理过、产物被
手动搬走之后它仍然成立，于是用户想处理的文件被默默跳过，而界面上没有任何东西能解释
为什么。内存记录的代价是不跨进程 —— 而这正好是想要的行为：关掉程序重开，同一个文件
照常处理。一句话能说完的规则，用户也能自己验证。

于是**不存在任何「这个文件以前处理过」的磁盘证据**：产物旁边的零字节残留、被打断的
半成品，都不参与判断 —— 它们只会被下一次运行的 `-y` 覆盖掉。（判据曾经是「输出位置
有个非空文件」，那是在猜是谁把文件放在那里的：用户自己拷过去的、别的工具生成的，全都
被算成「我们已经处理过」。）

`Action` 只有三种：`""`（不处理，留在原处）/ `ActionMove` / `ActionCopy`，与
「匹配条件」的被排除文件、「错误与警告」的错误 / 警告文件**完全一致** —— 三段作用的
对象不同（已处理过的文件 / 被筛掉的源文件 / 出问题的源文件），但规则读起来一样，
不需要各记一套。

作用对象是**源文件**，不是那份旧输出。这也正是为什么 `ResolveOutput` **不用**
`EnsureUnique` 给占用路径换名 —— 悄悄换个名字等于把同一份文件编码两遍，而界面上看不出。

**两条路径共用一个搬迁函数**（`existingRelocate`），因为「移动到目标目录」在两处都要发生：

- **跳过路径**（`handleProcessed`）：本次运行里已经处理过这个文件，本次不编码，只安置
  源文件。安置失败 → 任务失败（`stopJob`）。
- **完成路径**（`fileProcessedSource`）：本次真的编码完了，记下这一笔，就地安置源文件。
  安置失败 → 只加一条 warning（编码是成功的，不该把好结果标红）。
  调用点在 step 6 验证产物之后、写状态之前，所以那条 warning 能进 `Warnings` 并让
  状态自然变成 `warning`，而不是事后往一个已经写着「已完成」的行上贴注释。

只有跳过路径的版本曾经是个死设置：跳过依赖一个第一次跑不可能有的触发条件，于是
「移动到目标目录」编码照跑、源文件原地不动 —— 用户报的「我设置移动到其他目录了，
这边处理完并没有移动」就是它。

四处容易写错的地方：

- **搬的是源文件，所以锚点是源目录树**，`Relocate` 的 `SrcRoot` 传
  `job.SourceRoot`（用户添加的那个目录）。
- **记录要能自己失效**，否则它会挡住用户明确想要的重跑。三个出口，缺一个就会出一个
  「明明改了参数却什么都没发生」的 bug：
  - `dropProcessed` —— 「删除输出文件」时清掉这一个（记录是「有产物」的断言，产物没了
    它就不成立）。产物已经不在的那条分支也要清。
  - `ForgetTemplate` —— `SaveTemplate` 时清掉这个模板的全部记录。改了编码参数再重跑，
    那条记录只对改之前的参数成立。
  - `ForgetAllProcessed` —— `SaveGlobalTemplate` 时要清空，因为「基本配置」喂着所有
    模板的默认值。
- **记录是「同一个文件 + 同一个模板」**，所以换模板天然会重跑；`AddInputs` 按输入路径
  去重，同一个文件在队列里只会有一行，所以跳过路径出现在**移除那一行再重新加进来**
  的时候（重扫整个目录、只想处理新增文件）。
- **这一段在全局模板里默认是 nil（不启用）**。非 nil 就一定会跳过已处理的文件，
  而「同一批只处理一次」是少数人要的默认行为 —— 内置一个非 nil 的段等于给所有人
  悄悄改了默认。段开关打开时前端从 `seedSection` 拿到 `keep` 作为起点。
  **记录也只在这一段非 nil 时才写**：不启用这一段的人根本不会读它。
- 回归测试见 `internal/engine/existing_test.go`、`internal/engine/delete_test.go`。

### DestRule：五段输出共用一套规则

主输出、筛选排除的文件、已处理过的旧输出、问题文件都用
`store.DestRule{Mode,Dir,Suffix}`，由 `store.ResolveDestDir` 统一解析成目录。四种模式：

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

所以默认命名模板是 `{name}` 而不是 `{name}_out` —— 目录已经带了标记，
文件名再加一遍纯属噪音。命名模板只表达**文件名**：扩展名由「输出格式」（容器）
决定，`ResolveOutput` 发现展开结果没有扩展名时会自动补上 `outExt`。

**「有没有扩展名」这个判断本身是错的**：`filepath.Ext("qqq.123")` 返回 `".123"`，所以
「最后一个点之后的东西」不一定是扩展名。命名模板只表达文件名，扩展名由「输出格式」决定
—— 该问的只有一件事：这个名字**是否已经是我要写的那个扩展名**（`EnsureExt(name, ext)`）。
踩过的症状：源 `qqq.123.mp4` + 默认 `{name}` 展开出 `qqq.123`，旧代码判成「已有扩展名」
跳过追加，落盘成没有扩展名的 `qqq.123`，ffmpeg 推断不出 muxer。`ReplaceExt` 只适用于
「最后一个点一定是扩展名」的场合；`ResolveOutput`（含源文件自保护分支）与 `Relocate`
三处同源修。

**两套变量，别合并成一个**（第 26 批）：`ExpandOutputPattern` 给「输出与命名」，**没有**
`{ext}` —— 扩展名由容器决定，多一个 `{ext}` 就等于让文件名同时依赖两个设置，而症状
只会在换容器之后才显形。`ExpandPattern` 给被搬走的文件（匹配条件排除的、已处理过的
源文件、错误 / 警告），它们不重新编码、保留自己的扩展名，那里 `{ext}` 仍然有意义。
输出侧的 `expand` 会把残留的 `{ext}` 删掉（老模板写着 `{name}.{ext}`），
因此还要处理 `clip.` 这种只剩一个点的名字，否则补后缀时得到 `clip..mp4`。

命令预览（`app.go` 的 `buildPreview`）在读不到源文件时会用示例媒体，**这条路也必须走
`ResolveOutput`**。早先它直接输出 `{输出}` 占位，于是命名模板和输出容器在预览里完全
看不见，`{name}` 显示成没有扩展名的样子 —— 逼着用户把 `{ext}` 写进模板才能看到结果。

「输出格式」（`Container`）放在「输出与命名」段里、与命名模板同一行，但它**不是**
可继承字段——每个模板自己选容器。所以段处于跟随状态时它单独留在原位（
`inheritableSection` 里 `!active` 才渲染独立行；展开时由 `outputBody` 渲染，
两处只会出现一处，否则同名控件渲染两份会各改各的）。

### 分辨率：只写钉死的那一边

`computeScale` 在长边 / 短边（以及只填了宽或高的 exact）模式下，把用户钉死的那
一边写进 `scale=`，另一边交给 ffmpeg：`-1` 保持比例，`-n`（对齐倍数 N）保持比例
并取整到 N 的倍数，默认对齐 2 即 `-2`。程序不再自己算另一边 —— 算出来的两个数
交给 ffmpeg 与只交一个数相比，只是把猜测从 ffmpeg 挪进了程序。返回的
`TargetW/TargetH` 仍是同一公式算出的估算值，供预览与记录展示。

缩放算法为空 = 不写 `:flags=`（ffmpeg 默认 bicubic）。`Normalize` 因此**不能**
把空算法补成 lanczos，那等于替用户做了一次没被要求的选择。

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

「未设置」在界面上只有**一种说法**（`app.go` 的 `DefaultOptionLabel`）：
下拉框 =「默认（由 ffmpeg 决定）」，占位文字 =「留空 = 默认」，数值 0 =
「保持原样」或「不限」。同一个意思写四种词，用户会读成四种行为。
`Normalize` 同理不能把空的 `Resize.Algorithm` / `Preset` 补成具体值。

### 全局模板永远不能绑定任务

`a.templates` 里 **全局模板是排在第一位的**（`ReorderTemplates` 强制置顶），
所以任何「取第一个模板兜底」的写法都会把任务绑到 `t-global` 上 —— 它只有默认值，
处理不了文件，而且界面上会出现「工具栏是 A、任务是 全局模板」的错位。
`App.enqueue` 的兜底必须 `!t.Global` 过滤；前端 `syncTemplateOptions` 在回落时
要把选中的 id 写回 `settings.lastTemplateId`，别让后端自己去猜。

### 输入框随选项联动（前端）

`templates.js` 的 `ENABLED_BY` 声明「哪个下拉的哪个值点亮哪些输入框」；
`refreshEnabled()` 在渲染后和每次变更后把无关的框置灰（`disabled` +
`.is-disabled`），**不整表重渲染** —— 重渲染会抢走焦点、打断正在输入的内容。
一个字段被多条规则提及时，必须每条都允许才算可用。

### 两处命令必须同源

| 入口 | 模板来源 | 媒体信息来源 |
|---|---|---|
| runner（真实执行） | job.TemplateID | 运行时探测 |
| 任务面板「命令预览」`PreviewCommand` | job.TemplateID | 实时探测 |

工具栏那个"给整批任务生成命令"的 `PreviewQueue` 已经删掉了：它按工具栏模板另
算一份，与任务面板天然可能不同，而用户只读得出"两份命令打架"。现在切换模板
直接改 `job.TemplateID`（见下），两处也就没有第二份可算了。

这两处一旦各算各的，用户就会看到"两份都说得通的命令互相矛盾"。已定的规矩：

- **工具栏切换模板立刻改 `job.TemplateID`**（`SetAllTemplates` →
  `Runner.UpdateAllTemplates`），不是"只影响之后添加的文件"。除了**正在运行/
  准备中**的那条（中途换参数只会产出半旧半新的文件），其余全部跟着走，
  已完成的回到 `pending` —— 否则留下一份结果，却没有任何地方写着它是按什么
  参数做出来的。返回 `ApplyResult{Applied, Requeued}` 供前端如实提示
- **`emitState()` 只广播 stats**，不广播任务列表。结构变更（改模板、删除）
  之后前端必须自己 `refreshJobs()`，否则行里会一直显示旧的模板名
- **`{index}` 用 `job.Index`**（入队顺序，1 起，删除不重排）。预览以前用切片
  下标、runner 根本不传 —— 同一个变量两处展开出不同文件名
- **探测失败回退到 `sampleInfo()` 必须写进 Warnings/Notes**，"按 3840×2160 示例
  计算"。静默替换就是"两块面板数字对不上且都看不出谁错"的头号来源
- 模板页的预览走 `PreviewTemplate(draft)`：按 id 查只能拿到**已保存**的模板，
  那条"未保存的修改也会体现在这里"的说明原本是假的

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

### 队列的启动与暂停（`armed` / `paused`）

**两个状态，不是一个。** `paused` 是"跑起来了、按住不放"；`armed` 是"用户点过开始"。
worker 池在加载模板时就建好（`EnsureWorkers`），所以只有让 `take()` 额外看
`r.armed && !r.paused`，才能做到"拖入文件只排队、不开跑"。

- `Start()`：`armed=true` + `paused=false`，这是**唯一**能让任务跑起来的入口
- `CancelAll()`（停止）：`armed=false` + `paused=false`。之后加进来的文件重新排队等开始，
  而不是接着上一次的进度继续
- `Stats.Started` 给前端区分「未开始」和「已暂停」—— 没启动过的队列按暂停处理时，
  「暂停」按钮是个点了看不出区别的死按钮（这正是第 25 批前它"无效"的原因）

`Stats` 的 `Started` 与 `paused` 必须一起看：只报 `paused` 的话，界面无法区分
「还没开始」和「按住了」。

### 暂停 = 真的挂起 ffmpeg 进程

「暂停」不只是按住队列——**正在转码的那个 ffmpeg 进程也会被冻住**
（`internal/sysx/suspend_windows.go`，`NtSuspendProcess`）。ffmpeg 没有暂停动词，
杀掉会丢掉已转的帧，"从上次进度重跑"需要再编码一遍并在容器层拼接，产出的文件
不是原来那个。只有冻进程才是真暂停。

- **句柄必须带三个权限**：`PROCESS_SUSPEND_RESUME`（挂起）、`PROCESS_TERMINATE`
  （退出时杀）、`PROCESS_QUERY_LIMITED_INFORMATION`（`GetExitCodeProcess`，
  `Alive()` 靠它）。**少第三个 `Alive()` 会把每个冻住的进程都报成已死** ——
  唯独这个函数的职责就是分清这两者，方向反了最糟。
- **内核挂起计数**：按两次「暂停」需要两次「继续」。`Pause()` 因此跳过已持有
  句柄的任务（`j.suspend != nil`）；不跳的话用户狂点按钮后界面显示「已继续」，
  进程却永远冻着。
- **`Pause` 传 job 指针，不传 pid 去查**。Windows 会回收 pid：前一个 ffmpeg 退出、
  下一个任务的 ffmpeg 拿到同一个号，按 pid 认领就会**冻错行**，「继续」去解冻一个
  没人挂起的进程，真正的那个还冻着。`attachSuspend` 校验 `j.pid` 没变再认领，
  变了就 `Resume()` 掉那个散进程。
- **`killSuspended()` 在 `Shutdown()` 和 `CancelAll()` 里都要调**。取消 context 对
  冻住的进程无效（没有可运行的线程去观察取消），只关管道的话，用户关掉程序后
  面对的是一个**孤儿 ffmpeg** 占着 CPU 和输出文件。
- 进程句柄走 job 锁，但**绝不跨 OS 调用持锁**（`OpenProcess` 期间会挡住进度
  读取那条 goroutine）。`runningJobs()` 先在 runner 锁下复制列表再逐个加锁，
  两把锁不嵌套。
- 恢复失败时 `markFrozen` **保留徽标**并写「已暂停（恢复失败）」：显示成「处理中」
  配一条再也不动的进度条，用户只会以为界面坏了。

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

### 旧配置迁移（已删除）

曾经为"全局配置还在 `settings.json` 里"的旧版本保留过一条迁移路径：`Legacy*` 字段
（扁平键 + 嵌套的 `filters` 对象）、`NewGlobalFromLegacy` / `ClearLegacy` /
`HasLegacy`，启动时读一次建出全局模板再清空。**这套已经整体删掉**——现存的
`settings.json` 里早就没有那些键了，留着只是让每个读配置的人都要绕一遍。

当时踩过的坑记在这里，因为同样的写法还会再遇到：`encoding/json` **不支持点号键**
（`json:"filters.minSizeMB"` 会被当字面键名、静默读不到），嵌套结构必须真嵌套。

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
- **界面上的参数说明只写「这一项是什么」**，一句话，能省则省。用户明确要求过两次：
  说明里不许出现设计理由（为什么当初这么设计、为什么没有 `{ext}`）和版本变更叙述
  （以前怎样、现在改成怎样、不再如何）—— 那是给开发者看的，写进 `DEVELOP.md` 和
  代码注释里，不要写到参数旁边。
- 判断一条说明该不该留：**它是否说了标签和控件本身没说的事**。`最小体积` 旁边的
  「留 0 表示不限制」值得留；「小于该体积的文件不处理」是把标签念了一遍，而方向性
  的话（是保留还是排除）只在段首说一次，六个字段各说一遍必然写歪一次。

## 已知环境限制

在受限的 Job Object 中运行时，WebView2 的所有子进程会以 `exit_code=7` 崩溃
（Chromium 拒绝嵌套 / 脱离）。这是执行环境的限制，不是程序缺陷 ——
用户手动启动一切正常。`--no-sandbox` 可以绕过，但**不要**写进产品代码。
