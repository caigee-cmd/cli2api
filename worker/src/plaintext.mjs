import crypto from "node:crypto";

// Display-name / alias -> upstream model_config.key
// Confirmed/observed:
// - qmodel = Qwen3.7-Plus
// - auto = Auto router
// Other keys follow Qoder native short ids used by CLI/catalog.
const MODEL_ALIAS = {
  auto: "auto",
  Auto: "auto",

  // Qwen
  "qwen3.7-plus": "qmodel",
  "qwen3.7-Plus": "qmodel",
  "Qwen3.7-Plus": "qmodel",
  qwen3_7_plus: "qmodel",
  qmodel: "qmodel",
  "qwen3.7-max": "qwen3.7-max",
  "Qwen3.7-Max": "qwen3.7-max",
  "qwen3.8-max": "qwen3.8-max",
  "Qwen3.8-Max": "qwen3.8-max",
  "qwen3.8-max-preview": "qwen3.8-max",

  // GLM
  // Default: serve glm-5.2 via Qwen upstream key to avoid frequent Aliyun glm token-limit/429.
  // Override with env QODER_GLM52_UPSTREAM_KEY=glm-5.2 if you really want native GLM.
  "glm-5.2": "qmodel",
  "GLM-5.2": "qmodel",
  "glm-5.3": "glm-5.3",
  "GLM-5.3": "glm-5.3",
  "glm-5.1": "gm51model",
  gm51model: "gm51model",

  // DeepSeek
  "deepseek-v4-pro": "dmodel",
  "DeepSeek-V4-Pro": "dmodel",
  dmodel: "dmodel",
  "deepseek-v4-flash": "dfmodel",
  "DeepSeek-V4-Flash": "dfmodel",
  dfmodel: "dfmodel",

  // Kimi / MiniMax
  "kimi-k2.7-code": "kmodel",
  "Kimi-K2.7-Code": "kmodel",
  "kimi-k3": "kimi-k3",
  "Kimi-K3": "kimi-k3",
  kmodel: "kmodel",
  "minimax-m2.7": "mmodel",
  "minimax-m3": "mmodel",
  "MiniMax-M3": "mmodel",
  mmodel: "mmodel",

  // tiers
  ultimate: "ultimate",
  performance: "performance",
  efficient: "efficient",
  lite: "lite",
  cantus: "cantus",
};

const DISPLAY = {
  auto: "Auto",
  qmodel: "Qwen3.7-Plus",
  "qwen3.7-max": "Qwen3.7-Max",
  "qwen3.8-max": "Qwen3.8-Max",
  "glm-5.2": "GLM-5.2",
  "glm-5.3": "GLM-5.3",
  gm51model: "GLM-5.1",
  dmodel: "DeepSeek-V4-Pro",
  dfmodel: "DeepSeek-V4-Flash",
  kmodel: "Kimi-K2.7-Code",
  "kimi-k3": "Kimi-K3",
  mmodel: "MiniMax-M3",
  "minimax-m3": "MiniMax-M3",
  ultimate: "Ultimate",
  performance: "Performance",
  efficient: "Efficient",
  lite: "Lite",
  cantus: "Cantus",
};

const PUBLIC_MODEL_ID = {
  qmodel: "qwen3.7-plus",
  dmodel: "deepseek-v4-pro",
  dfmodel: "deepseek-v4-flash",
  kmodel: "kimi-k2.7-code",
  mmodel: "minimax-m3",
  gm51model: "glm-5.1",
};

export function canonicalModelID(model) {
  if (!model) return "auto";
  const normalized = String(model).trim().toLowerCase().replace(/[\s_]+/g, "-");
  return PUBLIC_MODEL_ID[normalized] || normalized;
}

