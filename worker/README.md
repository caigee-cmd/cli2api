# qoder-auth-worker

Keeps a hot `QoderContext` and encodes/executes upstream Qoder chat.

## How it boots

1. Check the pinned CLI bundle (`@qoder-ai/qodercli@1.1.27` by default).
2. Patch WASM capture needles (`prepareInferRequest` / `createWasmContext`) and skip CLI `main`.
3. Import qodercli, init wasm + local auth, adopt `QoderContext`. No warmup chat.
4. Later requests reuse that context; they do not cold-start a full agent CLI.

If the CLI source no longer matches the pinned needles, the worker **exits with a version-aware error** instead of starting half-broken.

Set `QODERCLI_JS` to the `qodercli.js` bundle path.

## Run

```bash
export QODERCLI_JS=/path/to/node_modules/@qoder-ai/qodercli/bundle/qodercli.js
export WORKER_PORT=3020
export PROXY_API_KEY=dev-key
export ALLOW_INSECURE_API_KEY=1
npm start
```

Health (`/health`) is open. Chat and `/admin/*` require `PROXY_API_KEY` when it is set.  
Placeholder keys (`change-me` / empty) refuse to boot unless `ALLOW_INSECURE_API_KEY=1`.

## Tests

```bash
npm test
```

## Multi-account

Set `QODER_HOMES=acc1=/root,acc2=/home/acc2`. The supervisor spawns one daemon per HOME because Qoder WASM is process-global.
