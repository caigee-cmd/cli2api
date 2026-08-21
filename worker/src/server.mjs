import http from "node:http";
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { warmupContext } from "./warmup.mjs";
import { buildPlainChatBody, wantsReasoning } from "./plaintext.mjs";
import { parseNestedOpenAIChunks, readSSEText } from "./sse.mjs";
import * as bridge from "./context-bridge.mjs";

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const port = Number(process.env.WORKER_PORT || 3020);
const host = process.env.WORKER_HOST || "127.0.0.1";
const apiKey = process.env.PROXY_API_KEY || "";

function loadTemplate() {
  const candidates = [
    process.env.PLAIN_TEMPLATE_PATH,
    "/tmp/qoder-wasm-spike/last-plain.json",
    path.resolve(__dirname, "../../testdata/last-plain.sample.json"),
  ].filter(Boolean);
  for (const p of candidates) {
    try {
      if (fs.existsSync(p)) return JSON.parse(fs.readFileSync(p, "utf8"));
    } catch {}
  }
  return null;
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
  if (!raw) return {};
  return JSON.parse(raw);
}

async function encodeAndChat(reqBody, { stream = false } = {}) {
  if (!bridge.hotContext) throw new Error("hot QoderContext not ready");
  const template = loadTemplate();
  const plain = buildPlainChatBody({
    messages: reqBody.messages || [],
    model: reqBody.model || bridge.hotModelKey || "auto",
    system: reqBody.system,
    maxTokens: reqBody.max_tokens || 32000,
    template,
    enableReasoning: wantsReasoning(reqBody),
  });

  // Disable capture recursion.
  const prev = globalThis.__qoderWorkerOnPrepareInfer;
  globalThis.__qoderWorkerOnPrepareInfer = null;
  let encoded;
  try {
    encoded = bridge.hotContext.prepareInferRequest(
      bridge.hotEndpoint,
      JSON.stringify(plain),
      plain.model_config?.key || bridge.hotModelKey || "auto",
      plain.model_config?.source || bridge.hotModelSource || "system",
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
  if (!upstream.ok && upstream.status !== 200) {
    const t = await upstream.text().catch(() => "");
    throw new Error(`upstream status=${upstream.status}: ${t.slice(0, 300)}`);
  }

  const sseText = await readSSEText(upstream);
  const parsed = parseNestedOpenAIChunks(sseText);
  return {
    plainIds: {
      request_id: plain.request_id,
      session_id: plain.session_id,
      model: plain.model_config.key,
    },
    content: parsed.content,
    reasoning: parsed.reasoning,
    sseText,
    stream,
  };
}

const server = http.createServer(async (req, res) => {
  try {
    if (req.url === "/health") {
      return sendJSON(res, 200, {
        ok: true,
        hot: !!bridge.hotContext,
        endpoint: bridge.hotEndpoint,
        modelKey: bridge.hotModelKey,
      });
    }
    if (!authOK(req)) return sendJSON(res, 401, { error: { code: "invalid_api_key", message: "unauthorized" } });

    if (req.method === "POST" && (req.url === "/v1/chat/completions" || req.url === "/internal/chat")) {
      const body = await readBody(req);
      if (body.stream) {
        // Phase C will do true streaming. For now collect then emulate one-shot.
      }
      const result = await encodeAndChat(body, { stream: !!body.stream });
      if (body.stream) {
        res.writeHead(200, {
          "content-type": "text/event-stream; charset=utf-8",
          "cache-control": "no-cache",
          connection: "keep-alive",
        });
        const id = `chatcmpl-${Date.now()}`;
        res.write(
          `data: ${JSON.stringify({
            id,
            object: "chat.completion.chunk",
            created: Math.floor(Date.now() / 1000),
            model: result.plainIds.model,
            choices: [{ index: 0, delta: { role: "assistant" }, finish_reason: null }],
          })}\n\n`,
        );
        if (result.content) {
          res.write(
            `data: ${JSON.stringify({
              id,
              object: "chat.completion.chunk",
              created: Math.floor(Date.now() / 1000),
              model: result.plainIds.model,
              choices: [{ index: 0, delta: { content: result.content }, finish_reason: null }],
            })}\n\n`,
          );
        }
        res.write(
          `data: ${JSON.stringify({
            id,
            object: "chat.completion.chunk",
            created: Math.floor(Date.now() / 1000),
            model: result.plainIds.model,
            choices: [{ index: 0, delta: {}, finish_reason: "stop" }],
          })}\n\n`,
        );
        res.write("data: [DONE]\n\n");
        return res.end();
      }

      return sendJSON(res, 200, {
        id: `chatcmpl-${Date.now()}`,
        object: "chat.completion",
        created: Math.floor(Date.now() / 1000),
        model: result.plainIds.model,
        choices: [
          {
            index: 0,
            message: Object.assign(
              { role: "assistant", content: result.content || "" },
              result.reasoning ? { reasoning_content: result.reasoning } : {},
            ),
            finish_reason: "stop",
          },
        ],
        usage: { prompt_tokens: 0, completion_tokens: 0, total_tokens: 0 },
      });
    }

    sendJSON(res, 404, { error: { code: "not_found", message: "not found" } });
  } catch (err) {
    sendJSON(res, 500, {
      error: {
        code: "worker_error",
        message: err?.message || String(err),
      },
    });
  }
});

console.error("[worker] warming QoderContext via one-shot qodercli...");
warmupContext()
  .then((meta) => {
    console.error("[worker] warm ready", meta);
    server.listen(port, host, () => {
      console.error(`[worker] listening on http://${host}:${port}`);
    });
  })
  .catch((err) => {
    console.error("[worker] warmup failed", err);
    process.exit(1);
  });
