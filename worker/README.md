# qoder account daemon

Runs one hot `QoderContext` for one Qoder account HOME.

The Go control plane starts one daemon per enabled SQLite account. This process does
not select accounts, persist account metadata, or fail over to another daemon.

## Boot

1. Verify pinned `@qoder-ai/qodercli@1.1.27` compatibility.
2. Patch the WASM capture and skip-main hooks.
3. Load local Qoder auth from the assigned HOME.
4. Reuse the hot context for HTTP/SSE chat requests.

## Standalone development

```bash
HOME=/path/to/account-home \
WORKER_HOST=127.0.0.1 WORKER_PORT=3020 \
PROXY_API_KEY=dev-key ALLOW_INSECURE_API_KEY=1 \
npm start
```

Health is open. Chat and `/admin/*` require `PROXY_API_KEY` when configured.
