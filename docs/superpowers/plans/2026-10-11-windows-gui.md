# Windows GUI Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking. 如用户选择逐任务子代理实施，则使用 superpowers:subagent-driven-development。

**Goal:** 交付可双击启动、能连接合成对端并完成受控读写与 SQLite 记录的 Windows x64 GUI 预览版。

**Architecture:** Fyne 中文单窗口调用 `internal/service` 会话控制器，直接复用现有 S7 驱动、系列档案、模拟器和 Recorder。控制器串行执行同一会话操作并维护取消、代次和不可变快照；UI 只在 Fyne 线程刷新。原生入口和 PowerShell 打包脚本复用现有字体、图标与高 DPI 资源。

**Tech Stack:** Go 1.26、Fyne v2.8.1、既有 SQLite Recorder、Windows x64 MinGW GCC 16.2.0、go-winres v0.3.3。

**Spec:** `docs/superpowers/specs/2026-10-11-windows-gui-design.md`

## Global Constraints

- 单工作区；本次交付与运行验证限定 Windows x64；无需安装 Go。
- GUI 包使用 `0.1.0-alpha.2` 预览标识；Windows 数字文件版本为 `0.1.0.0`。
- 默认禁用写入；用户勾选允许写入并确认目标、类型和值后才执行；不会自动重发写入。
- 保留 CONFIRMED、REJECTED、UNKNOWN 及各段状态；CONFIRMED 后执行一次独立读回。
- 本机演示仅监听 `127.0.0.1` 的实际空闲端口，并标注为合成协议对端。
- 默认记录目录 `%AppData%\S7AIStudio\records`；沿用按天/50 MB 轮转，记录失败可见。
- 本次先交付本地构建产物；已有 alpha.1 发行版资产不覆盖。
- 不添加多工作区、点表导入、历史浏览、AI、Web、其他协议或自动更新。

## Review Focus

1. 断开、重连或关闭发生在拨号/读取中：旧操作不能覆盖新会话结果，关闭不能死锁（Task 1）。
2. FALSE、浮点零、64 位整数与数组/RAW/STRING：精确显示并按实际编码比较读回（Task 1、2）。
3. 后续批次传输失败与单项服务拒绝：保留 GOOD 已完成项，失效会话停止轮询（Task 1）。
4. 数据库初始化、后台写入或队列丢弃失败：立即显示保存错误，正常退出仍排空剩余记录（Task 1、2）。
5. 小窗口、高 DPI、中文字体与干净 Windows：核心控件可操作，绿色包不依赖 MinGW DLL（Task 2、3）。

---

### Task 1: 可取消的会话控制器与演示/记录生命周期

**Files:** Create `internal/service/config.go`, `value.go`, `controller.go`, `operations.go`, `demo.go` 及同名测试；Modify `internal/recorder/recorder.go` 及 `recorder_test.go`（公开线程安全保存状态）。

**Interfaces:**
- Consumes: `profile.Lookup`, `ConnectionTSAP`, `ResolveAddress`; `s7.Dial`; `device.Driver`; `recorder.OpenDaily`, `StartSession`, `Record`, `EndSession`, `Close`; `simulator.New`, `Serve`, `Close`。
- Produces: `Config{Family, Target string; Rack, Slot int; LocalTSAP, RemoteTSAP string; Timeout time.Duration; VDB int; VOffset uint32}`、`DefaultConfig() Config`、`Point{Address string; Type device.DataType; Count int}`。
- Produces: `ParseValue(device.Address, string) (device.Value, error)`、`FormatValue(device.Value) string`；编码仍由 `device.EncodeValue` 校验。
- Produces: `WriteRequest{Point Point; Value string; Generation uint64}`；`Options{RecordDir string; Dial func(context.Context, string, s7.Options) (device.Driver, error)}`；`New(Options) *Controller`；`Connect(context.Context, Config) error`、`Disconnect() error`、`Read(context.Context, []Point) error`、`Write(context.Context, WriteRequest, bool) error`、`StartPolling(context.Context, []Point, time.Duration) error`、`StopPolling()`、`StartDemo(context.Context) error`、`StopDemo() error`、`Snapshot() Snapshot`、`Close() error`。
- `Snapshot` 包含单调 Revision、Generation、State/Target/Family/DemoTarget、Polling、Results、WriteResult、Readback、最近 256 条 Packets 与 100 条 Logs、RecordDir/RecordError/Dropped；返回深拷贝。Readback 固定为一致/不一致/无法确认。
- Produces: `recorder.(*Recorder).Health() (int64, error)`，安全返回 Dropped 与最近保存错误。

