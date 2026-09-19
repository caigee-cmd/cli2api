import assert from "node:assert/strict";
import { test } from "node:test";
import { createQoderCheckin } from "../src/checkin.mjs";

function fixture(responses, options = {}) {
  const calls = [];
  let refreshed = 0;
  const auth = {
    isAuthenticated: () => true,
    getUserInfo: () => ({ security_oauth_token: refreshed ? "new-test-token" : "test-token" }),
    refreshTokenIfNeeded: async () => {},
    forceRefreshToken: async () => { refreshed++; },
  };
  const checkin = createQoderCheckin({
    region: "cn",
    getAuthManager: () => auth,
    fetchImpl: async (url, init) => {
      calls.push({ url, init });
      const next = responses.shift();
      if (next instanceof Error) throw next;
      if (!next) throw new Error("unexpected request");
      return next;
    },
    ...options,
  });
  return { checkin, calls, auth, refreshes: () => refreshed };
}

function json(payload, status = 200) {
  return new Response(JSON.stringify(payload), { status, headers: { "content-type": "application/json" } });
}

test("CN check-in uses captured token, exact endpoints and empty JSON claim", async () => {
  const { checkin, calls } = fixture([json({ status: "CLAIMABLE" }), json({ success: true, rewardCredits: 100 })]);
  assert.deepEqual(await checkin(), { status: "success", message: "签到成功 +100 积分", reward_credits: 100 });
  assert.equal(calls.length, 2);
  assert.equal(calls[0].url, "https://openapi.qoder.com.cn/sash/api/v1/me/daily-check-in/status");
  assert.equal(calls[1].url, "https://openapi.qoder.com.cn/sash/api/v1/me/daily-check-in/claim");
  assert.equal(calls[0].init.method, "GET");
  assert.equal(calls[1].init.method, "POST");
  assert.equal(calls[1].init.body, "{}");
  assert.equal(calls[1].init.headers.Authorization, "Bearer test-token");
  assert.equal(calls[1].init.redirect, "manual");
  assert.ok(calls[1].init.signal instanceof AbortSignal);
});

for (const [state, result] of [["CLAIMED", "already"], ["DISABLED", "skipped"]]) {
  test(`${state} never issues a claim`, async () => {
    const { checkin, calls } = fixture([json({ status: state })]);
    assert.equal((await checkin()).status, result);
    assert.equal(calls.length, 1);
  });
}

test("unsupported regions never touch auth or the network", async () => {
  const { checkin, calls } = fixture([], { region: "global", getAuthManager: () => { throw new Error("unexpected auth access"); } });
  await assert.rejects(checkin(), /region_unsupported/);
  assert.equal(calls.length, 0);
});

test("missing login and incompatible auth accessor fail closed", async () => {
  for (const auth of [null, { isAuthenticated: () => true }]) {
    const { checkin, calls } = fixture([], { getAuthManager: () => auth });
    await assert.rejects(checkin(), /not_authenticated|auth_api_incompatible/);
    assert.equal(calls.length, 0);
  }
});

test("unknown state is a protocol error, not a disabled campaign", async () => {
  const { checkin, calls } = fixture([json({ status: "NEW_STATUS" })]);
  await assert.rejects(checkin(), /unknown_status/);
  assert.equal(calls.length, 1);
});

test("ALREADY_CLAIMED is idempotent", async () => {
  const { checkin, calls } = fixture([json({ status: "CLAIMABLE" }), json({ result: "ALREADY_CLAIMED" }, 409)]);
  assert.equal((await checkin()).status, "already");
  assert.equal(calls.length, 2);
});

for (const payload of [{}, { success: false }, { success: true, rewardCredits: -1 }, { success: true, rewardCredits: "100" }]) {
  test(`invalid claim is never recorded as successful: ${JSON.stringify(payload)}`, async () => {
    const { checkin, calls } = fixture([json({ status: "CLAIMABLE" }), json(payload), json({ status: "CLAIMABLE" })]);
    await assert.rejects(checkin(), /unconfirmed|invalid_reward/);
    assert.equal(calls.filter((call) => call.init.method === "POST").length, 1);
  });
}

test("ambiguous claim failure is confirmed by status instead of re-POSTing", async () => {
  const { checkin, calls } = fixture([json({ status: "CLAIMABLE" }), new Error("network failed with secret-token"), json({ status: "CLAIMED" })]);
  assert.equal((await checkin()).status, "already");
  assert.equal(calls.filter((call) => call.init.method === "POST").length, 1);
});

test("upstream errors cannot leak response bodies or credentials", async () => {
  const { checkin } = fixture([json({ error: "secret-token" }, 500)]);
  await assert.rejects(checkin(), (error) => error.message === "qoder_checkin_http_500");
});

test("401 refreshes once and reads the new token", async () => {
  const { checkin, calls, refreshes } = fixture([json({}, 401), json({ status: "CLAIMED" })]);
  assert.equal((await checkin()).status, "already");
  assert.equal(refreshes(), 1);
  assert.equal(calls[1].init.headers.Authorization, "Bearer new-test-token");
  const failing = fixture([json({}, 401), json({}, 401)]);
  await assert.rejects(failing.checkin(), /http_401/);
  assert.equal(failing.refreshes(), 1);
});

test("concurrent calls share one operation and subsequent calls can run again", async () => {
  const { checkin, calls } = fixture([json({ status: "CLAIMABLE" }), json({ success: true }), json({ status: "CLAIMED" })]);
  const first = checkin();
  assert.equal(first, checkin());
  await first;
  assert.equal(calls.length, 2);
  assert.equal((await checkin()).status, "already");
  assert.equal(calls.length, 3);
});

test("response limits and JSON validation fail closed", async () => {
  for (const response of [new Response(" ".repeat(65537)), new Response("not json"), json([]), new Response(null, { status: 204 })]) {
    const { checkin } = fixture([response]);
    await assert.rejects(checkin(), /too_large|invalid_json|invalid_response|empty_response/);
  }
});
