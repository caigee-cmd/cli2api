import { register } from "node:module";
import { pathToFileURL } from "node:url";
import { fileURLToPath } from "node:url";
import path from "node:path";

const here = path.dirname(fileURLToPath(import.meta.url));
register(pathToFileURL(path.join(here, "rewrite-loader.mjs")).href);

let resolveReady;
export const contextReady = new Promise((r) => {
  resolveReady = r;
});

export let hotContext = null;
export let hotEndpoint = null;
export let hotModelKey = "auto";
export let hotModelSource = "system";

globalThis.__qoderWorkerOnPrepareInfer = function (ctx, endpoint, body, modelKey, modelSource) {
  if (!hotContext && ctx && typeof ctx.prepareInferRequest === "function") {
    hotContext = ctx;
    hotEndpoint = endpoint;
    hotModelKey = modelKey || "auto";
    hotModelSource = modelSource || "system";
    resolveReady({
      endpoint: hotEndpoint,
      modelKey: hotModelKey,
      modelSource: hotModelSource,
    });
  }
};
