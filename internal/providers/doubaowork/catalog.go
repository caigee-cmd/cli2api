package doubaowork

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"time"

	"github.com/caigee-cmd/cli2api/internal/providers"
)

// effortNames maps the upstream numeric reasoning levels onto gateway names.
var effortNames = map[int]string{3: "low", 4: "medium", 5: "high", 6: "xhigh", 7: "max", 8: "auto"}

// effortOrder is the canonical ordering for reasoning options.
var effortOrder = []string{"low", "medium", "high", "xhigh", "max", "auto"}

const modelIDPrefix = "doubao-"

const (
	catalogTTL        = 60 * time.Second
	catalogFailureTTL = 10 * time.Minute
)

// doubaoModel is one selectable model in a per-account catalog.
type doubaoModel struct {
	ID            string         // public id, e.g. doubao-kimi-k3
	Name          string         // display name
	Native        string         // model_item_key sent upstream
	ItemID        int            // option.need_deep_think
	ModeID        string         // mode_id
	AgentMode     int            // agent_mode
	ExtraParams   map[string]any // model_extra_params
	Levels        map[string]int // effort name -> numeric level
	DefaultEffort string
	Context       int
	Tag           string
	Rate          float64
	Free          bool
	RateText      string
}

// baselineItem mirrors the built-in fallback menu captured from the office mode.
type baselineItem struct {
	key         string
	name        string
	itemID      int
	window      int
	levels      []int
	defaultLvl  int
	needUpgrade bool
	tag         string
}

var baselineMenu = []baselineItem{
	{key: "9", name: "自动", itemID: 9, window: 256000, levels: []int{3, 8, 5}, defaultLvl: 8, tag: "小额免费"},
	{key: "seed-lite-7b", name: "豆包 2.1 Lite", itemID: 10001, window: 256000, levels: []int{3, 4, 5}, defaultLvl: 5, tag: "限时优惠 0.01x"},
	{key: "5", name: "豆包 2.1 Pro", itemID: 5, window: 256000, levels: []int{3, 4, 5}, defaultLvl: 5, needUpgrade: true, tag: "0.65x"},
	{key: "person-deepseek-v4-1-flash-261017", name: "DeepSeek V4.1 Flash", itemID: 10001, window: 300000, levels: []int{3, 4, 5, 6, 7}, defaultLvl: 5, tag: "限时优惠 0.01x"},
	{key: "person-kimi-k3-1-pro-261111", name: "Kimi K3.1 Pro", itemID: 10001, window: 300000, levels: []int{3, 4, 5, 6, 7}, defaultLvl: 5, tag: "0.95x"},
	{key: "person-kimi-k3-1-pro-highspeed-261111", name: "Kimi K3.1 Pro 高速", itemID: 10001, window: 300000, levels: []int{3, 4, 5, 6, 7}, defaultLvl: 5, tag: "1.44x"},
	{key: "person-kimi-k3-261111", name: "Kimi K3", itemID: 10001, window: 300000, levels: []int{3, 4, 5, 6, 7}, defaultLvl: 5, tag: "1.48x"},
	{key: "person-glm-5-3-261111", name: "GLM 5.3", itemID: 10001, window: 300000, levels: []int{3, 5, 7}, defaultLvl: 5, tag: "0.80x"},
	{key: "person-glm-5-3-flash-261111", name: "GLM 5.3 Flash", itemID: 10001, window: 300000, levels: []int{3, 5, 7}, defaultLvl: 5, tag: "0.08x"},
	{key: "person-deepseek-v4-pro-ga-260813", name: "DeepSeek V4 Pro", itemID: 10001, window: 300000, levels: []int{3, 4, 5, 6, 7}, defaultLvl: 5, tag: "0.46x"},
}

// readRate parses the billing multiplier from the upstream tag_list text.
func readRate(tags []string) (rate float64, free bool, text string) {
	for _, raw := range tags {
		normalized := normalizeTagText(raw)
		if normalized == "" {
			continue
		}
		if value, ok := rateFromText(normalized); ok {
			return value, value == 0, normalized
		}
		if strings.Contains(normalized, "免费") {
			return 0, true, normalized
		}
	}
	return 0, false, ""
}

