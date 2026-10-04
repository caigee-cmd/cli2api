// Package zhipu implements the Zhipu GLM (open.bigmodel.cn) in-process provider.
//
// Zhipu is an API-key upstream, not a browser login. A pasted key is stored as
// the account credential and reused for chat, the model list, and Coding Plan
// quota. There is no child process and no per-request CLI.
//
// Chat speaks the official OpenAI-compatible Chat Completions endpoint. Pay-as-
// you-go and Coding Plan only differ by base URL; the request shape is the same.
// Zhipu has no Responses endpoint, so /v1/responses stays on the shared chat
// translation path. The Anthropic-compatible endpoint is not used: this gateway
// already converts Anthropic Messages into the internal chat form before the
// adapter sees them.
//
// GLM reasoning is not the OpenAI scale. Older glm-* models accept only
// high/max, so low and medium collapse to high and xhigh/max stay max. glm-5.3
// accepts low as well. The adapter rewrites the outbound reasoning_effort and
// does not invent levels a model does not declare.
package zhipu
