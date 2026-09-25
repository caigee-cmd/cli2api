import assert from "node:assert/strict";
import { test } from "node:test";
import { createModelCatalogSnapshot, resolveCatalogModel } from "../src/catalog.mjs";

test("routes canonical public IDs to Qoder catalog keys", () => {
  const snapshot = createModelCatalogSnapshot([
    { key: "gmodel", display_name: "GLM-5.3", source: "system", max_input_tokens: 200000 },
    { key: "dfmodel", display_name: "DeepSeek-V4-Flash", source: "system" },
  ]);

  assert.equal(resolveCatalogModel(snapshot, "glm-5.3")?.key, "gmodel");
  assert.equal(resolveCatalogModel(snapshot, "deepseek_v4_flash")?.key, "dfmodel");
  assert.equal(snapshot.models[0].mapped_key, "gmodel");
});

test("permits native Qoder keys and excludes disabled catalog entries", () => {
  const snapshot = createModelCatalogSnapshot([
    { key: "gmodel", display_name: "GLM-5.3" },
    { key: "oldmodel", display_name: "Old", enable: false },
  ]);

  assert.equal(resolveCatalogModel(snapshot, "gmodel")?.display_name, "GLM-5.3");
  assert.equal(resolveCatalogModel(snapshot, "old"), null);
});

test("surfaces Qoder price_factor and is_free without inventing values", () => {
  const snapshot = createModelCatalogSnapshot([
    { key: "kmodel_latest", display_name: "Kimi-K3", price_factor: 1.4, is_free: false },
    { key: "qfmodel", display_name: "Qwen3.8-Flash", price_factor: 0, is_free: true },
    // No price reported: must stay absent, not become 0 (which reads as free).
    { key: "npmodel", display_name: "NoPrice" },
  ]);

  const byId = Object.fromEntries(snapshot.models.map((m) => [m.id, m]));
  assert.equal(byId["kimi-k3"].price_factor, 1.4);
  assert.equal(byId["kimi-k3"].is_free, false);
  assert.equal(byId["qwen3.8-flash"].price_factor, 0);
  assert.equal(byId["qwen3.8-flash"].is_free, true);
  assert.equal(byId["noprice"].price_factor, undefined);
  assert.equal(byId["noprice"].is_free, undefined);
});

test("forwards tags so the free badge follows Qoder's limited_time_free signal", () => {
  const snapshot = createModelCatalogSnapshot([
    { key: "a", display_name: "Tagged", price_factor: 0.5, is_free: true, tags: ["limited_time_free"] },
    { key: "b", display_name: "Dual", price_factor: 0.5, is_free: true },
  ]);
  const byId = Object.fromEntries(snapshot.models.map((m) => [m.id, m]));
  assert.deepEqual(byId["tagged"].tags, ["limited_time_free"]);
  assert.equal(byId["dual"].tags, undefined);
});

test("forwards Qoder context metadata (default + selectable windows)", () => {
  const snapshot = createModelCatalogSnapshot([
    { key: "qmodel_38max", display_name: "Qwen3.8-Max", context_length: 180000,
      default_context_window: 200000, available_context_windows: [200000, 400000, 1000000],
      max_output_tokens: 32000 },
    { key: "bare", display_name: "Bare" },
  ]);
  const byId = Object.fromEntries(snapshot.models.map((m) => [m.id, m]));
  assert.equal(byId["qwen3.8-max"].default_context_window, 200000);
  assert.deepEqual(byId["qwen3.8-max"].available_context_windows, [200000, 400000, 1000000]);
  assert.equal(byId["qwen3.8-max"].max_output_tokens, 32000);
  assert.equal(byId["bare"].default_context_window, undefined);
  assert.equal(byId["bare"].available_context_windows, undefined);
});
