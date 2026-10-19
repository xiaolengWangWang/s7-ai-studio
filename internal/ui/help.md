# S7 AI Studio 帮助手册

## 连接 PLC

1. 确认 PLC 或通信模块提供经典 S7 通信（RFC 1006 / ISO-on-TCP），填写 `IP:端口`，常见端口为 `102`。
2. 选择系列。S7-300 默认 Rack 0 / Slot 2；S7-1200、S7-1500 等默认 Rack 0 / Slot 1。这些只是起点，实际值以 CPU、模块和工程配置为准。
3. S7-200、SMART、部分 S7-400、LOGO! 或其他设备，如需自定义连接参数，请在高级配置填入现场的 Remote TSAP。列表没有对应型号但设备确实提供经典 S7 TCP 时，可选 `generic-s7-tcp` 并填写 Remote TSAP。
4. 对 S7-1200/1500 的 DB 绝对地址读取，在 TIA Portal 中允许远端 PUT/GET 访问，将目标 DB 设为标准访问（关闭优化块访问），并检查 CPU 的保护级别。连接成功后先读取一个已知值，再导入点表。

系列选项只配置连接参数，不识别 CPU，也不能证明某个型号或固件已通过实机验证。S7-200 的 PPI、OPC UA、S5 专用协议以及纯串口设备不走此经典 S7 TCP 驱动。S7-200 的 V/VM 地址须按设备映射填写 DB 和偏移。

## 系列接入范围

| 系列选项 | 当前经典 S7 TCP 接入前提 |
| --- | --- |
| S7-300 | CPU 以太网口、支持 S7 服务的 CP 或已配置网关；默认 Rack 0 / Slot 2 |
| S7-1200 / 1200 G2、S7-1500、带 CPU 的 ET 200SP / 200pro、软件 / 开放式 / Drive Controller | 提供经典 S7 TCP 服务；默认 Rack 0 / Slot 1；DB 绝对地址需检查访问许可与标准访问 |
| S7-200 / 200 CN、S7-200 SMART / SMART G2、S7-400、ET 200S / 200X、WinAC、SIPLUS、LOGO! | 设备、通信模块或网关提供经典 S7 TCP；按工程配置填写 Remote TSAP |
| 其他 Siemens 控制器 | 确认提供经典 S7 TCP 后选 `generic-s7-tcp`，并填写 Remote TSAP |

同一系列的型号、固件和通信模块可能不同；表格只说明软件如何配置，不表示每个设备已验证。S5 与不提供经典 S7 TCP 的设备不能直接用这个驱动读取。

## 手动读点

地址框支持逗号分隔的多个地址。西门子绝对地址示例：`DB1.DBX52.0`（BOOL）、`DB1.DBW4`（INT/WORD）、`DB1.DBD0`（DINT/REAL）、`M0.7`、`IW10`、`QB2`。原生地址需要选择匹配的“类型”；同一次手动读取使用同一类型和数量。

viSCADA 使用 Telegraf `inputs.s7comm` 的 `<区域>.<类型><字节偏移>[.附加参数]` 标识，类型由地址决定，同一次读取可混用不同类型：

| 地址 | 读取类型 | 说明 |
| --- | --- | --- |
| `DB1.X52.0` | BOOL | DB1 第 52 字节第 0 位 |
| `DB1.B0`、`DB1.C0` | BYTE、CHAR | 字节；CHAR 是一个字节，扩展字符按 Windows-1252 显示 |
| `DB1.W2`、`DB1.I2` | WORD、INT | 16 位无符号、有符号 |
| `DB1.DW4`、`DB1.DI4` | DWORD、DINT | 32 位无符号、有符号 |
| `DB1.R8` | REAL | 32 位浮点 |
| `DB1.LI16`、`DB1.LR24` | LINT、LREAL | 64 位整数、浮点；需 PLC 具备相应数据类型 |
| `DB1.S32.20` | STRING[20] | 第 32 字节开始，字符串最大 20 字节，另有 2 字节头；扩展字符按 Windows-1252 显示 |
| `PE0.X0.1`、`PA0.B2`、`MK0.R4` | BOOL、BYTE、REAL | 分别映射输入 I、输出 Q、标志 M；区域编号须为 0 |

