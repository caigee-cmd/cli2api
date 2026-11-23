# Prompt cache affinity

- Keep aggregate logical-model sessions bound to a member account instead of
  resetting to the general pool on every turn.
- Bind a streaming request as soon as the upstream accepts the response
  headers/first event, not after the body ends; a follow-up turn can then reuse
  the same account while the first stream is still running.
- Always bind, cool down, and fail over by pool account ID. Worker response
  labels are never used as routing keys.
- Invalidate a session binding on failover, including sticky escapes.
- Canonicalize provider cache token fields so reported cache usage is stable
  across provider protocols.
