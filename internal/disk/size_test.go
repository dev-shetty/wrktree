package disk

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestHuman(t *testing.T) {
	cases := []struct {
		in   int64
		want string
	}{
		{0, "0 KB"},
		{1, "1 KB"},
		{1023, "1 KB"},
		{1024, "1 KB"},
		{512 * 1024, "512 KB"},
		{1<<20 - 1, "1 MB"},
		{340 * 1024 * 1024, "340 MB"},
		{1<<30 - 1, "1.0 GB"},
		{1503238553, "1.4 GB"},
		{15246635827, "14.2 GB"},
	}
	for _, c := range cases {
		if got := Human(c.in); got != c.want {
			t.Errorf("Human(%d) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestSize(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "big"), make([]byte, 1<<20), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := Size(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if got < 1<<20 || got > 2<<20 {
		t.Errorf("Size = %d, want between 1 MiB and 2 MiB", got)
	}
	if _, err := Size(context.Background(), filepath.Join(dir, "missing")); err == nil {
		t.Error("missing dir: want error")
	}
}

func TestDecide(t *testing.T) {
	now := time.Date(2026, 11, 24, 12, 0, 0, 0, time.UTC)
	ttl := 24 * time.Hour
	at := func(age time.Duration) Entry { return Entry{Bytes: 42, At: now.Add(-age)} }
	cases := []struct {
		name  string
		e     Entry
		ok    bool
		ttl   time.Duration
		fresh bool
		bytes int64
		state State
		run   bool
	}{
		{"no entry", Entry{}, false, ttl, false, 0, SizeNone, true},
		{"no entry fresh", Entry{}, false, ttl, true, 0, SizeNone, true},
		{"1 hour old", at(time.Hour), true, ttl, false, 42, SizeCached, false},
		{"25 hours old", at(25 * time.Hour), true, ttl, false, 42, SizeStale, true},
		{"1 hour old fresh", at(time.Hour), true, ttl, true, 42, SizeStale, true},
		{"age equals ttl", at(ttl), true, ttl, false, 42, SizeStale, true},
		{"ttl 0", at(0), true, 0, false, 42, SizeStale, true},
	}
	for _, c := range cases {
		b, s, r := Decide(c.e, c.ok, c.ttl, now, c.fresh)
		if b != c.bytes || s != c.state || r != c.run {
			t.Errorf("%s: got (%d, %v, %v), want (%d, %v, %v)", c.name, b, s, r, c.bytes, c.state, c.run)
		}
	}
}
