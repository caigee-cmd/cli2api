import assert from "node:assert/strict";
import { test } from "node:test";
import {
  buildPlainChatBody,
  canonicalModelID,
  mapModel,
  wantsReasoning,
  estimateTokens,
} from "../src/plaintext.mjs";

test("maps known display names to upstream keys", () => {
  assert.equal(mapModel("qwen3.7-plus"), "qmodel");
  assert.equal(mapModel("DeepSeek-V4-Pro"), "dmodel");
  assert.equal(mapModel("minimax-m3"), "mmodel");
  assert.equal(mapModel("MiniMax-M3"), "mmodel");
  assert.equal(mapModel("auto"), "auto");
});

test("normalizes public model ids without exposing Qoder keys", () => {
  assert.equal(canonicalModelID("MiniMax-M3"), "minimax-m3");
  assert.equal(canonicalModelID("mmodel"), "minimax-m3");
  assert.equal(canonicalModelID("Qwen3.7-Plus"), "qwen3.7-plus");
  assert.equal(canonicalModelID("qmodel"), "qwen3.7-plus");
  assert.equal(canonicalModelID("GLM-5.2"), "glm-5.2");
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

test("forwards Qoder reasoning and context parameters", () => {
  const body = buildPlainChatBody({
    messages: [{ role: "user", content: "hi" }],
    model: "minimax-m3",
    reasoningEffort: "high",
    enableThinking: true,
    reasoningBudgetTokens: 16384,
    contextLength: 500000,
    maxInputTokens: 1000000,
  });
  assert.equal(body.model_config.key, "mmodel");
  assert.equal(body.model_config.max_input_tokens, 1000000);
  assert.equal(body.parameters.reasoning_effort, "high");
  assert.equal(body.parameters.enable_thinking, true);
  assert.equal(body.parameters.reasoning_budget_tokens, 16384);
  assert.equal(body.parameters.context_length, 500000);
});

test("does not inject thinking controls into ordinary requests", () => {
  const body = buildPlainChatBody({
    messages: [{ role: "user", content: "hi" }],
    model: "qwen3.7-plus",
  });
  assert.equal("enable_thinking" in body.parameters, false);
  assert.equal("reasoning_effort" in body.parameters, false);
});

test("uses the Qoder catalog input limit for MiniMax-M3", () => {
  const body = buildPlainChatBody({
    messages: [{ role: "user", content: "hi" }],
    model: "MiniMax-M3",
    reasoningEffort: "medium",
  });
  assert.equal(body.model_config.max_input_tokens, 1000000);
  assert.equal(body.parameters.enable_thinking, true);
});

test("estimates CJK heavier than ascii", () => {
  assert.ok(estimateTokens("你好世界") >= 4);
  assert.ok(estimateTokens("abcd") <= 2);
});
