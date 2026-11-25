package minimaxcode

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/caigee-cmd/cli2api/internal/providers"
)

// Daily check-in endpoints used by the MiniMax Code TUI (/checkin). They live on
// the same agent gateway as chat but under the first-party "minimax-cloud"
// prefix, and every request carries the signed query/headers the desktop client
// builds. Status is a read; claim is a POST of "{}".
const (
	signinStatusPath = "/minimax-cloud/api/v1/signin/status"
	signinClaimPath  = "/minimax-cloud/api/v1/signin/claim"
	userInfoPath     = "/v1/api/user/info"

	// Public-gateway attribution fields. The route validates the signature built
	// from the exact path+query, so the version fields only tag the caller.
	// 0.7.9 is the current MCode TUI release (npm @minimax-ai/code, 2026-11-20);
	// the build code is a fixed tag the gateway does not gate on.
	gatewayAppVersion  = "0.7.9"
	gatewayVersionCode = "22201"
	gatewaySignature   = "I*7Cf%WZ#S&%1RlZJ&C2"
	// The daily panel rolls over at 00:00 Asia/Shanghai for both regions, so the
	// query carries that offset instead of the host's local zone.
	gatewayTimezoneOffset = 8 * 3600
	checkinTimeout        = 30 * time.Second
)

// Panel day statuses and claim results, mirrored from the desktop client.
const (
	signinDayUpcoming  = 1
	signinDayClaimable = 2
	signinDayClaimed   = 3
	signinDayDisabled  = 4

	signinSceneCompleted = 3
	signinSceneBroken    = 4

	signinClaimed        = 1
	signinAlreadyClaimed = 2
)

type signinDay struct {
	DayNo       int     `json:"day_no"`
	Points      float64 `json:"points"`
	BonusPoints float64 `json:"bonus_points,omitempty"`
	Status      int     `json:"status"`
	IsToday     bool    `json:"is_today"`
}

type signinPanel struct {
	Scene int         `json:"scene"`
	Days  []signinDay `json:"days"`
}

type signinClaim struct {
	ClaimResult int     `json:"claim_result"`
	DayNo       int     `json:"day_no"`
	Points      float64 `json:"points"`
	ExpireAtMs  int64   `json:"expire_at_ms"`
}

// CheckinStatus reads the seven-day panel without claiming.
func (c *Client) CheckinStatus(ctx context.Context, accountID string) (providers.CheckinResult, error) {
	ctx, cancel := context.WithTimeout(ctx, checkinTimeout)
	defer cancel()
	credential, err := c.credential(ctx, accountID)
	if err != nil {
		return providers.CheckinResult{}, err
	}
	userID, err := c.checkinUserID(ctx, accountID, credential)
	if err != nil {
		return providers.CheckinResult{}, err
	}
	data, err := c.signedGatewayCall(ctx, accountID, http.MethodGet, signinStatusPath, "", userID, "status")
	if err != nil {
		return providers.CheckinResult{}, err
	}
	var panel signinPanel
	if err := json.Unmarshal(data, &panel); err != nil {
		return providers.CheckinResult{}, fmt.Errorf("minimaxcode check-in: invalid status payload")
	}
	today := todaySigninDay(panel)
	result := providers.CheckinResult{EarliestCheckin: "00:00"}
	switch {
	case today != nil && today.Status == signinDayClaimed:
		// is_today is computed by the provider panel; it is already the
		// provider's own day, so no local date guess is layered on top.
		result.Status = "already"
		result.Message = "今天已签到"
		result.RewardCredits = today.Points
	case today != nil && today.Status == signinDayClaimable:
		result.Status = "pending"
		result.Message = "上游显示今日可签到"
	case today != nil && today.Status == signinDayUpcoming:
		result.Status = "skipped"
		result.Message = "签到尚未开始"
	default:
		result.Status = "skipped"
		result.Message = skippedCheckinMessage(panel)
	}
	return result, nil
}

