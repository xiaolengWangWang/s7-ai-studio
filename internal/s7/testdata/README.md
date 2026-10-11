# 独立报文依据

测试中的十六进制报文逐字段手工构造，不由待测编码器生成。

- TPKT 头：RFC 1006，版本 3、保留字节 0、大端总长度（含四字节头）。
- Setup：S7 Job，参考号 1，参数 8 字节，功能 F0，并发参数 1/1，请求 PDU 480。
- Read：功能 04，S7ANY 参数项 12 字节，BYTE 传输，DB1 偏移 4 字节，长度 4 字节，位地址 0x20。
- 位写：功能 05，M0.7，BIT 传输，数量 1，位地址 7，数据长度 1 bit，数据 01。
- 两项读响应：DB1 的 DINT 42 成功，另一项返回 05（地址越界），各项独立判定。

字段结构交叉核对：[Snap7 s7_types.h](https://github.com/SCADACS/snap7/blob/master/src/core/s7_types.h)；TPKT：[RFC 1006](https://www.rfc-editor.org/rfc/rfc1006)。这里只借助资料核对协议，不复制参考库代码。
