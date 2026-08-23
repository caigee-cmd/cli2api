const PROMPT_KEYS = [
  "prompt_tokens",
  "input_tokens",
  "input_token_count",
  "total_input_tokens",
  "promptTokens",
  "inputTokens",
];
const COMPLETION_KEYS = [
  "completion_tokens",
  "output_tokens",
  "output_token_count",
  "total_output_tokens",
  "completionTokens",
  "outputTokens",
];
const TOTAL_KEYS = ["total_tokens", "total_token_count", "totalTokens"];
const CACHE_READ_KEYS = [
  "cache_read_tokens",
  "cache_read_input_tokens",
  "cached_tokens",
  "cached_content_token_count",
];
const CACHE_WRITE_KEYS = [
  "cache_write_tokens",
  "cache_creation_input_tokens",
  "cache_creation_tokens",
];
const CREDIT_KEYS = ["credits", "total_credits", "original_credits"];

function coerceCount(value) {
  if (value == null || value === "") return null;
  const n = typeof value === "number" ? value : Number(value);
  return Number.isFinite(n) && n >= 0 ? n : null;
}

function pickKeys(obj, keys) {
  if (!obj || typeof obj !== "object") return null;
  for (const key of keys) {
    if (Object.prototype.hasOwnProperty.call(obj, key)) {
      const n = coerceCount(obj[key]);
      if (n != null) return n;
    }
  }
  return null;
}

function parseMaybeJson(value) {
  if (typeof value !== "string") return value;
  const text = value.trim();
  if (!text.startsWith("{") && !text.startsWith("[")) return null;
  try {
    return JSON.parse(text);
  } catch {
    return null;
  }
}

export function extractUsage(input) {
  const best = {
    prompt_tokens: null,
    completion_tokens: null,
    total_tokens: null,
    cache_read_tokens: null,
    cache_write_tokens: null,
    credits: null,
  };

  const visit = (value, depth) => {
    if (value == null || depth > 8) return;
    if (typeof value === "string") {
      const parsed = parseMaybeJson(value);
      if (parsed) visit(parsed, depth + 1);
      return;
    }
    if (typeof value !== "object") return;
    if (Array.isArray(value)) {
      for (const item of value) visit(item, depth + 1);
      return;
    }

    const prompt = pickKeys(value, PROMPT_KEYS);
    const completion = pickKeys(value, COMPLETION_KEYS);
    const total = pickKeys(value, TOTAL_KEYS);
    const cacheRead = pickKeys(value, CACHE_READ_KEYS);
    const cacheWrite = pickKeys(value, CACHE_WRITE_KEYS);
    const credits = pickKeys(value, CREDIT_KEYS);
    if (prompt != null) best.prompt_tokens = prompt;
    if (completion != null) best.completion_tokens = completion;
    if (total != null) best.total_tokens = total;
    if (cacheRead != null) best.cache_read_tokens = cacheRead;
    if (cacheWrite != null) best.cache_write_tokens = cacheWrite;
    if (credits != null) best.credits = credits;

    if (value.prompt_tokens_details) visit(value.prompt_tokens_details, depth + 1);
    if (value.usage) visit(value.usage, depth + 1);
    if (value.llm_model_result) visit(value.llm_model_result, depth + 1);
    if (value.body) visit(value.body, depth + 1);
    if (value.data) visit(value.data, depth + 1);
  };

  visit(input, 0);
  return {
    ...best,
    source:
      best.prompt_tokens != null ||
      best.completion_tokens != null ||
      best.cache_read_tokens != null ||
      best.cache_write_tokens != null
        ? "upstream"
        : "estimate",
  };
}

export function resolveUsage(input, fallback = {}) {
  const extracted = extractUsage(input);
  const prompt = extracted.prompt_tokens ?? fallback.prompt_tokens ?? 0;
  const completion = extracted.completion_tokens ?? fallback.completion_tokens ?? 0;
  const total = extracted.total_tokens ?? prompt + completion;
  const usage = {
    prompt_tokens: prompt,
    completion_tokens: completion,
    total_tokens: total,
    source: extracted.source === "upstream" ? "upstream" : fallback.source || "estimate",
  };
  const cacheRead = extracted.cache_read_tokens ?? fallback.cache_read_tokens;
  const cacheWrite = extracted.cache_write_tokens ?? fallback.cache_write_tokens;
  if (cacheRead != null) {
    usage.cache_read_tokens = cacheRead;
    usage.prompt_tokens_details = { cached_tokens: cacheRead };
  }
  if (cacheWrite != null) usage.cache_write_tokens = cacheWrite;
  if (extracted.credits != null) usage.credits = extracted.credits;
  return usage;
}

export function usageLooksUseful(body) {
  if (!body || typeof body !== "object") return false;
  return Boolean(
    body.usage ||
      body.llm_model_result ||
      pickKeys(body, PROMPT_KEYS) != null ||
      pickKeys(body, COMPLETION_KEYS) != null ||
      pickKeys(body, CACHE_READ_KEYS) != null ||
      pickKeys(body, CACHE_WRITE_KEYS) != null ||
      pickKeys(body, CREDIT_KEYS) != null,
  );
}
