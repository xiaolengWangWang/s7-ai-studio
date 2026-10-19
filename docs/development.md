# 开发说明

## 模块边界

| 目录 | 职责 |
| --- | --- |
| `internal/device` | 地址、带类型数值、驱动接口、不可变事件快照 |
| `internal/profile` | 系列路线、rack/slot 与 TSAP 建议、显式 V/VM 映射 |
| `internal/s7` | 有边界的 TPKT/COTP/S7 解析、串行会话、多项读写及 PDU 分批 |
| `internal/simulator` | 独立解析请求的合成 TCP 对端、内存和故障注入 |
| `internal/recorder` | SQLite 存储、轮转、迁移与旧历史只读查询 |
| `internal/cli`、`cmd/s7-cli` | 输入校验、文本/JSON 输出、写权限和记录生命周期 |
| `cmd/s7-sim` | 模拟器参数、监听和退出生命周期 |
| `internal/service` | 会话取消与代次、串行读写、轮询、独立读回、演示与记录生命周期 |
| `internal/ui`、`cmd/s7-ai` | 中文 Fyne GUI、主线程刷新、写入确认、Windows 原生入口 |

通用业务接口不使用站号或 Modbus 寄存器表示西门子地址。旧协议字段仅放在历史事件的 `Legacy` 中。Recorder 的新元数据带版本；没有元数据的旧库读取时动态检测列，不会因查看而修改文件。

Read 在发送前校验全部地址，按请求和回应预算分组。已完成项即使后续批次断线也保留 GOOD；未完成项为 UNKNOWN，服务拒绝项为 BAD。跨 PDU 的读取不是原子快照。Write 使用原生位写入保护相邻位；大写入分段，不能保证原子性。没有自动写重试，也没有自动连接重试。`s7.Options.ReadRetries` 为后续服务层重试预留，目前不会触发重试；传输超时会使会话失效。

同一连接的请求串行执行；等待中的操作可取消，Close 可打断阻塞读取。Observer 在释放会话锁后接收深拷贝事件。远端身份始终未经验证，选择的系列档案不能作为 CPU 识别证据。

## 验证命令

```powershell
go mod tidy
go vet -tags ci ./...
go test -tags ci -count=1 -timeout 15m ./...
go test ./internal/s7 -run '^$' -fuzz FuzzDecodePDU -fuzztime 10s
go build -o bin/s7-cli.exe ./cmd/s7-cli
go build -o bin/s7-sim.exe ./cmd/s7-sim
```

固定黄金报文来源见 `internal/s7/testdata/README.md`。测试同时使用手工报文对端和独立请求解析模拟器，覆盖碎片/粘包、长度上限、PDU 引用、部分失败、超时、取消、关闭、原生位写入、写回应丢失和数据库兼容。CLI 测试通过真实回环 TCP 调用驱动。

CLI 可使用 `CGO_ENABLED=0` 的纯 Go SQLite。Windows GUI 和 race 检测使用 CGO=1；本机 MinGW 位于 `D:\tools\winlibs-gcc16\mingw64`，CI 的 Windows 原生打包脚本也可从 PATH 查找 gcc.exe。测试使用 `-tags ci` 的 Fyne 软件驱动；Linux/macOS CI 使用 race，Linux 另检查纯 Go 核心。实际远端结果以 GitHub Actions 为准。

GUI 构建入口为 `build/windows.ps1`，版本为 `0.5.1` / Windows 文件版本 `0.5.1.0`。原生验证命令 `S7AIStudio.exe --smoke-test <专用目录>` 只连接应用自己创建的回环合成对端，读取 42、写入 123 并读回、捕获原生画布、排空 SQLite，输出 `smoke.json`；普通启动不会自动连接或写入。服务测试覆盖默认只读、旧确认拒绝、UNKNOWN 不重试、部分 GOOD 结果、取消与记录健康状态。

## 后续阶段与发版边界

阶段 A 已交付核心和两个开发工具；`0.2.0` 交付 Windows GUI 预览版及绿色包；`0.3.0` 优化 GUI：状态着色与进度提示、记住上次连接与点位输入（不含写入值）、回车提交、报文与日志复制、打开记录目录；`0.4.0` 新增 viSCADA 物模型点表核对：S7 属性标识（`DB1.R0`、`DB1.X52.0` 等）自带类型，`internal/pointtable` 读取 viSCADA 导出 xlsx 或 CSV 并按期望值逐点核对，GUI 导入点表、`s7-cli verify` 与 `s7-sim -points` 共用。

后续按总体设计完成 Web、AI 工具、OPC UA、PPI/S5/网关路线、实机兼容矩阵和其他平台产物。旧多平台打包模板仍依赖本版未提供的入口，不能作为所有平台已完成的证据。发布前检查当前提交的本地/远端测试、实际 PE 与运行库依赖、版本/图标/高 DPI 清单、解压运行证据与归档字节校验。

每个系列/型号/固件记录真实使用的协议、工程前提、连接参数、读写能力和验证来源。合成对端测试只能证明本实现与测试协议报文一致，无法代替西门子全部系列的实机验证。
