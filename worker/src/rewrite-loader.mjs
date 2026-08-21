import { fileURLToPath } from "node:url";
import { patchQodercliSource, readQodercliVersion } from "./compat.mjs";

export async function load(url, context, nextLoad) {
  const result = await nextLoad(url, context);
  if (!url.includes("qodercli.js") || result.format !== "module") return result;

  let source = result.source;
  if (source && typeof source !== "string") source = Buffer.from(source).toString("utf8");
  if (typeof source !== "string") return result;

  let version = null;
  try {
    version = readQodercliVersion(fileURLToPath(url));
  } catch {}
  const next = patchQodercliSource(source, { version });
  return { ...result, source: next, shortCircuit: true };
}
