export async function load(url, context, nextLoad) {
  const result = await nextLoad(url, context);
  if (!url.includes("qodercli.js") || result.format !== "module") return result;

  let source = result.source;
  if (source && typeof source !== "string") source = Buffer.from(source).toString("utf8");
  if (typeof source !== "string") return result;
  if (source.includes("__QODER_WORKER_INJECTED__")) return result;

  let next = source;

  const prepareNeedle = "prepareInferRequest(A,e,t,i){";
  if (next.includes(prepareNeedle)) {
    next = next.replace(
      prepareNeedle,
      "prepareInferRequest(A,e,t,i){ /* __QODER_WORKER_INJECTED__ */ try{ if(typeof globalThis.__qoderWorkerOnPrepareInfer==='function'){ globalThis.__qoderWorkerOnPrepareInfer(this,A,e,t,i); } }catch(_e){} ",
    );
  }

  // Capture AuthManager + newly created QoderContext during createWasmContext.
  const createNeedle =
    "async createWasmContext(){let A=await Yi();this.machineId||(this.machineId=await this.getMachineId()),XsA(this.machineId,A,JSON.stringify(this.getUserInfoForAuth()))}";
  if (next.includes(createNeedle)) {
    next = next.replace(
      createNeedle,
      "async createWasmContext(){ /* __QODER_WORKER_INJECTED__ */ try{ globalThis.__qoderAuthManager=this; }catch(_e){} let A=await Yi();this.machineId||(this.machineId=await this.getMachineId()); const __ctx=XsA(this.machineId,A,JSON.stringify(this.getUserInfoForAuth())); try{ if(typeof globalThis.__qoderWorkerAdoptContext==='function'){ globalThis.__qoderWorkerAdoptContext(__ctx,this); } }catch(_e2){} }",
    );
  }

  return { ...result, source: next, shortCircuit: true };
}
