// Package kimi implements the China-region Kimi Code (Moonshot AI) in-process
// provider.
//
// Kimi Code is a subscription product, not the Moonshot Open Platform. The two
// must not be mixed: the subscription authorizes with an OAuth device grant and
// talks to https://api.kimi.com/coding/v1, while the Open Platform uses an
// sk-… API key against api.moonshot.cn/v1. This adapter implements the former.
//
// # Login
//
// The device grant has three steps and no callback listener: ask
// {authHost}/api/oauth/device_authorization for a code, show the user
// verification_uri_complete, then poll {authHost}/api/oauth/token until the
// browser round completes. Kimi Code's CLI, desktop app and the older kimi-cli
// all use the same public client id, so a token minted here is the same token
// the official client would hold. Accounts can also paste the credential the
// official client already wrote to <KIMI_CODE_HOME>/credentials/kimi-code.json.
//
// Import is supported; export is not. A Kimi Code credential is a bearer token
// pair, so handing it back out of the console would hand out the account. The
// descriptor still advertises ImportExport because that capability covers the
// import side, and the console export action reports "unsupported" on purpose.
//
// # Tokens
//
// Tokens are stored in the provider credential and refreshed five minutes before
// expiry, single-flighted per account. Kimi rotates the refresh token on every
// refresh, so the rotated pair is persisted immediately and a failed write fails
// the request: continuing with an in-memory-only token would leave the stored
// refresh token already-invalidated. A rejected refresh surfaces as an auth
// failure and asks for a new login.
//
// # Chat
//
// The managed endpoint speaks OpenAI Chat Completions with four Kimi-specific
// deviations the adapter applies: reasoning travels as a top-level `thinking`
// object instead of `reasoning_effort`, only `max_completion_tokens` is
// accepted, `temperature` only accepts 0.6 (thinking off) or 1.0 (on), and the
// `developer` role is rewritten to `system`. Streams are returned to the gateway
// unchanged because they are already chat-completions deltas.
//
// # Catalog and quota
//
// GET /models is the source of truth for the model list and for every
// capability: context window, declared thinking mode and efforts, image input,
// tool use. The adapter never invents a capability the server did not declare.
// GET /me supplies the display profile, and GET /usages supplies the four
// subscription windows (5-hour, 7-day, monthly, monthly-code) as used fractions.
// The `boosterWallet` block is parsed by the upstream but not surfaced: the
// console has no prepaid-balance slot, so it stays an unverified metric instead
// of being forced into a percentage window.
//
// The managed /models list is not scoped to the subscription tier: a plan can
// list a model it cannot call, and the upstream then answers with an
// authentication-shaped "your current subscription does not have access to
// <model>. Upgrade to higher-tier Kimi Code plans" error. That is a model
// entitlement, not an account auth failure, so it classifies as
// model_not_available and the executor evicts the model for that account; the
// account itself stays ready.
package kimi
