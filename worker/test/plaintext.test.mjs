import assert from "node:assert/strict";
import { test } from "node:test";
import {
  buildPlainChatBody,
  canonicalModelID,
  wantsReasoning,
  estimateTokens,
  normalizeMessagesForUpstream,
  diagnoseOpenAIToolHistory,
  diagnoseToolResults,
  filterUnknownToolHistory,
  normalizeToolResultContent,
  summarizeNormalizedToolHistory,
} from "../src/plaintext.mjs";

test("does not statically remap requested model ids", () => {
  const body = buildPlainChatBody({ messages: [{ role: "user", content: "hi" }], model: "deepseek-v4-flash" });
  assert.equal(body.model_config.key, "deepseek-v4-flash");
  assert.equal(body.model_config.display_name, "deepseek-v4-flash");
});

test("uses the dynamic catalog route in the plaintext request", () => {
  const body = buildPlainChatBody({
    messages: [{ role: "user", content: "hi" }],
    model: "deepseek-v4-flash",
    modelConfig: { key: "dfmodel", display_name: "DeepSeek-V4-Flash" },
  });
  assert.equal(body.model_config.key, "dfmodel");
  assert.equal(body.model_config.display_name, "DeepSeek-V4-Flash");
});

test("normalizes model ids only for stable public/settings keys", () => {
  assert.equal(canonicalModelID("MiniMax-M3"), "minimax-m3");
  assert.equal(canonicalModelID("Qwen3.7-Plus"), "qwen3.7-plus");
  assert.equal(canonicalModelID("qmodel"), "qmodel");
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
  assert.equal(body.model_config.key, "qwen3.7-plus");
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
  assert.equal(body.model_config.key, "minimax-m3");
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
    modelConfig: { key: "mmodel", max_input_tokens: 1000000 },
    reasoningEffort: "medium",
  });
  assert.equal(body.model_config.max_input_tokens, 1000000);
  assert.equal(body.parameters.enable_thinking, true);
});

test("normalizes OpenAI tool history and repairs missing tool ids", () => {
  const messages = normalizeMessagesForUpstream([
    { role: "user", content: "inspect the repo" },
    {
      role: "assistant",
      content: null,
      tool_calls: [
        { type: "function", function: { name: "read_file", arguments: '{"path":"a"}' } },
        { type: "function", function: { name: "list_files", arguments: { path: "." } } },
      ],
    },
    { role: "tool", content: "file contents" },
    { role: "tool", content: "file list" },
  ]);

  const calls = messages[1].tool_calls;
  assert.equal(calls.length, 2);
  assert.notEqual(calls[0].id, calls[1].id);
  assert.equal(calls[0].function.name, "read_file");
  assert.equal(calls[1].function.arguments, '{"path":"."}');
  assert.equal(messages[2].tool_call_id, calls[0].id);
  assert.equal("name" in messages[2], false);
  assert.equal(messages[3].tool_call_id, calls[1].id);
  assert.equal("name" in messages[3], false);
});

test("filters unknown historical tool calls and matching results", () => {
  const result = filterUnknownToolHistory(
    [
      { role: "assistant", content: null, tool_calls: [
        { id: "call_task", function: { name: "TaskCreate", arguments: "{}" } },
        { id: "call_read", function: { name: "Read", arguments: "{}" } },
      ] },
      { role: "tool", tool_call_id: "call_task", content: "task result" },
      { role: "tool", tool_call_id: "call_read", content: "read result" },
    ],
    [{ type: "function", function: { name: "Read", parameters: {} } }],
  );
  assert.deepEqual(result.droppedToolCallIds, ["call_task"]);
  assert.deepEqual(result.droppedToolNames, ["TaskCreate"]);
  assert.equal(result.messages.length, 2);
  assert.equal(result.messages[0].tool_calls.length, 1);
  assert.equal(result.messages[0].tool_calls[0].function.name, "Read");
  assert.equal(result.messages[1].tool_call_id, "call_read");
});

test("preserves history when no tool definitions are provided", () => {
  const messages = [{ role: "assistant", tool_calls: [{ id: "call_task", function: { name: "TaskCreate", arguments: "{}" } }] }];
  const result = filterUnknownToolHistory(messages, []);
  assert.deepEqual(result.messages, messages);
  assert.deepEqual(result.droppedToolNames, []);
});

test("keeps distinct existing tool ids across multiple assistant turns", () => {
  const messages = normalizeMessagesForUpstream([
    {
      role: "assistant",
      content: null,
      tool_calls: [{ id: "call_a", type: "function", function: { name: "one", arguments: "{}" } }],
    },
    { role: "tool", tool_call_id: "call_a", content: "one result" },
    {
      role: "assistant",
      content: null,
      tool_calls: [{ id: "call_b", type: "function", function: { name: "two", arguments: "{}" } }],
    },
    { role: "tool", tool_call_id: "call_b", content: "two result" },
  ]);
  assert.equal(messages[1].tool_call_id, "call_a");
  assert.equal(messages[3].tool_call_id, "call_b");
});