export function mapModel(model) {
  if (!model) return "auto";
  const glmOverride = String(process.env.QODER_GLM52_UPSTREAM_KEY || "").trim();
  const raw = String(model);
  const lower = raw.toLowerCase();
  if ((raw === "glm-5.2" || raw === "GLM-5.2" || lower === "glm-5.2") && glmOverride) {
    return glmOverride;
  }
  if (MODEL_ALIAS[model]) return MODEL_ALIAS[model];
  if (MODEL_ALIAS[lower]) return MODEL_ALIAS[lower];
  // normalize underscores/spaces
  const norm = lower.replace(/[\s_]+/g, "-");
  if (MODEL_ALIAS[norm]) return MODEL_ALIAS[norm];
  return model;
}

export function displayModel(key) {
  return DISPLAY[key] || key;
}

function contentToString(content) {
  if (typeof content === "string") return content;
  if (Array.isArray(content)) {
    return content
      .map((part) => {
        if (typeof part === "string") return part;
        if (part && typeof part === "object") return part.text || part.content || "";
        return "";
      })
      .filter(Boolean)
      .join("\n");
  }
  if (content == null) return "";
  return String(content);
}


// Rough OpenAI-style token estimate: CJK chars ~1 token, other chars ~1 token/4.
export function estimateTokens(text) {
  const s = String(text || "");
  if (!s) return 0;
  let cjk = 0;
  let other = 0;
  for (const ch of s) {
    if (/[\u4e00-\u9fff\u3400-\u4dbf\uf900-\ufaff\u3040-\u30ff\uac00-\ud7af]/.test(ch)) cjk += 1;
    else other += 1;
  }
  return Math.max(1, Math.round(cjk + other / 4));
}

export function estimatePromptTokens(messages = []) {
  let sum = 0;
  for (const m of messages) {
    const c = contentToString(m?.content);
    sum += estimateTokens(c) + 4; // per-message overhead
  }
  return sum;
}

export function wantsReasoning(reqBody = {}) {
  if (reqBody.is_reasoning === true) return true;
  if (reqBody.enable_thinking === true) return true;
  if (reqBody.enable_reasoning === true) return true;

  const thinking = reqBody.thinking;
  if (thinking === true) return true;
  if (typeof thinking === "string") {
    const t = thinking.toLowerCase();
    if (t && t !== "disabled" && t !== "none" && t !== "off" && t !== "false") return true;
  }
  if (thinking && typeof thinking === "object") {
    const typ = String(thinking.type || thinking.mode || "").toLowerCase();
    if (typ && typ !== "disabled" && typ !== "none" && typ !== "off") return true;
    if (thinking.enabled === true) return true;
  }

  const effort = reqBody.reasoning_effort;
  if (typeof effort === "string") {
    const e = effort.toLowerCase();
    if (e && e !== "none" && e !== "off" && e !== "disabled") return true;
  }
  return false;
}

function normalizeTools(tools) {
  if (!Array.isArray(tools)) return [];
  return tools
    .map((t) => {
      if (!t || typeof t !== "object") return null;
      // Already OpenAI function-tool shape
      if (t.type === "function" && t.function && typeof t.function === "object") {
        return {
          type: "function",
          function: {
            name: String(t.function.name || ""),
            description: t.function.description || "",
            parameters: t.function.parameters || { type: "object", properties: {} },
          },
        };
      }
      // Bare function definition
      if (t.name && (t.parameters || t.description != null)) {
        return {
          type: "function",
          function: {
            name: String(t.name),
            description: t.description || "",
            parameters: t.parameters || { type: "object", properties: {} },
          },
        };
      }
      return null;
    })
    .filter((t) => t && t.function?.name);
}

