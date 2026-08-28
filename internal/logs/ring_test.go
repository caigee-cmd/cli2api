package logs

import (
	"strings"
	"testing"
)

func TestRingKeepsLatestAndRedactsSecrets(t *testing.T) {
	ring := NewRing(3)
	ring.Append("first")
	ring.Append("[account=acc_1] [daemon] warm complete")
	ring.Append("Authorization: Bearer super-secret-token")
	ring.Append("[security] initialized API key and stored it in SQLite: abcdef")

	entries := ring.Latest(10)
	if len(entries) != 3 {
		t.Fatalf("len=%d entries=%+v", len(entries), entries)
	}
	if !strings.Contains(entries[0].Message, "warm complete") {
		t.Fatalf("oldest kept unexpectedly: %+v", entries[0])
	}
	if entries[0].AccountID != "acc_1" || entries[0].Source != "daemon" {
		t.Fatalf("account/source = %+v", entries[0])
	}
	if strings.Contains(entries[1].Message, "super-secret-token") || !strings.Contains(entries[1].Message, "***") {
		t.Fatalf("bearer not redacted: %q", entries[1].Message)
	}
	if strings.Contains(entries[2].Message, "abcdef") {
		t.Fatalf("api key not redacted: %q", entries[2].Message)
	}
}

func TestPrefixWriterAddsAccountPrefix(t *testing.T) {
	ring := NewRing(10)
	writer := &PrefixWriter{Prefix: "[account=acc_x] ", Next: ring}
	if _, err := writer.Write([]byte("hello\nworld")); err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Write([]byte("\n")); err != nil {
		t.Fatal(err)
	}
	entries := ring.Latest(10)
	if len(entries) != 2 {
		t.Fatalf("entries=%+v", entries)
	}
	if entries[0].AccountID != "acc_x" || !strings.Contains(entries[0].Message, "[account=acc_x] hello") {
		t.Fatalf("first=%+v", entries[0])
	}
	if entries[1].AccountID != "acc_x" {
		t.Fatalf("second=%+v", entries[1])
	}
}

func TestRingSnapshotFilters(t *testing.T) {
	ring := NewRing(10)
	ring.Append("ok line")
	ring.Append("something failed hard")
	entries := ring.Snapshot(0, 10, "error", "failed", "")
	if len(entries) != 1 || entries[0].Level != "error" {
		t.Fatalf("filtered=%+v", entries)
	}
	ring.Append("[account=acc_1] warn line")
	byAccount := ring.Snapshot(0, 10, "", "", "acc_1")
	if len(byAccount) != 1 || byAccount[0].AccountID != "acc_1" {
		t.Fatalf("account filter=%+v", byAccount)
	}
}
