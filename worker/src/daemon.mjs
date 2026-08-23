import http from "node:http";
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";
import { register } from "node:module";
import crypto from "node:crypto";
import { execFile } from "node:child_process";
import { promisify } from "node:util";
import { buildPlainChatBody, canonicalModelID, displayModel, mapModel, wantsReasoning, estimateTokens, estimatePromptTokens, diagnoseOpenAIToolHistory } from "./plaintext.mjs";
import { parseNestedOpenAIChunks, readSSEText, pipeNestedSseToOpenAI } from "./sse.mjs";
import { inspectQodercliSource, PINNED_QODERCLI_VERSION, readQodercliVersion } from "./compat.mjs";
import { resolveUsage } from "./usage.mjs";
import { classifyError } from "./errors.mjs";

const __dirname = path.dirname(fileURLToPath(import.meta.url));
register(pathToFileURL(path.join(__dirname, "rewrite-loader.mjs")).href);

const port = Number(process.env.WORKER_PORT || 3020);
const host = process.env.WORKER_HOST || "127.0.0.1";
const apiKey = process.env.PROXY_API_KEY || "";
const accountId = process.env.QODER_ACCOUNT_ID || "default";
const skipCliMain = process.env.QODER_SKIP_CLI_MAIN !== "0";
let bootMode = "pending";
function defaultQodercliPath() {
  const candidates = [
    "/usr/local/lib/node_modules/@qoder-ai/qodercli/bundle/qodercli.js",
    path.join(__dirname, "../node_modules/@qoder-ai/qodercli/bundle/qodercli.js"),
  ];
  for (const candidate of candidates) {
    if (fs.existsSync(candidate)) return candidate;
  }
  return candidates[0];
}

const qodercliPath = process.env.QODERCLI_JS || defaultQodercliPath();

let hotContext = null;
let hotEndpoint = null;
let hotModelKey = "auto";
let hotModelSource = "system";
let serverStarted = false;
let resolveWarm;
const warmPromise = new Promise((r) => (resolveWarm = r));
const execFileAsync = promisify(execFile);

function log(...a) {
  console.error("[daemon]", ...a);
}

function serializeToolDiagnostics(diagnostics) {
  if (!diagnostics || typeof diagnostics !== "object") return diagnostics;
  return {
    ...diagnostics,
    toolResultDiagnostics: JSON.stringify(diagnostics.toolResultDiagnostics || []),
  };
}

function loadTemplate() {
  const candidates = [
    process.env.PLAIN_TEMPLATE_PATH,
    path.join(__dirname, "../last-plain.sample.json"),
  ].filter(Boolean);
  for (const p of candidates) {
    try {
      if (fs.existsSync(p)) return JSON.parse(fs.readFileSync(p, "utf8"));
    } catch {}
  }
  return null;
}

let authManager = null;
let rewarmCount = 0;
let lastRewarmAt = null;
let lastError = null;
let rewarmPromise = null;
let loginState = {
  status: "idle", // idle|pending|ok|error
  authUrl: null,
  message: null,
  startedAt: null,
  finishedAt: null,
};
let loginWaitPromise = null;
let cachedModels = null;
let cachedModelsAt = 0;
let encodeChain = Promise.resolve();
let inFlight = 0;
const maxInFlight = Math.max(1, Number(process.env.QODER_MAX_INFLIGHT || 4) || 4);


function sealContext(ctx) {
  try {
    ctx.free = function () {
      log("ignored QoderContext.free()");
    };
    if (Symbol.dispose) {
      ctx[Symbol.dispose] = function () {
        log("ignored QoderContext[Symbol.dispose]()");
      };
    }
    globalThis.__qoderHotContext = ctx;
  } catch (e) {
    log("seal context failed", e.message);
  }
}

function adoptContext(ctx, endpoint, modelKey, modelSource, mgr) {
  if (!ctx || typeof ctx.prepareInferRequest !== "function") return false;
  hotContext = ctx;
  if (endpoint) hotEndpoint = endpoint;
  if (!hotEndpoint) hotEndpoint = "https://api1.qoder.sh";
  if (modelKey) hotModelKey = modelKey;
  if (modelSource) hotModelSource = modelSource;
  if (mgr) authManager = mgr;
  sealContext(ctx);
  log("adopted hot context", {
    endpoint: hotEndpoint,
    modelKey: hotModelKey,
    ptr: ctx.__wbg_ptr,
    hasAuthManager: !!authManager,
  });
  resolveWarm(true);
  maybeStartServer();
  return true;
}

