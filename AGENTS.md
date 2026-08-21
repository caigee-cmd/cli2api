# AGENTS

Go service that turns Qoder Global auth into an OpenAI-compatible API.

## Do

- Keep architecture: auth / endpoint / executor / translate / api
- Prefer direct HTTP/SSE to Qoder cloud APIs
- Keep capture notes redacted

## Don't

- Spawn `qodercli`
- Expose host ports publicly
- Commit raw auth blobs / tokens
