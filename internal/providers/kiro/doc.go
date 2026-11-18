// Package kiro is the in-process adapter for a personal Kiro login.
//
// Upstream framing and tool-call assembly follow kiro-reverse-api (MIT, see
// NOTICE): AWS event-stream for generateAssistantResponse, and the
// CreateResponse SSE mapping that turns function_call items into the same
// toolUseEvent fragments. Account selection, cooldown, and failover stay in
// the executor. This package does not embed that project's HTTP server,
// SQLite, admin UI, or account pool.
package kiro
