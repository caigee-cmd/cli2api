# Account routing failover

- Release the implicit region latch on cross-provider routes so a bare model
  shared by multiple providers can fail over to another supplier.
- Keep the Qoder global/CN region isolation for provider-scoped routes.
- Preserve the retry budget after the first pick instead of shrinking it to the
  pinned region's account count.
- Give each priority tier its own retry budget so sibling accounts cannot
  consume the whole budget before a lower-priority provider is reached.
- Apply account model aliases in the legacy worker request path.
