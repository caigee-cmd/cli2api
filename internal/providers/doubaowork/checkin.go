package doubaowork

import (
	"context"
	"strings"

	"github.com/caigee-cmd/cli2api/internal/providers"
)

// Check-in is READ-ONLY. DO NOT ADD A CLAIM/REWARD REQUEST.
//
// The upstream daily check-in reward can only be claimed in the Doubao desktop
// client. A write to any claim/reward endpoint has been observed to consume the
// account's reward for the current day even when the call fails, and the loss is
// not reversible or recoverable — the user must wait until the next day. Any
// "let me just try claiming" code path directly costs the user quota. This
// capability therefore only ever reads status and never returns "success".
//
//   - claimed today -> Status "already"
//   - not claimed / not eligible -> Status "skipped" ("需去豆包电脑版手动领取")
//   - never returns "success", never POSTs a claim/reward endpoint.

const (
	campaignScene = "pc_incentive_task_mvp_v3"
	signinTask    = "daily_signin"
	checkinPath   = "/alice/ug/campaign/v3/detail/"
)

// checkinState is the parsed read-only check-in status.
type checkinState struct {
	Found     bool
	Claimed   bool
	Points    int
	Streak    int
	Claimable bool
}

// readCheckin parses the campaign detail payload for the daily-signin task.
func readCheckin(detail map[string]any) checkinState {
	data, _ := detail["data"].(map[string]any)
	if data == nil {
		data = detail
	}
	tasks := asSlice(data["tasks"])
	if tasks == nil {
		if group, ok := data["campaign_group"].(map[string]any); ok {
			tasks = asSlice(group["tasks"])
		}
	}
	var chosen map[string]any
	for _, taskRaw := range tasks {
		task, ok := taskRaw.(map[string]any)
		if !ok {
			continue
		}
		daily, ok := task["daily_checkin"].(map[string]any)
		if !ok || daily == nil {
			continue
		}
		if asString(task["campaign_code"]) == signinTask {
			chosen = task
			break
		}
		if chosen == nil {
			chosen = task
		}
	}
	if chosen == nil {
		return checkinState{Found: false}
	}
	daily, _ := chosen["daily_checkin"].(map[string]any)
	state := checkinState{
		Found:     true,
		Claimed:   asBool(daily["checked_in_today"]),
		Claimable: intOf(chosen["task_status"]) == 2,
	}
	if reward, ok := daily["reward"].(map[string]any); ok {
		if points, ok := numOf(reward["reward_amount"]); ok {
			state.Points = int(points)
		}
	}
	if streak, ok := numOf(daily["continuous_days"]); ok {
		state.Streak = int(streak)
	}
	return state
}

// Checkin implements providers.AccountCheckiner as a read-only status query.
func (c *Client) Checkin(ctx context.Context, accountID string) (providers.CheckinResult, error) {
	sess := c.sessionFor(accountID)
	if err := sess.ready(ctx); err != nil {
		return providers.CheckinResult{Status: "error", Message: err.Error()}, nil
	}

	group := sess.campaignGroup()
	if group == "" {
		if launch, err := sess.json(ctx, "POST", "/alice/user/launch", map[string]any{}, nil); err == nil {
			if info, ok := launch["ug_campaign_info"].(map[string]any); ok {
				group = strings.TrimSpace(asString(info["campaign_group_code"]))
			}
		}
	}
	if group == "" {
		return providers.CheckinResult{Status: "skipped", Message: "这个账号当前没有投放签到活动"}, nil
	}

	// READ-ONLY GET: never a claim/reward write.
	detail, err := sess.json(ctx, "GET", checkinPath, nil, map[string]string{"campaign_group_code": group})
	if err != nil {
		if typed, ok := err.(*providers.Error); ok && typed.Kind == "auth" {
			return providers.CheckinResult{Status: "error", Message: "登录已失效：" + typed.Message}, nil
		}
		if strings.Contains(strings.ToLower(err.Error()), "campaign not found") {
			return providers.CheckinResult{Status: "skipped", Message: "这个账号当前没有投放签到活动"}, nil
		}
		return providers.CheckinResult{Status: "error", Message: err.Error()}, nil
	}

	state := readCheckin(detail)
	earliest := doubaoEarliestClock(detail)
	if !state.Found {
		return providers.CheckinResult{Status: "skipped", Message: "活动里没有每日签到任务", EarliestCheckin: earliest}, nil
	}
	if state.Claimed {
		message := "今天已签到"
		if state.Streak > 0 {
			message = "今天已签到，连续 " + itoa(state.Streak) + " 天"
		}
		return providers.CheckinResult{Status: "already", Message: message, RewardCredits: float64(state.Points), Streak: state.Streak, EarliestCheckin: earliest}, nil
	}
	message := "今日未签。豆包把领取限死在电脑版客户端，请在那里点一下"
	if !state.Claimable {
		message = "今日签到暂不可领：豆包按账号投放这个活动，你的账号当前没被投放"
	}
	return providers.CheckinResult{Status: "skipped", Message: message, RewardCredits: float64(state.Points), EarliestCheckin: earliest}, nil
}

// CheckinStatus reports the same read-only state without treating the
// client-only claim as an unavailable activity.
func (c *Client) CheckinStatus(ctx context.Context, accountID string) (providers.CheckinResult, error) {
	result, err := c.Checkin(ctx, accountID)
	if err != nil {
		return providers.CheckinResult{}, err
	}
	if result.Status == "skipped" && strings.Contains(result.Message, "今日未签") {
		result.Status = "pending"
		result.Message = "上游显示今日未签到（豆包电脑版领取）"
	}
	return result, nil
}

func doubaoEarliestClock(detail map[string]any) string {
	data, _ := detail["data"].(map[string]any)
	if data == nil {
		data = detail
	}
	for _, container := range []map[string]any{data, detail} {
		for _, key := range []string{"start_time", "startTime", "begin_time"} {
			if clock := providers.CheckinClockFromText(asString(container[key])); clock != "" {
				return clock
			}
		}
	}
	return "00:00"
}
