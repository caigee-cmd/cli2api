import http from "node:http";
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";
import { register } from "node:module";
import crypto from "node:crypto";
import { buildPlainChatBody, mapModel, wantsReasoning, estimateTokens, estimatePromptTokens } from "./plaintext.mjs";
import { parseNestedOpenAIChunks, readSSEText, pipeNestedSseToOpenAI } from "./sse.mjs";

const __dirname = path.dirname(fileURLToPath(import.meta.url));
register(pathToFileURL(path.join(__dirname, "rewrite-loader.mjs")).href);

const port = Number(process.env.WORKER_PORT || 3020);
const host = process.env.WORKER_HOST || "127.0.0.1";
const apiKey = process.env.PROXY_API_KEY || "";
const qodercliPath =
  process.env.QODERCLI_JS ||
  "/root/.nvm/versions/node/v20.20.2/lib/node_modules/@qoder-ai/qodercli/bundle/qodercli.js";

let hotContext = null;
let hotEndpoint = null;
let hotModelKey = "auto";
let hotModelSource = "system";
let serverStarted = false;
let resolveWarm;
const warmPromise = new Promise((r) => (resolveWarm = r));

function log(...a) {
  console.error("[daemon]", ...a);
}

function loadTemplate() {
  const candidates = [
    process.env.PLAIN_TEMPLATE_PATH,
    "/tmp/qoder-wasm-spike/last-plain.json",
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
  const s = String(errOrText || "");
  // Do NOT treat insufficient_quota / token-limit as auth failures.
  // Aliyun's quota error URL contains "#token-limit", which previously matched /token/ and caused useless rewarm.
  if (/insufficient_quota|token-limit|exceeded your current quota|rate.?limit|too many requests/i.test(s)) {
    return false;
  }
  return /null pointer|FORBIDDEN|Duplicate request|\b401\b|\b403\b|unauthorized|auth|credential|refresh.?token|access.?token/i.test(s);
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
  rewarmPromise = (async () => {
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
  })()
    .finally(() => {
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

  const prev = globalThis.__qoderWorkerOnPrepareInfer;
  globalThis.__qoderWorkerOnPrepareInfer = null;
  let encoded;
  try {
    encoded = hotContext.prepareInferRequest(
      hotEndpoint,
      JSON.stringify(plain),
      plain.model_config?.key || hotModelKey,
      plain.model_config?.source || hotModelSource,
    );
  } finally {
    globalThis.__qoderWorkerOnPrepareInfer = prev;
  }

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
  const promptTokens = estimatePromptTokens(reqBody.messages || []);
  const completionTokens = estimateTokens((parsed.content || "") + (parsed.reasoning || ""));
  return {
    model: plain.model_config.key,
    requestedModel: reqBody.model || plain.model_config.key,
    content: parsed.content || "",
    reasoning: parsed.reasoning || "",
    tool_calls: toolCalls,
    finish_reason: parsed.finish_reason || (toolCalls.length ? "tool_calls" : "stop"),
    promptTokens,
    completionTokens,
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
      res.writeHead(200, {
        "content-type": "text/event-stream; charset=utf-8",
        "cache-control": "no-cache",
        connection: "keep-alive",
      });
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
    if (isAuthError(lastError) && !res.headersSent) {
      log("auth/context failure on stream, rewarm+retry", lastError);
      await rewarmContext(lastError);
      return await run();
    }
    if (res.headersSent) {
      // error event may already have been written by sse pipe
      try { res.end(); } catch {}
      return null;
    }
    throw err;
  }
}

function maybeStartServer() {
  if (serverStarted) return;
  serverStarted = true;
  const server = http.createServer(async (req, res) => {
    try {
      if (req.url === "/health") {
        return sendJSON(res, 200, {
          ok: true,
          hot: !!hotContext,
          endpoint: hotEndpoint,
          modelKey: hotModelKey,
          hasAuthManager: !!(authManager || globalThis.__qoderAuthManager),
          rewarmCount,
          lastRewarmAt,
          lastError,
        });
      }
      if (!authOK(req)) {
        return sendJSON(res, 401, { error: { code: "invalid_api_key", message: "unauthorized" } });
      }
      if (req.method === "POST" && (req.url === "/v1/chat/completions" || req.url === "/internal/chat")) {
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
            prompt_tokens: result.promptTokens || 0,
            completion_tokens: result.completionTokens || 0,
            total_tokens: (result.promptTokens || 0) + (result.completionTokens || 0),
          },
        });
}
      if (req.method === "POST" && req.url === "/admin/rewarm") {
        await rewarmContext("admin_rewarm");
        return sendJSON(res, 200, {
          ok: true,
          rewarmCount,
          lastRewarmAt,
          hot: !!hotContext,
        });
      }
      return sendJSON(res, 404, { error: { code: "not_found", message: "not found" } });
    } catch (err) {
      return sendJSON(res, 500, {
        error: { code: "worker_error", message: err?.message || String(err) },
      });
    }
  });
  server.listen(port, host, () => log(`listening on http://${host}:${port}`));
}

// Keep process alive even if qodercli tries to exit after warmup.
const origExit = process.exit;
process.exit = ((code) => {
  if (hotContext) {
    log(`ignored process.exit(${code}) because worker is warm`);
    return;
  }
  return origExit(code);
});

// Pretend to be a qodercli one-shot warmup, then stay alive.
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

log("importing qodercli for warmup...", qodercliPath);
await import(pathToFileURL(qodercliPath).href);
await warmPromise;
log("warm complete");
