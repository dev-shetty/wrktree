package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dev-shetty/wrktree/internal/testrepo"
)

type opened struct{ template, path string }

// runOpts holds the fakes for one run.
type runOpts struct {
	openErr error
	opened  *opened
}

func runWith(t *testing.T, dir string, o runOpts, args ...string) (string, string, error) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	c := New(Env{
		Dir:        dir,
		Stdin:      strings.NewReader(""),
		Stdout:     &stdout,
		Stderr:     &stderr,
		IsTerminal: func() bool { return false },
		RunOpen: func(template, path string) error {
			if o.opened != nil {
				*o.opened = opened{template, path}
			}
			return o.openErr
		},
	})
	c.SetArgs(args)
	err := c.Execute()
	return stdout.String(), stderr.String(), err
}

func run(t *testing.T, dir string, args ...string) (string, string, error) {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(t.TempDir(), "config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(t.TempDir(), "cache"))
	return runWith(t, dir, runOpts{}, args...)
}

func twoWorktrees(t *testing.T) (*testrepo.Repo, string) {
	t.Helper()
	repo := testrepo.New(t)
	feat := repo.AddWorktree(t, "feat-a")
	// git prints real paths. On macOS the temporary directory is a symlink.
	if real, err := filepath.EvalSymlinks(feat); err == nil {
		feat = real
	}
	return repo, feat
}

func TestLs(t *testing.T) {
	repo, _ := twoWorktrees(t)
	out, _, err := run(t, repo.Dir, "ls")
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{"master", "feat-a", "origin/master"} {
		if !strings.Contains(out, s) {
			t.Errorf("output lacks %q:\n%s", s, out)
		}
	}
}

func TestSwitch(t *testing.T) {
	repo, feat := twoWorktrees(t)

	cdFile := filepath.Join(t.TempDir(), "cd")
	t.Setenv("WRKTREE_CD_FILE", cdFile)
	out, _, err := run(t, repo.Dir, "switch", "feat-a")
	if err != nil || out != "" {
		t.Fatalf("err %v, stdout %q", err, out)
	}
	if b, _ := os.ReadFile(cdFile); string(b) != feat {
		t.Errorf("cd file = %q, want %q", b, feat)
	}

	t.Setenv("WRKTREE_CD_FILE", "")
	out, _, err = run(t, repo.Dir, "switch", "feat-a")
	if err != nil || out != feat+"\n" {
		t.Errorf("err %v, stdout %q", err, out)
	}

	repo.AddWorktree(t, "feat-b")
	_, stderr, err := run(t, repo.Dir, "switch", "feat")
	if err == nil {
		t.Fatal("want error for ambiguous name")
	}
	for _, s := range []string{"feat-a", "feat-b"} {
		if !strings.Contains(stderr, s) {
			t.Errorf("stderr lacks %q:\n%s", s, stderr)
		}
	}

	_, _, err = run(t, repo.Dir, "switch", "zzz")
	if err == nil {
		t.Errorf("err = %v", err)
	}
}

func TestOpen(t *testing.T) {
	repo, feat := twoWorktrees(t)
	var got opened
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	// With no configuration file, the open command is $VISUAL, then $EDITOR, then VS Code.
	for _, c := range []struct{ visual, editor, want string }{
		{"", "", "code -n {path}"},
		{"", "vim", "vim"},
		{"zed", "vim", "zed"},
	} {
		t.Setenv("VISUAL", c.visual)
		t.Setenv("EDITOR", c.editor)
		if _, _, err := runWith(t, repo.Dir, runOpts{opened: &got}, "open", "feat-a"); err != nil {
			t.Fatal(err)
		}
		if got.template != c.want || got.path != feat {
			t.Errorf("VISUAL=%q EDITOR=%q: got %+v, want template %q", c.visual, c.editor, got, c.want)
		}
	}

	cfg := filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "wrktree", "config.toml")
	os.MkdirAll(filepath.Dir(cfg), 0o755)
	os.WriteFile(cfg, []byte("open = \"cursor {path}\"\n"), 0o644)
	if _, _, err := runWith(t, repo.Dir, runOpts{opened: &got}, "open", "feat-a"); err != nil {
		t.Fatal(err)
	}
	if got.template != "cursor {path}" {
		t.Errorf("got %+v", got)
	}
}
