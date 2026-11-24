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

// mergedScanLimit is the largest count of base commits that Merged reads.
// A var, so that a test can make it small.
var mergedScanLimit = 2000

// diffFlags make the diff text independent of the git configuration.
var diffFlags = []string{"--no-color", "--no-ext-diff", "--no-textconv"}

// Merged reports if the base branch already contains the changes of the
// worktree at path. ok is false when the answer is not known.
//
// HEAD is merged if it is an ancestor of base, if it adds no change to the
// merge base, or if a commit on base since the merge base has the same patch
// id as the whole change of HEAD. The last test finds a squash merge. Merged
// runs only commands that read, and it writes no objects.
func (r Runner) Merged(ctx context.Context, path, base string) (merged, ok bool, err error) {
	g := r.In(path)
	head, err := g.Run(ctx, "rev-parse", "HEAD")
	if err != nil {
		return false, false, err
	}
	tip, err := g.Run(ctx, "rev-parse", base)
	if err != nil {
		return false, false, err
	}
	if head == tip {
		return false, true, nil
	}
	mb, err := g.Run(ctx, "merge-base", base, "HEAD")
	if err != nil {
		return false, false, err
	}
	if mb == head {
		return true, true, nil
	}
	diff, err := g.Run(ctx, append(append([]string{"diff"}, diffFlags...), mb, "HEAD")...)
	if err != nil {
		return false, false, err
	}
	if strings.TrimSpace(diff) == "" {
		return true, true, nil
	}
	out, err := g.Run(ctx, "rev-list", "--count", "--no-merges", mb+".."+base)
	if err != nil {
		return false, false, err
	}
	n, err := strconv.Atoi(strings.TrimSpace(out))
	if err != nil {
		return false, false, err
	}
	if n > mergedScanLimit {
		return false, false, nil
	}
	if n == 0 {
		return false, true, nil
	}
	want, err := g.patchIDs(ctx, diff+"\n")
	if err != nil || len(want) == 0 {
		return false, false, err
	}
	logArgs := append(append([]string{"log", "-p", "--no-merges", "--format=commit %H"}, diffFlags...), mb+".."+base)
	log, err := g.Run(ctx, logArgs...)
	if err != nil {
		return false, false, err
	}
	have, err := g.patchIDs(ctx, log+"\n")
	if err != nil {
		return false, false, err
	}
	for _, id := range have {
		if id == want[0] {
			return true, true, nil
		}
	}
	return false, true, nil
}

// patchIDs returns the stable patch ids of the patches in text. git
// patch-id reads only stdin.
func (r Runner) patchIDs(ctx context.Context, text string) ([]string, error) {
	out, err := r.RunInput(ctx, text, "patch-id", "--stable")
	if err != nil {
		return nil, err
	}
	var ids []string
	for _, line := range strings.Split(out, "\n") {
		if f := strings.Fields(line); len(f) > 0 {
			ids = append(ids, f[0])
		}
	}
	return ids, nil
}
