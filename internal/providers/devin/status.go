package devin

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"runtime"
	"strings"
	"time"
)

// UserStatus contains plan, quota, and account metadata from GetUserStatus.
type UserStatus struct {
	Email                       string
	UserName                    string
	UserID                      string
	TeamID                      string
	OrgID                       string
	OrgName                     string
	Plan                        string
	DailyQuotaRemainingPercent  int64
	WeeklyQuotaRemainingPercent int64
	DailyQuotaResetAt           time.Time
	WeeklyQuotaResetAt          time.Time
	PlanStart                   time.Time
	PlanEnd                     time.Time
}

func BuildGetUserStatusRequest(sessionToken, deviceFingerprint string) []byte {
	if deviceFingerprint == "" {
		deviceFingerprint = GenerateDeviceFingerprint(sessionToken)
	}
	var f1Bytes []byte
	f1Bytes = AppendTag(f1Bytes, 1, BytesType)
	f1Bytes = AppendString(f1Bytes, ClientName)
	f1Bytes = AppendTag(f1Bytes, 2, BytesType)
	f1Bytes = AppendString(f1Bytes, ClientVersion)
	f1Bytes = AppendTag(f1Bytes, 3, BytesType)
	f1Bytes = AppendString(f1Bytes, sessionToken)
	f1Bytes = AppendTag(f1Bytes, 4, BytesType)
	f1Bytes = AppendString(f1Bytes, "en")
	f1Bytes = AppendTag(f1Bytes, 5, BytesType)
	f1Bytes = AppendString(f1Bytes, runtime.GOOS)
	f1Bytes = AppendTag(f1Bytes, 7, BytesType)
	f1Bytes = AppendString(f1Bytes, ClientVersion)
	f1Bytes = AppendTag(f1Bytes, 12, BytesType)
	f1Bytes = AppendString(f1Bytes, ClientName)
	f1Bytes = AppendTag(f1Bytes, 31, BytesType)
	f1Bytes = AppendString(f1Bytes, deviceFingerprint)

	var reqBytes []byte
	reqBytes = AppendTag(reqBytes, 1, BytesType)
	reqBytes = AppendBytes(reqBytes, f1Bytes)
	return reqBytes
}

func ParseGetUserStatusResponse(data []byte) (*UserStatus, error) {
	if len(data) == 0 {
		return nil, errors.New("empty response data")
	}
	status := &UserStatus{}
	rem := data
	for len(rem) > 0 {
		num, wireType, n := ConsumeTag(rem)
		if n <= 0 {
			return nil, ParseError(n)
		}
		rem = rem[n:]
		if num == 1 && wireType == BytesType {
			userStatusBytes, m := ConsumeBytes(rem)
			if m <= 0 {
				return nil, ParseError(m)
			}
			rem = rem[m:]
			parseUserStatus(userStatusBytes, status)
		} else {
			m := ConsumeFieldValue(num, wireType, rem)
			if m <= 0 {
				return nil, ParseError(m)
			}
			rem = rem[m:]
		}
	}
	return status, nil
}

func parseUserStatus(data []byte, status *UserStatus) {
	rem := data
	for len(rem) > 0 {
		num, wireType, n := ConsumeTag(rem)
		if n <= 0 {
			return
		}
		rem = rem[n:]
		if wireType == BytesType {
			bytesVal, m := ConsumeBytes(rem)
			if m <= 0 {
				return
			}
			rem = rem[m:]
			switch num {
			case 3:
				status.UserName = string(bytesVal)
			case 5:
				status.TeamID = string(bytesVal)
			case 7:
				status.Email = string(bytesVal)
			case 13:
				parsePlanStatus(bytesVal, status)
			case 36:
				status.UserID = string(bytesVal)
			}
		} else {
			m := ConsumeFieldValue(num, wireType, rem)
			if m <= 0 {
				return
			}
			rem = rem[m:]
		}
	}
}

