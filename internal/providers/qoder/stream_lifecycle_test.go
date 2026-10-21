package qoder

import (
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

// Regression for #333: an explicit [DONE] ends the walk without transport EOF.
func TestWalkUpstreamDoneDoesNotRequireEOF(t *testing.T) {
	r, w := io.Pipe()
	defer r.Close()
	defer w.Close()
	done := make(chan error, 1)
	go func() { done <- walkUpstream(r, func(map[string]any) error { return nil }) }()
	written := make(chan error, 1)
	go func() {
		_, err := io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"fixture\"}}]}\n\ndata: [DONE]\n\n")
		written <- err
	}()
	select {
	case err := <-written:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("synthetic upstream write did not complete")
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Error("upstream DONE still waits for transport EOF")
	}
}

type observableBody struct {
	io.Reader
	closed chan struct{}
	once   sync.Once
}

func (b *observableBody) Close() error {
	b.once.Do(func() { close(b.closed) })
	return nil
}

// Regression for #334: closing the converted body must release the pipe
// writer and the upstream body.
func TestClosingConvertedBodyReleasesWriter(t *testing.T) {
	upstream := &observableBody{
		Reader: strings.NewReader("data: {\"choices\":[{\"delta\":{\"content\":\"fixture\"},\"finish_reason\":\"stop\"}]}\n\n"),
		closed: make(chan struct{}),
	}
	response := rewriteUpstreamSSE(&http.Response{Header: http.Header{}, Body: upstream}, "synthetic-model")
	defer io.Copy(io.Discard, response.Body)
	if err := response.Body.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-upstream.closed:
	case <-time.After(time.Second):
		t.Error("closing converted Body did not release pipe writer and upstream Body")
	}
}
