import http from "node:http";
import { spawn } from "node:child_process";
import { fileURLToPath, pathToFileURL } from "node:url";
import path from "node:path";
import { classifyError } from "./errors.mjs";
import { parseHomes, pickChild, publicAccountView } from "./pool.mjs";

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const daemonPath = path.join(__dirname, "daemon.mjs");

function parseAccounts() {
  const homes = parseHomes(process.env.QODER_HOMES || process.env.QODER_ACCOUNTS);
  if (homes.length) return homes;
  const home = process.env.QODER_HOME || process.env.HOME || "/root";
  return [{ id: process.env.QODER_ACCOUNT_ID || "default", home }];
}

function qoderHomeFor(home) {
  const root = String(home || "").replace(/\/$/, "");
  return root.endsWith(".qoder") ? root : path.join(root, ".qoder");
}

function redactHome() {
  return undefined;
}

const accounts = parseAccounts();
const multiHomes = Boolean(String(process.env.QODER_HOMES || process.env.QODER_ACCOUNTS || "").trim());
if (accounts.length <= 1 && process.env.QODER_SUPERVISOR !== "1") {
  process.env.QODER_ACCOUNT_ID = accounts[0]?.id || "default";
  if (multiHomes && accounts[0]?.home) {
    process.env.HOME = accounts[0].home.endsWith(".qoder") ? path.dirname(accounts[0].home) : accounts[0].home;
    process.env.QODER_HOME = qoderHomeFor(accounts[0].home);
  }
  await import(pathToFileURL(daemonPath).href);
} else {
  const host = process.env.WORKER_HOST || "127.0.0.1";
  const publicPort = Number(process.env.WORKER_PORT || 3020);
  const apiKey = process.env.PROXY_API_KEY || "";
  const children = [];
  let cursor = 0;

  function log(...a) {
    console.error("[supervisor]", ...a);
  }

  function markDown(child, classified) {
    if (!child || !classified) return;
    child.lastError = classified.message;
    child.lastKind = classified.kind;
    if (!classified.failover && classified.kind === "quota") return;
    const sec = Math.max(classified.retryAfterSec || classified.cooldownSec || 0, 0);
    if (sec > 0) child.downUntil = Date.now() + sec * 1000;
    if (classified.kind === "not_ready") child.hot = false;
  }

  function spawnChild(acc, existing) {
    const port = existing?.port || publicPort + children.length + 1;
    const childProc = spawn(process.execPath, [daemonPath], {
      env: {
        ...process.env,
        WORKER_HOST: "127.0.0.1",
        WORKER_PORT: String(port),
        QODER_ACCOUNT_ID: acc.id,
        HOME: acc.home.endsWith(".qoder") ? path.dirname(acc.home) : acc.home,
        QODER_HOME: qoderHomeFor(acc.home),
      },
      stdio: "inherit",
    });
    const rec = existing || {
      id: acc.id,
      home: acc.home,
      port,
      url: `http://127.0.0.1:${port}`,
      restarts: 0,
      ok: true,
      hot: undefined,
      lastError: null,
      downUntil: 0,
    };
    rec.child = childProc;
    rec.port = port;
    rec.url = `http://127.0.0.1:${port}`;
    rec.ok = true;
    childProc.on("exit", (code) => {
      rec.ok = false;
      rec.hot = false;
      rec.lastError = `exited ${code}`;
      rec.downUntil = Date.now() + Math.min(30_000, 2000 * (1 + rec.restarts));
      log(`account ${acc.id} exited`, code);
      const delay = Math.min(30_000, 1000 * 2 ** Math.min(rec.restarts, 5));
      rec.restarts += 1;
      setTimeout(() => spawnChild(acc, rec), delay);
    });
    if (!existing) children.push(rec);
    log("spawned", { id: acc.id, port });
    return rec;
  }

  for (const acc of accounts) spawnChild(acc);

  async function refreshHealth(child) {
    try {
      const r = await fetch(`http://127.0.0.1:${child.port}/health`);
      const body = await r.json();
      child.ok = body.ok !== false && r.ok;
      child.hot = !!body.hot;
      child.inFlight = body.inFlight || 0;
      child.rewarmCount = body.rewarmCount || 0;
      child.lastError = body.lastError || child.lastError;
      child.accountId = body.accountId || child.id;
      return body;
    } catch (err) {
      child.ok = false;
      child.hot = false;
      child.lastError = err.message;
      return { id: child.id, ok: false, error: err.message };
    }
  }

  function authOK(req) {
    if (!apiKey) return true;
    const h = req.headers.authorization || "";
    const got = h.startsWith("Bearer ") ? h.slice(7) : req.headers["x-api-key"];
    return got === apiKey;
  }

  async function readBody(req) {
    const chunks = [];
    for await (const c of req) chunks.push(c);
    return Buffer.concat(chunks);
  }

  function hopByHop() {
    return new Set(["connection", "keep-alive", "proxy-authenticate", "proxy-authorization", "te", "trailers", "transfer-encoding", "upgrade", "host", "content-length"]);
  }

  async function proxyTo(child, req, url, body) {
    const headers = {};
    const skip = hopByHop();
    for (const [k, v] of Object.entries(req.headers)) {
      if (!skip.has(k.toLowerCase()) && v != null) headers[k] = Array.isArray(v) ? v.join(",") : v;
    }
    headers["x-qoder-account"] = child.id;
    if (body && body.length && !headers["content-type"]) headers["content-type"] = "application/json";
    const upstream = await fetch(`http://127.0.0.1:${child.port}${url.pathname}${url.search}`, {
      method: req.method,
      headers,
      body: req.method === "GET" || req.method === "HEAD" ? undefined : body,
    });
    return upstream;
  }

  function writeUpstreamHead(res, upstream, extra = {}) {
    const headers = Object.fromEntries(upstream.headers);
    delete headers["content-length"];
    delete headers["transfer-encoding"];
    Object.assign(headers, extra);
    res.writeHead(upstream.status, headers);
  }

  const server = http.createServer(async (req, res) => {
    try {
      const url = new URL(req.url, `http://${host}:${publicPort}`);
      if (url.pathname === "/health") {
        const snaps = [];
        for (const child of children) {
          const body = await refreshHealth(child);
          snaps.push({
            ...body,
            ...publicAccountView(child),
            id: child.id,
            port: child.port,
            home: redactHome(child.home),
          });
        }
        const body = JSON.stringify({
          ok: snaps.some((s) => s.ok || s.ready || s.hot),
          supervisor: true,
          accounts: snaps,
          hot: snaps.some((s) => s.hot),
          bootMode: "supervisor",
        });
        res.writeHead(200, { "content-type": "application/json", "content-length": Buffer.byteLength(body) });
        res.end(body);
        return;
      }
      if (url.pathname === "/admin/accounts") {
        if (!authOK(req)) {
          res.writeHead(401, { "content-type": "application/json" });
          res.end(JSON.stringify({ error: { code: "invalid_api_key", message: "unauthorized" } }));
          return;
        }
        await Promise.all(children.map((c) => refreshHealth(c)));
        const body = JSON.stringify({
          object: "list",
          data: children.map((c) => publicAccountView(c)),
        });
        res.writeHead(200, { "content-type": "application/json", "content-length": Buffer.byteLength(body) });
        res.end(body);
        return;
      }

      const prefer = url.searchParams.get("account") || req.headers["x-qoder-account"];
      const needsBody = req.method !== "GET" && req.method !== "HEAD";
      const body = needsBody ? await readBody(req) : undefined;
      const excluded = new Set();
      const attempts = Math.max(1, children.length);
      let lastErr = null;
      for (let i = 0; i < attempts; i++) {
        const picked = pickChild(children, { prefer: i === 0 ? prefer : "", excluded, cursor });
        const target = picked.item;
        if (!target) break;
        cursor = picked.cursor;
        try {
          const upstream = await proxyTo(target, req, url, body);
          const kindHeader = upstream.headers.get("x-qoder-error-kind");
          const failoverHeader = upstream.headers.get("x-qoder-failover");
          if (upstream.status >= 400) {
            const text = await upstream.text();
            const classified = classifyError({
              status: upstream.status,
              body: text,
              kind: kindHeader,
              retryAfter: upstream.headers.get("retry-after"),
            });
            if (failoverHeader === "0") classified.failover = false;
            markDown(target, classified);
            lastErr = { classified, text, status: classified.status, headers: Object.fromEntries(upstream.headers) };
            if (classified.failover && i + 1 < attempts) {
              excluded.add(target.id);
              continue;
            }
            res.writeHead(classified.status, {
              "content-type": "application/json",
              "x-qoder-account": target.id,
              "x-qoder-error-kind": classified.kind,
              "x-qoder-failover": classified.failover ? "1" : "0",
              ...(classified.retryAfterSec ? { "retry-after": String(classified.retryAfterSec) } : {}),
            });
            res.end(text);
            return;
          }
          target.ok = true;
          target.lastError = null;
          target.downUntil = 0;
          writeUpstreamHead(res, upstream, { "x-qoder-account": target.id });
          if (upstream.body) {
            for await (const chunk of upstream.body) res.write(chunk);
          }
          res.end();
          return;
        } catch (err) {
          const classified = classifyError({ message: err.message, kind: "unavailable" });
          markDown(target, classified);
          lastErr = { classified, text: JSON.stringify({ error: { message: err.message, code: "worker_error" } }), status: classified.status };
          excluded.add(target.id);
        }
      }
      const payload = lastErr?.text || JSON.stringify({ error: { code: "no_accounts", message: "no worker accounts available" } });
      const classified = lastErr?.classified || classifyError({ message: "no worker accounts available", kind: "unavailable" });
      res.writeHead(classified.status || 503, {
        "content-type": "application/json",
        "x-qoder-error-kind": classified.kind,
        "x-qoder-failover": classified.failover ? "1" : "0",
      });
      res.end(payload);
    } catch (err) {
      const classified = classifyError({ message: err.message });
      res.writeHead(classified.status, { "content-type": "application/json" });
      res.end(JSON.stringify({ error: { code: classified.code, message: classified.message, kind: classified.kind } }));
    }
  });
  server.listen(publicPort, host, () => log(`listening on http://${host}:${publicPort}`));
}
