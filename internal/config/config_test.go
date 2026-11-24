package config

import (
	"os"
	"path/filepath"
	"strings"
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

func TestDefault(t *testing.T) {
	want := Config{Open: "code -n {path}", Base: "", Fetch: true, SizeTTL: 24 * time.Hour}
	if got := Default(); got != want {
		t.Errorf("Default() = %+v, want %+v", got, want)
	}
}

func TestLoadMissing(t *testing.T) {
	got, err := Load("/no/such/config.toml")
	if err != nil {
		t.Fatal(err)
	}
	if got != Default() {
		t.Errorf("got %+v, want default", got)
	}
}

func TestLoadPartial(t *testing.T) {
	got, err := Load(write(t, `open = "cursor -n {path}"`))
	if err != nil {
		t.Fatal(err)
	}
	want := Default()
	want.Open = "cursor -n {path}"
	if got != want {
		t.Errorf("got %+v, want %+v", got, want)
	}
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

func TestLoadBadTTL(t *testing.T) {
	_, err := Load(write(t, `size_ttl = "soon"`))
	if err == nil || !strings.Contains(err.Error(), "size_ttl") {
		t.Errorf("err = %v, want error that names size_ttl", err)
	}
}

func TestLoadUnknownKey(t *testing.T) {
	_, err := Load(write(t, `colour = "red"`))
	if err == nil || !strings.Contains(err.Error(), "colour") {
		t.Errorf("err = %v, want error that names colour", err)
	}
}

func TestLoadMalformed(t *testing.T) {
	f := write(t, `open = = "x"`)
	_, err := Load(f)
	if err == nil || !strings.Contains(err.Error(), f) {
		t.Errorf("err = %v, want error that names %s", err, f)
	}
}

func TestPathXDG(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/x")
	if got, want := Path(), "/x/wrktree/config.toml"; got != want {
		t.Errorf("Path() = %q, want %q", got, want)
	}
}

func TestPathHome(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", "/h")
	if got, want := Path(), "/h/.config/wrktree/config.toml"; got != want {
		t.Errorf("Path() = %q, want %q", got, want)
	}
}
