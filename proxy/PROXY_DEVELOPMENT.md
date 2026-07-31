# MASQUE Proxy 开发内容

本文从 `masque-go_CONNECT-UDP软件设计文档.md` 中提取 Proxy 模块需要开发的内容。Proxy 是 CONNECT-UDP 的核心转发节点：一侧接收 Client 的 HTTP/3 CONNECT-UDP 请求，另一侧创建普通 UDP socket 访问 Target UDP Server。

## 模块定位

Proxy 负责完成两段通信之间的转换：

```text
+--------+      QUIC + HTTP/3 + CONNECT-UDP      +--------------+      UDP      +-------------------+
| Client |  <--------------------------------->  | MASQUE Proxy |  <-------->  | Target UDP Server |
+--------+                                      +--------------+             +-------------------+
```

Proxy 不理解业务 Payload 的业务语义，但在当前设计中需要在发往 Target 的 UDP Payload 外层增加一段自定义元信息，用于携带 Client IP。Proxy 负责：

1. 解析 Client 的 CONNECT-UDP 请求。
2. 根据 URI Template 提取 `target_host` 和 `target_port`。
3. 创建 Proxy 到 Target 的后端 UDP socket。
4. 将 Client 发来的 HTTP Datagram 解封装为原始业务 UDP Payload。
5. 在转发给 Target 前，为业务 Payload 增加 Client IP 自定义头。
6. 将 Target 回包重新封装成 HTTP Datagram 发回 Client。

## 需要开发的内容

1. 启动 HTTP/3 / QUIC 服务并监听指定地址。
2. 加载 TLS 证书和私钥，因为 HTTP/3 基于 QUIC + TLS。
3. 配置与 Client 一致的 URI Template。
4. 注册 CONNECT-UDP Handler，用于处理 `:protocol = connect-udp` 的请求。
5. 启用 HTTP Datagram / QUIC Datagram 能力。
6. 解析 CONNECT-UDP 请求并校验 method、protocol、authority、Capsule-Protocol 和目标地址。
7. 根据解析出的 Target 地址创建后端 UDP socket。
8. 返回成功或失败的 HTTP/3 响应头。
9. 在数据面进行双向转发：
   - Client -> Proxy -> Target
   - Target -> Proxy -> Client
10. 管理连接关闭和 Proxy 关闭时的资源清理。
11. 在 Proxy -> Target 方向增加 Client IP 元信息封装。
12. 根据需要补充认证、目标地址白名单、端口限制等安全策略。

## 启动阶段

Proxy 启动时只做服务准备，不会提前连接 Target：

```text
1. 监听 Client -> Proxy 的 HTTP/3 / QUIC 端口。
2. 注册 CONNECT-UDP 路由。
3. 准备 URI Template，用于后续解析 target_host / target_port。
4. 等待 Client 发起 CONNECT-UDP 请求。
```

设计文档中的典型配置：

```text
Proxy 地址：
    https://192.168.1.10:443

URI Template：
    https://192.168.1.10:443/masque/{target_host}/{target_port}
```

当前建议把 Proxy 地址做成配置项，默认值先使用：

```text
https://192.168.1.10:443
```

后续如果 Proxy 所在机器 IP 变化，只需要修改配置中的 Proxy 地址，不应修改业务逻辑代码。

当前代码使用 query 形式也可以：

```text
https://192.168.1.10:443/masque?h={target_host}&p={target_port}
```

对应入口文件：

- `cmd/proxy/main.go`

当前启动参数：

| 参数 | 含义 |
|---|---|
| `-b` | Proxy HTTP/3 / QUIC 监听地址，例如 `192.168.1.10:443` |
| `-t` | URI Template，必须与 Client 侧保持一致 |
| `-c` | TLS 证书文件 |
| `-k` | TLS 私钥文件 |

建议后续补充一个 Proxy 地址配置项，例如 `proxy_url` 或命令行参数 `-proxy`，默认值为 `https://192.168.1.10:443`，再由它生成默认 URI Template：

```text
{proxy_url}/masque?h={target_host}&p={target_port}
```

## 连接请求解析

Proxy 收到 CONNECT-UDP 请求后，需要用同一个 URI Template 解析请求：

```go
preq, err := masque.ParseProxyRequest(r, template)
```

解析结果包含：

```text
preq.Target = "TargetHost:TargetPort"
preq.Host   = "ProxyHost:ProxyPort"
```