globalThis.__qoderWorkerAdoptContext = function (ctx, mgr) {
  authManager = mgr || authManager || globalThis.__qoderAuthManager || null;
  adoptContext(ctx, hotEndpoint, hotModelKey, hotModelSource, authManager);
};

globalThis.__qoderWorkerOnPrepareInfer = function (ctx, endpoint, body, modelKey, modelSource) {
  authManager = globalThis.__qoderAuthManager || authManager;
  if (!hotContext) {
    adoptContext(ctx, endpoint, modelKey || "auto", modelSource || "system", authManager);
  } else if (endpoint) {
    hotEndpoint = endpoint;
    hotModelKey = modelKey || hotModelKey;
    hotModelSource = modelSource || hotModelSource;
  }
};

function isAuthError(errOrText) {
  return classifyError({ message: errOrText }).kind === "auth";
}

function withEncodeLock(fn) {
  const run = encodeChain.then(fn, fn);
  encodeChain = run.then(
    () => undefined,
    () => undefined,
  );
  return run;
}

async function acquireInFlight() {
  if (inFlight >= maxInFlight) {
    const err = new Error(`account busy: ${inFlight} in-flight requests (limit ${maxInFlight})`);
    err.kind = "rate_limit";
    err.status = 429;
    throw err;
  }
  inFlight += 1;
}

function releaseInFlight() {
  inFlight = Math.max(0, inFlight - 1);
}

function sendClassified(res, err, extra = {}) {
  const classified = classifyError({
    message: err?.message || String(err || ""),
    kind: err?.kind,
    status: err?.status,
    retryAfter: err?.retryAfter,
    body: err?.body,
  });
  lastError = classified.message;
  if (classified.retryAfterSec > 0) {
    extra["retry-after"] = String(classified.retryAfterSec);
  }
  extra["x-qoder-error-kind"] = classified.kind;
  extra["x-qoder-failover"] = classified.failover ? "1" : "0";
  extra["x-qoder-account"] = accountId;
  const headers = {
    "content-type": "application/json",
    ...extra,
  };
  const body = JSON.stringify({
    error: {
      message: classified.message,
      type: classified.type,
      code: classified.code,
      kind: classified.kind,
      failover: classified.failover,
    },
  });
  headers["content-length"] = Buffer.byteLength(body);
  res.writeHead(classified.status, headers);
  res.end(body);
  return classified;
}

function humanizeUpstreamError(code, msg) {
  const c = String(code || "");
  const m = String(msg || "");
  if (
    c === "insufficient_quota" ||
    /token-limit|exceeded your current quota/i.test(m)
  ) {
    return {
      code: "insufficient_quota",
      type: "insufficient_quota",
      message:
        "Upstream model token/quota limit hit (often input too large or model-specific rate limit), not necessarily zero account balance. Reduce system prompt / history / tools. Detail: " +
        m,
    };
  }
  if (/unknown sse issue|response code=429/i.test(m)) {
    return {
      code: c || "upstream_error",
      type: "api_error",
      message: "Upstream rate-limited/quota error (429). Original: " + m,
    };
  }
  return { code: c || "upstream_error", type: "api_error", message: m };
}

async function rewarmContext(reason = "unknown") {
  if (rewarmPromise) return rewarmPromise;
  rewarmPromise = withEncodeLock(async () => {
    lastError = reason;
    log("rewarm start", reason);
    const mgr = authManager || globalThis.__qoderAuthManager;
    if (!mgr) throw new Error("auth manager not captured; cannot rewarm in-process");
    if (typeof mgr.forceRefreshToken === "function") {
      await mgr.forceRefreshToken(undefined, "worker_rewarm");
    } else if (typeof mgr.refreshTokenIfNeeded === "function") {
      await mgr.refreshTokenIfNeeded(undefined, "worker_rewarm");
    }
    if (typeof mgr.createWasmContext !== "function") {
      throw new Error("auth manager missing createWasmContext");
    }
    await mgr.createWasmContext();
    if (!hotContext) throw new Error("rewarm finished but hot context missing");
    rewarmCount += 1;
    lastRewarmAt = new Date().toISOString();
    lastError = null;
    log("rewarm success", { rewarmCount, lastRewarmAt });
    return true;
  }).finally(() => {
    rewarmPromise = null;
  });
  return rewarmPromise;
}

