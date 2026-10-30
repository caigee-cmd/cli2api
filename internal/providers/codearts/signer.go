package codearts

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

const signingAlgorithm = "SDK-HMAC-SHA256"

// SignCredential is the short-lived STS trio used to sign every gateway call.
type SignCredential struct {
	AccessKeyID     string
	SecretAccessKey string
	SecurityToken   string
}

// signRequest adds the Huawei Cloud SDK-HMAC-SHA256 headers. The upstream
// verifies the exact header set, so this must stay byte-for-byte compatible
// with the official signer.
func signRequest(req *http.Request, body []byte, cred SignCredential) {
	xDate := time.Now().UTC().Format("20060102T150405Z")
	req.Header.Set("X-Sdk-Date", xDate)
	req.Header.Set("Host", req.URL.Host)
	if cred.SecurityToken != "" {
		req.Header.Set("X-Security-Token", cred.SecurityToken)
	}
	payloadHash := sha256Hex(body)
	req.Header.Set("X-Sdk-Content-Sha256", payloadHash)

	canonicalHeaders, signedHeaders := canonicalHeaders(req)
	canonicalRequest := strings.Join([]string{
		req.Method,
		canonicalURI(req.URL.EscapedPath()),
		canonicalQuery(req.URL.RawQuery),
		canonicalHeaders,
		signedHeaders,
		payloadHash,
	}, "\n")
	stringToSign := strings.Join([]string{
		signingAlgorithm,
		xDate,
		sha256Hex([]byte(canonicalRequest)),
	}, "\n")
	signature := hmacHex(cred.SecretAccessKey, stringToSign)
	req.Header.Set("Authorization", signingAlgorithm+
		" Access="+cred.AccessKeyID+
		", SignedHeaders="+signedHeaders+
		", Signature="+signature)
}

// canonicalHeaders signs every request header (lower-case keys, sorted), as the
// official Java/JS SDKs do.
func canonicalHeaders(req *http.Request) (headers, signed string) {
	keys := make([]string, 0, len(req.Header))
	for k := range req.Header {
		keys = append(keys, strings.ToLower(k))
	}
	sort.Strings(keys)
	var sb strings.Builder
	for _, k := range keys {
		v := req.Header.Get(k)
		sb.WriteString(k + ":" + strings.TrimSpace(v) + "\n")
	}
	return sb.String(), strings.Join(keys, ";")
}

// canonicalURI percent-encodes each segment and appends a trailing slash.
func canonicalURI(path string) string {
	segments := strings.Split(path, "/")
	for i, seg := range segments {
		segments[i] = url.PathEscape(seg)
	}
	out := strings.Join(segments, "/")
	if !strings.HasSuffix(out, "/") {
		out += "/"
	}
	return out
}

// canonicalQuery sorts keys and values.
func canonicalQuery(raw string) string {
	if raw == "" {
		return ""
	}
	vals, err := url.ParseQuery(raw)
	if err != nil {
		return raw
	}
	keys := make([]string, 0, len(vals))
	for k := range vals {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		vs := vals[k]
		sort.Strings(vs)
		for _, v := range vs {
			parts = append(parts, url.QueryEscape(k)+"="+url.QueryEscape(v))
		}
	}
	return strings.Join(parts, "&")
}

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func hmacHex(key, msg string) string {
	mac := hmac.New(sha256.New, []byte(key))
	_, _ = mac.Write([]byte(msg))
	return hex.EncodeToString(mac.Sum(nil))
}
