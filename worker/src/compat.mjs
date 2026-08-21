import fs from "node:fs";
import path from "node:path";

export const PINNED_QODERCLI_VERSION = "1.1.27";

export const NEEDLES = {
  prepareInfer: "prepareInferRequest(A,e,t,i){",
  createWasm:
    "async createWasmContext(){let A=await Yi();this.machineId||(this.machineId=await this.getMachineId()),XsA(this.machineId,A,JSON.stringify(this.getUserInfoForAuth()))}",
};

export function inspectQodercliSource(source, { version } = {}) {
  const text = String(source || "");
  const alreadyPatched = text.includes("__QODER_WORKER_INJECTED__");
  const prepareInferFound = alreadyPatched || text.includes(NEEDLES.prepareInfer);
  const createWasmFound = alreadyPatched || text.includes(NEEDLES.createWasm);
  const ok = alreadyPatched || (prepareInferFound && createWasmFound);
  const found = version ? `, found ${version}` : "";
  return {
    ok,
    alreadyPatched,
    prepareInferFound,
    createWasmFound,
    prepareInferPatched: alreadyPatched,
    createWasmPatched: alreadyPatched,
    version: version || null,
    pinnedVersion: PINNED_QODERCLI_VERSION,
    message: ok
      ? `qodercli hooks compatible${version ? ` (${version})` : ""}`
      : `incompatible qodercli source: missing WASM capture needles (pinned ${PINNED_QODERCLI_VERSION}${found}). Pin @qoder-ai/qodercli@${PINNED_QODERCLI_VERSION} or update worker/src/compat.mjs.`,
  };
}

export function patchQodercliSource(source, { version } = {}) {
  const text = String(source || "");
  if (text.includes("__QODER_WORKER_INJECTED__")) return text;
  const report = inspectQodercliSource(text, { version });
  if (!report.ok) {
    throw new Error(report.message);
  }
  return text
    .replace(
      NEEDLES.prepareInfer,
      "prepareInferRequest(A,e,t,i){ /* __QODER_WORKER_INJECTED__ */ try{ if(typeof globalThis.__qoderWorkerOnPrepareInfer==='function'){ globalThis.__qoderWorkerOnPrepareInfer(this,A,e,t,i); } }catch(_e){} ",
    )
    .replace(
      NEEDLES.createWasm,
      "async createWasmContext(){ /* __QODER_WORKER_INJECTED__ */ try{ globalThis.__qoderAuthManager=this; }catch(_e){} let A=await Yi();this.machineId||(this.machineId=await this.getMachineId()); const __ctx=XsA(this.machineId,A,JSON.stringify(this.getUserInfoForAuth())); try{ if(typeof globalThis.__qoderWorkerAdoptContext==='function'){ globalThis.__qoderWorkerAdoptContext(__ctx,this); } }catch(_e2){} }",
    );
}

export function readQodercliVersion(jsPath) {
  const candidates = [
    path.resolve(path.dirname(jsPath), "../../package.json"),
    path.resolve(path.dirname(jsPath), "../package.json"),
  ];
  for (const pkgPath of candidates) {
    try {
      const raw = fs.readFileSync(pkgPath, "utf8");
      const parsed = JSON.parse(raw);
      if (parsed?.name === "@qoder-ai/qodercli" || parsed?.version) {
        return String(parsed.version || "");
      }
    } catch {}
  }
  return null;
}
