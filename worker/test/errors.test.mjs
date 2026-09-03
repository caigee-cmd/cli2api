import assert from "node:assert/strict";
import { test } from "node:test";
import { classifyError, parseRetryAfter, shouldFailover } from "../src/errors.mjs";

test("quota is 429 and does not failover", () => {
  const got = classifyError({
    message: "insufficient_quota: Upstream model token/quota limit hit #token-limit",
  });
  assert.equal(got.kind, "quota");
  assert.equal(got.status, 429);
  assert.equal(got.failover, false);
  assert.equal(got.cooldownSec, 0);
});

test("generic 429 rate limit failovers with cooldown", () => {
  const got = classifyError({ status: 429, message: "unknown sse issue response code=429" });
  assert.equal(got.kind, "rate_limit");
  assert.equal(got.failover, true);
  assert.equal(got.cooldownSec, 60);
});

test("auth is not confused by token-limit", () => {
  const got = classifyError({ message: "insufficient_quota token-limit unauthorized" });
  assert.equal(got.kind, "quota");
  assert.equal(got.failover, false);
});

test("not ready is 503 and failovers", () => {
  const got = classifyError({ message: "hot context not ready" });
  assert.equal(got.kind, "not_ready");
  assert.equal(got.status, 503);
  assert.equal(got.failover, true);
});

test("device-flow not warm is not_ready not auth", () => {
  const got = classifyError({
    message: "AuthManager.loginWithDeviceFlow unavailable. Worker may not be warm yet.",
  });
  assert.equal(got.kind, "not_ready");
  assert.equal(got.status, 503);
  assert.equal(got.failover, true);
});

test("Retry-After is capped", () => {
  const got = classifyError({ status: 429, message: "too many requests", retryAfter: "99999" });
  assert.equal(got.retryAfterSec, 600);
});

test("model_not_available failovers without cooldown", () => {
  const got = classifyError({
    message: "model_not_available: hy3 is not available for this Qoder account",
  });
  assert.equal(got.kind, "model_not_available");
  assert.equal(got.status, 400);
  assert.equal(got.failover, true);
  assert.equal(got.cooldownSec, 0);
});

test("shouldFailover reads nested JSON", () => {
  assert.equal(
    shouldFailover(429, JSON.stringify({ error: { message: "insufficient_quota", code: "insufficient_quota" } })),
    false,
  );
  assert.equal(shouldFailover(429, JSON.stringify({ error: { message: "too many requests" } })), true);
});

test("short rate-limit hints use the storm-protection floor", () => {
  const got = classifyError({
    status: 400,
    body: JSON.stringify({ error: { code: "RESOURCE_EXHAUSTED", quotaResetDelay: "708.717057ms" } }),
  });
  assert.equal(got.kind, "rate_limit");
  assert.equal(got.retryAfterSec, 30);
});

test("parses duration and Unix millisecond retry hints", () => {
  assert.equal(parseRetryAfter("708.717057ms", 60), 1);
  const resetAt = Date.now() + 45_000;
  const seconds = parseRetryAfter(String(resetAt), 0);
  assert.ok(seconds >= 40 && seconds <= 46, `seconds=${seconds}`);
});

test("recognizes nested CodeBuddy quota code", () => {
  const got = classifyError({
    body: JSON.stringify({ error: { data: { code: 14018, msg: "额度已用尽" } } }),
  });
  assert.equal(got.kind, "quota");
  assert.equal(got.code, "14018");
  assert.equal(got.failover, false);
});
