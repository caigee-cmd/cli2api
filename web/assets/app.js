const state = {
  overview: null,
  page: "home",
};

const pages = {
  home: {
    kicker: "Console",
    title: "Overview",
    desc: "Inspect local proxy health and ready endpoints.",
  },
  auth: {
    kicker: "Credentials",
    title: "Auth",
    desc: "Check Qoder CLI login state and worker hot context.",
  },
  providers: {
    kicker: "Upstream",
    title: "Providers",
    desc: "Models currently exposed by the Qoder provider.",
  },
  access: {
    kicker: "Integrate",
    title: "API Access",
    desc: "Copy Base URL and run a quick Chat Completions check.",
  },
};

function $(id) {
  return document.getElementById(id);
}

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
    headers: {
      ...headers(!(opts.body instanceof FormData) && opts.method !== "GET"),
      ...(opts.headers || {}),
    },
  });
  const text = await res.text();
  let data;
  try {
    data = text ? JSON.parse(text) : null;
  } catch {
    data = text;
  }
  if (!res.ok) {
    const msg = data?.error?.message || data?.message || text || res.statusText;
    throw new Error(msg);
  }
  return data;
}

function setStatus(el, text, kind = "ok") {
  el.className = `status-chip ${kind === "ok" ? "" : kind}`.trim();
  el.innerHTML = `<span class="dot"></span><span>${text}</span>`;
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
  $("endpoint-list").innerHTML = items
    .map(
      (it) => `
    <div class="endpoint">
      <div>
        <div>${it.name}</div>
        <code>${it.url}</code>
      </div>
      <button class="btn ghost" data-copy="${it.url}">Copy</button>
    </div>`,
    )
    .join("");
  $("endpoint-list").querySelectorAll("[data-copy]").forEach((btn) => {
    btn.onclick = async () => {
      await navigator.clipboard.writeText(btn.dataset.copy);
      btn.textContent = "Copied";
      setTimeout(() => (btn.textContent = "Copy"), 900);
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
  if (!models.length) {
    $("model-table").innerHTML = `<div class="empty">No providers loaded.</div>`;
    $("test-model").innerHTML = "";
    return;
  }
  $("model-table").innerHTML = `
    <div class="tr head"><div>Model</div><div>Provider</div><div>State</div></div>
    ${models
      .map(
        (m) => `
      <div class="tr">
        <div><b>${m.id}</b></div>
        <div>${m.owned_by || "qoder"}</div>
        <div><span class="chip">ready</span></div>
      </div>`,
      )
      .join("")}
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
  $("home-updated").textContent = overview?.time || new Date().toLocaleString();
  $("home-kv").innerHTML = `
    <div><span>Proxy</span><b>${proxyOk ? "ok" : "down"}</b></div>
    <div><span>Worker hot</span><b>${hot ? "true" : "false"}</b></div>
    <div><span>Endpoint</span><b>${overview?.worker?.endpoint || overview?.proxy?.chat_url || "—"}</b></div>
    <div><span>Rewarm</span><b>${String(overview?.worker?.rewarmCount ?? overview?.worker?.rewarm_count ?? 0)}</b></div>
    <div><span>Last error</span><b id="kv-error">${overview?.worker?.lastError || overview?.worker?.last_error || "—"}</b></div>
  `;
  setStatus($("sidebar-runtime"), proxyOk && workerOk ? "Running" : "Degraded", proxyOk && workerOk ? "ok" : "warn");
}

async function refresh() {
  saveKey();
  $("btn-refresh").textContent = "Refreshing";
  $("endpoint-list").innerHTML = `
    <div class="skeleton-line"></div>
    <div class="skeleton-line"></div>
    <div class="skeleton-line"></div>
  `;
  try {
    const overview = await fetch("/api/overview").then(async (res) => {
      const text = await res.text();
      let data;
      try {
        data = text ? JSON.parse(text) : null;
      } catch {
        data = text;
      }
      if (!res.ok) throw new Error(data?.error?.message || text || res.statusText);
      return data;
    });
    state.overview = overview;
    renderHome(overview);
    renderEndpoints(overview);
    renderAuth(overview);
    renderModels(overview);
    if (overview?.ui?.needs_api_key_for_chat && !getKey()) {
      const err = $("kv-error");
      if (err) err.textContent = "Dashboard loaded. Fill PROXY_API_KEY before chat/rewarm.";
    }
  } catch (err) {
    setStatus($("sidebar-runtime"), "Error", "bad");
    $("m-runtime").textContent = "Error";
    $("endpoint-list").innerHTML = `<div class="empty">Failed to load overview: ${err.message}</div>`;
    $("home-kv").innerHTML = `<div><span>Last error</span><b>${err.message}</b></div>`;
    console.error(err);
  } finally {
    $("btn-refresh").innerHTML = `
      <svg width="15" height="15" viewBox="0 0 24 24" fill="none"><path d="M20 12a8 8 0 1 1-2.2-5.5M20 4v5h-5" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round"/></svg>
      Refresh`;
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
  $("page-kicker").textContent = pages[name].kicker;
  $("page-title").textContent = pages[name].title;
  $("page-desc").textContent = pages[name].desc;
}

async function rewarm() {
  $("btn-rewarm").textContent = "Rewarming";
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
  $("test-out").textContent = "Requesting…";
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
    const url = state.overview?.access?.openai_base_url
      ? absUrl(state.overview.access.openai_base_url)
      : `${originBase()}/v1`;
    await navigator.clipboard.writeText(url);
    $("btn-copy-openai").textContent = "Copied";
    setTimeout(() => ($("btn-copy-openai").textContent = "Copy Base URL"), 900);
  };
  $("api-key").addEventListener("change", saveKey);
  switchPage("home");
  refresh();
}

boot();
