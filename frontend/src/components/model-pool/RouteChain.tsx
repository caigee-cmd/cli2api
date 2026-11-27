import { useEffect, useId, useLayoutEffect, useRef, useState, type ReactNode, type SyntheticEvent } from "react";



export type AttemptStatus = "waiting" | "trying" | "failed" | "answered" | "skipped";

/** One line in a hover card, e.g. { label: "Tokens", value: "1,284 in · 96 out" } */
export type ChainDetail = { label: string; value: ReactNode };

export type FallbackAttempt = {
  id: string;
  /** The model, e.g. "Claude Opus 5.5" */
  model: string;
  /** Who serves it, e.g. "Anthropic" or "Azure" */
  provider?: string;
  /** A small mark – defaults to the initials */
  logo?: ReactNode;
  /** Defaults to waiting */
  status?: AttemptStatus;
  /** When this attempt started – drives the live timer while it's trying */
  startedAt?: number | Date;
  /** How long it took, in ms, once it failed or answered */
  duration?: number;
  /** How long it may take before it counts as failed, in ms (default 10s) – its blocks fill toward it */
  timeout?: number;
  /** What it cost so far, in US dollars – update it while it runs and the counters follow */
  cost?: number;
  /** Why it failed, e.g. "Rate limited · 429" */
  reason?: string;
  /** A short line once it answered, e.g. "412 tokens" */
  note?: string;
  /** Who serves it, as a logo – leads its hover card (e.g. AWS for Claude on Bedrock) */
  providerLogo?: ReactNode;
  /** Where it's served, e.g. "us-east-1" */
  region?: string;
  /** More lines for its hover card – tokens, price, the error code */
  details?: ChainDetail[];
};

/** Where the request came from or where the answer goes – shown in the hover cards on the two ends */
export type ChainEnd = {
  /** e.g. "HubSpot · Ticket #4821" or "Slack · #support" */
  name?: string;
  logo?: ReactNode;
  /** The prompt, or the answer – a short preview in the card */
  text?: string;
  details?: ChainDetail[];
};

export type RouteChainProps = {
  attempts: FallbackAttempt[];
  /** What was asked, above the diagram */
  title?: string;
  /** The route on the request node, e.g. "openrouter/auto" */
  route?: string;
  /** Where the request came from – its hover card */
  request?: ChainEnd;
  /** Where the answer goes – its hover card */
  response?: ChainEnd;
  /** A clock you control – without it the timer follows real time */
  now?: number;
  /** Shown as a button once every model failed */
  onRetry?: () => void;
  className?: string;
};

/* Fixed geometry, so the branches can be drawn without measuring: card height, gap between cards, branch length */
const ROW = 60;
const GAP = 10;
const LINK = 48;

/** A time as ms – NaN when it's missing or not a valid date */
const ms = (d: number | Date | undefined) => (typeof d === "number" ? d : d instanceof Date ? d.getTime() : NaN);
/** 40ms · 4.2s · 1m 05s · 1h 02m – NaN and negative times read 0ms */
const secs = (n: number) => {
  const v = Number.isFinite(n) ? Math.max(0, n) : 0;
  if (v < 1000) return `${Math.round(v / 10) * 10}ms`;
  if (v < 60_000) return `${(v / 1000).toFixed(1)}s`;
  const s = Math.round(v / 1000);
  if (s < 3600) return `${Math.floor(s / 60)}m ${String(s % 60).padStart(2, "0")}s`;
  const m = Math.round(s / 60);
  return `${Math.floor(m / 60).toLocaleString("en-US")}h ${String(m % 60).padStart(2, "0")}m`;
};
const usd = (n: number) => {
  const v = Math.max(0, Number.isFinite(n) ? n : 0);
  return v === 0 ? "$0" : v < 0.0001 ? "<$0.0001" : v < 0.01 ? `$${v.toFixed(4)}` : v < 1 ? `$${v.toFixed(3)}` : `$${v.toLocaleString("en-US", { minimumFractionDigits: 2, maximumFractionDigits: 2 })}`;
};
/** Whole characters (graphemes), so an emoji or an accent never splits */
const chars = (s: string) => (typeof Intl !== "undefined" && "Segmenter" in Intl ? Array.from(new Intl.Segmenter().segment(s), (g) => g.segment) : Array.from(s));
/** The first letters of the first two words – words that start with a letter first, so "🦊 Fox" reads F */
const initials = (name: string) => {
  const words = String(name ?? "").trim().split(/\s+/).filter(Boolean);
  const letters = words.filter((w) => /^\p{L}/u.test(w));
  return (letters.length ? letters : words).slice(0, 2).map((w) => chars(w)[0]).join("").toUpperCase();
};
const STATUSES = ["waiting", "trying", "failed", "answered", "skipped"];
/** How long it may take – 10s when it's missing, 0 or invalid */
const limitOf = (a: FallbackAttempt) => (typeof a.timeout === "number" && Number.isFinite(a.timeout) && a.timeout > 0 ? a.timeout : 10_000);
/** A logo that fails to load hides its tile instead of showing a broken image */
const hide = (e: SyntheticEvent<HTMLElement>) => (e.currentTarget.style.display = "none");

/** A model's mark: its logo, or its initials when there's none or it fails to load */
function Mark({ logo, model }: { logo?: ReactNode; model: string }) {
  const [broken, setBroken] = useState(false);
  const show = logo && !broken;
  return (
    <span className="rp-chain__mark" data-logo={show ? "" : undefined} onErrorCapture={() => setBroken(true)}>
      {show ? logo : initials(model)}
    </span>
  );
}

/** A model's time against its timeout: square blocks of this size and gap – as many as fit the card's row */
const BLOCK = 5;
const BLOCK_GAP = 2;

/** A soft S-curve from one side to the other */
const curve = (y1: number, y2: number, w: number) => `M0 ${y1}C${w * 0.55} ${y1} ${w * 0.45} ${y2} ${w} ${y2}`;
/** A tree branch: down the spine, then round into the card */
const branch = (y: number, x: number) => `M${x} 0V${Math.max(0, y - 10)}Q${x} ${y} ${x + 10} ${y}H${x + 22}`;