func normalizeTagText(text string) string {
	var b strings.Builder
	for _, r := range text {
		switch {
		case r >= '０' && r <= '９':
			b.WriteRune(r - 0xfee0)
		case r == '．':
			b.WriteByte('.')
		case r == 'ｘ' || r == 'Ｘ':
			b.WriteByte('x')
		default:
			b.WriteRune(r)
		}
	}
	return strings.TrimSpace(b.String())
}

func rateFromText(text string) (float64, bool) {
	// "0.47x" / "1.44X" / "0.80×" / "0.47 倍"
	for i := 0; i < len(text); i++ {
		if text[i] < '0' || text[i] > '9' {
			continue
		}
		j := i
		dot := false
		for j < len(text) && ((text[j] >= '0' && text[j] <= '9') || (text[j] == '.' && !dot)) {
			if text[j] == '.' {
				dot = true
			}
			j++
		}
		value := text[i:j]
		rest := strings.TrimSpace(text[j:])
		if strings.HasPrefix(rest, "x") || strings.HasPrefix(rest, "X") || strings.HasPrefix(rest, "×") || strings.HasPrefix(rest, "倍") {
			if parsed, err := parseFloat(value); err == nil {
				return parsed, true
			}
		}
		i = j - 1
	}
	return 0, false
}

func parseFloat(text string) (float64, error) {
	var value float64
	if err := json.Unmarshal([]byte(text), &value); err == nil {
		return value, nil
	}
	return 0, &json.SyntaxError{}
}

// baselineCatalog builds the built-in model map.
func baselineCatalog() map[string]doubaoModel {
	out := make(map[string]doubaoModel, len(baselineMenu))
	for _, item := range baselineMenu {
		if item.needUpgrade {
			continue
		}
		extra := map[string]any{"total_window_size": itoa(item.window)}
		if item.window >= 300000 {
			extra["memory_profile"] = "300k_230"
		} else {
			extra["memory_profile"] = "256k"
		}
		entry := buildEntry(item.key, item.name, item.itemID, "3", 1, extra, item.levels, item.defaultLvl, item.window, item.tag, nil)
		out[entry.ID] = entry
	}
	return out
}

func buildEntry(key, name string, itemID int, modeID string, agentMode int, extra map[string]any, rawLevels []int, defaultLevel, window int, tag string, tags []string) doubaoModel {
	levels := map[string]int{}
	for _, level := range rawLevels {
		if effort, ok := effortNames[level]; ok {
			if _, exists := levels[effort]; !exists {
				levels[effort] = level
			}
		}
	}
	defaultEffort := effortNames[defaultLevel]
	if _, ok := levels[defaultEffort]; !ok {
		defaultEffort = firstEffort(levels)
	}
	if extra == nil {
		extra = map[string]any{}
	}
	if _, ok := extra["total_window_size"]; !ok && window > 0 {
		extra["total_window_size"] = itoa(window)
	}
	if _, ok := extra["memory_profile"]; !ok {
		if window >= 300000 {
			extra["memory_profile"] = "300k_230"
		} else {
			extra["memory_profile"] = "256k"
		}
	}
	rate, free, rateText := readRate(tags)
	if rateText == "" && tag != "" {
		rate, free, rateText = readRate([]string{tag})
	}
	return doubaoModel{
		ID:            modelIDPrefix + slugify(name, key),
		Name:          name,
		Native:        key,
		ItemID:        itemID,
		ModeID:        modeID,
		AgentMode:     agentMode,
		ExtraParams:   extra,
		Levels:        levels,
		DefaultEffort: defaultEffort,
		Context:       window,
		Tag:           tag,
		Rate:          rate,
		Free:          free,
		RateText:      rateText,
	}
}

func firstEffort(levels map[string]int) string {
	for _, effort := range effortOrder {
		if _, ok := levels[effort]; ok {
			return effort
		}
	}
	return ""
}

func slugify(name, key string) string {
	if isAllDigits(key) {
		alias := map[string]string{"0": "fast", "3": "expert", "5": "pro", "9": "auto"}
		if value, ok := alias[key]; ok {
			return value
		}
		fromName := slugText(name)
		if fromName != "" {
			return fromName
		}
		return "mode-" + key
	}
	trimmed := strings.TrimPrefix(key, "person-")
	for _, suffix := range []string{"-261017", "-261111", "-260813", "-261010"} {
		trimmed = strings.TrimSuffix(trimmed, suffix)
	}
	if isSlugCandidate(trimmed) {
		return strings.ToLower(trimmed)
	}
	return slugText(name)
}

