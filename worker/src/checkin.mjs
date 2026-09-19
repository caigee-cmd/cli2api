const endpoints = {
  cn: { base: "https://openapi.qoder.com.cn", origin: "https://qoder.com.cn" },
};
const campaignsPath = "/sash/api/v1/me/campaigns";
const maxResponseBytes = 65536;

function object(value) {
  return value !== null && typeof value === "object" && !Array.isArray(value);
}

function campaignId(value) {
  return typeof value === "string" && value !== "" ? value : "";
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
      if (size > maxResponseBytes) throw new Error("qoder_checkin_response_too_large");
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

function campaignsFrom(payload) {
  if ("campaigns" in payload) {
    if (!Array.isArray(payload.campaigns)) throw new Error("qoder_checkin_invalid_response");
    return payload.campaigns.filter(object);
  }
  if (object(payload.data) && Array.isArray(payload.data.campaigns)) return payload.data.campaigns.filter(object);
  if (Array.isArray(payload.data)) return payload.data.filter(object);
  return [];
}

function unwrapClaim(payload) {
  return object(payload.data) ? payload.data : payload;
}

function creditCampaigns(items) {
  return items.filter((item) => item.actionType === "CLAIM_BENEFIT" && campaignId(item.campaignId));
}

function claimStatus(items, id) {
  const item = items.find((campaign) => campaign.campaignId === id);
  return item?.claimStatus;
}

function rewardFrom(campaign) {
  const benefit = object(campaign.benefit) ? campaign.benefit : null;
  if (!benefit || benefit.kind !== "CREDITS") return;
  if (typeof benefit.amount !== "number" || !Number.isFinite(benefit.amount) || benefit.amount < 0) return;
  return benefit.amount;
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

    async function request(path, method, refreshed = false) {
      const user = auth.getUserInfo();
      const token = user?.security_oauth_token ?? user?.access_token;
      if (typeof token !== "string" || !token) throw new Error("qoder_checkin_token_unavailable");
      let response;
      try {
        response = await fetchImpl(`${endpoint.base}${path}`, {
          method,
          headers: {
            Authorization: `Bearer ${token}`,
            Accept: "application/json",
            "Content-Type": "application/json",
            Origin: endpoint.origin,
            Referer: `${endpoint.origin}/`,
          },
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
        return request(path, method, true);
      }
      if (!response.ok) {
        await response.body?.cancel().catch(() => {});
        throw new Error(`qoder_checkin_http_${response.status}`);
      }
      return readJSON(response);
    }

    async function list() {
      return campaignsFrom(await request(campaignsPath, "GET"));
    }

    let items;
    try {
      items = await list();
    } catch (error) {
      if (error instanceof Error && error.message === "qoder_checkin_http_404") {
        return { status: "skipped", message: "签到活动未开放" };
      }
      throw error;
    }

    const benefits = creditCampaigns(items);
    if (!benefits.length) return { status: "skipped", message: "签到活动未开放" };
    const claimable = benefits.filter((item) => item.claimStatus === "CLAIMABLE");
    if (!claimable.length) {
      if (benefits.some((item) => item.claimStatus === "CLAIMED")) return { status: "already", message: "今日已签到" };
      return { status: "skipped", message: "签到活动未开放" };
    }

    let confirmed = 0;
    let recovered = 0;
    let reward = 0;
    let hasReward = false;
    for (const campaign of claimable) {
      const path = `${campaignsPath}/${encodeURIComponent(campaign.campaignId)}/claim`;
      try {
        const payload = unwrapClaim(await request(path, "POST"));
        if (payload.status !== "CLAIMED") throw new Error("qoder_checkin_claim_unconfirmed");
        confirmed += 1;
      } catch (error) {
        const current = claimStatus(await list().catch(() => []), campaign.campaignId);
        if (current === "CLAIMED") recovered += 1;
        else throw error;
      }
      const amount = rewardFrom(campaign);
      if (amount !== undefined) {
        hasReward = true;
        reward += amount;
      }
    }
    if (!confirmed && !recovered) throw new Error("qoder_checkin_claim_unconfirmed");
    if (!confirmed) return { status: "already", message: "已签到（复查确认）" };
    return {
      status: "success",
      message: hasReward ? `签到成功 +${reward} 积分` : "签到成功",
      ...(hasReward ? { reward_credits: reward } : {}),
    };
  }

  return function checkin() {
    if (!pending) pending = execute().finally(() => { pending = undefined; });
    return pending;
  };
}
