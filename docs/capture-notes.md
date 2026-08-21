# Phase A Capture Notes

last-updated: 2026-08-21

> 只记脱敏事实。不要提交 Authorization / cookie / auth blob / raw capture。

## Confirmed endpoints

```text
center  = https://center.qoder.sh
openapi = https://openapi.qoder.sh
infer   = https://api1.qoder.sh
```

## Critical upstream calls

| Step | Method | URL | Notes |
|------|--------|-----|-------|
| endpoint election | GET | `center:/algo/api/v3|v4/service/region/endpoints` | CLI 启动时多次 |
| userinfo | GET | `openapi:/api/v1/userinfo` | |
| user status | GET | `openapi:/api/v3/user/status` | |
| chat | POST | `api1:/algo/api/v2/service/pro/sse/agent_chat_generation?FetchKeys=llm_model_result&AgentId=agent_common&Encode=1` | **真正推理**，SSE |
| finish | POST | `api1:/algo/api/v2/service/business/finish?Encode=1` | 收尾 |
| model list | GET | `api1:/algo/api/v2/model/list?Encode=1` | |

## Auth / header facts (已抓到)

一次成功 `qodercli` 请求的 chat headers（值已脱敏）：

```text
Accept: text/event-stream
Authorization: Bearer COSY.<payload>.<sig>   # 不是普通 JWT；request-scoped
Content-Type: application/json
Cosy-Business-Product: cli
Cosy-Business-Type: agent
Cosy-ClientType: 5
Cosy-Data-Policy: disagree
Cosy-Date: <unix-ish>
Cosy-Key: <base64 blob>
Cosy-MachineId: <uuid>
Cosy-MachineToken: <uuid/token>
Cosy-MachineType: 5
Cosy-Organization-Id: <uuid>
Cosy-Organization-Tags: Normal
Cosy-Scene: assistant
Cosy-User: <uuid>
Cosy-Version: 1.1.27
Login-Version: v2
X-Model-Key: auto
X-Model-Source: system
Cosy-MachineOS: x86_64_linux
Cosy-MachineHostname: <host>
traceparent: ...
X-Qoder-HTTPDNS-IP: <ip>
```

`Authorization` 解析：

```text
Bearer COSY.<base64json>.<sig>
base64json ~= {
  "version": "v1",
  "requestId": "<uuid>",
  "info": "<encrypted blob>",
  "cosyVersion": "1.1.27",
  "ideVersion": ""
}
```

结论：**chat 鉴权不是长期 PAT 明文直传**，而是每次请求生成的 `COSY.*` 票据。

## Body facts（关键阻塞）

- chat/finish body **不是明文 JSON**
- URL 带 `Encode=1`
- body 是大段编码字符串（一次最小请求约 150KB+）
- CLI 里通过 WASM 绑定 `qodercontext_prepareRequest(...)` 生成最终 `url/headers/body`
- 同请求原样 replay：
  - HTTP 200 + SSE
  - 首包：`{"code":"103","message":"Duplicate request"}` / 403
  - 说明请求有 **防重放 / requestId 绑定**

## Plaintext body shape（编码前，来自 bundle 字符串）

编码前对象大致包含：

```js
{
  session_type,
  model_config,   // { key, display_name, format, ... }
  custom_model,
  system,
  messages,
  tools,
  parameters,
  // optional patches / business
}
```

`sendRemoteChatAsk` 会 `JSON.stringify(A)`，再交给 prepare/encode 层。

## 这意味着什么

Phase A 已证明：

1. 上游确实可直打，不必 spawn 完整 agent runtime
2. 但直打门槛是：
   - 生成 `COSY.*` Authorization
   - 生成 `Cosy-Key` / `Cosy-Date` 等
   - 把 body 做 `Encode=1`
   - 处理 requestId 防重放

所以下一步不是“猜 OpenAI mapping”，而是二选一：

### Path 1（推荐，务实）
复用 `@qoder-ai/qodercli` 里的 **WASM prepareRequest**（或同等 native helper），我们只做：
- 组装明文 chat 对象
- 调 prepareRequest 得最终 headers/body
- 自己发 HTTP/SSE
- 自己转 OpenAI

这样仍避免完整 CLI 冷启动，但借用官方编码器。

### Path 2（更难）
完整逆向 encode + COSY token 算法。时间不可控。

## Capture method used

Capture host:

```bash
NODE_OPTIONS="--import /tmp/qoder_capture_preload2.mjs" \
  qodercli --print --output-format json --model auto \
  --dangerously-skip-permissions --cwd /tmp -- "只回复OK"
```

