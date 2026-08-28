import fs from "node:fs";
import path from "node:path";

export const PINNED_QODERCLI_VERSION = "1.1.32";

export const NEEDLES = {
  prepareInfer: "prepareInferRequest(A,e,t,i){",
  createWasm:
    "async createWasmContext(){let A=await Ki();this.machineId||(this.machineId=await this.getMachineId()),HlA(this.machineId,A,JSON.stringify(this.getUserInfoForAuth()))}",
  modelCatalog: "function kn(){return r9e||(r9e=new o9e),r9e}",
  quotaApi:
    "function zf(){return B_t||(B_t=new nw(_e())),B_t}function tAe(){c4e.clear(),nFA.clear()}",
  skipMain:
    "async function QNu(){let{main:A}=await Promise.resolve().then(()=>(Uds(),Fds));await A()}",
};

export function inspectQodercliSource(source, { version } = {}) {
  const text = String(source || "");
  const alreadyPatched =
    text.includes("__QODER_WORKER_INJECTED__") &&
    text.includes("__QODER_WORKER_MODEL_CATALOG__") &&
    text.includes("__QODER_WORKER_QUOTA_API__");
  const prepareInferFound = alreadyPatched || text.includes(NEEDLES.prepareInfer);
  const createWasmFound = alreadyPatched || text.includes(NEEDLES.createWasm);
  const modelCatalogFound = alreadyPatched || text.includes(NEEDLES.modelCatalog);
  const quotaApiFound = alreadyPatched || text.includes(NEEDLES.quotaApi);
  const skipMainFound = alreadyPatched || text.includes(NEEDLES.skipMain);
  const ok = alreadyPatched || (prepareInferFound && createWasmFound && modelCatalogFound && quotaApiFound);
  const found = version ? `, found ${version}` : "";
  return {
    ok,
    alreadyPatched,
    prepareInferFound,
    createWasmFound,
    modelCatalogFound,
    quotaApiFound,
    skipMainFound,
    prepareInferPatched: alreadyPatched,
    createWasmPatched: alreadyPatched,
    modelCatalogPatched: alreadyPatched,
    quotaApiPatched: alreadyPatched,
    skipMainPatched: alreadyPatched && text.includes("__QODER_WORKER_SKIP_MAIN__"),
    version: version || null,
    pinnedVersion: PINNED_QODERCLI_VERSION,
    message: ok
      ? `qodercli hooks compatible${version ? ` (${version})` : ""}`
      : `incompatible qodercli source: missing WASM/catalog/quota capture needles (pinned ${PINNED_QODERCLI_VERSION}${found}). Pin @qoder-ai/qodercli@${PINNED_QODERCLI_VERSION} or @qodercn-ai/qoderclicn@${PINNED_QODERCLI_VERSION}, or update worker/src/compat.mjs.`,
  };
}

export function patchQodercliSource(source, { version } = {}) {
  const text = String(source || "");
  if (
    text.includes("__QODER_WORKER_INJECTED__") &&
    text.includes("__QODER_WORKER_MODEL_CATALOG__") &&
    text.includes("__QODER_WORKER_QUOTA_API__")
  ) {
    return text;
  }
  const report = inspectQodercliSource(text, { version });
  if (!report.ok) {
    throw new Error(report.message);
  }
  let next = text
    .replace(
      NEEDLES.prepareInfer,
      "prepareInferRequest(A,e,t,i){ /* __QODER_WORKER_INJECTED__ */ try{ if(typeof globalThis.__qoderWorkerOnPrepareInfer==='function'){ globalThis.__qoderWorkerOnPrepareInfer(this,A,e,t,i); } }catch(_e){} ",
    )
    .replace(
      NEEDLES.createWasm,
      "async createWasmContext(){ /* __QODER_WORKER_INJECTED__ */ try{ globalThis.__qoderAuthManager=this; }catch(_e){} let A=await Ki();this.machineId||(this.machineId=await this.getMachineId()); const __ctx=HlA(this.machineId,A,JSON.stringify(this.getUserInfoForAuth())); try{ if(typeof globalThis.__qoderWorkerAdoptContext==='function'){ globalThis.__qoderWorkerAdoptContext(__ctx,this); } }catch(_e2){} }",
    )
    .replace(
      NEEDLES.modelCatalog,
      "function kn(){return r9e||(r9e=new o9e),r9e} /* __QODER_WORKER_MODEL_CATALOG__ */ try{globalThis.__qoderWorkerGetModelCatalog=()=>{ph();return kn()}}catch(_e){}",
    )
    .replace(
      NEEDLES.quotaApi,
      "function zf(){return B_t||(B_t=new nw(_e())),B_t}function tAe(){c4e.clear(),nFA.clear()} /* __QODER_WORKER_QUOTA_API__ */ try{globalThis.__qoderWorkerGetQuotaApi=()=>{o5();return zf()}}catch(_e){}",
    );
  if (next.includes(NEEDLES.skipMain)) {
    next = next.replace(
      NEEDLES.skipMain,
      "async function QNu(){ /* __QODER_WORKER_INJECTED__ __QODER_WORKER_SKIP_MAIN__ */ if(typeof globalThis.__qoderWorkerBoot==='function'){ const {getQoderAuthManager}=await Promise.resolve().then(()=>(Ul(),D3A)); const {initializeQoderRuntime}=await Promise.resolve().then(()=>(eG(),FeA)); return globalThis.__qoderWorkerBoot({getQoderAuthManager,initializeQoderRuntime}); } let{main:A}=await Promise.resolve().then(()=>(Uds(),Fds));await A()}",
    );
  }
  return next;
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
      if (parsed?.name === "@qoder-ai/qodercli" || parsed?.name === "@qodercn-ai/qoderclicn" || parsed?.version) {
        return String(parsed.version || "");
      }
    } catch {}
  }
  return null;
}
