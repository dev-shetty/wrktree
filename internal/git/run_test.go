package git

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/dev-shetty/wrktree/internal/testrepo"
)

func TestCommandFlags(t *testing.T) {
	r := Runner{Dir: "/r"}
	for _, tc := range []struct {
		args     []string
		readOnly bool
	}{
		{[]string{"status", "--porcelain"}, true},
		{[]string{"rev-list", "--count", "HEAD"}, true},
		{[]string{"log", "-1"}, true},
		{[]string{"rev-parse", "--git-common-dir"}, true},
		{[]string{"symbolic-ref", "--short", "refs/remotes/origin/HEAD"}, true},
		{[]string{"worktree", "list", "--porcelain"}, true},
		{[]string{"worktree", "remove", "/r/x"}, false},
		{[]string{"worktree", "prune", "-v"}, false},
		{[]string{"branch", "-d", "x"}, false},
		{[]string{"fetch", "origin", "master"}, false},
	} {
		cmd := r.command(context.Background(), tc.args...)
		want := []string{"git", "-C", "/r"}
		if tc.readOnly {
			want = append(want, "--no-optional-locks")
		}
		want = append(want, tc.args...)
		if !slices.Equal(cmd.Args, want) {
			t.Errorf("%v: Args = %q, want %q", tc.args, cmd.Args, want)
		}
		if cmd.WaitDelay != 2*time.Second {
			t.Errorf("%v: WaitDelay = %v, want 2s", tc.args, cmd.WaitDelay)
		}
	}
}

func envValue(env []string, key string) (string, bool) {
	val, ok := "", false
	for _, kv := range env {
		if k, v, _ := strings.Cut(kv, "="); k == key {
			val, ok = v, true
		}
	}
	return val, ok
}

func TestGitEnv(t *testing.T) {
	base := []string{"HOME=/h", "GIT_TERMINAL_PROMPT=1"}
	fetch := []string{"fetch", "origin", "master"}

	env := gitEnv(base, fetch)
	if v, _ := envValue(env, "GIT_TERMINAL_PROMPT"); v != "0" {
		t.Errorf("GIT_TERMINAL_PROMPT = %q, want 0", v)
	}
	// The ssh command of the user (core.sshCommand) must stay in effect.
	if v, ok := envValue(env, "GIT_SSH_COMMAND"); ok {
		t.Errorf("GIT_SSH_COMMAND = %q, want it not set", v)
	}
	if v, _ := envValue(env, "HOME"); v != "/h" {
		t.Errorf("HOME = %q", v)
	}

	env = gitEnv(append(base, "GIT_SSH_COMMAND=ssh -i key"), fetch)
	if v, _ := envValue(env, "GIT_SSH_COMMAND"); v != "ssh -i key" {
		t.Errorf("user GIT_SSH_COMMAND = %q, want it kept", v)
	}
	if v, _ := envValue(env, "GIT_TERMINAL_PROMPT"); v != "0" {
		t.Errorf("with user ssh: GIT_TERMINAL_PROMPT = %q, want 0", v)
	}

	env = gitEnv(base, []string{"status"})
	if !slices.Equal(env, base) {
		t.Errorf("status: env = %q, want %q", env, base)
	}
}

// The real fetch command gets the environment and runs in a new session
// with no stdin, so neither git nor ssh can open the terminal for a prompt.
// Other git calls do not.
func TestFetchCommandNoTerminal(t *testing.T) {
	t.Setenv("GIT_SSH_COMMAND", "")
	os.Unsetenv("GIT_SSH_COMMAND")
	r := Runner{Dir: "/r"}
	cmd := r.command(context.Background(), "fetch", "origin", "master")
	if v, _ := envValue(cmd.Env, "GIT_TERMINAL_PROMPT"); v != "0" {
		t.Errorf("GIT_TERMINAL_PROMPT = %q, want 0", v)
	}
	if v, ok := envValue(cmd.Env, "GIT_SSH_COMMAND"); ok {
		t.Errorf("GIT_SSH_COMMAND = %q, want it not set", v)
	}
	if cmd.SysProcAttr == nil || !cmd.SysProcAttr.Setsid || cmd.Stdin != nil {
		t.Errorf("fetch: SysProcAttr %+v, Stdin %v; want Setsid and no stdin", cmd.SysProcAttr, cmd.Stdin)
	}
	for _, args := range [][]string{{"status"}, {"worktree", "remove", "/r/x"}, {"branch", "-d", "x"}} {
		if c := r.command(context.Background(), args...); c.SysProcAttr != nil && c.SysProcAttr.Setsid {
			t.Errorf("%v runs in a new session", args)
		}
	}
}

// Open gives ErrNotRepo only for a folder outside a repository. Other git
// errors keep the git message.
func TestOpenErrors(t *testing.T) {
	if _, err := Open(t.TempDir()); !errors.Is(err, ErrNotRepo) {
		t.Errorf("folder outside a repository: err = %v, want ErrNotRepo", err)
	}
	_, err := Open(filepath.Join(t.TempDir(), "missing"))
	if err == nil || errors.Is(err, ErrNotRepo) || !strings.Contains(err.Error(), "cannot change to") {
		t.Errorf("missing folder: err = %v, want the git message", err)
	}
}

func TestPruneVerbose(t *testing.T) {
	repo := testrepo.New(t)
	trace := filepath.Join(t.TempDir(), "trace")
	t.Setenv("GIT_TRACE", trace)
	if err := (Runner{Dir: repo.Dir}).Prune(context.Background()); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(trace)
	if err != nil || !strings.Contains(string(b), "built-in: git worktree prune -v") {
		t.Errorf("trace (%v):\n%s", err, b)
	}
}
