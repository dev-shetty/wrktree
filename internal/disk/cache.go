package disk

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Entry is one cached size.
type Entry struct {
	Bytes int64     `json:"bytes"`
	At    time.Time `json:"at"`
}

// Cache stores sizes by repo and path. It is safe for concurrent use.
type Cache struct {
	file string
	mu   sync.Mutex
	m    map[string]Entry
}

func key(repo, path string) string { return repo + "\x00" + path }

// DefaultCachePath returns $XDG_CACHE_HOME/wrktree/sizes.json, or
// ~/.cache/wrktree/sizes.json when XDG_CACHE_HOME is empty.
func DefaultCachePath() string {
	base := os.Getenv("XDG_CACHE_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			home = "."
		}
		base = filepath.Join(home, ".cache")
	}
	return filepath.Join(base, "wrktree", "sizes.json")
}

// LoadCache reads file. A missing or damaged file gives an empty cache.
func LoadCache(file string) *Cache {
	c := &Cache{file: file, m: map[string]Entry{}}
	data, err := os.ReadFile(file)
	if err != nil {
		return c
	}
	var m map[string]Entry
	if json.Unmarshal(data, &m) != nil || m == nil {
		return c
	}
	c.m = m
	return c
}

func (c *Cache) Get(repo, path string) (Entry, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.m[key(repo, path)]
	return e, ok
}

func (c *Cache) Set(repo, path string, bytes int64, at time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.m[key(repo, path)] = Entry{Bytes: bytes, At: at}
}

func (c *Cache) Delete(repo, path string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.m, key(repo, path))
}

// Retain removes the entries of repo whose path is not in paths.
func (c *Cache) Retain(repo string, paths []string) {
	keep := make(map[string]bool, len(paths))
	for _, p := range paths {
		keep[key(repo, p)] = true
	}
	prefix := repo + "\x00"
	c.mu.Lock()
	defer c.mu.Unlock()
	for k := range c.m {
		if len(k) >= len(prefix) && k[:len(prefix)] == prefix && !keep[k] {
			delete(c.m, k)
		}
	}
}

// Save writes the cache to disk. It creates the directory. It writes a
// temporary file, then renames it.
func (c *Cache) Save() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	data, err := json.Marshal(c.m)
	if err != nil {
		return err
	}
	dir := filepath.Dir(c.file)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".sizes-*.tmp")
	if err != nil {
		return err
	}
	_, werr := tmp.Write(data)
	cerr := tmp.Close()
	if werr != nil || cerr != nil {
		os.Remove(tmp.Name())
		if werr != nil {
			return werr
		}
		return cerr
	}
	if err := os.Rename(tmp.Name(), c.file); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return nil
}