function authOK(req) {
  if (!apiKey) return true;
  const h = req.headers.authorization || "";
  const got = h.startsWith("Bearer ") ? h.slice(7) : req.headers["x-api-key"];
  return got === apiKey;
}

function sendJSON(res, status, obj) {
  const body = JSON.stringify(obj);
  res.writeHead(status, {
    "content-type": "application/json",
    "content-length": Buffer.byteLength(body),
    "x-qoder-account": accountId,
  });
  res.end(body);
}

async function readBody(req) {
  const chunks = [];
  for await (const c of req) chunks.push(c);
  const raw = Buffer.concat(chunks).toString("utf8");
  return raw ? JSON.parse(raw) : {};
}

async function prepareUpstream(reqBody) {
  if (!hotContext) throw new Error("hot context not ready");
  const template = loadTemplate();
  const plain = buildPlainChatBody({
    messages: reqBody.messages || [],
    model: reqBody.model || hotModelKey,
    system: reqBody.system,
    maxTokens: reqBody.max_tokens || 32000,
    template,
    enableReasoning: wantsReasoning(reqBody),
    enableThinking: typeof reqBody.enable_thinking === "boolean" ? reqBody.enable_thinking : undefined,
    reasoningEffort: reqBody.reasoning_effort,
    reasoningBudgetTokens: reqBody.reasoning_budget_tokens,
    contextLength: reqBody.context_length,
    maxInputTokens: reqBody.max_input_tokens,
    tools: reqBody.tools || [],
    toolChoice: reqBody.tool_choice,
  });
  const approxPrompt = estimatePromptTokens(reqBody.messages || []) + estimateTokens(plain.system || "");
  const systemLen = String(plain.system || "").length;
  const reqSystemMsgs = (reqBody.messages || [])
    .filter((m) => m?.role === "system" || m?.role === "developer")
    .map((m) => String(m?.content || ""));
  const reqSystemJoined = reqSystemMsgs.join("\n\n");
  log("chat prepare", {
    requestedModel: reqBody.model || hotModelKey,
    mappedModel: plain.model_config?.key,
    msgCount: (reqBody.messages || []).length,
    systemLen,
    reqSystemLen: reqSystemJoined.length,
    reqSystemCount: reqSystemMsgs.length,
    approxPromptTokens: approxPrompt,
    toolsInRequest: Array.isArray(reqBody.tools) ? reqBody.tools.length : 0,
    toolDiagnostics: serializeToolDiagnostics(diagnoseOpenAIToolHistory(reqBody.messages || [], reqBody.tools || [])),
    stream: !!reqBody.stream,
    systemPreview: String(plain.system || "").slice(0, 200),
    reqSystemPreview: reqSystemJoined.slice(0, 200),
  });
  // Soft guard: Aliyun/Qoder often returns insufficient_quota when input is too large.
  const maxApprox = Number(process.env.QODER_MAX_APPROX_PROMPT_TOKENS || 120000);
  if (approxPrompt > maxApprox) {
    throw new Error(
      `insufficient_quota: Local precheck rejected oversized prompt (~${approxPrompt} tokens, limit ${maxApprox}). Reduce system prompt / chat history / attachments.`,
    );
  }

  const encoded = await withEncodeLock(async () => {
    if (!hotContext) throw new Error("hot context not ready");
    const prev = globalThis.__qoderWorkerOnPrepareInfer;
    globalThis.__qoderWorkerOnPrepareInfer = null;
    try {
      return hotContext.prepareInferRequest(
        hotEndpoint,
        JSON.stringify(plain),
        plain.model_config?.key || hotModelKey,
        plain.model_config?.source || hotModelSource,
      );
    } finally {
      globalThis.__qoderWorkerOnPrepareInfer = prev;
    }
  });

  const headers = {};
  if (encoded?.headers?.forEach) encoded.headers.forEach((v, k) => (headers[k] = v));
  else Object.assign(headers, encoded?.headers || {});

  const upstream = await fetch(encoded.url, {
    method: "POST",
    headers,
    body: encoded.body,
  });
  return { plain, upstream };
}

