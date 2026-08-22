export function parseHomes(raw) {
  const text = String(raw || "").trim();
  if (!text) return [];
  return text
    .split(",")
    .map((part) => part.trim())
    .filter(Boolean)
    .map((part, idx) => {
      const split = part.includes("=") ? part.split("=") : part.split(":");
      if (split.length >= 2 && (split[1].startsWith("/") || split[1].startsWith("~") || split[0].length < 64)) {
        const id = split[0].trim();
        const home = split.slice(1).join(":").trim();
        return { id: id || `acc${idx + 1}`, home };
      }
      return { id: `acc${idx + 1}`, home: part };
    });
}

export function isDown(child, now = Date.now()) {
  return Boolean(child?.downUntil && now < child.downUntil);
}

export function isReady(child, now = Date.now()) {
  if (!child) return false;
  if (isDown(child, now)) return false;
  if (child.ok === false) return false;
  return true;
}

export function pickChild(children, { prefer, excluded, cursor = 0, now = Date.now() } = {}) {
  const list = Array.isArray(children) ? children : [];
  const skip = excluded instanceof Set ? excluded : new Set(excluded || []);
  if (prefer) {
    const hit = list.find((c) => c.id === prefer);
    if (hit && !skip.has(hit.id) && isReady(hit, now)) {
      return { item: hit, cursor, escaped: false };
    }
  }
  const n = list.length;
  for (let i = 0; i < n; i++) {
    const idx = (cursor + i) % n;
    const item = list[idx];
    if (!item || skip.has(item.id) || !isReady(item, now)) continue;
    return { item, cursor: (idx + 1) % n, escaped: Boolean(prefer) };
  }
  let fallback = null;
  for (const item of list) {
    if (!item || skip.has(item.id)) continue;
    if (!fallback || (item.downUntil || 0) < (fallback.downUntil || 0)) fallback = item;
  }
  return { item: fallback, cursor, escaped: Boolean(prefer) };
}

export function publicAccountView(child, now = Date.now()) {
  const down = isDown(child, now);
  return {
    id: child.id,
    url: child.url || (child.port ? `http://127.0.0.1:${child.port}` : undefined),
    ok: child.ok !== false && !down,
    hot: !!child.hot,
    ready: isReady(child, now),
    inFlight: child.inFlight || 0,
    rewarmCount: child.rewarmCount || 0,
    restarts: child.restarts || 0,
    last_error: child.lastError || child.last_error || null,
    down_until: down ? new Date(child.downUntil).toISOString() : null,
  };
}
