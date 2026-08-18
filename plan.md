# RAYNEO-ctrl 介绍物品与 IMAGE_REASONING 计划

## Summary
在 `RAYNEO-ctrl` 配置页的物品选择区下方新增“介绍物品”区域，包含 QoS endpoint 输入框、一排 `Describe Yellow / Red / Green` 按钮，以及“切换为识别模式”。控制端继续通过眼镜 TCP remote 协议发送命令；Describe 按钮会先调用本地 QoS client 的 `POST /send_qos`，收到 HTTP 200 后再触发眼镜端 IMAGE_REASONING。眼镜端向 sandbox endpoint `POST /api/v1/image/reasoning` 提交当前机器狗视频帧 Base64；下一次语音识别模式会在 Whisper 转写后先发 QoS，再发 reasoning。

## Key Changes
- `RAYNEO-ctrl`
  - 在 `objectSelectionActionGroup` 下方、物品抓取区上方新增标题 `介绍物品`、QoS endpoint 文本框和 Describe 按钮组。
  - QoS endpoint 文本框默认值为 `192.168.1.10:9666`，保存到 SharedPreferences；请求 URL 规范为 `http://{endpoint}/send_qos`。
  - Describe 按钮使用与 `Select Yellow/Red/Green` 相同的三色横排样式：
    - `Describe Yellow` -> `CONFIG_ACTION {"id":"9","label":"Describe Yellow"}`
    - `Describe Red` -> `CONFIG_ACTION {"id":"10","label":"Describe Red"}`
    - `Describe Green` -> `CONFIG_ACTION {"id":"11","label":"Describe Green"}`
  - Describe 按钮流程：先 `POST /send_qos`，body 为空；仅 HTTP status 等于 `200` 时继续发送对应 `CONFIG_ACTION` 给眼镜。QoS 失败或非 200 时不发 TCP Describe 命令，并在配置页 response 区显示错误。
  - `切换为识别模式` 改为 `CONFIG_ACTION {"id":"12","label":"切换为识别模式","qosEndpoint":"192.168.1.10:9666"}`。该按钮不立即发 QoS，而是把当前 endpoint 交给眼镜端用于下一次 voice reasoning。
  - 新增 `QosEndpoint` / `QosClient`，复用现有禁用/恢复控件、响应显示和错误展示模式。

- `RAYNEO` remote/action handling
  - `CONFIG_ACTION` 协议新增可选字段 `qosEndpoint`；旧格式仍兼容。
  - 眼镜端识别 `id=9/10/11` 为立即 IMAGE_REASONING，分别使用当前 UI 语言生成 message：
    - `id=9`：中文 `介绍黄色盒子`，英文 `describe the yellow box`
    - `id=10`：中文 `介绍红色盒子`，英文 `describe the red box`
    - `id=11`：中文 `介绍绿色盒子`，英文 `describe the green box`
  - 眼镜端识别 `id=12` 为下一次语音覆盖模式：保存 `qosEndpoint`，设置一次性 flag，只影响下一次录音转写消息；不影响后续配置按钮消息。
  - 下一次语音被消费后立刻清除 flag，即使发送失败也不继续影响后续消息。
  - 下一次语音 reasoning 流程为：录音 -> Whisper 转写 -> `POST http://{qosEndpoint}/send_qos` -> 收到 HTTP 200 -> 抓取当前机器狗视频帧 -> `POST /api/v1/image/reasoning`。QoS 失败时不发 reasoning。

- IMAGE_REASONING request
  - 新增 sandbox image reasoning endpoint：`POST {resolvedSandboxBase}/api/v1/image/reasoning`。
  - 请求 JSON 固定为：
    ```json
    {
      "computing_session_id": "comp-sess-xxxx",
      "action": "IMAGE_REASONING",
      "message": "介绍红色盒子",
      "image": "<base64-jpeg>"
    }
    ```
  - `computing_session_id` 复用 core_network computing 成功后保存在 `SessionSnapshot.computingSessionId` 的真实值，不在眼镜端随机生成。
  - `message` 对 Describe 按钮使用当前 UI 语言文本；对下一次语音识别模式使用 Whisper 转写文本。
  - `image` 从当前 WebRTC 机器狗视频画面抓帧，使用左眼 `SurfaceViewRenderer` 当前帧，通过 `PixelCopy` 截取并 JPEG/Base64 编码。