func slugText(text string) string {
	var b strings.Builder
	prevDash := false
	for _, r := range strings.ToLower(text) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '.' {
			b.WriteRune(r)
			prevDash = false
		} else if !prevDash {
			b.WriteByte('-')
			prevDash = true
		}
	}
	return strings.Trim(b.String(), "-")
}

func isAllDigits(text string) bool {
	if text == "" {
		return false
	}
	for _, r := range text {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func isSlugCandidate(text string) bool {
	if text == "" {
		return false
	}
	for _, r := range text {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-') {
			return false
		}
	}
	return true
}

// parseModelsFromData extracts the office-mode models from a modeSelectData map.
func parseModelsFromData(data map[string]any) map[string]doubaoModel {
	if data == nil {
		return nil
	}
	conf := digMap(data, "menuConfV2")
	if conf == nil {
		conf = data
	}
	modes := digSlice(conf, "mode_list", "item_list")
	if modes == nil {
		return nil
	}
	for _, raw := range modes {
		mode, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if asString(mode["default_conversation_mode"]) != "office" {
			continue
		}
		modelList := digSlice(mode, "model_list", "item_list")
		if len(modelList) == 0 {
			continue
		}
		catalog := map[string]doubaoModel{}
		modeID := asString(mode["mode_id"])
		agentMode := intOf(mode["agent_mode"])
		for _, itemRaw := range modelList {
			item, ok := itemRaw.(map[string]any)
			if !ok {
				continue
			}
			entry, ok := toEntry(item, modeID, agentMode)
			if !ok {
				continue
			}
			if _, exists := catalog[entry.ID]; !exists {
				catalog[entry.ID] = entry
			}
		}
		if len(catalog) > 0 {
			return catalog
		}
	}
	return nil
}

func toEntry(item map[string]any, modeID string, modeAgentMode int) (doubaoModel, bool) {
	if needUpgrade(item) {
		return doubaoModel{}, false
	}
	key := strings.TrimSpace(firstNonEmpty(asString(item["model_item_key"]), asString(item["key"])))
	if key == "" {
		return doubaoModel{}, false
	}
	modes := stringSlice(item["conversation_mode_list"])
	if len(modes) > 0 && !containsString(modes, "office") {
		return doubaoModel{}, false
	}
	name := firstNonEmpty(asString(item["name"]), key)
	extra := map[string]any{}
	if rawExtra, ok := item["model_extra_params"].(map[string]any); ok {
		for k, v := range rawExtra {
			extra[k] = v
		}
	}
	window := intOf(firstOf(item["model_extra_params"], item["window"]))
	if window == 0 {
		window = intOf(item["window"])
	}
	if nested, ok := item["model_extra_params"].(map[string]any); ok {
		window = intOf(nested["total_window_size"])
	}
	var rawLevels []int
	if rec, ok := item["reasoning_effort_config"].(map[string]any); ok {
		for _, optRaw := range asSlice(rec["option_list"]) {
			opt, ok := optRaw.(map[string]any)
			if !ok {
				continue
			}
			rawLevels = append(rawLevels, intOf(opt["level"]))
		}
	}
	if rawLevels == nil {
		for _, value := range asSlice(item["levels"]) {
			rawLevels = append(rawLevels, intOf(value))
		}
	}
	defaultLevel := 0
	if rec, ok := item["reasoning_effort_config"].(map[string]any); ok {
		defaultLevel = intOf(rec["default_level"])
	}
	if defaultLevel == 0 {
		defaultLevel = intOf(item["defaultLevel"])
	}
	tag := ""
	tags := make([]string, 0)
	if tagList := asSlice(item["tag_list"]); len(tagList) > 0 {
		for _, tagRaw := range tagList {
			obj, ok := tagRaw.(map[string]any)
			if !ok {
				continue
			}
			if text := asString(obj["text"]); text != "" {
				tags = append(tags, text)
			}
		}
		if len(tags) > 0 {
			tag = tags[0]
		}
	}
	if tag == "" {
		tag = asString(item["tag"])
	}
	itemID := intOf(firstOf(item["item_id"], item["itemId"]))
	agentMode := intOf(firstOf(item["agent_mode"], item["agentMode"]))
	if agentMode == 0 {
		agentMode = modeAgentMode
	}
	entry := buildEntry(key, name, itemID, modeID, agentMode, extra, rawLevels, defaultLevel, window, tag, tags)
	return entry, true
}

// parseSsrModels scans the /chat page HTML for an inline modeSelectData object.
func parseSsrModels(html string) map[string]doubaoModel {
	const key = `"modeSelectData"`
	for at := 0; ; {
		found := strings.Index(html[at:], key)
		if found < 0 {
			return nil
		}
		at += found + len(key)
		i := at
		for i < len(html) && isSpace(html[i]) {
			i++
		}
		if i >= len(html) || html[i] != ':' {
			continue
		}
		i++
		for i < len(html) && isSpace(html[i]) {
			i++
		}
		if i >= len(html) || html[i] != '{' {
			continue
		}
		end := endOfJSONObject(html, i)
		if end < 0 {
			continue
		}
		var data map[string]any
		if json.Unmarshal([]byte(html[i:end]), &data) != nil {
			continue
		}
		if catalog := parseModelsFromData(data); catalog != nil {
			return catalog
		}
	}
}

func endOfJSONObject(html string, start int) int {
	depth := 0
	inString := false
	escape := false
	for i := start; i < len(html); i++ {
		ch := html[i]
		if escape {
			escape = false
			continue
		}
		if ch == '\\' {
			if inString {
				escape = true
			}
			continue
		}
		if ch == '"' {
			inString = !inString
			continue
		}
		if inString {
			continue
		}
		switch ch {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return i + 1
			}
		}
	}
	return -1
}