export function filterUnknownToolHistory(messages = [], tools = []) {
  const normalizedTools = normalizeTools(tools);
  const definedNames = new Set(normalizedTools.map((tool) => tool.function.name));
  const droppedToolCallIds = new Set();
  const droppedToolNames = new Set();
  const filtered = [];

  if (!definedNames.size) {
    return { messages: Array.isArray(messages) ? messages : [], droppedToolCallIds: [], droppedToolNames: [] };
  }

  for (const message of Array.isArray(messages) ? messages : []) {
    if (message?.role === "assistant" && Array.isArray(message.tool_calls)) {
      const retainedCalls = [];
      for (const toolCall of message.tool_calls) {
        const fn = toolCall?.function && typeof toolCall.function === "object" ? toolCall.function : {};
        const name = String(fn.name || toolCall?.name || "").trim();
        if (name && definedNames.has(name)) {
          retainedCalls.push(toolCall);
          continue;
        }
        if (typeof toolCall?.id === "string" && toolCall.id.trim()) {
          droppedToolCallIds.add(toolCall.id.trim());
        }
        if (name) droppedToolNames.add(name);
      }
      if (!retainedCalls.length) {
        if (contentToString(message.content)) filtered.push({ ...message, tool_calls: undefined });
        continue;
      }
      filtered.push({ ...message, tool_calls: retainedCalls });
      continue;
    }
    if (message?.role === "tool" && typeof message.tool_call_id === "string" && droppedToolCallIds.has(message.tool_call_id.trim())) {
      continue;
    }
    filtered.push(message);
  }

  return {
    messages: filtered,
    droppedToolCallIds: [...droppedToolCallIds],
    droppedToolNames: [...droppedToolNames],
  };
}

function normalizeToolResultContent(content, isError = false) {
  let normalized;
  if (typeof content === "string") {
    normalized = content;
  } else if (Array.isArray(content)) {
    normalized = content
      .map((part) => {
        if (typeof part === "string") return part;
        if (!part || typeof part !== "object") return "";
        if (part.type === "text" && typeof part.text === "string") return part.text;
        if (part.type === "image" || part.type === "image_url") {
          const mime = part.mime_type || part.mimeType || part.image_url?.detail || "unknown";
          return "[Image: " + mime + "]";
        }
        if (part.type === "resource_link") {
          return "[Link to " + (part.title || part.name || part.uri || "resource") + "]";
        }
        if (part.type === "resource") {
          return typeof part.resource?.text === "string"
            ? part.resource.text
            : "[Embedded Resource: " + (part.resource?.mime_type || part.resource?.mimeType || "unknown") + "]";
        }
        return JSON.stringify(part);
      })
      .filter(Boolean)
      .join("\n");
  } else if (content == null) {
    normalized = "(no content)";
  } else {
    normalized = JSON.stringify(content);
  }

  if (!normalized) normalized = "(no content)";
  if (isError && !/^Error:\s/u.test(normalized)) normalized = "Error: " + normalized;
  return normalized;
}

function normalizeToolCall(toolCall, messageIndex, callIndex, usedIds, sourceIds) {
  const functionPart = toolCall?.function && typeof toolCall.function === "object"
    ? toolCall.function
    : {};
  const sourceId = typeof toolCall?.id === "string" ? toolCall.id.trim() : "";
  let id = sourceId;
  if (!id || usedIds.has(id)) {
    id = `qoder_call_${messageIndex}_${callIndex}`;
    while (usedIds.has(id)) id += "_retry";
  }
  usedIds.add(id);
  if (sourceId && !sourceIds.has(sourceId)) sourceIds.set(sourceId, id);
  return {
    id,
    type: "function",
    function: {
      name: String(functionPart.name || toolCall?.name || ""),
      arguments: typeof functionPart.arguments === "string"
        ? functionPart.arguments
        : JSON.stringify(functionPart.arguments ?? {}),
    },
  };
}