产物（仅服务器本地，勿提交）：

```text
/tmp/qoder-capture2/capture-*.jsonl
/tmp/qoder-capture2/auth-headers.raw.json
```

## Next actions

1. 在容器/本机定位 `prepareRequest` WASM 导出，写最小 Node helper：
   - input: plaintext chat JSON + auth context
   - output: final url/headers/body
2. 用 helper 发一次非重复 requestId 的 chat，确认能拿到正常 SSE 文本
3. 成功后把 `executor.ChatNonStream` 从 stub 换成真调用
4. 同步把 header/body 契约样本（脱敏）写入 `testdata/`



## Plaintext chat body（已抓到，2026-08-21）

通过 hook `JSON.stringify` 在 encode 前截获。关键字段：

```json
{
  "request_id": "<uuid>",
  "request_set_id": "<uuid>",
  "chat_record_id": "<uuid>",
  "session_id": "<uuid>",
  "stream": true,
  "chat_task": "FREE_INPUT",
  "chat_context": { "text": "<skills/system reminders...>" },
  "is_reply": ...,
  "is_retry": ...,
  "source": ...,
  "version": ...,
  "agent_id": ...,
  "task_id": ...,
  "session_type": ...,
  "aliyun_user_type": "",
  "model_config": { "key": "auto", "...": "..." },
  "custom_model": null,
  "system": "<default Qoder identity + tools...>",
  "messages": [{ "role": "user", "content": "..." }],
  "tools": [ /* many agent tools */ ],
  "parameters": { /* sampling etc */ },
  "business": { /* optional */ }
}
```

样本（服务器本地，勿提交）：

```text
/tmp/qoder-wasm-spike/last-plain.json
```

## WASM auth module（已定位）

- 嵌入在 `qodercli.js` 的 base64：`mAs="AGFzbQ..."`  
- 已提取：`/tmp/qoder-wasm-spike/qoder_auth_wasm_bg.wasm`（~297KB，magic=`\0asm`）
- JS 绑定同包内联，导出包括：
  - `qodercontext_new`
  - `qodercontext_prepareInferRequest`  ← chat 用这个
  - `qodercontext_prepareRequest`
  - `qodercontext_refreshAuthFields`
  - `credential_storage_decrypt` / `encrypt`
  - `decrypt_server_response`

Chat 路径：

```text
plaintext JSON
  -> QoderContext.prepareInferRequest(endpoint, body, modelKey, modelSource)
  -> { url, headers, body }   # Encode=1 + COSY token
  -> POST SSE
```



## Breakthrough: reuse live QoderContext.prepareInferRequest（已验证）

2026-08-21 用 Node module rewrite hook 改写 `qodercli.js` 的 `prepareInferRequest`：

1. 截获第一次真实编码调用（拿到 live `this` / QoderContext）
2. 用**新 request_id** 再调一次 `prepareInferRequest`
3. 直接 `fetch` 上游 SSE

结果：

- HTTP `200`
- `content-type: text/event-stream`
- 返回真实 delta（不是 Duplicate request）
- 证明：**只要有热的 QoderContext，就能脱离完整 agent 冷启动发 chat**

SSE 事件形态（嵌套）：

```text
data:{"headers":{"Content-Type":["application/json"]},"body":"<openai-chunk-json-string>","statusCodeValue":200,"statusCode":"OK"}
```

其中 `body` 是字符串化的 OpenAI chat.completion.chunk。

本地验证产物（勿提交）：

```text
/tmp/qoder-wasm-spike/rewrite.log
/tmp/qoder-wasm-spike/second-sse.txt
```


## 当前阻塞（更新）

不是“找不到协议”，而是：

1. `prepareInferRequest` 需要已初始化的 `QoderContext`
2. Context 依赖解密后的 `userInfoJson`（来自 `~/.qoder/.auth/user`）
3. WASM instance exports 只读；但可通过 rewrite `prepareInferRequest` / 常驻持有 QoderContext 复用

## 下一步（更具体）

优先做 **常驻 auth worker**：

1. 启动时 init 内嵌 wasm + `credential_storage_decrypt(user blob)`
2. `QoderContext.new(machineId, cosyVersion, userInfoJson, clientMeta)`
3. 每个请求：组明文 body → `prepareInferRequest` → fetch SSE
4. 这样可避开完整 qodercli agent 冷启动，只复用认证/编码层


## Non-goals

- 不要把 raw auth capture 提交进 git
- 不要继续在 spawn-qodercli wrapper 上加功能冒充“直打 API”
