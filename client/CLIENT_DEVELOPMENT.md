# MASQUE Client 开发内容

本文从 `masque-go_CONNECT-UDP软件设计文档.md` 中提取 Client 模块需要开发的内容。Client 是业务侧入口：它连接 MASQUE Proxy，发起 CONNECT-UDP 请求，并通过隧道向 Target UDP Server 发送和接收业务 UDP Payload。

## 模块定位

Client 负责第一段通信：

```text
+--------+      QUIC + HTTP/3 + CONNECT-UDP + HTTP Datagram      +--------------+
| Client |  <-------------------------------------------------->  | MASQUE Proxy |
+--------+                                                       +--------------+
```

Client 不直接连接 Target UDP Server，而是告诉 Proxy：

```text
请帮我创建一条到 Target UDP Server 的 UDP 代理隧道。
```

当前 Proxy 地址规划为可配置项，默认先使用：

```text
https://192.168.1.10:443
```

推荐 URI Template：

```text
https://192.168.1.10:443/masque?h={target_host}&p={target_port}
```

## 需要开发的内容

1. 配置 MASQUE Proxy 地址，例如 `https://192.168.1.10:443`。
2. 配置与 Proxy 一致的 URI Template。
3. 配置 Target UDP Server 地址，例如 `192.168.123.100:7400`。
4. 创建 CONNECT-UDP 请求。
5. 建立 Client 到 Proxy 的 QUIC + HTTP/3 连接。
6. 通过 Extended CONNECT 发起 `:protocol = connect-udp` 请求。
7. 接收并校验 Proxy 返回的 2xx 响应。
8. 使用 `*masque.Conn` 发送原始业务 UDP Payload。
9. 使用同一个 `*masque.Conn` 接收 Target 通过 Proxy 返回的响应。
10. 支持一个 ClientConn 复用多个 CONNECT-UDP Request Stream，对应多个 Target。

## 核心对象

### ClientConn

`ClientConn` 表示 Client 到某一个 MASQUE Proxy 的连接。底层是一条 QUIC 连接，上面运行 HTTP/3。

一条 `ClientConn` 可以承载多个 CONNECT-UDP 会话：

```text
ClientConn，底层一条 Client <-> Proxy QUIC 连接
  -> CONNECT-UDP Request Stream A -> Target A
  -> CONNECT-UDP Request Stream B -> Target B
  -> CONNECT-UDP Request Stream C -> Target C
```

### Conn

`clientConn.Dial(req)` 返回：

```go
*masque.Conn
```

它对上层表现得像 `net.PacketConn`：

```go
conn.WriteTo(payload, nil)
conn.ReadFrom(buffer)
```

但它不是普通 UDP socket，而是绑定到固定 Target UDP Server 的 CONNECT-UDP 逻辑连接。

## 前置配置

Client 需要提前知道两个地址：

| 配置 | 示例 | 含义 |
|---|---|---|
| Proxy 地址 | `https://192.168.1.10:443` | Client 要连接的 MASQUE Proxy |
| URI Template | `https://192.168.1.10:443/masque?h={target_host}&p={target_port}` | Client 与 Proxy 约定的 CONNECT-UDP 请求模板 |
| Target 地址 | `192.168.123.100:7400` | Proxy 最终转发到的 UDP 服务 |

Client 侧准备示例：

```go
template := uritemplate.MustNew(
    "https://192.168.1.10:443/masque?h={target_host}&p={target_port}",
)

target := "192.168.123.100:7400"

req, err := masque.NewRequest(ctx, template, target)
```

这里需要区分：

```text
https://192.168.1.10:443
    是 MASQUE Proxy 地址。

/masque?h={target_host}&p={target_port}
    是 Client 与 Proxy 约定的 URI Template。

192.168.123.100:7400
    是 Target UDP Server 地址。
```

## 连接请求

连接请求阶段，Client 通过 URI Template 把 Target 地址编码到 CONNECT-UDP 请求中。

`masque.NewRequest()` 需要完成：

1. 将 `target` 拆成 `target_host` 和 `target_port`。
2. 展开 URI Template。
3. 构造 HTTP/3 Extended CONNECT 请求。
4. 设置 `:protocol = connect-udp`。
5. 设置 `Capsule-Protocol: ?1`。

使用 query 模板时，展开结果类似：

```text
https://192.168.1.10:443/masque?h=192.168.123.100&p=7400
```

CONNECT-UDP 请求语义：

```http
:method = CONNECT
:protocol = connect-udp
:scheme = https
:authority = 192.168.1.10:443
:path = /masque?h=192.168.123.100&p=7400
capsule-protocol = ?1
```