// Checkin implements providers.AccountCheckiner for the MiniMax Code daily
// sign-in. It reads the seven-day panel, claims today when the panel offers it,
// and reports "already"/"skipped" otherwise so the console card and records stay
// accurate.
func (c *Client) Checkin(ctx context.Context, accountID string) (providers.CheckinResult, error) {
	ctx, cancel := context.WithTimeout(ctx, checkinTimeout)
	defer cancel()

	credential, err := c.credential(ctx, accountID)
	if err != nil {
		return providers.CheckinResult{}, err
	}
	userID, err := c.checkinUserID(ctx, accountID, credential)
	if err != nil {
		return providers.CheckinResult{}, err
	}

	data, err := c.signedGatewayCall(ctx, accountID, http.MethodGet, signinStatusPath, "", userID, "status")
	if err != nil {
		return providers.CheckinResult{}, err
	}
	var panel signinPanel
	if err := json.Unmarshal(data, &panel); err != nil {
		return providers.CheckinResult{}, fmt.Errorf("minimaxcode check-in: invalid status payload")
	}

	today := todaySigninDay(panel)
	switch {
	case today != nil && today.Status == signinDayClaimed:
		// is_today already identifies the provider's current day; the panel is
		// authoritative and does not need an extra claim to prove the date.
		return alreadyCheckedInResult(panel), nil
	case today != nil && today.Status == signinDayClaimable:
		return c.claimSignin(ctx, accountID, userID)
	default:
		return providers.CheckinResult{Status: "skipped", Message: skippedCheckinMessage(panel)}, nil
	}
}

// claimSignin POSTs the claim for a panel that reported today as CLAIMABLE.
func (c *Client) claimSignin(ctx context.Context, accountID, userID string) (providers.CheckinResult, error) {
	data, err := c.signedGatewayCall(ctx, accountID, http.MethodPost, signinClaimPath, "{}", userID, "claim")
	if err != nil {
		return providers.CheckinResult{}, err
	}
	var claim signinClaim
	if err := json.Unmarshal(data, &claim); err != nil {
		return providers.CheckinResult{}, fmt.Errorf("minimaxcode check-in: invalid claim payload")
	}
	switch claim.ClaimResult {
	case signinClaimed:
		message := "签到成功"
		if claim.Points > 0 {
			message = fmt.Sprintf("签到成功 +%g 积分", claim.Points)
		}
		return providers.CheckinResult{Status: "success", Message: message, RewardCredits: claim.Points}, nil
	case signinAlreadyClaimed:
		// The panel was CLAIMABLE before this claim in the provider's own day;
		// "already claimed" is today's result, not a replay from yesterday.
		return providers.CheckinResult{Status: "already", Message: "今天已签到"}, nil
	default:
		return providers.CheckinResult{}, fmt.Errorf("minimaxcode check-in: unexpected claim result %d", claim.ClaimResult)
	}
}

// checkinUserID resolves the real MiniMax user id the gateway scopes the panel
// to. Login does not persist it, so the account's observed remote id is used
// when present and otherwise fetched once from the account identity endpoint.
func (c *Client) checkinUserID(ctx context.Context, accountID string, credential Credential) (string, error) {
	if id := strings.TrimSpace(credential.UserID); id != "" {
		return id, nil
	}
	if account, err := c.store.Get(ctx, accountID); err == nil {
		if id := strings.TrimSpace(account.RemoteUID); id != "" {
			return id, nil
		}
	}
	data, err := c.signedGatewayCall(ctx, accountID, http.MethodGet, userInfoPath, "", "0", "account identity")
	if err != nil {
		return "", err
	}
	var document struct {
		Data struct {
			UserInfo      userInfoPerson `json:"userInfo"`
			UserInfoSnake userInfoPerson `json:"user_info"`
		} `json:"data"`
		UserInfo      userInfoPerson `json:"userInfo"`
		UserInfoSnake userInfoPerson `json:"user_info"`
	}
	if err := json.Unmarshal(data, &document); err != nil {
		return "", fmt.Errorf("minimaxcode check-in: invalid account identity payload")
	}
	id := first(
		document.Data.UserInfo.RealUserID,
		document.Data.UserInfo.RealUserIDSnake,
		document.Data.UserInfoSnake.RealUserID,
		document.Data.UserInfoSnake.RealUserIDSnake,
		document.UserInfo.RealUserID,
		document.UserInfo.RealUserIDSnake,
		document.UserInfoSnake.RealUserID,
		document.UserInfoSnake.RealUserIDSnake,
	)
	if id == "" {
		return "", fmt.Errorf("minimaxcode check-in: account identity returned no user id")
	}
	_ = c.store.Observe(ctx, accountID, id, "ready", "", "")
	return id, nil
}

type userInfoPerson struct {
	RealUserID      string `json:"realUserID"`
	RealUserIDSnake string `json:"real_user_id"`
}

