package qwenwork

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/md5"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// cosyServerPubKeyPEM is the COSY envelope key shipped in the QwenWork desktop
// bundle. It is the same key Qoder uses; the copy keeps this package free of
// any dependency on internal/providers/qoder.
const cosyServerPubKeyPEM = `-----BEGIN PUBLIC KEY-----
MIGfMA0GCSqGSIb3DQEBAQUAA4GNADCBiQKBgQDA8iMH5c02LilrsERw9t6Pv5Nc
4k6Pz1EaDicBMpdpxKduSZu5OANqUq8er4GM95omAGIOPOh+Nx0spthYA2BqGz+l
6HRkPJ7S236FZz73In/KVuLnwI8JJ2CbuJap8kvheCCZpmAWpb/cPx/3Vr/J6I17
XcW+ML9FoCI6AOvOzwIDAQAB
-----END PUBLIC KEY-----`

// session is one account's COSY envelope: a random temp key wrapped with the
// server RSA key, and the account identity AES-encrypted under that temp key.
// It is reused until the access token changes.
type session struct {
	token     string
	uid       string
	machineID string
	cosyKey   string
	info      string
}

func newSession(credential Credential) (*session, error) {
	token := strings.TrimSpace(credential.AccessToken)
	uid := strings.TrimSpace(credential.UID)
	if token == "" {
		return nil, fmt.Errorf("qwenwork session requires an access token")
	}
	tempKey, err := randomTempKey()
	if err != nil {
		return nil, err
	}
	wrapped, err := rsaEncryptPKCS1(tempKey)
	if err != nil {
		return nil, err
	}
	identity, err := sortedCompact([][2]string{
		{"aid", uid},
		{"email", strings.TrimSpace(credential.Email)},
		{"name", strings.TrimSpace(credential.Nickname)},
		{"security_oauth_token", token},
		{"uid", uid},
	})
	if err != nil {
		return nil, err
	}
	enc, err := aesCBCEncrypt(identity, tempKey, tempKey)
	if err != nil {
		return nil, err
	}
	machineID := strings.TrimSpace(credential.MachineID)
	if machineID == "" {
		machineID = deriveID(uid, "machine")
	}
	return &session{
		token:     token,
		uid:       uid,
		machineID: machineID,
		cosyKey:   base64.StdEncoding.EncodeToString(wrapped),
		info:      base64.StdEncoding.EncodeToString(enc),
	}, nil
}

// sign returns the full desktop header set for one request. body is the exact
// bytes sent; modelKey is empty for non-chat calls.
func (s *session) sign(st site, body, rawURL, modelKey, accept string, now time.Time) (map[string]string, error) {
	if s == nil {
		return nil, fmt.Errorf("qwenwork session is nil")
	}
	requestID, err := randomUUID()
	if err != nil {
		return nil, err
	}
	payloadJSON, err := sortedCompact([][2]string{
		{"cosyVersion", cosyVersion},
		{"ideVersion", st.AppVersion},
		{"info", s.info},
		{"requestId", requestID},
		{"version", "v1"},
	})
	if err != nil {
		return nil, err
	}
	payload := base64.StdEncoding.EncodeToString(payloadJSON)
	date := fmt.Sprintf("%d", now.Unix())
	sum := md5.Sum([]byte(payload + "\n" + s.cosyKey + "\n" + date + "\n" + body + "\n" + cosySignPath(rawURL)))
	xRequestID, err := randomUUID()
	if err != nil {
		return nil, err
	}
	headers := map[string]string{
		"Accept":                     accept,
		"Content-Type":               "application/json",
		"User-Agent":                 st.userAgent(),
		"X-Request-Id":               xRequestID,
		"X-Qwenwork-Version":         st.AppVersion,
		"X-Qwenwork-Release-Version": st.releaseVersion(),
		"X-Qwenwork-Build":           st.Build,
		"X-Qwenwork-Platform":        "win32",
		"X-Qwenwork-Arch":            "x64",
		"X-Qwenwork-Channel":         "stable",
		"Cosy-Version":               cosyVersion,
		"Cosy-ClientType":            clientType,
		"Cosy-Business-Product":      businessProduct,
		"Cosy-Business-Type":         businessType,
		"Cosy-Scene":                 scene,
		"Cosy-MachineOS":             machineOS,
		"Login-Version":              "v2",
		"Authorization":              "Bearer COSY." + payload + "." + hex.EncodeToString(sum[:]),
		"Cosy-Key":                   s.cosyKey,
		"Cosy-User":                  s.uid,
		"Cosy-Date":                  date,
		"Cosy-MachineId":             s.machineID,
		"Accept-Encoding":            "identity",
		"Cache-Control":              "no-cache",
	}
	if modelKey != "" {
		headers["X-Model-Key"] = modelKey
		headers["X-Model-Source"] = "system"
	}
	return headers, nil
}

// cosySignPath is the signed path: the URL path without query and without the
// /algo gateway prefix.
func cosySignPath(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Path == "" {
		return "/"
	}
	path := strings.TrimPrefix(parsed.Path, "/algo")
	if path == "" {
		return "/"
	}
	return path
}

// sortedCompact renders fields, already in key order, as compact JSON.
func sortedCompact(fields [][2]string) ([]byte, error) {
	var b strings.Builder
	b.WriteByte('{')
	for i, field := range fields {
		if i > 0 {
			b.WriteByte(',')
		}
		key, err := json.Marshal(field[0])
		if err != nil {
			return nil, err
		}
		value, err := json.Marshal(field[1])
		if err != nil {
			return nil, err
		}
		b.Write(key)
		b.WriteByte(':')
		b.Write(value)
	}
	b.WriteByte('}')
	return []byte(b.String()), nil
}

func deriveID(uid, salt string) string {
	if uid == "" {
		uid = "anonymous"
	}
	sum := md5.Sum([]byte(salt + ":" + uid))
	return hex.EncodeToString(sum[:])
}

func randomTempKey() ([]byte, error) {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		return nil, err
	}
	return []byte(hex.EncodeToString(buf)), nil
}

func randomUUID() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	buf[6] = (buf[6] & 0x0f) | 0x40
	buf[8] = (buf[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", buf[0:4], buf[4:6], buf[6:8], buf[8:10], buf[10:16]), nil
}

func newID() string {
	id, err := randomUUID()
	if err != nil {
		return "00000000-0000-4000-8000-000000000000"
	}
	return id
}

func rsaEncryptPKCS1(plain []byte) ([]byte, error) {
	block, _ := pem.Decode([]byte(cosyServerPubKeyPEM))
	if block == nil {
		return nil, fmt.Errorf("decode qwenwork cosy public key")
	}
	pub, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, err
	}
	key, ok := pub.(*rsa.PublicKey)
	if !ok {
		return nil, fmt.Errorf("qwenwork cosy public key is not RSA")
	}
	return rsa.EncryptPKCS1v15(rand.Reader, key, plain)
}

func aesCBCEncrypt(plain, key, iv []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	padded := pkcs7Pad(plain, block.BlockSize())
	out := make([]byte, len(padded))
	cipher.NewCBCEncrypter(block, iv[:block.BlockSize()]).CryptBlocks(out, padded)
	return out, nil
}

func pkcs7Pad(plain []byte, size int) []byte {
	pad := size - len(plain)%size
	out := make([]byte, len(plain)+pad)
	copy(out, plain)
	for i := len(plain); i < len(out); i++ {
		out[i] = byte(pad)
	}
	return out
}
