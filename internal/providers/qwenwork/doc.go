// Package qwenwork implements the QwenWork in-process provider.
//
// Wire-protocol facts (device-authorization endpoints, COSY header set,
// wrapped SSE frame shape, error-frame precedence) were cross-checked against
// nostalgia296/qwenwork2api-makers
// (https://github.com/nostalgia296/qwenwork2api-makers), a JavaScript project
// that declares no license. No code is imported or copied; this package is an
// independent Go implementation on cli2api's Provider Adapter surface, reusing
// the COSY signing approach of internal/providers/qoder. The camelCase import
// shape accepts that project's account export.
//
// Daily free credits: the desktop bundles (global 1.0.4, CN 1.4.0) call
// GET /sash/api/v1/me/daily-check-in/status, POST .../claim, and
// GET /sash/api/v1/me/{invitationRewards,invitationCode} for the growth card,
// gated by the remote `operations/announcement:credits-growth-card` config.
// Both production gateways answer 404 for every /sash path, and the config
// client logs "no remote value" for both accounts, so the campaign is not
// claimable today; the daily credits are granted server-side instead. CN
// exposes the grant through /api/v1/adapter/user/wallets (daily_credits,
// expiring at 00:00 Asia/Shanghai); the global gateway exposes only the
// account-context quota total.
package qwenwork
