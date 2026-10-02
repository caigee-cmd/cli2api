import { spawn } from "node:child_process";
import { createHash } from "node:crypto";
import { readdirSync } from "node:fs";
import os from "node:os";
import path from "node:path";

const endpoints = {
  cn: { base: "https://openapi.qoder.com.cn", origin: "https://qoder.com.cn" },
};
const campaignsPath = "/sash/api/v1/me/campaigns";
const maxResponseBytes = 65536;
const riskIdentityTimeoutMs = 15000;
const printableHeader = /^[\x21-\x7e](?:[\x20-\x7e]*[\x21-\x7e])?$/u;

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

function headerValue(value, maxBytes = 4096) {
  if (typeof value !== "string") return "";
  const trimmed = value.trim();
  if (!trimmed || Buffer.byteLength(trimmed, "utf8") > maxBytes || !printableHeader.test(trimmed)) return "";
  return trimmed;
}

function machineOS() {
  const arch = process.arch === "arm64" ? "aarch64" : process.arch === "x64" ? "x86_64" : process.arch;
  return `${arch}_${process.platform}`;
}

function machineHostname() {
  let name = "";
  try {
    name = os.hostname();
  } catch {
    return "";
  }
  const trimmed = name.trim();
  if (printableHeader.test(trimmed)) return trimmed.length <= 96 ? trimmed : "";
  const digest = createHash("sha256").update(trimmed, "utf8").digest("hex").slice(0, 8);
  const safe = trimmed.replace(/[^\x21-\x7e]+/gu, "-").replace(/-{2,}/gu, "-").replace(/^-+|-+$/gu, "");
  const shortened = `${safe.slice(0, 87)}-${digest}`.replace(/-+$/u, "");
  return printableHeader.test(shortened) ? shortened : "";
}

function runtimeInfoPath(home = process.env.QODER_HOME || process.env.HOME || "") {
  if (!home) return "";
  const directory = path.join(home, ".bin");
  let names = [];
  try {
    names = readdirSync(directory);
  } catch {
    return "";
  }
  const prefix = `runtime-info-${process.platform}-${process.arch}-`;
  const match = names.filter((name) => name.startsWith(prefix)).sort().at(-1);
  return match ? path.join(directory, match) : "";
}

function accountId(user) {
  for (const value of [user?.uid, user?.user_id, user?.userId, user?.id]) {
    if (typeof value === "string" && value.trim()) return value.trim();
  }
  return "";
}

function readRiskIdentity(executable, account) {
  return new Promise((resolve) => {
    let child;
    try {
      child = spawn(executable, ["3", "--account-stdin"], { stdio: ["pipe", "pipe", "pipe"] });
    } catch {
      resolve(null);
      return;
    }
    let output = "";
    let settled = false;
    const finish = (value) => {
      if (settled) return;
      settled = true;
      clearTimeout(timer);
      resolve(value);
    };
    const timer = setTimeout(() => {
      child.kill("SIGKILL");
      finish(null);
    }, riskIdentityTimeoutMs);
    timer.unref?.();
    child.stdout?.on("data", (chunk) => {
      output += chunk.toString("utf8");
      if (Buffer.byteLength(output, "utf8") > maxResponseBytes) {
        child.kill("SIGKILL");
        finish(null);
      }
    });
    child.once("error", () => finish(null));
    child.once("close", (code) => {
      if (code !== 0) {
        finish(null);
        return;
      }
      const line = output.split("\n", 1)[0];
      try {
        const payload = JSON.parse(line);
        const identity = {
          machineToken: headerValue(payload.machineToken),
          machineType: headerValue(payload.machineType),
          machineCode: headerValue(payload.machineCode),
        };
        finish(identity.machineToken && identity.machineType && identity.machineCode ? identity : null);
      } catch {
        finish(null);
      }
    });
    child.stdin?.on("error", () => {});
    child.stdin?.end(`${JSON.stringify({ account })}\n`);
  });
}

async function machineHeaders(auth, user, options) {
  const headers = {
    "Cosy-MachineOS": machineOS(),
    "Cosy-MachineHostname": machineHostname(),
  };
  let machineId = auth.machineId;
  if (typeof auth.getMachineId === "function") {
    try {
      machineId = await auth.getMachineId();
    } catch {
      machineId = "";
    }
  }
  const id = headerValue(machineId);
  if (id) {
    headers["Cosy-MachineId"] = id;
    headers["Cosy-MachineToken"] = id;
  }
  const executable = (options.runtimeInfoPath ?? runtimeInfoPath)();
  const account = accountId(user);
  if (executable && account) {
    const identity = await (options.readRiskIdentity ?? readRiskIdentity)(executable, account);
    if (identity) {
      headers["Cosy-MachineToken"] = identity.machineToken;
      headers["Cosy-MachineType"] = identity.machineType;
      headers["Cosy-MachineCode"] = identity.machineCode;
    }
  }
  return headers;
}

export function createQoderCheckin({ region, getAuthManager, fetchImpl = (...args) => globalThis.fetch(...args), ...options }) {
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
    const machineHeadersValue = await machineHeaders(auth, auth.getUserInfo(), options);

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
            "User-Agent": "Qoder",
            "Cosy-ClientType": "10",
            "Cosy-Version": "0.3.4",
            ...machineHeadersValue,
            Origin: endpoint.origin,
            Referer: `${endpoint.base}/growth-page/activity-iframe`,
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
    const summary = items.map((item) => `${item.actionType || "unknown"}:${item.claimStatus || "none"}`).join(",");
    if (!benefits.length) {
      console.error("[checkin] no credit campaigns", { count: items.length, summary });
      return { status: "skipped", message: "签到活动未开放" };
    }
    const claimable = benefits.filter((item) => item.claimStatus === "CLAIMABLE");
    if (!claimable.length) {
      if (benefits.some((item) => item.claimStatus === "CLAIMED")) return { status: "already", message: "今日已签到" };
      console.error("[checkin] credit campaigns not claimable", { count: items.length, summary });
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
