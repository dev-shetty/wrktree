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
	// changed holds the keys that this process set (true) or removed (false)
	// after the last Save. Save writes only these keys to the file, so it
	// keeps the entries that a different process wrote.
	changed map[string]bool
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
	return &Cache{file: file, m: readFile(file), changed: map[string]bool{}}
}

// readFile returns the entries of file. A missing or damaged file gives an
// empty map.
func readFile(file string) map[string]Entry {
	data, err := os.ReadFile(file)
	if err != nil {
		return map[string]Entry{}
	}
	var m map[string]Entry
	if json.Unmarshal(data, &m) != nil || m == nil {
		return map[string]Entry{}
	}
	return m
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
	c.changed[key(repo, path)] = true
}

func (c *Cache) Delete(repo, path string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.m, key(repo, path))
	c.changed[key(repo, path)] = false
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
			c.changed[k] = false
		}
	}
}

// Save writes the cache to disk. It creates the directory. It reads the
// file again and applies only the changes of this process, so the entries
// of a different process stay. It writes a temporary file, then renames it.
func (c *Cache) Save() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	disk := readFile(c.file)
	for k, set := range c.changed {
		if set {
			disk[k] = c.m[k]
		} else {
			delete(disk, k)
		}
	}
	data, err := json.Marshal(disk)
	if err != nil {
		return err
	}
	if err := c.write(data); err != nil {
		return err
	}
	c.changed = map[string]bool{}
	return nil
}

func (c *Cache) write(data []byte) error {
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