async function encodeAndFetch(reqBody) {
  const { plain, upstream } = await prepareUpstream(reqBody);
  const sseText = await readSSEText(upstream);
  const parsed = parseNestedOpenAIChunks(sseText);
  if (parsed.error && !(parsed.content || parsed.reasoning)) {
    const code = parsed.error.code || parsed.error.type || "upstream_error";
    const msg = parsed.error.message || parsed.error.localizedMessage || JSON.stringify(parsed.error).slice(0, 300);
    const formatted = humanizeUpstreamError(code, msg);
    throw new Error(`${formatted.code}: ${formatted.message}`);
  }
  if (!(parsed.content || parsed.reasoning) && /Duplicate request|FORBIDDEN|401|403|null pointer/i.test(sseText)) {
    throw new Error(`upstream bad response: ${sseText.slice(0, 300)}`);
  }
  const toolCalls = Array.isArray(parsed.tool_calls) ? parsed.tool_calls : [];
  if (!(parsed.content || parsed.reasoning || toolCalls.length)) {
    throw new Error("upstream returned empty content (possible context overflow / silent reject)");
  }
  const usage = resolveUsage(parsed.usage, {
    prompt_tokens: estimatePromptTokens(reqBody.messages || []),
    completion_tokens: estimateTokens((parsed.content || "") + (parsed.reasoning || "")),
    source: "estimate",
  });
  return {
    model: plain.model_config.key,
    requestedModel: reqBody.model || plain.model_config.key,
    content: parsed.content || "",
    reasoning: parsed.reasoning || "",
    tool_calls: toolCalls,
    finish_reason: parsed.finish_reason || (toolCalls.length ? "tool_calls" : "stop"),
    promptTokens: usage.prompt_tokens,
    completionTokens: usage.completion_tokens,
    usage,
  };
}

async function chatOnce(reqBody) {
  try {
    return await encodeAndFetch(reqBody);
  } catch (err) {
    lastError = err?.message || String(err);
    if (!isAuthError(lastError)) throw err;
    log("auth/context failure, rewarm+retry", lastError);
    await rewarmContext(lastError);
    return await encodeAndFetch(reqBody);
  }
}

async function chatStream(reqBody, res) {
  const run = async () => {
    const { plain, upstream } = await prepareUpstream(reqBody);
    if (!res.headersSent) {
      res.setHeader("content-type", "text/event-stream; charset=utf-8");
      res.setHeader("cache-control", "no-cache");
      res.setHeader("connection", "keep-alive");
    }
    return pipeNestedSseToOpenAI(upstream, res, {
      model: reqBody.model || plain.model_config.key,
      promptTokens: estimatePromptTokens(reqBody.messages || []),
    });
  };
  try {
    return await run();
  } catch (err) {
    lastError = err?.message || String(err);
    console.error("[daemon] chat stream failed", {
      model: reqBody.model || "auto",
      headersSent: res.headersSent,
      error: lastError,
    });
    if (isAuthError(lastError) && !res.headersSent) {
      log("auth/context failure on stream, rewarm+retry", lastError);
      await rewarmContext(lastError);
      return await run();
    }
    if (res.headersSent) {
      res.destroy(err);
      return null;
    }
    throw err;
  }
}


function qodercliBin() {
  return (
    process.env.QODERCLI_BIN ||
    "qodercli"
  );
}

function parseModelList(stdout) {
  const lines = String(stdout || "")
    .split(/\n/)
    .map((l) => l.trim())
    .filter(Boolean);
  const out = [];
  for (const line of lines) {
    if (/^MODEL$/i.test(line)) continue;
    if (/^----/.test(line)) continue;
    out.push(line);
  }
  // unique preserve order
  return [...new Set(out)];
}

