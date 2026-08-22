# Console

React + Tailwind CSS v4 + HeroUI management UI, embedded into the Go proxy.

```bash
cd frontend
npm install
npm run sync   # build + copy dist -> ../internal/webui/static
```

Pages: Overview, Auth, Providers, Accounts, API Access.

Docker already builds this before compiling the Go binary. Do not commit `node_modules/` or `dist/`.