// signedGatewayCall performs one signed public-gateway request and unwraps the
// response envelope. A 401/403 refreshes the single-use token once and retries;
// the identity and status calls are reads, and status must pass before any
// claim is attempted, so the retry cannot double-claim.
func (c *Client) signedGatewayCall(ctx context.Context, accountID string, method, path, body, userID, operation string) (json.RawMessage, error) {
	raw, status, err := c.signedGatewayCallOnce(ctx, accountID, method, path, body, userID)
	if err != nil {
		return nil, err
	}
	if status == http.StatusUnauthorized || status == http.StatusForbidden {
		if _, refreshErr := c.forceRefresh(ctx, accountID); refreshErr != nil {
			return nil, fmt.Errorf("minimaxcode check-in: %s authentication failed (HTTP %d): %w", operation, status, refreshErr)
		}
		raw, status, err = c.signedGatewayCallOnce(ctx, accountID, method, path, body, userID)
		if err != nil {
			return nil, err
		}
	}
	if status >= 300 {
		return nil, fmt.Errorf("minimaxcode check-in: %s HTTP %d: %s", operation, status, truncate(string(raw), 300))
	}
	return gatewayEnvelopeData(raw)
}

func (c *Client) signedGatewayCallOnce(ctx context.Context, accountID string, method, path, body, userID string) ([]byte, int, error) {
	credential, err := c.credential(ctx, accountID)
	if err != nil {
		return nil, 0, err
	}
	client, err := c.httpClient(ctx, accountID)
	if err != nil {
		return nil, 0, err
	}
	request, err := c.newSignedGatewayRequest(ctx, credential, method, path, body, userID)
	if err != nil {
		return nil, 0, err
	}
	response, err := client.Do(request)
	if err != nil {
		return nil, 0, fmt.Errorf("minimaxcode check-in: %w", err)
	}
	defer response.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	return raw, response.StatusCode, nil
}

// newSignedGatewayRequest mirrors the desktop client's URL query and
// yy/x-timestamp/x-signature attribution headers. The signature is an md5 over
// the exact path+query sent, so both sides must build the same string.
func (c *Client) newSignedGatewayRequest(ctx context.Context, credential Credential, method, path, body, userID string) (*http.Request, error) {
	now := c.now()
	query := url.Values{}
	if path == signinStatusPath || path == signinClaimPath {
		query.Set("device_platform", "web")
		query.Set("is_desktop", "1")
		query.Set("desktop_version", gatewayAppVersion)
	} else {
		query.Set("device_platform", "mcode")
	}
	query.Set("biz_id", "3")
	query.Set("app_id", "3001")
	query.Set("version_code", gatewayVersionCode)
	query.Set("unix", strconv.FormatInt(now.UnixMilli(), 10))
	query.Set("timezone_offset", strconv.Itoa(gatewayTimezoneOffset))
	if path == signinStatusPath || path == signinClaimPath {
		// The desktop signs with the numeric offset; the web panel also sends
		// the IANA zone. Keep both so either server build resolves the same day.
		query.Set("timezone_id", "Asia/Shanghai")
	}
	language := "zh"
	if credential.Site == "global" {
		language = "en"
	}
	query.Set("sys_language", language)
	query.Set("lang", language)
	query.Set("device_id", "0")
	query.Set("os_name", gatewayOSName())
	query.Set("browser_name", "mcode")
	query.Set("user_id", strings.TrimSpace(userID))
	query.Set("client", "mcode")
	encodedQuery := query.Encode()
	pathWithSearch := path + "?" + encodedQuery
	endpoint := strings.TrimRight(credential.agentBase(), "/") + pathWithSearch

	second := now.Unix()
	signatureBody := body
	yyBody := body
	if method == http.MethodGet {
		signatureBody = ""
		yyBody = "{}"
	}
	if yyBody == "" {
		yyBody = "{}"
	}
	yy := md5Hex(encodeURIComponent(pathWithSearch) + "_" + yyBody + md5Hex(strconv.FormatInt(now.UnixMilli(), 10)) + "ooui")
	signature := md5Hex(strconv.FormatInt(second, 10) + gatewaySignature + signatureBody)

	var reader io.Reader
	if method == http.MethodPost {
		reader = bytes.NewReader([]byte(body))
	}
	request, err := http.NewRequestWithContext(ctx, method, endpoint, reader)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("User-Agent", "MiniMaxCode")
	request.Header.Set("Authorization", "Bearer "+credential.AccessToken)
	request.Header.Set("yy", yy)
	request.Header.Set("x-timestamp", strconv.FormatInt(second, 10))
	request.Header.Set("x-signature", signature)
	return request, nil
}