需要校验：

1. HTTP method 必须是 `CONNECT`。
2. protocol 必须是 `connect-udp`。
3. request authority 必须与 URI Template 的 authority 匹配。
4. `Capsule-Protocol` 如果存在，必须是合法的 `?1`。
5. URI Template 必须能提取出 `target_host` 和 `target_port`。
6. `target_port` 必须是合法端口。

对应源码：

- `proxy_request.go`

同时需要记录 Client 到 Proxy 这条连接的来源地址：

```go
clientAddr := r.RemoteAddr
```

该地址用于后续 Proxy -> Target 方向的自定义元信息封装。需要注意：`r.RemoteAddr` 是 Proxy 看到的 Client 地址；如果 Client 和 Proxy 之间存在 NAT、反向代理或隧道，它不一定等于用户设备的真实内网地址。

## 创建后端 UDP Socket

Proxy 接受请求后，为这一条 CONNECT-UDP Request Stream 创建一个后端 UDP socket：

```go
targetAddr, err := net.ResolveUDPAddr("udp", preq.Target)
udpConn, err := net.DialUDP("udp", nil, targetAddr)
```

注意：

1. `net.DialUDP()` 对 UDP 没有 TCP 三次握手。
2. 它是在 Proxy 本机创建一个 UDP socket，并固定远端地址为 Target。
3. 后续这一条 CONNECT-UDP 隧道上的所有业务 UDP Payload 都复用这个 `udpConn`。
4. Proxy 不会替 Target 创建监听 socket，Target 必须提前监听自己的 UDP 端口。

建立后的映射关系：

```text
HTTP/3 Request Stream
  -> Target = 192.168.123.100:7400
  -> Proxy 后端 udpConn
  -> ProxyIP:RandomPort <-> 192.168.123.100:7400
```

对应源码：

- `proxy.go` 中的 `Proxy()`

## 连接响应

Proxy 成功接受请求后，返回 HTTP/3 响应头：

```http
:status = 200
capsule-protocol = ?1
proxy-status = 192.168.1.10:443; next-hop="192.168.123.100:7400"
```

最小成功响应：

```http
:status = 200
capsule-protocol = ?1
```

失败时返回非 2xx 状态码，例如：

| 场景 | 建议状态 |
|---|---|
| 请求格式错误 | `400` |
| method 不是 CONNECT | `405` |
| protocol 不是 connect-udp | `501` |
| 鉴权失败 | `401` |
| 目标被策略拒绝 | `403` |
| DNS 解析失败或目标不可达 | `502` |
| 目标超时 | `504` |

可通过 `Proxy-Status` 响应头记录 next-hop 或失败原因。

## Client 到 Target 的转发

Proxy 收到 Client 发来的 HTTP Datagram 后：

```text
ReceiveDatagram()
  -> 根据 HTTP/3 Request Stream 找到对应 udpConn
  -> 解析 HTTP Datagram Payload 中的 Context ID
  -> Context ID == 0：取出 UDP Payload
  -> 封装 Client IP 元信息 + 原始 UDP Payload
  -> udpConn.Write(wrappedPayload)
  -> 普通 UDP 发给 Target UDP Server
```

普通 CONNECT-UDP 中：

| Context ID | 处理 |
|---|---|
| `0` | 有效 UDP Payload |
| 非 `0` | 当前普通 UDP 转发不处理，丢弃或忽略 |

Proxy 发给 Target 的消息格式：

```text
UDP/IP Header:
    src = ProxyIP:ProxyRandomPort
    dst = TargetIP:TargetPort

UDP Payload:
    Client 地址元信息 + Client 原始业务 Payload
```

对应源码：

- `proxy.go` 中的 `proxyConnSend()`

## Client 地址元信息封装

普通 CONNECT-UDP 中 Target 默认只能看到 Proxy 的后端 UDP socket 地址。为了让 Target 获得 Client IP，本项目采用自定义 payload 头的方式。

Proxy 转发给 Target 的 UDP Payload 格式定义为：

```text
+----------------------+----------------------+
| Client Address Header | Original UDP Payload |
+----------------------+----------------------+
```

建议先采用易调试的文本头格式：

```text
CLIENT-IP: <ip>\r\n
\r\n
<原始 UDP Payload>
```

示例：

```text
CLIENT-IP: 192.168.1.20

hello
```

