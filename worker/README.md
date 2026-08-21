# qoder-auth-worker

Keeps a hot `QoderContext` and encodes/executes upstream Qoder chat.

## How it boots

1. Check the pinned CLI bundle (`@qoder-ai/qodercli@1.1.27` by default).
2. Patch WASM capture needles (`prepareInferRequest` / `createWasmContext`).
3. Import qodercli once, adopt the live context, then stay alive.
4. Later requests reuse that context; they do not cold-start a full agent CLI.

If the CLI source no longer matches the pinned needles, the worker **exits with a version-aware error** instead of starting half-broken.

Set `QODERCLI_JS` to the `qodercli.js` bundle path.

## Run

```bash
export QODERCLI_JS=/path/to/node_modules/@qoder-ai/qodercli/bundle/qodercli.js
export WORKER_PORT=3020
export PROXY_API_KEY=change-me
npm start
```

Health (`/health`) is open. Chat and `/admin/*` require `PROXY_API_KEY` when it is set.

## Tests

```bash
npm test
```