- QoS request
  - `RAYNEO-ctrl` 和 `RAYNEO` 都通过空 body `POST /send_qos` 触发 `client_rayneo/run_client.py` 中的硬编码 `PAYLOAD`。
  - 默认 endpoint 为 `192.168.1.10:9666`，允许输入 `ip:port` 或带 scheme 的 HTTP/HTTPS URL。
  - 仅 HTTP status `200` 被视为成功；其它 2xx 目前也按失败处理，避免与需求“收到 200OK”不一致。

## Failure Behavior
- Describe 按钮如果 QoS endpoint 无效、`POST /send_qos` 失败、或返回非 HTTP 200：
  - 不发送眼镜 TCP `CONFIG_ACTION`。
  - 控制端配置页 response 区显示 QoS/transport 错误。
- 下一次语音识别模式如果 QoS endpoint 无效，眼镜端拒绝 `id=12` 并返回 `ERR INVALID_CONFIG_ACTION`。
- 下一次语音 reasoning 如果 Whisper 成功但 QoS 失败或非 HTTP 200：
  - 不发送 IMAGE_REASONING。
  - 眼镜端 HUD/日志显示失败原因。
  - 一次性 flag 已清除，不继续影响后续语音。
- 如果没有 `computing_session_id`、当前没有可用视频帧、PixelCopy 失败、或 sandbox endpoint 请求失败：
  - 不发送不完整请求。
  - 眼镜端 remote 返回 `ERR NOT_READY` 或在 HUD/日志显示未就绪原因。
  - “下一次语音识别模式”flag 在该次语音尝试后清除。
- reset、前后台停止、physical reset 时清除一次性 IMAGE_REASONING flag，避免 stale 状态残留。

## Test Plan
- `RAYNEO-ctrl`
  - UI wiring test：确认新增标题、QoS endpoint 输入框、Describe 三色按钮、布局位置在物品选择区下方。
  - Protocol test：确认 `id=9/10/11/12` 构造出的 `CONFIG_ACTION` wire line 正确，且 `id=12` 可携带 `qosEndpoint`。
  - QoS endpoint/client test：确认 `192.168.1.10:9666` 解析为 `http://192.168.1.10:9666/send_qos`，且 client 发送空 body `POST /send_qos`。
  - MainActivity wiring test：确认 Describe 走 `sendDescribeConfigAction`，先 `qosClient.sendQos` 再发送 TCP；识别模式走 `sendSwitchRecognitionModeAction` 并携带 endpoint。

- `RAYNEO`
  - Remote protocol/executor test：`CONFIG_ACTION id=9/10/11` 触发 image reasoning；`id=12` 设置一次性下一语音 flag 并保留 `qosEndpoint`。
  - QoS client test：确认眼镜端空 body `POST /send_qos`，并校验默认 endpoint 解析。
  - Image reasoning client test：确认 POST path 为 `/api/v1/image/reasoning`，body 只包含 `computing_session_id/action/message/image`，`action` 为 `IMAGE_REASONING`。
  - Routing test：开启下一语音模式后，下一条 voice transcript 不走原 `SANDBOX_UP -> /api/v1/voice`，而是先走 `/send_qos`，成功后走 `/api/v1/image/reasoning`，随后 flag 清除。
  - Failure test：缺少 `computing_session_id` 或图片时不发请求并返回未就绪。

## Assumptions
- sandbox 后端 endpoint 不在当前工作区内，本计划只实现客户端调用约定。
- 新 endpoint 固定为 `/api/v1/image/reasoning`，不从 `computing_instance` 动态读取。
- 图片来源是眼镜端当前渲染的机器狗视频帧，不是手势相机帧。
- `切换为识别模式` 只影响下一次语音转写消息，不影响配置按钮消息。
- `client_rayneo/run_client.py` 的 `POST /send_qos` body 可以为空，服务端会发送脚本内硬编码的 `PAYLOAD`。
- `Describe Yellow/Red/Green` 的 QoS 前置请求由 `RAYNEO-ctrl` 发起；下一次语音 reasoning 的 QoS 前置请求由 `RAYNEO` 眼镜端在 Whisper 之后发起。
