const state = {
  overview: null,
  page: "home",
};

const pages = {
  home: { title: "首页", desc: "查看本地代理运行状态与可用端点" },
  auth: { title: "登录授权", desc: "检查 Qoder CLI 登录态与 worker 热上下文" },
  providers: { title: "供应商", desc: "当前已接入的 provider 与模型别名" },
  access: { title: "API 接入", desc: "复制 Base URL，并快速验证 Chat Completions" },
};

function $(id) { return document.getElementById(id); }

function getKey() {
  return ($("api-key").value || localStorage.getItem("cli2api_key") || "").trim();
}

function saveKey() {
  localStorage.setItem("cli2api_key", getKey());
}

function headers(json = true) {
  const h = {};
  if (json) h["Content-Type"] = "application/json";
  const key = getKey();
  if (key) h.Authorization = `Bearer ${key}`;
  return h;
}

async function api(path, opts = {}) {
  const res = await fetch(path, {
    ...opts,
    headers: { ...headers(!(opts.body instanceof FormData) && opts.method !== "GET"), ...(opts.headers || {}) },
  });
  const text = await res.text();
  let data;
  try { data = text ? JSON.parse(text) : null; } catch { data = text; }
  if (!res.ok) {
    const msg = data?.error?.message || data?.message || text || res.statusText;
    throw new Error(msg);
  }
  return data;
}

function setPill(el, text, kind = "ok") {
  el.className = `pill ${kind === "ok" ? "" : kind}`.trim();
  el.textContent = text;
}

function originBase() {
  return `${location.protocol}//${location.host}`;
}