注意：Target 地址不是通过 `connect-udp-target` Header 传递，而是通过 URI Template 中的 `{target_host}` 和 `{target_port}` 进入 path 或 query。

## 建立隧道

Client 创建请求后，通过 `ClientConn` 或 `Transport` 建立 CONNECT-UDP 隧道：

```go
conn, rsp, err := clientConn.Dial(req)
```

或：

```go
conn, rsp, err := transport.Dial(req)
```

成功条件：

1. 能连上 Proxy 的 QUIC / HTTP/3 服务。
2. Proxy 返回 2xx 响应。
3. 响应侧启用 Capsule Protocol。
4. QUIC Datagram / HTTP Datagram 能力可用。

## 发送消息

Client 使用 `*masque.Conn` 发送原始业务 UDP Payload：

```go
payload := []byte("hello")
_, err := conn.WriteTo(payload, nil)
```

`WriteTo()` 中的 `addr` 参数通常被忽略，因为普通 CONNECT-UDP 的一个 `Conn` 已经绑定到固定 Target。

Client 到 Proxy 的 HTTP Datagram 语义：

```text
HTTP Datagram Payload
+----------------+----------------------+
| Context ID = 0 | UDP Proxying Payload |
+----------------+----------------------+
```

其中：

```text
Context ID = 0
UDP Proxying Payload = Client 原始业务 Payload
```

Client 不需要手动添加 `CLIENT-IP` 头。该头由 Proxy 在转发给 Target 前添加。

## 接收响应

Client 使用同一个 `*masque.Conn` 接收 Target 响应：

```go
buf := make([]byte, 1500)
n, addr, err := conn.ReadFrom(buf)
```

`masque-go` 内部处理：

```text
ReceiveDatagram()
  -> 解析 Context ID
  -> Context ID == 0：取出 UDP Payload
  -> copy 到用户 buffer
  -> ReadFrom() 返回
```

上层应用最终看到的是 Target 返回的原始 UDP Payload，例如：

```text
reply: hello
```

## 多目标映射

一条 Client 到 Proxy 的 QUIC 连接可以承载多个 CONNECT-UDP 服务，但每个服务对应独立 Request Stream 和独立 `*masque.Conn`：

```go
req1, _ := masque.NewRequest(ctx, template, "8.8.8.8:53")
conn1, _, _ := clientConn.Dial(req1)

req2, _ := masque.NewRequest(ctx, template, "192.168.123.100:7400")
conn2, _, _ := clientConn.Dial(req2)
```

映射关系：

```text
clientConn，一条 QUIC 连接
  -> conn1 / Request Stream A -> udpConn A -> 8.8.8.8:53
  -> conn2 / Request Stream B -> udpConn B -> 192.168.123.100:7400
```

不同 Target 依靠不同 HTTP/3 Request Stream 区分，不靠 Context ID 区分。普通 UDP Payload 的 Context ID 都是 `0`。

## 最小验收标准

1. Client 能配置 Proxy 地址、URI Template 和 Target 地址。
2. Client 能用 Target 地址创建 CONNECT-UDP 请求。
3. Client 能连接 Proxy 的 HTTP/3 / QUIC 服务。
4. Proxy 返回 2xx 后，Client 能得到可用的 `*masque.Conn`。
5. Client 调用 `WriteTo()` 发送原始业务 Payload。
6. Client 调用 `ReadFrom()` 接收 Target 的原始响应 Payload。
7. Client 不需要自行封装 `CLIENT-IP`，该逻辑由 Proxy 负责。
8. 多个 Target 可以通过多个 Request Stream / 多个 `*masque.Conn` 区分。

## 对应源码

- `cmd/client/main.go`：示例 Client 命令入口。
- `request.go`：创建 CONNECT-UDP 请求，展开 URI Template。
- `transport.go`：建立到 Proxy 的 QUIC / HTTP3 连接，并创建 ClientConn。
- `client.go`：在 HTTP/3 Request Stream 上发起 CONNECT-UDP。
- `conn.go`：实现类似 `net.PacketConn` 的读写接口。

## 设计文档来源

主要对应设计文档中的以下章节：

- `3.1 ClientConn`
- `3.2 Conn`
- `3.3 Request Stream 与 Target 的映射`
- `4.4 Client 准备 CONNECT-UDP 请求参数`
- `5.1 流程目标`
- `5.2 URI Template`
- `5.3 masque-go 客户端调用`
- `5.4 连接请求消息格式`
- `5.5 连接请求在协议栈中的位置`
- `7.2 Client 侧发送 API`
- `7.3 Client 到 Proxy 的消息格式：HTTP Datagram`
- `8.6 Client 接收响应`
- `10. 多目标服务如何区分`
- `11. 关键结论`
