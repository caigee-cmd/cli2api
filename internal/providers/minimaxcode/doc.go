// Package minimaxcode implements an in-process adapter for the MiniMax Code
// subscription product (agent.minimax.cn / agent.minimax.io). It is not the
// MiniMax Open Platform API-key gateway.
//
// Authentication uses MiniMax Code's own OAuth 2.0 device flow with PKCE S256
// (public client mcode-public). Refresh tokens are single-use, so refreshes are
// serialized per account and persisted immediately. Chat speaks the Anthropic
// Messages protocol the Code CLI uses and is translated to/from the gateway's
// OpenAI-compatible surface. Model discovery reads the live catalog with a
// static fallback; quota is status-only because the gateway exposes no numeric
// balance endpoint over this surface.
package minimaxcode
