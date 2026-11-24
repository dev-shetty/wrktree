package git

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// ErrNoBase means that no base branch exists.
var ErrNoBase = errors.New("no base branch")

// ChangedFiles counts the changed and untracked files of the worktree at path.
// The flags make the count independent of the git configuration.
func (r Runner) ChangedFiles(ctx context.Context, path string) (int, error) {
	out, err := r.In(path).Run(ctx, "status", "--porcelain", "--untracked-files=all", "--ignore-submodules=none")
	if err != nil {
		return 0, err
	}
	n := 0
	for _, line := range strings.Split(out, "\n") {
		if strings.TrimSpace(line) != "" {
			n++
		}
	}
	return n, nil
}

// BaseBranch returns the base branch, for example "origin/master".
// It returns override if set. It returns ErrNoBase if no base exists.
func (r Runner) BaseBranch(ctx context.Context, override string) (string, error) {
	if override != "" {
		return override, nil
	}
	if out, err := r.Run(ctx, "symbolic-ref", "--short", "refs/remotes/origin/HEAD"); err == nil && out != "" {
		return out, nil
	}
	for _, c := range []string{"origin/main", "origin/master"} {
		if _, err := r.Run(ctx, "rev-parse", "--verify", "--quiet", c); err == nil {
			return c, nil
		}
	}
	return "", ErrNoBase
}

// Distance returns how many commits HEAD of the worktree at path is ahead of
// and behind base.
func (r Runner) Distance(ctx context.Context, path, base string) (ahead, behind int, err error) {
	out, err := r.In(path).Run(ctx, "rev-list", "--left-right", "--count", base+"...HEAD")
	if err != nil {
		return 0, 0, err
	}
	f := strings.Fields(out)
	if len(f) != 2 {
		return 0, 0, fmt.Errorf("unexpected rev-list output %q", out)
	}
	if behind, err = strconv.Atoi(f[0]); err != nil {
		return 0, 0, err
	}
	if ahead, err = strconv.Atoi(f[1]); err != nil {
		return 0, 0, err
	}
	return ahead, behind, nil
}

// LastCommit returns the commit time of HEAD in the worktree at path.
func (r Runner) LastCommit(ctx context.Context, path string) (time.Time, error) {
	out, err := r.In(path).Run(ctx, "log", "-1", "--format=%ct")
	if err != nil {
		return time.Time{}, err
	}
	sec, err := strconv.ParseInt(strings.TrimSpace(out), 10, 64)
	if err != nil {
		return time.Time{}, err
	}
	return time.Unix(sec, 0), nil
}

// FetchBase fetches the branch of base. "origin/master" runs
// "git fetch origin master". A base with no remote-tracking ref, for example
// a local branch "main", is not fetched, and that is no error. The remote is
// the longest configured remote name that base starts with, so a remote or a
// branch can contain a slash.
func (r Runner) FetchBase(ctx context.Context, base string) error {
	_, err := r.Run(ctx, "rev-parse", "--verify", "--quiet", "refs/remotes/"+base)
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
		return nil
	}
	if err != nil {
		return err
	}
	out, err := r.Run(ctx, "remote")
	if err != nil {
		return err
	}
	remote := ""
	for _, name := range strings.Fields(out) {
		if strings.HasPrefix(base, name+"/") && len(name) > len(remote) {
			remote = name
		}
	}
	if remote == "" {
		return fmt.Errorf("base %q matches no configured remote", base)
	}
	_, err = r.Run(ctx, "fetch", remote, strings.TrimPrefix(base, remote+"/"))
	return err
}