function normalizeMessagesForUpstream(messages = []) {
  const usedIds = new Set();
  const sourceIds = new Map();
  const callsById = new Map();
  const normalized = [];
  let currentBatch = [];
  let consumedBatchIds = new Set();
  let pendingToolResults = [];

  const flushToolResults = () => {
    if (!pendingToolResults.length) return;
    const order = new Map(currentBatch.map((id, index) => [id, index]));
    pendingToolResults.sort((left, right) => {
      const leftOrder = order.has(left.toolCallId) ? order.get(left.toolCallId) : Number.MAX_SAFE_INTEGER;
      const rightOrder = order.has(right.toolCallId) ? order.get(right.toolCallId) : Number.MAX_SAFE_INTEGER;
      return leftOrder - rightOrder;
    });
    for (const result of pendingToolResults) {
      const belongsToCurrentBatch = currentBatch.length > 0
        ? order.has(result.toolCallId)
        : callsById.has(result.toolCallId);
      if (belongsToCurrentBatch) normalized.push(result.message);
    }
    pendingToolResults = [];
  };

  for (const [messageIndex, message] of (Array.isArray(messages) ? messages : []).entries()) {
    const role = message?.role;
    if (role === "assistant") {
      flushToolResults();
      consumedBatchIds = new Set();
      const out = { role, content: contentToString(message?.content) };
      if (Array.isArray(message?.tool_calls) && message.tool_calls.length) {
        out.tool_calls = message.tool_calls.map((toolCall, callIndex) => {
          const normalizedCall = normalizeToolCall(toolCall, messageIndex, callIndex, usedIds, sourceIds);
          callsById.set(normalizedCall.id, normalizedCall);
          return normalizedCall;
        });
        currentBatch = out.tool_calls.map((toolCall) => toolCall.id);
        if (!out.content) out.content = "";
      } else {
        currentBatch = [];
      }
      normalized.push(out);
      continue;
    }

    if (role === "tool") {
      const requestedId = typeof message?.tool_call_id === "string" ? message.tool_call_id.trim() : "";
      let toolCallId = sourceIds.get(requestedId) || requestedId;
      if (!toolCallId) {
        toolCallId = currentBatch.find((id) => !consumedBatchIds.has(id)) || "";
      }
      if (!toolCallId || !callsById.has(toolCallId)) continue;
      consumedBatchIds.add(toolCallId);
      const out = {
        role,
        content: normalizeToolResultContent(message?.content, message?.is_error === true),
        tool_call_id: toolCallId,
      };
      if (message?.name) out.name = String(message.name);
      pendingToolResults.push({ message: out, toolCallId });
      continue;
    }

    flushToolResults();
    currentBatch = [];
    consumedBatchIds = new Set();
    normalized.push({ role, content: contentToString(message?.content) });
  }

  flushToolResults();
  return normalized;
}
export { normalizeMessagesForUpstream, normalizeToolResultContent };

export function summarizeNormalizedToolHistory(messages = []) {
  return (Array.isArray(messages) ? messages : []).slice(0, 128).map((message, index) => {
    const summary = {
      index,
      role: message?.role || "<missing>",
    };
    if (summary.role === "assistant" && Array.isArray(message?.tool_calls)) {
      summary.toolCalls = message.tool_calls.slice(0, 32).map((toolCall) => ({
        id: typeof toolCall?.id === "string" ? toolCall.id : "<missing>",
        name: String(toolCall?.function?.name || toolCall?.name || "<missing>"),
        argumentLength: typeof toolCall?.function?.arguments === "string"
          ? toolCall.function.arguments.length
          : 0,
      }));
    }
    if (summary.role === "tool") {
      summary.toolCallId = typeof message?.tool_call_id === "string" ? message.tool_call_id : "<missing>";
      summary.contentType = message?.content == null ? "null" : Array.isArray(message.content) ? "array" : typeof message.content;
      summary.contentLength = typeof message?.content === "string" ? message.content.length : JSON.stringify(message?.content ?? "").length;
    }
    if (summary.role !== "tool" && !summary.toolCalls) {
      const content = message?.content;
      summary.contentLength = typeof content === "string" ? content.length : content == null ? 0 : JSON.stringify(content).length;
    }
    return summary;
  });
}