async function listModelsFromCli({ refresh = false } = {}) {
  const now = Date.now();
  if (!refresh && cachedModels && now - cachedModelsAt < 60_000) return cachedModels;
  try {
    const { stdout } = await execFileAsync(
      qodercliBin(),
      ["--list-models"],
      {
        timeout: 45_000,
        env: {
          ...process.env,
          HOME: process.env.HOME || "/root",
        },
        maxBuffer: 2_000_000,
      },
    );
    const names = parseModelList(stdout);
    cachedModels = names.map((name) => {
      const displayName = String(name);
      const id = canonicalModelID(displayName);
      const mapped = mapModel(displayName);
      return {
        id,
        display_name: displayName,
        mapped_key: mapped,
        route_display_name: displayModel(mapped),
        object: "model",
        owned_by: "qoder",
      };
    });
    cachedModelsAt = now;
    return cachedModels;
  } catch (err) {
    // fallback static catalog if CLI fails (e.g. not logged in)
    const fallback = [
      "Auto","Ultimate","Performance","Efficient","Lite","Cantus",
      "Qwen3.8-Max","Qwen3.7-Max","Qwen3.7-Plus",
      "Kimi-K3","Kimi-K2.7-Code",
      "GLM-5.3","GLM-5.2",
      "DeepSeek-V4-Pro","DeepSeek-V4-Flash",
      "MiniMax-M3",
    ];
    return fallback.map((displayName) => ({
      id: canonicalModelID(displayName),
      display_name: displayName,
      mapped_key: mapModel(displayName),
      route_display_name: displayModel(mapModel(displayName)),
      object: "model",
      owned_by: "qoder",
      stale: true,
      error: err?.message || String(err),
    }));
  }
}

function getAuthManager() {
  return authManager || globalThis.__qoderAuthManager || null;
}

async function startDeviceLogin() {
  const mgr = getAuthManager();
  if (!mgr || typeof mgr.loginWithDeviceFlow !== "function") {
    throw new Error("AuthManager.loginWithDeviceFlow unavailable. Worker may not be warm yet.");
  }
  if (loginWaitPromise) {
    return {
      ok: true,
      status: loginState.status,
      authUrl: loginState.authUrl,
      message: "login already in progress",
    };
  }
  loginState = {
    status: "pending",
    authUrl: null,
    message: "starting device flow",
    startedAt: new Date().toISOString(),
    finishedAt: null,
  };
  const result = await mgr.loginWithDeviceFlow();
  const authUrl = result?.authUrl || result?.url || null;
  const loginComplete = result?.loginComplete;
  if (!authUrl) throw new Error("device flow did not return authUrl");
  loginState.authUrl = authUrl;
  loginState.message = "Open the auth URL in a browser to finish login";
  loginWaitPromise = Promise.resolve()
    .then(async () => {
      if (loginComplete && typeof loginComplete.then === "function") {
        await loginComplete;
      }
      loginState = {
        status: "ok",
        authUrl,
        message: "login completed",
        startedAt: loginState.startedAt,
        finishedAt: new Date().toISOString(),
      };
      try {
        await rewarmContext("login_complete");
      } catch (e) {
        loginState.message = `login completed, rewarm failed: ${e?.message || e}`;
      }
      cachedModels = null;
    })
    .catch((err) => {
      loginState = {
        status: "error",
        authUrl,
        message: err?.message || String(err),
        startedAt: loginState.startedAt,
        finishedAt: new Date().toISOString(),
      };
    })
    .finally(() => {
      loginWaitPromise = null;
    });
  return {
    ok: true,
    status: loginState.status,
    authUrl,
    message: loginState.message,
  };
}

async function loginWithPat(pat) {
  const mgr = getAuthManager();
  if (!mgr || typeof mgr.loginWithPAT !== "function") {
    throw new Error("AuthManager.loginWithPAT unavailable");
  }
  const token = String(pat || "").trim();
  if (!token) throw new Error("pat required");
  await mgr.loginWithPAT(token, { persist: true });
  cachedModels = null;
  await rewarmContext("pat_login");
  loginState = {
    status: "ok",
    authUrl: null,
    message: "PAT login completed",
    startedAt: new Date().toISOString(),
    finishedAt: new Date().toISOString(),
  };
  return { ok: true, status: "ok" };
}