func isSpace(b byte) bool { return b == ' ' || b == '\t' || b == '\n' || b == '\r' }

// needUpgrade reports whether the menu item is gated behind a subscription the
// current account does not have (online nested form or baseline flat form).
func needUpgrade(item map[string]any) bool {
	if subscribe, ok := item["subscribe_config"].(map[string]any); ok {
		if value, ok := subscribe["need_upgrade"].(bool); ok && value {
			return true
		}
	}
	if value, ok := item["needUpgrade"].(bool); ok && value {
		return true
	}
	return false
}

// mergeCatalogs unions catalogs in order; earlier entries win, never shrinks.
func mergeCatalogs(catalogs ...map[string]doubaoModel) map[string]doubaoModel {
	merged := map[string]doubaoModel{}
	for _, one := range catalogs {
		for id, entry := range one {
			if _, exists := merged[id]; !exists {
				merged[id] = entry
			}
		}
	}
	if len(merged) == 0 {
		return nil
	}
	return merged
}

// selectModel resolves the requested public model against the catalog, falling
// back to the auto entry, then any model.
func selectModel(catalog map[string]doubaoModel, requested string) (doubaoModel, bool) {
	want := strings.TrimSpace(requested)
	want = strings.TrimPrefix(want, "doubaowork/")
	want = strings.TrimPrefix(want, modelIDPrefix)
	lowerWant := strings.ToLower(want)
	for id, entry := range catalog {
		if strings.EqualFold(id, "doubao-"+lowerWant) || strings.EqualFold(entry.Native, want) || strings.EqualFold(entry.Name, want) {
			return entry, true
		}
	}
	if entry, ok := catalog["doubao-auto"]; ok {
		return entry, true
	}
	for _, entry := range catalog {
		return entry, true
	}
	return doubaoModel{}, false
}

// toModelInfo converts a doubaoModel into the provider-neutral ModelInfo,
// folding the rate into the display name (it is a multiplier, not a price).
func toModelInfo(entry doubaoModel) providers.ModelInfo {
	display := entry.Name
	if entry.RateText != "" && !strings.Contains(display, entry.RateText) {
		display = entry.Name + " · " + entry.RateText
	}
	options := make([]string, 0, len(entry.Levels))
	for _, effort := range effortOrder {
		if _, ok := entry.Levels[effort]; ok {
			options = append(options, effort)
		}
	}
	return providers.ModelInfo{
		NativeModel: entry.Native,
		PublicModel: entry.ID,
		DisplayName: display,
		Free:        entry.Free,
		Capabilities: providers.ModelCapabilities{
			ContextWindow:    entry.Context,
			Tools:            true,
			Reasoning:        len(options) > 0,
			ReasoningOptions: options,
			ReasoningDefault: entry.DefaultEffort,
			ReasoningType:    "effort",
		},
	}
}

