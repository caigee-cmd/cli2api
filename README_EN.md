# CLI2API

[中文](README.md) · [Issues](https://github.com/caigee-cmd/cli2api/issues) · [Contributing](CONTRIBUTING.md)

[![CI](https://github.com/caigee-cmd/cli2api/actions/workflows/ci.yml/badge.svg)](https://github.com/caigee-cmd/cli2api/actions/workflows/ci.yml)
[![Docker Image](https://ghcr-badge.egpl.dev/caigee-cmd/cli2api/latest_tag?label=docker&color=blue)](https://github.com/caigee-cmd/cli2api/pkgs/container/cli2api)
[![License](https://img.shields.io/github/license/caigee-cmd/cli2api)](LICENSE)

<p align="center">
  <img src="./docs/assets/readme/hero-en.svg" width="100%" alt="CLI2API — your Qoder CLI login, served as a local OpenAI-compatible API">
</p>

> **A self-hosted multi-account gateway** — aggregates your own Qoder Global, Qoder CN, and WorkBuddy logins into one local OpenAI-compatible API. Long-lived warm workers, multi-account scheduling with failover, a single Docker container, and a web console.

Unofficial project, not affiliated with or endorsed by Qoder. Use only accounts you are authorized to use, and follow the terms of Qoder and related services.

![Supported login methods, account types, endpoints, and deployment targets](docs/assets/overview-card.png)

## Features

- **OpenAI-compatible proxy**: `/v1/chat/completions`, `/v1/models` — streaming/non-streaming, tool calls, `reasoning_content`
- **Multi-channel account pool**: Qoder Global / Qoder CN / WorkBuddy with region isolation, account pinning, concurrency limits, cooldowns, and same-family failover
- **Long-lived warm workers**: one isolated Node process and runtime directory per account keeps authentication, WASM encoding, and cloud SSE connections warm. Typical small-chat latency is ~1-2s after warmup, versus ~10s+ for spawn-per-request wrappers
- **Multiple login methods**: browser Device Flow OAuth, PAT, and `qoder-native-v1` credential import/export
- **Web console**: accounts, models, access, request history, and runtime logs, with light and dark themes
- **Deployment and ops**: single Docker Compose container, safe managed updates (pre-update snapshot, automatic rollback on failure, next-version-only upgrades), binds `127.0.0.1` by default
- **Cross-platform**: `linux/amd64` / `linux/arm64` images; macOS and Windows run them through Docker Desktop

## Quick start

Requirements: Docker (Docker Desktop on macOS/Windows, Docker Engine + Compose on Linux) and a Qoder account you control. On Windows, Docker Desktop must use Linux containers.

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

Without an account header the scheduler picks a ready account; pin a request with the `X-Qoder-Account: acc_...` header. curl / PowerShell examples in the [deployment guide](deploy/README.md).

## How it works

<p align="center">
  <img src="./docs/assets/readme/architecture-en.svg" width="100%" alt="CLI2API architecture: OpenAI clients are routed by the Go control plane to one isolated Node worker per account, then to the Qoder cloud">
</p>

Each enabled account gets its own Node process and runtime directory so Qoder WASM state is not shared across accounts. Go owns persistence, scheduling, concurrency limits, cooldowns, failover, and child-process lifecycle.

## Console

<p align="center">
  <img src="./docs/assets/readme/console-window-en.svg" width="100%" alt="CLI2API console Accounts page: each account shows its login method, ready state, and quota, with an Access panel offering the Base URL and a quick check">
</p>

Accounts, models, access, and logs all live in one web console. Each account signs in on its own (browser OAuth, PAT, or credential import), readiness and quota are visible at a glance, and the Access page lets you copy the Base URL and run a quick check.

## Use cases

- Connect Qoder / WorkBuddy to local or private-server tooling
- Reuse OpenAI-compatible clients and scripts
- Route requests across multiple accounts with failover
- Keep login state available without starting a full CLI Agent per request

CLI2API is a local gateway: it does not provide accounts, quotas, or an official API service, and it is not a shared multi-user resale service.

## Roadmap

The full checklist lives in [docs/PLAN.md](docs/PLAN.md).

**In progress**

- Live-account acceptance for Qoder CN and WorkBuddy (login, failover, mixed account pools)
- Phase I: Anthropic `/v1/messages` ingress adapter and the canonical conversation contract

**Planned**

- Session-sticky routing: prefer reusing one account per session to improve upstream cache hit rates
- WorkBuddy daily check-in and token keepalive (per-account opt-in, off by default)
- Request history improvements: per-account filtering and usage statistics

**Longer term**

- More upstream channels (Cursor, TraeWork, etc.; evaluated after WorkBuddy acceptance)
- Optional prompt/completion capture behind an explicit switch (off by default)

## Documentation

- [Deployment and operations: setup steps, environment variables, endpoints, managed updates](deploy/README.md)
- [Development and release workflow](docs/DEVELOPMENT.md)
- [Architecture, login, routing, and console design](docs/DESIGN.md)
- [Milestones and development plan](docs/PLAN.md)
- [Multi-upstream account type comparison](docs/PROVIDERS.md)
- [Changelog](CHANGELOG.md)

## Security

The service binds `127.0.0.1:3010` by default and every endpoint requires the API key. Never commit `.qoder`, tokens, cookies, auth blobs, or raw captures; credential export is an explicit sensitive operation — protect exported files. Upstream API or CLI changes may affect compatibility; qodercli is pinned and checked. Please report security issues privately according to [SECURITY.md](SECURITY.md).

## Contributing

Issues, documentation improvements, and pull requests are welcome — see [CONTRIBUTING.md](CONTRIBUTING.md).

## License

[MIT](LICENSE) — for personal learning use; please follow the terms of each upstream platform.