function assertAPIKey() {
  const allowInsecure = process.env.ALLOW_INSECURE_API_KEY === "1";
  if (!allowInsecure && (!apiKey || apiKey === "change-me" || apiKey === "dev-key")) {
    throw new Error("PROXY_API_KEY is required. Set a real key, or ALLOW_INSECURE_API_KEY=1 for local experiments.");
  }
}

function maybeStartServer() {
  if (serverStarted) return;
  assertAPIKey();
  serverStarted = true;
  const server = http.createServer(async (req, res) => {
    try {
      const url = new URL(req.url, `http://${host}:${port}`);
      if (url.pathname === "/health") {
        const mgr = getAuthManager();
        const user = mgr && typeof mgr.getUserInfo === "function" ? mgr.getUserInfo() : null;
        return sendJSON(res, 200, {
          ok: true,
          hot: !!hotContext,
          ready: !!hotContext,
          endpoint: hotEndpoint,
          modelKey: hotModelKey,
          hasAuthManager: !!mgr,
          accountId,
          bootMode,
          uid: user?.uid || null,
          inFlight,
          maxInFlight,
          rewarmCount,
          lastRewarmAt,
          lastError,
          login: {
            status: loginState.status,
            authUrl: loginState.authUrl,
            message: loginState.message,
          },
        });
      }
      // Management + chat both require PROXY_API_KEY when configured.
      if (!authOK(req)) {
        return sendJSON(res, 401, { error: { code: "invalid_api_key", message: "unauthorized" } });
      }
      if (req.method === "GET" && url.pathname === "/admin/models") {
        const refresh = url.searchParams.get("refresh") === "1";
        const models = await listModelsFromCli({ refresh });
        return sendJSON(res, 200, { object: "list", data: models });
      }
      if (req.method === "GET" && url.pathname === "/admin/login/status") {
        return sendJSON(res, 200, {
          ok: true,
          hot: !!hotContext,
          hasAuthManager: !!getAuthManager(),
          login: loginState,
        });
      }
      if (req.method === "POST" && url.pathname === "/admin/login/device") {
        const out = await startDeviceLogin();
        return sendJSON(res, 200, out);
      }
      if (req.method === "POST" && url.pathname === "/admin/login/pat") {
        const body = await readBody(req);
        const out = await loginWithPat(body.pat || body.token || body.PAT);
        return sendJSON(res, 200, out);
      }
      if (req.method === "POST" && url.pathname === "/admin/rewarm") {
        await rewarmContext("admin_rewarm");
        return sendJSON(res, 200, {
          ok: true,
          rewarmCount,
          lastRewarmAt,
          hot: !!hotContext,
        });
      }
      if (req.method === "POST" && (url.pathname === "/v1/chat/completions" || url.pathname === "/internal/chat")) {
        await acquireInFlight();
        try {
        const body = await readBody(req);
        if (body.stream) {
          await chatStream(body, res);
          return res.end();
        }
        const result = await chatOnce(body);
        const message = { role: "assistant", content: result.content || (result.tool_calls?.length ? null : "") };
        if (result.reasoning) {
          message.reasoning_content = result.reasoning;
        }
        if (Array.isArray(result.tool_calls) && result.tool_calls.length) {
          message.tool_calls = result.tool_calls;
        }
        return sendJSON(res, 200, {
          id: `chatcmpl-${Date.now()}`,
          object: "chat.completion",
          created: Math.floor(Date.now() / 1000),
          model: result.requestedModel || result.model,
          choices: [
            {
              index: 0,
              message,
              finish_reason: result.finish_reason || (result.tool_calls?.length ? "tool_calls" : "stop"),
            },
          ],
          usage: {
            prompt_tokens: result.usage?.prompt_tokens || result.promptTokens || 0,
            completion_tokens: result.usage?.completion_tokens || result.completionTokens || 0,
            total_tokens: result.usage?.total_tokens || ((result.promptTokens || 0) + (result.completionTokens || 0)),
            source: result.usage?.source || "estimate",
            ...(result.usage?.cache_read_tokens != null ? { cache_read_tokens: result.usage.cache_read_tokens } : {}),
            ...(result.usage?.cache_write_tokens != null ? { cache_write_tokens: result.usage.cache_write_tokens } : {}),
            ...(result.usage?.prompt_tokens_details ? { prompt_tokens_details: result.usage.prompt_tokens_details } : {}),
            ...(result.usage?.credits != null ? { credits: result.usage.credits } : {}),
          },
        });
        } finally {
          releaseInFlight();
        }
      }
      return sendJSON(res, 404, { error: { code: "not_found", message: "not found" } });
    } catch (err) {
      if (!res.headersSent) return sendClassified(res, err);
      try { res.end(); } catch {}
    }
  });
  server.listen(port, host, () => log(`listening on http://${host}:${port}`));
}

