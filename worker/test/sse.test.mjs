import assert from "node:assert/strict";
import { test } from "node:test";
import { parseNestedOpenAIChunks, pipeNestedSseToOpenAI } from "../src/sse.mjs";

test("parses nested OpenAI content and reasoning deltas", () => {
  const sse = [
    `data: ${JSON.stringify({ body: JSON.stringify({ choices: [{ delta: { content: "Hel" } }] }) })}`,
    `data: ${JSON.stringify({ body: JSON.stringify({ choices: [{ delta: { content: "lo" } }] }) })}`,
    `data: ${JSON.stringify({ body: JSON.stringify({ choices: [{ delta: { reasoning_content: "think" } }] }) })}`,
    "data: [DONE]",
  ].join("\n");
  const parsed = parseNestedOpenAIChunks(sse);
  assert.equal(parsed.content, "Hello");
  assert.equal(parsed.reasoning, "think");
});

test("accumulates streamed tool calls", () => {
  const sse = [
    `data: ${JSON.stringify({
      body: JSON.stringify({
        choices: [{ delta: { tool_calls: [{ index: 0, id: "call_1", function: { name: "exec", arguments: "{" } }] } }],
      }),
    })}`,
    `data: ${JSON.stringify({
      body: JSON.stringify({
        choices: [{ delta: { tool_calls: [{ index: 0, function: { arguments: "}" } }] }, finish_reason: "tool_calls" }],
      }),
    })}`,
  ].join("\n");
  const parsed = parseNestedOpenAIChunks(sse);
  assert.equal(parsed.tool_calls.length, 1);
  assert.equal(parsed.tool_calls[0].id, "call_1");
  assert.equal(parsed.tool_calls[0].function.name, "exec");
  assert.equal(parsed.tool_calls[0].function.arguments, "{}");
  assert.equal(parsed.finish_reason, "tool_calls");
});

test("prefers concrete quota errors over empty content", () => {
  const sse = [
    "event: error",
    `data: ${JSON.stringify({ error: { code: "insufficient_quota", message: "token-limit" } })}`,
  ].join("\n");
  const parsed = parseNestedOpenAIChunks(sse);
  assert.equal(parsed.error?.code, "insufficient_quota");
});

test("does not commit an SSE response when upstream fails before the first event", async () => {
  const upstream = {
    body: new ReadableStream({
      start(controller) {
        controller.error(new Error("socket closed"));
      },
    }),
  };
  let output = "";
  const res = {
    write(chunk) {
      output += chunk;
    },
  };

  await assert.rejects(
    pipeNestedSseToOpenAI(upstream, res, { model: "minimax-m3" }),
    /upstream_stream_interrupted: socket closed/,
  );
  assert.equal(output, "");
});


test("surfaces a provider SSE error before committing output", async () => {
  const payload = JSON.stringify({ error: { code: "provider_error", message: "upstream rejected" } });
  const upstream = {
    body: new ReadableStream({
      start(controller) {
        controller.enqueue(new TextEncoder().encode("event: error\ndata: " + payload + "\n\n"));
        controller.close();
      },
    }),
  };
  let output = "";
  const res = { write(chunk) { output += chunk; } };

  await assert.rejects(
    pipeNestedSseToOpenAI(upstream, res, { model: "minimax-m3" }),
    /provider_error:.*upstream rejected/,
  );
  assert.equal(output, "");
});

test("finishes a valid stream that ends without upstream DONE", async () => {
  const upstream = {
    body: new ReadableStream({
      start(controller) {
        const body = JSON.stringify({ body: JSON.stringify({ choices: [{ delta: { content: "partial" } }] }) });
        controller.enqueue(new TextEncoder().encode("data: " + body + "\n\n"));
        controller.close();
      },
    }),
  };
  const res = { write() {} };

  const result = await pipeNestedSseToOpenAI(upstream, res, { model: "minimax-m3" });
  assert.equal(result.content, "partial");
});
