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
    desc: "Browser device-flow login, PAT login, and worker rewarm.",
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
  const el = $("api-key");
  return ((el && el.value) || localStorage.getItem("cli2api_key") || "").trim();
}

function saveKey() {
  const el = $("api-key");
  if (el) localStorage.setItem("cli2api_key", el.value.trim());
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
  const login = overview?.login?.login || overview?.login || {};
  $("auth-kv").innerHTML = `
    <div><span>Has user blob</span><b>${auth.has_user_blob ? "yes" : "no"}</b></div>
    <div><span>User blob bytes</span><b>${auth.user_blob_bytes ?? 0}</b></div>
    <div><span>Machine ID</span><b>${auth.machine_id || "—"}</b></div>
    <div><span>Worker hot</span><b>${worker.hot ? "yes" : "no"}</b></div>
    <div><span>Worker endpoint</span><b>${worker.endpoint || "—"}</b></div>
    <div><span>Rewarm count</span><b>${worker.rewarmCount ?? worker.rewarm_count ?? 0}</b></div>
  `;
  const status = login.status || "idle";
  $("login-status").textContent = status;
  $("login-message").textContent = login.message || "Click Browser login to get an auth URL.";
  if (login.authUrl) {
    $("login-url-wrap").hidden = false;
    $("login-url").textContent = login.authUrl;
  } else if ($("login-url-wrap")) {
    $("login-url-wrap").hidden = true;
  }
  $("auth-json").textContent = JSON.stringify({ auth, worker, login }, null, 2);
}

function renderModels(overview) {
  const models = overview?.models || [];
  const filter = (($("model-filter") && $("model-filter").value) || "").trim().toLowerCase();
  const filtered = !filter
    ? models
    : models.filter((m) => `${m.id} ${m.mapped_key || ""}`.toLowerCase().includes(filter));
  if ($("model-meta")) {
    $("model-meta").textContent = models.length
      ? `${filtered.length} shown / ${models.length} total`
      : "No models yet";
  }
  if (!filtered.length) {
    $("model-table").innerHTML = `<div class="empty">${models.length ? "No models match this filter." : "No providers loaded."}</div>`;
  } else {
    $("model-table").innerHTML = `
    <div class="tr head"><div>Model</div><div>Mapped key</div><div>State</div></div>
    ${filtered
      .map(
        (m, idx) => `
      <div class="tr" style="animation-delay:${idx * 20}ms">
        <div><b>${m.id}</b></div>
        <div class="mono">${m.mapped_key || m.id}</div>
        <div><span class="chip">${m.stale ? "fallback" : "ready"}</span></div>
      </div>`,
      )
      .join("")}
  `;
  }
  if ($("test-model")) {
    $("test-model").innerHTML = models.map((m) => `<option value="${m.id}">${m.id}</option>`).join("");
  }
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
    // Dashboard/login/models do not require PROXY_API_KEY.
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
    const out = await fetch("/api/rewarm", { method: "POST", headers: { "Content-Type": "application/json" }, body: "{}" }).then(async (res) => {
      const text = await res.text();
      const data = text ? JSON.parse(text) : null;
      if (!res.ok) throw new Error(data?.error?.message || text || res.statusText);
      return data;
    });
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
    const data = await fetch("/api/chat", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        model,
        stream: false,
        messages: [{ role: "user", content }],
      }),
    }).then(async (res) => {
      const text = await res.text();
      const parsed = text ? JSON.parse(text) : null;
      if (!res.ok) throw new Error(parsed?.error?.message || text || res.statusText);
      return parsed;
    });
    $("test-out").textContent = JSON.stringify(data, null, 2);
  } catch (err) {
    $("test-out").textContent = err.message;
  }
}

