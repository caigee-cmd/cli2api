package qoder

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCheckinWorkerTransport(t *testing.T) {
	for _, fixture := range []struct {
		payload string
		status  int
		want    string
	}{
		{`{"ok":true,"status":"success","message":"claimed","reward_credits":100}`, 200, "success"},
		{`{"ok":true,"status":"already"}`, 200, "already"},
		{`{"ok":true,"status":"skipped"}`, 200, "skipped"},
		{`{}`, 200, ""},
		{`{"ok":true,"status":"unexpected"}`, 200, ""},
		{`{"error":{"message":"qoder_checkin_http_401"}}`, 502, ""},
	} {
		t.Run(fixture.payload, func(t *testing.T) {
			worker := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				body, _ := io.ReadAll(request.Body)
				if request.Method != http.MethodPost || request.URL.Path != "/admin/checkin" || string(body) != "{}" {
					t.Errorf("request=%s %s body=%s", request.Method, request.URL.Path, body)
				}
				if request.Header.Get("Authorization") != "Bearer local-key" || request.Header.Get("X-Qoder-Account") != "account" {
					t.Error("missing worker authentication/account boundary")
				}
				writer.WriteHeader(fixture.status)
				fmt.Fprint(writer, fixture.payload)
			}))
			defer worker.Close()
			client := NewClient()
			client.Bind(func(string) (string, bool) { return worker.URL, true }, func() string { return "local-key" })
			result, err := client.Checkin(context.Background(), "account")
			if fixture.want == "" && err == nil {
				t.Fatal("invalid response accepted")
			}
			if fixture.want != "" && (err != nil || result.Status != fixture.want) {
				t.Fatalf("result=%+v err=%v", result, err)
			}
			if client.Adapter().Checkin == nil || client.Adapter().Prober != nil {
				t.Fatal("checkin must not register a Qoder prober")
			}
		})
	}
}
