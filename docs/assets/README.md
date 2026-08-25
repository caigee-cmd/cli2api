# docs/assets/

Static assets embedded by `README.md` and `README_ZH.md` via relative links.

## Files

| File | Source | Update rule |
|------|--------|-------------|
| `console.png` | Manual screenshot capture of the web console | Re-capture on every UI redesign |
| `og-card.svg` | Hand-synced from `frontend/public/og-card.svg` | Re-copy when the favicon suite changes |

## Why this folder is hand-maintained

- `README.md` lives at the repo root and uses **relative** paths so it renders correctly on github.com, npmjs.com, and `go install` package pages.
- `frontend/public/og-card.svg` is the **runtime** asset served by the Go binary; the file in this folder is the **documentation** copy.
- Keep them byte-identical. The `favicon-suite` task in this repo's hand-off log includes copying `frontend/public/og-card.svg` here.