- [ ] **Step 1: 写行为测试。** `TestConfigRejectsBeforeDial` 断言空 host、非法端口/TSAP、缺失 V 映射不拨号；`TestValuesPreservePrecision` 断言 FALSE/零/LINT 极值、数组、RAW、STRING 编码显示；`TestWritePermissionAndReadback` 断言未批准不写、确认后只写一次、编码匹配/不匹配；`TestUnknownNeverRetries` 断言丢 ACK 为 UNKNOWN 且写计数为 1。`TestPartialReadStopsPolling` 保留已完成 GOOD；`TestDisconnectSuppressesLateResult`、`TestCloseCancelsDialAndRead` 验证取消、代次和限时退出。`TestDemoRecorderLifecycle` 实际回环读 42/写 123，关闭后端口释放且 SQLite 会话结束并保留真实报文；`TestRecorderHealthReportsFailure` 验证后台失败及丢弃可见。
- [ ] **Step 2: 验证 RED。** Run `go test -tags ci ./internal/service ./internal/recorder`；Expected: 新 API 缺失或对应行为断言失败，现有 Recorder 测试不因改动被削弱。
- [ ] **Step 3: 实现接口。** 配置先校验，单会话操作可取消串行；Disconnect/Close 先取消并关闭驱动，再等待后台操作，防止锁反转。有效会话出错以实际传输状态判断失效。轮询间隔不得小于 100 ms。写入仅接受一个点位，未明确批准或请求代次与当前连接不符直接拒绝；确认写后读回一次，以 EncodeValue 字节判断一致。快照只发布当前代次；原始事件仍归档。演示资源由控制器持有，记录失败不阻断诊断但必须显示失败。
- [ ] **Step 4: 验证 GREEN。** Run `go test -tags ci -count=1 -timeout 5m ./internal/service ./internal/recorder`；Expected: 全部通过，无遗留监听器或会话。
- [ ] **Step 5: Commit。** `git add internal/service internal/recorder`；`git commit -m "feat: add desktop S7 session controller"`。

### Task 2: 中文 Fyne 窗口及可审阅操作流程

**Files:** Create `internal/ui/desktop.go`, `connection.go`, `points.go`, `diagnostics.go`, `desktop_test.go`。

**Interfaces:**
- Consumes: Task 1 的 `Controller`、`Config`、`Point`、`Snapshot` 与 `FormatValue`。
- Produces: `NewDesktop(fyne.App, *service.Controller, string) *Desktop`；`(*Desktop).Window() fyne.Window`、`Show()`、`Shutdown() error`。后台刷新用 `fyne.Do`，关闭时取消刷新并关闭控制器。

- [ ] **Step 1: 写软件渲染行为测试。** `TestDesktopReadOnlyByDefault` 断言写按钮禁用、无连接时读取禁用、版本/预览标识可见；`TestDesktopDemoAndWriteConfirmation` 通过真实服务启动演示读 42，勾选写权限仍需确认，取消确认不写，确认显示 CONFIRMED 与一致读回；`TestDesktopResultsAndRecordingError` 断言 false/0/精确整数与保存错误可见。`TestDesktopCompactLayout` 在 1000×720 软件画布生成中文截图并核查主要操作区可见。
- [ ] **Step 2: 验证 RED。** Run `go test -tags ci ./internal/ui`；Expected: NewDesktop/API 缺失或新控件行为失败。
- [ ] **Step 3: 实现窗口。** 顶部版本、连接与演示状态；高级配置用展开面板；点位区支持单次读/开始停止轮询、类型与数量；表格逐项保留质量/错误。写入确认展示当前会话目标、地址、类型、值并绑定当时的代次，期间会话变化则拒绝。报文/日志放在页签，使用可滚动视图，报文字节完整显示，列表有界。UI 收集输入后启动后台调用，按单调快照刷新，所有控件操作在 Fyne 线程执行。
- [ ] **Step 4: 验证 GREEN。** Run `go test -tags ci -count=1 -timeout 5m ./internal/ui ./internal/service`；Expected: 全部通过并生成可检查的软件渲染截图；检查中文、错误提示及控件布局。
- [ ] **Step 5: Commit。** `git add internal/ui`；`git commit -m "feat: add Chinese S7 desktop workspace"`。

