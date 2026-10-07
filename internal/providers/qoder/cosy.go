package qoder

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/md5"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha512"
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

// COSYVersion is the CLI protocol version sent on chat. It matches the pinned
// worker CLI, not a newer desktop SDK constant.
const COSYVersion = "1.1.32"

const cosyServerPubKeyPEM = `-----BEGIN PUBLIC KEY-----
MIGfMA0GCSqGSIb3DQEBAQUAA4GNADCBiQKBgQDA8iMH5c02LilrsERw9t6Pv5Nc
4k6Pz1EaDicBMpdpxKduSZu5OANqUq8er4GM95omAGIOPOh+Nx0spthYA2BqGz+l
6HRkPJ7S236FZz73In/KVuLnwI8JJ2CbuJap8kvheCCZpmAWpb/cPx/3Vr/J6I17
XcW+ML9FoCI6AOvOzwIDAQAB
-----END PUBLIC KEY-----`

const (
	cosyStdAlphabet    = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"
	cosyCustomAlphabet = "_doRTgHZBKcGVjlvpC,@aFSx#DPuNJme&i*MzLOEn)sUrthbf%Y^w.(kIQyXqWA!"
)

// CLIUser is the plaintext of ~/.qoder*/.auth/user. Field names follow the
// CLI JSON; missing tokens mean the account still needs a worker login.
type CLIUser struct {
	Name               string `json:"name"`
	UID                string `json:"uid"`
	Aid                string `json:"aid"`
	YxUID              string `json:"yx_uid"`
	OrganizationID     string `json:"organization_id"`
	OrganizationName   string `json:"organization_name"`
	UserType           string `json:"user_type"`
	AccessToken        string `json:"access_token"`
	SecurityOauthToken string `json:"security_oauth_token"`
	RefreshToken       string `json:"refresh_token"`
}

func (u CLIUser) token() string {
	if strings.TrimSpace(u.SecurityOauthToken) != "" {
		return strings.TrimSpace(u.SecurityOauthToken)
	}
	return strings.TrimSpace(u.AccessToken)
}

func (u CLIUser) uid() string {
	if strings.TrimSpace(u.UID) != "" {
		return strings.TrimSpace(u.UID)
	}
	return strings.TrimSpace(u.Aid)
}

// DecryptCLIUser opens a CLI user blob. Plain JSON is accepted. Ciphertext is
// standard base64 of AES-128-CBC, with key and IV equal to the first 16 bytes
// of machine_id.
func DecryptCLIUser(blob []byte, machineID string) (CLIUser, error) {
	raw := strings.TrimSpace(string(blob))
	if raw == "" {
		return CLIUser{}, fmt.Errorf("qoder user credential is empty")
	}
	plain := []byte(raw)
	if !strings.HasPrefix(raw, "{") {
		key := machineKey(machineID)
		if key == nil {
			return CLIUser{}, fmt.Errorf("qoder machine id is shorter than 16 bytes")
		}
		decoded, err := base64.StdEncoding.DecodeString(raw)
		if err != nil {
			return CLIUser{}, fmt.Errorf("decode qoder user credential: %w", err)
		}
		plain, err = aesCBCDecrypt(decoded, key, key)
		if err != nil {
			return CLIUser{}, err
		}
	}
	var user CLIUser
	if err := json.Unmarshal(plain, &user); err != nil {
		return CLIUser{}, fmt.Errorf("parse qoder user credential: %w", err)
	}
	if user.token() == "" || user.uid() == "" {
		return CLIUser{}, fmt.Errorf("qoder user credential has no access token")
	}
	return user, nil
}

func machineKey(machineID string) []byte {
	machineID = strings.TrimSpace(machineID)
	if len(machineID) < 16 {
		return nil
	}
	return []byte(machineID[:16])
}

// Session is one account's COSY signing state. Machine fields are derived
// from the uid so a restart does not mint a new device.
type Session struct {
	User         CLIUser
	MachineID    string
	MachineToken string
	MachineType  string
	tempKey      []byte
	cosyKey      string
	info         string
}

