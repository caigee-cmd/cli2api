package doubaowork

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"strings"

	"github.com/caigee-cmd/cli2api/internal/accounts"
	"github.com/caigee-cmd/cli2api/internal/providers"
)

// readDoubaoTranslates turns the upstream block-patch event stream into OpenAI
// content/reasoning deltas. onDelta is called for every non-empty delta.
//
// The upstream does not send OpenAI SSE: text arrives as block patches
// (STREAM_CHUNK/CHUNK_DELTA/STREAM_MSG_NOTIFY/FULL_MSG_NOTIFY), so a block table
// keyed by messageID:blockID is maintained to reconstruct deltas. block_type
// 10040 (and its descendants via parent_id) is reasoning, everything else is
// content.
func readDoubaoTranslates(ctx context.Context, body io.Reader, sess *session, lease *lease, onDelta func(content, reasoning string) error) error {
	blocks := map[string]*blockState{}
	produced := false
	finished := false
	var lastMessageID string
	var lastBlock map[string]any

	reader := bufio.NewReaderSize(body, 64*1024)
	for {
		raw, ok, err := nextEvent(reader)
		if err != nil {
			return streamError("doubao event stream read failed")
		}
		if !ok {
			break
		}
		ctxErr := ctx.Err()
		if ctxErr != nil {
			return ctxErr
		}
		name, data := parseEvent(raw)
		if name == "gateway-error" {
			return newProviderError(502, data)
		}
		payload := safeJSON(data)
		if lease != nil {
			if err := lease.observe(name, payload); err != nil {
				return err
			}
		}
		if name == "STREAM_ERROR" || hasUpstreamError(payload) {
			return newProviderError(502, data)
		}
		if name == "SSE_REPLY_END" && intOf(payload["end_type"]) == 3 {
			finished = true
			break
		}
		emit := func(content, reasoning string) error {
			if content == "" && reasoning == "" {
				return nil
			}
			produced = true
			return onDelta(content, reasoning)
		}
		switch name {
		case "FULL_MSG_NOTIFY":
			if message, ok := payload["message"].(map[string]any); ok && intOf(message["user_type"]) == 2 {
				if err := applyBlocks(blocks, asString(message["message_id"]), blocksOf(message), 2, emit); err != nil {
					return err
				}
			}
		case "STREAM_MSG_NOTIFY":
			meta, _ := payload["meta"].(map[string]any)
			messageID := ""
			if meta != nil {
				messageID = asString(meta["message_id"])
			}
			if err := applyBlocks(blocks, messageID, blocksOf(payload["content"]), 2, emit); err != nil {
				return err
			}
		case "STREAM_CHUNK":
			messageID := asString(payload["message_id"])
			for _, patchRaw := range asSlice(payload["patch_op"]) {
				patch, ok := patchRaw.(map[string]any)
				if !ok || intOf(patch["patch_object"]) != 1 {
					continue
				}
				patchValue, _ := patch["patch_value"].(map[string]any)
				if patchValue == nil {
					continue
				}
				updates := asSlice(patchValue["content_block"])
				if err := applyBlocks(blocks, messageID, updates, intOf(patch["patch_type"]), emit); err != nil {
					return err
				}
				if len(updates) == 1 {
					if block, ok := updates[0].(map[string]any); ok {
						lastMessageID = messageID
						lastBlock = block
					}
				}
			}
		case "CHUNK_DELTA":
			if lastBlock == nil {
				return streamError("doubao delta missing block context")
			}
			blockCopy := map[string]any{}
			for k, v := range lastBlock {
				blockCopy[k] = v
			}
			blockCopy["patch_type"] = 1
			blockCopy["content"] = map[string]any{"text_block": payload}
			if err := applyBlocks(blocks, lastMessageID, []any{blockCopy}, 1, emit); err != nil {
				return err
			}
		}
	}

	if !finished {
		return streamError("doubao event stream ended before completion")
	}
	if !produced {
		return streamError("doubao event stream produced no content")
	}
	return nil
}

type blockState struct {
	blockType int
	parentID  string
	text      string
}

