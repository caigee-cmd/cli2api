# AGENTS

Go service that turns Qoder Global auth into an OpenAI-compatible API.

## Do

- Keep architecture: auth / endpoint / executor / translate / api
- Prefer direct HTTP/SSE to Qoder cloud APIs
- Keep capture notes redacted
- Pin qodercli hooks in `worker/src/compat.mjs`; fail loudly on mismatch

## Don't

- Spawn a full `qodercli` agent per request
- Expose host ports publicly
- Commit raw auth blobs / tokens
- Leave console `/api/*` or worker `/admin/*` unauthenticated when `PROXY_API_KEY` is set
