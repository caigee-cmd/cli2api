package devin

import (
	"bytes"
	"compress/gzip"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"

	apipb "github.com/caigee-cmd/cli2api/internal/providers/devin/devinpb/api_server_pb"
	chatpb "github.com/caigee-cmd/cli2api/internal/providers/devin/devinpb/chat_pb"
	commonpb "github.com/caigee-cmd/cli2api/internal/providers/devin/devinpb/codeium_common_pb"
	cortexpb "github.com/caigee-cmd/cli2api/internal/providers/devin/devinpb/cortex_pb"
	"google.golang.org/protobuf/proto"
)

// Tool is a tool definition in GetChatMessageRequest.
type Tool struct {
	Name        string
	Description string
	Parameters  []byte
}

// ToolCall is a completed tool call on an assistant prompt.
type ToolCall struct {
	ID        string
	Name      string
	Arguments string
}

// ToolCallDelta is a streaming tool-call chunk from response field 6.
type ToolCallDelta struct {
	ID        string
	Name      string
	Arguments string
}

// Image is an image attachment on a prompt.
type Image struct {
	Base64Data string
	MimeType   string
}

// Prompt is one history turn (repeated field 3).
type Prompt struct {
	MessageID     string
	Source        int // 1=user, 2=assistant, 4=tool
	Content       string
	Images        []Image
	ToolCalls     []ToolCall
	ToolCallID    string
	Thinking      string
	Signature     []byte
	SignatureType string
}

// Usage captures token accounting from response field 7.
type Usage struct {
	PromptTokens     int64
	CompletionTokens int64
	CachedTokens     int64
	CacheWriteTokens int64
	RequestID        string
	ModelName        string
	Headers          map[string]string
}

// FrameResult is decoded content from one Connect-proto data frame.
type FrameResult struct {
	OutputID       string
	Timestamp      uint64
	ContentText    string
	DeltaTokens    uint64
	StopReason     uint64
	ToolCallDeltas []ToolCallDelta
	ThinkingText   string
	DeltaSignature []byte
	DeltaSigType   string
	Latency        float64
	MessageID      string
	Usage          *Usage
}

func GenerateDeviceFingerprint(seed string) string {
	if seed == "" {
		var b [FingerprintHexLen / 2]byte
		if _, err := rand.Read(b[:]); err == nil {
			return hex.EncodeToString(b[:])
		}
		seed = randomHex(16)
	}
	var sb strings.Builder
	counter := 0
	for sb.Len() < FingerprintHexLen {
		h := sha256.Sum256([]byte(fmt.Sprintf("%s-%d", seed, counter)))
		sb.WriteString(hex.EncodeToString(h[:]))
		counter++
	}
	return sb.String()[:FingerprintHexLen]
}

func GenerateSentryTrace() string {
	var b [24]byte
	if _, err := rand.Read(b[:]); err != nil {
		return randomHex(16) + "-" + randomHex(8) + "-1"
	}
	return hex.EncodeToString(b[:16]) + "-" + hex.EncodeToString(b[16:24]) + "-1"
}

var (
	sessionTurnMu sync.Mutex
	sessionTurns  = map[string]*atomic.Uint64{}
)

// NextSessionTurnIndex returns the next 0-based request ordinal for a session.
func NextSessionTurnIndex(sessionID string) int {
	cleanID := strings.TrimSpace(sessionID)
	if cleanID == "" {
		return 0
	}
	sessionTurnMu.Lock()
	counter, ok := sessionTurns[cleanID]
	if !ok {
		counter = &atomic.Uint64{}
		sessionTurns[cleanID] = counter
	}
	sessionTurnMu.Unlock()
	return int(counter.Add(1) - 1)
}

func WrapConnectEnvelope(protoBytes []byte) []byte {
	return WrapConnectEnvelopeWithFlag(ConnectFlagData, protoBytes)
}

func WrapConnectEnvelopeWithFlag(flag byte, protoBytes []byte) []byte {
	header := make([]byte, 5, 5+len(protoBytes))
	header[0] = flag
	binary.BigEndian.PutUint32(header[1:5], uint32(len(protoBytes)))
	return append(header, protoBytes...)
}

func ReadConnectFrame(r io.Reader) (flag byte, payload []byte, err error) {
	header := make([]byte, 5)
	if _, err := io.ReadFull(r, header); err != nil {
		return 0, nil, err
	}
	flag = header[0]
	if flag != ConnectFlagData && flag != ConnectFlagCompressed && flag != ConnectFlagEndStream && flag != (ConnectFlagCompressed|ConnectFlagEndStream) {
		return flag, nil, fmt.Errorf("invalid connect frame flag: 0x%02x", flag)
	}
	length := binary.BigEndian.Uint32(header[1:5])
	if length > maxConnectFrameSize {
		return flag, nil, fmt.Errorf("connect frame length %d exceeds maximum", length)
	}
	payload = make([]byte, length)
	if _, err := io.ReadFull(r, payload); err != nil {
		return 0, nil, err
	}
	if flag&ConnectFlagCompressed != 0 {
		gz, errGz := gzip.NewReader(bytes.NewReader(payload))
		if errGz != nil {
			return flag, nil, fmt.Errorf("decompress gzip connect frame: %w", errGz)
		}
		defer gz.Close()
		decomp, errRead := io.ReadAll(io.LimitReader(gz, maxDecompressedFrameSize+1))
		if errRead != nil {
			return flag, nil, fmt.Errorf("read decompressed connect frame: %w", errRead)
		}
		if len(decomp) > maxDecompressedFrameSize {
			return flag, nil, fmt.Errorf("decompressed frame size exceeds maximum")
		}
		payload = decomp
	}
	return flag, payload, nil
}

