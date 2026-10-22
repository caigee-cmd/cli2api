/*
 * CountUp: odometer-style number roll for the Overview metric cards.
 *
 * Adapted from affectui "Number Ticker" (motion/number-ticker.tsx),
 * https://github.com/flohoeller/affectui
 *
 * MIT License
 * Copyright (c) 2026 Florian Höller (adfects)
 *
 * Permission is hereby granted, free of charge, to any person obtaining a copy
 * of this software and associated documentation files (the "Software"), to deal
 * in the Software without restriction, including without limitation the rights
 * to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
 * copies of the Software, and to permit persons to whom the Software is
 * furnished to do so, subject to the following conditions:
 *
 * The above copyright notice and this permission notice shall be included in all
 * copies or substantial portions of the Software.
 *
 * THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
 * IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
 * FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
 * AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
 * LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
 * OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
 * SOFTWARE.
 *
 * Local changes: takes the console's own formatter (formatCountKind) instead of
 * Intl.NumberFormat so "4.2k" / "93%" / "6.4s" keep their existing shape;
 * starts immediately (no IntersectionObserver / delay); new columns slide in
 * without an opacity fade; exported as CountUp with the previous props.
 */
import { useEffect, useMemo, useRef } from "react";
import { formatCountKind } from "@/lib/format";

/*
 * Number Ticker – animated numbers like an odometer, in a single self-contained file.
 * Every digit is a small vertical column that rolls to its new value: up when the number grows, down when it
 * shrinks. On first view it counts in from zero. Formatting comes from Intl.NumberFormat, so currency signs,
 * separators and percent signs stay put while only the digits move. The styles are injected below.
 */

export type CountUpProps = {
  /** The number to show */
  value: number;
  /** Console formatter kind */
  kind?: CountKind;
  /** Length of a roll in ms */
  duration?: number;
  className?: string;
};

type CountKind = "int" | "compact" | "percent" | "ms";

type Part = { key: string; digit: number } | { key: string; char: string };

/** "$48,240" → parts keyed from the right, so the ones column keeps its identity when the number grows */
function split(text: string): Part[] {
  const chars = Array.from(text);
  const parts: Part[] = [];
  let d = 0;
  let s = 0;
  for (let i = chars.length - 1; i >= 0; i--) {
    const c = chars[i];
    if (c >= "0" && c <= "9") parts.unshift({ key: `d${d++}`, digit: c.charCodeAt(0) - 48 });
    else parts.unshift({ key: `s${s++}`, char: c });
  }
  return parts;
}

const mod10 = (n: number) => ((n % 10) + 10) % 10;
const ease = (t: number) => 1 - Math.pow(1 - t, 4);
/** Position → transform; the stack holds 0–9 plus a trailing 0, so 9 → 0 rolls seamlessly */
const place = (el: HTMLElement, pos: number) => {
  el.style.transform = `translateY(${(-mod10(pos) / 11) * 100}%)`;
};

type Roll = { pos: number; from: number; to: number };

