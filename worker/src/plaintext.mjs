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

function normalizeMessagesForUpstream(messages = []) {
  return (messages || []).map((m) => {
    const role = m?.role;
    const out = { role, content: contentToString(m?.content) };
    if (role === "assistant" && Array.isArray(m?.tool_calls) && m.tool_calls.length) {
      out.tool_calls = m.tool_calls;
      // OpenAI allows content null when tool_calls present
      if (!out.content) out.content = null;
    }
    if (role === "tool") {
      if (m?.tool_call_id) out.tool_call_id = m.tool_call_id;
      if (m?.name) out.name = m.name;
    }
    return out;
  });
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

  const openaiMessages = normalizeMessagesForUpstream(messages || []);

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