/** The route: three stops, the last one reached */
const RouteIcon = () => (
  <svg width="16" height="16" viewBox="0 0 16 16" fill="none" aria-hidden="true">
    <circle cx="3.5" cy="3.5" r="1.75" fill="currentColor" opacity="0.45" />
    <circle cx="12.5" cy="8" r="1.75" fill="currentColor" opacity="0.45" />
    <circle cx="3.5" cy="12.5" r="2.25" fill="currentColor" />
    <path d="M5.2 3.5h4.3a3 3 0 0 1 0 6H6.3a1.5 1.5 0 0 0 0 3" stroke="currentColor" strokeWidth="1.3" strokeLinecap="round" opacity="0.7" />
  </svg>
);
/** The answer: a speech bubble – outlined while waiting, filled once there is one */
const ReplyIcon = ({ filled }: { filled?: boolean }) => (
  <svg width="16" height="16" viewBox="0 0 16 16" fill="none" aria-hidden="true">
    <path d="M3 4.5A1.5 1.5 0 0 1 4.5 3h7A1.5 1.5 0 0 1 13 4.5v5a1.5 1.5 0 0 1-1.5 1.5H8l-3 2.5V11h-.5A1.5 1.5 0 0 1 3 9.5v-5Z" fill={filled ? "currentColor" : "none"} stroke="currentColor" strokeWidth="1.3" strokeLinejoin="round" />
    <path d="M5.75 6.25h4.5M5.75 8.25h2.75" stroke={filled ? "var(--rp-chain-card)" : "currentColor"} strokeWidth="1.3" strokeLinecap="round" />
  </svg>
);

const STATUS_LABEL: Record<AttemptStatus, string> = { waiting: "Waiting", trying: "Trying", failed: "Failed", answered: "Answered", skipped: "Not needed" };

/** A small card that opens on hover or focus: a head with a logo, an optional quote, then label – value lines */
function Pop({ id, logo, title, sub, badge, tone, quote, rows, place }: { id: string; logo?: ReactNode; title: ReactNode; sub?: ReactNode; badge?: string; tone?: string; quote?: string; rows: ChainDetail[]; place: string }) {
  return (
    <span className="rp-chain__pop" id={id} role="tooltip" data-place={place}>
      <span className="rp-chain__pop-head">
        {logo && (
          <span className="rp-chain__pop-logo" onErrorCapture={hide}>
            {logo}
          </span>
        )}
        <span className="rp-chain__pop-who">
          <span className="rp-chain__pop-title">{title}</span>
          {sub && <span className="rp-chain__pop-sub">{sub}</span>}
        </span>
        {badge && (
          <span className="rp-chain__pop-badge" data-tone={tone}>
            {badge}
          </span>
        )}
      </span>
      {quote && (
        <span className="rp-chain__pop-quote">
          <span>{quote}</span>
        </span>
      )}
      {rows.length > 0 && (
        <span className="rp-chain__pop-rows">
          {rows.map((r, i) => (
            <span key={`${r.label}-${i}`} className="rp-chain__pop-row">
              <span className="rp-chain__pop-label">{r.label}</span>
              <span className="rp-chain__pop-value">{r.value}</span>
            </span>
          ))}
        </span>
      )}
    </span>
  );
}

/**
 * The light on a branch: a gradient that fades in, peaks and fades out (0 → 100% → 0) and sweeps along the branch,
 * so the streak has soft ends instead of a hard dash
 */
function Light({ id, d, size, vertical }: { id: string; d: string; size: number; vertical?: boolean }) {
  const band = Math.min(size * 0.7, 120);
  const axis = vertical ? { x1: 0, y1: 0, x2: 0, y2: band } : { x1: 0, y1: 0, x2: band, y2: 0 };
  const move = vertical ? [`0 ${-band}`, `0 ${size}`] : [`${-band} 0`, `${size} 0`];
  return (
    <>
      <defs>
        <linearGradient id={id} gradientUnits="userSpaceOnUse" {...axis}>
          <stop offset="0" style={{ stopColor: "var(--rp-chain-accent)", stopOpacity: 0 }} />
          <stop offset="0.5" style={{ stopColor: "var(--rp-chain-accent)", stopOpacity: 1 }} />
          <stop offset="1" style={{ stopColor: "var(--rp-chain-accent)", stopOpacity: 0 }} />
          <animateTransform attributeName="gradientTransform" type="translate" from={move[0]} to={move[1]} dur="1.4s" repeatCount="indefinite" calcMode="spline" keyTimes="0;1" keySplines="0.45 0 0.55 1" />
        </linearGradient>
      </defs>
      <path className="rp-chain__light" d={d} stroke={`url(#${id})`} />
    </>
  );
}

