package updater

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	control "github.com/caigee-cmd/cli2api/internal/update"
)

type blockingApplier struct{}

func (blockingApplier) Apply(context.Context, string, ApplyRequest, func(string)) (bool, error) {
	return false, nil
}

func TestServiceSerializesUpdateJobs(t *testing.T) {
	service := NewService(Config{}, blockingApplier{})
	body := []byte(`{"current_version":"v0.2.1","target_version":"v0.2.2","backup_path":"/data/backups/qoder-20260824T010203.000000000Z.db"}`)

	first := httptest.NewRecorder()
	service.Handler().ServeHTTP(first, httptest.NewRequest(http.MethodPost, "/v1/update", bytes.NewReader(body)))
	if first.Code != http.StatusAccepted {
		t.Fatalf("first status = %d body=%s", first.Code, first.Body.String())
	}
	second := httptest.NewRecorder()
	service.Handler().ServeHTTP(second, httptest.NewRequest(http.MethodPost, "/v1/update", bytes.NewReader(body)))
	if second.Code != http.StatusConflict {
		t.Fatalf("second status = %d body=%s", second.Code, second.Body.String())
	}
}

func TestServiceRequiresConfiguredBearerToken(t *testing.T) {
	service := NewService(Config{AuthToken: "local-token"}, blockingApplier{})

	unauthorized := httptest.NewRecorder()
	service.Handler().ServeHTTP(unauthorized, httptest.NewRequest(http.MethodGet, "/health", nil))
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized status = %d", unauthorized.Code)
	}

	authorizedRequest := httptest.NewRequest(http.MethodGet, "/health", nil)
	authorizedRequest.Header.Set("Authorization", "Bearer local-token")
	authorized := httptest.NewRecorder()
	service.Handler().ServeHTTP(authorized, authorizedRequest)
	if authorized.Code != http.StatusOK {
		t.Fatalf("authorized status = %d body=%s", authorized.Code, authorized.Body.String())
	}
}

func TestServiceRejectsNonLoopbackTCP(t *testing.T) {
	service := NewService(Config{ListenAddress: "0.0.0.0:3011", AuthToken: "local-token"}, blockingApplier{})
	if _, _, err := service.listen(); err == nil {
		t.Fatal("non-loopback TCP listener unexpectedly accepted")
	}
}

type stagedApplier struct {
	prepareCalled chan struct{}
	applyCalled   chan struct{}
}

func (a *stagedApplier) Prepare(context.Context, string, control.PrepareRequest, func(string)) error {
	close(a.prepareCalled)
	return nil
}

func (a *stagedApplier) Apply(context.Context, string, ApplyRequest, func(string)) (bool, error) {
	close(a.applyCalled)
	return false, nil
}

func TestServicePreparesImageBeforeApply(t *testing.T) {
	applier := &stagedApplier{prepareCalled: make(chan struct{}), applyCalled: make(chan struct{})}
	service := NewService(Config{}, applier)
	prepareBody := []byte(`{"current_version":"v0.2.1","target_version":"v0.2.2"}`)
	prepare := httptest.NewRecorder()
	service.Handler().ServeHTTP(prepare, httptest.NewRequest(http.MethodPost, "/v1/prepare", bytes.NewReader(prepareBody)))
	if prepare.Code != http.StatusAccepted {
		t.Fatalf("prepare status = %d body=%s", prepare.Code, prepare.Body.String())
	}
	select {
	case <-applier.prepareCalled:
	case <-time.After(time.Second):
		t.Fatal("image preparation did not start")
	}
	deadline := time.Now().Add(time.Second)
	ready := false
	for time.Now().Before(deadline) {
		status := httptest.NewRecorder()
		service.Handler().ServeHTTP(status, httptest.NewRequest(http.MethodGet, "/v1/status", nil))
		if strings.Contains(status.Body.String(), `"state":"ready_to_apply"`) {
			ready = true
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !ready {
		t.Fatal("image preparation did not become ready")
	}
	applyBody := []byte(`{"current_version":"v0.2.1","target_version":"v0.2.2","backup_path":"/data/backups/qoder-20260824T010203.000000000Z.db"}`)
	apply := httptest.NewRecorder()
	service.Handler().ServeHTTP(apply, httptest.NewRequest(http.MethodPost, "/v1/apply", bytes.NewReader(applyBody)))
	if apply.Code != http.StatusAccepted {
		t.Fatalf("apply status = %d body=%s", apply.Code, apply.Body.String())
	}
	select {
	case <-applier.applyCalled:
	case <-time.After(3 * time.Second):
		t.Fatal("prepared update did not start")
	}
}
