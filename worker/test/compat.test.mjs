import assert from "node:assert/strict";
import { test } from "node:test";
import { inspectQodercliSource, patchQodercliSource } from "../src/compat.mjs";

const PINNED_SOURCE = [
  "prepareInferRequest(A,e,t,i){",
  "async createWasmContext(){let A=await Yi();this.machineId||(this.machineId=await this.getMachineId()),XsA(this.machineId,A,JSON.stringify(this.getUserInfoForAuth()))}",
  "function En(){return WLe||(WLe=new XLe),WLe}",
  "function Of(){return c2t||(c2t=new CC(_e())),c2t}function zXA(){aHe.clear(),ARA.clear()}",
].join("\n");

test("inspect reports missing hooks on unknown CLI source", () => {
  const report = inspectQodercliSource("export function noop(){return 1}");
  assert.equal(report.ok, false);
  assert.equal(report.prepareInferPatched, false);
  assert.equal(report.createWasmPatched, false);
  assert.match(report.message, /incompatible/i);
});

test("inspect reports pinned 1.1.27 needles as compatible", () => {
  const report = inspectQodercliSource(PINNED_SOURCE);
  assert.equal(report.ok, true);
  assert.equal(report.prepareInferFound, true);
  assert.equal(report.createWasmFound, true);
  assert.equal(report.modelCatalogFound, true);
  assert.equal(report.quotaApiFound, true);
});

test("patch injects capture hooks into pinned CLI source", () => {
  const patched = patchQodercliSource(PINNED_SOURCE);
  assert.match(patched, /__QODER_WORKER_INJECTED__/);
  assert.match(patched, /__qoderWorkerOnPrepareInfer/);
  assert.match(patched, /__qoderWorkerAdoptContext/);
  assert.match(patched, /__qoderAuthManager/);
  assert.match(patched, /__qoderWorkerGetModelCatalog/);
  assert.match(patched, /Hm\(\);return En\(\)/);
  assert.match(patched, /__QODER_WORKER_QUOTA_API__/);
  assert.match(patched, /__qoderWorkerGetQuotaApi/);
  assert.match(patched, /Cq\(\);return Of\(\)/);
});

test("patch is idempotent", () => {
  const once = patchQodercliSource(PINNED_SOURCE);
  const twice = patchQodercliSource(once);
  assert.equal(once, twice);
});

test("patch throws a version-aware error when needles are missing", () => {
  assert.throws(
    () => patchQodercliSource("export const x = 1;"),
    /qodercli/i,
  );
});

test("incompatible message mentions both CLI packages", () => {
  const report = inspectQodercliSource("export const x = 1;");
  assert.match(report.message, /@qoder-ai\/qodercli/);
  assert.match(report.message, /@qodercn-ai\/qoderclicn/);
});

test("patch injects skip-main boot hook when HEg needle is present", () => {
  const source = [
    "prepareInferRequest(A,e,t,i){",
    "async createWasmContext(){let A=await Yi();this.machineId||(this.machineId=await this.getMachineId()),XsA(this.machineId,A,JSON.stringify(this.getUserInfoForAuth()))}",
    "function En(){return WLe||(WLe=new XLe),WLe}",
    "function Of(){return c2t||(c2t=new CC(_e())),c2t}function zXA(){aHe.clear(),ARA.clear()}",
    "async function HEg(){let{main:A}=await Promise.resolve().then(()=>(b$o(),U$o));await A()}",
  ].join("\n");
  const patched = patchQodercliSource(source);
  assert.match(patched, /__QODER_WORKER_SKIP_MAIN__/);
  assert.match(patched, /__qoderWorkerBoot/);
});