### Task 3: 原生 Windows 入口、绿色包及最终验证

**Files:** Create `cmd/s7-ai/main.go`, `build/windows.ps1`, `platform/windows/README-gui.txt`；Modify `README.md`, `docs/development.md`；复用 `assets/icon/windows/winres.json`。

**Interfaces:**
- Consumes: `ui.NewDesktop`、`service.New`、`platform.Setup`, `PrewarmFonts`, `ConfigureApp`, `WorkArea`, `FitSize`、`icon.Application()`。
- Produces: `build/windows.ps1 -Version 0.1.0-alpha.2 -CompilerRoot D:\tools\winlibs-gcc16\mingw64`，输出 `dist/windows-gui/S7AIStudio-0.1.0-alpha.2-Windows-x64.zip` 与 SHA256SUMS、build-manifest、验证报告/截图。数字资源版本固定 `0.1.0.0`，UI/说明保留完整预览版本。

- [ ] **Step 1: 核对启动/关闭合同。** 入口先配置平台与字体，再创建 app/controller/window；关闭拦截在后台 Shutdown 后经 Fyne 线程退出，保存失败写入 app.log。提供显式本机验证模式 `--smoke-test <目录>`，由应用自身执行演示读 42、受控写 123、读回、原生 Canvas 截图、SQLite 排空后输出 JSON 并退出；普通启动不自动连接或写入。模式只使用自有合成对端。
- [ ] **Step 2: 实现入口与 PowerShell 打包。** 定位并验证 GCC，临时配置 CGO/CC/PATH；资源构建和链接后补写高 DPI/icon/version；使用 `-H windowsgui`、`no_emoji`、静态 MinGW 运行库。每次仅更新本版专用 dist 子目录，说明采用 UTF-8 BOM，检查无加密标头、无占位符，再 ZIP/hash。文档注明实际能力、记录路径、明确写入操作与实机未验证。
- [ ] **Step 3: 全仓检查。** Run `go vet -tags ci ./...`、`go test -tags ci -count=1 -timeout 15m ./...`；使用上述 MinGW 和 CGO=1 执行 `go test -race -tags ci -count=1 -timeout 15m ./internal/service ./internal/ui ./internal/recorder ./internal/s7`。Expected: 全部通过；不为未运行的平台作通过声明。
- [ ] **Step 4: 构建并检查实际产物。** Run `powershell -NoProfile -ExecutionPolicy Bypass -File build/windows.ps1`；Expected: x64 GUI ZIP。使用 objdump/VersionInfo/go-winres 检查 PE amd64、GUI subsystem、DLL imports 无 MinGW 运行库、图标与 per-monitor-v2 清单、数字文件版本。解压 ZIP 后运行 `S7AIStudio.exe --smoke-test <专用目录>`；Expected: 原生窗口演示读 42、写 123、读回一致、截图可读、无退出后进程/端口、记录会话结束。复算 ZIP SHA-256 与清单一致。
- [ ] **Step 5: Commit 与终审。** `git add cmd/s7-ai build/windows.ps1 platform/windows/README-gui.txt README.md docs/development.md`；`git commit -m "build: package Windows GUI preview"`。使用 executing-plans 要求的新上下文整分支审查；重要问题用 RED→GREEN 修复后重跑相关及全仓检查。交付本地 ZIP、校验值、验证报告和限制；保留实现分支以便后续发版。

## 执行方式与交付

推荐当前会话直接实施：三个任务共享控制器快照与生命周期，直接实施便于保持接口一致，最后再做独立整分支审查。用户确认本计划与执行方式后连续完成各任务，中间不再逐任务询问是否继续。所有实现放在隔离工作区；保留现有 alpha.1 资产和旧计划工作目录。
