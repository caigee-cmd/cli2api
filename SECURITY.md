# Security

Intended for personal / self-hosted use of **your own** upstream accounts.

## Do not

- Expose `:3010` or worker / updater ports directly to the public internet
- Give client applications the administrator key when a scoped client key is enough
- Share one login across many users commercially
- Share tokens, cookies, credential exports, SQLite databases, backups, or raw captures

## Report privately

Use a private contact listed on the maintainer's GitHub profile. Do not put
credentials or exploitable security details in a public issue.

Console `/api/*` and Qoder worker `/admin/*` require the administrator key.
Client keys only access `/v1/*`; the administrator key also works there.
`/health`, static frontend resources, and `/v1/*` CORS preflight `OPTIONS` stay open.
