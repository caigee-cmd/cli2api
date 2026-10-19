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
package qwenwork
