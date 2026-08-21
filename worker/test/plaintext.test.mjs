import assert from "node:assert/strict";
import { test } from "node:test";
import {
  buildPlainChatBody,
  mapModel,
  wantsReasoning,
  estimateTokens,
} from "../src/plaintext.mjs";

test("maps known display names to upstream keys", () => {
  assert.equal(mapModel("qwen3.7-plus"), "qmodel");
  assert.equal(mapModel("DeepSeek-V4-Pro"), "dmodel");
  assert.equal(mapModel("auto"), "auto");
});

test("detects reasoning flags from OpenAI-style fields", () => {
  assert.equal(wantsReasoning({ enable_thinking: true }), true);
  assert.equal(wantsReasoning({ reasoning_effort: "high" }), true);
  assert.equal(wantsReasoning({ reasoning_effort: "none" }), false);
  assert.equal(wantsReasoning({}), false);
});

test("does not inherit capture-template system or tools", () => {
  const body = buildPlainChatBody({
    messages: [
      { role: "system", content: "You are GLM." },
      { role: "user", content: "hi" },
    ],
    model: "qwen3.7-plus",
    template: {
      system: "HUGE QODER AGENT SYSTEM",
      tools: [{ type: "function", function: { name: "internal_tool", parameters: {} } }],
      model_config: { key: "auto", source: "system" },
    },
    tools: [
      {
        type: "function",
        function: { name: "exec_command", description: "run", parameters: { type: "object" } },
      },
    ],
  });
  assert.equal(body.system, "You are GLM.");
  assert.equal(body.messages[0].role, "system");
  assert.equal(body.messages[0].content, "You are GLM.");
  assert.equal(body.model_config.key, "qmodel");
  assert.equal(body.tools.length, 1);
  assert.equal(body.tools[0].function.name, "exec_command");
  assert.notEqual(body.request_id, body.session_id);
});

test("estimates CJK heavier than ascii", () => {
  assert.ok(estimateTokens("你好世界") >= 4);
  assert.ok(estimateTokens("abcd") <= 2);
});
