// Package codearts implements the Huawei Cloud CodeArts Agent (码道) in-process
// provider.
//
// CodeArts has four client forms (IDE, IDE extension, JetBrains plugin, CLI).
// This adapter implements the cloud-facing protocol they all share, not the
// CLI's local agent server. The protocol facts below were verified against
// codearts2api (MIT, commit 8d1e43d) and re-checked against the locally
// installed CodeArts Agent kernel; see NOTICE.
//
// # Login
//
// Huawei's public docs describe AK/SK for the CLI, but the personal-account
// OAuth flow is narrower and is what the official extension and the CLI use for
// chat: the portal at codearts.huaweicloud.com/portal issues an authorization
// code (PKCE S256, client_id codearts-agent, local callback
// 127.0.0.1:<port>/oauth/callback), and the code is exchanged at
// sts.cn-north-4.myhuaweicloud.com/v1/oauth2/tokens for a short-lived STS trio
// (access_key_id, secret_access_key, security_token). The refresh token is
// bound to the PKCE verifier and the DPoP P-256 key used for the first
// exchange, so both are stored with the credential. A rejected refresh
// surfaces as an auth failure and asks for a new login; a successful rotation
// is persisted before the request continues.
//
// Import is supported; export is not. A credential contains the STS keys and
// the refresh token, so handing it back out of the console would hand out the
// account. The descriptor advertises ImportExport because that capability
// covers the import side; the console export action reports unsupported.
//
// # Chat
//
// Chat is a direct HTTP/SSE OpenAI-compatible call to
// snap-access.cn-north-4.myhuaweicloud.com/api/v2/chat/completions. The request
// carries x-auth-token (the STS security token) and is signed with Huawei's
// SDK-HMAC-SHA256 AK/SK scheme. The body uses the native OpenAI shape; tools,
// tool_choice, reasoning_effort, max_tokens, temperature and top_p all pass
// through. tool_choice accepts only the string forms, so a named function
// choice is sent as "required" with the tools list narrowed to that function.
// Assistant tool calls and tool results are forwarded only when they form a
// complete pair; CodeArts rejects orphaned tool history with a 400 and then
// keeps failing every later turn.
//
// Streams are CodeArts/OpenAI SSE events. The adapter normalizes the event
// names and emits standard chat-completions deltas, a final usage chunk when
// the upstream supplies usage, and a single [DONE]. The non-stream path
// aggregates the same stream, so usage, reasoning and tool-call assembly share
// one parser.
//
// # Catalog and quota
//
// The catalog is per account and merges three upstream sources: the CodeAgent
// detail's gpts.models, GET /v1/model/builtin (Agent-Type: PromptCenter), and
// the limited-benefit gateway config. Each record declares its exact model id
// plus context/output limits; those declarations are the only capability
// source. The adapter declares tools because the chat endpoint natively
// accepts them, and forwards image_url content parts for models whose catalog
// entry advertises supports_images. A text-only model rejects image input with
// a 400 instead of silently dropping it. GET /v1/model/thinking-variants
// supplies each model's allowed reasoning strengths; the adapter exposes them
// as ReasoningOptions and forwards the selected level as reasoning_effort.
// Benefits models are marked and routed with maas_type: benefit; the adapter
// claims the benefit idempotently while discovering the catalog, mirroring the
// official client opening its model menu.
//
// The discovered list is not the same as the display list: CodeArts' agent
// and builtin catalogs are keyed by model_parameters.model_id, while
// model_name is only the label. Sending the display name (for example
// GLM-5.2-ArkTS-SPARK instead of glm-5.2-sft-harmony) returns 002002009. The
// adapter keeps model_id as NativeModel and model_name only as DisplayName.
//
// Benefits are a separate routing path. A model advertised by a successful
// benefit gateway/config is marked and sent with maas_type: benefit. A
// credit-only trial account has no benefit group: claim/config answer 4000
// invalid channel/group not found, and adding maas_type: benefit to the same
// model returns 4004.200 benefit not found. Crucially, the same model works
// over the credit path without that header. Therefore benefit discovery is
// additive and a failed claim/config never hides agent/builtin models. Only a
// real 002002009/4004.200 chat answer marks that model unusable for 30 minutes
// and removes it from /v1/models; the account stays ready because this is an
// entitlement failure, not auth.
//
// The catalog endpoint's values are capped for output: the upstream rejects
// max_tokens above 65536 even when the catalog advertises more, so the public
// ceiling is min(declared, 65536).
//
// GET /snap-manager/v1/statistics/plugin supplies the package meters
// (usageTotalPackageCredit, usageTokenChatMessages, ...). The adapter maps the
// visible meters into quota windows; the package has no per-meter expiry, so
// the FEFO layer treats it as unknown rather than inventing an end date.
// codearts stats is only a local session ledger and is not used.
//
// # Check-in
//
// The daily check-in and new-user credits are two credit campaigns delivered by
// the ops endpoints (/v1/ops/delivery, /v1/ops/claim, /v1/ops/confirm), not by
// the CLI. Claiming is idempotent and the confirm step is retried separately;
// the adapter reports the claimed amount and never presents it as the account
// balance.
package codearts