func parsePlanStatus(data []byte, status *UserStatus) {
	rem := data
	for len(rem) > 0 {
		num, wireType, n := ConsumeTag(rem)
		if n <= 0 {
			return
		}
		rem = rem[n:]
		switch wireType {
		case BytesType:
			bytesVal, m := ConsumeBytes(rem)
			if m <= 0 {
				return
			}
			rem = rem[m:]
			switch num {
			case 1:
				parsePlanInfo(bytesVal, status)
			case 2:
				if sec := parseSecondsSubfield(bytesVal); sec > 0 {
					status.PlanStart = time.Unix(sec, 0).UTC()
				}
			case 3:
				if sec := parseSecondsSubfield(bytesVal); sec > 0 {
					status.PlanEnd = time.Unix(sec, 0).UTC()
				}
			}
		case VarintType:
			val, m := ConsumeVarint(rem)
			if m <= 0 {
				return
			}
			rem = rem[m:]
			switch num {
			case 14:
				status.DailyQuotaRemainingPercent = int64(val)
			case 15:
				status.WeeklyQuotaRemainingPercent = int64(val)
			case 17:
				if val > 0 {
					status.DailyQuotaResetAt = time.Unix(int64(val), 0).UTC()
				}
			case 18:
				if val > 0 {
					status.WeeklyQuotaResetAt = time.Unix(int64(val), 0).UTC()
				}
			}
		default:
			m := ConsumeFieldValue(num, wireType, rem)
			if m <= 0 {
				return
			}
			rem = rem[m:]
		}
	}
}

func parsePlanInfo(data []byte, status *UserStatus) {
	rem := data
	for len(rem) > 0 {
		num, wireType, n := ConsumeTag(rem)
		if n <= 0 {
			return
		}
		rem = rem[n:]
		if wireType == BytesType {
			bytesVal, m := ConsumeBytes(rem)
			if m <= 0 {
				return
			}
			rem = rem[m:]
			switch num {
			case 2:
				status.Plan = string(bytesVal)
			case 33:
				parsePlanInfoOrg(bytesVal, status)
			}
		} else {
			m := ConsumeFieldValue(num, wireType, rem)
			if m <= 0 {
				return
			}
			rem = rem[m:]
		}
	}
}

func parsePlanInfoOrg(data []byte, status *UserStatus) {
	rem := data
	for len(rem) > 0 {
		num, wireType, n := ConsumeTag(rem)
		if n <= 0 {
			return
		}
		rem = rem[n:]
		if wireType == BytesType {
			bytesVal, m := ConsumeBytes(rem)
			if m <= 0 {
				return
			}
			rem = rem[m:]
			switch num {
			case 4:
				status.OrgID = string(bytesVal)
			case 8:
				status.OrgName = string(bytesVal)
			}
		} else {
			m := ConsumeFieldValue(num, wireType, rem)
			if m <= 0 {
				return
			}
			rem = rem[m:]
		}
	}
}

func parseSecondsSubfield(data []byte) int64 {
	rem := data
	for len(rem) > 0 {
		num, wireType, n := ConsumeTag(rem)
		if n <= 0 {
			return 0
		}
		rem = rem[n:]
		if wireType == VarintType {
			val, m := ConsumeVarint(rem)
			if m <= 0 {
				return 0
			}
			rem = rem[m:]
			if num == 1 {
				return int64(val)
			}
		} else {
			m := ConsumeFieldValue(num, wireType, rem)
			if m <= 0 {
				return 0
			}
			rem = rem[m:]
		}
	}
	return 0
}

func FetchUserStatus(ctx context.Context, client *http.Client, serverBase, sessionToken, deviceSeed string) (*UserStatus, error) {
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	sessionToken = strings.TrimSpace(sessionToken)
	if sessionToken == "" {
		return nil, errors.New("devin session token is required")
	}
	reqBody := BuildGetUserStatusRequest(sessionToken, GenerateDeviceFingerprint(deviceSeed))
	endpoint := strings.TrimRight(firstNonEmpty(serverBase, ServerBase), "/") + PathGetUserStatus
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(reqBody))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", BasicAuthHeader(sessionToken))
	req.Header.Set("Connect-Protocol-Version", ConnectProtocolVersion)
	req.Header.Set("Content-Type", ContentTypeProto)
	req.Header.Set("Accept", "*/*")
	req.Header["User-Agent"] = []string{""}

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	respBytes, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("devin seat management error (status %d): %s", resp.StatusCode, string(respBytes))
	}
	return ParseGetUserStatusResponse(respBytes)
}
