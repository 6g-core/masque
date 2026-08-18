# MASQUE Python Client 使用说明

这个目录是客户端最小运行包，用 Python 调用已经编译好的 `client.exe`。

## 客户端运行步骤

第一次在当前客户端主机运行时，用管理员 PowerShell 执行一次证书安装：

```powershell
certutil -addstore -f Root .\rootCA.pem
```

然后启动客户端：

```powershell
python .\run_client.py
```

启动后客户端会先连接 Proxy，然后监听本机接口：

```text
http://127.0.0.1:18080/send_qos
```

需要发送 QoS 消息时，调用：

```powershell
Invoke-RestMethod `
  -Method Post `
  -Uri http://127.0.0.1:18080/send_qos
```

`POST /send_qos` 的 body 可以为空，客户端会发送 `run_client.py` 里写死的 `PAYLOAD`。

以后再次运行时，如果证书和 IP 没有变化，只需要先启动客户端：

```powershell
python .\run_client.py
```

## 客户端需要修改什么

如果客户端要连接的地址变化，修改 `config.json`：

```json
{
  "proxy": "https://192.168.1.45:443",
  "target": "192.168.1.45:7400"
}
```

如果客户端要发送的内容变化，修改 `run_client.py`：

```python
PAYLOAD = json.dumps({...}, ensure_ascii=False)
```

## 文件说明

| 文件 | 客户端用途 |
|---|---|
| `run_client.py` | 客户端启动入口，启动后连接 Proxy 并监听 `POST /send_qos` |
| `config.json` | 客户端连接配置，包括 Proxy 地址、Target 地址、TLS 校验和重传参数 |
| `masque_client.py` | Python 工具模块，负责启动 `client.exe` 并提供 `connect()` / `send()` / `close()` |
| `client.exe` | 实际建立 MASQUE 隧道和发送数据的程序 |
| `rootCA.pem` | 客户端用于信任 Proxy 证书的 CA 根证书 |

## run_client.py

`run_client.py` 里最常改的是：

```python
PAYLOAD = json.dumps({...}, ensure_ascii=False)
```

`main()` 会按下面流程执行：

1. 调用 `client_from_config()` 读取 `config.json` 并创建 Client。
2. 调用 `client.connect()` 建立 MASQUE session。
3. 监听 `127.0.0.1:18080`。
4. 收到 `POST /send_qos` 后调用 `client.send(PAYLOAD)` 发送业务数据。
5. 返回发送结果。
6. 程序退出时调用 `client.close()` 关闭 session。

## config.json

完整示例：

```json
{
  "client_exe": "client.exe",
  "proxy": "https://192.168.1.45:443",
  "target": "192.168.1.45:7400",
  "timeout_seconds": 10,
  "insecure": false,
  "ttl": "2m",
  "max_entries": 10000,
  "max_payload": 1000,
  "max_retries": 3,
  "timeout": "1s"
}
```

字段说明：

| 字段 | 客户端含义 |
|---|---|
| `client_exe` | 要启动的客户端程序，默认使用当前目录下的 `client.exe` |
| `proxy` | 客户端要连接的 MASQUE Proxy 地址 |
| `target` | 客户端最终要访问的 Target 地址 |
| `timeout_seconds` | Python 等待 `client.exe` 响应和退出的超时时间 |
| `insecure` | 是否跳过 TLS 证书校验，默认保持 `false` |
| `max_payload` | 客户端允许发送的最大原始 payload 字节数 |
| `max_retries` | 客户端超时后的最大重试次数 |
| `timeout` | 客户端单次等待响应的超时时间 |
| `ttl` / `max_entries` | 随配置保留，需和服务端可靠性配置保持一致 |

## TLS 证书问题

`insecure` 默认是 `false`，表示客户端会校验 Proxy 证书。

如果运行时报错：

```text
x509: certificate signed by unknown authority
```

说明当前客户端主机还没有信任 `rootCA.pem`，需要管理员 PowerShell 执行：

```powershell
certutil -addstore -f Root .\rootCA.pem
```

如果 Proxy IP 变化，客户端只需要修改 `config.json` 里的 `proxy` 地址。新的 Proxy 证书需要由 Proxy 维护方重新生成，并确保证书中包含新的 Proxy IP；客户端侧拿到正确证书后再运行即可。