export function CountUp({ value, kind = "int", duration = 900, className }: CountUpProps) {
  const text = useMemo(() => formatCountKind(value, kind), [value, kind]);
  const parts = useMemo(() => split(text), [text]);

  const root = useRef<HTMLSpanElement>(null);
  const rolls = useRef(new Map<string, Roll>());
  const stacks = useRef(new Map<string, HTMLElement>());
  const prev = useRef(0);
  const up = useRef(true);
  const mounted = useRef(false);
  const entering = useRef(new Set<string>());
  const seen = useRef(new Set<string>());

  // Columns that show up after the first render slide in (e.g. 9,999 → 10,000)
  const keysNow = new Set(parts.filter((p) => "digit" in p).map((p) => p.key));
  for (const k of keysNow) if (mounted.current && !seen.current.has(k)) entering.current.add(k);
  for (const k of entering.current) if (!keysNow.has(k)) entering.current.delete(k);
  seen.current = keysNow;

  useEffect(() => {
    mounted.current = true;
  }, []);

  // Roll every digit column to its new value
  useEffect(() => {
    // Same value again (e.g. a format change) keeps the last direction
    if (value !== prev.current) up.current = value > prev.current;
    prev.current = value;
    const reduce = window.matchMedia("(prefers-reduced-motion: reduce)").matches;
    const keys = new Set<string>();

    for (const p of parts) {
      if (!("digit" in p)) continue;
      keys.add(p.key);
      const r = rolls.current.get(p.key) ?? { pos: 0, from: 0, to: 0 };
      const delta = up.current ? mod10(p.digit - r.pos) : -mod10(r.pos - p.digit);
      r.from = r.pos;
      r.to = r.pos + delta;
      if (reduce) r.pos = r.from = r.to;
      rolls.current.set(p.key, r);
    }
    for (const k of rolls.current.keys()) if (!keys.has(k)) rolls.current.delete(k);

    const apply = () => {
      for (const [k, r] of rolls.current) {
        const el = stacks.current.get(k);
        if (el) place(el, r.pos);
      }
    };
    if (reduce) return apply();

    let raf = 0;
    const t0 = performance.now();
    const tick = (now: number) => {
      const t = Math.min(1, (now - t0) / duration);
      const e = ease(t);
      for (const r of rolls.current.values()) r.pos = r.from + (r.to - r.from) * e;
      apply();
      if (t < 1) raf = requestAnimationFrame(tick);
    };
    raf = requestAnimationFrame(tick);
    return () => cancelAnimationFrame(raf);
  }, [parts, value, duration]);

  return (
    <>
      <style href="ui-number-ticker" precedence="default">
        {css}
      </style>
      <span ref={root} className={["ui-nt", className].filter(Boolean).join(" ")} aria-live="polite">
        <span className="ui-nt__sr">{text}</span>
        <span className="ui-nt__track" aria-hidden="true">
          {parts.map((p) =>
            "digit" in p ? (
              <span key={p.key} className="ui-nt__col" data-enter={entering.current.has(p.key) || undefined}>
                <span
                  className="ui-nt__stack"
                  ref={(el) => {
                    if (!el) return;
                    stacks.current.set(p.key, el);
                    place(el, rolls.current.get(p.key)?.pos ?? 0);
                    return () => {
                      if (stacks.current.get(p.key) === el) stacks.current.delete(p.key);
                    };
                  }}
                >
                  {[0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 0].map((n, i) => (
                    <span key={i} className="ui-nt__cell">
                      {n}
                    </span>
                  ))}
                </span>
              </span>
            ) : (
              <span key={p.key} className="ui-nt__static">
                {p.char}
              </span>
            ),
          )}
        </span>
      </span>
    </>
  );
}

/* ---------- Styles ---------- */

const css = /* css */ `
.ui-nt {
  --ui-nt-lh: 1.15em;
  --ui-nt-ease: cubic-bezier(0.22, 1, 0.36, 1);
  position: relative;
  display: inline-flex;
  line-height: var(--ui-nt-lh);
  font-variant-numeric: tabular-nums;
  white-space: nowrap;
}
.ui-nt__sr {
  position: absolute;
  width: 1px;
  height: 1px;
  margin: -1px;
  padding: 0;
  overflow: hidden;
  clip: rect(0 0 0 0);
  white-space: nowrap;
  border: 0;
}
.ui-nt__track {
  display: inline-flex;
  align-items: flex-start;
  height: var(--ui-nt-lh);
  line-height: var(--ui-nt-lh);
}
.ui-nt__col {
  display: inline-block;
  height: var(--ui-nt-lh);
  overflow: hidden;
  /* Soft top and bottom edges while digits pass through */
  -webkit-mask-image: linear-gradient(transparent, #000 14%, #000 86%, transparent);
  mask-image: linear-gradient(transparent, #000 14%, #000 86%, transparent);
}
.ui-nt__col[data-enter] { animation: ui-nt-in 450ms var(--ui-nt-ease) both; }
.ui-nt__stack {
  display: flex;
  flex-direction: column;
  will-change: transform;
}
.ui-nt__cell,
.ui-nt__static {
  display: block;
  height: var(--ui-nt-lh);
  text-align: center;
}
.ui-nt__static { white-space: pre; }

@keyframes ui-nt-in {
  from { transform: translateY(-35%); }
}
@media (prefers-reduced-motion: reduce) {
  .ui-nt__col[data-enter] { animation: none; }
}

/* Dark mode: follows a .dark class or data-theme="dark" on any ancestor, e.g. <html>.
   The ticker inherits its color, so nothing needs to change here. */
:where(.dark, [data-theme="dark"]) .ui-nt {
  --ui-nt-lh: 1.15em;
}
`;