func clientMetadata(sessionToken, deviceSeed, osName string) *commonpb.Metadata {
	return metadataWithFingerprint(sessionToken, GenerateDeviceFingerprint(deviceSeed), osName)
}

func metadataWithFingerprint(sessionToken, deviceFingerprint, osName string) *commonpb.Metadata {
	if osName == "" {
		osName = runtime.GOOS
	}
	return &commonpb.Metadata{
		IdeName:          ClientProductLabel,
		ExtensionVersion: ClientVersion,
		ApiKey:           sessionToken,
		Locale:           "en",
		Os:               osName,
		IdeVersion:       ClientVersion,
		ExtensionName:    ClientName,
		IdeType:          ClientName,
		F:                deviceFingerprint,
	}
}

func statusMetadata(sessionToken, deviceFingerprint string) *commonpb.Metadata {
	return &commonpb.Metadata{
		IdeName:          ClientName,
		ExtensionVersion: ClientVersion,
		ApiKey:           sessionToken,
		Locale:           "en",
		Os:               runtime.GOOS,
		IdeVersion:       ClientVersion,
		ExtensionName:    ClientName,
		F:                deviceFingerprint,
	}
}

func BuildGetChatMessageRequest(
	sessionToken string,
	deviceSeed string,
	chatModelUID string,
	systemPrompt string,
	prompts []Prompt,
	tools []Tool,
	temperature *float64,
	maxTokens int,
	sessionID string,
	cascadeID string,
) ([]byte, error) {
	if maxTokens <= 0 {
		maxTokens = DefaultMaxTokens
	}
	if sessionID == "" {
		sessionID = randomHex(16)
	}
	if cascadeID == "" {
		cascadeID = sessionID
	}
	osName := runtime.GOOS

	req := &apipb.GetChatMessageRequest{
		Metadata:    clientMetadata(sessionToken, deviceSeed, osName),
		RequestType: apipb.ChatMessageRequestType_CHAT_MESSAGE_REQUEST_TYPE_CASCADE,
		Configuration: &commonpb.CompletionConfiguration{
			NumCompletions: 1, MaxTokens: uint64(maxTokens),
			MaxNewlines: 400, TopK: 40,
			TopP: float64(float32(0.95)),
		},
		CascadeId:    cascadeID,
		PlannerMode:  commonpb.ConversationalPlannerMode_CONVERSATIONAL_PLANNER_MODE_DEFAULT,
		ChatModelUid: chatModelUID,
	}
	if strings.TrimSpace(systemPrompt) != "" {
		req.Prompt = systemPrompt
	}
	tempVal := 1.0
	if temperature != nil {
		tempVal = *temperature
	}
	req.Configuration.Temperature = tempVal
	for _, p := range prompts {
		msgID := p.MessageID
		if msgID == "" {
			msgID = randomHex(16)
		}
		source := p.Source
		if source <= 0 {
			source = 1
		}
		prompt := &chatpb.ChatMessagePrompt{MessageId: msgID, Source: commonpb.ChatMessageSource(source), Prompt: p.Content}
		for _, tc := range p.ToolCalls {
			prompt.ToolCalls = append(prompt.ToolCalls, &commonpb.ChatToolCall{Id: tc.ID, Name: tc.Name, ArgumentsJson: tc.Arguments})
		}
		if p.ToolCallID != "" {
			prompt.ToolCallId = p.ToolCallID
		}
		for _, img := range p.Images {
			data := strings.TrimSpace(img.Base64Data)
			if data == "" {
				continue
			}
			mime := strings.TrimSpace(img.MimeType)
			if mime == "" {
				mime = "image/png"
			}
			prompt.Images = append(prompt.Images, &commonpb.ImageData{Base64Data: data, MimeType: mime})
		}
		if p.Thinking != "" {
			prompt.Thinking = p.Thinking
		}
		if len(p.Signature) > 0 {
			prompt.Signature = string(p.Signature)
		}
		if p.SignatureType != "" {
			prompt.SignatureType = p.SignatureType
		}
		req.ChatMessagePrompts = append(req.ChatMessagePrompts, prompt)
	}
	for _, tool := range tools {
		desc := tool.Description
		if strings.Contains(desc, "Takes a task_id parameter identifying the task") {
			desc = strings.ReplaceAll(desc, "Takes a task_id parameter identifying the task", "Takes a taskId parameter identifying the task")
		}
		definition := &chatpb.ChatToolDefinition{Name: tool.Name, Description: desc}
		if len(tool.Parameters) > 0 {
			definition.JsonSchemaString = string(tool.Parameters)
		}
		req.Tools = append(req.Tools, definition)
	}
	turnIndex := NextSessionTurnIndex(sessionID)
	trajectory := &cortexpb.CortexTrajectoryReference{TrajectoryId: sessionID, TrajectoryType: cortexpb.CortexTrajectoryType_CORTEX_TRAJECTORY_TYPE_CASCADE}
	if turnIndex > 0 {
		trajectory.StepIndex = int32(turnIndex)
	}
	if len(prompts) > 0 && prompts[len(prompts)-1].Source == 1 {
		if turnIndex == 0 || len(prompts) < 2 || prompts[len(prompts)-2].Source != 1 {
			trajectory.StepType = cortexpb.CortexStepType_CORTEX_STEP_TYPE_USER_INPUT
		}
	}
	req.TrajectoryReference = trajectory
	return proto.Marshal(req)
}

