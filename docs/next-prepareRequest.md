# Worker boot

The auth worker keeps one live `QoderContext` and encodes each chat through official WASM instead of spawning the full CLI agent.

## Boot

1. Pin `@qoder-ai/qodercli@1.1.27` and patch capture needles in `worker/src/compat.mjs`.
2. Skip CLI `main` when the skip-main needle is present (`pure-wasm`).
3. Init wasm + local auth from `QODER_HOME` / `~/.qoder`.
4. Serve chat on `:3020`. Later requests reuse the same context.

If needles no longer match, startup fails with a version-aware error.

Fallback: one-shot warmup import when skip-main is missing or `QODER_SKIP_CLI_MAIN=0`.

## Per request

1. Build plaintext from the caller + `PLAIN_TEMPLATE_PATH` (defaults to `worker/last-plain.sample.json`)
2. `prepareInferRequest(endpoint, body, modelKey, modelSource)`
3. POST nested SSE
4. Translate to OpenAI chunks / final JSON

## Multi-account

Qoder WASM is process-global. `QODER_HOMES=acc1=/root,acc2=/home/acc2` starts `worker/src/supervisor.mjs`, one daemon per HOME.

## Acceptance

- Worker stays up across many chats
- No full `qodercli` agent spawn per request
- Small-chat latency close to upstream after warmup
