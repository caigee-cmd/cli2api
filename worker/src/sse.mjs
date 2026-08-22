import { estimateTokens } from "./plaintext.mjs";
import { resolveUsage, usageLooksUseful } from "./usage.mjs";

function scoreUpstreamError(err) {
  if (!err || typeof err !== "object") return 0;
  const code = String(err.code || "").toLowerCase();
  const type = String(err.type || "").toLowerCase();
  const msg = String(err.message || err.localizedMessage || "").toLowerCase();
  let score = 1;
  if (code === "insufficient_quota" || type === "insufficient_quota") score += 100;
  if (msg.includes("exceeded your current quota") || msg.includes("token-limit")) score += 90;
  if (code.includes("429") || msg.includes("response code=429") || msg.includes("too many requests")) score += 40;
  if (msg.includes("unknown sse issue")) score -= 50; // generic wrapper, prefer nested quota error
  if (err.message || err.localizedMessage) score += Math.min(20, String(err.message || err.localizedMessage).length / 20);
  return score;
}

function rememberError(prev, next) {
  if (!next) return prev;
  if (!prev) return next;
  return scoreUpstreamError(next) >= scoreUpstreamError(prev) ? next : prev;
}

function formatUpstreamError(err) {
  const code = err?.code || err?.type || "upstream_error";
  const type = err?.type || "api_error";
  let msg = err?.message || err?.localizedMessage || JSON.stringify(err).slice(0, 300);
  // Aliyun/Qoder often returns insufficient_quota for input token-limit, not zero balance.
  if (code === "insufficient_quota" || type === "insufficient_quota" || /token-limit|exceeded your current quota/i.test(String(msg))) {
    return {
      code: "insufficient_quota",
      type: "insufficient_quota",
      message:
        "Upstream model token/quota limit hit (often input too large or model-specific rate limit), not necessarily zero account balance. Reduce system prompt / history / tools. Detail: " +
        msg,
    };
  }
  if (/unknown sse issue|sse connection failed|response code=429/i.test(String(msg))) {
    msg = `Upstream rate-limited/quota error (429). Original: ${msg}`;
  }
  return { code, type, message: msg };
}


export function parseNestedOpenAIChunks(sseText) {
  let content = "";
  let reasoning = "";
  let error = null;
  const events = [];
  const toolCallsByIndex = new Map();
  let finishReason = null;
  let eventName = "message";
  let usageAcc = null;
  for (const line of String(sseText || "").split(/\n/)) {
    if (line.startsWith("event:")) {
      eventName = line.slice(6).trim() || "message";
      continue;
    }
    if (!line.startsWith("data:")) continue;
    const raw = line.slice(5).trim();
    if (!raw || raw === "[DONE]") continue;
    try {
      if (eventName === "error") {
        const errBody = JSON.parse(raw);
        error = rememberError(error, errBody.error || errBody);
        events.push(errBody);
        eventName = "message";
        continue;
      }
      const outer = JSON.parse(raw);
      const bodyRaw = outer.body;
      if (bodyRaw === "[DONE]") continue;
      const body = typeof bodyRaw === "string" ? JSON.parse(bodyRaw) : bodyRaw;
      events.push(body);
      if (usageLooksUseful(outer) || usageLooksUseful(body)) {
        usageAcc = resolveUsage([usageAcc, outer, body], usageAcc || {});
      }
      if (body?.error && !body?.choices) {
        error = rememberError(error, body.error);
      } else if ((body?.code || body?.msgCode) && !body?.choices) {
        error = rememberError(error, body);
      } else {
        const choice = body?.choices?.[0] || {};
        if (choice.finish_reason) finishReason = choice.finish_reason;
        const delta = choice.delta || {};
        if (delta.content) content += delta.content;
        if (delta.reasoning_content) reasoning += delta.reasoning_content;
        else if (delta.reasoning) reasoning += delta.reasoning;
        const tcs = delta.tool_calls || choice.message?.tool_calls;
        if (Array.isArray(tcs)) {
          for (const tc of tcs) {
            const idx = Number.isInteger(tc.index) ? tc.index : toolCallsByIndex.size;
            const prev = toolCallsByIndex.get(idx) || {
              id: "",
              type: "function",
              function: { name: "", arguments: "" },
            };
            if (tc.id) prev.id = tc.id;
            if (tc.type) prev.type = tc.type;
            if (tc.function?.name) prev.function.name += tc.function.name;
            if (tc.function?.arguments) prev.function.arguments += tc.function.arguments;
            toolCallsByIndex.set(idx, prev);
          }
        }
      }
      eventName = "message";
    } catch {
      eventName = "message";
      // ignore malformed event
    }
  }
  const tool_calls = [...toolCallsByIndex.entries()]
    .sort((a, b) => a[0] - b[0])
    .map(([, v]) => v)
    .filter((v) => v.function?.name);
  return { content, reasoning, events, error, tool_calls, finish_reason: finishReason, usage: usageAcc };
}

