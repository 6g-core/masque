# masque-go CONNECT-UDP 软件设计文档

## 1. 文档目的

本文基于 `masque-go` 的普通 **RFC 9298 CONNECT-UDP** 模型，按照流程说明 MASQUE 如何完成 UDP 代理转发。

本文不再按“客户端、MASQUE Proxy、Target UDP Server”三个模块孤立展开，而是按一次完整通信过程拆成 **一个前置准备 + 四个主流程**：

0. **前置准备**：Target UDP Server 启动 UDP 监听 socket；MASQUE Proxy 启动 HTTP/3/QUIC 服务并注册 CONNECT-UDP Handler；Client 准备 Proxy 地址、URI Template 和目标 UDP 服务地址。
1. **连接请求**：Client 向 MASQUE Proxy 发起 CONNECT-UDP 请求。
2. **连接响应**：MASQUE Proxy 接受请求，创建到 Target UDP Server 的后端 UDP socket，并返回 2xx 响应。
3. **发送消息**：Client 通过 CONNECT-UDP 隧道发送 UDP Payload，MASQUE Proxy 转发给 Target UDP Server。
4. **回复响应**：Target UDP Server 回包给 MASQUE Proxy，MASQUE Proxy 再通过 HTTP Datagram 转回 Client。

本文重点讲清楚每个流程中的：

- 参与方；
- 协议栈；
- 消息格式；
- `masque-go` 中对应的主要 API；
- Client、Proxy、Target 之间如何建立映射关系。

本文讨论的是 `masque-go` 当前常用的普通 CONNECT-UDP 模型，不讨论 CONNECT-IP、TUN/VPN、Bound UDP / CONNECT-UDP Bind 等扩展能力。

---

## 2. 总体架构与连接关系

### 2.1 三个参与方

```text
+-------------------+        QUIC + HTTP/3 + CONNECT-UDP        +-------------------+       UDP       +---------------------+
|                   |  <-------------------------------------->  |                   |  <---------->  |                     |
|      Client       |                                            |   MASQUE Proxy    |                |  Target UDP Server  |
|                   |                                            |                   |                |                     |
+-------------------+                                            +-------------------+                +---------------------+
```

三个参与方的含义：

| 参与方 | 作用 |
|---|---|
| Client | 业务客户端，也是 MASQUE 客户端；负责连接 MASQUE Proxy，并把业务 UDP Payload 放进 CONNECT-UDP 隧道 |
| MASQUE Proxy | UDP 代理；一侧接收 Client 的 HTTP/3 CONNECT-UDP，另一侧创建普通 UDP socket 访问 Target |
| Target UDP Server | 真正的目标 UDP 服务；例如 DNS、STUN、RTP、机器人控制 UDP 服务、自定义 UDP 服务等 |

---

### 2.2 两段连接

MASQUE CONNECT-UDP 不是 Client 到 Target 的端到端直连，而是两段连接：

```text
第一段：Client <-> MASQUE Proxy
    协议栈：UDP / QUIC / HTTP/3 / Extended CONNECT / HTTP Datagram

第二段：MASQUE Proxy <-> Target UDP Server
    协议：普通 UDP
```

第一段的协议栈可以理解为：

```text
应用层 UDP Payload
  ↓
HTTP Datagram
  ↓
HTTP/3
  ↓
QUIC
  ↓
UDP/IP
```

这句话的含义不是“五条连接”，而是 **Client 到 Proxy 这一段的协议封装层次**：

| 层次 | 作用 | 在 CONNECT-UDP 中的含义 |
|---|---|---|
| UDP/IP | 最外层网络承载 | QUIC 本身运行在 UDP 之上，所以 Client 与 Proxy 之间底层仍是 UDP 报文 |
| QUIC | 安全传输层 | 提供 TLS 1.3 加密、连接管理、拥塞控制、多路复用、QUIC Datagram 能力 |
| HTTP/3 | 应用协议层 | 运行在 QUIC 上，用于发送请求头、响应头、SETTINGS，以及承载 HTTP Datagram 语义 |
| Extended CONNECT | 建隧道控制面 | Client 通过 `:method = CONNECT` 和 `:protocol = connect-udp` 请求 Proxy 创建 UDP 代理隧道 |
| HTTP Datagram | 隧道数据面 | 隧道建立后，Client 与 Proxy 之间的 UDP Payload 通过 HTTP Datagram 传输 |

第二段是普通 UDP：

```text
MASQUE Proxy UDP socket  <----普通 UDP---->  Target UDP Server
```

Target UDP Server 看到的源地址通常是 **MASQUE Proxy 的 UDP 地址和端口**，不是 Client 的真实地址。

