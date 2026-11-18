package kiro

import "crypto/rand"

// legacyPayload is generateAssistantResponse. API keys post it as AWS JSON 1.0
// to the CLI runtime; IDE accounts post it to the Amazon Q and CodeWhisperer
// hosts. Tool results travel both as structured toolResults and as a short
// continuation in the user text, which is what that API requires.
type legacyPayload struct {
	ConversationState struct {
		ChatTriggerType string `json:"chatTriggerType"`
		ConversationID  string `json:"conversationId"`
		CurrentMessage  struct {
			UserInputMessage legacyUser `json:"userInputMessage"`
		} `json:"currentMessage"`
		History []legacyHistory `json:"history,omitempty"`
	} `json:"conversationState"`
	ProfileArn string `json:"profileArn,omitempty"`
}

type legacyUser struct {
	Content                 string       `json:"content"`
	ModelID                 string       `json:"modelId,omitempty"`
	Origin                  string       `json:"origin"`
	Images                  []legacyBlob `json:"images,omitempty"`
	UserInputMessageContext *legacyCtx   `json:"userInputMessageContext,omitempty"`
}

type legacyBlob struct {
	Format string `json:"format"`
	Source struct {
		Bytes string `json:"bytes"`
	} `json:"source"`
}

type legacyCtx struct {
	Tools       []legacyTool   `json:"tools,omitempty"`
	ToolResults []legacyResult `json:"toolResults,omitempty"`
}

type legacyTool struct {
	ToolSpecification struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		InputSchema struct {
			JSON any `json:"json"`
		} `json:"inputSchema"`
	} `json:"toolSpecification"`
}

type legacyResult struct {
	ToolUseID string              `json:"toolUseId"`
	Content   []map[string]string `json:"content"`
	Status    string              `json:"status"`
}

type legacyHistory struct {
	UserInputMessage         *legacyUser      `json:"userInputMessage,omitempty"`
	AssistantResponseMessage *legacyAssistant `json:"assistantResponseMessage,omitempty"`
}

type legacyAssistant struct {
	Content  string          `json:"content"`
	ToolUses []legacyToolUse `json:"toolUses,omitempty"`
}

type legacyToolUse struct {
	ToolUseID string         `json:"toolUseId"`
	Name      string         `json:"name"`
	Input     map[string]any `json:"input"`
}

type legacyEndpoint struct {
	Name      string
	URL       string
	Origin    string
	AmzTarget string
	JSON10    bool
}

func (c conversation) legacyBody(origin string, apiKey bool) legacyPayload {
	var body legacyPayload
	body.ConversationState.ChatTriggerType = "MANUAL"
	body.ConversationState.ConversationID = newConversationID()
	if !apiKey {
		body.ProfileArn = c.ProfileArn
	}
	for _, item := range c.History {
		switch {
		case item.User != nil:
			user := item.User.legacy("")
			body.ConversationState.History = append(body.ConversationState.History, legacyHistory{UserInputMessage: &user})
		case item.Assistant != nil:
			assistant := item.Assistant.legacy()
			body.ConversationState.History = append(body.ConversationState.History, legacyHistory{AssistantResponseMessage: &assistant})
		}
	}
	current := c.Current.legacy(c.Model)
	current.Origin = origin
	if ctx := toolsContext(c.Tools, c.Current.Results); ctx != nil {
		current.UserInputMessageContext = ctx
		// generateAssistantResponse rejects a user turn with no text, so the
		// result is repeated as a short continuation beside the structured one.
		if text := continuation(c.Current.Results); text != "" {
			current.Content = joinText(current.Content, text)
		}
	}
	body.ConversationState.CurrentMessage.UserInputMessage = current
	return body
}

func (u userTurn) legacy(model string) legacyUser {
	user := legacyUser{Content: u.Text, ModelID: model, Origin: Origin}
	for _, image := range u.Images {
		blob := legacyBlob{Format: image.Format}
		blob.Source.Bytes = image.Bytes
		user.Images = append(user.Images, blob)
	}
	return user
}

func (a assistantTurn) legacy() legacyAssistant {
	content := a.Text
	for _, block := range a.Reasoning {
		if block.Text != "" {
			content = joinText("<thinking>"+block.Text+"</thinking>", content)
		}
	}
	assistant := legacyAssistant{Content: content}
	for _, call := range a.ToolUses {
		input := call.Input
		if input == nil {
			input = map[string]any{}
		}
		assistant.ToolUses = append(assistant.ToolUses, legacyToolUse{ToolUseID: call.ID, Name: call.Name, Input: input})
	}
	return assistant
}

func toolsContext(tools []toolSpec, results []toolResult) *legacyCtx {
	if len(tools) == 0 && len(results) == 0 {
		return nil
	}
	ctx := &legacyCtx{}
	for _, tool := range tools {
		item := legacyTool{}
		item.ToolSpecification.Name = tool.Name
		item.ToolSpecification.Description = tool.Description
		item.ToolSpecification.InputSchema.JSON = tool.Parameters
		ctx.Tools = append(ctx.Tools, item)
	}
	for _, result := range results {
		ctx.ToolResults = append(ctx.ToolResults, legacyResult{
			ToolUseID: result.ID, Status: "success", Content: []map[string]string{{"text": result.Output}},
		})
	}
	return ctx
}

func legacyEndpoints(cred Credential) []legacyEndpoint {
	if cred.isAPIKey() {
		return []legacyEndpoint{{
			Name: "Kiro CLI", URL: chatBaseFor(cred) + "/",
			Origin: "KIRO_CLI", AmzTarget: "AmazonCodeWhispererStreamingService.GenerateAssistantResponse", JSON10: true,
		}}
	}
	q := restBase(cred) + "/generateAssistantResponse"
	cw := codeWhispererBase(cred) + "/generateAssistantResponse"
	return []legacyEndpoint{
		{Name: "Kiro IDE", URL: q, Origin: Origin},
		{Name: "CodeWhisperer", URL: cw, Origin: Origin, AmzTarget: "AmazonCodeWhispererStreamingService.GenerateAssistantResponse"},
		{Name: "AmazonQ", URL: q, Origin: Origin, AmzTarget: "AmazonQDeveloperStreamingService.SendMessage"},
	}
}

func trimSpaceString(value string) string { return string(trimSpace([]byte(value))) }

func joinText(parts ...string) string {
	out := ""
	for _, part := range parts {
		if part == "" {
			continue
		}
		if out != "" {
			out += "\n\n"
		}
		out += part
	}
	return out
}

func joinLines(parts []string) string {
	out := ""
	for _, part := range parts {
		if out != "" {
			out += "\n"
		}
		out += part
	}
	return out
}

// newConversationID is a random id the legacy body always carries. Kiro does
// not use it to resume a conversation; history in the body does that.
func newConversationID() string {
	var buf [16]byte
	_, _ = rand.Read(buf[:])
	buf[6] = (buf[6] & 0x0f) | 0x40
	buf[8] = (buf[8] & 0x3f) | 0x80
	const hex = "0123456789abcdef"
	out := make([]byte, 36)
	pos := 0
	for i, b := range buf {
		if i == 4 || i == 6 || i == 8 || i == 10 {
			out[pos] = '-'
			pos++
		}
		out[pos] = hex[b>>4]
		out[pos+1] = hex[b&0x0f]
		pos += 2
	}
	return string(out)
}
