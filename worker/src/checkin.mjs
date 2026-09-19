const endpoints = {
  cn: { base: "https://openapi.qoder.com.cn", origin: "https://qoder.com.cn" },
};

function object(value) {
  return value !== null && typeof value === "object" && !Array.isArray(value);
}

async function readJSON(response) {
  if (!response.body) throw new Error("qoder_checkin_empty_response");
  const reader = response.body.getReader();
  const chunks = [];
  let size = 0;
  try {
    for (;;) {
      const { value, done } = await reader.read();
      if (done) break;
      size += value.byteLength;
      if (size > 65536) throw new Error("qoder_checkin_response_too_large");
      chunks.push(value);
    }
  } finally {
    await reader.cancel().catch(() => {});
    reader.releaseLock();
  }
  let payload;
  try {
    payload = JSON.parse(Buffer.concat(chunks).toString("utf8"));
  } catch {
    throw new Error("qoder_checkin_invalid_json");
  }
  if (!object(payload)) throw new Error("qoder_checkin_invalid_response");
  return payload;
}

export function createQoderCheckin({ region, getAuthManager, fetchImpl = (...args) => globalThis.fetch(...args) }) {
  let pending;

  async function execute() {
    const endpoint = endpoints[region];
    if (!endpoint) throw new Error("qoder_checkin_region_unsupported");
    const auth = getAuthManager();
    if (!auth?.isAuthenticated?.()) throw new Error("qoder_checkin_not_authenticated");
    if (typeof auth.getUserInfo !== "function" || typeof auth.refreshTokenIfNeeded !== "function") {
      throw new Error("qoder_checkin_auth_api_incompatible");
    }
    try {
      await auth.refreshTokenIfNeeded(undefined, "worker_checkin");
    } catch {
      throw new Error("qoder_checkin_auth_refresh_failed");
    }

    async function request(action, method, refreshed = false) {
      const user = auth.getUserInfo();
      const token = user?.security_oauth_token ?? user?.access_token;
      if (typeof token !== "string" || !token) throw new Error("qoder_checkin_token_unavailable");
      let response;
      try {
        response = await fetchImpl(`${endpoint.base}/sash/api/v1/me/daily-check-in/${action}`, {
          method,
          headers: {
            Authorization: `Bearer ${token}`,
            Accept: "application/json",
            "Content-Type": "application/json",
            Origin: endpoint.origin,
            Referer: `${endpoint.origin}/`,
          },
          ...(method === "POST" ? { body: "{}" } : {}),
          redirect: "manual",
          signal: AbortSignal.timeout(15000),
        });
      } catch {
        throw new Error("qoder_checkin_request_failed");
      }
      if (response.status === 401 && !refreshed && typeof auth.forceRefreshToken === "function") {
        await response.body?.cancel().catch(() => {});
        try {
          await auth.forceRefreshToken(undefined, "worker_checkin_unauthorized");
        } catch {
          throw new Error("qoder_checkin_auth_refresh_failed");
        }
        return request(action, method, true);
      }
      if (!response.ok && response.status !== 409) {
        await response.body?.cancel().catch(() => {});
        throw new Error(`qoder_checkin_http_${response.status}`);
      }
      const payload = await readJSON(response);
      if (response.status === 409 && payload.result !== "ALREADY_CLAIMED") {
        throw new Error("qoder_checkin_http_409");
      }
      return payload;
    }

    async function status() {
      const payload = await request("status", "GET");
      if (!["CLAIMABLE", "CLAIMED", "DISABLED"].includes(payload.status)) {
        throw new Error("qoder_checkin_unknown_status");
      }
      return payload.status;
    }

    const current = await status();
    if (current === "CLAIMED") return { status: "already", message: "今日已签到" };
    if (current === "DISABLED") return { status: "skipped", message: "签到活动未开放" };
    try {
      const payload = await request("claim", "POST");
      if (payload.result === "ALREADY_CLAIMED") return { status: "already", message: "今日已签到" };
      if (payload.success !== true) throw new Error("qoder_checkin_claim_unconfirmed");
      const reward = payload.rewardCredits;
      if (reward !== undefined && (typeof reward !== "number" || !Number.isFinite(reward) || reward < 0)) {
        throw new Error("qoder_checkin_invalid_reward");
      }
      return {
        status: "success",
        message: reward === undefined ? "签到成功" : `签到成功 +${reward} 积分`,
        ...(reward === undefined ? {} : { reward_credits: reward }),
      };
    } catch (error) {
      if (await status().catch(() => null) === "CLAIMED") {
        return { status: "already", message: "已签到（复查确认）" };
      }
      throw error;
    }
  }

  return function checkin() {
    if (!pending) pending = execute().finally(() => { pending = undefined; });
    return pending;
  };
}