---

### 2.3 HTTP Datagram 与 Media over QUIC / MoQ 的关系

`HTTP Datagram` 和 `Media over QUIC / MoQ` 不是同一层级的协议。

在 MASQUE CONNECT-UDP 中，`HTTP Datagram` 是 **Client 与 MASQUE Proxy 之间** 用来承载 UDP Payload 的隧道数据面封装。

如果业务是 RTP / SRTP / 自定义 UDP：

```text
RTP / SRTP / 自定义 UDP Payload
  ↓
HTTP Datagram: Context ID = 0 + UDP Payload
  ↓
外层 QUIC：Client <-> MASQUE Proxy
```

如果业务本身是 Media over QUIC / MoQ，则它本身已经运行在一条业务 QUIC 连接上。通过普通 CONNECT-UDP 转发时，可以理解为：**内层媒体 QUIC 包被当成 UDP Payload，再放进外层 MASQUE QUIC 隧道**。

```text
MoQ / Media QUIC 应用数据
  ↓
内层 QUIC Packet：Client <-> Target UDP Server
  ↓
作为 UDP Payload 交给 MASQUE CONNECT-UDP
  ↓
HTTP Datagram: Context ID = 0 + 内层 QUIC Packet
  ↓
外层 QUIC 连接：Client <-> MASQUE Proxy
  ↓
Proxy 解封装后，用普通 UDP 发给 Target
```

因此：

| 概念 | 所在位置 | 作用 | MASQUE 是否理解其内容 |
|---|---|---|---|
| HTTP Datagram | Client 与 MASQUE Proxy 之间 | 承载 CONNECT-UDP 的 UDP Payload | 只理解 `Context ID = 0`，不理解业务内容 |
| Media over QUIC / MoQ | 业务 Client 与媒体 Server 之间 | 传输媒体对象、音视频数据或实时媒体流 | MASQUE 不理解，只把内层 QUIC Packet 当作普通 UDP Payload |
| RTP / SRTP | 业务 Client 与媒体 Server 之间 | 传统实时音视频媒体包 | MASQUE 不理解，只负责转发字节 |

---

## 3. 核心对象与映射关系

### 3.1 `ClientConn`

`ClientConn` 表示 Client 到某一个 MASQUE Proxy 的连接。底层是一条 QUIC 连接，上面运行 HTTP/3。

一个 `ClientConn` 可以承载多个 CONNECT-UDP 会话：

```text
ClientConn，底层一条 Client <-> Proxy QUIC 连接
  ├── CONNECT-UDP Request Stream A -> Target A
  ├── CONNECT-UDP Request Stream B -> Target B
  └── CONNECT-UDP Request Stream C -> Target C
```

---

### 3.2 `Conn`

`clientConn.Dial(req)` 返回的 `conn` 类型是：

```go
*masque.Conn
```

它对上层表现得像 `net.PacketConn`：

```go
conn.WriteTo(payload, nil)
conn.ReadFrom(buffer)
```

但它不是普通 UDP socket，而是一个绑定到固定 Target UDP Server 的 CONNECT-UDP 逻辑连接。

---

### 3.3 Request Stream 与 Target 的映射

普通 CONNECT-UDP 中，不同目标服务不是靠 `Context ID` 区分，而是靠 **不同 HTTP/3 Request Stream** 区分。

```text
一条 QUIC Connection
  ├── Request Stream A -> CONNECT-UDP -> 8.8.8.8:53
  ├── Request Stream B -> CONNECT-UDP -> 192.168.123.100:7400
  └── Request Stream C -> CONNECT-UDP -> stun.example.com:3478
```

Proxy 侧对应维护：

```text
Request Stream A -> udpConn A -> 8.8.8.8:53
Request Stream B -> udpConn B -> 192.168.123.100:7400
Request Stream C -> udpConn C -> stun.example.com:3478
```

`Context ID = 0` 只表示 HTTP Datagram 中承载的是普通 UDP Payload，不用于区分不同目标服务。

---

## 4. 流程零：前置准备

### 4.1 流程目标

在正式发起 CONNECT-UDP 请求之前，三端都需要具备基本运行条件：

```text
Target UDP Server：
    在目标 IP:Port 上创建 UDP socket 并 bind() 监听。

MASQUE Proxy：
    启动 HTTP/3 / QUIC 服务，监听客户端连接，并注册 CONNECT-UDP Handler。

Client：
    准备 MASQUE Proxy 地址、URI Template 和目标 UDP 服务地址，后续用于构造 CONNECT-UDP 请求。
```

前置准备完成后，后续四个主流程才可以串起来：

