import { spawn } from "node:child_process";
import path from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";
import fs from "node:fs";

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const bridge = pathToFileURL(path.join(__dirname, "context-bridge.mjs")).href;

export async function warmupContext({
  qodercliBin = process.env.QODERCLI_BIN || "qodercli",
  cwd = process.env.QODER_WARMUP_CWD || "/tmp",
  timeoutMs = Number(process.env.WARMUP_TIMEOUT_MS || 120000),
} = {}) {
  const { contextReady } = await import(pathToFileURL(path.join(__dirname, "context-bridge.mjs")).href);

  await new Promise((resolve, reject) => {
    const child = spawn(
      qodercliBin,
      [
        "--print",
        "--output-format",
        "json",
        "--model",
        "auto",
        "--dangerously-skip-permissions",
        "--cwd",
        cwd,
        "--",
        "只回复OK",
      ],
      {
        env: {
          ...process.env,
          NODE_OPTIONS: `${process.env.NODE_OPTIONS || ""} --import ${bridge}`.trim(),
        },
        stdio: ["ignore", "pipe", "pipe"],
      },
    );

    let stderr = "";
    const timer = setTimeout(() => {
      child.kill();
      reject(new Error(`warmup timed out after ${timeoutMs}ms`));
    }, timeoutMs);

    child.stderr.on("data", (d) => {
      stderr += d.toString("utf8");
    });
    child.on("error", (err) => {
      clearTimeout(timer);
      reject(err);
    });
    child.on("close", (code) => {
      clearTimeout(timer);
      if (code !== 0) {
        reject(new Error(`warmup qodercli exit=${code}: ${stderr.slice(-400)}`));
        return;
      }
      resolve();
    });
  });

  const meta = await contextReady;
  // Persist a plaintext template from last successful local capture if present.
  return meta;
}

if (import.meta.url === pathToFileURL(process.argv[1]).href) {
  warmupContext()
    .then((meta) => {
      console.log(JSON.stringify({ ok: true, meta }, null, 2));
      process.exit(0);
    })
    .catch((err) => {
      console.error(err);
      process.exit(1);
    });
}
