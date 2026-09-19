import assert from "node:assert/strict";
import { test } from "node:test";
import { createQoderCheckin } from "../src/checkin.mjs";

const campaignsURL = "https://openapi.qoder.com.cn/sash/api/v1/me/campaigns";
const claimURL = (id) => `${campaignsURL}/${id}/claim`;

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

function credit({ id = "01a0b4aa-fbd4-720f-87bc-7eb1c6c9ddb6", status = "CLAIMABLE", amount = 100 } = {}) {
  return {
    campaignId: id,
    campaignKey: "act-20260918-628",
    actionType: "CLAIM_BENEFIT",
    claimStatus: status,
    benefit: { kind: "CREDITS", amount },
  };
}

function details({ id = "01a05bbf-5668-7031-83d6-91545f97ec05", status = "CLAIMABLE" } = {}) {
  return { campaignId: id, campaignKey: "act-20260901-922", actionType: "VIEW_DETAILS", claimStatus: status };
}

function listed(...campaigns) {
  return { uid: "user", showCampaign: true, claimable: campaigns.some((item) => item.claimStatus === "CLAIMABLE"), campaigns };
}

test("CN check-in lists campaigns then claims the CLAIM_BENEFIT campaign", async () => {
  const id = "01a0b4aa-fbd4-720f-87bc-7eb1c6c9ddb6";
  const { checkin, calls } = fixture([json(listed(credit({ id }), details())), json({ status: "CLAIMED" })]);
  assert.deepEqual(await checkin(), { status: "success", message: "签到成功 +100 积分", reward_credits: 100 });
  assert.equal(calls.length, 2);
  assert.equal(calls[0].url, campaignsURL);
  assert.equal(calls[0].init.method, "GET");
  assert.equal(calls[1].url, claimURL(id));
  assert.equal(calls[1].init.method, "POST");
  assert.equal(calls[1].init.body, undefined);
  assert.equal(calls[1].init.headers.Authorization, "Bearer test-token");
  assert.equal(calls[1].init.headers.Origin, "https://qoder.com.cn");
  assert.equal(calls[1].init.headers["User-Agent"], "Qoder");
  assert.equal(calls[1].init.headers["Cosy-ClientType"], "10");
  assert.equal(calls[1].init.headers["Cosy-Version"], "0.3.4");
  assert.equal(calls[1].init.headers.Referer, "https://openapi.qoder.com.cn/growth-page/activity-iframe");
  assert.equal(calls[1].init.redirect, "manual");
  assert.ok(calls[1].init.signal instanceof AbortSignal);
  assert.ok(!calls.some((call) => String(call.url).includes("daily-check-in")));
});

test("VIEW_DETAILS campaigns are never claimed", async () => {
  const { checkin, calls } = fixture([json(listed(details()))]);
  assert.equal((await checkin()).status, "skipped");
  assert.equal(calls.length, 1);
});

test("already-claimed credit campaigns never POST", async () => {
  const { checkin, calls } = fixture([json(listed(credit({ status: "CLAIMED" }), details({ status: "CLAIMED" })))]);
  assert.deepEqual(await checkin(), { status: "already", message: "今日已签到" });
  assert.equal(calls.length, 1);
});

test("empty or closed campaign lists skip without claiming", async () => {
  for (const payload of [{ showCampaign: false, campaigns: [] }, listed()]) {
    const { checkin, calls } = fixture([json(payload)]);
    assert.equal((await checkin()).status, "skipped");
    assert.equal(calls.length, 1);
    assert.equal(calls[0].init.method, "GET");
  }
});

test("missing campaigns list is treated as closed", async () => {
  const { checkin, calls } = fixture([json({ showCampaign: true, claimable: false })]);
  assert.equal((await checkin()).status, "skipped");
  assert.equal(calls.length, 1);
});

test("404 list is skipped, not a protocol error", async () => {
  const { checkin, calls } = fixture([json({}, 404)]);
  assert.equal((await checkin()).status, "skipped");
  assert.equal(calls.length, 1);
});

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

