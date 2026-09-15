// Package devin implements the Devin / Codeium Connect-RPC in-process provider.
//
// Protocol facts (endpoints, Connect frame layout, Basic token-token auth,
// client metadata, GetChatMessage / GetUserStatus wire shapes) are derived from
// router-for-me/CLIProxyAPI @ 8335eac under the MIT License. See NOTICE in this
// directory. This package rewrites those facts into the local Provider Adapter
// surface and does not import CLIProxyAPI packages.
package devin