本版不解析 Telegraf 的 `DT` 日期时间与 `C`/`T` 计数器、定时器区域；导入时会报告跳过的行。`DB1.C0` 中的 `C` 是字符类型，与计数器区域 `C0.*` 不同。
PLC 若使用其他字符代码页，可在报文页核对原始字节。

## 导入 viSCADA 点表

点击“导入点表…”，选择 viSCADA 导出的 `.xlsx` 或 `.csv`。XLSX 优先读取“设备类属性信息”工作表；CSV 支持 UTF-8 和 GBK/GB18030。地址列用“属性标识”或 `address`，可选列包括“属性名称”、`name`、“单位”、“期望值”或“模拟值”。示例：

```csv
address,name,expected
DB1.R0,一次供温,85.0
DB1.X52.0,通信状态,true
DB1.S60.16,故障信息,OK
```

导入后点击“读取一次”，工具会按地址顺序逐个读取，每个地址完成后更新结果及“已读取/总数”进度。单个地址被拒绝时继续下一项；连接失效时停止，保留已完成结果，未执行的地址标记“未读取”。需要中止时点击“断开”。“开始轮询”仍按原来的批量读取方式周期刷新。结果表显示每点的读数、质量和期望值核对；“复制结果”可粘贴到 Excel。无效地址、重复地址和错误期望值会列出行号。每张表最多 1024 点。点表中的“数据类型”数字编码是 viSCADA 元数据，不当作 S7 数据类型；Telegraf 地址自带类型。

## 读取失败时检查

- 连接失败：核对 IP、端口、Rack/Slot 或 Remote TSAP，以及 PLC/模块的连接资源。
- 全部点失败：核对 CPU 访问权限。读 S7-1200/1500 的 DB 时，还要检查 PUT/GET 设置和 DB 是否为标准访问。
- 部分点失败：核对 DB 编号、字节偏移、位号、实际数据类型及 DB 长度。报文页可查看每次请求与回应。
- 数值异常：确认同一字节偏移在 PLC 项目里的类型。例如 `DB1.R0` 要按 REAL 解码，不能按 DINT 解码。

模拟器只能验证软件的点表和通信流程；具体型号、固件和接入路径需要实机核验。

## 软件内更新

1. 切换“更新”页，点击“检查更新”，查看最新正式版、来源及更新说明。
2. 安装前断开 PLC 并停止本机演示，保存需要留存的结果。
3. 点击“下载并安装更新”，确认后等待下载及 SHA-256 校验；下载期间可点击“取消”。
4. 校验通过后安装软件，保存通信记录并自动重启；重新启动后核对顶部版本。

软件同时检查 Gitee 与 GitHub，选择最新版本，下载失败时可切换镜像或续传。校验失败不会安装。目录不可写、从源码运行或程序被改名时，请使用“手动下载”。0.6.0 起提供该入口，旧版本首次升级需手动下载绿色包。

## 资料

- [西门子 STEP 7：GET 读取远端 CPU（S7-1200/1500/1200 G2）](https://docs.tia.siemens.cloud/r/en-us/v21/s7-communication-s7-1200-s7-1500-s7-1200-g2/get-read-data-from-a-remote-cpu-s7-1200-s7-1500-s7-1200-g2)
- [西门子 STEP 7：S7-300/400 连接类型](https://docs.tia.siemens.cloud/r/en-us/v20/editing-devices-and-networks/configure-networks/communication-via-connections/connections-purpose-and-selection/selecting-connection-types-s7-300-s7-400)
- [西门子 STEP 7：CHAR 数据类型](https://docs.tia.siemens.cloud/r/en-us/v20/data-types/character-strings/character/char)
- [Telegraf `inputs.s7comm` 地址与类型说明](https://github.com/influxdata/telegraf/blob/master/plugins/inputs/s7comm/README.md)