func applyBlocks(blocks map[string]*blockState, messageID string, updates []any, operation int, emit func(string, string) error) error {
	for _, updateRaw := range updates {
		block, ok := updateRaw.(map[string]any)
		if !ok {
			continue
		}
		blockID := asString(block["block_id"])
		if blockID == "" {
			return streamError("doubao content block missing id")
		}
		key := messageID + ":" + blockID
		prior := blocks[key]
		patchType := operation
		if value, exists := block["patch_type"]; exists {
			patchType = intOf(value)
		}
		isDeleted, _ := block["is_deleted"].(bool)
		if patchType == 3 || isDeleted {
			if prior != nil && prior.text != "" {
				return streamError("doubao retracted already-sent content")
			}
			delete(blocks, key)
			continue
		}
		if patchType != 1 && patchType != 2 {
			return streamError("doubao sent an unknown block operation")
		}
		current := &blockState{blockType: intOf(block["block_type"]), parentID: asString(block["parent_id"])}
		if prior != nil {
			current.text = prior.text
			if current.blockType == 0 {
				current.blockType = prior.blockType
			}
			if current.parentID == "" {
				current.parentID = prior.parentID
			}
		}
		blocks[key] = current

		text, hasText := blockText(block)
		if !hasText {
			continue
		}
		var next string
		if patchType == 1 {
			next = current.text + text
		} else {
			next = text
		}
		if !strings.HasPrefix(next, current.text) {
			return streamError("doubao rewrote already-sent content")
		}
		delta := next[len(current.text):]
		current.text = next
		if delta == "" {
			continue
		}
		reasoning := current.blockType == 10040
		parent := current
		seen := map[string]struct{}{}
		for parent.parentID != "" {
			if _, dup := seen[parent.parentID]; dup {
				return streamError("doubao content block parent cycle")
			}
			seen[parent.parentID] = struct{}{}
			parent = blocks[messageID+":"+parent.parentID]
			if parent == nil {
				break
			}
			if parent.blockType == 10040 {
				reasoning = true
			}
		}
		if reasoning {
			if err := emit("", delta); err != nil {
				return err
			}
		} else {
			if err := emit(delta, ""); err != nil {
				return err
			}
		}
	}
	return nil
}

// blockText extracts a text_block.text value from a block, reporting whether a
// text field was present.
func blockText(block map[string]any) (string, bool) {
	content, ok := block["content"].(map[string]any)
	if !ok {
		return "", false
	}
	textBlock, ok := content["text_block"].(map[string]any)
	if !ok {
		return "", false
	}
	text, ok := textBlock["text"].(string)
	if !ok {
		return "", false
	}
	return text, true
}

// blocksOf extracts the block array from a message holder, tolerating the
// several shapes the upstream uses.
func blocksOf(holder any) []any {
	if holder == nil {
		return nil
	}
	object, ok := holder.(map[string]any)
	if !ok {
		if text, ok := holder.(string); ok {
			return textBlocks(text)
		}
		return nil
	}
	if arr, ok := object["content_block"].([]any); ok {
		return arr
	}
	raw := object["content"]
	if arr, ok := raw.([]any); ok {
		return arr
	}
	if nested, ok := raw.(map[string]any); ok {
		if arr, ok := nested["content_block"].([]any); ok {
			return arr
		}
		return nil
	}
	if text, ok := raw.(string); ok {
		return textBlocks(text)
	}
	return nil
}

func textBlocks(text string) []any {
	trimmed := strings.TrimSpace(text)
	if strings.HasPrefix(trimmed, "[") {
		var arr []any
		if json.Unmarshal([]byte(trimmed), &arr) == nil {
			return arr
		}
	}
	if trimmed == "" {
		return nil
	}
	return []any{map[string]any{"content": map[string]any{"text_block": map[string]any{"text": text}}}}
}

func hasUpstreamError(payload map[string]any) bool {
	if len(payload) == 0 {
		return false
	}
	if _, ok := payload["error_code"]; ok {
		return true
	}
	if details, ok := payload["error_details"].(map[string]any); ok {
		if value, ok := details["has_error"].(bool); ok && value {
			return true
		}
	}
	return false
}

func safeJSON(text string) map[string]any {
	text = strings.TrimSpace(text)
	if text == "" {
		return map[string]any{}
	}
	var out map[string]any
	if json.Unmarshal([]byte(text), &out) != nil {
		return map[string]any{}
	}
	return out
}

func parseEvent(raw string) (string, string) {
	name := "message"
	dataLines := make([]string, 0)
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSuffix(line, "\r")
		if strings.HasPrefix(line, "event:") {
			name = strings.TrimSpace(line[len("event:"):])
		} else if strings.HasPrefix(line, "data:") {
			dataLines = append(dataLines, strings.TrimSpace(line[len("data:"):]))
		}
	}
	return name, strings.Join(dataLines, "\n")
}

// nextEvent reads one SSE event (lines up to a blank line).
func nextEvent(reader *bufio.Reader) (string, bool, error) {
	var builder strings.Builder
	sawAny := false
	for {
		line, err := readSSELine(reader)
		if err == io.EOF {
			if builder.Len() > 0 {
				return builder.String(), true, nil
			}
			return "", false, nil
		}
		if err != nil {
			return "", false, err
		}
		if line == "" {
			if sawAny {
				return builder.String(), true, nil
			}
			continue
		}
		sawAny = true
		builder.WriteString(line)
		builder.WriteByte('\n')
	}
}

// readSSELine reads a line terminated by \n, \r\n, or \r.
func readSSELine(reader *bufio.Reader) (string, error) {
	var builder strings.Builder
	for {
		c, err := reader.ReadByte()
		if err != nil {
			return builder.String(), err
		}
		switch c {
		case '\n':
			return builder.String(), nil
		case '\r':
			if next, peekErr := reader.Peek(1); peekErr == nil && len(next) == 1 && next[0] == '\n' {
				_, _ = reader.ReadByte()
			}
			return builder.String(), nil
		default:
			builder.WriteByte(c)
		}
	}
}

func streamError(message string) *providers.Error {
	return &providers.Error{Kind: accounts.KindUnavailable, Status: 502, Message: message}
}