export async function readSSEText(res, { maxBytes = 2_000_000 } = {}) {
  const reader = res.body.getReader();
  let out = "";
  while (true) {
    const { value, done } = await reader.read();
    if (done) break;
    out += Buffer.from(value).toString("utf8");
    if (out.length >= maxBytes) break;
  }
  try {
    reader.cancel();
  } catch {}
  return out;
}

function extractDeltaFromOuter(raw) {
  const outer = JSON.parse(raw);
  if (typeof outer?.body === "string") {
    if (outer.body === "[DONE]") return { done: true };
    const body = JSON.parse(outer.body);
    return { body, statusCode: outer.statusCodeValue || outer.statusCode };
  }
  if (outer?.choices) return { body: outer };
  return { body: outer };
}

/**
 * Stream nested Qoder SSE to OpenAI-compatible SSE chunks in realtime.
 * Returns { model, content, reasoning }.
 */
export async function pipeNestedSseToOpenAI(upstreamRes, res, { model = "auto", id, promptTokens = 0, estimatedCompletion = 0 } = {}) {
  const chatId = id || `chatcmpl-${Date.now()}`;
  const created = Math.floor(Date.now() / 1000);
  let content = "";
  let reasoning = "";
  let roleSent = false;
  let buffer = "";
  let rawAll = "";
  let sawError = null;
  let eventCount = 0;
  let sampleEvents = [];
  const toolCallsByIndex = new Map();
  let finishReason = null;
  let usageAcc = null;

  const writeChunk = (delta, finish_reason = null) => {
    res.write(
      `data: ${JSON.stringify({
        id: chatId,
        object: "chat.completion.chunk",
        created,
        model,
        choices: [{ index: 0, delta, finish_reason }],
      })}\n\n`,
    );
    if (typeof res.flush === "function") res.flush();
  };

  if (!roleSent) {
    writeChunk({ role: "assistant" });
    roleSent = true;
  }

  const reader = upstreamRes.body.getReader();
  const decoder = new TextDecoder("utf-8");
  while (true) {
    const { value, done } = await reader.read();
    if (done) break;
    const chunkText = decoder.decode(value, { stream: true });
    rawAll += chunkText;
    buffer += chunkText;
    const parts = buffer.split(/\n/);
    buffer = parts.pop() || "";
    let eventName = "message";
    for (const line of parts) {
      if (line.startsWith("event:")) {
        eventName = line.slice(6).trim() || "message";
        continue;
      }
      if (!line.startsWith("data:")) continue;
      const raw = line.slice(5).trim();
      if (!raw) continue;
      try {
        if (eventName === "error") {
          try {
            const errBody = JSON.parse(raw);
            sawError = rememberError(sawError, errBody.error || errBody);
            eventCount += 1;
            if (sampleEvents.length < 8) sampleEvents.push(errBody);
          } catch {
            sawError = rememberError(sawError, { message: raw });
          }
          eventName = "message";
          continue;
        }
        const extracted = extractDeltaFromOuter(raw);
        if (extracted.done) continue;
        const body = extracted.body;
        if (!body) continue;
        eventCount += 1;
        if (sampleEvents.length < 8) sampleEvents.push(body);
        if (usageLooksUseful(body)) {
          usageAcc = resolveUsage([usageAcc, body], usageAcc || {});
        }
        // Nested OpenAI-style error: { error: { message, type, code } }
        if (body.error && !body.choices) {
          sawError = rememberError(sawError, body.error);
          continue;
        }
        // Flat error payloads sometimes come as {code,message}
        if ((body.code || body.msgCode) && !body.choices) {
          sawError = rememberError(sawError, body);
          continue;
        }
        if (extracted.statusCode && Number(extracted.statusCode) >= 400 && !body.choices) {
          sawError = rememberError(sawError, body.error || body);
          continue;
        }
        const choice = body?.choices?.[0] || {};
        if (choice.finish_reason) finishReason = choice.finish_reason;
        const delta = choice.delta || {};
        const outDelta = {};
        if (delta.content) {
          content += delta.content;
          outDelta.content = delta.content;
        }
        // Pass through structured thinking for OpenAI-compatible clients that understand it.
        if (delta.reasoning_content) {
          reasoning += delta.reasoning_content;
          outDelta.reasoning_content = delta.reasoning_content;
        } else if (delta.reasoning) {
          reasoning += delta.reasoning;
          outDelta.reasoning_content = delta.reasoning;
        }
        if (Array.isArray(delta.tool_calls) && delta.tool_calls.length) {
          outDelta.tool_calls = delta.tool_calls;
          for (const tc of delta.tool_calls) {
            const idx = Number.isInteger(tc.index) ? tc.index : toolCallsByIndex.size;
            const prev = toolCallsByIndex.get(idx) || {
              id: "",
              type: "function",
              function: { name: "", arguments: "" },
            };
            if (tc.id) prev.id = tc.id;
            if (tc.type) prev.type = tc.type;
            if (tc.function?.name) prev.function.name += tc.function.name;
            if (tc.function?.arguments) prev.function.arguments += tc.function.arguments;
            toolCallsByIndex.set(idx, prev);
          }
        }
        if (Object.keys(outDelta).length) writeChunk(outDelta);
        eventName = "message";
      } catch {
        eventName = "message";
        // ignore parse errors for non-data frames
      }
    }
  }

  const usage = resolveUsage(usageAcc, {
    prompt_tokens: promptTokens,
    completion_tokens: estimatedCompletion || estimateTokens(content + reasoning),
    source: "estimate",
  });
  const promptTokensOut = usage.prompt_tokens;
  const completionTokens = usage.completion_tokens;
  if (sawError && !content && !reasoning) {
    const formatted = formatUpstreamError(sawError);
    res.write(`data: ${JSON.stringify({ error: { message: formatted.message, type: formatted.type, code: formatted.code } })}\n\n`);
    res.write("data: [DONE]\n\n");
    throw new Error(`${formatted.code}: ${formatted.message}`);
  }
  const tool_calls = [...toolCallsByIndex.entries()]
    .sort((a, b) => a[0] - b[0])
    .map(([, v]) => v)
    .filter((v) => v.function?.name);
  if (!content && !reasoning && tool_calls.length === 0) {
    console.error("[sse] empty upstream", {
      model,
      promptTokens,
      sawError: Boolean(sawError),
      eventCount,
    });
    const msg = "upstream returned empty content (possible context overflow / silent reject)";
    res.write(`data: ${JSON.stringify({ error: { message: msg, type: "api_error", code: "empty_upstream" } })}\n\n`);
    res.write("data: [DONE]\n\n");
    throw new Error(msg);
  }
  const finalReason = finishReason || (tool_calls.length ? "tool_calls" : "stop");
  writeChunk({}, finalReason);
  // Final usage chunk (OpenAI stream_options.include_usage style) so billing can record tokens.
  res.write(
    `data: ${JSON.stringify({
      id: chatId,
      object: "chat.completion.chunk",
      created,
      model,
      choices: [],
      usage: {
        prompt_tokens: promptTokensOut,
        completion_tokens: completionTokens,
        total_tokens: usage.total_tokens,
        source: usage.source,
        ...(usage.credits != null ? { credits: usage.credits } : {}),
      },
    })}\n\n`,
  );
  res.write("data: [DONE]\n\n");
  return {
    model,
    content,
    reasoning,
    id: chatId,
    promptTokens: promptTokensOut,
    completionTokens,
    usage,
    tool_calls,
    finish_reason: finalReason,
  };
}
