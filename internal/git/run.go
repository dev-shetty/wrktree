// Package git wraps the git command line.
package git

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// ErrNotRepo means that the directory is not inside a git repository.
var ErrNotRepo = errors.New("not a git repository")

// Runner runs git commands in one directory.
type Runner struct{ Dir string }

// Open returns a Runner for dir. It returns ErrNotRepo if dir is not in a repository.
func Open(dir string) (Runner, error) {
	r := Runner{Dir: dir}
	if _, err := r.Run(context.Background(), "rev-parse", "--git-common-dir"); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return Runner{}, ErrNotRepo
		}
		return Runner{}, err
	}
	return r, nil
}

// Run runs git with args and returns stdout without the trailing newline.
// The error text includes stderr.
func (r Runner) Run(ctx context.Context, args ...string) (string, error) {
	cmd := r.command(ctx, args...)
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
func (r Runner) command(ctx context.Context, args ...string) *exec.Cmd {
	full := []string{"-C", r.Dir}
	if readOnly(args) {
		full = append(full, "--no-optional-locks")
	}
	cmd := exec.CommandContext(ctx, "git", append(full, args...)...)
	cmd.Env = gitEnv(os.Environ(), args)
	cmd.WaitDelay = waitDelay
	return cmd
}

// readOnly reports whether the git command args only reads the repository.
func readOnly(args []string) bool {
	if len(args) == 0 {
		return false
	}
	switch args[0] {
	case "status", "rev-list", "log", "rev-parse", "symbolic-ref":
		return true
	case "worktree":
		return len(args) > 1 && args[1] == "list"
	}
	return false
}

// gitEnv returns the environment of the git command args. A fetch runs in
// the background, so it must not ask for a password or a passphrase on the
// terminal. A GIT_SSH_COMMAND of the user stays.
func gitEnv(environ, args []string) []string {
	if len(args) == 0 || args[0] != "fetch" {
		return environ
	}
	env := make([]string, 0, len(environ)+2)
	userSSH := false
	for _, kv := range environ {
		k, v, _ := strings.Cut(kv, "=")
		switch {
		case k == "GIT_TERMINAL_PROMPT":
			continue
		case k == "GIT_SSH_COMMAND" && v != "":
			userSSH = true
		}
		env = append(env, kv)
	}
	env = append(env, "GIT_TERMINAL_PROMPT=0")
	if !userSSH {
		env = append(env, "GIT_SSH_COMMAND=ssh -oBatchMode=yes")
	}
	return env
}

// In returns a Runner for the same repository in a different directory.
func (r Runner) In(path string) Runner { return Runner{Dir: path} }