func modelInfos(catalog map[string]doubaoModel) []providers.ModelInfo {
	ids := make([]string, 0, len(catalog))
	for id := range catalog {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	out := make([]providers.ModelInfo, 0, len(ids))
	for _, id := range ids {
		out = append(out, toModelInfo(catalog[id]))
	}
	return out
}

// models returns the per-account catalog: online menu unioned with the baseline,
// never shrinking. Cached for catalogTTL; failures back off for catalogFailureTTL.
func (c *Client) models(ctx context.Context, sess *session) (map[string]doubaoModel, error) {
	if err := sess.ready(ctx); err != nil {
		return nil, err
	}
	now := time.Now()
	c.mu.Lock()
	cached, ok := c.catalogs[sess.accountID]
	failed, hasFailed := c.failedAt[sess.accountID]
	c.mu.Unlock()
	if ok && len(cached.catalog) > 0 && now.Sub(cached.at) < catalogTTL {
		return cached.catalog, nil
	}
	if hasFailed && now.Sub(failed) < catalogFailureTTL {
		if ok && len(cached.catalog) > 0 {
			return cached.catalog, nil
		}
		return baselineCatalog(), nil
	}

	baseline := baselineCatalog()
	online := map[string]doubaoModel{}
	if payload, err := sess.json(ctx, "POST", "/alice/slot/action_bar_v3/brief_list", map[string]any{
		"language_code": "zh", "bot_id": sess.bot,
	}, nil); err == nil {
		if parsed := parseModelsFromBrief(payload); parsed != nil {
			online = parsed
		}
	}
	if ssr, err := sess.fetchChatHTML(ctx); err == nil {
		if parsed := parseSsrModels(ssr); parsed != nil {
			online = mergeCatalogs(online, parsed)
		}
	}

	catalog := mergeCatalogs(online, baseline)
	if catalog == nil {
		c.mu.Lock()
		c.failedAt[sess.accountID] = now
		c.mu.Unlock()
		return nil, newCodeError(codeInvalidParam, "doubaowork model catalog unavailable")
	}
	c.rememberCatalog(sess.accountID, catalog, now)
	// If neither online source yielded anything, remember the failure so the
	// next calls within the backoff window skip the upstream entirely.
	if len(online) == 0 {
		c.mu.Lock()
		c.failedAt[sess.accountID] = now
		c.mu.Unlock()
	}
	return catalog, nil
}

func parseModelsFromBrief(payload map[string]any) map[string]doubaoModel {
	if data, ok := payload["modeSelectData"].(map[string]any); ok {
		if catalog := parseModelsFromData(data); catalog != nil {
			return catalog
		}
	}
	return parseModelsFromData(payload)
}

func (c *Client) rememberCatalog(accountID string, catalog map[string]doubaoModel, at time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.catalogs == nil {
		c.catalogs = map[string]catalogCache{}
	}
	c.catalogs[accountID] = catalogCache{catalog: catalog, at: at}
	delete(c.failedAt, accountID)
}

// Models implements providers.ModelCatalogProvider.
func (c *Client) Models(ctx context.Context, accountID string) ([]providers.ModelInfo, error) {
	sess := c.sessionFor(accountID)
	catalog, err := c.models(ctx, sess)
	if err != nil {
		return nil, err
	}
	return modelInfos(catalog), nil
}

// helpers for tolerant map/slice digging

func digMap(root map[string]any, keys ...string) map[string]any {
	current := root
	for _, key := range keys {
		next, ok := current[key].(map[string]any)
		if !ok {
			return nil
		}
		current = next
	}
	return current
}

func digSlice(root map[string]any, keys ...string) []any {
	if len(keys) == 0 {
		return nil
	}
	parent := root
	if len(keys) > 1 {
		parent = digMap(root, keys[:len(keys)-1]...)
	}
	if parent == nil {
		return nil
	}
	return asSlice(parent[keys[len(keys)-1]])
}

func asSlice(value any) []any {
	if slice, ok := value.([]any); ok {
		return slice
	}
	return nil
}

func stringSlice(value any) []string {
	if slice, ok := value.([]any); ok {
		out := make([]string, 0, len(slice))
		for _, item := range slice {
			out = append(out, asString(item))
		}
		return out
	}
	return nil
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func intOf(value any) int {
	switch typed := value.(type) {
	case int:
		return typed
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	case json.Number:
		parsed, _ := typed.Int64()
		return int(parsed)
	case string:
		return atoi(typed)
	default:
		return 0
	}
}

func firstOf(values ...any) any {
	for _, value := range values {
		if value != nil {
			return value
		}
	}
	return nil
}
