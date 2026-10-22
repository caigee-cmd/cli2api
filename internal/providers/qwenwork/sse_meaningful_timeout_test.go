package qwenwork

import (
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/caigee-cmd/cli2api/internal/providers"
)

// Heartbeat-only forever stream must time out with upstream_empty (same hang
// shape as qoder PAT in-process ChatStream; issue #344).
func TestEnsureMeaningfulUpstreamTimesOutHeartbeatOnly(t *testing.T) {
	pr, pw := io.Pipe()
	go func() {
		defer pw.Close()
		// Empty body choices are not meaningful for qwenwork either.
		heartbeat := "data: {\"headers\":{},\"body\":\"{\\\"choices\\\":[{\\\"delta\\\":{},\\\"finish_reason\\\":null}]}\",\"statusCodeValue\":200}\n\n"
		for i := 0; i < 100; i++ {
			if _, err := io.WriteString(pw, heartbeat); err != nil {
				return
			}
			time.Sleep(15 * time.Millisecond)
		}
	}()
	resp := &http.Response{StatusCode: http.StatusOK, Body: pr}
	started := time.Now()
	out, err := ensureMeaningfulUpstream(resp, 60*time.Millisecond)
	elapsed := time.Since(started)
	if err == nil {
		out.Body.Close()
		t.Fatal("expected timeout")
	}
	var providerErr *providers.Error
	if !errors.As(err, &providerErr) || providerErr.Code != "upstream_empty" {
		t.Fatalf("err = %#v", err)
	}
	if out != nil {
		t.Fatalf("out = %v, want nil", out)
	}
	if elapsed > 2*time.Second {
		t.Fatalf("took %s", elapsed)
	}
}

func TestEnsureMeaningfulUpstreamPreservesBufferedBytes(t *testing.T) {
	body := "data: {\"headers\":{},\"body\":\"{\\\"choices\\\":[{\\\"delta\\\":{},\\\"finish_reason\\\":null}]}\",\"statusCodeValue\":200}\n\n" +
		"data: {\"headers\":{},\"body\":\"{\\\"choices\\\":[{\\\"delta\\\":{\\\"content\\\":\\\"hi\\\"},\\\"finish_reason\\\":\\\"stop\\\"}]}\",\"statusCodeValue\":200}\n\n" +
		"data: {\"headers\":{},\"body\":\"{\\\"choices\\\":[{\\\"delta\\\":{\\\"content\\\":\\\" more\\\"}}]}\",\"statusCodeValue\":200}\n\n"
	resp := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body))}
	out, err := ensureMeaningfulUpstream(resp, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer out.Body.Close()
	got, _ := io.ReadAll(out.Body)
	if string(got) != body {
		t.Fatalf("peeked replay mismatch:\n got %q\nwant %q", got, body)
	}
}
