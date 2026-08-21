# Next: persistent QoderContext worker

## Proven

We can encode + POST a new chat using a live `QoderContext.prepareInferRequest`:

- rewrite-hook captured context from one qodercli request
- second call with fresh request_id returned real SSE (200)
- no Duplicate request

## Build this next

A long-lived worker that:

1. Boots once:
   - init wasm
   - decrypt `~/.qoder/.auth/user`
   - create `QoderContext`
2. Serves:
   - `POST /v1/chat/completions` (or `/internal/chat`)
3. Per request:
   - build plaintext body (from captured schema)
   - `prepareInferRequest(endpoint, body, modelKey, modelSource)`
   - fetch SSE
   - translate nested SSE → OpenAI chunks / final JSON

## Minimal plaintext template

Use fields from `/tmp/qoder-wasm-spike/last-plain.json`:

- ids: `request_id`, `request_set_id`, `chat_record_id`, `session_id`
- `stream: true`
- `chat_task: FREE_INPUT`
- `model_config`
- `system` / `messages` / `tools` / `parameters` / `business`

First MVP can keep tools empty or minimal if upstream allows; validate carefully.

## Acceptance

- worker stays up
- 10 chats without respawning full qodercli agent runtime
- p50 near upstream 2-4s
