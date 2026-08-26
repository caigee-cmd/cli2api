import assert from "node:assert/strict";
import { test } from "node:test";
import { classifyError, shouldFailover } from "../src/errors.mjs";

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

test("shouldFailover reads nested JSON", () => {
  assert.equal(
    shouldFailover(429, JSON.stringify({ error: { message: "insufficient_quota", code: "insufficient_quota" } })),
    false,
  );
  assert.equal(shouldFailover(429, JSON.stringify({ error: { message: "too many requests" } })), true);
});