test("orders parallel tool results and drops orphan results", () => {
  const messages = normalizeMessagesForUpstream([
    {
      role: "assistant",
      content: null,
      tool_calls: [
        { id: "call_a", function: { name: "Read", arguments: "{}" } },
        { id: "call_b", function: { name: "Bash", arguments: "{}" } },
      ],
    },
    { role: "tool", tool_call_id: "orphan", content: "discard me" },
    { role: "tool", tool_call_id: "call_b", content: "b" },
    { role: "tool", tool_call_id: "call_a", content: "a" },
  ]);
  assert.deepEqual(messages.slice(1).map((message) => [message.tool_call_id, message.content]), [
    ["call_a", "a"],
    ["call_b", "b"],
  ]);
});


test("summarizes normalized tool history without content", () => {
  const summary = summarizeNormalizedToolHistory([
    { role: "assistant", content: null, tool_calls: [
      { id: "call_a", function: { name: "Read", arguments: "{\"path\":\"a\"}" } },
    ] },
    { role: "tool", tool_call_id: "call_a", content: "secret output" },
  ]);
  assert.equal(summary[0].toolCalls[0].id, "call_a");
  assert.equal(summary[0].toolCalls[0].name, "Read");
  assert.equal(summary[1].toolCallId, "call_a");
  assert.equal(summary[1].contentLength, 13);
  assert.equal(JSON.stringify(summary).includes("secret output"), false);
});

test("diagnoses malformed OpenAI tool history without logging content", () => {
  const diagnostics = diagnoseOpenAIToolHistory(
    [
      { role: "assistant", tool_calls: [
        { id: "call_1", function: { name: "read_file", arguments: "{bad" } },
        { id: "call_1", function: { name: "missing_tool", arguments: "{}" } },
      ] },
      { role: "user", content: "interrupted" },
      { role: "tool", tool_call_id: "orphan", name: "read_file", content: "secret output" },
    ],
    [{ type: "function", function: { name: "read_file", parameters: {} } }],
  );
  assert.equal(diagnostics.valid, false);
  assert.equal(diagnostics.assistantToolCallCount, 2);
  assert.equal(diagnostics.toolResultCount, 1);
  assert.equal(diagnostics.duplicateToolCallIds.length, 1);
  assert.equal(diagnostics.invalidArguments[0].reason, "invalid_json");
  assert.equal(diagnostics.undefinedToolCalls[0].name, "missing_tool");
  assert.equal(diagnostics.orphanToolResultIds[0].id, "orphan");
  assert.equal(diagnostics.sequenceBreaks[0].role, "user");
  assert.equal(JSON.stringify(diagnostics).includes("secret output"), false);
});

test("diagnoses tool result metadata without recording content", () => {
  const diagnostics = diagnoseToolResults([
    { role: "assistant", tool_calls: [
      { id: "call_read", function: { name: "Read", arguments: "{}" } },
      { id: "call_bash", function: { name: "Bash", arguments: "{}" } },
    ] },
    { role: "tool", tool_call_id: "call_read", content: "file contents\n" },
    { role: "tool", tool_call_id: "call_bash", content: "{\"ok\":true}" },
  ]);
  assert.equal(diagnostics.length, 2);
  assert.equal(diagnostics[0].toolName, "Read");
  assert.equal(diagnostics[0].contentLength, 14);
  assert.equal(diagnostics[0].parallelBatchSize, 2);
  assert.equal(diagnostics[0].hasControlChars, false);
  assert.equal(diagnostics[0].jsonValid, false);
  assert.equal(diagnostics[1].toolName, "Bash");
  assert.equal(diagnostics[1].jsonValid, true);
  assert.equal(JSON.stringify(diagnostics).includes("file contents"), false);
});


test("normalizes tool results like Qoder CLI", () => {
  assert.equal(normalizeToolResultContent("plain output"), "plain output");
  assert.equal(normalizeToolResultContent({ ok: true }), '{"ok":true}');
  assert.equal(normalizeToolResultContent(null), "(no content)");
  assert.equal(normalizeToolResultContent("denied", true), "Error: denied");
  assert.equal(
    normalizeToolResultContent([
      { type: "text", text: "first" },
      { type: "resource", resource: { text: "second" } },
      { type: "image", mime_type: "image/png" },
    ]),
    "first\nsecond\n[Image: image/png]",
  );
});

test("normalizes tool result content while preserving call ids", () => {
  const messages = normalizeMessagesForUpstream([
    {
      role: "assistant",
      content: null,
      tool_calls: [{ id: "call_a", function: { name: "Read", arguments: "{}" } }],
    },
    { role: "tool", tool_call_id: "call_a", content: { output: "ok" } },
  ]);
  assert.equal(messages[0].content, "");
  assert.equal(messages[1].tool_call_id, "call_a");
  assert.equal(messages[1].content, '{"output":"ok"}');
});

test("estimates CJK heavier than ascii", () => {
  assert.ok(estimateTokens("你好世界") >= 4);
  assert.ok(estimateTokens("abcd") <= 2);
});

test("uses a dynamic catalog route without static aliases", () => {
  const body = buildPlainChatBody({
    messages: [{ role: "user", content: "hello" }],
    model: "glm-5.3",
    modelConfig: {
      key: "catalog-glm-key",
      display_name: "GLM-5.3",
      source: "catalog",
      max_input_tokens: 240000,
      is_reasoning: true,
    },
  });
  assert.equal(body.model_config.key, "catalog-glm-key");
  assert.equal(body.model_config.display_name, "GLM-5.3");
  assert.equal(body.model_config.source, "catalog");
  assert.equal(body.model_config.max_input_tokens, 240000);
  assert.equal(body.model_config.is_reasoning, true);
});