export function RouteChain({ attempts, title = "Request", route, request, response, now: nowProp, onRetry, className }: RouteChainProps) {
  const uid = "fc" + useId().replace(/[^a-zA-Z0-9_-]/g, "");
  // Gaps in the list are left out; an unknown status reads as waiting
  attempts = (Array.isArray(attempts) ? attempts : []).filter(Boolean);
  const statusOf = (a: FallbackAttempt): AttemptStatus => (STATUSES.includes(a.status as string) ? a.status! : "waiting");
  // The clock only runs while something is being tried since a known time
  const live = attempts.some((a) => statusOf(a) === "trying" && Number.isFinite(ms(a.startedAt)));

  // A live clock while something is being tried – only when you don't pass one
  // Count how many blocks fit a card's row, and keep counting as it resizes
  const models = useRef<HTMLOListElement>(null);
  const [blocks, setBlocks] = useState(30);
  useLayoutEffect(() => {
    const row = models.current?.querySelector<HTMLElement>(".rp-chain__blocks");
    if (!row) return;
    const fit = () => setBlocks(Math.max(8, Math.floor((row.clientWidth + BLOCK_GAP) / (BLOCK + BLOCK_GAP))));
    fit();
    if (typeof ResizeObserver === "undefined") return;
    const ro = new ResizeObserver(fit);
    ro.observe(row);
    return () => ro.disconnect();
  }, [attempts.length]);

  const [tick, setTick] = useState(() => Date.now());
  useEffect(() => {
    if (nowProp !== undefined || !live) return;
    const id = window.setInterval(() => setTick(Date.now()), 100);
    return () => window.clearInterval(id);
  }, [nowProp, live]);
  const now = nowProp ?? tick;

  const ran = (a: FallbackAttempt) => ["trying", "failed", "answered"].includes(statusOf(a));
  const took = (a: FallbackAttempt) => {
    const s = statusOf(a);
    const t = s === "trying" ? now - ms(a.startedAt) : s === "failed" || s === "answered" ? Number(a.duration) : 0;
    return Number.isFinite(t) ? Math.max(0, t) : 0;
  };

  const tryingAt = attempts.findIndex((a) => statusOf(a) === "trying");
  const answeredAt = attempts.findIndex((a) => statusOf(a) === "answered");
  const tried = attempts.filter(ran);
  const allFailed = attempts.length > 0 && attempts.every((a) => ["failed", "skipped"].includes(statusOf(a))) && attempts.some((a) => statusOf(a) === "failed");
  const time = tried.reduce((n, a) => n + took(a), 0);
  const costOf = (a: FallbackAttempt) => (typeof a.cost === "number" && Number.isFinite(a.cost) ? Math.max(0, a.cost) : 0);
  const spent = tried.reduce((n, a) => n + costOf(a), 0);
  const winner = answeredAt >= 0 ? attempts[answeredAt] : undefined;
  const state = winner ? "answered" : allFailed ? "failed" : tryingAt >= 0 ? "trying" : "waiting";

  const n = Math.max(1, attempts.length);
  const height = n * ROW + (n - 1) * GAP;
  const mid = height / 2;
  const rowY = (i: number) => i * (ROW + GAP) + ROW / 2;

  const lineOf = (a: FallbackAttempt, i: number) => {
    const s = statusOf(a);
    if (s === "trying") return "Trying…";
    if (s === "failed") return a.reason || "Failed";
    if (s === "answered") return a.note ? `Answered · ${a.note}` : "Answered";
    if (s === "skipped") return "Not needed";
    return i === tried.length && state === "trying" ? "Up next" : "Waiting";
  };

  // In: a branch from the request to every model – the one being tried carries the light
  const inLinks = (shape: (y: number) => string, size: number, vertical?: boolean) =>
    attempts.map((a, i) => {
      const s = statusOf(a);
      const d = shape(rowY(i));
      return (
        <g key={i}>
          <path className="rp-chain__link" data-status={s} d={d} key={s} pathLength={1} />
          {s === "trying" && <Light id={`${uid}-${vertical ? "t" : "w"}${i}`} d={d} size={size} vertical={vertical} />}
        </g>
      );
    });

  return (
    <div className={["rp-chain", className].filter(Boolean).join(" ")}>
      <style href="route-preview" precedence="default">
        {css}
      </style>

      {/* Above the diagram: what was asked, the attempt and the running totals */}
      <header className="rp-chain__bar">
        <span className="rp-chain__title" title={title}>
          {title}
        </span>
        <span className="rp-chain__summary" data-state={state} role="status">
          <span className="rp-chain__state" key={state}>
            {state === "answered" ? (
              "Answered"
            ) : state === "failed" ? (
              "No model answered"
            ) : state === "trying" ? (
              <>
                Attempt <b key={tryingAt}>{tryingAt + 1}</b> of {attempts.length}
              </>
            ) : (
              "Waiting"
            )}
          </span>
          <span className="rp-chain__total" aria-label="Time so far">
            <svg width="12" height="12" viewBox="0 0 12 12" fill="none" aria-hidden="true">
              <circle cx="6" cy="6" r="4.6" stroke="currentColor" strokeWidth="1.2" />
              <path d="M6 3.6V6l1.6 1" stroke="currentColor" strokeWidth="1.2" strokeLinecap="round" />
            </svg>
            {secs(time)}
          </span>
          <span className="rp-chain__total" aria-label="Cost so far">
            {usd(spent)}
          </span>
        </span>
        {state === "failed" && onRetry && (
          <button type="button" className="rp-chain__btn" onClick={onRetry}>
            Retry
          </button>
        )}
      </header>

      <div className="rp-chain__card">
        <div className="rp-chain__flow">
          {/* Where it starts */}
          <div className="rp-chain__node rp-chain__node--from rp-chain__hov" tabIndex={0} aria-describedby={`${uid}-req`}>
            <span className="rp-chain__node-icon">
              <RouteIcon />
            </span>
            <span className="rp-chain__node-text">
              <span className="rp-chain__node-title">Request</span>
              {route && (
                <span className="rp-chain__node-sub" title={route}>
                  {route}
                </span>
              )}
            </span>
            <Pop
              id={`${uid}-req`}
              place="start"
              logo={request?.logo ?? <RouteIcon />}
              title={request?.name ?? "Request"}
              sub={route}
              quote={request?.text}
              rows={[...(request?.details ?? []), { label: "Tried so far", value: `${tried.length.toLocaleString("en-US")} of ${attempts.length.toLocaleString("en-US")}` }]}
            />
          </div>

          {/* Branches out to every model – side by side on wide screens, a tree on narrow ones */}
          <svg className="rp-chain__links rp-chain__links--wide" width={LINK} height={height} viewBox={`0 0 ${LINK} ${height}`} aria-hidden="true">
            {inLinks((y) => curve(mid, y, LINK), LINK)}
          </svg>
          <svg className="rp-chain__links rp-chain__links--tree" width="44" height={height} viewBox={`0 0 44 ${height}`} aria-hidden="true">
            {inLinks((y) => branch(y, 20), height, true)}
          </svg>

          <ol ref={models} className="rp-chain__models" aria-label="Models, in the order they're tried">
            {attempts.length === 0 && <li className="rp-chain__empty">No models in this chain</li>}
            {attempts.map((a, i) => {
              const s = statusOf(a);
              const share = Math.min(1, took(a) / limitOf(a));
              return (
                <li key={i} className="rp-chain__model rp-chain__hov" data-status={s} tabIndex={0} aria-describedby={`${uid}-m${i}`}>
                  <span className="rp-chain__logo">
                    <Mark logo={a.logo} model={a.model} />
                    <span className="rp-chain__n">{i + 1}</span>
                  </span>
                  <span className="rp-chain__who">
                    <span className="rp-chain__top">
                      <span className="rp-chain__name" title={a.model}>
                        {a.model}
                      </span>
                      {a.provider && <span className="rp-chain__provider">{a.provider}</span>}
                    </span>
                    <span className="rp-chain__status" key={s} title={lineOf(a, i)}>
                      {lineOf(a, i)}
                    </span>
                  </span>
                  <span className="rp-chain__meta">
                    <span>{ran(a) ? secs(took(a)) : "–"}</span>
                    <span>{ran(a) ? usd(costOf(a)) : ""}</span>
                  </span>
                  {/* Its time against its timeout: a row of blocks that fill one by one */}
                  <span className="rp-chain__blocks" aria-hidden="true">
                    {Array.from({ length: blocks }, (_, k) => {
                      const filled = Math.round(share * blocks);
                      return <i key={k} data-on={k < filled || undefined} data-head={(s === "trying" && k === filled - 1) || undefined} />;
                    })}
                  </span>
                  {/* More on hover: who serves it and where, its time and cost, tokens and price */}
                  <Pop
                    id={`${uid}-m${i}`}
                    place={i < n / 2 ? "below" : "above"}
                    logo={a.providerLogo ?? a.logo}
                    title={a.provider ?? a.model}
                    sub={a.region}
                    badge={STATUS_LABEL[s]}
                    tone={s}
                    rows={[
                      { label: "Model", value: a.model },
                      { label: "Time", value: ran(a) ? `${secs(took(a))} of ${secs(limitOf(a))}` : `up to ${secs(limitOf(a))}` },
                      ...(ran(a) ? [{ label: "Cost", value: usd(costOf(a)) }] : []),
                      ...(Array.isArray(a.details) ? a.details.filter(Boolean) : []),
                      ...(s === "failed" ? [{ label: "Why", value: <span className="rp-chain__pop-err">{a.reason || "Failed"}</span> }] : []),
                    ]}
                  />
                </li>
              );
            })}
          </ol>

          {/* Out: a line from every model on to the response – only the one that answered lights up */}
          <svg className="rp-chain__links rp-chain__links--wide" width={LINK} height={height} viewBox={`0 0 ${LINK} ${height}`} aria-hidden="true">
            {attempts.map((_, i) => (
              <path key={`${i}-${i === answeredAt}`} className="rp-chain__link rp-chain__link--out" data-status={i === answeredAt ? "answered" : "idle"} d={curve(rowY(i), mid, LINK)} pathLength={1} />
            ))}
          </svg>

          {/* Where it ends */}
          <div className="rp-chain__node rp-chain__node--to rp-chain__hov" data-state={state} tabIndex={0} aria-describedby={`${uid}-res`}>
            <span className="rp-chain__node-icon">
              <ReplyIcon filled={state === "answered"} />
            </span>
            <span className="rp-chain__node-text">
              <span className="rp-chain__node-title">{state === "failed" ? "No answer" : "Response"}</span>
              {(() => {
                const sub = state === "answered" ? winner?.note || winner?.model : state === "failed" ? `${attempts.filter((a) => statusOf(a) === "failed").length.toLocaleString("en-US")} failed` : "Waiting…";
                return (
                  <span className="rp-chain__node-sub" key={state} title={sub}>
                    {sub}
                  </span>
                );
              })()}
            </span>
            <Pop
              id={`${uid}-res`}
              place="end"
              logo={response?.logo ?? <ReplyIcon filled={state === "answered"} />}
              title={response?.name ?? "Response"}
              sub={state === "answered" ? `from ${winner?.model}` : state === "failed" ? "No model answered" : "Waiting for an answer"}
              badge={state === "answered" ? "Delivered" : state === "failed" ? "Failed" : "Pending"}
              tone={state === "answered" ? "answered" : state === "failed" ? "failed" : "waiting"}
              quote={state === "answered" ? response?.text : undefined}
              rows={[
                { label: "Total time", value: secs(time) },
                { label: "Total cost", value: usd(spent) },
                { label: "Attempts", value: `${tried.length.toLocaleString("en-US")} of ${attempts.length.toLocaleString("en-US")}` },
                ...(state === "answered" ? (response?.details ?? []) : []),
              ]}
            />
          </div>
        </div>
      </div>
    </div>
  );
}