```text
Client 发 CONNECT-UDP 请求
  -> Proxy 创建后端 udpConn
  -> Client 发送 HTTP Datagram
  -> Proxy 转成普通 UDP 发给 Target
  -> Target 回包给 Proxy
  -> Proxy 封装 HTTP Datagram 发回 Client
```

---

### 4.2 Target UDP Server 启动监听 socket

Target UDP Server 需要先在目标地址上启动一个普通 UDP Server。这个目标地址就是后续 CONNECT-UDP 请求中的 `target_host:target_port`。

例如本文示例中的 Target 是：

```text
192.168.123.100:7400
```

那么 Target UDP Server 需要在 `192.168.123.100` 这台机器上监听 UDP `7400` 端口。

Python 示例：

```python
import socket

TARGET_LISTEN_IP = "0.0.0.0"   # 也可以写成 "192.168.123.100"
TARGET_LISTEN_PORT = 7400

sock = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
sock.bind((TARGET_LISTEN_IP, TARGET_LISTEN_PORT))

print(f"Target UDP Server listening on {TARGET_LISTEN_IP}:{TARGET_LISTEN_PORT}")

while True:
    data, addr = sock.recvfrom(2048)
    print("received from", addr, data)

    reply = b"reply: " + data
    sock.sendto(reply, addr)
```

这里的监听 socket 是普通 UDP socket：

```text
Target UDP Server
  bind 0.0.0.0:7400
  或 bind 192.168.123.100:7400
```

`0.0.0.0:7400` 表示监听本机所有网卡的 UDP 7400 端口；如果只希望从某个网卡接收，可以绑定具体地址，例如 `192.168.123.100:7400`。

需要注意：Proxy 后续执行 `net.DialUDP()` 创建的是 **Proxy 侧后端 UDP socket**，不会替 Target 创建监听 socket。Target 是否先监听，不影响 Proxy 创建 UDP socket 这个动作；但如果 Target 没有监听，后续业务 UDP Payload 可能无人接收，Client 也收不到有效响应。

---

### 4.3 MASQUE Proxy 启动 HTTP/3 / QUIC 服务

MASQUE Proxy 需要提前启动，等待 Client 的 CONNECT-UDP 请求。

Proxy 前置准备包括：

```text
1. 监听 HTTP/3 / QUIC 端口，例如 443 或 4443。
2. 配置 TLS 证书，因为 HTTP/3 基于 QUIC + TLS。
3. 配置与 Client 一致的 URI Template。
4. 注册 CONNECT-UDP Handler，用于处理 :protocol = connect-udp 的请求。
5. 支持 Extended CONNECT。
6. 支持 HTTP Datagram / QUIC Datagram。
7. 根据需要增加认证、白名单、目标端口限制等安全策略。
```

例如 Proxy 对外暴露的服务地址和模板可以是：

```text
Proxy 地址：
    https://proxy.example.com:443

URI Template：
    https://proxy.example.com:443/masque/{target_host}/{target_port}
```

在 `masque-go` 服务端，典型逻辑是：

```go
template := uritemplate.MustNew(
    "https://proxy.example.com:443/masque/{target_host}/{target_port}",
)

var proxy masque.Proxy

mux.HandleFunc("/masque/", func(w http.ResponseWriter, r *http.Request) {
    preq, err := masque.ParseProxyRequest(r, template)
    if err != nil {
        // 返回 400 / 405 / 501 等错误
        return
    }

    // 可在这里做认证、目标地址白名单、端口限制等策略检查

    if err := proxy.Proxy(w, preq); err != nil {
        // 记录代理失败原因
        return
    }
})
```

这个阶段 Proxy 只是在监听 Client 的 HTTP/3 CONNECT-UDP 请求，还没有创建到 Target 的 UDP socket。

```text
Proxy 启动阶段：
    监听 Client -> Proxy 的 HTTP/3 / QUIC 连接
    注册 CONNECT-UDP 路由
    准备解析 URI Template

收到 CONNECT-UDP 请求后：
    才会解析 target_host / target_port
    才会 net.DialUDP() 创建 Proxy -> Target 的后端 udpConn
```

---

### 4.4 Client 准备 CONNECT-UDP 请求参数

Client 需要提前知道两个地址：

| 地址 | 在哪里配置 | 含义 |
|---|---|---|
| MASQUE Proxy 地址 | URI Template 的 scheme + authority | Client 要连接的代理服务，例如 `https://proxy.example.com:443` |
| Target UDP Server 地址 | `masque.NewRequest()` 的 `target` 参数 | Proxy 最终要转发到的 UDP 服务，例如 `192.168.123.100:7400` |

Client 侧准备示例：