// Keep process alive even if qodercli tries to exit after boot.
const origExit = process.exit;
process.exit = ((code) => {
  if (serverStarted || hotContext) {
    log(`ignored process.exit(${code}) because worker is running`);
    return;
  }
  return origExit(code);
});

async function bootFromAuthManager({ getQoderAuthManager, initializeQoderRuntime }) {
  bootMode = "pure-wasm";
  log("pure-wasm boot: init runtime + local auth, skip CLI warmup chat");
  if (typeof initializeQoderRuntime === "function") {
    await initializeQoderRuntime({ initializeWasm: true });
  }
  const mgr = typeof getQoderAuthManager === "function" ? getQoderAuthManager() : null;
  if (mgr) {
    authManager = mgr;
    globalThis.__qoderAuthManager = mgr;
    try {
      if (typeof mgr.initAuth === "function") await mgr.initAuth();
    } catch (err) {
      lastError = err?.message || String(err);
      log("initAuth failed; console login still available", lastError);
    }
  }
  maybeStartServer();
  resolveWarm(true);
}

globalThis.__qoderWorkerBoot = bootFromAuthManager;

function useWarmupArgv() {
  process.argv = [
    process.execPath,
    qodercliPath,
    "--print",
    "--output-format",
    "json",
    "--model",
    "auto",
    "--dangerously-skip-permissions",
    "--cwd",
    process.env.QODER_WARMUP_CWD || "/tmp",
    "--",
    "只回复OK",
  ];
}

function assertQodercliCompatible(jsPath) {
  if (!fs.existsSync(jsPath)) {
    throw new Error(
      `qodercli bundle not found at ${jsPath}. Install @qoder-ai/qodercli@${PINNED_QODERCLI_VERSION} and set QODERCLI_JS.`,
    );
  }
  const source = fs.readFileSync(jsPath, "utf8");
  const version = readQodercliVersion(jsPath);
  const report = inspectQodercliSource(source, { version });
  log("qodercli compat", {
    path: jsPath,
    version: version || "unknown",
    pinned: PINNED_QODERCLI_VERSION,
    ok: report.ok,
  });
  if (!report.ok) throw new Error(report.message);
  if (version && version !== PINNED_QODERCLI_VERSION) {
    log(
      `warning: qodercli ${version} != pinned ${PINNED_QODERCLI_VERSION}; hooks may break after CLI upgrades`,
    );
  }
}

assertQodercliCompatible(qodercliPath);
const qodercliSource = fs.readFileSync(qodercliPath, "utf8");
const canSkipMain = skipCliMain && qodercliSource.includes("async function HEg(){let{main:A}=await Promise.resolve().then(()=>(b$o(),U$o));await A()}");
if (!canSkipMain) {
  bootMode = "warmup-import";
  log("skip-main needle missing or disabled; falling back to one-shot warmup import");
  useWarmupArgv();
} else {
  bootMode = "pure-wasm";
  log("skip-main needle present; CLI main will hand off to worker boot");
}
log("importing qodercli...", qodercliPath);
await import(pathToFileURL(qodercliPath).href);
await warmPromise;
if (!serverStarted) maybeStartServer();
log("warm complete", { bootMode, hot: !!hotContext, accountId });
