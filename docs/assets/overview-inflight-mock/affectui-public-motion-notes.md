# Agent Queue — publicly readable Motion / structure notes

Extracted 2026-10-29 from the **public docs page** and its marketing/demo chunks.
**Do not copy Pro component source into cli2api.** Apply equivalent GSAP/CSS with project tokens.

## Locked (Pro)

| Asset | Status |
|-------|--------|
| `https://www.affectui.com/r/agent-queue.json` | HTTP 401 — Bearer access key required |
| `assets/component-DkFrPebu.js` / `component.clean-*.js` | Serves `var e="";export{e as default}` (empty stub) |
| Registry install via shadcn | Pro gate |

## Publicly readable

| Asset | What it gives |
|-------|----------------|
| Page HTML + `assets/index-*.js/css` | Catalog entry, lazy chunk names, marketing copy |
| `assets/docs-OUFUpYq4.js` | Full public docs object: props, a11y, responsive, **Motion prose** |
| `assets/demo-CRKNaMKk.js` (and `.clean`) | Live preview: **CSS keyframes, `--ui-agq-ease`, timing constants, class structure** |
| Reference PNGs (light / pinned / rate-limited) | Layout feel |
| handoff-spec.md | Stage plan + earlier Motion paraphrase |

### Docs Motion prose (public)

- New jobs drop in at the back; high-priority lands then glides past
- Front job → free worker → growing bar with **sheen + glowing front edge**
- Done: green beat, flies into Done; Failed: **shakes red** (or retry slides back)
- Rate limit: bars **stop + amber**, countdown; depth climbs then drains
- Numbers ease; chart scale glides; sleeps off-screen / idle
- `prefers-reduced-motion`: appear in place
- Wide: Queue | Workers | Done/Failed; `<600px` stacks

### Demo timing constants (public)

```
s = 560   // layout / bar grow glide (ms)
c = 1100  // front-edge glow period
l = 1500  // sheen / sweep period
u = 520   // drop-in
d = 200   // short fades
f = 520   // flash/drop companion
```

### Public easing

```
--ui-agq-ease: cubic-bezier(0.22, 1, 0.36, 1);
```

(Note: earlier handoff paraphrase cited `cubic-bezier(0.32, 0.72, 0.24, 1)` — **demo CSS uses 0.22, 1, 0.36, 1**. Prefer the public demo value.)

### Public keyframes (names + from-state only — reimplement, don’t paste Pro file)

| Name | From | Duration / use |
|------|------|----------------|
| `ui-agq-drop` | opacity 0; translateY(-10px) scale(0.6) | **520ms** ease, backwards |
| `ui-agq-slot` | opacity 0; translateX(-8px) | **420ms** ease |
| `ui-agq-grow` | clip-path: inset(0 100% 0 0 round 8px) | **560ms** ease, backwards |
| `ui-agq-sweep` | background-position → | **1500ms** linear infinite (sheen) |
| `ui-agq-glow` | filter opacity ~0.45 | **1100ms** ease-in-out **alternate** |
| `ui-agq-shake` | translateX(-3px) @20% | **420ms** ease (with drop on fail flash) |
| `ui-agq-calm` | opacity 0 | 300ms |
| `ui-agq-pulse` | opacity mid 0.35 | 1400ms ease-in-out infinite |
| `ui-agq-pop` | scale 0.9 | — |
| `ui-agq-open` | translateY(-4px) scale(0.97) | 200ms |
| `ui-agq-hit` | — | 700ms ease-out + 380ms delay |
| `ui-agq-ping` | — | 1600ms ease-out infinite |

### Progress bar (public CSS idea)

```
clip-path: inset(0 calc((1 - var(--p)) * 100%) 0 0 round 8px);
transition: clip-path 260ms linear, …;
```

Sheen via `::before` sweep; glow via `::after` at the front edge.

### Class / token structure (names only)

Root `.ui-agq` with CSS variables `--ui-agq-*` (kind tints llm/tool/agent/human, warn/ok/error, depth, ease).
Jobs `.ui-agq__job` with `data-shape` (bar / square / …), `data-flash`, workers rail, bins Done/Failed, chart bands.

### cli2api mapping (read-only Stage 1)

- Reimplement timings/easing above with **GSAP + small CSS keyframes**
- Colors: **cold project tokens** only (`foreground` / `surface` / `warning` / `success` / `danger`; chart hues only for kind chips if needed)
- No Pause / Cancel / concurrency stepper / promote
- No Pro source file, no `.ui-agq` wholesale CSS copy
