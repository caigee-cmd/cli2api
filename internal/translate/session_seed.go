package translate

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"sort"
	"strings"
)

type contentSessionFingerprint struct {
	Model     string                  `json:"model"`
	Tools     string                  `json:"tools,omitempty"`
	Prefix    []sessionFingerprintMsg `json:"prefix,omitempty"`
	FirstUser sessionFingerprintMsg   `json:"first_user"`
}

type sessionFingerprintMsg struct {
	Role    string               `json:"role"`
	Content []sessionContentPart `json:"content"`
}

type sessionContentPart struct {
	Type  string `json:"type"`
	Text  string `json:"text,omitempty"`
	Image string `json:"image,omitempty"`
}

// ContentSessionSeed builds a stable conversation fingerprint from fields that
// stay constant across turns: model, tool definitions, the leading
// system/developer prefix, and the first user message. Later turns append
// assistant/user messages and must not change the seed, or prompt-cache
// affinity would break.
//
// The fingerprint is a hash of role-tagged structured fields, including image
// identifiers on the first user message. An empty result means there is no
// user anchor; callers should leave the request on ordinary pool routing.
func ContentSessionSeed(req ChatRequest) string {
	fingerprint, ok := buildContentSessionFingerprint(req)
	if !ok {
		return ""
	}
	raw, err := json.Marshal(fingerprint)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func buildContentSessionFingerprint(req ChatRequest) (contentSessionFingerprint, bool) {
	fingerprint := contentSessionFingerprint{
		Model: strings.ToLower(strings.TrimSpace(req.Model)),
		Tools: compactSessionJSON(NormalizeToolsOrder(req.Tools)),
	}
	systemPrefixOpen := true
	sawFirstUser := false
	for _, message := range req.Messages {
		role := strings.ToLower(strings.TrimSpace(message.Role))
		switch role {
		case "system", "developer":
			if !systemPrefixOpen {
				continue
			}
			parts := sessionContentParts(message.Content)
			if len(parts) == 0 {
				continue
			}
			fingerprint.Prefix = append(fingerprint.Prefix, sessionFingerprintMsg{Role: role, Content: parts})
		case "user":
			systemPrefixOpen = false
			if sawFirstUser {
				continue
			}
			sawFirstUser = true
			fingerprint.FirstUser = sessionFingerprintMsg{Role: "user", Content: sessionContentParts(message.Content)}
		default:
			systemPrefixOpen = false
		}
	}
	if len(fingerprint.FirstUser.Content) == 0 {
		return contentSessionFingerprint{}, false
	}
	return fingerprint, true
}

func sessionContentParts(content any) []sessionContentPart {
	switch value := content.(type) {
	case nil:
		return nil
	case string:
		text := strings.TrimSpace(value)
		if text == "" {
			return nil
		}
		return []sessionContentPart{{Type: "text", Text: text}}
	case []any:
		parts := make([]sessionContentPart, 0, len(value))
		for _, item := range value {
			parts = append(parts, sessionContentParts(item)...)
		}
		return parts
	case map[string]any:
		if part, ok := sessionPartFromMap(value); ok {
			return []sessionContentPart{part}
		}
		return nil
	case map[string]string:
		converted := make(map[string]any, len(value))
		for key, item := range value {
			converted[key] = item
		}
		if part, ok := sessionPartFromMap(converted); ok {
			return []sessionContentPart{part}
		}
		return nil
	case json.RawMessage:
		trimmed := bytes.TrimSpace(value)
		if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
			return nil
		}
		var decoded any
		if err := json.Unmarshal(trimmed, &decoded); err != nil {
			return nil
		}
		return sessionContentParts(decoded)
	default:
		text := strings.TrimSpace(ContentToString(value))
		if text == "" || text == "null" || text == "{}" || text == "[]" {
			return nil
		}
		return []sessionContentPart{{Type: "text", Text: text}}
	}
}