function absUrl(pathOrUrl) {
  if (!pathOrUrl) return originBase();
  if (/^https?:\/\//i.test(pathOrUrl)) return pathOrUrl;
  if (pathOrUrl.startsWith("/")) return `${originBase()}${pathOrUrl}`;
  return `${originBase()}/${pathOrUrl}`;
}

function renderEndpoints(overview) {
  const base = absUrl(overview?.access?.openai_base_url || "/v1");
  const items = [
    { name: "OpenAI Compatible", url: base },
    { name: "Chat Completions", url: absUrl(overview?.access?.chat_completions || `${base}/chat/completions`) },
    { name: "Models", url: absUrl(overview?.access?.models || `${base}/models`) },
    { name: "Health", url: absUrl(overview?.access?.health || "/health") },
  ];
  $("endpoint-list").innerHTML = items.map((it) => `
    <div class="endpoint">
      <div>
        <div>${it.name}</div>
        <code>${it.url}</code>
      </div>
      <button class="btn ghost" data-copy="${it.url}">复制</button>
    </div>
  `).join("");
  $("endpoint-list").querySelectorAll("[data-copy]").forEach((btn) => {
    btn.onclick = async () => {
      await navigator.clipboard.writeText(btn.dataset.copy);
      btn.textContent = "已复制";
      setTimeout(() => (btn.textContent = "复制"), 1000);
    };
  });
  $("access-base").textContent = base;
  $("access-curl").textContent = `curl -s ${base}/chat/completions \\
  -H "Authorization: Bearer $PROXY_API_KEY" \\
  -H "Content-Type: application/json" \\
  -d '{"model":"qwen3.7-plus","messages":[{"role":"user","content":"只回复OK"}]}'`;
}

function renderAuth(overview) {
  const auth = overview?.auth || {};
  const worker = overview?.worker || {};
  $("auth-kv").innerHTML = `
    <div><span>Has user blob</span><b>${auth.has_user_blob ? "yes" : "no"}</b></div>
    <div><span>User blob bytes</span><b>${auth.user_blob_bytes ?? 0}</b></div>
    <div><span>Machine ID</span><b>${auth.machine_id || "—"}</b></div>
    <div><span>Worker hot</span><b>${worker.hot ? "yes" : "no"}</b></div>
    <div><span>Worker endpoint</span><b>${worker.endpoint || "—"}</b></div>
    <div><span>Rewarm count</span><b>${worker.rewarmCount ?? worker.rewarm_count ?? 0}</b></div>
  `;
  $("auth-json").textContent = JSON.stringify({ auth, worker }, null, 2);
}

function renderModels(overview) {
  const models = overview?.models || [];
  $("model-table").innerHTML = `
    <div class="tr head"><div>模型</div><div>Provider</div><div>状态</div></div>
    ${models.map((m) => `
      <div class="tr">
        <div><b>${m.id}</b></div>
        <div>${m.owned_by || "qoder"}</div>
        <div><span class="pill soft">ready</span></div>
      </div>
    `).join("")}
  `;
  $("test-model").innerHTML = models.map((m) => `<option value="${m.id}">${m.id}</option>`).join("");
}

function renderHome(overview) {
  const proxyOk = !!overview?.proxy?.ok;
  const workerOk = !!overview?.worker?.ok;
  const hot = !!overview?.worker?.hot;
  const authOk = !!overview?.auth?.has_user_blob || !!overview?.auth?.has_pat;
  $("m-runtime").textContent = proxyOk ? "Running" : "Down";
  $("m-worker").textContent = workerOk ? (hot ? "Hot" : "Up") : "Down";
  $("m-auth").textContent = authOk ? "Ready" : "Missing";
  $("m-port").textContent = overview?.proxy?.port || location.port || "3010";

  $("kv-proxy").textContent = proxyOk ? "ok" : "down";
  $("kv-hot").textContent = hot ? "true" : "false";
  $("kv-endpoint").textContent = overview?.worker?.endpoint || overview?.proxy?.chat_url || "—";
  $("kv-rewarm").textContent = String(overview?.worker?.rewarmCount ?? overview?.worker?.rewarm_count ?? 0);
  $("kv-error").textContent = overview?.worker?.lastError || overview?.worker?.last_error || "—";
  $("home-updated").textContent = overview?.time || new Date().toLocaleString();

  setPill($("sidebar-runtime"), proxyOk && workerOk ? "Running" : "Degraded", proxyOk && workerOk ? "ok" : "warn");
}

async function refresh() {
  saveKey();
  $("btn-refresh").textContent = "刷新中…";
  try {
    const overview = await api("/api/overview");
    state.overview = overview;
    renderHome(overview);
    renderEndpoints(overview);
    renderAuth(overview);
    renderModels(overview);
  } catch (err) {
    setPill($("sidebar-runtime"), "Error", "bad");
    $("m-runtime").textContent = "Error";
    $("kv-error").textContent = err.message;
    console.error(err);
  } finally {
    $("btn-refresh").textContent = "刷新";
  }
}

function switchPage(name) {
  state.page = name;
  document.querySelectorAll(".nav-item").forEach((btn) => {
    btn.classList.toggle("active", btn.dataset.page === name);
  });
  document.querySelectorAll(".page").forEach((page) => {
    page.classList.toggle("active", page.id === `page-${name}`);
  });
  $("page-title").textContent = pages[name].title;
  $("page-desc").textContent = pages[name].desc;
}

async function rewarm() {
  $("btn-rewarm").textContent = "Rewarming…";
  try {
    const out = await api("/api/rewarm", { method: "POST", body: "{}" });
    $("auth-json").textContent = JSON.stringify(out, null, 2);
    await refresh();
  } catch (err) {
    alert(err.message);
  } finally {
    $("btn-rewarm").textContent = "Rewarm";
  }
}

async function testChat() {
  const model = $("test-model").value || "qwen3.7-plus";
  const content = $("test-prompt").value || "只回复OK";
  $("test-out").textContent = "请求中…";
  try {
    const data = await api("/v1/chat/completions", {
      method: "POST",
      body: JSON.stringify({
        model,
        stream: false,
        messages: [{ role: "user", content }],
      }),
    });
    $("test-out").textContent = JSON.stringify(data, null, 2);
  } catch (err) {
    $("test-out").textContent = err.message;
  }
}

function boot() {
  $("api-key").value = localStorage.getItem("cli2api_key") || "";
  document.querySelectorAll(".nav-item").forEach((btn) => {
    btn.onclick = () => switchPage(btn.dataset.page);
  });
  $("btn-refresh").onclick = refresh;
  $("btn-rewarm").onclick = rewarm;
  $("btn-test").onclick = testChat;
  $("btn-copy-openai").onclick = async () => {
    const url = state.overview?.access?.openai_base_url || `${originBase()}/v1`;
    await navigator.clipboard.writeText(url);
    $("btn-copy-openai").textContent = "已复制";
    setTimeout(() => ($("btn-copy-openai").textContent = "复制 OpenAI Base URL"), 1000);
  };
  $("api-key").addEventListener("change", saveKey);
  switchPage("home");
  refresh();
}

boot();