```go
template := uritemplate.MustNew(
    "https://proxy.example.com:443/masque/{target_host}/{target_port}",
)

target := "192.168.123.100:7400"

req, err := masque.NewRequest(ctx, template, target)
```

这里需要区分：

```text
https://proxy.example.com:443
    是 MASQUE Proxy 地址。

/masque/{target_host}/{target_port}
    是 Client 与 Proxy 约定的 URI Template 路径格式。

192.168.123.100:7400
    是 Target UDP Server 地址。
```

Client 在前置阶段只是准备参数和传输配置。真正创建 CONNECT-UDP 隧道发生在后续调用：

```go
conn, rsp, err := clientConn.Dial(req)
```

或者使用快捷方式：

```go
conn, rsp, err := transport.Dial(req)
```

---

### 4.5 前置准备后的连接关系

前置准备完成后，系统状态如下：

```text
Client：
    已知道 Proxy 地址和 Target 地址
    已准备好 CONNECT-UDP Request

MASQUE Proxy：
    已监听 HTTP/3 / QUIC 端口
    已注册 CONNECT-UDP Handler
    尚未创建到 Target 的后端 udpConn

Target UDP Server：
    已 bind TargetIP:TargetPort
    等待普通 UDP 报文
```

随后进入流程一：Client 发起 CONNECT-UDP 连接请求。

---

### 4.6 Target 监听 socket 与 MASQUE 的关系

Target UDP Server 不需要理解 MASQUE、QUIC、HTTP/3、CONNECT-UDP、HTTP Datagram 或 `Context ID`。

从 Target 的视角，它只是在和一个普通 UDP 客户端通信：

```text
MASQUE Proxy IP:ProxyRandomPort  --->  TargetIP:TargetPort
```

也就是说，Target 看到的源地址通常是 MASQUE Proxy 创建的后端 UDP socket 地址，而不是原始 Client 地址。

当 Target 收到数据时：

```python
data, addr = sock.recvfrom(2048)
```

这里的 `addr` 通常是：

```text
MASQUE Proxy IP:ProxyRandomPort
```

Target 回包时直接使用：

```python
sock.sendto(reply, addr)
```

这样回包会先到 MASQUE Proxy，再由 Proxy 封装成 HTTP Datagram 发回 Client。

---

### 4.7 Target 监听 socket 的消息格式

Target 侧收到和发出的都是普通 UDP 报文。

收到请求时：

```text
UDP/IP Header:
    src = ProxyIP:ProxyRandomPort
    dst = TargetIP:TargetPort

UDP Payload:
    Client 原始业务 Payload
```

发送响应时：

```text
UDP/IP Header:
    src = TargetIP:TargetPort
    dst = ProxyIP:ProxyRandomPort

UDP Payload:
    Target 原始响应 Payload
```

这一步是后续 MASQUE 转发流程的业务前置条件：**如果 Target 没有在目标 IP:Port 上启动 UDP socket 监听，Proxy 即使成功创建 CONNECT-UDP 隧道，也无法让目标应用处理业务 UDP Payload。**

---

## 5. 流程一：连接请求

### 5.1 流程目标

连接请求阶段的目标是：

```text
Client 告诉 MASQUE Proxy：
    请帮我创建一条到某个 Target UDP Server 的 UDP 代理隧道。
```

例如：

```text
MASQUE Proxy 地址：proxy.example.com:443
Target UDP Server 地址：192.168.123.100:7400
```

Client 想表达的是：

```text
请 proxy.example.com:443 帮我转发 UDP 到 192.168.123.100:7400。
```

---

### 5.2 URI Template

CONNECT-UDP 使用 URI Template 表达目标地址。典型模板如下：

```text
https://proxy.example.com:443/masque/{target_host}/{target_port}
```

其中：

| 部分 | 含义 |
|---|---|
| `https://proxy.example.com:443` | MASQUE Proxy 的地址，即 Client 要连接的代理服务 |
| `/masque` | Proxy 端约定的 HTTP 路由前缀，不是协议强制字段 |
| `{target_host}` | 目标 UDP 服务的 host，例如 `192.168.123.100` |
| `{target_port}` | 目标 UDP 服务的 port，例如 `7400` |

`/masque` 不是必须叫这个名字，它只是工程上的路由前缀，用来让 Proxy 的 HTTP/3 Server 把请求分发到 CONNECT-UDP Handler。

以下写法都可以，只要 Client 与 Proxy 使用同一个 URI Template：

```text
https://proxy.example.com:443/masque/{target_host}/{target_port}
https://proxy.example.com:443/udp/{target_host}/{target_port}
https://proxy.example.com:443/.well-known/masque/udp/{target_host}/{target_port}/
https://proxy.example.com:443/{target_host}/{target_port}
https://proxy.example.com:443/masque?h={target_host}&p={target_port}
```

