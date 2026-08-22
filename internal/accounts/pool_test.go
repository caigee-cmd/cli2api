package accounts

import (
	"testing"
	"time"
)

func TestRoundRobinSkipsDownAccounts(t *testing.T) {
	p := NewPool([]string{"http://a:3020", "http://b:3020", "http://c:3020"}, []string{"a", "b", "c"})
	first, ok := p.Pick("", nil)
	if !ok || first.ID != "a" {
		t.Fatalf("first=%+v ok=%v", first, ok)
	}
	second, ok := p.Pick("", nil)
	if !ok || second.ID != "b" {
		t.Fatalf("second=%+v ok=%v", second, ok)
	}
	p.MarkDown("c", time.Hour, "quota")
	third, ok := p.Pick("", nil)
	if !ok || third.ID != "a" {
		t.Fatalf("third after wrap should skip down c, got %+v", third)
	}
	preferred, ok := p.Pick("b", nil)
	if !ok || preferred.ID != "b" {
		t.Fatalf("prefer b got %+v", preferred)
	}
	excluded := map[string]struct{}{"a": {}, "b": {}}
	fallback, ok := p.Pick("", excluded)
	if !ok || fallback.ID != "c" {
		t.Fatalf("excluded fallback got %+v ok=%v", fallback, ok)
	}
}

func TestParseWorkerURLsDedupes(t *testing.T) {
	got := ParseWorkerURLs(" http://a:3020/,http://a:3020,http://b:3020 ")
	if len(got) != 2 || got[0] != "http://a:3020" || got[1] != "http://b:3020" {
		t.Fatalf("got %#v", got)
	}
}
