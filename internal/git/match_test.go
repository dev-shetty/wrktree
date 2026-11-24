package git

import (
	"errors"
	"testing"
)

func TestMatch(t *testing.T) {
	wts := []Worktree{
		{Path: "/w/mono", Branch: "master"},
		{Path: "/w/master-fix", Branch: "master-fix"},
		{Path: "/w/fix-1201-login", Branch: "fix-1201-login-redirect"},
		{Path: "/w/folder-x", Branch: "fix-1458-keys"},
	}
	for _, c := range []struct{ name, branch string }{
		{"master", "master"},
		{"1201", "fix-1201-login-redirect"},
		{"FIX-1201", "fix-1201-login-redirect"},
		{"folder-x", "fix-1458-keys"},
	} {
		got, err := Match(wts, c.name)
		if err != nil || got.Branch != c.branch {
			t.Errorf("Match(%q) = %+v, %v; want branch %q", c.name, got, err, c.branch)
		}
	}

	_, err := Match(wts, "fix-1")
	var amb *AmbiguousError
	if !errors.As(err, &amb) || len(amb.Matches) != 2 {
		t.Fatalf("fix-1: err = %v, want *AmbiguousError with 2 matches", err)
	}

	_, err = Match(wts, "zzz")
	if !errors.Is(err, ErrNoMatch) {
		t.Fatalf("zzz: err = %v, want ErrNoMatch", err)
	}
}