export function diagnoseToolResults(messages = []) {
  const callsById = new Map();
  const resultBatchById = new Map();
  let currentBatch = [];

  for (const message of Array.isArray(messages) ? messages : []) {
    if (message?.role === "assistant" && Array.isArray(message.tool_calls) && message.tool_calls.length) {
      currentBatch = [];
      for (const toolCall of message.tool_calls) {
        const fn = toolCall?.function && typeof toolCall.function === "object" ? toolCall.function : {};
        const id = typeof toolCall?.id === "string" ? toolCall.id.trim() : "";
        if (!id) continue;
        const name = String(fn.name || toolCall?.name || "<missing>").trim() || "<missing>";
        callsById.set(id, name);
        currentBatch.push(id);
      }
      for (const id of currentBatch) resultBatchById.set(id, currentBatch.length);
      continue;
    }
    if (message?.role !== "tool") {
      currentBatch = [];
      continue;
    }
  }

  const diagnostics = [];
  for (const [resultIndex, message] of (Array.isArray(messages) ? messages : []).entries()) {
    if (message?.role !== "tool") continue;
    const toolCallId = typeof message.tool_call_id === "string" ? message.tool_call_id.trim() : "";
    const content = message.content;
    const contentType = content == null ? "null" : Array.isArray(content) ? "array" : typeof content;
    const serialized = typeof content === "string" ? content : content == null ? "" : JSON.stringify(content);
    const hasControlChars = /[\u0000-\u0008\u000b\u000c\u000e-\u001f\u007f]/u.test(serialized);
    const looksBinary = serialized.includes("\u0000") || serialized.includes("\ufffd") || (serialized.length > 0 && (serialized.match(/[\u0000-\u0008\u000e-\u001f]/g) || []).length / serialized.length > 0.01);
    let jsonValid = null;
    if (typeof content === "string") {
      try { JSON.parse(content); jsonValid = true; } catch { jsonValid = false; }
    }
    diagnostics.push({
      resultIndex,
      toolCallId: toolCallId || "<missing>",
      toolName: callsById.get(toolCallId) || String(message.name || "<unknown>"),
      contentType,
      contentLength: serialized.length,
      hasControlChars,
      looksBinary,
      jsonValid,
      parallelBatchSize: resultBatchById.get(toolCallId) || 1,
    });
    if (diagnostics.length >= 64) break;
  }
  return diagnostics;
}

