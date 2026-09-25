package control

import (
	"context"
	"errors"
	"testing"
)

func TestModelContextPolicy(t *testing.T) {
	for _, tt := range []struct {
		model, key string
		context    int
	}{
		{"", "", 180000},
		{" \t", "", 180000},
		{"auto", "auto", 180000},
		{" MINIMAX_M3 ", "minimax-m3", 1000000},
		{"MiniMax M3", "minimax-m3", 1000000},
		{" GLM_5.2 ", "glm-5.2", 180000},
	} {
		t.Run(tt.model, func(t *testing.T) {
			if got := ModelContextKey(tt.model); got != tt.key {
				t.Fatalf("key=%q want %q", got, tt.key)
			}
			if got := DefaultContextForModel(tt.model); got != tt.context {
				t.Fatalf("context=%d want %d", got, tt.context)
			}
		})
	}
}

func TestUpdateProviderModelSettingRejectsWorkBuddyMaxMode(t *testing.T) {
	svc, _, _, _ := newTestServices()
	on := true
	_, err := svc.Settings.UpdateProviderModelSetting(context.Background(), "workbuddy", "glm-5.3", ProviderModelSettingPatch{MaxMode: &on})
	var op *OperationError
	if !errors.As(err, &op) || op.Code != "invalid_request" {
		t.Fatalf("err=%v", err)
	}
}

func TestUpdateProviderModelSettingReadFailureIsBusinessError(t *testing.T) {
	svc, _, store, _ := newTestServices()
	store.getErr = errors.New("db read failed")
	on := true
	_, err := svc.Settings.UpdateProviderModelSetting(context.Background(), "trae", "glm-5.3", ProviderModelSettingPatch{MaxMode: &on})
	var op *OperationError
	if !errors.As(err, &op) || op.Code != "model_setting_failed" {
		t.Fatalf("err=%v", err)
	}
}

func TestUpdateProviderModelSettingWriteFailureIsBusinessError(t *testing.T) {
	svc, _, store, _ := newTestServices()
	store.updateErr = errors.New("db write failed")
	on := true
	_, err := svc.Settings.UpdateProviderModelSetting(context.Background(), "trae", "glm-5.3", ProviderModelSettingPatch{MaxMode: &on})
	var op *OperationError
	if !errors.As(err, &op) || op.Code != "model_setting_failed" {
		t.Fatalf("err=%v", err)
	}
}

func TestUpdateProviderModelSettingMergesPatch(t *testing.T) {
	svc, _, store, _ := newTestServices()
	store.providerSetting.ReasoningEffort = "high"
	on := true
	effort := "low"
	got, err := svc.Settings.UpdateProviderModelSetting(context.Background(), "trae", "glm-5.3", ProviderModelSettingPatch{
		MaxMode: &on, ReasoningEffort: &effort,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !got.MaxMode || got.ReasoningEffort != "low" {
		t.Fatalf("got=%+v", got)
	}
}

func TestReadAndUpdateModelSetting(t *testing.T) {
	svc, _, store, _ := newTestServices()
	got, err := svc.Settings.ReadModelSetting(context.Background(), "qoder", "glm-5.2")
	if err != nil || got.ContextLength != 180000 || got.ContextCustom {
		t.Fatalf("default=%+v err=%v", got, err)
	}
	length := 250000
	got, err = svc.Settings.UpdateModelSetting(context.Background(), "qoder", "glm-5.2", &length, ProviderModelSettingPatch{})
	if err != nil || got.ContextLength != 250000 || !got.ContextCustom {
		t.Fatalf("updated=%+v err=%v", got, err)
	}
	clear := 0
	got, err = svc.Settings.UpdateModelSetting(context.Background(), "qoder", "glm-5.2", &clear, ProviderModelSettingPatch{})
	if err != nil || got.ContextLength != 180000 || got.ContextCustom {
		t.Fatalf("cleared=%+v err=%v", got, err)
	}
	on := true
	got, err = svc.Settings.UpdateModelSetting(context.Background(), "trae", "glm-5.2", nil, ProviderModelSettingPatch{MaxMode: &on})
	if err != nil || !got.MaxMode || !got.ContextCustom {
		t.Fatalf("trae=%+v err=%v", got, err)
	}
	store.updateErr = errors.New("db write failed")
	_, err = svc.Settings.UpdateModelSetting(context.Background(), "qoder", "glm-5.2", &length, ProviderModelSettingPatch{})
	var op *OperationError
	if !errors.As(err, &op) || op.Code != "model_setting_failed" {
		t.Fatalf("persist err=%v", err)
	}
}

func TestQoderContextToggle(t *testing.T) {
	svc, _, _, _ := newTestServices()
	ctx := context.Background()

	// Default read: no stored override, toggle off.
	got, err := svc.Settings.ReadModelSetting(ctx, "qoder", "glm-5.2")
	if err != nil || got.MaxMode || got.ContextCustom {
		t.Fatalf("default=%+v err=%v", got, err)
	}

	// Turning the toggle on without a catalog bound has no larger tier, so it
	// must not fabricate a max window.
	on := true
	if _, err := svc.Settings.UpdateModelSetting(ctx, "qoder", "glm-5.2", nil, ProviderModelSettingPatch{MaxMode: &on}); err != nil {
		t.Fatalf("toggle on: %v", err)
	}
	got, _ = svc.Settings.ReadModelSetting(ctx, "qoder", "glm-5.2")
	if got.MaxMode {
		t.Fatalf("no catalog max tier must stay off: %+v", got)
	}

	// An explicit window round-trips and reads back as custom.
	length := 250000
	got, err = svc.Settings.UpdateModelSetting(ctx, "qoder", "glm-5.2", &length, ProviderModelSettingPatch{})
	if err != nil || got.ContextLength != 250000 || !got.ContextCustom {
		t.Fatalf("explicit=%+v err=%v", got, err)
	}
	// Clearing removes the override.
	clear := 0
	got, err = svc.Settings.UpdateModelSetting(ctx, "qoder", "glm-5.2", &clear, ProviderModelSettingPatch{})
	if err != nil || got.ContextCustom {
		t.Fatalf("cleared=%+v err=%v", got, err)
	}
}
