import { canonicalModelID } from "./plaintext.mjs";

export const DEFAULT_CATALOG_TTL_MS = 5 * 60_000;

function cleanString(value) {
  return typeof value === "string" ? value.trim() : "";
}

// numberOrUndefined keeps a numeric upstream field only when it is actually a
// finite number, so an absent price_factor stays absent instead of becoming 0
// (which the console would read as "free").
function numberOrUndefined(value) {
  return typeof value === "number" && Number.isFinite(value) ? value : undefined;
}

export function createModelCatalogSnapshot(entries) {
  const routes = new Map();
  const models = [];
  const seenIDs = new Set();

  for (const rawEntry of Array.isArray(entries) ? entries : []) {
    if (!rawEntry || typeof rawEntry !== "object" || rawEntry.enable === false) continue;
    const key = cleanString(rawEntry.key);
    if (!key) continue;
    const displayName = cleanString(rawEntry.display_name) || key;
    const id = canonicalModelID(displayName);
    const route = {
      key,
      display_name: displayName,
      source: cleanString(rawEntry.source) || "system",
      max_input_tokens: Number(rawEntry.max_input_tokens) || undefined,
      is_reasoning: typeof rawEntry.is_reasoning === "boolean" ? rawEntry.is_reasoning : undefined,
      is_vl: typeof rawEntry.is_vl === "boolean" ? rawEntry.is_vl : undefined,
      // Qoder reports each model's price multiplier and free flag; the Go
      // adapter renders them (credits text + free badge). Kept as undefined
      // when upstream omits them so the console shows nothing rather than 0.
      price_factor: numberOrUndefined(rawEntry.price_factor),
      is_free: typeof rawEntry.is_free === "boolean" ? rawEntry.is_free : undefined,
      tags: Array.isArray(rawEntry.tags) && rawEntry.tags.length > 0 ? rawEntry.tags.map(String) : undefined,
    };

    routes.set(id, route);
    routes.set(canonicalModelID(key), route);
    if (!seenIDs.has(id)) {
      seenIDs.add(id);
      models.push({
        id,
        display_name: displayName,
        mapped_key: key,
        route_display_name: displayName,
        object: "model",
        owned_by: "qoder",
        context_length: route.max_input_tokens,
        is_reasoning: route.is_reasoning,
        price_factor: route.price_factor,
        is_free: route.is_free,
        tags: route.tags,
      });
    }
  }

  return { models, routes };
}

export function resolveCatalogModel(snapshot, requestedModel) {
  const requested = cleanString(requestedModel) || "auto";
  return snapshot?.routes?.get(canonicalModelID(requested)) || null;
}