export function diagnoseOpenAIToolHistory(messages = [], tools = []) {
  const definedToolNames = new Set();
  const duplicateToolNames = [];
  const malformedToolDefinitions = [];
  for (const [index, tool] of (Array.isArray(tools) ? tools : []).entries()) {
    const fn = tool?.type === "function" ? tool.function : tool;
    const name = typeof fn?.name === "string" ? fn.name.trim() : "";
    if (!name) {
      malformedToolDefinitions.push(index);
      continue;
    }
    if (definedToolNames.has(name)) duplicateToolNames.push(name);
    definedToolNames.add(name);
  }

  const seenCallIds = new Set();
  const pendingCalls = new Map();
  const assistantToolNames = [];
  const toolResultNames = [];
  const duplicateToolCallIds = [];
  const missingToolCallIds = [];
  const orphanToolResultIds = [];
  const invalidArguments = [];
  const undefinedToolCalls = [];
  const sequenceBreaks = [];
  let assistantToolCallCount = 0;
  let toolResultCount = 0;

  for (const [messageIndex, message] of (Array.isArray(messages) ? messages : []).entries()) {
    if (message?.role === "assistant" && Array.isArray(message.tool_calls) && message.tool_calls.length) {
      for (const [callIndex, toolCall] of message.tool_calls.entries()) {
        assistantToolCallCount += 1;
        const fn = toolCall?.function && typeof toolCall.function === "object" ? toolCall.function : {};
        const name = String(fn.name || toolCall?.name || "").trim();
        assistantToolNames.push(name || "<missing>");
        const id = typeof toolCall?.id === "string" ? toolCall.id.trim() : "";
        if (!id) missingToolCallIds.push({ messageIndex, callIndex, name: name || "<missing>" });
        else if (seenCallIds.has(id)) duplicateToolCallIds.push({ messageIndex, callIndex, id, name: name || "<missing>" });
        else {
          seenCallIds.add(id);
          pendingCalls.set(id, { name: name || "<missing>", messageIndex, callIndex });
        }
        if (definedToolNames.size && name && !definedToolNames.has(name)) {
          undefinedToolCalls.push({ messageIndex, callIndex, name });
        }
        const rawArguments = fn.arguments;
        if (typeof rawArguments !== "string" || !rawArguments.trim()) {
          invalidArguments.push({ messageIndex, callIndex, name: name || "<missing>", reason: "not_string_or_empty" });
        } else {
          try {
            JSON.parse(rawArguments);
          } catch {
            invalidArguments.push({ messageIndex, callIndex, name: name || "<missing>", reason: "invalid_json" });
          }
        }
      }
      continue;
    }

    if (message?.role === "tool") {
      toolResultCount += 1;
      const id = typeof message.tool_call_id === "string" ? message.tool_call_id.trim() : "";
      const name = String(message.name || "").trim();
      toolResultNames.push(name || "<missing>");
      if (!id) {
        missingToolCallIds.push({ messageIndex, name: name || "<missing>", kind: "tool_result" });
      } else if (!pendingCalls.has(id)) {
        orphanToolResultIds.push({ messageIndex, id, name: name || "<missing>" });
      } else {
        pendingCalls.delete(id);
      }
      continue;
    }

    if (pendingCalls.size && message?.role !== "system" && message?.role !== "developer") {
      sequenceBreaks.push({
        messageIndex,
        role: message?.role || "<missing>",
        pendingCount: pendingCalls.size,
        pendingToolNames: [...pendingCalls.values()].map((call) => call.name).slice(0, 16),
      });
    }
  }

  return {
    messageCount: Array.isArray(messages) ? messages.length : 0,
    definedToolCount: definedToolNames.size,
    definedToolNames: [...definedToolNames].slice(0, 32),
    assistantToolCallCount,
    toolResultCount,
    pendingToolCallCount: pendingCalls.size,
    pendingToolNames: [...pendingCalls.values()].map((call) => call.name).slice(0, 16),
    assistantToolNames: assistantToolNames.slice(0, 32),
    toolResultNames: toolResultNames.slice(0, 32),
    duplicateToolCallIds,
    missingToolCallIds,
    orphanToolResultIds,
    invalidArguments,
    undefinedToolCalls,
    undefinedToolNames: [...new Set(undefinedToolCalls.map((call) => call.name))],
    toolResultDiagnostics: diagnoseToolResults(messages),
    duplicateToolNames,
    malformedToolDefinitions,
    sequenceBreaks,
    valid: duplicateToolCallIds.length === 0
      && missingToolCallIds.length === 0
      && orphanToolResultIds.length === 0
      && invalidArguments.length === 0
      && undefinedToolCalls.length === 0
      && sequenceBreaks.length === 0,
  };
}

