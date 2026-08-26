const QUOTA_RE = /insufficient_quota|#token-limit|token-limit|exceeded your current quota|oversized prompt|local precheck rejected/i;
const RATE_RE = /too many requests|rate.?limit|response code=429|account busy|in-flight/i;
const AUTH_RE = /null pointer|FORBIDDEN|Duplicate request|\b401\b|\b403\b|unauthorized|auth|credential|refresh.?token|access.?token/i;
const NOT_READY_RE = /hot context not ready|auth manager not captured|not ready|loginWithDeviceFlow unavailable|loginWithPAT unavailable|worker may not be warm/i;

export const KIND_QUOTA = "quota";
export const KIND_RATE_LIMIT = "rate_limit";
export const KIND_AUTH = "auth";
export const KIND_NOT_READY = "not_ready";
export const KIND_UNAVAILABLE = "unavailable";

const MAX_RETRY_AFTER_SEC = 10 * 60;

export function parseRetryAfter(raw, fallbackSec = 0) {
  const text = String(raw || "").trim();
  if (text) {
    const asNum = Number(text);
    if (Number.isFinite(asNum) && asNum > 0) {
      return Math.min(MAX_RETRY_AFTER_SEC, Math.ceil(asNum));
    }
    const parsed = Date.parse(text);
    if (!Number.isNaN(parsed)) {
      const sec = Math.ceil((parsed - Date.now()) / 1000);
      if (sec > 0) return Math.min(MAX_RETRY_AFTER_SEC, sec);
    }
  }
  return Math.max(0, fallbackSec);
}

function parseJSON(text) {
  try {
    return JSON.parse(String(text || ""));
  } catch {
    return null;
  }
}

function errorPayload(input = {}) {
  if (input && typeof input === "object" && input.error && typeof input.error === "object") {
    return input.error;
  }
  const parsed = parseJSON(input.body || input.message);
  if (parsed?.error && typeof parsed.error === "object") return parsed.error;
  return parsed && typeof parsed === "object" ? parsed : {};
}

export function classifyError(input = {}) {
  const payload = errorPayload(input);
  const message = String(
    input.message || payload.message || input.body || input.error || "",
  );
  const kindHint = String(input.kind || payload.kind || "").trim();
  const statusHint = Number(input.status || payload.status || 0);
  const retryRaw = input.retryAfter || input.retry_after;

  let kind = kindHint;
  if (!kind) {
    if (QUOTA_RE.test(message) || payload.code === "insufficient_quota" || payload.type === "insufficient_quota") {
      kind = KIND_QUOTA;
    } else if (NOT_READY_RE.test(message)) {
      kind = KIND_NOT_READY;
    } else if (AUTH_RE.test(message) && !QUOTA_RE.test(message) && !RATE_RE.test(message)) {
      kind = KIND_AUTH;
    } else if (RATE_RE.test(message) || statusHint === 429) {
      kind = KIND_RATE_LIMIT;
    } else if (statusHint === 401 || statusHint === 403) {
      kind = KIND_AUTH;
    } else {
      kind = KIND_UNAVAILABLE;
    }
  }

  if (kind === KIND_AUTH && QUOTA_RE.test(message)) kind = KIND_QUOTA;
  if (kind === KIND_RATE_LIMIT && QUOTA_RE.test(message)) kind = KIND_QUOTA;

  const defaults = {
    [KIND_QUOTA]: { status: 429, failover: false, cooldownSec: 0, code: "insufficient_quota", type: "insufficient_quota" },
    [KIND_RATE_LIMIT]: { status: 429, failover: true, cooldownSec: 60, code: "rate_limit", type: "api_error" },
    [KIND_AUTH]: { status: statusHint === 401 ? 401 : 403, failover: true, cooldownSec: 30, code: "unauthorized", type: "api_error" },
    [KIND_NOT_READY]: { status: 503, failover: true, cooldownSec: 10, code: "not_ready", type: "api_error" },
    [KIND_UNAVAILABLE]: { status: statusHint >= 500 ? statusHint : 502, failover: true, cooldownSec: 15, code: "upstream_error", type: "api_error" },
  };
  const conf = defaults[kind] || defaults[KIND_UNAVAILABLE];
  const status = statusHint >= 400 && kind !== KIND_QUOTA ? (kind === KIND_RATE_LIMIT ? 429 : statusHint) : conf.status;
  const retryAfterSec = parseRetryAfter(retryRaw, conf.cooldownSec);
  return {
    kind,
    status: kind === KIND_QUOTA ? 429 : status || conf.status,
    failover: typeof payload.failover === "boolean" ? payload.failover : conf.failover,
    cooldownSec: retryAfterSec,
    retryAfterSec,
    code: payload.code || conf.code,
    type: payload.type || conf.type,
    message: message || conf.code,
  };
}

export function shouldFailover(status, body, headers = {}) {
  const classified = classifyError({
    status,
    body,
    retryAfter: headers["retry-after"] || headers["Retry-After"],
  });
  return classified.failover;
}