如果 `r.RemoteAddr` 是 IPv6 地址，也需要正确解析 host 和 port，例如：

```text
[2408:xxxx::1]:53001
```

封装逻辑建议：

```text
1. Proxy 在处理 CONNECT-UDP 请求时读取 r.RemoteAddr。
2. 使用 net.SplitHostPort() 从 r.RemoteAddr 提取 host 部分，作为 client_ip。
3. 在 proxyConnSend() 写入 Target 前拼接自定义头。
4. Target 收包后先解析自定义头，再取出原始业务 Payload。
```

Target 侧必须配套改造，不能再把收到的整个 UDP Payload 都当作业务数据。Target 应解析出：

```text
client_ip
original_payload
```

然后再处理 `original_payload`。

后续如需降低 payload 开销，可以把文本头替换为二进制头，但协议字段含义保持一致：

```text
client_ip
original_payload
```

对应源码：

- `proxy.go` 中的 `proxyConnSend()`

## Target 到 Client 的转发

Proxy 从后端 UDP socket 读到 Target 响应后，需要重新封装为 HTTP Datagram：

```text
HTTP Datagram Payload
+----------------+----------------------+
| Context ID = 0 | UDP Proxying Payload |
+----------------+----------------------+
```

其中：

```text
UDP Proxying Payload = Target 返回的原始 UDP Payload
```

然后通过该 `udpConn` 所属的 HTTP/3 Request Stream 发回 Client。

对应源码：

- `proxy.go` 中的 `proxyConnReceive()`

## 多目标映射

一条 Client 到 Proxy 的 QUIC 连接可以承载多个 CONNECT-UDP 服务，但每个 Target 对应独立的 HTTP/3 Request Stream 和后端 UDP socket：

```text
ClientConn，一条 QUIC 连接
  -> Request Stream A -> udpConn A -> 8.8.8.8:53
  -> Request Stream B -> udpConn B -> 192.168.123.100:7400
  -> Request Stream C -> udpConn C -> stun.example.com:3478
```

Context ID 不用于区分不同 Target；Target 的区分依靠不同 Request Stream。

## 资源管理

Proxy 需要处理以下关闭场景：

1. Client 关闭 CONNECT-UDP Request Stream。
2. Target 后端 UDP socket 读写失败。
3. Proxy 主动关闭。
4. HTTP Datagram 发送失败。

关闭时应释放：

1. HTTP/3 Request Stream。
2. 后端 `udpConn`。
3. Proxy 内部保存的 active flow 记录。

对应源码：

- `proxy.go` 中的 `ProxyConnectedSocket()`
- `proxy.go` 中的 `Close()`

## 最小验收标准

1. Proxy 能加载 TLS 证书并监听 HTTP/3 / QUIC 地址。
2. Proxy 能用与 Client 一致的 URI Template 解析 `target_host` 和 `target_port`。
3. Proxy 能为每条 CONNECT-UDP Request Stream 创建后端 UDP socket。
4. Proxy 能记录当前 CONNECT-UDP 请求对应的 Client IP。
5. Client 发来的 Context ID = 0 的 HTTP Datagram 能被转成普通 UDP 发给 Target。
6. Proxy 发给 Target 的 UDP Payload 中包含 Client IP 自定义头和原始业务 Payload。
7. Target 能解析出 Client IP 和原始业务 Payload。
8. Target 的 UDP 响应能被封装为 Context ID = 0 的 HTTP Datagram 发回 Client。
9. 请求解析失败、目标地址非法、DNS 失败等场景能返回合理 HTTP 状态码。
10. 多个 Request Stream 可以映射到不同 Target。
11. 连接关闭后对应的 stream 和 udpConn 能被释放。

## 设计文档来源

主要对应设计文档中的以下章节：

- `4.3 MASQUE Proxy 启动 HTTP/3 / QUIC 服务`
- `6.1 流程目标`
- `6.2 Proxy 解析请求`
- `6.3 Proxy 创建后端 UDP socket`
- `6.4 连接响应消息格式`
- `6.5 失败响应格式`
- `6.6 连接响应后的状态`
- `7.4 Proxy 侧转发逻辑`
- `7.5 Proxy 到 Target 的 UDP 消息格式`
- `8.5 Proxy 转回 Client 的 HTTP Datagram 格式`
- `10. 多目标服务如何区分`
- `11. 关键结论`
