# Console

React + Tailwind CSS v4 + HeroUI management UI, embedded into the Go proxy.

Must use HeroUI for components. Taste and IA: [`../docs/DESIGN.md`](../docs/DESIGN.md).  
Current work: [`../docs/PLAN.md`](../docs/PLAN.md).

```bash
cd frontend
npm install
npm run sync   # build + copy dist -> ../internal/webui/static
```

Pages: Login, Overview, Accounts, Models, Access. `/auth` redirects to Accounts.

Docker already builds this before compiling the Go binary. Do not commit `node_modules/` or `dist/`.