func sessionPartFromMap(block map[string]any) (sessionContentPart, bool) {
	if block == nil {
		return sessionContentPart{}, false
	}
	partType := strings.ToLower(strings.TrimSpace(anyString(block["type"])))
	switch partType {
	case "text", "input_text", "output_text":
		text := strings.TrimSpace(anyString(block["text"]))
		if text == "" {
			return sessionContentPart{}, false
		}
		return sessionContentPart{Type: "text", Text: text}, true
	case "image_url", "image":
		if image := sessionImageFingerprint(block); image != "" {
			return sessionContentPart{Type: "image", Image: image}, true
		}
		return sessionContentPart{}, false
	}
	if image := sessionImageFingerprint(block); image != "" {
		return sessionContentPart{Type: "image", Image: image}, true
	}
	text := strings.TrimSpace(anyString(block["text"]))
	if text != "" {
		return sessionContentPart{Type: "text", Text: text}, true
	}
	return sessionContentPart{}, false
}

func sessionImageFingerprint(block map[string]any) string {
	if url := imageURLFromAny(block["image_url"]); url != "" {
		return hashSessionImage(url, "", "")
	}
	if url := strings.TrimSpace(anyString(block["url"])); url != "" && lookupMap(block, "image_url") == nil && lookupMap(block, "source") == nil {
		if strings.ToLower(strings.TrimSpace(anyString(block["type"]))) == "image" || looksLikeImageURL(url) {
			return hashSessionImage(url, "", "")
		}
	}
	if source := lookupMap(block, "source"); source != nil {
		url := strings.TrimSpace(anyString(source["url"]))
		mediaType := strings.TrimSpace(anyString(source["media_type"]))
		data := strings.TrimSpace(anyString(source["data"]))
		if url != "" || data != "" {
			return hashSessionImage(url, mediaType, data)
		}
	}
	return ""
}

func imageURLFromAny(value any) string {
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	case map[string]any:
		if url := strings.TrimSpace(anyString(typed["url"])); url != "" {
			return url
		}
		return strings.TrimSpace(anyString(typed["image_url"]))
	case map[string]string:
		if url := strings.TrimSpace(typed["url"]); url != "" {
			return url
		}
		return strings.TrimSpace(typed["image_url"])
	default:
		return ""
	}
}

func lookupMap(block map[string]any, key string) map[string]any {
	switch nested := block[key].(type) {
	case map[string]any:
		return nested
	case map[string]string:
		out := make(map[string]any, len(nested))
		for nestedKey, nestedValue := range nested {
			out[nestedKey] = nestedValue
		}
		return out
	default:
		return nil
	}
}

func looksLikeImageURL(url string) bool {
	lower := strings.ToLower(url)
	return strings.HasPrefix(lower, "data:image/") || strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://")
}

func hashSessionImage(url, mediaType, data string) string {
	url = strings.TrimSpace(url)
	mediaType = strings.TrimSpace(mediaType)
	data = strings.TrimSpace(data)
	if url == "" && data == "" {
		return ""
	}
	if url != "" && data == "" && !strings.HasPrefix(strings.ToLower(url), "data:") {
		return "url:" + url
	}
	sum := sha256.Sum256([]byte(url + "\x00" + mediaType + "\x00" + data))
	return "data:" + hex.EncodeToString(sum[:])
}

func anyString(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case json.RawMessage:
		var text string
		if json.Unmarshal(typed, &text) == nil {
			return text
		}
		return strings.Trim(string(bytes.TrimSpace(typed)), `"`)
	default:
		return ""
	}
}

func compactSessionJSON(raw json.RawMessage) string {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) || bytes.Equal(trimmed, []byte("[]")) {
		return ""
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, trimmed); err != nil {
		return string(trimmed)
	}
	return compact.String()
}

