# Qoder API Proxy Plan

last-updated: 2026-08-21

Qoder-first OpenAI-compatible proxy. Cursor and other CLIs are later providers, not this milestone.

## Boundary

Do:

- Reuse one local Qoder login
- Serve `POST /v1/chat/completions`
- Keep a hot `QoderContext` and call cloud HTTP/SSE directly
- Self-host on a private Docker network

Do not (this milestone):

- Spawn a full `qodercli` agent per request
- Public exposure without `PROXY_API_KEY`
- Commercial multi-user resale of one login
- Cursor / other CLI providers

## Status

| Phase | Result |
|-------|--------|
| A protocol | Confirmed `COSY.*` request-scoped auth, `Encode=1` body, nested SSE |
| B non-stream MVP | Worker encodes via live WASM context; Go proxy fronts OpenAI JSON |
| C usable | Real streaming, tool calls, reasoning passthrough, rewarm/self-heal, React console |

Typical small-chat latency is ~1-2s after warmup, versus ~10s+ for spawn-CLI wrappers.

## Runtime

```text
Client
  -> qoder-api-proxy (:3010)
    -> qoder-auth-worker (:3020, hot QoderContext)
      -> https://api1.qoder.sh/.../agent_chat_generation?Encode=1
```

Worker pins `@qoder-ai/qodercli@1.1.27` and patches WASM capture needles. If the CLI source no longer matches, startup fails with a version-aware error instead of running half-broken.

## Auth

- Console `/api/*` and `/v1` require `PROXY_API_KEY` when it is set
- Worker `/admin/*` and chat require the same key
- `/health` stays open for probes

## Next

- Exact upstream token accounting
- Multi-account rotation
- Pure-wasm boot without importing qodercli
- Cursor provider (separate milestone)
