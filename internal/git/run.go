// Package git wraps the git command line.
package git

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"
)

// ErrNotRepo means that the directory is not inside a git repository.
var ErrNotRepo = errors.New("not a git repository")

// Runner runs git commands in one directory.
type Runner struct{ Dir string }

// Open returns a Runner for dir. It returns ErrNotRepo if git says that dir
// is not in a repository. Other errors keep the git message.
func Open(dir string) (Runner, error) {
	r := Runner{Dir: dir}
	if _, err := r.Run(context.Background(), "rev-parse", "--git-common-dir"); err != nil {
		if strings.Contains(err.Error(), "not a git repository") {
			return Runner{}, ErrNotRepo
		}
		return Runner{}, err
	}
	return r, nil
}

// Run runs git with args and returns stdout without the trailing newline.
// The error text includes stderr.
func (r Runner) Run(ctx context.Context, args ...string) (string, error) {
	return r.run(ctx, nil, args...)
}

// RunInput is Run with stdin as the input of git.
func (r Runner) RunInput(ctx context.Context, stdin string, args ...string) (string, error) {
	return r.run(ctx, strings.NewReader(stdin), args...)
}

func (r Runner) run(ctx context.Context, stdin io.Reader, args ...string) (string, error) {
	cmd := r.command(ctx, args...)
	if stdin != nil {
		cmd.Stdin = stdin
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			return "", fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
		}
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, msg)
	}
	return strings.TrimSuffix(stdout.String(), "\n"), nil
}

// waitDelay is the time that a git command has to stop after its context
// ends. Then Run closes the pipes and returns.
const waitDelay = 2 * time.Second

// command builds every git call: the global flags, the environment, and the
// wait delay. A command that only reads gets --no-optional-locks, so that it
// never takes index.lock from a git command in another terminal.
//
// A fetch runs in the background, so it must not ask for a password or a
// passphrase on the terminal. It starts in a new session with no stdin:
// then git and its ssh child have no controlling terminal and cannot open
// /dev/tty. The context still kills git and its children, and WaitDelay
// stops the wait for a child that keeps the pipes open.
func (r Runner) command(ctx context.Context, args ...string) *exec.Cmd {
	full := []string{"-C", r.Dir}
	if readOnly(args) {
		full = append(full, "--no-optional-locks")
	}
	cmd := exec.CommandContext(ctx, "git", append(full, args...)...)
	cmd.Env = gitEnv(os.Environ(), args)
	cmd.WaitDelay = waitDelay
	if isFetch(args) {
		cmd.Stdin = nil
		cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
		// The new session is a process group with the id of git. At the end
		// of the context, kill the whole group, so that ssh stops too.
		cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	}
	return cmd
}

func isFetch(args []string) bool { return len(args) > 0 && args[0] == "fetch" }

// readOnly reports whether the git command args only reads the repository.
func readOnly(args []string) bool {
	if len(args) == 0 {
		return false
	}
	switch args[0] {
	case "status", "rev-list", "log", "rev-parse", "symbolic-ref", "diff", "merge-base":
		return true
	case "worktree":
		return len(args) > 1 && args[1] == "list"
	}
	return false
}

// gitEnv returns the environment of the git command args. A fetch gets
// GIT_TERMINAL_PROMPT=0, so git does not ask for a user name or a password.
// The ssh command and the askpass settings of the user stay as they are. A
// merge also gets GIT_MERGE_AUTOEDIT=no, so git does not open an editor.
func gitEnv(environ, args []string) []string {
	set := map[string]string{}
	switch {
	case isFetch(args):
		set["GIT_TERMINAL_PROMPT"] = "0"
	case len(args) > 0 && args[0] == "merge":
		set["GIT_TERMINAL_PROMPT"] = "0"
		set["GIT_MERGE_AUTOEDIT"] = "no"
	default:
		return environ
	}
	env := make([]string, 0, len(environ)+len(set))
	for _, kv := range environ {
		if k, _, _ := strings.Cut(kv, "="); set[k] == "" {
			env = append(env, kv)
		}
	}
	for _, k := range []string{"GIT_TERMINAL_PROMPT", "GIT_MERGE_AUTOEDIT"} {
		if v := set[k]; v != "" {
			env = append(env, k+"="+v)
		}
	}
	return env
}

// In returns a Runner for the same repository in a different directory.
func (r Runner) In(path string) Runner { return Runner{Dir: path} }