// PrefixSessionSeed is the cache-affine part of ContentSessionSeed: model,
// tools and the leading system/developer prefix only. It deliberately excludes
// the first user message so workbuddy2api-hub-style sharding does not scatter
// one (key, model, system, tools) tuple across the whole account pool.
func PrefixSessionSeed(req ChatRequest) string {
	parts := []string{
		strings.ToLower(strings.TrimSpace(req.Model)),
		compactSessionJSON(NormalizeToolsOrder(req.Tools)),
	}
	for _, message := range req.Messages {
		role := strings.ToLower(strings.TrimSpace(message.Role))
		if role != "system" && role != "developer" {
			break
		}
		for _, part := range sessionContentParts(message.Content) {
			parts = append(parts, role, part.Type, part.Text, part.Image)
		}
	}
	raw, err := json.Marshal(parts)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// SessionSeedComponents exposes the per-component hashes behind
// ContentSessionSeed. Routing only needs the combined hash; diagnostics need
// this split to answer "which part of the request moved", because a chat that
// silently re-anchors every turn is usually carrying a volatile timestamp in
// its system prefix or reordering its tool list.
type SessionSeedComponents struct {
	Model     string `json:"model"`
	Tools     string `json:"tools"`
	Prefix    string `json:"prefix"`
	FirstUser string `json:"first_user"`
}

// SeedComponents returns short component hashes. Zero values mean the component
// is absent (for example no system prefix at all).
func SeedComponents(req ChatRequest) SessionSeedComponents {
	fingerprint, ok := buildContentSessionFingerprint(req)
	if !ok {
		return SessionSeedComponents{}
	}
	out := SessionSeedComponents{
		Model: shortHash(fingerprint.Model),
		Tools: shortHash(fingerprint.Tools),
	}
	if len(fingerprint.Prefix) > 0 {
		if raw, err := json.Marshal(fingerprint.Prefix); err == nil {
			out.Prefix = shortHash(string(raw))
		}
	}
	if raw, err := json.Marshal(fingerprint.FirstUser); err == nil {
		out.FirstUser = shortHash(string(raw))
	}
	return out
}

func shortHash(value string) string {
	if strings.TrimSpace(value) == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:4])
}

// NormalizeToolsOrder rewrites a tools array into a stable, name-ordered form.
// Tool definitions are a set: their order carries no meaning to the model but
// changes both the session fingerprint and the upstream cached prefix. Clients
// such as Claude Code and Cline rebuild the array per turn, which would
// otherwise scatter one chat across accounts and defeat prompt caching.
//
// A payload without names, an empty array, or invalid JSON is returned
// unchanged, so callers can apply this unconditionally to the fingerprint.
func NormalizeToolsOrder(raw json.RawMessage) json.RawMessage {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return raw
	}
	var items []json.RawMessage
	if err := json.Unmarshal(trimmed, &items); err != nil || len(items) < 2 {
		return raw
	}
	type keyedTool struct {
		key  string
		item json.RawMessage
	}
	keyed := make([]keyedTool, len(items))
	sorted := true
	for i, item := range items {
		keyed[i] = keyedTool{key: toolSortKey(item), item: item}
		if i > 0 && keyed[i-1].key > keyed[i].key {
			sorted = false
		}
	}
	if sorted {
		return raw
	}
	sort.SliceStable(keyed, func(i, j int) bool { return keyed[i].key < keyed[j].key })
	out := make([]json.RawMessage, len(keyed))
	for i := range keyed {
		out[i] = keyed[i].item
	}
	encoded, err := json.Marshal(out)
	if err != nil {
		return raw
	}
	return encoded
}

// toolSortKey is the tool's name when it has one, plus the compact body so two
// tools that share a name still order deterministically. Unnamed tools sort
// last under a marker no real name can produce.
func toolSortKey(raw json.RawMessage) string {
	var probe struct {
		Name     string `json:"name"`
		Function struct {
			Name string `json:"name"`
		} `json:"function"`
	}
	name := ""
	if err := json.Unmarshal(raw, &probe); err == nil {
		name = strings.TrimSpace(probe.Name)
		if name == "" {
			name = strings.TrimSpace(probe.Function.Name)
		}
	}
	if name == "" {
		return "\uffff" + compactSessionJSON(raw)
	}
	return name + "\x00" + compactSessionJSON(raw)
}

// NormalizeToolsForPayload returns the payload tools array. Ordering is only
// rewritten when the operator opts in: reordering the outbound array is a
// semantic change some models notice, so it ships behind
// CLI2API_TOOLS_NORMALIZE and the fingerprint normalization alone (which is
// always on) is the safe default.
func NormalizeToolsForPayload(raw json.RawMessage) json.RawMessage {
	if !payloadToolsNormalizeOn() {
		return raw
	}
	return NormalizeToolsOrder(raw)
}

func payloadToolsNormalizeOn() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("CLI2API_TOOLS_NORMALIZE"))) {
	case "1", "true", "on", "yes":
		return true
	default:
		return false
	}
}
