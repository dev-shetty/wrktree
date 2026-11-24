package disk

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestCacheRoundTrip(t *testing.T) {
	file := filepath.Join(t.TempDir(), "sub", "dir", "sizes.json")
	at := time.Date(2026, 11, 24, 12, 0, 0, 0, time.UTC)
	c := LoadCache(file)
	c.Set("/repo", "/repo/a", 100, at)
	c.Set("/repo", "/repo/b", 200, at)
	c.Set("/other", "/other/a", 300, at)
	if err := c.Save(); err != nil {
		t.Fatal(err)
	}

	c2 := LoadCache(file)
	e, ok := c2.Get("/repo", "/repo/a")
	if !ok || e.Bytes != 100 || !e.At.Equal(at) {
		t.Errorf("Get a = %+v, %v", e, ok)
	}
	if e, ok := c2.Get("/repo", "/repo/b"); !ok || e.Bytes != 200 {
		t.Errorf("Get b = %+v, %v", e, ok)
	}

	c2.Retain("/repo", []string{"/repo/a"})
	if _, ok := c2.Get("/repo", "/repo/b"); ok {
		t.Error("b must be removed by Retain")
	}
	if _, ok := c2.Get("/repo", "/repo/a"); !ok {
		t.Error("a must stay")
	}
	if _, ok := c2.Get("/other", "/other/a"); !ok {
		t.Error("entry of other repo must stay")
	}

	c2.Delete("/repo", "/repo/a")
	if _, ok := c2.Get("/repo", "/repo/a"); ok {
		t.Error("a must be deleted")
	}

	entries, _ := os.ReadDir(filepath.Dir(file))
	if len(entries) != 1 {
		t.Errorf("want only the cache file, got %d entries", len(entries))
	}
}

func TestLoadCacheDamaged(t *testing.T) {
	file := filepath.Join(t.TempDir(), "sizes.json")
	if err := os.WriteFile(file, []byte("not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	c := LoadCache(file)
	if _, ok := c.Get("r", "p"); ok {
		t.Error("damaged file: want empty cache")
	}
	c.Set("r", "p", 1, time.Now())
	if err := c.Save(); err != nil {
		t.Fatal(err)
	}
	if _, ok := LoadCache(file).Get("r", "p"); !ok {
		t.Error("cache must work after damaged load")
	}
	if c := LoadCache(filepath.Join(t.TempDir(), "missing.json")); c == nil {
		t.Error("missing file: want empty cache")
	}
}

func TestCacheConcurrent(t *testing.T) {
	c := LoadCache(filepath.Join(t.TempDir(), "sizes.json"))
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c.Set("r", "p", int64(i), time.Now())
			c.Get("r", "p")
			_ = c.Save()
		}()
	}
	wg.Wait()
}

func TestDefaultCachePath(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", "/x/cache")
	t.Setenv("HOME", "/h")
	if got, want := DefaultCachePath(), "/x/cache/wrktree/sizes.json"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	t.Setenv("XDG_CACHE_HOME", "")
	if got, want := DefaultCachePath(), "/h/.cache/wrktree/sizes.json"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