工程上更推荐保留类似 `/masque`、`/udp`、`/.well-known/masque/udp` 这样的前缀，避免和健康检查、管理接口、其他 API 混在一起。

---

### 5.3 `masque-go` 客户端调用

客户端构造请求：

```go
template := uritemplate.MustNew(
    "https://proxy.example.com:443/masque/{target_host}/{target_port}",
)

req, err := masque.NewRequest(
    ctx,
    template,
    "192.168.123.100:7400",
)
```

`masque.NewRequest()` 做的事情：

1. 将 `target` 拆成 `target_host` 和 `target_port`。
2. 展开 URI Template。
3. 构造 HTTP/3 Extended CONNECT 请求。
4. 设置 `:protocol = connect-udp`。
5. 设置 `Capsule-Protocol: ?1`。

展开结果：

```text
https://proxy.example.com:443/masque/192.168.123.100/7400
```

---

### 5.4 连接请求消息格式

从 HTTP/3 语义看，CONNECT-UDP 请求可以表示为：

```http
HEADERS
:method = CONNECT
:protocol = connect-udp
:scheme = https
:authority = proxy.example.com:443
:path = /masque/192.168.123.100/7400
capsule-protocol = ?1
```

字段含义：

| 字段 | 含义 |
|---|---|
| `:method = CONNECT` | 表示这是 CONNECT 请求 |
| `:protocol = connect-udp` | 表示使用 Extended CONNECT 创建 UDP 代理隧道 |
| `:scheme = https` | 表示使用 HTTPS / HTTP/3 语义 |
| `:authority = proxy.example.com:443` | MASQUE Proxy 的 authority，不是 Target 地址 |
| `:path = /masque/192.168.123.100/7400` | 由 URI Template 展开得到，里面携带 Target UDP Server 地址 |
| `capsule-protocol = ?1` | 表示启用 Capsule Protocol，CONNECT-UDP 需要该能力配合 HTTP Datagram 使用 |

注意：普通 RFC 9298 / `masque-go` 中，目标地址不是通过 `connect-udp-target` Header 传递，而是通过 URI Template 中的 `{target_host}` 和 `{target_port}` 进入 `:path` 或 query。

---

### 5.5 连接请求在协议栈中的位置

连接请求属于 **控制面**，走 HTTP/3 Request Stream：

```text
Client
  ↓
HTTP/3 HEADERS: CONNECT + :protocol=connect-udp
  ↓
QUIC Stream
  ↓
UDP/IP
  ↓
MASQUE Proxy
```

此时还没有开始发送业务 UDP Payload。业务数据要等连接响应成功后，才通过 HTTP Datagram 发送。

---

## 6. 流程二：连接响应

### 6.1 流程目标

连接响应阶段的目标是：

```text
MASQUE Proxy 接受 CONNECT-UDP 请求，
解析 Target UDP Server 地址，
创建 Proxy 到 Target 的后端 UDP socket，
然后向 Client 返回成功响应。
```

这里隐含一个前提：Target UDP Server 已经在对应的 `TargetIP:TargetPort` 上启动 UDP socket 监听，例如 `192.168.123.100:7400`。Proxy 创建 `udpConn` 只是建立 Proxy 侧的后端发送/接收 socket，并不会替 Target 创建监听 socket。

---

### 6.2 Proxy 解析请求

Proxy 端使用同一个 URI Template 解析请求：

```go
preq, err := masque.ParseProxyRequest(r, template)
```

例如请求 path 是：

```text
/masque/192.168.123.100/7400
```

解析结果：

```text
preq.Target = "192.168.123.100:7400"
preq.Host   = "proxy.example.com:443"
```

---

### 6.3 Proxy 创建后端 UDP socket

Proxy 接受请求后，会为这一条 CONNECT-UDP Request Stream 创建一个后端 UDP socket。

逻辑等价于：

```go
targetAddr, err := net.ResolveUDPAddr("udp", preq.Target)
udpConn, err := net.DialUDP("udp", nil, targetAddr)
```

注意：

- `net.DialUDP()` 对 UDP 来说没有 TCP 那种三次握手。
- 它主要是在 Proxy 本机创建一个 UDP socket，并固定远端地址为 Target。
- 后续这一条 CONNECT-UDP 隧道上的所有业务 UDP Payload，都会复用这个 `udpConn` 发送给同一个 Target。

建立后的映射关系：

```text
HTTP/3 Request Stream B
  -> Target = 192.168.123.100:7400
  -> Proxy 后端 udpConn B
  -> ProxyIP:RandomPort <-> 192.168.123.100:7400
```