func NewSession(user CLIUser) (*Session, error) {
	uid := user.uid()
	if uid == "" || user.token() == "" {
		return nil, fmt.Errorf("qoder session requires uid and access token")
	}
	tempKey, err := randomTempKey()
	if err != nil {
		return nil, err
	}
	wrapped, err := rsaEncryptPKCS1(tempKey)
	if err != nil {
		return nil, err
	}
	identity, err := identityJSON(user)
	if err != nil {
		return nil, err
	}
	enc, err := aesCBCEncrypt(identity, tempKey, tempKey)
	if err != nil {
		return nil, err
	}
	return &Session{
		User:         user,
		MachineID:    deriveID(uid, "machine"),
		MachineToken: deriveMachineToken(uid),
		MachineType:  deriveMachineType(uid),
		tempKey:      tempKey,
		cosyKey:      base64.StdEncoding.EncodeToString(wrapped),
		info:         base64.StdEncoding.EncodeToString(enc),
	}, nil
}

func (s *Session) Sign(body, rawURL string, now time.Time) (headers map[string]string, err error) {
	if s == nil {
		return nil, fmt.Errorf("qoder session is nil")
	}
	path := cosySignPath(rawURL)
	payload, err := payloadB64(s.info)
	if err != nil {
		return nil, err
	}
	date := fmt.Sprintf("%d", now.Unix())
	raw := payload + "\n" + s.cosyKey + "\n" + date + "\n" + body + "\n" + path
	sum := md5.Sum([]byte(raw))
	bearer := "Bearer COSY." + payload + "." + hex.EncodeToString(sum[:])
	return map[string]string{
		"Accept":            "text/event-stream",
		"Authorization":     bearer,
		"Cache-Control":     "no-cache",
		"Content-Type":      "application/json",
		"Cosy-ClientType":   "5",
		"Cosy-Data-Policy":  "AGREE",
		"Cosy-Date":         date,
		"Cosy-Key":          s.cosyKey,
		"Cosy-MachineId":    s.MachineID,
		"Cosy-MachineToken": s.MachineToken,
		"Cosy-MachineType":  s.MachineType,
		"Cosy-User":         s.User.uid(),
		"Cosy-Version":      COSYVersion,
		"Login-Version":     "v2",
		"Accept-Encoding":   "identity",
	}, nil
}

// EncodeBody is the custom base64 used when the chat URL carries Encode=1.
func EncodeBody(plain []byte) string {
	std := base64.StdEncoding.EncodeToString(plain)
	n := len(std)
	a := n / 3
	rearranged := std[n-a:] + std[a:n-a] + std[:a]
	var b strings.Builder
	b.Grow(len(rearranged))
	for i := 0; i < len(rearranged); i++ {
		c := rearranged[i]
		if c == '=' {
			b.WriteByte('$')
			continue
		}
		if idx := strings.IndexByte(cosyStdAlphabet, c); idx >= 0 {
			b.WriteByte(cosyCustomAlphabet[idx])
			continue
		}
		b.WriteByte(c)
	}
	return b.String()
}

func DecodeBody(encoded string) ([]byte, error) {
	var b strings.Builder
	b.Grow(len(encoded))
	for i := 0; i < len(encoded); i++ {
		c := encoded[i]
		if c == '$' {
			b.WriteByte('=')
			continue
		}
		if idx := strings.IndexByte(cosyCustomAlphabet, c); idx >= 0 {
			b.WriteByte(cosyStdAlphabet[idx])
			continue
		}
		b.WriteByte(c)
	}
	std := b.String()
	n := len(std)
	a := n / 3
	r1, r2, r3 := std[:a], std[a:n-a], std[n-a:]
	original := r3 + r2 + r1
	return base64.StdEncoding.DecodeString(original)
}

// ChatEndpointHook replaces the signed chat URL in tests. Production leaves it nil.
var ChatEndpointHook func(region string) string