const css = /* css */ `
/* Colors and motion as variables – override them on .rp-chain */
.rp-chain {
  --rp-chain-tray: #f0f0f0;
  --rp-chain-card: #ffffff;
  --rp-chain-strong: #171717;
  --rp-chain-ink: #171717;
  --rp-chain-text: #6b6b6b;
  --rp-chain-muted: #8a8a8a;
  --rp-chain-faint: #c2c2c2;
  --rp-chain-line: #ececec;
  --rp-chain-link: #d6d6d6;
  --rp-chain-fill: #f2f2f2;
  --rp-chain-idle: #f5f5f5;
  --rp-chain-wait: #fafafa;
  --rp-chain-area: #ffffff;
  --rp-chain-grid: #ededed;
  --rp-chain-accent: #2f6bff;
  --rp-chain-accent-soft: #a9c2ff;
  --rp-chain-accent-bg: #edf2ff;
  --rp-chain-ok: #1a7f45;
  --rp-chain-ok-soft: #9fd9b4;
  --rp-chain-ok-bg: #e6f6ec;
  --rp-chain-fail: #c2410c;
  --rp-chain-fail-soft: #f0a56d;
  --rp-chain-fail-bg: #fdeee3;
  --rp-chain-focus: rgba(23, 23, 23, 0.22);
  --rp-chain-shadow: 0 0 0 1px #ececec, 0 1px 2px rgba(0, 0, 0, 0.04);
  --rp-chain-ease: cubic-bezier(0.22, 1, 0.36, 1);
  --rp-chain-row: ${ROW}px;
  --rp-chain-gap: ${GAP}px;
  position: relative;
  box-sizing: border-box;
  width: 100%;
  max-width: 748px;
  padding: 4px;
  border-radius: 20px;
  background: var(--rp-chain-tray);
  container-type: inline-size;
  font-family: "Inter", ui-sans-serif, system-ui, -apple-system, "Segoe UI", Roboto, sans-serif;
  font-size: 14px;
  font-weight: 400;
  line-height: 20px;
  letter-spacing: -0.006em;
  color: var(--rp-chain-text);
  text-align: left;
  -webkit-font-smoothing: antialiased;
}
.rp-chain *, .rp-chain *::before, .rp-chain *::after { box-sizing: border-box; }
:where(.rp-chain) button { margin: 0; padding: 0; border: 0; background: none; font: inherit; color: inherit; letter-spacing: inherit; cursor: pointer; -webkit-tap-highlight-color: transparent; }
.rp-chain button:focus-visible { outline: 2px solid var(--rp-chain-focus); outline-offset: 1px; }
:where(.rp-chain) ol { margin: 0; padding: 0; list-style: none; }
:where(.rp-chain) svg { display: block; flex: none; }
.rp-chain b { font-weight: 600; }

/* ----- On the tray: what was asked, the attempt and the running totals ----- */
.rp-chain__bar { display: flex; align-items: center; gap: 12px; min-width: 0; min-height: 44px; padding: 0 12px 4px 14px; }
.rp-chain__title { flex: 1; min-width: 0; overflow: hidden; font-weight: 600; color: var(--rp-chain-strong); white-space: nowrap; text-overflow: ellipsis; }
.rp-chain__summary { display: flex; flex: none; align-items: center; gap: 12px; font-size: 13px; line-height: 18px; color: var(--rp-chain-muted); white-space: nowrap; font-variant-numeric: tabular-nums; }
.rp-chain__state { display: inline-flex; align-items: center; gap: 6px; animation: rp-chain-swap 300ms var(--rp-chain-ease); }
.rp-chain__state::before { content: ""; width: 6px; height: 6px; border-radius: 50%; background: var(--rp-chain-faint); transition: background-color 300ms ease; }
.rp-chain__summary[data-state="trying"] .rp-chain__state::before { background: var(--rp-chain-accent); animation: rp-chain-breathe 1200ms ease-in-out infinite alternate; }
.rp-chain__summary[data-state="answered"] .rp-chain__state { color: var(--rp-chain-ok); }
.rp-chain__summary[data-state="answered"] .rp-chain__state::before { background: var(--rp-chain-ok); }
.rp-chain__summary[data-state="failed"] .rp-chain__state { color: var(--rp-chain-fail); }
.rp-chain__summary[data-state="failed"] .rp-chain__state::before { background: var(--rp-chain-fail); }
.rp-chain__state b { color: var(--rp-chain-ink); }
/* The running totals: time and cost, quiet and tabular so they don't jump */
.rp-chain__total { display: inline-flex; align-items: center; gap: 4px; padding-left: 12px; box-shadow: inset 0.5px 0 0 var(--rp-chain-link); color: var(--rp-chain-ink); }
.rp-chain__total svg { color: var(--rp-chain-faint); }
.rp-chain__btn { flex: none; height: 28px; padding: 0 12px; border-radius: 9px; background: var(--rp-chain-card); box-shadow: var(--rp-chain-shadow); font-size: 13px; font-weight: 600; color: var(--rp-chain-strong); transition: background-color 160ms ease; }
.rp-chain__btn:hover { background: var(--rp-chain-fill); }

/* ----- The diagram: request → models → response, on a faint dotted canvas ----- */
/* The diagram: a white card on the tray */
.rp-chain__card { border-radius: 16px; background: var(--rp-chain-area); box-shadow: var(--rp-chain-shadow); }
.rp-chain__flow {
  display: grid;
  grid-template-columns: 140px ${LINK}px minmax(0, 1fr) ${LINK}px 140px;
  align-items: center;
  padding: 22px 16px;
  border-radius: inherit;
  background-image: radial-gradient(circle, var(--rp-chain-grid) 1px, transparent 1.2px);
  background-size: 12px 12px;
  background-position: 6px 6px;
}
.rp-chain__links { overflow: visible; }
.rp-chain__links--tree { display: none; }

/* The two ends: small cards with an icon – the response fills in once there is one */
.rp-chain__node { position: relative; z-index: 1; display: flex; align-items: center; gap: 8px; min-width: 0; height: 52px; padding: 0 8px; border-radius: 12px; background: var(--rp-chain-card); box-shadow: var(--rp-chain-shadow); transition: background-color 400ms ease, box-shadow 400ms ease; }
.rp-chain__node-icon { display: grid; flex: none; place-items: center; width: 24px; height: 24px; border-radius: 7px; background: var(--rp-chain-fill); color: var(--rp-chain-text); transition: background-color 400ms ease, color 400ms ease; }
.rp-chain__node--from .rp-chain__node-icon { background: var(--rp-chain-accent-bg); color: var(--rp-chain-accent); }
.rp-chain__node-text { display: grid; min-width: 0; }
.rp-chain__node-title { overflow: hidden; font-size: 13px; font-weight: 600; line-height: 18px; color: var(--rp-chain-ink); white-space: nowrap; text-overflow: ellipsis; transition: color 400ms ease; }
.rp-chain__node-sub { overflow: hidden; font-size: 12px; line-height: 16px; color: var(--rp-chain-muted); white-space: nowrap; text-overflow: ellipsis; font-variant-numeric: tabular-nums; animation: rp-chain-swap 300ms var(--rp-chain-ease); }
.rp-chain__node--from .rp-chain__node-sub { font-family: ui-monospace, "SF Mono", Menlo, monospace; font-size: 10px; letter-spacing: -0.1px; animation: none; }
/* Waiting for an answer: a light grey card with the same soft shadow as the others, so the dots don't show through */
.rp-chain__node--to:is([data-state="waiting"], [data-state="trying"]) { background: var(--rp-chain-wait); box-shadow: var(--rp-chain-shadow); }
/* Its icon sits on a small white tile, like the models' logos */
.rp-chain__node--to:is([data-state="waiting"], [data-state="trying"]) .rp-chain__node-icon { background: var(--rp-chain-card); box-shadow: var(--rp-chain-shadow); color: var(--rp-chain-faint); }
.rp-chain__node--to:is([data-state="waiting"], [data-state="trying"]) .rp-chain__node-title { color: var(--rp-chain-muted); }
.rp-chain__node--to[data-state="answered"] { box-shadow: 0 0 0 1.5px var(--rp-chain-ok-soft), 0 0 0 5px var(--rp-chain-ok-bg); transition-delay: 500ms; }
.rp-chain__node--to[data-state="answered"] .rp-chain__node-icon { background: var(--rp-chain-ok-bg); color: var(--rp-chain-ok); transition-delay: 500ms; }
.rp-chain__node--to[data-state="answered"] .rp-chain__node-sub { color: var(--rp-chain-ok); }
.rp-chain__node--to[data-state="failed"] { box-shadow: 0 0 0 1px var(--rp-chain-fail-soft), var(--rp-chain-shadow); }
.rp-chain__node--to[data-state="failed"] .rp-chain__node-icon { background: var(--rp-chain-fail-bg); color: var(--rp-chain-fail); }
.rp-chain__node--to[data-state="failed"] .rp-chain__node-sub { color: var(--rp-chain-fail); }

/* Branches: grey until used; the one being tried draws itself in blue, a failed one turns red and dashed, the answer green */
.rp-chain__link { fill: none; stroke: var(--rp-chain-link); stroke-width: 1.25; stroke-linecap: round; vector-effect: non-scaling-stroke; }
.rp-chain__link:is([data-status="waiting"], [data-status="idle"]) { stroke-dasharray: 0.012 0.03; }
.rp-chain__link[data-status="skipped"] { stroke-dasharray: 0.012 0.03; opacity: 0.5; }
.rp-chain__link[data-status="trying"] { stroke: var(--rp-chain-accent-soft); stroke-dasharray: 1 1; animation: rp-chain-draw 600ms var(--rp-chain-ease) both; }
.rp-chain__link[data-status="failed"] { stroke: var(--rp-chain-fail-soft); stroke-dasharray: 0.035 0.035; animation: rp-chain-fade 500ms ease both; }
.rp-chain__link[data-status="answered"] { stroke: var(--rp-chain-ok); stroke-width: 1.5; stroke-dasharray: 1 1; animation: rp-chain-draw 500ms var(--rp-chain-ease) both; }
/* The answer goes on to the response right after its branch in is drawn */
.rp-chain__link--out[data-status="answered"] { animation-delay: 300ms; }
.rp-chain__light { fill: none; stroke-width: 2.5; stroke-linecap: round; vector-effect: non-scaling-stroke; animation: rp-chain-fade 400ms ease 300ms both; }

/* ----- Models: one card each, fixed size ----- */
.rp-chain__models { display: grid; grid-auto-rows: var(--rp-chain-row); gap: var(--rp-chain-gap); min-width: 0; }
.rp-chain__model {
  position: relative;
  display: grid;
  grid-template-columns: 26px minmax(0, 1fr) auto;
  grid-template-rows: auto auto;
  row-gap: 7px;
  align-content: center;
  align-items: center;
  column-gap: 10px;
  min-width: 0;
  padding: 0 12px 0 10px;
  border-radius: 12px;
  background: var(--rp-chain-card);
  box-shadow: var(--rp-chain-shadow);
  transition: box-shadow 400ms ease, opacity 400ms ease;
}
.rp-chain__model[data-status="trying"] { box-shadow: 0 0 0 1.5px var(--rp-chain-accent-soft), 0 0 0 5px var(--rp-chain-accent-bg); }
.rp-chain__model[data-status="answered"] { box-shadow: 0 0 0 1.5px var(--rp-chain-ok-soft), 0 0 0 5px var(--rp-chain-ok-bg); }
.rp-chain__model[data-status="waiting"] :is(.rp-chain__logo, .rp-chain__name) { opacity: 0.6; }
.rp-chain__model[data-status="skipped"] > :not(.rp-chain__pop) { opacity: 0.5; }
.rp-chain__logo { position: relative; grid-row: 1 / 3; display: grid; place-items: center; width: 26px; height: 26px; border-radius: 7px; background: var(--rp-chain-fill); font-size: 10px; font-weight: 500; letter-spacing: 0.3px; color: var(--rp-chain-text); transition: opacity 400ms ease; }
.rp-chain__logo:has(> .rp-chain__mark[data-logo]) { background: var(--rp-chain-card); box-shadow: var(--rp-chain-shadow); }
.rp-chain__mark { display: grid; place-items: center; }
.rp-chain__mark > :is(svg, img) { width: 14px; height: 14px; object-fit: contain; }
/* Its place in the order, on the corner of its mark */
.rp-chain__n { position: absolute; right: -5px; bottom: -4px; display: grid; place-items: center; min-width: 13px; height: 13px; padding: 0 3px; border-radius: 7px; background: var(--rp-chain-card); box-shadow: 0 0 0 0.5px var(--rp-chain-line), 0 1px 2px rgba(0, 0, 0, 0.08); font-size: 8.5px; font-weight: 500; line-height: 13px; color: var(--rp-chain-muted); font-variant-numeric: tabular-nums; transition: background-color 300ms ease, color 300ms ease; }
.rp-chain__model[data-status="trying"] .rp-chain__n { background: var(--rp-chain-accent); color: var(--rp-chain-card); box-shadow: none; }
.rp-chain__model[data-status="failed"] .rp-chain__n { background: var(--rp-chain-fail); color: var(--rp-chain-card); box-shadow: none; }
.rp-chain__model[data-status="answered"] .rp-chain__n { background: var(--rp-chain-ok); color: var(--rp-chain-card); box-shadow: none; }
.rp-chain__who { display: grid; min-width: 0; }
.rp-chain__top { display: flex; align-items: baseline; gap: 6px; min-width: 0; }
/* The provider gives way first when the name needs the room */
.rp-chain__name { flex: 0 1 auto; min-width: 0; overflow: hidden; font-size: 13px; font-weight: 600; line-height: 18px; color: var(--rp-chain-ink); white-space: nowrap; text-overflow: ellipsis; transition: opacity 400ms ease; }
.rp-chain__provider { flex: 0 1000 auto; min-width: 0; overflow: hidden; font-size: 12px; line-height: 16px; color: var(--rp-chain-faint); white-space: nowrap; text-overflow: ellipsis; }
.rp-chain__status { min-width: 0; overflow: hidden; font-size: 12px; line-height: 16px; color: var(--rp-chain-muted); white-space: nowrap; text-overflow: ellipsis; animation: rp-chain-swap 300ms var(--rp-chain-ease); }
.rp-chain__model[data-status="trying"] .rp-chain__status { color: var(--rp-chain-accent); }
.rp-chain__model[data-status="failed"] .rp-chain__status { color: var(--rp-chain-fail); }
.rp-chain__model[data-status="answered"] .rp-chain__status { color: var(--rp-chain-ok); }
/* Its time and cost so far, stacked like the name and status */
.rp-chain__meta { display: grid; justify-items: end; font-size: 12px; line-height: 16px; color: var(--rp-chain-muted); font-variant-numeric: tabular-nums; white-space: nowrap; }
.rp-chain__meta > :first-child { line-height: 18px; font-weight: 500; color: var(--rp-chain-ink); }
.rp-chain__model:is([data-status="waiting"], [data-status="skipped"]) .rp-chain__meta > :first-child { color: var(--rp-chain-faint); }
/* Its time against its timeout: small blocks in a row, each one filling as its share of the timeout passes */
/* Square blocks of a fixed size, as many as fit – spread out so the row fills the card's width */
.rp-chain__blocks { grid-column: 2 / 4; display: flex; justify-content: space-between; gap: ${BLOCK_GAP}px; min-width: 0; overflow: hidden; }
.rp-chain__blocks > i { flex: none; width: ${BLOCK}px; height: ${BLOCK}px; border-radius: 1.25px; background: var(--rp-chain-fill); transition: background-color 220ms ease, opacity 220ms ease; }
.rp-chain__model[data-status="trying"] .rp-chain__blocks > i[data-on] { background: var(--rp-chain-accent); opacity: 0.7; }
.rp-chain__model[data-status="failed"] .rp-chain__blocks > i[data-on] { background: var(--rp-chain-fail-soft); }
.rp-chain__model[data-status="answered"] .rp-chain__blocks > i[data-on] { background: var(--rp-chain-ok-soft); }
/* Waiting and not needed: the empty row steps back, so only the running and finished rows stand out */
.rp-chain__model:is([data-status="waiting"], [data-status="skipped"]) .rp-chain__blocks > i { opacity: 0.55; }
/* The block filling right now breathes */
.rp-chain__blocks > i[data-head] { animation: rp-chain-breathe 700ms ease-in-out infinite alternate; }
.rp-chain__empty { display: grid; place-items: center; height: var(--rp-chain-row); font-size: 12px; color: var(--rp-chain-muted); }

/* While a model runs, a soft sheen sweeps over its time and cost now and then – and over the totals on top */
.rp-chain__model[data-status="trying"] .rp-chain__meta > span, .rp-chain__summary[data-state="trying"] .rp-chain__total {
  --rp-chain-shine: var(--rp-chain-ink);
  background: linear-gradient(100deg, var(--rp-chain-shine) 0 40%, var(--rp-chain-faint) 50%, var(--rp-chain-shine) 60% 100%) 100% 0 / 300% 100% no-repeat;
  -webkit-background-clip: text;
  background-clip: text;
  color: transparent;
  animation: rp-chain-shimmer 2600ms ease-in-out infinite;
}
.rp-chain__model[data-status="trying"] .rp-chain__meta > span:last-child { --rp-chain-shine: var(--rp-chain-muted); }
.rp-chain__summary[data-state="trying"] .rp-chain__total svg { color: var(--rp-chain-faint); }

/* ----- Hover cards ----- */
.rp-chain__hov { outline: none; cursor: default; }
.rp-chain__hov:is(:hover, :focus-visible) { z-index: 20; }
.rp-chain__model.rp-chain__hov:focus-visible, .rp-chain__node.rp-chain__hov:focus-visible { box-shadow: 0 0 0 2px var(--rp-chain-focus), var(--rp-chain-shadow); }
.rp-chain__pop {
  position: absolute;
  z-index: 30;
  display: grid;
  gap: 10px;
  width: 280px;
  padding: 12px;
  border-radius: 16px;
  background: var(--rp-chain-card);
  box-shadow: 0 0 0 1px #ececec, 0 8px 24px -6px rgba(0, 0, 0, 0.16);
  text-align: left;
  opacity: 0;
  visibility: hidden;
  pointer-events: none;
  transition: opacity 160ms ease, transform 200ms var(--rp-chain-ease), visibility 0s linear 200ms;
}
.rp-chain__pop[data-place="below"] { top: calc(100% + 8px); left: 50%; transform: translate(-50%, -4px); }
.rp-chain__pop[data-place="above"] { bottom: calc(100% + 8px); left: 50%; transform: translate(-50%, 4px); }
.rp-chain__pop[data-place="start"] { top: calc(100% + 8px); left: 0; transform: translateY(-4px); }
.rp-chain__pop[data-place="end"] { top: calc(100% + 8px); right: 0; transform: translateY(-4px); }
/* Opens after a short pause, so moving across the diagram doesn't flash cards */
.rp-chain__hov:is(:hover, :focus-visible) > .rp-chain__pop { opacity: 1; visibility: visible; transition: opacity 160ms ease 140ms, transform 220ms var(--rp-chain-ease) 140ms, visibility 0s; }
.rp-chain__hov:is(:hover, :focus-visible) > .rp-chain__pop:is([data-place="below"], [data-place="above"]) { transform: translate(-50%, 0); }
.rp-chain__hov:is(:hover, :focus-visible) > .rp-chain__pop:is([data-place="start"], [data-place="end"]) { transform: none; }
.rp-chain__pop-head { display: flex; align-items: center; gap: 9px; min-width: 0; }
.rp-chain__pop-logo { display: grid; flex: none; place-items: center; width: 28px; height: 28px; border-radius: 8px; background: var(--rp-chain-card); box-shadow: var(--rp-chain-shadow); color: var(--rp-chain-text); }
.rp-chain__pop-logo > :is(svg, img) { width: 16px; height: 16px; object-fit: contain; }
.rp-chain__pop-who { display: grid; flex: 1; min-width: 0; }
.rp-chain__pop-title { overflow: hidden; font-size: 13px; font-weight: 600; line-height: 18px; color: var(--rp-chain-strong); white-space: nowrap; text-overflow: ellipsis; }
.rp-chain__pop-sub { overflow: hidden; font-size: 11.5px; line-height: 16px; color: var(--rp-chain-muted); white-space: nowrap; text-overflow: ellipsis; }
.rp-chain__pop-badge { flex: none; height: 22px; padding: 0 8px; border-radius: 7px; background: var(--rp-chain-fill); font-size: 12px; font-weight: 600; line-height: 20px; color: var(--rp-chain-text); white-space: nowrap; }
.rp-chain__pop-badge[data-tone="trying"] { background: var(--rp-chain-accent-bg); color: var(--rp-chain-accent); }
.rp-chain__pop-badge[data-tone="failed"] { background: var(--rp-chain-fail-bg); color: var(--rp-chain-fail); }
.rp-chain__pop-badge[data-tone="answered"] { background: var(--rp-chain-ok-bg); color: var(--rp-chain-ok); }
.rp-chain__pop-quote { display: block; padding: 9px 11px; border-radius: 10px; background: var(--rp-chain-idle); font-size: 13px; line-height: 19px; color: var(--rp-chain-text); }
/* Three lines at most – the clamp sits inside the padding, so no half line peeks out */
.rp-chain__pop-quote > span { display: -webkit-box; overflow: hidden; -webkit-box-orient: vertical; -webkit-line-clamp: 3; overflow-wrap: anywhere; }
.rp-chain__pop-rows { display: grid; gap: 5px; padding-top: 10px; border-top: 1px solid var(--rp-chain-line); }
.rp-chain__pop-row { display: flex; align-items: baseline; gap: 12px; min-width: 0; font-size: 13px; line-height: 18px; }
.rp-chain__pop-label { flex: none; color: var(--rp-chain-muted); }
/* The card is where the full value is read: a long one wraps, up to three lines */
.rp-chain__pop-value { display: -webkit-box; flex: 1; min-width: 0; overflow: hidden; color: var(--rp-chain-ink); text-align: right; overflow-wrap: anywhere; font-variant-numeric: tabular-nums; -webkit-box-orient: vertical; -webkit-line-clamp: 3; }
.rp-chain__pop-err { color: var(--rp-chain-fail); }

@keyframes rp-chain-swap { from { opacity: 0; transform: translateY(2px); } }
@keyframes rp-chain-shimmer { 0% { background-position: 100% 0; } 55%, 100% { background-position: 0 0; } }
@keyframes rp-chain-breathe { from { opacity: 0.4; } to { opacity: 1; } }
@keyframes rp-chain-draw { from { stroke-dashoffset: 1; } to { stroke-dashoffset: 0; } }
@keyframes rp-chain-fade { from { opacity: 0; } }

/* Less room: the provider goes (it stays in the hover card), so the model's name keeps its room */
@container (max-width: 680px) {
  .rp-chain__provider { display: none; }
}
/* Narrow: a tree, top to bottom – the request on top, the models branching off one line, the response goes */
@container (max-width: 520px) {
  .rp-chain__flow { grid-template-columns: 44px minmax(0, 1fr); padding: 16px 12px 16px 14px; }
  .rp-chain__node--from { grid-column: 1 / -1; justify-self: start; min-width: 150px; margin-bottom: 12px; }
  .rp-chain__links--wide, .rp-chain__node--to { display: none; }
  .rp-chain__links--tree { display: block; align-self: start; margin-top: -12px; }
  .rp-chain__bar { flex-wrap: wrap; row-gap: 4px; }
  .rp-chain__title { flex-basis: 100%; }
  .rp-chain__pop { width: min(280px, calc(100cqw - 24px)); }
  .rp-chain__pop:is([data-place="below"], [data-place="above"]) { left: auto; right: 0; transform: translateY(-4px); }
  .rp-chain__hov:is(:hover, :focus-visible) > .rp-chain__pop:is([data-place="below"], [data-place="above"]) { transform: none; }
}
@media (prefers-reduced-motion: reduce) {
  .rp-chain *, .rp-chain *::before, .rp-chain *::after { animation: none !important; transition: none !important; }
  .rp-chain__light { display: none; }
}

/* Dark mode: follows a .dark class or data-theme="dark" on any ancestor, e.g. <html> */
:where(.dark, [data-theme="dark"]) .rp-chain {
  --rp-chain-tray: #1a1a1c;
  --rp-chain-card: #232326;
  --rp-chain-strong: #f5f5f5;
  --rp-chain-ink: #f0f0f0;
  --rp-chain-text: #a1a1a1;
  --rp-chain-muted: #8a8a8a;
  --rp-chain-faint: #5c5c5c;
  --rp-chain-line: #2e2e31;
  --rp-chain-link: #3a3a3e;
  --rp-chain-fill: #2c2c30;
  --rp-chain-idle: #1f1f22;
  --rp-chain-wait: #1f1f22;
  --rp-chain-area: #202023;
  --rp-chain-grid: #2c2c30;
  --rp-chain-accent: #6d93ff;
  --rp-chain-accent-soft: #3c5fb3;
  --rp-chain-accent-bg: #1d2b4d;
  --rp-chain-ok: #55d68c;
  --rp-chain-ok-soft: #2f7a4b;
  --rp-chain-ok-bg: #15321f;
  --rp-chain-fail: #ffa057;
  --rp-chain-fail-soft: #b06a35;
  --rp-chain-fail-bg: #432614;
  --rp-chain-focus: rgba(255, 255, 255, 0.3);
  --rp-chain-shadow: 0 0 0 1px #2e2e31, 0 1px 2px rgba(0, 0, 0, 0.4);
}
:where(.dark, [data-theme="dark"]) .rp-chain__pop { background: #2a2a2e; box-shadow: 0 0 0 1px #34343a, 0 10px 28px -6px rgba(0, 0, 0, 0.6); }
:where(.dark, [data-theme="dark"]) .rp-chain__pop-logo { background: #333337; }
`;
