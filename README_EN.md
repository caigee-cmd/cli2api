<div align="center">

# CLI2API

**Turn your own logins into a local OpenAI-compatible API**

Supports **Qoder Global**, **Qoder CN**, **WorkBuddy Global**, **WorkBuddy CN**, **Trae CN Work**, and experimental **Devin** (`provider=devin`, browser OAuth / session token import; not claimed production-ready).

Long-lived account runtimes, multi-account scheduling. Deploy with Docker; that is the supported install and update path.

[![License](https://img.shields.io/github/license/caigee-cmd/cli2api)](LICENSE)
[![LINUX DO](https://img.shields.io/badge/LINUX%20DO-community-ff6a00)](https://linux.do)

<sub>[中文](README.md) · [Issues](https://github.com/caigee-cmd/cli2api/issues) · [LINUX DO](https://linux.do)</sub>

<img src="./docs/assets/readme/hero-en.svg" width="100%" alt="CLI2API — turn your own logins into a local OpenAI-compatible API">

</div>

## Features

- **OpenAI / Anthropic-compatible proxy**: `/v1/chat/completions`, `/v1/responses`, `/v1/messages`, `/v1/models` — streaming/non-streaming text and function tools; image support depends on the provider (currently supported by Qoder, not WorkBuddy / Trae); file inputs are rejected explicitly. `messages` / `responses` are stateless adapters today and do not support server-side conversations or upstream-specific tools.
- **Multi-channel account pool**: region isolation, account pinning, concurrency limits, cooldowns, and same-family failover
- **Outbound proxies**: set one global HTTP(S) proxy or override it per account; use `direct` / `none` for explicit direct access. SOCKS5 is available for WorkBuddy / Trae / Devin account-level proxies only; Qoder account-level proxies are HTTP(S) only
- **Provider-specific login methods**: browser Device Flow OAuth, PAT, and credential import/export where supported
- **Web console**: accounts, models, access, request history, and runtime logs, with light and dark themes; request history can be filtered by account and shows status, latency, token, and usage statistics
- **Keepalive**: WorkBuddy daily check-in and token keepalive (per-account opt-in, off by default; console can check in now / refresh credits)
- **Deployment and ops**: single Docker Compose container, safe managed updates (pre-update snapshot, automatic rollback on failure, jump to the latest stable release, roll back to one of the three previous stables), binds `127.0.0.1` by default; `linux/amd64` / `linux/arm64` images, with macOS and Windows running them through Docker Desktop

## Quick start

**Deploy with Docker.** Published images and console managed updates are built around the single Compose container; running the Go / Node sources directly is not on that update path.

Requirements: Docker (Docker Desktop on macOS/Windows, Docker Engine + Compose on Linux) and a Qoder, WorkBuddy, Trae, or experimental Devin account you control. On Windows, Docker Desktop must use Linux containers.

```bash
git clone https://github.com/caigee-cmd/cli2api.git
cd cli2api
./scripts/start.sh        # Windows: scripts\start.ps1
```

The first startup generates a random API key and prints it once in the logs — save it. Then open `http://127.0.0.1:3010`, sign in, and add accounts from **Accounts**. Full steps in the [deployment guide](deploy/README.md).

## Connect a client

Any OpenAI-compatible client (OpenAI SDKs, Codex, CherryStudio, …) works out of the box:

```text
Base URL: http://127.0.0.1:3010/v1
API Key:  <the key printed on first startup>
```

Without an account header the scheduler picks a ready account; pin a request with the `X-Qoder-Account: acc_...` header (a historical name that applies to every provider). Multi-turn requests stick to the same account from the first user message (including image-only turns) by default; `X-CLI2API-Session` is an optional override. curl / PowerShell examples in the [deployment guide](deploy/README.md).

## How it works

<p align="center">
  <img src="./docs/assets/readme/architecture-en.svg" width="100%" alt="CLI2API architecture: OpenAI clients are routed by the Go control plane to one isolated runtime per account, then to the provider upstream">
</p>

Each enabled account gets an isolated runtime: Qoder uses its own Node process, HOME, and WASM context, while WorkBuddy / Trae / Devin use in-process adapters. Go owns persistence, scheduling, concurrency limits, cooldowns, failover, and the lifecycle of providers that need child processes.

## Console

<p align="center">
  <img src="./docs/assets/readme/console-window-en.svg" width="100%" alt="CLI2API console Accounts page: each account shows its login method, ready state, and quota, with an Access panel offering the Base URL and a quick check">
</p>

Accounts, models, access, and logs all live in one web console: readiness and quota are visible at a glance, and the Access page lets you copy the Base URL and run a quick check.

## Use cases

- Connect your upstream accounts on a local or private server, with automatic routing and failover across them
- Reuse OpenAI-compatible clients and scripts without starting a full CLI Agent per request

CLI2API is a local gateway: it does not provide accounts, quotas, or an official API service, and it is not a shared multi-user resale service.

## Roadmap

**In progress**

- Live-account acceptance for Qoder CN, WorkBuddy, and Trae CN Work (login, failover, mixed account pools)

**Longer term**

- More upstream channels (Cursor, etc.)
- Optional prompt/completion capture behind an explicit switch (off by default)

## Documentation

- [Deployment and operations: setup steps, environment variables, endpoints, managed updates](deploy/README.md)
- [Changelog](CHANGELOG.md)

## Security

The service binds `127.0.0.1:3010` by default; all APIs and console data endpoints require the API key except `/health`, static frontend assets, and CORS preflight `OPTIONS` for the OpenAI-compatible `/v1/*` endpoints. Never commit `.qoder`, tokens, cookies, auth blobs, or raw captures; credential export is an explicit sensitive operation — protect exported files. Upstream API or CLI changes may affect compatibility; qodercli is pinned and checked. Please report security issues privately according to [SECURITY.md](SECURITY.md).

## Community & Contributing

Chinese-language discussion is on [LINUX DO](https://linux.do). Bugs and feature requests go to GitHub [Issues](https://github.com/caigee-cmd/cli2api/issues); documentation improvements and pull requests are welcome — see [CONTRIBUTING.md](CONTRIBUTING.md).

## License

[MIT](LICENSE) — for personal learning use; please follow the terms of each upstream platform.
