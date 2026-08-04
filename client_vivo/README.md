# MASQUE Python Client 最小运行包

这个目录用于在终端或智能眼镜侧通过 Python 调用已经编译好的 Go Client。

## 文件说明

| 文件 | 作用 |
|---|---|
| `client.exe` | Go 编译出的 MASQUE Client，可建立到 Proxy 的 CONNECT-UDP 隧道，并通过隧道向 Target 发送数据 |
| `masque_client.py` | Python 工具模块，负责读取 `config.json`、启动 `client.exe -session`、封装 `connect()` / `send()` / `close()` |
| `run_client.py` | Python 运行入口，适合直接在终端执行 |
| `config.json` | 运行配置文件，保存 Client 连接 Proxy、Target 以及重传机制相关参数 |

## 运行方式

```powershell
cd D:\zxy\qos\client_vivo
python .\run_client.py
```

运行前需要确认 Proxy 和 Target 已经启动，并且 `config.json` 里的 IP 地址正确。

## TLS 证书信任

`config.json` 默认使用：

```json
"insecure": false
```

这表示 Client 会校验 Proxy 的 TLS 证书。如果在其他主机运行时报错：

```text
x509: certificate signed by unknown authority
```

需要把签发 Proxy 证书的 CA 根证书安装到当前 Client 主机。

在生成 Proxy 证书的机器上执行：

```powershell
mkcert -CAROOT
```

把输出目录里的 `rootCA.pem` 复制到当前 Client 主机，然后用管理员 PowerShell 执行：

```powershell
certutil -addstore -f Root .\rootCA.pem
```

注意：必须安装“签发 Proxy 证书的那个 CA”。只在 Client 主机重新执行 `mkcert -install` 不会自动信任 Proxy 机器上已有 CA 签发的证书。

还需要确认 Proxy 证书包含 `config.json` 里的 Proxy IP。例如：

```json
"proxy": "https://192.168.1.45:443"
```

则 Proxy 证书必须包含 `192.168.1.45`。如果 Proxy IP 变化，需要在 Proxy 侧重新生成 `cert.pem` / `key.pem`，并让 Proxy 使用新证书。

## run_client.py

`run_client.py` 是业务入口脚本，主要包含：

```python
PAYLOAD = "hello"
```

`PAYLOAD` 是本次要发送给 Target 的业务数据。如果需要修改发送内容，直接修改这里。

```python
def main(argv: Sequence[str] | None = None) -> int:
```

`main()` 的执行流程：

1. 检查命令行参数。当前不支持命令行覆盖配置，如果传入参数会提示修改 `config.json`。
2. 调用 `client_from_config()` 读取 `config.json` 并创建 Client。
3. 调用 `client.connect()` 建立 MASQUE session。
4. 调用 `client.send(PAYLOAD)` 发送业务数据。
5. 打印 Target 返回的响应。
6. 调用 `client.close()` 关闭 session。

## config.json

`config.json` 用来配置连接参数和可靠性参数，不配置业务 payload。

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

| 字段 | 作用 |
|---|---|
| `client_exe` | 要启动的 Go Client 程序路径。写 `client.exe` 表示使用当前目录下的文件 |
| `proxy` | MASQUE Proxy 地址 |
| `target` | Target UDP Server 地址 |
| `timeout_seconds` | Python 等待 `client.exe` 响应和退出的超时时间 |
| `insecure` | 是否跳过 TLS 证书校验。默认建议设为 `false`；只有自签证书本地测试时才临时设为 `true` |
| `ttl` | Target 缓存已处理请求的时间 |
| `max_entries` | Target 最多缓存的请求数量 |
| `max_payload` | 允许发送的最大原始业务 payload 字节数 |
| `max_retries` | Client 超时后的最大重试次数 |
| `timeout` | Client 单次等待响应的超时时间 |

如果 Proxy 或 Target IP 变化，只需要修改 `proxy` 和 `target`。如果需要修改发送内容，改 `run_client.py` 里的 `PAYLOAD`。
