// Package command implements the Command Code (commandcode.ai) in-process
// provider.
//
// Command Code exposes three model-traffic surfaces. /provider/v1/messages
// (Anthropic) and /provider/v1/chat/completions (OpenAI) require the Pro plan
// or higher and return 403 upgrade_required on the $1 Go plan. /alpha/generate
// is the undocumented envelope the CLI itself uses on every turn; it is not
// plan-gated, so it serves every plan — and is the only generation path Go
// accounts can use. This adapter therefore always speaks /alpha/generate, which
// serves every model in the catalog regardless of that model's declared
// supported_endpoints.
//
// Auth is a single user_… Bearer key shared by the CLI and the API: no OAuth,
// no browser loopback, no device fingerprint. The catalog
// (GET /provider/v1/models) is readable anonymously.
//
// Protocol facts (the strict config envelope, the Vercel AI SDK ModelMessage[]
// message schema, the tool shape, and the newline-delimited-JSON event stream —
// which is NDJSON, NOT SSE) are derived from safzanpirani/pi-commandcode-provider
// under the MIT License. See NOTICE in this directory. This package rewrites
// those facts into the local Provider Adapter surface and imports no upstream
// packages.
package command