func chatEndpoint(region string) string {
	if ChatEndpointHook != nil {
		return ChatEndpointHook(region)
	}
	return ChatEndpoint(region)
}

func ChatEndpoint(region string) string {
	if strings.EqualFold(strings.TrimSpace(region), "cn") {
		return "https://gateway.qoder.com.cn/algo/api/v2/service/pro/sse/agent_chat_generation?FetchKeys=llm_model_result&AgentId=agent_common&Encode=1"
	}
	return "https://api1.qoder.sh/algo/api/v2/service/pro/sse/agent_chat_generation?FetchKeys=llm_model_result&AgentId=agent_common&Encode=1"
}

func cosySignPath(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Path == "" {
		return "/"
	}
	path := parsed.Path
	if strings.HasPrefix(path, "/algo") {
		path = path[len("/algo"):]
	}
	if path == "" {
		return "/"
	}
	return path
}

func identityJSON(user CLIUser) ([]byte, error) {
	aid := strings.TrimSpace(user.Aid)
	if aid == "" {
		aid = user.uid()
	}
	token := user.token()
	fields := [][2]string{
		{"aid", aid},
		{"name", strings.TrimSpace(user.Name)},
		{"organization_id", strings.TrimSpace(user.OrganizationID)},
		{"organization_name", strings.TrimSpace(user.OrganizationName)},
		{"refresh_token", strings.TrimSpace(user.RefreshToken)},
		{"security_oauth_token", token},
		{"uid", user.uid()},
		{"user_type", strings.TrimSpace(user.UserType)},
		{"yx_uid", strings.TrimSpace(user.YxUID)},
	}
	return sortedCompact(fields)
}

func payloadB64(info string) (string, error) {
	id, err := randomUUID()
	if err != nil {
		return "", err
	}
	fields := [][2]string{
		{"cosyVersion", COSYVersion},
		{"ideVersion", ""},
		{"info", info},
		{"requestId", id},
		{"version", "v1"},
	}
	raw, err := sortedCompact(fields)
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(raw), nil
}

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

func deriveMachineType(uid string) string {
	id := strings.ReplaceAll(deriveID(uid, "machinetype"), "-", "")
	if len(id) > 18 {
		return id[:18]
	}
	return id
}

func deriveMachineToken(uid string) string {
	sum := sha512.Sum512([]byte("machinetoken:" + uid))
	return strings.TrimRight(base64.RawURLEncoding.EncodeToString(sum[:]), "=")[:43]
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

func rsaEncryptPKCS1(plain []byte) ([]byte, error) {
	block, _ := pem.Decode([]byte(cosyServerPubKeyPEM))
	if block == nil {
		return nil, fmt.Errorf("decode qoder cosy public key")
	}
	pub, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, err
	}
	key, ok := pub.(*rsa.PublicKey)
	if !ok {
		return nil, fmt.Errorf("qoder cosy public key is not RSA")
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

func aesCBCDecrypt(data, key, iv []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	if len(data) == 0 || len(data)%block.BlockSize() != 0 {
		return nil, fmt.Errorf("qoder user credential length is not a full AES block")
	}
	out := make([]byte, len(data))
	cipher.NewCBCDecrypter(block, iv[:block.BlockSize()]).CryptBlocks(out, data)
	return pkcs7Unpad(out, block.BlockSize())
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

func pkcs7Unpad(padded []byte, size int) ([]byte, error) {
	if len(padded) == 0 || len(padded)%size != 0 {
		return nil, fmt.Errorf("invalid pkcs7 padding")
	}
	pad := int(padded[len(padded)-1])
	if pad == 0 || pad > size || pad > len(padded) {
		return nil, fmt.Errorf("invalid pkcs7 padding")
	}
	for _, b := range padded[len(padded)-pad:] {
		if int(b) != pad {
			return nil, fmt.Errorf("invalid pkcs7 padding")
		}
	}
	return padded[:len(padded)-pad], nil
}