test("invalid campaign list is a protocol error", async () => {
  const { checkin, calls } = fixture([json({ campaigns: "nope" })]);
  await assert.rejects(checkin(), /invalid_response/);
  assert.equal(calls.length, 1);
});

test("claim response must be CLAIMED", async () => {
  const { checkin, calls } = fixture([json(listed(credit())), json({ success: true }), json(listed(credit()))]);
  await assert.rejects(checkin(), /unconfirmed/);
  assert.equal(calls.filter((call) => call.init.method === "POST").length, 1);
});

test("wrapped claim data.status CLAIMED counts as success", async () => {
  const { checkin } = fixture([json(listed(credit({ amount: 100 }))), json({ data: { status: "CLAIMED" } })]);
  assert.deepEqual(await checkin(), { status: "success", message: "签到成功 +100 积分", reward_credits: 100 });
});

test("ALREADY claimed after a failed POST is idempotent", async () => {
  const { checkin, calls } = fixture([
    json(listed(credit())),
    json({}, 409),
    json(listed(credit({ status: "CLAIMED" }))),
  ]);
  assert.equal((await checkin()).status, "already");
  assert.equal(calls.filter((call) => call.init.method === "POST").length, 1);
});

test("ambiguous claim failure is confirmed by a second list instead of re-POSTing", async () => {
  const { checkin, calls } = fixture([
    json(listed(credit())),
    new Error("network failed with secret-token"),
    json(listed(credit({ status: "CLAIMED" }))),
  ]);
  assert.equal((await checkin()).status, "already");
  assert.equal(calls.filter((call) => call.init.method === "POST").length, 1);
});

test("upstream errors cannot leak response bodies or credentials", async () => {
  const { checkin } = fixture([json({ error: "secret-token" }, 500)]);
  await assert.rejects(checkin(), (error) => error.message === "qoder_checkin_http_500");
});

test("401 refreshes once and reads the new token", async () => {
  const { checkin, calls, refreshes } = fixture([json({}, 401), json(listed(credit({ status: "CLAIMED" })))]);
  assert.equal((await checkin()).status, "already");
  assert.equal(refreshes(), 1);
  assert.equal(calls[1].init.headers.Authorization, "Bearer new-test-token");
  const failing = fixture([json({}, 401), json({}, 401)]);
  await assert.rejects(failing.checkin(), /http_401/);
  assert.equal(failing.refreshes(), 1);
});

test("concurrent calls share one operation and subsequent calls can run again", async () => {
  const { checkin, calls } = fixture([
    json(listed(credit())),
    json({ status: "CLAIMED" }),
    json(listed(credit({ status: "CLAIMED" }))),
  ]);
  const first = checkin();
  assert.equal(first, checkin());
  await first;
  assert.equal(calls.length, 2);
  assert.equal((await checkin()).status, "already");
  assert.equal(calls.length, 3);
});

test("multiple CLAIMABLE credit campaigns are claimed and rewards summed", async () => {
  const first = credit({ id: "camp-a", amount: 100 });
  const second = credit({ id: "camp-b", amount: 50 });
  const { checkin, calls } = fixture([
    json(listed(first, second, details())),
    json({ status: "CLAIMED" }),
    json({ status: "CLAIMED" }),
  ]);
  assert.deepEqual(await checkin(), { status: "success", message: "签到成功 +150 积分", reward_credits: 150 });
  assert.deepEqual(calls.map((call) => call.url), [campaignsURL, claimURL("camp-a"), claimURL("camp-b")]);
});

test("response limits and JSON validation fail closed", async () => {
  for (const response of [new Response(" ".repeat(65537)), new Response("not json"), json([]), new Response(null, { status: 204 })]) {
    const { checkin } = fixture([response]);
    await assert.rejects(checkin(), /too_large|invalid_json|invalid_response|empty_response/);
  }
});