func ParseFrame(payload []byte) (FrameResult, error) {
	var frame apipb.GetChatMessageResponse
	if err := proto.Unmarshal(payload, &frame); err != nil {
		return FrameResult{}, fmt.Errorf("decode Devin response: %w", err)
	}
	result := FrameResult{
		OutputID:       frame.GetOutputId(),
		Timestamp:      uint64(frame.GetTimestamp().GetSeconds()),
		ContentText:    frame.GetDeltaText(),
		DeltaTokens:    uint64(frame.GetDeltaTokens()),
		StopReason:     uint64(frame.GetStopReason()),
		ThinkingText:   frame.GetDeltaThinking(),
		DeltaSignature: []byte(frame.GetDeltaSignature()),
		DeltaSigType:   frame.GetDeltaSignatureType(),
		Latency:        frame.GetLatency(),
		MessageID:      frame.GetMessageId(),
	}
	for _, call := range frame.GetDeltaToolCalls() {
		result.ToolCallDeltas = append(result.ToolCallDeltas, ToolCallDelta{ID: call.GetId(), Name: call.GetName(), Arguments: call.GetArgumentsJson()})
	}
	if usage := frame.GetUsage(); usage != nil {
		result.Usage = usageFromProto(usage)
	}
	return result, nil
}

func usageFromProto(usage *commonpb.ModelUsageStats) *Usage {
	result := &Usage{
		PromptTokens:     int64(usage.GetInputTokens() + usage.GetCacheWriteTokens() + usage.GetCacheReadTokens()),
		CompletionTokens: int64(usage.GetOutputTokens()),
		CachedTokens:     int64(usage.GetCacheReadTokens()),
		CacheWriteTokens: int64(usage.GetCacheWriteTokens()),
		ModelName:        usage.GetModelUid(),
	}
	for key, value := range usage.GetResponseHeader() {
		if key != "" {
			if result.Headers == nil {
				result.Headers = map[string]string{}
			}
			result.Headers[key] = value
			if (strings.EqualFold(key, "x-request-id") || strings.EqualFold(key, "request-id")) && value != "" {
				result.RequestID = value
			}
		}
	}
	return result
}

func ParseTrailerError(payload []byte) (statusCode int, err error) {
	trimmed := bytes.TrimSpace(payload)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("{}")) {
		return 0, nil
	}
	var trailer struct {
		Error *struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(trimmed, &trailer) != nil || trailer.Error == nil {
		return 0, nil
	}
	codeStr := strings.ToLower(trailer.Error.Code)
	msgLower := strings.ToLower(trailer.Error.Message)
	httpCode := http.StatusBadGateway
	switch codeStr {
	case "invalid_argument":
		if strings.Contains(msgLower, "internal error") {
			httpCode = http.StatusBadGateway
		} else {
			httpCode = http.StatusBadRequest
		}
	case "internal":
		httpCode = http.StatusBadGateway
	case "unauthenticated":
		httpCode = http.StatusUnauthorized
	case "permission_denied":
		httpCode = http.StatusForbidden
	case "resource_exhausted":
		httpCode = http.StatusTooManyRequests
	case "unavailable":
		httpCode = http.StatusServiceUnavailable
	case "canceled":
		httpCode = 499
	case "deadline_exceeded":
		httpCode = http.StatusGatewayTimeout
	case "failed_precondition":
		if strings.Contains(msgLower, "quota") ||
			strings.Contains(msgLower, "credit") ||
			strings.Contains(msgLower, "acu") ||
			strings.Contains(msgLower, "exhausted") ||
			strings.Contains(msgLower, "limit") {
			httpCode = http.StatusTooManyRequests
		} else {
			httpCode = http.StatusBadRequest
		}
	}
	return httpCode, fmt.Errorf("devin upstream error (%s): %s", trailer.Error.Code, trailer.Error.Message)
}

func BasicAuthHeader(sessionToken string) string {
	token := strings.TrimSpace(sessionToken)
	return "Basic " + token + "-" + token
}