export function buildPlainChatBody({
  messages,
  model = "auto",
  system,
  maxTokens = 32000,
  template,
  enableReasoning = false,
  enableThinking,
  reasoningEffort,
  reasoningBudgetTokens,
  contextLength,
  maxInputTokens,
  tools = [],
  toolChoice,
}) {
  const base = template ? structuredClone(template) : {};
  const reqId = crypto.randomUUID();
  const sessionId = crypto.randomUUID();
  const mapped = mapModel(model);

  const filteredHistory = filterUnknownToolHistory(messages || [], tools);
  const openaiMessages = normalizeMessagesForUpstream(filteredHistory.messages);

  // Chat-API mode:
  // - Do NOT inherit capture-template Qoder agent system/tools (they explode multiturn).
  // - DO preserve caller/sub2api identity system prompts.
  // Upstream capture format keeps system both as top-level `system` and as messages[role=system].
  const sysFromMsgs = openaiMessages
    .filter((m) => m.role === "system" || m.role === "developer")
    .map((m) => m.content)
    .filter(Boolean);
  const nonSystem = openaiMessages.filter((m) => m.role !== "system" && m.role !== "developer");

  let systemText = "";
  if (system != null && String(system).trim()) systemText = String(system).trim();
  else if (sysFromMsgs.length) systemText = sysFromMsgs.join("\n\n");
  // No hard-coded default identity. Empty means "use model default".
  // Callers (sub2api identity injection / client system) should supply their own.

  const systemMessages = [];
  if (systemText) {
    // Prefer a single canonical system message at the front.
    systemMessages.push({ role: "system", content: systemText });
  }

  const normalizedEffort = typeof reasoningEffort === "string" && reasoningEffort.trim()
    ? reasoningEffort.trim()
    : undefined;
  const effectiveThinking = typeof enableThinking === "boolean"
    ? enableThinking
    : normalizedEffort
      ? !["none", "off", "disabled"].includes(normalizedEffort.toLowerCase())
      : enableReasoning
        ? true
        : undefined;
  const defaultMaxInputTokens = mapped === "mmodel"
    ? 1000000
    : (base.model_config?.max_input_tokens || 180000);

  const modelConfig = {
    ...(base.model_config || {}),
    key: mapped,
    display_name: displayModel(mapped),
    model: "",
    format: base.model_config?.format || "openai",
    is_vl: base.model_config?.is_vl ?? true,
    is_reasoning: effectiveThinking ? true : (effectiveThinking === false ? false : (base.model_config?.is_reasoning ?? false)),
    api_key: "",
    url: "",
    source: base.model_config?.source || "system",
    max_input_tokens: maxInputTokens ?? defaultMaxInputTokens,
  };

  const parameters = {
    ...(base.parameters || {}),
    max_tokens: maxTokens || base.parameters?.max_tokens || 32000,
    ...(normalizedEffort !== undefined ? { reasoning_effort: normalizedEffort } : {}),
    ...(effectiveThinking !== undefined ? { enable_thinking: effectiveThinking } : {}),
    ...(reasoningBudgetTokens !== undefined ? { reasoning_budget_tokens: reasoningBudgetTokens } : {}),
    ...(contextLength !== undefined ? { context_length: contextLength } : {}),
    ...(toolChoice !== undefined ? { tool_choice: toolChoice } : {}),
  };

  return {
    ...base,
    request_id: reqId,
    request_set_id: crypto.randomUUID(),
    chat_record_id: reqId,
    session_id: sessionId,
    stream: true,
    chat_task: base.chat_task || "FREE_INPUT",
    chat_context: {
      text: "",
      features: {},
      extra: {},
      chatPrompt: "",
      imageUrls: [],
    },
    is_reply: false,
    is_retry: false,
    source: base.source || "cli",
    version: base.version || "1.0",
    agent_id: base.agent_id || "agent_common",
    task_id: "common",
    session_type: base.session_type || "assistant",
    aliyun_user_type: base.aliyun_user_type || "",
    model_config: modelConfig,
    custom_model: null,
    system: systemText || "",
    messages: (systemMessages.length || nonSystem.length)
      ? [...systemMessages, ...(nonSystem.length ? nonSystem : [{ role: "user", content: "ping" }])]
      : [{ role: "user", content: "ping" }],
    // Pass through caller tools (OpenAI function tools). Do NOT inherit capture-template tools.
    tools: normalizeTools(tools),
    parameters,
    business: {
      product: "cli",
      version: process.env.QODER_CLI_VERSION || "1.1.27",
      type: "agent",
      id: crypto.randomUUID(),
      name: String(nonSystem[0]?.content || "chat").slice(0, 40),
      begin_at: Date.now(),
      stage: "start",
    },
  };
}
