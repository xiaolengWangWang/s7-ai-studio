# 阶段 A 验收与审查

日期：2026-10-11。工作分支：`feat/s7-core`。

## 验收结果

`go mod tidy`、`go vet ./...`、`go test -tags ci -count=1 -timeout 15m ./...` 通过；修复后全仓 12 个有测试的包全部通过。以本机默认 `CGO_ENABLED=0` 构建 `bin/s7-cli.exe` 与 `bin/s7-sim.exe`，无需 Go 环境即可运行。

最终可执行文件通过 9 个实际回环演示：读取初始 42、确认写入 123、读回 123、BOOL false JSON、REAL 零值 JSON、单项失败、区域拒绝、写回应丢失返回 UNKNOWN，以及重新连接后读回已写入的 99。退出码与 stdout 保存在 [演示记录](stage-a-demo.json)。

SQLite 查询确认记录会话已结束，包含 6 条 S7_TCP 握手与读取事件，DINT 42 和系列配置标签完整保留，见 [记录查询](stage-a-recorder.json)。最终程序 SHA256 见 [构建校验值](stage-a-binaries.json)。

本机没有 C 编译器，未执行 race 检测；远端 CI 未触发。未连接真实 PLC，未创建 release tag、安装包或上传资产。

自动审批以“策略拦截”拒绝清理 `.superpowers/sdd/2026-10-11-s7-core` 临时目录，因此保留该目录。程序、Git 提交和归档验证证据不受影响。

## 独立审查与修复

独立审查针对 `632ea73..8ea67c0`，结论为修复后可合入；没有 Critical 或 Minor 项，发现 3 项 Important。执行者另将旧日志兼容问题提升为 Important。四项均先编写回归测试并观察失败，再修复和观察通过，最后运行全仓检查。按执行计划只进行一次审查和一次集中修复，没有另行进行二次审查。

| 问题 | 修复 | 回归证据 |
| --- | --- | --- |
| false、浮点零和空字符串缺少 JSON 值字段 | 按 Type 序列化有效字段，数组逐项保留零值 | `TestValueJSONKeepsActiveZeroFields`、`TestRecorderKeepsZeroValueJSON` |
| 截断响应补零或重建头，原始证据失真 | 记录实际收到的头与载荷；保留原声明长度及非法/部分头 | `TestTPKTTruncationReturnsOnlyReceivedPayload`、`TestObserverRecordsExactTruncatedOrInvalidWire` |
| 握手校验失败仍记 SUCCESS | COTP、Setup 和协商参数失败时更新 RX 状态与原因 | `TestFailedHandshakeObserverRecordsParseError` |
| 极旧日志表缺少可选列时无法只读查看 | 查询时检测缺列并返回默认值，保持文件不变 | `TestLegacyEventsFileMissingColumnsReadOnly` |

分批读取后续断线导致已完成项丢失的问题在审查前已修复，回归证据为 `TestReadRetainsCompletedItemsAfterLaterBatchDisconnect`。

## 实施取舍与未完成范围

| 决定 | 原因与代价 |
| --- | --- |
| 在项目原目录建立 `feat/s7-core` | 原目录没有已有 Git 历史或用户改动；如需额外隔离，后续增加 worktree |
| 使用 PowerShell/.NET 读取计划和进度 | Git Bash 读取受保护 Markdown 时得到密文；通用辅助脚本在本机不能复用 |
| Value 增加 Elements，STRING 要求容量描述 | 保留数组元素和字符串上限；后续消费者需要接受对应字段 |
| 传输超时后不自动重读 | 避免复用错位会话；调用者需要显式重连和重新发起读取 |
| WriteResult 增加 Segments | 暴露非原子分段写入状态；后续消费者需要接受此可选字段 |
| 桌面/Web/AI 闭环与自动写后回读留到后续 | 阶段 A 只交付核心和开发工具；这些功能尚未交付 |
| PPI/OPC UA/S5 和全系列实机验证留到后续 | 当前只实现经典 S7 TCP；档案不代表实机支持证明 |
| 发版资产、安装包和远端跨平台 CI 留到后续 | 本阶段没有发布或远端验证；正式版及跨平台保证仍未完成 |
| 将极旧日志只读兼容纳入本阶段修复 | 旧历史应能查看日志；缺失字段只能返回默认值，不能恢复不存在的数据 |

没有延后处理的 Minor 项。后续范围以 [总体设计](../superpowers/specs/2026-10-11-siemens-plc-design.md) 为准。
