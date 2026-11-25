package accounts

import (
	"context"
	"testing"
)

type checkinSecretReader map[string]string

func (r checkinSecretReader) GetSecret(_ context.Context, name string) (string, bool, error) {
	value, ok := r[name]
	return value, ok, nil
}

// The provider check-in time setting is the operator's single source of truth.
// Every provider defaults to 09:00, a stored per-account auto time never
// outranks it, and only a per-account manual override does.
func TestResolveCheckinTimeFollowsProviderSetting(t *testing.T) {
	ctx := context.Background()
	for _, provider := range []string{"qoder", "workbuddy", "trae", "qwenwork", "codearts", "doubaowork", "minimaxcode"} {
		account := Account{Provider: provider, ProviderRegion: "cn", CheckinAutoTime: "01:00"}
		effective, err := ResolveCheckinTime(ctx, checkinSecretReader{}, account)
		if err != nil || effective != DefaultCheckinTime {
			t.Fatalf("%s default=%q err=%v want %q", provider, effective, err, DefaultCheckinTime)
		}
		store := checkinSecretReader{CheckinTimeSecret(provider): "10:30"}
		effective, err = ResolveCheckinTime(ctx, store, account)
		if err != nil || effective != "10:30" {
			t.Fatalf("%s setting=%q err=%v", provider, effective, err)
		}
		account.CheckinTime = "18:30"
		effective, err = ResolveCheckinTime(ctx, store, account)
		if err != nil || effective != "18:30" {
			t.Fatalf("%s manual override=%q err=%v", provider, effective, err)
		}
	}
}

func TestResolveCheckinTimeFallsBackWithoutReader(t *testing.T) {
	account := Account{Provider: "qoder"}
	if got, err := ResolveCheckinTime(context.Background(), nil, account); err != nil || got != DefaultCheckinTime {
		t.Fatalf("nil reader=%q err=%v", got, err)
	}
	account.CheckinAutoTime = "11:00"
	if got, err := ResolveCheckinTime(context.Background(), nil, account); err != nil || got != "11:00" {
		t.Fatalf("stored mirror=%q err=%v", got, err)
	}
}