---

### 6.4 连接响应消息格式

Proxy 成功接受请求后，返回 HTTP/3 响应头：

```http
HEADERS
:status = 200
capsule-protocol = ?1
proxy-status = proxy.example.com:443; next-hop="192.168.123.100:7400"
```

其中最小成功响应可以理解为：

```http
HEADERS
:status = 200
capsule-protocol = ?1
```

字段含义：

| 字段 | 含义 |
|---|---|
| `:status = 200` | CONNECT-UDP 隧道创建成功 |
| `capsule-protocol = ?1` | 响应侧确认该请求流启用 Capsule Protocol |
| `proxy-status` | 可选；用于携带 Proxy 状态，例如解析出的 next-hop 或失败原因 |

`masque-go` 客户端只要收到 2xx 状态码，就认为 CONNECT-UDP 隧道建立成功。

---

### 6.5 失败响应格式

如果请求失败，Proxy 可以返回非 2xx 状态码。

例如鉴权失败：

```http
HEADERS
:status = 401
www-authenticate = Bearer
```

目标被策略拒绝：

```http
HEADERS
:status = 403
proxy-status = proxy.example.com:443; error=destination_ip_prohibited
```

目标 DNS 解析失败或不可达：

```http
HEADERS
:status = 502
proxy-status = proxy.example.com:443; error=dns_error
```

请求格式错误：

```http
HEADERS
:status = 400
```

---

### 6.6 连接响应后的状态

连接响应成功后，系统进入数据转发状态。

此时有两段状态：

```text
Client <-> Proxy：
    已建立 QUIC + HTTP/3 连接
    已建立一个 CONNECT-UDP Request Stream

Proxy <-> Target：
    已创建一个后端 connected UDP socket
```

后续业务数据不再走 CONNECT 请求头，而是走 HTTP Datagram。

---

## 7. 流程三：发送消息

### 7.1 流程目标

发送消息阶段的目标是：

```text
Client 把业务 UDP Payload 发送给 Target UDP Server。

但路径不是 Client 直接发给 Target，
而是 Client -> MASQUE Proxy -> Target UDP Server。
```

---

### 7.2 Client 侧发送 API

Client 使用 `*masque.Conn` 发送数据：

```go
payload := []byte("hello")
_, err := conn.WriteTo(payload, nil)
```

这里传入的 `payload` 是业务层原始 UDP Payload。

`WriteTo()` 中的 `addr` 参数通常会被忽略，因为普通 `masque-go` 的一个 `Conn` 已经绑定到了固定 Target。

```go
conn.WriteTo(payload, nil)      // 推荐理解方式
conn.WriteTo(payload, someAddr) // someAddr 不用于选择目标
```

---

### 7.3 Client 到 Proxy 的消息格式：HTTP Datagram

CONNECT-UDP 建立成功后，Client 到 Proxy 的业务数据通过 HTTP Datagram 传输。

`masque-go` 在业务 UDP Payload 前面添加 `Context ID = 0`：

```text
HTTP Datagram Payload
+----------------+----------------------+
| Context ID = 0 | UDP Proxying Payload |
+----------------+----------------------+
```

其中：

| 字段 | 含义 |
|---|---|
| `Context ID = 0` | 表示后面是普通 UDP Payload |
| `UDP Proxying Payload` | Client 要发送给 Target UDP Server 的原始业务数据 |

例如业务 payload 是：

```text
hello
```

则 HTTP Datagram 的语义可以表示为：

```text
Context ID = 0
UDP Proxying Payload = "hello"
```

在 HTTP/3 底层，HTTP Datagram 会被关联到某个 HTTP/3 Request Stream。也就是说，Proxy 收到 Datagram 时不仅看到 `Context ID = 0`，还知道它属于哪个 CONNECT-UDP Request Stream。

因此真正决定转发目标的是：

```text
所属 HTTP/3 Request Stream -> 找到对应 udpConn -> 找到对应 Target
```

不是：

```text
Context ID -> 选择 Target
```

---

### 7.4 Proxy 侧转发逻辑

Proxy 收到 Client 发来的 HTTP Datagram 后，处理逻辑是：

```text
ReceiveDatagram()
  ↓
根据 QUIC/HTTP/3 关联关系确定所属 Request Stream
  ↓
查找该 Request Stream 对应的 udpConn
  ↓
解析 HTTP Datagram Payload 中的 Context ID
  ↓
Context ID == 0：取出 UDP Payload
  ↓
udpConn.Write(payload)
  ↓
普通 UDP 发给 Target UDP Server
```

