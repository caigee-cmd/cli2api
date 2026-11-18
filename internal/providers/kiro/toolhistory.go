package kiro

import (
	"fmt"
	"regexp"
	"strings"
)

// Tool history follows kiro-reverse-api. A toolUse is kept structured only when
// the next user turn answers exactly those calls and every name is declared in
// this request. Anything else is narrated as text: Kiro answers
// TOOL_CONFIG_MISSING when a historical call names a tool the request did not
// declare, and an unpaired call has no result the model can continue from.

const (
	toolResultPrefix    = "Tool results:"
	toolHistoryFallback = "[No new user text in this turn. Continue from the conversation above.]"
	maxToolContinuation = 4000
)

var calledToolPattern = regexp.MustCompile(`\[Called tool [^\]]*\]`)

// attachToolHistory keeps matched historical pairs structured and narrates the
// rest. The current turn's results stay structured only when they answer the
// last assistant call in full.
func attachToolHistory(c *conversation) {
	declared := map[string]string{}
	if c.ToolChoice != "none" {
		for _, tool := range c.Tools {
			declared[tool.Name] = tool.Name
			if tool.ClientName != "" {
				declared[tool.ClientName] = tool.Name
			}
		}
	}
	renameCalls(c, declared)
	currentIDs := resultIDs(c.Current.Results)
	keep := map[int]bool{}
	if len(declared) > 0 {
		keep = structuredPairs(c.History, declared)
	}
	active := len(c.History) > 0 && callsCovered(c.History[len(c.History)-1].Assistant, currentIDs)
	c.History = narrateUnkept(c.History, keep, active)
	if len(c.Current.Results) == 0 {
		return
	}
	if active && c.ToolChoice != "none" {
		// CreateResponse carries the result as function_call_output, so a
		// continuation here would only duplicate it. The legacy path adds its own.
		return
	}
	c.Current.Text = joinHistoryText(c.Current.Text, narrateResults(c.Current.Results, namesIn(c.History)))
	c.Current.Results = nil
}

// flattenToolHistory rewrites every structured call as narration. It reports
// whether there was anything to flatten, so a 400 retry can skip a payload
// that was already narrated.
func flattenToolHistory(c *conversation) bool {
	if !hasStructuredHistory(c) {
		return false
	}
	c.History = narrateUnkept(c.History, nil, false)
	if len(c.Current.Results) > 0 {
		c.Current.Text = joinHistoryText(c.Current.Text, narrateResults(c.Current.Results, nil))
		c.Current.Results = nil
	}
	return true
}

// renameCalls rewrites historical calls onto the name this request declared
// upstream. A client replays "Read"; Kiro was offered "read".
func renameCalls(c *conversation, declared map[string]string) {
	for i := range c.History {
		assistant := c.History[i].Assistant
		if assistant == nil {
			continue
		}
		for j := range assistant.ToolUses {
			if upstream := declared[assistant.ToolUses[j].Name]; upstream != "" {
				assistant.ToolUses[j].Name = upstream
			}
		}
	}
}

func hasStructuredHistory(c *conversation) bool {
	for _, turn := range c.History {
		if turn.Assistant != nil && len(turn.Assistant.ToolUses) > 0 {
			return true
		}
		if turn.User != nil && len(turn.User.Results) > 0 {
			return true
		}
	}
	return len(c.Current.Results) > 0
}

// structuredPairs marks an assistant turn and the user turn right after it when
// the results answer exactly those calls and every name is declared.
func structuredPairs(history []historyTurn, declared map[string]string) map[int]bool {
	keep := map[int]bool{}
	for i := 0; i+1 < len(history); i++ {
		assistant := history[i].Assistant
		user := history[i+1].User
		if assistant == nil || user == nil || !pairMatches(assistant.ToolUses, user.Results, declared) {
			continue
		}
		keep[i], keep[i+1] = true, true
	}
	return keep
}

func pairMatches(uses []toolCall, results []toolResult, declared map[string]string) bool {
	if len(uses) == 0 || len(uses) != len(results) {
		return false
	}
	ids := map[string]bool{}
	for _, call := range uses {
		if call.ID == "" || ids[call.ID] || declared[call.Name] == "" {
			return false
		}
		ids[call.ID] = true
	}
	for _, result := range results {
		if !ids[result.ID] {
			return false
		}
		delete(ids, result.ID)
	}
	return len(ids) == 0
}

