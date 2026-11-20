package doubaowork

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"testing"
)

type requestLog struct {
	mu      sync.Mutex
	records []string
}

func (r *requestLog) add(method, path string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.records = append(r.records, method+" "+path)
}

func (r *requestLog) all() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, len(r.records))
	copy(out, r.records)
	return out
}

func quotaCheckinHandler(log *requestLog) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/passport/account/info/v2/", func(w http.ResponseWriter, _ *http.Request) {
		writeEnvelope(w, map[string]any{"user_id_str": "42", "screen_name": "Alice"})
	})
	mux.HandleFunc("/alice/user/launch", func(w http.ResponseWriter, _ *http.Request) {
		writeEnvelope(w, map[string]any{"assistant_bot_id": "998877", "ug_campaign_info": map[string]any{"campaign_group_code": "grp-1"}})
	})
	mux.HandleFunc("/alice/commerce/sale/subscription/quota/summary/", func(w http.ResponseWriter, _ *http.Request) {
		log.add("POST", "/alice/commerce/sale/subscription/quota/summary/")
		writeEnvelope(w, map[string]any{
			"window_limit_section": map[string]any{
				"usage_exhausted": false,
				"window_limit_groups": []any{
					map[string]any{
						"feature_group_name": "订阅额度",
						"window_limits": []any{
							map[string]any{"used_amount": 0, "total_amount": 500, "used_percent": 0, "end_time": 4102444800},
						},
					},
				},
			},
		})
	})
	mux.HandleFunc("/alice/commerce/sale/subscription/overview/", func(w http.ResponseWriter, _ *http.Request) {
		log.add("POST", "/alice/commerce/sale/subscription/overview/")
		writeEnvelope(w, map[string]any{"current_subscription": map[string]any{"display": map[string]any{"short_name": "豆包工作 Pro"}}})
	})
	mux.HandleFunc("/alice/commerce/marketing/card/list/", func(w http.ResponseWriter, _ *http.Request) {
		log.add("POST", "/alice/commerce/marketing/card/list/")
		writeEnvelope(w, map[string]any{"data": map[string]any{"cards": []any{
			map[string]any{"source_display_text": "活动赠送", "track_params": map[string]any{"card_balance": 5}},
		}}})
	})
	mux.HandleFunc("/alice/ug/campaign/v3/detail/", func(w http.ResponseWriter, r *http.Request) {
		log.add(r.Method, "/alice/ug/campaign/v3/detail/")
		writeEnvelope(w, map[string]any{"data": map[string]any{"tasks": []any{
			map[string]any{
				"campaign_code":       "daily_signin",
				"task_status":         1,
				"campaign_group_code": "grp-1",
				"daily_checkin": map[string]any{
					"checked_in_today": false,
					"continuous_days":  4,
					"reward":           map[string]any{"reward_amount": 200},
				},
			},
		}}})
	})
	return mux
}

func TestQuotaAggregatesSources(t *testing.T) {
	log := &requestLog{}
	client := newChatClient(t, quotaCheckinHandler(log))
	info, err := client.Quota(context.Background(), "acc1")
	if err != nil {
		t.Fatalf("quota: %v", err)
	}
	if info == nil {
		t.Fatal("nil quota")
	}
	if info.Plan != "豆包工作 Pro" {
		t.Fatalf("plan = %q", info.Plan)
	}
	// subscription window + activity card
	if len(info.Windows) < 2 {
		t.Fatalf("expected >=2 windows, got %d: %+v", len(info.Windows), info.Windows)
	}
}

func TestQuotaDegradesWhenSummaryMissing(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/passport/account/info/v2/", func(w http.ResponseWriter, _ *http.Request) {
		writeEnvelope(w, map[string]any{"user_id_str": "42"})
	})
	mux.HandleFunc("/alice/user/launch", func(w http.ResponseWriter, _ *http.Request) {
		writeEnvelope(w, map[string]any{"assistant_bot_id": "998877"})
	})
	// summary/overview/cards all fail with 500.
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	})
	client := newChatClient(t, mux)
	info, err := client.Quota(context.Background(), "acc1")
	if err != nil {
		t.Fatalf("quota must degrade, not error: %v", err)
	}
	if info == nil || info.ProviderID != "doubaowork" {
		t.Fatalf("expected non-nil degraded quota, got %+v", info)
	}
}

func TestCheckinReadOnlyNeverWrites(t *testing.T) {
	log := &requestLog{}
	client := newChatClient(t, quotaCheckinHandler(log))
	result, err := client.Checkin(context.Background(), "acc1")
	if err != nil {
		t.Fatalf("checkin: %v", err)
	}
	if result.Status == "success" {
		t.Fatal("check-in must never report success")
	}
	if result.Status != "skipped" {
		t.Fatalf("unclaimed status = %q", result.Status)
	}
	for _, record := range log.all() {
		if strings.Contains(record, "landing") || strings.Contains(record, "reward") || strings.Contains(record, "claim") {
			t.Fatalf("check-in issued a write-like request: %q", record)
		}
		if strings.HasPrefix(record, "POST /alice/ug/campaign/v3/detail") {
			t.Fatalf("check-in detail must be GET, got: %q", record)
		}
	}
}

func TestCheckinAlreadyReported(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/passport/account/info/v2/", func(w http.ResponseWriter, _ *http.Request) {
		writeEnvelope(w, map[string]any{"user_id_str": "42"})
	})
	mux.HandleFunc("/alice/user/launch", func(w http.ResponseWriter, _ *http.Request) {
		writeEnvelope(w, map[string]any{"assistant_bot_id": "998877", "ug_campaign_info": map[string]any{"campaign_group_code": "grp-1"}})
	})
	mux.HandleFunc("/alice/ug/campaign/v3/detail/", func(w http.ResponseWriter, _ *http.Request) {
		writeEnvelope(w, map[string]any{"data": map[string]any{"tasks": []any{
			map[string]any{"campaign_code": "daily_signin", "task_status": 2, "daily_checkin": map[string]any{"checked_in_today": true, "continuous_days": 7, "reward": map[string]any{"reward_amount": 200}}},
		}}})
	})
	client := newChatClient(t, mux)
	result, err := client.Checkin(context.Background(), "acc1")
	if err != nil {
		t.Fatalf("checkin: %v", err)
	}
	if result.Status != "already" {
		t.Fatalf("status = %q", result.Status)
	}
}

func TestQuotaNoopWhenAccountInactive(t *testing.T) {
	// launch carries no campaign group -> checkin skipped, quota still computes.
	mux := http.NewServeMux()
	mux.HandleFunc("/passport/account/info/v2/", func(w http.ResponseWriter, _ *http.Request) {
		writeEnvelope(w, map[string]any{"user_id_str": "42"})
	})
	mux.HandleFunc("/alice/user/launch", func(w http.ResponseWriter, _ *http.Request) {
		writeEnvelope(w, map[string]any{"assistant_bot_id": "998877"})
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) { writeEnvelope(w, map[string]any{}) })
	client := newChatClient(t, mux)
	result, err := client.Checkin(context.Background(), "acc1")
	if err != nil {
		t.Fatalf("checkin: %v", err)
	}
	if result.Status != "skipped" {
		t.Fatalf("inactive status = %q", result.Status)
	}
}

var _ = json.Marshal