对于普通 CONNECT-UDP：

- `Context ID = 0`：有效 UDP 数据。
- `Context ID != 0`：普通 `masque-go` 不作为 UDP payload 处理，通常丢弃或忽略。

---

### 7.5 Proxy 到 Target 的 UDP 消息格式

Proxy 发给 Target UDP Server 的是普通 UDP 包。

从 Target 视角看：

```text
UDP/IP Header:
    src = ProxyIP:ProxyRandomPort
    dst = TargetIP:TargetPort

UDP Payload:
    Client 原始业务 Payload
```

例如：

```text
ProxyIP:53124 -> 192.168.123.100:7400
payload = "hello"
```

Target UDP Server 不知道也不关心 MASQUE、HTTP/3、QUIC、Context ID。它只看到一个普通 UDP 客户端，也就是 Proxy 的 UDP socket。

---

## 8. 流程四：回复响应

### 8.1 流程目标

回复响应阶段的目标是：

```text
Target UDP Server 收到 UDP 请求后，
回包给 MASQUE Proxy 的后端 UDP socket，
Proxy 再封装成 HTTP Datagram 发回 Client。
```

---

### 8.2 Target UDP Server 回包逻辑

Target 是普通 UDP Server，可以用 Python 表示为：

```python
import socket

sock = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
sock.bind(("0.0.0.0", 7400))

while True:
    data, addr = sock.recvfrom(2048)
    print("received from", addr, data)

    reply = b"reply: " + data
    sock.sendto(reply, addr)
```

关键点是：

```python
sock.sendto(reply, addr)
```

这里的 `addr` 是 `recvfrom()` 返回的源地址。对于 MASQUE 场景，它通常是：

```text
MASQUE Proxy IP:ProxyRandomPort
```

不是 Client 的真实地址。

---

### 8.3 UDP socket 是否只能一对一

Target UDP Server 使用的是普通 UDP socket：

```python
sock = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
sock.bind(("0.0.0.0", 7400))
```

这种 `bind + recvfrom + sendto` 模型可以接收多个来源地址：

```text
ProxyIP:51001 -> Target:7400
ProxyIP:51002 -> Target:7400
ProxyIP:51003 -> Target:7400
```

每次 `recvfrom()` 都会返回本次报文的源地址：

```python
data, addr = sock.recvfrom(2048)
```

Target 回包时，只要回给本次的 `addr` 即可：

```python
sock.sendto(reply, addr)
```

只有当 UDP socket 主动调用 `connect()` 后，它才更像固定一个 peer 的一对一 UDP socket。Target UDP Server 通常不调用 `connect()`，而是用 `recvfrom()` 和 `sendto()` 支持多个来源。

---

### 8.4 Target 到 Proxy 的 UDP 消息格式

Target 回给 Proxy 的仍然是普通 UDP 包：

```text
UDP/IP Header:
    src = TargetIP:TargetPort
    dst = ProxyIP:ProxyRandomPort

UDP Payload:
    Target 原始响应 Payload
```

例如：

```text
192.168.123.100:7400 -> ProxyIP:53124
payload = "reply: hello"
```

---

### 8.5 Proxy 转回 Client 的 HTTP Datagram 格式

Proxy 从后端 `udpConn.Read()` 读到 Target 的响应后，会重新封装为 HTTP Datagram：

```text
HTTP Datagram Payload
+----------------+----------------------+
| Context ID = 0 | UDP Proxying Payload |
+----------------+----------------------+
```

其中：

```text
Context ID = 0
UDP Proxying Payload = Target 返回的原始 UDP Payload
```

例如：

```text
Context ID = 0
UDP Proxying Payload = "reply: hello"
```

然后 Proxy 通过该 `udpConn` 所属的 HTTP/3 Request Stream 发回 Client。

---

### 8.6 Client 接收响应

Client 使用同一个 `*masque.Conn` 接收响应：

```go
buf := make([]byte, 1500)
n, addr, err := conn.ReadFrom(buf)
```

`masque-go` 内部处理：

```text
ReceiveDatagram()
  ↓
解析 Context ID
  ↓
Context ID == 0：取出 UDP Payload
  ↓
copy 到用户 buffer
  ↓
ReadFrom() 返回
```

上层应用最终看到的是 Target 的原始 UDP Payload：

```text
reply: hello
```

---

## 9. 完整时序图

