import assert from "node:assert/strict";
import { test } from "node:test";
import { classifyError, shouldFailover } from "../src/errors.mjs";
import { pickChild, publicAccountView } from "../src/pool.mjs";

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

test("pickChild skips down and sticky-escapes", () => {
  const now = 1_000_000;
  const children = [
    { id: "a", downUntil: now + 10_000, ok: true },
    { id: "b", ok: true },
  ];
  const pinned = pickChild(children, { prefer: "a", now, cursor: 0 });
  assert.equal(pinned.item.id, "b");
  assert.equal(pinned.escaped, true);
  const rr = pickChild(children, { now, cursor: 0 });
  assert.equal(rr.item.id, "b");
});

test("publicAccountView redacts home and reports cooldown", () => {
  const view = publicAccountView(
    { id: "acc1", home: "/root/.qoder", url: "http://127.0.0.1:3021", downUntil: Date.now() + 5000, lastError: "429" },
    Date.now(),
  );
  assert.equal(view.id, "acc1");
  assert.equal(view.home, undefined);
  assert.equal(view.ready, false);
  assert.ok(view.down_until);
});