async function startDeviceLogin() {
  $("btn-login-device").textContent = "Starting…";
  try {
    const out = await fetch("/api/login/device", { method: "POST", headers: { "Content-Type": "application/json" }, body: "{}" }).then(async (res) => {
      const text = await res.text();
      const data = text ? JSON.parse(text) : null;
      if (!res.ok) throw new Error(data?.error?.message || text || res.statusText);
      return data;
    });
    if (out.authUrl) {
      $("login-url-wrap").hidden = false;
      $("login-url").textContent = out.authUrl;
      $("login-status").textContent = out.status || "pending";
      $("login-message").textContent = out.message || "Open the auth URL to finish login";
      window.open(out.authUrl, "_blank", "noopener,noreferrer");
    }
    await refresh();
    // poll login status a few times
    for (let i = 0; i < 30; i++) {
      await new Promise((r) => setTimeout(r, 2000));
      const st = await fetch("/api/login/status").then((r) => r.json());
      const login = st.login || {};
      $("login-status").textContent = login.status || "pending";
      $("login-message").textContent = login.message || "";
      if (login.status === "ok" || login.status === "error") {
        await refresh();
        break;
      }
    }
  } catch (err) {
    alert(err.message);
  } finally {
    $("btn-login-device").textContent = "Browser login";
  }
}

async function loginWithPat() {
  const pat = ($("pat-input").value || "").trim();
  if (!pat) {
    alert("Paste a PAT first");
    return;
  }
  $("btn-login-pat").textContent = "Logging in…";
  try {
    const out = await fetch("/api/login/pat", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ pat }),
    }).then(async (res) => {
      const text = await res.text();
      const data = text ? JSON.parse(text) : null;
      if (!res.ok) throw new Error(data?.error?.message || text || res.statusText);
      return data;
    });
    $("login-status").textContent = out.status || "ok";
    $("login-message").textContent = "PAT login completed";
    $("pat-input").value = "";
    await refresh();
  } catch (err) {
    alert(err.message);
  } finally {
    $("btn-login-pat").textContent = "Login with PAT";
  }
}

async function refreshModels() {
  $("btn-refresh-models").textContent = "Refreshing…";
  try {
    const data = await fetch("/api/models?refresh=1").then(async (res) => {
      const text = await res.text();
      const parsed = text ? JSON.parse(text) : null;
      if (!res.ok) throw new Error(parsed?.error?.message || text || res.statusText);
      return parsed;
    });
    if (state.overview) state.overview.models = data.data || [];
    renderModels(state.overview || { models: data.data || [] });
  } catch (err) {
    alert(err.message);
  } finally {
    $("btn-refresh-models").textContent = "Refresh models";
  }
}

function boot() {
  if ($("api-key")) $("api-key").value = localStorage.getItem("cli2api_key") || "";
  document.querySelectorAll(".nav-item").forEach((btn) => {
    btn.onclick = () => switchPage(btn.dataset.page);
  });
  $("btn-refresh").onclick = refresh;
  $("btn-rewarm").onclick = rewarm;
  $("btn-test").onclick = testChat;
  $("btn-login-device").onclick = startDeviceLogin;
  $("btn-login-pat").onclick = loginWithPat;
  $("btn-refresh-models").onclick = refreshModels;
  $("btn-copy-login").onclick = async () => {
    const url = $("login-url").textContent;
    if (!url || url === "—") return;
    await navigator.clipboard.writeText(url);
    $("btn-copy-login").textContent = "Copied";
    setTimeout(() => ($("btn-copy-login").textContent = "Copy"), 900);
  };
  $("btn-open-login").onclick = () => {
    const url = $("login-url").textContent;
    if (url && url !== "—") window.open(url, "_blank", "noopener,noreferrer");
  };
  $("btn-copy-openai").onclick = async () => {
    const url = state.overview?.access?.openai_base_url
      ? absUrl(state.overview.access.openai_base_url)
      : `${originBase()}/v1`;
    await navigator.clipboard.writeText(url);
    $("btn-copy-openai").textContent = "Copied";
    setTimeout(() => ($("btn-copy-openai").textContent = "Copy Base URL"), 900);
  };
  if ($("api-key")) $("api-key").addEventListener("change", saveKey);
  if ($("model-filter")) $("model-filter").addEventListener("input", () => renderModels(state.overview || { models: [] }));
  switchPage("home");
  refresh();
}

boot();
