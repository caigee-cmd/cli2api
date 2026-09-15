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
	"math"
	"net/http"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
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
	Index     int
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
	StatusCode       uint64
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

// ResetSessionTurnIndex clears a session counter (tests).
func ResetSessionTurnIndex(sessionID string) {
	sessionTurnMu.Lock()
	delete(sessionTurns, strings.TrimSpace(sessionID))
	sessionTurnMu.Unlock()
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

func BuildClientMetadata(sessionToken, deviceSeed, osName string) []byte {
	if osName == "" {
		osName = runtime.GOOS
	}
	deviceFingerprint := GenerateDeviceFingerprint(deviceSeed)
	var f1Bytes []byte
	f1Bytes = AppendTag(f1Bytes, 1, BytesType)
	f1Bytes = AppendString(f1Bytes, ClientProductLabel)
	f1Bytes = AppendTag(f1Bytes, 2, BytesType)
	f1Bytes = AppendString(f1Bytes, ClientVersion)
	f1Bytes = AppendTag(f1Bytes, 3, BytesType)
	f1Bytes = AppendString(f1Bytes, sessionToken)
	f1Bytes = AppendTag(f1Bytes, 4, BytesType)
	f1Bytes = AppendString(f1Bytes, "en")
	f1Bytes = AppendTag(f1Bytes, 5, BytesType)
	f1Bytes = AppendString(f1Bytes, osName)
	f1Bytes = AppendTag(f1Bytes, 7, BytesType)
	f1Bytes = AppendString(f1Bytes, ClientVersion)
	f1Bytes = AppendTag(f1Bytes, 12, BytesType)
	f1Bytes = AppendString(f1Bytes, ClientName)
	f1Bytes = AppendTag(f1Bytes, 28, BytesType)
	f1Bytes = AppendString(f1Bytes, ClientName)
	f1Bytes = AppendTag(f1Bytes, 31, BytesType)
	f1Bytes = AppendString(f1Bytes, deviceFingerprint)
	return f1Bytes
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
) []byte {
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

	reqBytes := make([]byte, 0, 4096+len(systemPrompt))
	f1Bytes := BuildClientMetadata(sessionToken, deviceSeed, osName)
	reqBytes = AppendTag(reqBytes, 1, BytesType)
	reqBytes = AppendBytes(reqBytes, f1Bytes)

	if strings.TrimSpace(systemPrompt) != "" {
		reqBytes = AppendTag(reqBytes, 2, BytesType)
		reqBytes = AppendString(reqBytes, systemPrompt)
	}

	for _, p := range prompts {
		var pBytes []byte
		msgID := p.MessageID
		if msgID == "" {
			msgID = randomHex(16)
		}
		pBytes = AppendTag(pBytes, 1, BytesType)
		pBytes = AppendString(pBytes, msgID)

		source := p.Source
		if source <= 0 {
			source = 1
		}
		pBytes = AppendTag(pBytes, 2, VarintType)
		pBytes = AppendVarint(pBytes, uint64(source))

		pBytes = AppendTag(pBytes, 3, BytesType)
		pBytes = AppendString(pBytes, p.Content)

		for _, tc := range p.ToolCalls {
			var tcBytes []byte
			if tc.ID != "" {
				tcBytes = AppendTag(tcBytes, 1, BytesType)
				tcBytes = AppendString(tcBytes, tc.ID)
			}
			if tc.Name != "" {
				tcBytes = AppendTag(tcBytes, 2, BytesType)
				tcBytes = AppendString(tcBytes, tc.Name)
			}
			if tc.Arguments != "" {
				tcBytes = AppendTag(tcBytes, 3, BytesType)
				tcBytes = AppendString(tcBytes, tc.Arguments)
			}
			pBytes = AppendTag(pBytes, 6, BytesType)
			pBytes = AppendBytes(pBytes, tcBytes)
		}

		if p.ToolCallID != "" {
			pBytes = AppendTag(pBytes, 7, BytesType)
			pBytes = AppendString(pBytes, p.ToolCallID)
		}

		for _, img := range p.Images {
			data := strings.TrimSpace(img.Base64Data)
			if data == "" {
				continue
			}
			var imgBytes []byte
			imgBytes = AppendTag(imgBytes, 1, BytesType)
			imgBytes = AppendString(imgBytes, data)
			mime := strings.TrimSpace(img.MimeType)
			if mime == "" {
				mime = "image/png"
			}
			imgBytes = AppendTag(imgBytes, 2, BytesType)
			imgBytes = AppendString(imgBytes, mime)
			pBytes = AppendTag(pBytes, 10, BytesType)
			pBytes = AppendBytes(pBytes, imgBytes)
		}

		if p.Thinking != "" {
			pBytes = AppendTag(pBytes, 11, BytesType)
			pBytes = AppendString(pBytes, p.Thinking)
		}
		if len(p.Signature) > 0 {
			pBytes = AppendTag(pBytes, 12, BytesType)
			pBytes = AppendBytes(pBytes, p.Signature)
		}
		if p.SignatureType != "" {
			pBytes = AppendTag(pBytes, 18, BytesType)
			pBytes = AppendString(pBytes, p.SignatureType)
		}

		reqBytes = AppendTag(reqBytes, 3, BytesType)
		reqBytes = AppendBytes(reqBytes, pBytes)
	}

	reqBytes = AppendTag(reqBytes, 7, VarintType)
	reqBytes = AppendVarint(reqBytes, 5)

	var f8Bytes []byte
	f8Bytes = AppendTag(f8Bytes, 1, VarintType)
	f8Bytes = AppendVarint(f8Bytes, 1)
	f8Bytes = AppendTag(f8Bytes, 2, VarintType)
	f8Bytes = AppendVarint(f8Bytes, uint64(maxTokens))
	f8Bytes = AppendTag(f8Bytes, 3, VarintType)
	f8Bytes = AppendVarint(f8Bytes, 400)
	tempVal := 1.0
	if temperature != nil {
		tempVal = *temperature
	}
	f8Bytes = AppendTag(f8Bytes, 5, Fixed64Type)
	f8Bytes = AppendFloat64(f8Bytes, tempVal)
	f8Bytes = AppendTag(f8Bytes, 7, VarintType)
	f8Bytes = AppendVarint(f8Bytes, 40)
	f8Bytes = AppendTag(f8Bytes, 8, Fixed64Type)
	f8Bytes = AppendFixed64(f8Bytes, math.Float64bits(float64(float32(0.95))))
	reqBytes = AppendTag(reqBytes, 8, BytesType)
	reqBytes = AppendBytes(reqBytes, f8Bytes)

	for _, tool := range tools {
		var tBytes []byte
		if tool.Name != "" {
			tBytes = AppendTag(tBytes, 1, BytesType)
			tBytes = AppendString(tBytes, tool.Name)
		}
		desc := tool.Description
		if strings.Contains(desc, "Takes a task_id parameter identifying the task") {
			desc = strings.ReplaceAll(desc, "Takes a task_id parameter identifying the task", "Takes a taskId parameter identifying the task")
		}
		if desc != "" {
			tBytes = AppendTag(tBytes, 2, BytesType)
			tBytes = AppendString(tBytes, desc)
		}
		if len(tool.Parameters) > 0 {
			tBytes = AppendTag(tBytes, 3, BytesType)
			tBytes = AppendBytes(tBytes, tool.Parameters)
		}
		reqBytes = AppendTag(reqBytes, 10, BytesType)
		reqBytes = AppendBytes(reqBytes, tBytes)
	}

	turnIndex := NextSessionTurnIndex(sessionID)
	var f15Bytes []byte
	f15Bytes = AppendTag(f15Bytes, 1, BytesType)
	f15Bytes = AppendString(f15Bytes, sessionID)
	if turnIndex > 0 {
		f15Bytes = AppendTag(f15Bytes, 2, VarintType)
		f15Bytes = AppendVarint(f15Bytes, uint64(turnIndex))
	}
	f15Bytes = AppendTag(f15Bytes, 3, VarintType)
	f15Bytes = AppendVarint(f15Bytes, 4)
	if len(prompts) > 0 && prompts[len(prompts)-1].Source == 1 {
		if turnIndex == 0 || len(prompts) < 2 || prompts[len(prompts)-2].Source != 1 {
			f15Bytes = AppendTag(f15Bytes, 4, VarintType)
			f15Bytes = AppendVarint(f15Bytes, 14)
		}
	}
	reqBytes = AppendTag(reqBytes, 15, BytesType)
	reqBytes = AppendBytes(reqBytes, f15Bytes)

	reqBytes = AppendTag(reqBytes, 16, BytesType)
	reqBytes = AppendString(reqBytes, cascadeID)
	reqBytes = AppendTag(reqBytes, 20, VarintType)
	reqBytes = AppendVarint(reqBytes, 1)
	reqBytes = AppendTag(reqBytes, 21, BytesType)
	reqBytes = AppendString(reqBytes, chatModelUID)
	return reqBytes
}

func ParseFrame(payload []byte) (FrameResult, error) {
	var res FrameResult
	var textParts []string
	var thinkingParts []string
	pos := 0
	for pos < len(payload) {
		num, typ, n := ConsumeTag(payload[pos:])
		if n <= 0 {
			return res, fmt.Errorf("consume tag error at offset %d: %w", pos, ParseError(n))
		}
		pos += n
		switch typ {
		case VarintType:
			v, vn := ConsumeVarint(payload[pos:])
			if vn <= 0 {
				return res, fmt.Errorf("consume varint error at offset %d: %w", pos, ParseError(vn))
			}
			pos += vn
			switch num {
			case 2:
				res.Timestamp = v
			case 4:
				res.DeltaTokens = v
			case 5:
				res.StopReason = v
			}
		case Fixed64Type:
			v, fn := ConsumeFixed64(payload[pos:])
			if fn <= 0 {
				return res, fmt.Errorf("consume fixed64 error at offset %d: %w", pos, ParseError(fn))
			}
			pos += fn
			if num == 12 {
				res.Latency = math.Float64frombits(v)
			}
		case Fixed32Type:
			_, fn := ConsumeFixed32(payload[pos:])
			if fn <= 0 {
				return res, fmt.Errorf("consume fixed32 error at offset %d: %w", pos, ParseError(fn))
			}
			pos += fn
		case BytesType:
			val, bn := ConsumeBytes(payload[pos:])
			if bn <= 0 {
				return res, fmt.Errorf("consume bytes error at offset %d: %w", pos, ParseError(bn))
			}
			pos += bn
			switch num {
			case 1:
				res.OutputID = string(val)
			case 2:
				res.Timestamp = parseTimestamp(val)
			case 3:
				textParts = append(textParts, string(val))
			case 6:
				if tc, err := parseToolCallDelta(val); err == nil {
					res.ToolCallDeltas = append(res.ToolCallDeltas, tc)
				}
			case 7:
				res.Usage = parseUsageField(val)
			case 9:
				thinkingParts = append(thinkingParts, string(val))
			case 10:
				res.DeltaSignature = append(res.DeltaSignature, val...)
			case 17:
				res.MessageID = string(val)
			case 21:
				res.DeltaSigType = string(val)
			}
		default:
			return res, fmt.Errorf("unsupported wire type %d at offset %d", typ, pos)
		}
	}
	if len(textParts) > 0 {
		res.ContentText = strings.Join(textParts, "")
	}
	if len(thinkingParts) > 0 {
		res.ThinkingText = strings.Join(thinkingParts, "")
	}
	return res, nil
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

func parseToolCallDelta(data []byte) (ToolCallDelta, error) {
	var tc ToolCallDelta
	pos := 0
	for pos < len(data) {
		num, typ, n := ConsumeTag(data[pos:])
		if n <= 0 {
			return tc, ParseError(n)
		}
		pos += n
		switch typ {
		case VarintType:
			v, vn := ConsumeVarint(data[pos:])
			if vn <= 0 {
				return tc, ParseError(vn)
			}
			pos += vn
			if num == 4 {
				tc.Index = int(v)
			}
		case BytesType:
			val, bn := ConsumeBytes(data[pos:])
			if bn <= 0 {
				return tc, ParseError(bn)
			}
			pos += bn
			switch num {
			case 1:
				tc.ID = string(val)
			case 2:
				tc.Name = string(val)
			case 3:
				tc.Arguments = string(val)
			}
		default:
			nSkip := ConsumeFieldValue(num, typ, data[pos:])
			if nSkip <= 0 {
				return tc, ParseError(nSkip)
			}
			pos += nSkip
		}
	}
	return tc, nil
}

func parseTimestamp(data []byte) uint64 {
	pos := 0
	var secs uint64
	for pos < len(data) {
		num, typ, n := ConsumeTag(data[pos:])
		if n <= 0 {
			break
		}
		pos += n
		if typ == VarintType {
			v, vn := ConsumeVarint(data[pos:])
			if vn <= 0 {
				break
			}
			pos += vn
			if num == 1 {
				secs = v
			}
		} else {
			break
		}
	}
	return secs
}

func parseUsageField(data []byte) *Usage {
	u := &Usage{}
	pos := 0
	for pos < len(data) {
		num, typ, n := ConsumeTag(data[pos:])
		if n <= 0 {
			break
		}
		pos += n
		switch typ {
		case VarintType:
			v, vn := ConsumeVarint(data[pos:])
			if vn <= 0 {
				return u
			}
			pos += vn
			switch num {
			case 2:
				u.PromptTokens += int64(v)
			case 3:
				u.CompletionTokens = int64(v)
			case 4:
				u.PromptTokens += int64(v)
			case 5:
				u.CachedTokens = int64(v)
			case 6:
				u.StatusCode = v
			}
		case BytesType:
			val, bn := ConsumeBytes(data[pos:])
			if bn <= 0 {
				return u
			}
			pos += bn
			switch num {
			case 8:
				k, v := parseHeaderField(val)
				if k != "" {
					if u.Headers == nil {
						u.Headers = map[string]string{}
					}
					u.Headers[k] = v
					if (strings.EqualFold(k, "x-request-id") || strings.EqualFold(k, "request-id")) && v != "" {
						u.RequestID = v
					}
				}
			case 9:
				u.ModelName = string(val)
			}
		case Fixed64Type:
			_, fn := ConsumeFixed64(data[pos:])
			if fn <= 0 {
				return u
			}
			pos += fn
		case Fixed32Type:
			_, fn := ConsumeFixed32(data[pos:])
			if fn <= 0 {
				return u
			}
			pos += fn
		default:
			nSkip := ConsumeFieldValue(num, typ, data[pos:])
			if nSkip <= 0 {
				return u
			}
			pos += nSkip
		}
	}
	return u
}

func parseHeaderField(data []byte) (string, string) {
	var key, val string
	pos := 0
	for pos < len(data) {
		num, typ, n := ConsumeTag(data[pos:])
		if n <= 0 {
			break
		}
		pos += n
		switch typ {
		case BytesType:
			b, bn := ConsumeBytes(data[pos:])
			if bn <= 0 {
				return key, val
			}
			pos += bn
			switch num {
			case 1:
				key = string(b)
			case 2:
				val = string(b)
			}
		default:
			nSkip := ConsumeFieldValue(num, typ, data[pos:])
			if nSkip <= 0 {
				return key, val
			}
			pos += nSkip
		}
	}
	return key, val
}

func BasicAuthHeader(sessionToken string) string {
	token := strings.TrimSpace(sessionToken)
	return "Basic " + token + "-" + token
}
