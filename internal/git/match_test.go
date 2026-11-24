package git

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestCurrentSubdirAndSymlink(t *testing.T) {
	root := t.TempDir()
	mk := func(name string) string {
		p := filepath.Join(root, name)
		if err := os.MkdirAll(p, 0o755); err != nil {
			t.Fatal(err)
		}
		return p
	}
	a, ab, feat := mk("a"), mk("a-b"), mk("feat")
	sub := filepath.Join(feat, "sub", "dir")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "link")
	if err := os.Symlink(feat, link); err != nil {
		t.Fatal(err)
	}
	wts := []Worktree{{Path: a}, {Path: ab}, {Path: feat}}

	cases := []struct {
		name, cwd string
		want      int
	}{
		{"subdir", sub, 2},
		{"symlink", link, 2},
		{"outside", t.TempDir(), -1},
		{"prefix sibling", ab, 1},
		{"exact", a, 0},
	}
	for _, c := range cases {
		if got := Current(wts, c.cwd); got != c.want {
			t.Errorf("%s: Current = %d, want %d", c.name, got, c.want)
		}
	}
}

func TestMatch(t *testing.T) {
	wts := []Worktree{
		{Path: "/w/mono", Branch: "master"},
		{Path: "/w/master-fix", Branch: "master-fix"},
		{Path: "/w/eng-6201-alerts", Branch: "eng-6201-onboarding-v2-alerts"},
		{Path: "/w/folder-x", Branch: "eng-6458-keys"},
	}
	for _, c := range []struct{ name, branch string }{
		{"master", "master"},
		{"6201", "eng-6201-onboarding-v2-alerts"},
		{"ENG-6201", "eng-6201-onboarding-v2-alerts"},
		{"folder-x", "eng-6458-keys"},
	} {
		got, err := Match(wts, c.name)
		if err != nil || got.Branch != c.branch {
			t.Errorf("Match(%q) = %+v, %v; want branch %q", c.name, got, err, c.branch)
		}
	}

	_, err := Match(wts, "eng-6")
	var amb *AmbiguousError
	if !errors.As(err, &amb) || len(amb.Matches) != 2 {
		t.Fatalf("eng-6: err = %v, want *AmbiguousError with 2 matches", err)
	}
	if want := `more than one worktree matches "eng-6"`; err.Error() != want {
		t.Errorf("message = %q, want %q", err.Error(), want)
	}

	_, err = Match(wts, "zzz")
	if !errors.Is(err, ErrNoMatch) {
		t.Fatalf("zzz: err = %v, want ErrNoMatch", err)
	}
	if want := `no worktree matches "zzz"`; err.Error() != want {
		t.Errorf("message = %q, want %q", err.Error(), want)
	}
}

func TestMatchEmptyName(t *testing.T) {
	wts := []Worktree{{Path: "/w/mono", Branch: "master"}}
	for _, name := range []string{"", "   "} {
		_, err := Match(wts, name)
		if !errors.Is(err, ErrNoMatch) {
			t.Errorf("Match(%q): err = %v, want ErrNoMatch", name, err)
		}
	}
}
