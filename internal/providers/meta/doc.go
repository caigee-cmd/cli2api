// Package meta is the in-process Adapter for Meta Muse (provider=meta, alias muse).
//
// Login follows RFC 8628 device grant against auth.meta.com (CPA Muse Code):
// device authorization → token (dca:…) → POST muse-code/key mint → Bearer API key.
// Chat speaks OpenAI Responses at api.meta.ai/v1/responses with muse-build UA and
// X-Client-Id tbh:tui. Reasoning encrypted_content is shape-validated
// (CPA signature/meta_validation) and passed through when valid.
//
// The adapter fits cli2api's kimi-shaped login (StartLogin/PollLogin, no
// loopback) and codex-shaped Responses (StreamFormatResponses + NativeResponses).
package meta
