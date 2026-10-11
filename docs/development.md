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

通用业务接口不使用站号或 Modbus 寄存器表示西门子地址。旧协议字段仅放在历史事件的 `Legacy` 中。Recorder 的新元数据带版本；没有元数据的旧库读取时动态检测列，不会因查看而修改文件。

Read 在发送前校验全部地址，按请求和回应预算分组。已完成项即使后续批次断线也保留 GOOD；未完成项为 UNKNOWN，服务拒绝项为 BAD。跨 PDU 的读取不是原子快照。Write 使用原生位写入保护相邻位；大写入分段，不能保证原子性。没有自动写重试，也没有自动连接重试。`s7.Options.ReadRetries` 为后续服务层重试预留，目前不会触发重试；传输超时会使会话失效。

同一连接的请求串行执行；等待中的操作可取消，Close 可打断阻塞读取。Observer 在释放会话锁后接收深拷贝事件。远端身份始终未经验证，选择的系列档案不能作为 CPU 识别证据。

## 验证命令

```powershell
go mod tidy
go vet ./...
go test -tags ci -count=1 -timeout 15m ./...
go test ./internal/s7 -run '^$' -fuzz FuzzDecodePDU -fuzztime 10s
go build -o bin/s7-cli.exe ./cmd/s7-cli
go build -o bin/s7-sim.exe ./cmd/s7-sim
```

固定黄金报文来源见 `internal/s7/testdata/README.md`。测试同时使用手工报文对端和独立请求解析模拟器，覆盖碎片/粘包、长度上限、PDU 引用、部分失败、超时、取消、关闭、原生位写入、写回应丢失和数据库兼容。CLI 测试通过真实回环 TCP 调用驱动。

Windows 本地默认 `CGO_ENABLED=0`，Recorder 使用纯 Go SQLite；没有 C 编译器时不能运行 Go race 检测。CI 的 Linux/macOS 检查使用 race，Linux 还单独检查纯 Go 构建。CI 尚未在远端触发，不能将本地通过当作所有操作系统均已通过。

## 后续阶段与发版边界

阶段 A 交付核心和两个开发工具，不创建正式 release tag 或上传资产。

后续按总体设计完成桌面/Web 操作界面与 AI 工具、OPC UA、PPI/S5/网关路线、实机兼容矩阵、五个平台产物及发版验证。当前已有打包模板依赖尚未实现的桌面/Web 入口，不能用于宣称正式版已经可发布。

每个系列/型号/固件记录真实使用的协议、工程前提、连接参数、读写能力和验证来源。合成对端测试只能证明本实现与测试协议报文一致，无法代替西门子全部系列的实机验证。
