import assert from "node:assert/strict";
import { test } from "node:test";
import { extractUsage, resolveUsage } from "../src/usage.mjs";
import { parseNestedOpenAIChunks } from "../src/sse.mjs";

test("prefers nested OpenAI usage over estimates", () => {
  const extracted = extractUsage({
    usage: { prompt_tokens: 11, completion_tokens: 7, total_tokens: 18 },
  });
  assert.equal(extracted.prompt_tokens, 11);
  assert.equal(extracted.completion_tokens, 7);
  assert.equal(extracted.source, "upstream");
});

test("reads llm_model_result and credits", () => {
  const extracted = extractUsage({
    llm_model_result: { input_tokens: 40, output_tokens: 12, credits: 1.5 },
  });
  assert.equal(extracted.prompt_tokens, 40);
  assert.equal(extracted.completion_tokens, 12);
  assert.equal(extracted.credits, 1.5);
});

test("falls back to estimate when upstream is silent", () => {
  const usage = resolveUsage({}, { prompt_tokens: 9, completion_tokens: 3, source: "estimate" });
  assert.equal(usage.prompt_tokens, 9);
  assert.equal(usage.completion_tokens, 3);
  assert.equal(usage.source, "estimate");
});

test("parseNestedOpenAIChunks captures usage from nested body", () => {
  const sse = [
    `data: ${JSON.stringify({ body: JSON.stringify({ choices: [{ delta: { content: "Hi" } }] }) })}`,
    `data: ${JSON.stringify({
      body: JSON.stringify({
        choices: [{ finish_reason: "stop" }],
        usage: { prompt_tokens: 21, completion_tokens: 2, total_tokens: 23 },
      }),
    })}`,
  ].join("\n");
  const parsed = parseNestedOpenAIChunks(sse);
  assert.equal(parsed.content, "Hi");
  assert.equal(parsed.usage.prompt_tokens, 21);
  assert.equal(parsed.usage.completion_tokens, 2);
  assert.equal(parsed.usage.source, "upstream");
});
