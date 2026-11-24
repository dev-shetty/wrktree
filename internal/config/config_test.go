package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func write(t *testing.T, text string) string {
	t.Helper()
	f := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(f, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	return f
}

func TestLoadAll(t *testing.T) {
	got, err := Load(write(t, "fetch = false\nsize_ttl = \"1h\"\nbase = \"origin/develop\"\nopen = \"vim {path}\"\n"))
	if err != nil {
		t.Fatal(err)
	}
	want := Config{Open: "vim {path}", Base: "origin/develop", Fetch: false, SizeTTL: time.Hour}
	if got != want {
		t.Errorf("got %+v, want %+v", got, want)
	}
}
