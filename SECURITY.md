# Security

Intended for personal / self-hosted use of **your own** Qoder login.

## Do not

- Expose `:3010` without `PROXY_API_KEY`
- Share one login across many users commercially
- Open issues with auth blobs, cookies, COSY tokens, or `~/.qoder` dumps

## Report privately

Email the maintainer listed on GitHub. Do not file a public issue for leaked credentials.

Worker and console `/api/*` require the same key when `PROXY_API_KEY` is set. `/health` stays open for probes.
