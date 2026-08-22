# Protocol notes

last-updated: 2026-08-22

Redacted facts only. Do not commit Authorization values, cookies, auth blobs, or raw capture dumps.

## Endpoints

```text
center  = https://center.qoder.sh
openapi = https://openapi.qoder.sh
infer   = https://api1.qoder.sh
```

| Step | Method | URL | Notes |
|------|--------|-----|-------|
| endpoint election | GET | `center:/algo/api/v3\|v4/service/region/endpoints` | CLI startup |
| userinfo | GET | `openapi:/api/v1/userinfo` | |
| user status | GET | `openapi:/api/v3/user/status` | |
| chat | POST | `api1:/algo/api/v2/service/pro/sse/agent_chat_generation?FetchKeys=llm_model_result&AgentId=agent_common&Encode=1` | nested SSE |
| finish | POST | `api1:/algo/api/v2/service/business/finish?Encode=1` | |
| model list | GET | `api1:/algo/api/v2/model/list?Encode=1` | |

## Auth

Chat auth is not a long-lived PAT pasted into `Authorization`.  
Each request gets a request-scoped `Bearer COSY.<payload>.<sig>` plus Cosy machine/org headers.

Exact replay of the same encoded body/request id returns `Duplicate request`.

Header names (values redacted):

```text
Authorization: Bearer COSY.<payload>.<sig>
Cosy-Business-Product: cli
Cosy-Business-Type: agent
Cosy-ClientType: 5
Cosy-Date: <unix>
Cosy-Key: <blob>
Cosy-MachineId: <uuid>
Cosy-MachineToken: <token>
Cosy-Organization-Id: <uuid>
Cosy-User: <uuid>
Cosy-Version: 1.1.27
X-Model-Key: auto
X-Model-Source: system
```

See `testdata/chat_headers.redacted.json`.

## Body

- Chat/finish bodies are **not** plaintext JSON.
- URL query includes `Encode=1`.
- The official CLI encodes via WASM `QoderContext.prepareInferRequest(endpoint, body, modelKey, modelSource)`.
- This project reuses a hot `QoderContext` instead of reverse-engineering COSY/encode.

Plaintext shape before encode (sample in `worker/last-plain.sample.json`):

```text
request_id / request_set_id / chat_record_id / session_id
stream, chat_task=FREE_INPUT
model_config, system, messages, tools, parameters, business
```

## SSE

Upstream SSE is nested:

```text
data: {"headers":{"Content-Type":["application/json"]},"body":"<openai-chunk-json-string>","statusCodeValue":200}
```

`body` is a stringified OpenAI `chat.completion.chunk`. Worker unwraps it to OpenAI SSE/JSON.

If nested `usage` / `llm_model_result` is present, prefer it (`usage.source=upstream`). Otherwise estimate.

## Non-goals

- Do not commit raw captures or decrypted `~/.qoder/.auth/user`
- Do not spawn a full `qodercli` agent per request
