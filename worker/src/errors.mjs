const PROMPT_LIMIT_RE = /#?token-limit|oversized prompt|prompt too (large|long)|context length|local precheck rejected/i;
const QUOTA_RE = /insufficient_quota|exceeded your current quota|额度已用尽|额度用尽|购买加量包/i;
const RATE_RE = /too many requests|rate.?limit|response code=429|resource_exhausted|rate_limit_exceeded|account busy|in-flight/i;
const AUTH_RE = /null pointer|FORBIDDEN|Duplicate request|\b401\b|\b403\b|unauthorized|auth|credential|refresh.?token|access.?token/i;
const NOT_READY_RE = /hot context not ready|auth manager not captured|not ready|loginWithDeviceFlow unavailable|loginWithPAT unavailable|worker may not be warm/i;
const MODEL_RE = /model_not_available|model_catalog_unavailable|is not available for this qoder account/i;

export const KIND_QUOTA = "quota";
export const KIND_RATE_LIMIT = "rate_limit";
export const KIND_AUTH = "auth";
export const KIND_NOT_READY = "not_ready";
export const KIND_UNAVAILABLE = "unavailable";
export const KIND_MODEL_NOT_AVAILABLE = "model_not_available";
export const KIND_INVALID_REQUEST = "invalid_request";

const MAX_RETRY_AFTER_SEC = 10 * 60;
const MIN_RATE_LIMIT_COOLDOWN_SEC = 30;

function parseDurationSeconds(raw) {
  const match = String(raw || "").trim().match(/^([+-]?(?:\d+(?:\.\d*)?|\.\d+))\s*(ns|us|µs|μs|ms|s|m|h)$/i);
  if (!match) return 0;
  const value = Number(match[1]);
  if (!Number.isFinite(value) || value <= 0) return 0;
  const unit = match[2].toLowerCase();
  const multiplier = {
    ns: 1e-9,
    us: 1e-6,
    "µs": 1e-6,
    "μs": 1e-6,
    ms: 1e-3,
    s: 1,
    m: 60,
    h: 3600,
  }[unit];
  return value * multiplier;
}

function parseRetryAfterValue(raw) {
  if (raw == null || raw === "") return 0;
  if (typeof raw === "number") {
    if (!Number.isFinite(raw) || raw <= 0) return 0;
    if (raw >= 1e12 && raw < 1e14) return (raw - Date.now()) / 1000;
    if (raw >= 1e9 && raw < 1e11) return raw - Date.now() / 1000;
    return raw;
  }
  const text = String(raw).trim();
  if (!text) return 0;
  const duration = parseDurationSeconds(text);
  if (duration > 0) return duration;
  const asNum = Number(text);
  if (Number.isFinite(asNum) && asNum > 0) {
    if (asNum >= 1e12 && asNum < 1e14) return (asNum - Date.now()) / 1000;
    if (asNum >= 1e9 && asNum < 1e11) return asNum - Date.now() / 1000;
    return asNum;
  }
  const parsed = Date.parse(text);
  return Number.isNaN(parsed) ? 0 : (parsed - Date.now()) / 1000;
}

function retryHintFromValue(value, parsePrimitive = false) {
  if (typeof value === "string") {
    if (parsePrimitive) {
      const parsed = parseRetryAfterValue(value);
      if (parsed > 0) return parsed;
    }
    try {
      return retryHintFromValue(JSON.parse(value), false);
    } catch {
      return 0;
    }
  }
  if (typeof value === "number") return parsePrimitive ? parseRetryAfterValue(value) : 0;
  if (Array.isArray(value)) {
    for (const item of value) {
      const parsed = retryHintFromValue(item, false);
      if (parsed > 0) return parsed;
    }
    return 0;
  }
  if (!value || typeof value !== "object") return 0;
  const keys = ["retry_after", "retryAfter", "quotaResetDelay", "resets_in_seconds", "resets_at", "reset_at"];
  for (const key of keys) {
    const actual = Object.keys(value).find((candidate) => candidate.toLowerCase() === key.toLowerCase());
    if (!actual) continue;
    const parsed = retryHintFromValue(value[actual], true);
    if (parsed > 0) return parsed;
  }
  for (const item of Object.values(value)) {
    if (!item || typeof item !== "object") continue;
    const parsed = retryHintFromValue(item, false);
    if (parsed > 0) return parsed;
  }
  return 0;
}

