package cmd

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dev-shetty/wrktree/internal/config"
	"github.com/dev-shetty/wrktree/internal/ui"
)

// screenRun runs the root command with no arguments. screen replaces the
// interactive screen. The terminal flag is terminal.
func screenRun(t *testing.T, dir string, terminal bool, o runOpts, screen func(ui.Deps) (ui.Result, error)) (string, string, error) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	c := New(Env{
		Dir:        dir,
		Stdin:      strings.NewReader(""),
		Stdout:     &stdout,
		Stderr:     &stderr,
		IsTerminal: func() bool { return terminal },
		RunOpen: func(template, path string) error {
			if o.opened != nil {
				*o.opened = opened{template, path}
			}
			return o.openErr
		},
		RunScreen: screen,
	})
	c.SetArgs(nil)
	err := c.Execute()
	return stdout.String(), stderr.String(), err
}

func fakeScreen(res ui.Result, err error, got *ui.Deps) func(ui.Deps) (ui.Result, error) {
	return func(d ui.Deps) (ui.Result, error) {
		if got != nil {
			*got = d
		}
		return res, err
	}
}

func TestRootNoTerminalRunsLs(t *testing.T) {
	isolate(t)
	repo, _ := twoWorktrees(t)
	called := false
	screen := func(ui.Deps) (ui.Result, error) { called = true; return ui.Result{}, nil }
	out, _, err := screenRun(t, repo.Dir, false, runOpts{}, screen)
	if err != nil {
		t.Fatal(err)
	}
	if called {
		t.Error("the screen runs without a terminal")
	}
	for _, s := range []string{"BRANCH", "master", "feat-a", "origin/master"} {
		if !strings.Contains(out, s) {
			t.Errorf("stdout lacks %q:\n%s", s, out)
		}
	}
}

func TestRootScreenSwitch(t *testing.T) {
	isolate(t)
	repo, feat := twoWorktrees(t)
	main := repo.Dir
	if r, err := filepath.EvalSymlinks(main); err == nil {
		main = r
	}

	t.Setenv("WRKTREE_CD_FILE", "")
	var d ui.Deps
	out, stderr, err := screenRun(t, feat, true, runOpts{}, fakeScreen(ui.Result{SwitchPath: main}, nil, &d))
	if err != nil {
		t.Fatal(err)
	}
	if out != main+"\n" || !strings.Contains(stderr, "wrktree init zsh") {
		t.Errorf("stdout %q, stderr %q", out, stderr)
	}
	if d.CanCD {
		t.Error("CanCD is true with no cd file")
	}
	if d.Repo != main || d.Name != "main" || d.Cache == nil || d.Git.Dir == "" || d.Now == nil {
		t.Errorf("Deps = %+v", d)
	}
	if cwd, _ := filepath.Abs(feat); d.Cwd != cwd {
		t.Errorf("Cwd = %q, want %q", d.Cwd, cwd)
	}
	if d.Cfg.Open != "code -n {path}" {
		t.Errorf("Cfg = %+v", d.Cfg)
	}

	cdFile := filepath.Join(t.TempDir(), "cd")
	t.Setenv("WRKTREE_CD_FILE", cdFile)
	out, _, err = screenRun(t, repo.Dir, true, runOpts{}, fakeScreen(ui.Result{SwitchPath: feat}, nil, &d))
	if err != nil || out != "" {
		t.Fatalf("err %v, stdout %q", err, out)
	}
	if b, _ := os.ReadFile(cdFile); string(b) != feat {
		t.Errorf("cd file = %q, want %q", b, feat)
	}
	if !d.CanCD {
		t.Error("CanCD is false with a cd file")
	}
}

func TestRootScreenOpen(t *testing.T) {
	isolate(t)
	repo, feat := twoWorktrees(t)
	var got opened
	out, _, err := screenRun(t, repo.Dir, true, runOpts{opened: &got}, fakeScreen(ui.Result{OpenPath: feat}, nil, nil))
	if err != nil || out != "" {
		t.Fatalf("err %v, stdout %q", err, out)
	}
	if got.template != "code -n {path}" || got.path != feat {
		t.Errorf("opened %+v", got)
	}

	_, _, err = screenRun(t, repo.Dir, true, runOpts{openErr: exec127(t)}, fakeScreen(ui.Result{OpenPath: feat}, nil, nil))
	want := `open command not found: code -n {path} (set "open" in ` + config.Path() + ")"
	if err == nil || err.Error() != want {
		t.Errorf("err = %v", err)
	}
}

func TestRootScreenQuitDoesNothing(t *testing.T) {
	isolate(t)
	repo, _ := twoWorktrees(t)
	var got opened
	out, stderr, err := screenRun(t, repo.Dir, true, runOpts{opened: &got}, fakeScreen(ui.Result{}, nil, nil))
	if err != nil || out != "" || stderr != "" || got != (opened{}) {
		t.Errorf("err %v, stdout %q, stderr %q, opened %+v", err, out, stderr, got)
	}
}

func TestRootScreenMessage(t *testing.T) {
	isolate(t)
	repo, _ := twoWorktrees(t)
	t.Setenv("WRKTREE_CD_FILE", filepath.Join(t.TempDir(), "cd"))
	res := ui.Result{SwitchPath: repo.Dir, Message: "deleted /x, but the branch stays: boom"}
	_, stderr, err := screenRun(t, repo.Dir, true, runOpts{}, fakeScreen(res, nil, nil))
	if err != nil || stderr != "wrktree: deleted /x, but the branch stays: boom\n" {
		t.Errorf("err %v, stderr %q", err, stderr)
	}
}

func TestRootScreenError(t *testing.T) {
	isolate(t)
	repo, _ := twoWorktrees(t)
	_, _, err := screenRun(t, repo.Dir, true, runOpts{}, fakeScreen(ui.Result{}, errors.New("no tty"), nil))
	if err == nil || !strings.Contains(err.Error(), "no tty") {
		t.Errorf("err = %v", err)
	}
}

func TestRootScreenNotRepo(t *testing.T) {
	isolate(t)
	_, _, err := screenRun(t, t.TempDir(), true, runOpts{}, fakeScreen(ui.Result{}, nil, nil))
	if err == nil || err.Error() != "not a git repository" {
		t.Errorf("err = %v", err)
	}
}
