package meta

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"
)

// MaxMetaReasoningSignatureLen bounds raw base64url text (CPA parity).
const MaxMetaReasoningSignatureLen = 32 * 1024 * 1024

const (
	metaReasoningSignatureOverheadLen = 45
	metaReasoningSignatureMinPayload  = 1024
	maxMetaRouterSuffixLen            = 128
)

// Fixed five-byte prefix on every captured Meta Muse encrypted_content
// (CPA signature/meta_validation, 2026-10-15 corpus).
var metaReasoningSignatureHeader = []byte{0x43, 0xe3, 0xda, 0x0e, 0x01}

// MetaReasoningSignatureInfo describes the transport shape of a Meta Muse
// reasoning encrypted_content envelope.
type MetaReasoningSignatureInfo struct {
	DecodedLen     int
	PayloadLen     int
	BucketExponent int
	RouterSuffix   string
}

func IsValidMetaReasoningSignature(rawSignature string) bool {
	_, err := InspectMetaReasoningSignature(rawSignature)
	return err == nil
}

// InspectMetaReasoningSignature validates the outer envelope shape observed
// for Meta Muse reasoning encrypted_content (CPA meta_validation): canonical
// unpadded base64url, Q-PaDg / fixed header, decoded length 45+1024*2^k, and
// an optional alphanumeric router suffix after '.'.
func InspectMetaReasoningSignature(rawSignature string) (*MetaReasoningSignatureInfo, error) {
	sig := strings.TrimSpace(rawSignature)
	if sig == "" {
		return nil, fmt.Errorf("empty Meta reasoning signature")
	}
	if len(sig) > MaxMetaReasoningSignatureLen {
		return nil, fmt.Errorf("Meta reasoning signature exceeds maximum length (%d bytes)", MaxMetaReasoningSignatureLen)
	}
	if !strings.HasPrefix(sig, "Q-PaDg") {
		return nil, fmt.Errorf("invalid Meta reasoning signature: expected Q-PaDg prefix")
	}
	native := sig
	routerSuffix := ""
	if head, tail, ok := strings.Cut(sig, "."); ok {
		if !isValidMetaRouterSuffix(tail) {
			return nil, fmt.Errorf("invalid Meta reasoning signature: router suffix must be 1-%d alphanumeric characters", maxMetaRouterSuffixLen)
		}
		native = head
		routerSuffix = tail
	}
	info, err := inspectNativeMetaReasoningSignature(native)
	if err != nil {
		return nil, err
	}
	info.RouterSuffix = routerSuffix
	return info, nil
}

func inspectNativeMetaReasoningSignature(sig string) (*MetaReasoningSignatureInfo, error) {
	if !strings.HasPrefix(sig, "Q-PaDg") {
		return nil, fmt.Errorf("invalid Meta reasoning signature: expected Q-PaDg prefix")
	}
	if len(sig)%4 == 1 {
		return nil, fmt.Errorf("invalid Meta reasoning signature: base64url length %d cannot decode", len(sig))
	}
	decodedLen := len(sig) / 4 * 3
	switch len(sig) % 4 {
	case 2:
		decodedLen++
	case 3:
		decodedLen += 2
	}
	payloadLen := decodedLen - metaReasoningSignatureOverheadLen
	if payloadLen < metaReasoningSignatureMinPayload || payloadLen&(payloadLen-1) != 0 {
		return nil, fmt.Errorf("invalid Meta reasoning signature: payload length %d is not a power-of-two bucket", payloadLen)
	}
	if index, r, ok := firstInvalidMetaReasoningSignatureChar(sig); ok {
		return nil, fmt.Errorf("invalid Meta reasoning signature: contains non-base64url character U+%04X at byte %d", r, index)
	}
	decoded, err := base64.RawURLEncoding.Strict().DecodeString(sig)
	if err != nil {
		return nil, fmt.Errorf("invalid Meta reasoning signature: base64url decode failed")
	}
	if !bytes.Equal(decoded[:len(metaReasoningSignatureHeader)], metaReasoningSignatureHeader) {
		return nil, fmt.Errorf("invalid Meta reasoning signature: unexpected envelope header %x", decoded[:len(metaReasoningSignatureHeader)])
	}
	exponent := 0
	for p := payloadLen; p > metaReasoningSignatureMinPayload; p >>= 1 {
		exponent++
	}
	return &MetaReasoningSignatureInfo{
		DecodedLen:     len(decoded),
		PayloadLen:     payloadLen,
		BucketExponent: exponent,
	}, nil
}

var metaReasoningSignatureCharSet = base64AlphabetSet("-_")
var metaRouterSuffixCharSet = base64AlphabetSet("")

func base64AlphabetSet(extra string) [256]bool {
	var set [256]bool
	for c := 'A'; c <= 'Z'; c++ {
		set[c] = true
	}
	for c := 'a'; c <= 'z'; c++ {
		set[c] = true
	}
	for c := '0'; c <= '9'; c++ {
		set[c] = true
	}
	for i := 0; i < len(extra); i++ {
		set[extra[i]] = true
	}
	return set
}

func firstInvalidMetaReasoningSignatureChar(sig string) (int, rune, bool) {
	for index := 0; index < len(sig); index++ {
		if !metaReasoningSignatureCharSet[sig[index]] {
			r, _ := utf8.DecodeRuneInString(sig[index:])
			return index, r, true
		}
	}
	return 0, 0, false
}

func isValidMetaRouterSuffix(suffix string) bool {
	if len(suffix) == 0 || len(suffix) > maxMetaRouterSuffixLen {
		return false
	}
	for index := 0; index < len(suffix); index++ {
		if !metaRouterSuffixCharSet[suffix[index]] {
			return false
		}
	}
	return true
}

// sanitizeEncryptedContent walks a Responses JSON body and strips reasoning
// encrypted_content values that fail InspectMetaReasoningSignature. Valid
// envelopes (including router-suffixed) pass through verbatim. Fail-closed for
// native Meta upstream — mirrors CPA sanitize…ForMeta behavior at the Adapter
// boundary without porting CPA's executor state machine.
func sanitizeEncryptedContent(body []byte) []byte {
	var root map[string]json.RawMessage
	if err := json.Unmarshal(body, &root); err != nil {
		return body
	}
	inputRaw, ok := root["input"]
	if !ok || len(inputRaw) == 0 || string(inputRaw) == "null" {
		return body
	}
	var items []json.RawMessage
	if err := json.Unmarshal(inputRaw, &items); err != nil {
		return body
	}
	changed := false
	for i, itemRaw := range items {
		var item map[string]json.RawMessage
		if err := json.Unmarshal(itemRaw, &item); err != nil {
			continue
		}
		typ := strings.Trim(string(item["type"]), `"`)
		if typ != "reasoning" {
			continue
		}
		encRaw, has := item["encrypted_content"]
		if !has {
			continue
		}
		var enc string
		if err := json.Unmarshal(encRaw, &enc); err != nil {
			delete(item, "encrypted_content")
			changed = true
		} else if !IsValidMetaReasoningSignature(enc) {
			delete(item, "encrypted_content")
			changed = true
		} else {
			continue
		}
		rewritten, err := json.Marshal(item)
		if err != nil {
			continue
		}
		items[i] = rewritten
	}
	if !changed {
		return body
	}
	newInput, err := json.Marshal(items)
	if err != nil {
		return body
	}
	root["input"] = newInput
	out, err := json.Marshal(root)
	if err != nil {
		return body
	}
	return out
}
