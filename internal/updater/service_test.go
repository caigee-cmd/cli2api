package updater

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
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
