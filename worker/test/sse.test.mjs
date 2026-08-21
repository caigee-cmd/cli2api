import assert from "node:assert/strict";
import { test } from "node:test";
import { parseNestedOpenAIChunks } from "../src/sse.mjs";

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
