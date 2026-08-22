import http from "node:http";
import { spawn } from "node:child_process";
import { fileURLToPath, pathToFileURL } from "node:url";
import path from "node:path";

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const daemonPath = path.join(__dirname, "daemon.mjs");

function parseHomes(raw) {
  const text = String(raw || "").trim();
  if (!text) return [];
  return text
    .split(",")
    .map((part) => part.trim())
    .filter(Boolean)
    .map((part, idx) => {
      const split = part.includes("=") ? part.split("=") : part.split(":");
      if (split.length >= 2 && (split[1].startsWith("/") || split[1].startsWith("~") || split[0].length < 64)) {
        const id = split[0].trim();
        const home = split.slice(1).join(":").trim();
        return { id: id || `acc${idx + 1}`, home };
      }
      return { id: `acc${idx + 1}`, home: part };
    });
}

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

  for (const [i, acc] of accounts.entries()) {
    const port = publicPort + i + 1;
    const child = spawn(process.execPath, [daemonPath], {
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
    child.on("exit", (code) => {
      log(`account ${acc.id} exited`, code);
    });
    children.push({ ...acc, port, child });
    log("spawned", { id: acc.id, port, home: acc.home });
  }

  function pick(prefer) {
    if (prefer) {
      const hit = children.find((c) => c.id === prefer);
      if (hit) return hit;
    }
    const item = children[cursor % children.length];
    cursor += 1;
    return item;
  }

  function authOK(req) {
    if (!apiKey) return true;
    const h = req.headers.authorization || "";
    const got = h.startsWith("Bearer ") ? h.slice(7) : req.headers["x-api-key"];
    return got === apiKey;
  }

  const server = http.createServer(async (req, res) => {
    const url = new URL(req.url, `http://${host}:${publicPort}`);
    if (url.pathname === "/health") {
      const snaps = [];
      for (const child of children) {
        try {
          const r = await fetch(`http://127.0.0.1:${child.port}/health`);
          snaps.push({ id: child.id, port: child.port, ...(await r.json()) });
        } catch (err) {
          snaps.push({ id: child.id, port: child.port, ok: false, error: err.message });
        }
      }
      const body = JSON.stringify({
        ok: snaps.some((s) => s.ok),
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
      const body = JSON.stringify({
        object: "list",
        data: children.map((c) => ({ id: c.id, url: `http://127.0.0.1:${c.port}`, home: c.home })),
      });
      res.writeHead(200, { "content-type": "application/json", "content-length": Buffer.byteLength(body) });
      res.end(body);
      return;
    }
    const prefer = url.searchParams.get("account") || req.headers["x-qoder-account"];
    const target = pick(prefer);
    const upstream = await fetch(`http://127.0.0.1:${target.port}${url.pathname}${url.search}`, {
      method: req.method,
      headers: req.headers,
      body: req.method === "GET" || req.method === "HEAD" ? undefined : req,
      duplex: "half",
    });
    res.writeHead(upstream.status, {
      ...Object.fromEntries(upstream.headers),
      "x-qoder-account": target.id,
    });
    if (upstream.body) {
      for await (const chunk of upstream.body) res.write(chunk);
    }
    res.end();
  });
  server.listen(publicPort, host, () => log(`listening on http://${host}:${publicPort}`));
}
