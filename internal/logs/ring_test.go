package logs

import (
	"fmt"
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
	if strings.Contains(entries[0].Message, "abcdef") {
		t.Fatalf("api key not redacted: %q", entries[0].Message)
	}
	if strings.Contains(entries[1].Message, "super-secret-token") || !strings.Contains(entries[1].Message, "***") {
		t.Fatalf("bearer not redacted: %q", entries[1].Message)
	}
	if !strings.Contains(entries[2].Message, "warm complete") {
		t.Fatalf("oldest kept unexpectedly: %+v", entries[2])
	}
	if entries[2].AccountID != "acc_1" || entries[2].Source != "daemon" {
		t.Fatalf("account/source = %+v", entries[2])
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
	if entries[1].AccountID != "acc_x" || !strings.Contains(entries[1].Message, "[account=acc_x] hello") {
		t.Fatalf("first=%+v", entries[1])
	}
	if entries[0].AccountID != "acc_x" {
		t.Fatalf("second=%+v", entries[0])
	}
}

func TestRingSnapshotFilters(t *testing.T) {
	ring := NewRing(10)
	ring.Append("ok line")
	ring.Append("something failed hard")
	entries, total := ring.Snapshot(0, 10, 0, "error", "failed", "")
	if total != 1 || len(entries) != 1 || entries[0].Level != "error" {
		t.Fatalf("filtered=%+v total=%d", entries, total)
	}
	ring.Append("[account=acc_1] warn line")
	byAccount, accountTotal := ring.Snapshot(0, 10, 0, "", "", "acc_1")
	if accountTotal != 1 || len(byAccount) != 1 || byAccount[0].AccountID != "acc_1" {
		t.Fatalf("account filter=%+v total=%d", byAccount, accountTotal)
	}
}

func TestRingSnapshotPaginatesNewestFirst(t *testing.T) {
	ring := NewRing(20)
	for i := 1; i <= 5; i++ {
		ring.Append(fmt.Sprintf("line %d", i))
	}
	page, total := ring.Snapshot(0, 2, 0, "", "", "")
	if total != 5 || len(page) != 2 {
		t.Fatalf("page1 total=%d items=%+v", total, page)
	}
	if !strings.Contains(page[0].Message, "line 5") || !strings.Contains(page[1].Message, "line 4") {
		t.Fatalf("expected newest first: %+v", page)
	}
	page2, total := ring.Snapshot(0, 2, 2, "", "", "")
	if total != 5 || len(page2) != 2 {
		t.Fatalf("page2 total=%d items=%+v", total, page2)
	}
	if !strings.Contains(page2[0].Message, "line 3") || !strings.Contains(page2[1].Message, "line 2") {
		t.Fatalf("expected older page: %+v", page2)
	}
	pastEnd, total := ring.Snapshot(0, 2, 20, "", "", "")
	if total != 5 || len(pastEnd) != 0 {
		t.Fatalf("past end total=%d items=%+v", total, pastEnd)
	}
}
