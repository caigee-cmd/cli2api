# docs/assets/

Static assets embedded by `README.md` and `README_EN.md` via relative links.

## Files

| File | Source | Update rule |
|------|--------|-------------|
| `overview-card.png` | Legacy overview card, not currently linked | Retained for historical reference; do not regenerate |
| `readme/hero-{zh,en}.svg` | Hand-maintained SVG heroes (console dark-theme palette, Prompt C mark) | Re-sync copy with the tagline and flow text in the matching README |
| `readme/console-window-{zh,en}.svg` | Hand-maintained console mockup (Accounts + Access) | Regenerate when console pages, account card fields, or nav items change |
| `readme/architecture-{zh,en}.svg` | Hand-maintained architecture diagram | Regenerate when the request path or worker model changes |

## Why this folder is hand-maintained

- Both READMEs live at the repo root and use **relative** paths so they render correctly on github.com, npmjs.com, and `go install` package pages.
- `frontend/public/og-card.svg` is the **runtime** social-preview asset served by the Go binary; it is not linked from the READMEs.
- SVG assets stay GitHub-safe: no scripts, remote fonts, or filters; each carries `<title>` and `<desc>` for accessibility.
- Keep heroes provider-neutral; Qoder is not the only account type. Architecture diagrams distinguish Qoder child workers from Go in-process adapters.
- Console images are illustrative mockups, not proof of live-account acceptance.

## Console assets

The favicon suite and social card live in `frontend/public/`. Run
`cd frontend && npm run sync` after changing them; Go embeds the synchronized
files in `internal/webui/static`. README-only SVG edits do not need that sync.

When adding a console asset, update the `frontend/scripts/sync-static.mjs`
whitelist, route registration and fallback allowlist in `internal/server/router.go`,
and relevant links in `frontend/index.html`; generate the embedded copy with sync.
Keep favicon files under 1 KB, without gradients, filters, masks, or embedded text.
Theme variants already exist; preserve them rather than adding a second asset system.
