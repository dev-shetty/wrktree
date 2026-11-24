// Package git wraps the git command line.
package git

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
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
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", r.Dir}, args...)...)
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

// In returns a Runner for the same repository in a different directory.
func (r Runner) In(path string) Runner { return Runner{Dir: path} }