```text
Client                         MASQUE Proxy                         Target UDP Server
  |                                  |                                      |
  | 0. prepare proxyTemplate         |                                      |
  |    and target address            |                                      |
  |                                  | 0. listen HTTP/3 / QUIC              |
  |                                  |    register CONNECT-UDP Handler      |
  |                                  |                                      | 0. bind UDP socket
  |                                  |                                      |    listen TargetIP:TargetPort
  |                                  |                                      |
  | 1. CONNECT-UDP Request           |                                      |
  |--------------------------------->|                                      |
  | HEADERS:                         |                                      |
  | :method=CONNECT                  |                                      |
  | :protocol=connect-udp            |                                      |
  | :path=/masque/target/port        |                                      |
  | capsule-protocol=?1              |                                      |
  |                                  |                                      |
  |                                  | 解析 target_host / target_port        |
  |                                  | 创建 udpConn                         |
  |                                  | udpConn -> Target                    |
  |                                  |                                      |
  | 2. CONNECT-UDP Response          |                                      |
  |<---------------------------------|                                      |
  | HEADERS:                         |                                      |
  | :status=200                      |                                      |
  | capsule-protocol=?1              |                                      |
  | proxy-status=... next-hop=...    |                                      |
  |                                  |                                      |
  | 3. Send Message                  |                                      |
  |--------------------------------->|                                      |
  | HTTP Datagram:                   |                                      |
  | Context ID=0 + UDP Payload       |                                      |
  |                                  |                                      |
  |                                  | UDP Payload                          |
  |                                  |------------------------------------->|
  |                                  | ProxyIP:RandomPort -> TargetIP:Port |
  |                                  |                                      |
  |                                  | 4. Reply Response                    |
  |                                  |<-------------------------------------|
  |                                  | UDP Payload                          |
  |                                  | TargetIP:Port -> ProxyIP:RandomPort |
  |                                  |                                      |
  |<---------------------------------|                                      |
  | HTTP Datagram:                   |                                      |
  | Context ID=0 + UDP Payload       |                                      |
  |                                  |                                      |
```

---

## 10. 多目标服务如何区分

一条 Client 到 Proxy 的 QUIC 连接可以承载多个 CONNECT-UDP 服务，但每个服务对应一个独立的 CONNECT-UDP Request Stream。

客户端示例：

```go
req1, _ := masque.NewRequest(ctx, template, "8.8.8.8:53")
conn1, _, _ := clientConn.Dial(req1)

req2, _ := masque.NewRequest(ctx, template, "192.168.123.100:7400")
conn2, _, _ := clientConn.Dial(req2)

req3, _ := masque.NewRequest(ctx, template, "stun.example.com:3478")
conn3, _, _ := clientConn.Dial(req3)
```

映射关系：

```text
clientConn，一条 QUIC 连接
  ├── conn1 / Request Stream A -> udpConn A -> 8.8.8.8:53
  ├── conn2 / Request Stream B -> udpConn B -> 192.168.123.100:7400
  └── conn3 / Request Stream C -> udpConn C -> stun.example.com:3478
```

三个流里的 HTTP Datagram 都可以使用：

```text
Context ID = 0
```

因为 Context ID 的作用不是区分目标服务，而是在某条 Request Stream 内说明 Datagram 的语义。

---

## 11. 关键结论

1. MASQUE CONNECT-UDP 是两段通信：

```text
Client <-> MASQUE Proxy：QUIC + HTTP/3 + CONNECT-UDP + HTTP Datagram
MASQUE Proxy <-> Target UDP Server：普通 UDP
```

2. Target UDP Server 需要先在目标 IP:Port 上启动普通 UDP socket 监听，例如：

```python
sock = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
sock.bind(("0.0.0.0", 7400))
```

3. 连接请求是 HTTP/3 Extended CONNECT 请求：

```http
:method = CONNECT
:protocol = connect-udp
:path = /masque/{target_host}/{target_port}
capsule-protocol = ?1
```

4. 连接响应是 HTTP/3 响应头：

```http
:status = 200
capsule-protocol = ?1
```

5. Client 发送业务数据时，消息格式是：

```text
HTTP Datagram Payload = Context ID 0 + UDP Payload
```

6. Proxy 转发给 Target 时，消息格式变回普通 UDP：

```text
UDP Payload = Client 原始业务 Payload
```

7. Target 回包时，只需要普通 UDP 回包：

```python
sock.sendto(reply, addr)
```

8. Proxy 收到 Target 回包后，再封装为：

```text
HTTP Datagram Payload = Context ID 0 + UDP Payload
```

9. 多个目标服务靠多个 HTTP/3 Request Stream / 多个 `*masque.Conn` 区分，不靠 `Context ID` 区分。

10. `/masque` 只是 HTTP 路由前缀，不是协议强制字段；path 可以换成其他形式，只要 URI Template 中包含 `{target_host}` 和 `{target_port}`，并且 Client 与 Proxy 使用同一个模板。