export function parseRetryAfter(raw, fallbackSec = 0) {
  const parsed = parseRetryAfterValue(raw);
  if (parsed > 0) return Math.min(MAX_RETRY_AFTER_SEC, Math.ceil(parsed));
  const fallback = parseRetryAfterValue(fallbackSec);
  return Math.min(MAX_RETRY_AFTER_SEC, Math.max(0, Math.ceil(fallback)));
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
  const nestedPayload = payload.data && typeof payload.data === "object" ? payload.data : {};
  const kindHint = String(input.kind || payload.kind || nestedPayload.kind || "").trim();
  const statusHint = Number(input.status || payload.status || nestedPayload.status || 0);
  const payloadCode = String(payload.code ?? payload.msgCode ?? nestedPayload.code ?? nestedPayload.msgCode ?? "").trim();
  const payloadType = String(payload.type || nestedPayload.type || "").trim();
  const searchable = `${message} ${payloadCode} ${payloadType} ${payload.msg || nestedPayload.msg || ""}`;
  const retryRaw = input.retryAfter ?? input.retry_after ?? payload.retry_after ?? payload.retryAfter ?? nestedPayload.retry_after ?? nestedPayload.retryAfter;

  let kind = kindHint;
  if (PROMPT_LIMIT_RE.test(searchable)) {
    kind = KIND_INVALID_REQUEST;
  } else if (!kind) {
    if (QUOTA_RE.test(searchable) || ["insufficient_quota", "1005", "4008", "14018"].includes(payloadCode)) {
      kind = KIND_QUOTA;
    } else if (MODEL_RE.test(searchable) || payloadCode === "model_not_available" || payloadCode === "model_catalog_unavailable") {
      kind = KIND_MODEL_NOT_AVAILABLE;
    } else if (NOT_READY_RE.test(searchable)) {
      kind = KIND_NOT_READY;
    } else if (AUTH_RE.test(searchable) && !QUOTA_RE.test(searchable) && !RATE_RE.test(searchable)) {
      kind = KIND_AUTH;
    } else if (RATE_RE.test(searchable) || payloadCode === "429" || statusHint === 429) {
      kind = KIND_RATE_LIMIT;
    } else if (statusHint === 401 || statusHint === 403) {
      kind = KIND_AUTH;
    } else {
      kind = KIND_UNAVAILABLE;
    }
  }

  if (kind === KIND_AUTH && QUOTA_RE.test(searchable)) kind = KIND_QUOTA;
  if (kind === KIND_RATE_LIMIT && QUOTA_RE.test(searchable)) kind = KIND_QUOTA;

  const defaults = {
    [KIND_QUOTA]: { status: 429, failover: false, cooldownSec: 0, code: "insufficient_quota", type: "insufficient_quota" },
    [KIND_RATE_LIMIT]: { status: 429, failover: true, cooldownSec: 60, code: "rate_limit", type: "api_error" },
    [KIND_AUTH]: { status: statusHint === 401 ? 401 : 403, failover: true, cooldownSec: 30, code: "unauthorized", type: "api_error" },
    [KIND_NOT_READY]: { status: 503, failover: true, cooldownSec: 10, code: "not_ready", type: "api_error" },
    [KIND_MODEL_NOT_AVAILABLE]: { status: 400, failover: true, cooldownSec: 0, code: "model_not_available", type: "invalid_request_error" },
    [KIND_INVALID_REQUEST]: { status: 400, failover: false, cooldownSec: 0, code: "invalid_request", type: "invalid_request_error" },
    [KIND_UNAVAILABLE]: { status: statusHint >= 500 ? statusHint : 502, failover: true, cooldownSec: 15, code: "upstream_error", type: "api_error" },
  };
  const conf = defaults[kind] || defaults[KIND_UNAVAILABLE];
  const status = kind === KIND_INVALID_REQUEST
    ? 400
    : statusHint >= 400 && kind !== KIND_QUOTA
      ? (kind === KIND_RATE_LIMIT ? 429 : statusHint)
      : conf.status;
  let retryAfterSec = parseRetryAfter(retryRaw, retryHintFromValue(payload) || conf.cooldownSec);
  if (kind === KIND_RATE_LIMIT && retryAfterSec > 0 && retryAfterSec < MIN_RATE_LIMIT_COOLDOWN_SEC) {
    retryAfterSec = MIN_RATE_LIMIT_COOLDOWN_SEC;
  }
  return {
    kind,
    status: kind === KIND_QUOTA ? 429 : status || conf.status,
    failover: typeof payload.failover === "boolean" ? payload.failover : conf.failover,
    cooldownSec: retryAfterSec,
    retryAfterSec,
    code: payloadCode || conf.code,
    type: payloadType || conf.type,
    message: message || payloadCode || conf.code,
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
