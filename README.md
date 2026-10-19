# S7 AI Studio

为西门子 PLC 提供经典 S7 TCP 通讯、诊断和报文记录工具。`0.5.1` 包含 **Windows x64 单工作区 GUI 预览版**、命令行和合成模拟器。

当前入口为 `s7-ai`、`s7-cli` 和 `s7-sim`。Web、AI 工具集、OPC UA、PPI、S5 和其他平台安装包属于后续阶段；选择型号档案不代表实机认证。

## Windows GUI

连接、地址格式、viSCADA 点表导入与各系列前提见应用内“帮助”页，也可阅读[帮助手册](internal/ui/help.md)。

从 [Gitee Releases](https://gitee.com/sun_xuanqi/s7_ai_studio/releases) 或 [GitHub Releases](https://github.com/xiaolengWangWang/s7-ai-studio/releases) 下载 Windows x64 ZIP，解压后双击 `S7AIStudio.exe`，无需安装 Go 或 MinGW。需要支持 OpenGL 的图形驱动。

点击“启动本机演示”读取 `DB1.DBD0` 的 DINT 42。勾选“允许写入”，输入 123 并确认目标、地址、类型和值，查看写入状态与独立读回结果。现场连接需要明确的 `host:port` 和正确的高级参数。默认只读，UNKNOWN 不自动重试写入，通讯失败后需要手动重新连接。

记录保存到 `%AppData%\S7AIStudio\records`，按天 / 50 MB 轮转；保存错误显示在窗口底部。报文页可查看真实 TX/RX 字节，正常关闭会排空记录并释放本机演示端口。本版只经过合成对端验证，没有真实 PLC 认证。

0.3.0 优化了界面：顶部用颜色显示连接状态（绿色已连接、橙色连接中、红色失败），后台操作时显示进度；连接或读取成功后记住目标、系列、高级参数和点位，下次启动自动填入，写入值和“允许写入”不保存。目标框回车即连接，地址框回车即读取，写入值框回车只打开写入确认框。报文页可复制所选报文详情，日志可选中复制，窗口底部可直接打开记录目录。

0.5.1 增加导入点表后的逐点验证读取：每完成一个地址即显示结果和进度；单点被拒绝时继续，通信中断时保留已完成结果并停止。修复已停止轮询的排队任务覆盖验证结果，以及无期望值的点读取失败时未显示失败结论的问题。详细操作见[在线手册](http://mrdoc.iheatingos.com/project-36/doc-16517/)和[本地手册](docs/manuals/s7-ai-studio-user-manual.md)。

0.5.0 扩展 Telegraf S7 地址类型，加入 PE0/PA0/MK0 区域别名和应用内帮助手册；其他提供经典 S7 TCP 的型号可通过 `generic-s7-tcp` 与现场 Remote TSAP 配置接入。支持范围和前提见[帮助手册](internal/ui/help.md)。

0.4.0 新增 viSCADA 物模型点表核对：地址可直接填 viSCADA 的 S7 属性标识（如 `DB1.R0`、`DB1.X52.0`），类型由标识决定，REAL 与 BOOL 可在同一次读取中混用；“导入点表…”读取 viSCADA 物模型导出的 xlsx 或 CSV，结果表显示名称、单位和核对结论，上方汇总一致 / 不一致数量，“复制结果”可直接粘贴到 Excel。REAL 按 32 位精度显示（0.6 而不是 0.6000000238418579）。详见下文“viSCADA 物模型点表核对”。

本机构建（需要 Go 和 Windows x64 MinGW）：

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File build/windows.ps1 -Version 0.5.1 -CompilerRoot D:\tools\winlibs-gcc16\mingw64
```

产物位于 `dist/windows-gui/`，包括绿色 ZIP、SHA-256 校验和与构建清单。

## viSCADA 物模型点表核对

地址可直接填 viSCADA 物模型采用的 Telegraf S7 标识，类型由标识决定，不受“类型”下拉框和 `-type` 默认值影响：`DB1.R0`（REAL）、`DB1.X52.0`（BOOL）、`DB1.I4`（INT）、`DB1.W6`（WORD）、`DB1.DW8`（DWORD）、`DB1.DI12`（DINT）、`DB1.B0`（BYTE），以及 `C`（CHAR）、`LI`（LINT）、`LR`（LREAL）、`S偏移.最大长度`（STRING）。`PE0`、`PA0`、`MK0` 分别对应输入 I、输出 Q、标志 M。偏移与原生写法相同，如 `DB1.R0` 即 `DB1.DBD0` 按 REAL 读取。完整示例与不支持的标识见[帮助手册](internal/ui/help.md)。

点表可以是 viSCADA 物模型导出的 `.xlsx`（优先读取“设备类属性信息”工作表），也可以是 UTF-8 或 GBK 编码的 `.csv`。程序按表头识别列：“属性标识/地址”为必需列；“属性名称”“标准化名称”“单位”可选；“模拟值/期望值”用于核对；原生地址需要“类型”列填写 S7 类型（如 REAL），位地址默认按 BOOL。viSCADA 的“数据类型”编码（1/3/6）不作为 S7 类型使用。无法解析、地址重复或期望值无效的行会被跳过并列出行号。每张表最多 1024 点。

GUI 中点击“导入点表…”，地址框会换成表中全部点位；点击“读取一次”按地址顺序逐点读取并更新进度，结果表逐项显示名称、单位和核对结论（一致 / 不一致 / 读取失败），表格上方汇总数量。“复制结果”按制表符分隔，可直接粘贴到 Excel。REAL 按 32 位精度显示和比较，例如期望值 `0.60` 与 PLC 中的 32 位浮点 0.6 判为一致（viSCADA 按 double 显示为 `0.6000000238418579`，是同一个值）。

示例 `assets/examples/viscada-heat-station-s7.csv` 是热力站 S7 点表（DB1，39 个 REAL、8 个 BOOL，共 47 点）。不连接 PLC 时，可用模拟器按点表预置数据，再执行核对：

```powershell
.\bin\s7-sim.exe -listen 127.0.0.1:1102 -points assets\examples\viscada-heat-station-s7.csv
.\bin\s7-cli.exe -target 127.0.0.1:1102 -points assets\examples\viscada-heat-station-s7.csv verify
.\bin\s7-cli.exe -target 127.0.0.1:1102 -points assets\examples\viscada-heat-station-s7.csv -json verify
```

`verify` 全部读到且没有不一致时退出码为 `0`，否则为 `1`。没有期望值的点只读取，不核对。模拟器只回放点表里的值，用来检查点表和工具链路，不能替代实机核对；对实机核对时，PLC 侧 DB 仍须为非优化访问并允许 PUT/GET。

本机若有透明加密软件，被加密的 `.csv`/`.xlsx` 对本程序显示为密文，导入时会提示“已被 E-SafeNet 透明加密”；请改用浏览器从 viSCADA 直接下载的原始文件。
## 构建与运行

使用 `go.mod` 指定的 Go 工具链；运行构建后的程序不需要安装 Go。Windows PowerShell：

```powershell
go build -o bin/s7-cli.exe ./cmd/s7-cli
go build -o bin/s7-sim.exe ./cmd/s7-sim
```

在第一个终端启动合成对端，默认 DB1.DBD0 为 DINT 42：

```powershell
.\bin\s7-sim.exe -listen 127.0.0.1:1102
```

在第二个终端读写：

```powershell
.\bin\s7-cli.exe -target 127.0.0.1:1102 -type DINT read DB1.DBD0
.\bin\s7-cli.exe -target 127.0.0.1:1102 -type DINT -json read DB1.DBD0
.\bin\s7-cli.exe -target 127.0.0.1:1102 -type DINT -allow-write write DB1.DBD0 123
.\bin\s7-cli.exe -target 127.0.0.1:1102 -type BOOL -allow-write write M0.7 true
.\bin\s7-cli.exe -target 127.0.0.1:1102 -type INT -allow-write write MW10 1,2,3
.\bin\s7-cli.exe -target 127.0.0.1:1102 -type INT read MW10 3
.\bin\s7-cli.exe -target 127.0.0.1:1102 -type DINT -record '.\通信 历史' read DB1.DBD0,DB9.DBD0
```

所有参数放在 `read` / `write` / `verify` 前。目标必须明确指定为 `host:port`；工具不扫描网络。默认只读，写入必须带 `-allow-write`。`-help` 列出全部参数。Linux/macOS 构建时去掉 `.exe`，运行时使用 `./bin/s7-cli` 和 `./bin/s7-sim`。

退出码：`0` 全部成功；`1` 通讯错误、部分失败或记录失败；`2` 参数错误或未实现路线。多个读取地址用逗号分隔；同一命令使用相同类型和数量。JSON 的整数值使用十进制字符串，保留完整的 64 位精度。

## 当前能力与系列范围

经典 S7 TCP 已实现 TPKT/COTP、Setup Communication、PDU 协商、DB/M/I/Q 绝对地址、多项读取、PDU 分批及原生 BOOL 位写入。支持 BOOL、8/16/32/64 位整数、REAL、LREAL、`STRING[n]` 与 RAW。`STRING[n]` 的长度包含 PLC 字符串容量，不包含两个头字节；扩展字符按 Windows-1252 编解码。RAW 写入按每字节十六进制逗号分隔，如 `AA,BB,CC`。

连接档案覆盖设计中的 S7-200/CN、SMART/G2、300、400、1200/G2、1500、ET 200 CPU、软件控制器、开放式控制器、Drive Controller、WinAC、SIPLUS、LOGO! 和 S5 路线。**档案是配置建议，不代表全部型号、固件或实机已经验证。**本阶段只经过合成对端和固定报文测试，没有实机认证。

| 系列/路线 | 阶段 A 行为 |
| --- | --- |
| S7-300 | 默认 rack 0 / slot 2，可覆盖 |
| S7-1200/G2、S7-1500、ET 200SP/200pro、软件/开放式/Drive Controller | 默认 rack 0 / slot 1，可覆盖；仅执行经典 S7 TCP |
| S7-200/CN、SMART/G2、S7-400、ET 200S/X、WinAC、SIPLUS、LOGO! | 需要显式 `-remote-tsap`；实际连接前提由 CPU/通讯模块/网关配置决定 |
| 其他提供经典 S7 TCP 的设备 | 可选 `generic-s7-tcp` 并填写现场 Remote TSAP；不代表实机认证 |
| PPI、OPC UA、S5 专用协议 | 明确返回“未实现”，不会自动切换协议 |
| S5、旧 LOGO! 的直接 S7 TCP | 档案不提供此路线，拒绝连接 |

示例：`-family s7-1500 -rack 0 -slot 1` 或 `-family s7-200-smart -remote-tsap 0x0200`。后者 TSAP 只是示例，必须按现场配置填写。V/VM 地址需明确提供 `-v-db` 和可选 `-v-offset`；不能假设所有设备都映射到 DB1。

实机通常还需：正确 IP/端口和 TSAP、允许对应连接、CPU 访问权限、可绝对寻址的 DB，以及合适的通讯模块或网关。S7-1200/1500 的优化 DB 和访问保护会影响经典绝对地址访问。以具体 CPU 的工程配置和官方手册为准。

## 故障演示与记录

模拟器的内存区域包括 DB1、DB2、M、I、Q；其他 DB 可用于测试单项失败。它是合成协议对端，不模拟真实 CPU 身份和程序执行。

```powershell
# 小 PDU，验证分批
.\bin\s7-sim.exe -listen 127.0.0.1:1103 -pdu 128
# 区域拒绝
.\bin\s7-sim.exe -listen 127.0.0.1:1104 -reject-area DB
# 写入已生效，但回应丢失
.\bin\s7-sim.exe -listen 127.0.0.1:1105 -drop-write-ack
.\bin\s7-cli.exe -target 127.0.0.1:1105 -timeout 200ms -type DINT -allow-write -json write DB1.DBD0 99
```

其他故障参数：`-delay 500ms`、`-truncate`、`-wrong-reference`。按 Ctrl+C 退出模拟器。

写入 `CONFIRMED` 表示收到该操作的确认回应，未进行读回校验；`REJECTED` 表示未提交或被拒绝；`UNKNOWN` 表示不能确定最终状态。分段写入不具备事务原子性，结果中的 `segments` 保留已尝试各段的状态。写入不会自动重发；传输超时后会关闭会话，调用者需要重新连接。

`-record` 使用本地 SQLite，按日和大小分文件，退出前排空队列。记录包含握手、原始 TX/RX、请求引用、地址、类型和每项结果；旧 Modbus 历史库可只读查询并保留原协议及字段，新记录不依赖 Modbus 通讯模块。

更多结构、测试和阶段说明见 [开发说明](docs/development.md)、[总体设计](docs/superpowers/specs/2026-10-11-siemens-plc-design.md) 和 [阶段 A 计划](docs/superpowers/plans/2026-10-11-s7-core.md)。