func callsCovered(assistant *assistantTurn, ids map[string]bool) bool {
	if assistant == nil || len(assistant.ToolUses) == 0 || len(ids) == 0 {
		return false
	}
	for _, call := range assistant.ToolUses {
		if !ids[call.ID] {
			return false
		}
	}
	return true
}

func resultIDs(results []toolResult) map[string]bool {
	if len(results) == 0 {
		return nil
	}
	ids := make(map[string]bool, len(results))
	for _, result := range results {
		if result.ID != "" {
			ids[result.ID] = true
		}
	}
	return ids
}

// narrateUnkept turns tool turns that are not in keep into text. active is the
// last assistant turn the current message answers, which stays structured.
func narrateUnkept(history []historyTurn, keep map[int]bool, active bool) []historyTurn {
	names := namesIn(history)
	out := make([]historyTurn, 0, len(history))
	for i, turn := range history {
		if turn.Assistant != nil && len(turn.Assistant.ToolUses) > 0 && !keep[i] && !(active && i == len(history)-1) {
			turn.Assistant.Text = stripCalledTool(turn.Assistant.Text)
			turn.Assistant.ToolUses = nil
		}
		if turn.User != nil && len(turn.User.Results) > 0 && !keep[i] {
			turn.User.Text = joinHistoryText(turn.User.Text, narrateResults(turn.User.Results, names))
			turn.User.Results = nil
		}
		if turn.User != nil && strings.TrimSpace(turn.User.Text) == "" && len(turn.User.Images) == 0 && len(turn.User.Documents) == 0 && len(turn.User.Results) == 0 {
			turn.User.Text = toolHistoryFallback
		}
		if turn.Assistant != nil && len(turn.Assistant.ToolUses) == 0 && len(turn.Assistant.Reasoning) == 0 {
			text := strings.TrimSpace(turn.Assistant.Text)
			if text == "" || text == "." || text == toolHistoryFallback {
				continue
			}
		}
		out = append(out, turn)
	}
	return trimLeadingAssistant(out)
}

func trimLeadingAssistant(history []historyTurn) []historyTurn {
	for len(history) > 0 && history[0].Assistant != nil && history[0].User == nil {
		history = history[1:]
	}
	return history
}

func namesIn(history []historyTurn) map[string]string {
	names := map[string]string{}
	for _, turn := range history {
		if turn.Assistant == nil {
			continue
		}
		for _, call := range turn.Assistant.ToolUses {
			if call.ID != "" && call.Name != "" {
				names[call.ID] = call.Name
			}
		}
	}
	return names
}

func narrateResults(results []toolResult, names map[string]string) string {
	if len(results) == 0 {
		return ""
	}
	parts := make([]string, 0, len(results))
	for _, result := range results {
		body := strings.TrimSpace(result.Output)
		if body == "" {
			body = "(no output)"
		}
		if name := names[result.ID]; name != "" {
			parts = append(parts, fmt.Sprintf("[%s] %s", name, body))
			continue
		}
		parts = append(parts, body)
	}
	return toolResultPrefix + "\n\n" + strings.Join(parts, "\n\n")
}

func continuation(results []toolResult) string {
	parts := make([]string, 0, len(results))
	for _, result := range results {
		if text := strings.TrimSpace(result.Output); text != "" {
			parts = append(parts, text)
		}
	}
	if len(parts) == 0 {
		return toolHistoryFallback
	}
	joined := toolResultPrefix + "\n\n" + strings.Join(parts, "\n\n")
	if len(joined) <= maxToolContinuation {
		return joined
	}
	cut := maxToolContinuation
	for cut > 0 && !isRuneStart(joined[cut]) {
		cut--
	}
	if cut == 0 {
		return toolHistoryFallback
	}
	return joined[:cut]
}

func isRuneStart(b byte) bool { return b&0xC0 != 0x80 }

func joinHistoryText(existing, extra string) string {
	existing, extra = strings.TrimSpace(existing), strings.TrimSpace(extra)
	switch {
	case existing != "" && extra != "":
		return existing + "\n\n" + extra
	case extra != "":
		return extra
	default:
		return existing
	}
}

func stripCalledTool(content string) string {
	if !strings.Contains(content, "[Called tool ") {
		return content
	}
	cleaned := calledToolPattern.ReplaceAllString(content, "")
	cleaned = regexp.MustCompile(`\n{3,}`).ReplaceAllString(cleaned, "\n\n")
	return strings.TrimSpace(cleaned)
}
