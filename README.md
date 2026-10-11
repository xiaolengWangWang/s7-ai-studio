# S7 AI Studio

参考 `modbus-ai-studio` 的分层架构，为西门子 PLC 建立统一的通讯、诊断和报文记录工具。目前完成**阶段 A：经典 S7 TCP 核心、命令行和合成模拟器**，尚未发布正式版本。

当前可运行的入口是 `s7-cli` 和 `s7-sim`。桌面界面、Web、AI 工具集、OPC UA、PPI、S5 和跨平台安装包属于后续阶段。仓库中的界面资源、更新器及打包脚本来自架构参考，不能视为这些入口已完成。

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

所有参数放在 `read` / `write` 前。目标必须明确指定为 `host:port`；工具不扫描网络。默认只读，写入必须带 `-allow-write`。`-help` 列出全部参数。Linux/macOS 构建时去掉 `.exe`，运行时使用 `./bin/s7-cli` 和 `./bin/s7-sim`。

退出码：`0` 全部成功；`1` 通讯错误、部分失败或记录失败；`2` 参数错误或未实现路线。多个读取地址用逗号分隔；同一命令使用相同类型和数量。JSON 的整数值使用十进制字符串，保留完整的 64 位精度。

## 当前能力与系列范围

经典 S7 TCP 已实现 TPKT/COTP、Setup Communication、PDU 协商、DB/M/I/Q 绝对地址、多项读取、PDU 分批及原生 BOOL 位写入。支持 BOOL、8/16/32/64 位整数、REAL、LREAL、`STRING[n]` 与 RAW。`STRING[n]` 的长度包含 PLC 字符串容量，不包含两个头字节；写入按 ASCII 处理。RAW 写入按每字节十六进制逗号分隔，如 `AA,BB,CC`。

连接档案覆盖设计中的 S7-200/CN、SMART/G2、300、400、1200/G2、1500、ET 200 CPU、软件控制器、开放式控制器、Drive Controller、WinAC、SIPLUS、LOGO! 和 S5 路线。**档案是配置建议，不代表全部型号、固件或实机已经验证。**本阶段只经过合成对端和固定报文测试，没有实机认证。

| 系列/路线 | 阶段 A 行为 |
| --- | --- |
| S7-300 | 默认 rack 0 / slot 2，可覆盖 |
| S7-1200/G2、S7-1500、ET 200SP/200pro、软件/开放式/Drive Controller | 默认 rack 0 / slot 1，可覆盖；仅执行经典 S7 TCP |
| S7-200/CN、SMART/G2、S7-400、ET 200S/X、WinAC、SIPLUS、LOGO! | 需要显式 `-remote-tsap`；实际连接前提由 CPU/通讯模块/网关配置决定 |
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