// gatewayEnvelopeData returns the data member of a MiniMax gateway envelope,
// rejecting a non-zero base_resp/statusInfo code.
func gatewayEnvelopeData(raw []byte) (json.RawMessage, error) {
	var envelope struct {
		BaseResp struct {
			StatusCode int    `json:"status_code"`
			StatusMsg  string `json:"status_msg"`
		} `json:"base_resp"`
		StatusInfo struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
			Msg     string `json:"msg"`
		} `json:"statusInfo"`
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return nil, fmt.Errorf("minimaxcode check-in: invalid gateway response")
	}
	if envelope.BaseResp.StatusCode != 0 {
		message := first(envelope.BaseResp.StatusMsg, "request failed")
		return nil, fmt.Errorf("minimaxcode check-in: upstream status %d: %s", envelope.BaseResp.StatusCode, message)
	}
	if envelope.StatusInfo.Code != 0 {
		message := first(envelope.StatusInfo.Message, envelope.StatusInfo.Msg, "request failed")
		if envelope.StatusInfo.Code == 1_000_048 {
			return nil, fmt.Errorf("minimaxcode check-in: authentication failed: %s", message)
		}
		return nil, fmt.Errorf("minimaxcode check-in: upstream status %d: %s", envelope.StatusInfo.Code, message)
	}
	if len(envelope.Data) == 0 || string(envelope.Data) == "null" {
		return nil, fmt.Errorf("minimaxcode check-in: gateway response has no data")
	}
	return envelope.Data, nil
}

func todaySigninDay(panel signinPanel) *signinDay {
	for index := range panel.Days {
		if panel.Days[index].IsToday {
			return &panel.Days[index]
		}
	}
	return nil
}

// signinStreak counts consecutive claimed days up to and including today (or up
// to yesterday when today is still claimable), matching the TUI panel.
func signinStreak(panel signinPanel) int {
	days := append([]signinDay(nil), panel.Days...)
	sort.Slice(days, func(left, right int) bool { return days[left].DayNo < days[right].DayNo })
	todayIndex := -1
	for index := range days {
		if days[index].IsToday {
			todayIndex = index
			break
		}
	}
	if todayIndex < 0 {
		return 0
	}
	index := -1
	switch days[todayIndex].Status {
	case signinDayClaimed:
		index = todayIndex
	case signinDayClaimable:
		index = todayIndex - 1
	}
	streak := 0
	for ; index >= 0; index-- {
		if days[index].Status != signinDayClaimed {
			break
		}
		streak++
	}
	return streak
}

func alreadyCheckedInResult(panel signinPanel) providers.CheckinResult {
	today := todaySigninDay(panel)
	points := 0.0
	if today != nil {
		points = today.Points
	}
	message := "今天已签到"
	if points > 0 {
		message = fmt.Sprintf("今天已签到 +%g 积分", points)
	}
	if streak := signinStreak(panel); streak > 1 {
		message += fmt.Sprintf("（连签 %d 天）", streak)
	}
	return providers.CheckinResult{Status: "already", Message: message, RewardCredits: points}
}

func skippedCheckinMessage(panel signinPanel) string {
	switch panel.Scene {
	case signinSceneCompleted:
		return "本周签到已完成"
	case signinSceneBroken:
		return "签到活动暂时不可用"
	}
	today := todaySigninDay(panel)
	if today == nil {
		return "签到活动未开始"
	}
	switch today.Status {
	case signinDayDisabled:
		return "签到活动未开放"
	case signinDayUpcoming:
		return "今日签到尚未开始"
	default:
		return "今日暂不可签到"
	}
}

// encodeURIComponent mirrors JavaScript's encodeURIComponent, which the desktop
// client applies to the signed path+query before hashing.
func encodeURIComponent(value string) string {
	var builder strings.Builder
	for index := 0; index < len(value); index++ {
		char := value[index]
		switch {
		case char >= 'A' && char <= 'Z', char >= 'a' && char <= 'z', char >= '0' && char <= '9':
			builder.WriteByte(char)
		case strings.ContainsRune("-_.!~*'()", rune(char)):
			builder.WriteByte(char)
		default:
			builder.WriteByte('%')
			builder.WriteByte("0123456789ABCDEF"[char>>4])
			builder.WriteByte("0123456789ABCDEF"[char&15])
		}
	}
	return builder.String()
}

func md5Hex(value string) string {
	sum := md5.Sum([]byte(value))
	return hex.EncodeToString(sum[:])
}

func gatewayOSName() string {
	switch runtime.GOOS {
	case "windows":
		return "win32"
	case "darwin":
		return "darwin"
	default:
		return "linux"
	}
}

var _ providers.AccountCheckiner = (*Client)(nil)
