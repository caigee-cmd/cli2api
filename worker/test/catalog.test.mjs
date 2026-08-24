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
