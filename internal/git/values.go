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

// MergeState tells how the work of a branch relates to the base branch.
type MergeState int

const (
	// MergeUnknown means that the state is not known: too many base commits
	// to read, or an error.
	MergeUnknown MergeState = iota
	// MergeNone means that the branch has work that the base does not
	// contain, or that HEAD is the base tip.
	MergeNone
	// MergeNoCommits means that the branch has no commits of its own.
	MergeNoCommits
	// MergeMerged means that the base contains the work of the branch.
	MergeMerged
)

// Merged tells if the base branch already contains the work of the
// worktree at path.
//
// If HEAD is an ancestor of base, Merged finds c, the oldest commit on the
// first-parent line of base that HEAD does not contain. If the first parent
// of c is HEAD, HEAD is an old position of base: MergeNoCommits. A
// fast-forward merge gives the same result, because git cannot tell it
// apart. Otherwise HEAD came into base through a merge commit: MergeMerged.
//
// If HEAD is not an ancestor, the branch is merged if it adds no change to
// the merge base, or if a commit on base since the merge base has the same
// patch id as the whole change of HEAD. The last test finds a squash merge.
// Merged runs only commands that read, and it writes no objects.
func (r Runner) Merged(ctx context.Context, path, base string) (MergeState, error) {
	g := r.In(path)
	head, err := g.Run(ctx, "rev-parse", "HEAD")
	if err != nil {
		return MergeUnknown, err
	}
	tip, err := g.Run(ctx, "rev-parse", base)
	if err != nil {
		return MergeUnknown, err
	}
	if head == tip {
		return MergeNone, nil
	}
	mb, err := g.Run(ctx, "merge-base", base, "HEAD")
	if err != nil {
		return MergeUnknown, err
	}
	if mb == head {
		return g.ancestorState(ctx, head, base)
	}
	diff, err := g.Run(ctx, append(append([]string{"diff"}, diffFlags...), mb, "HEAD")...)
	if err != nil {
		return MergeUnknown, err
	}
	if strings.TrimSpace(diff) == "" {
		return MergeMerged, nil
	}
	out, err := g.Run(ctx, "rev-list", "--count", "--no-merges", mb+".."+base)
	if err != nil {
		return MergeUnknown, err
	}
	n, err := strconv.Atoi(strings.TrimSpace(out))
	if err != nil {
		return MergeUnknown, err
	}
	if n > mergedScanLimit {
		return MergeUnknown, nil
	}
	if n == 0 {
		return MergeNone, nil
	}
	want, err := g.patchIDs(ctx, diff+"\n")
	if err != nil || len(want) == 0 {
		return MergeUnknown, err
	}
	logArgs := append(append([]string{"log", "-p", "--no-merges", "--format=commit %H"}, diffFlags...), mb+".."+base)
	log, err := g.Run(ctx, logArgs...)
	if err != nil {
		return MergeUnknown, err
	}
	have, err := g.patchIDs(ctx, log+"\n")
	if err != nil {
		return MergeUnknown, err
	}
	for _, id := range have {
		if id == want[0] {
			return MergeMerged, nil
		}
	}
	return MergeNone, nil
}

// ancestorState is the state of a HEAD that is an ancestor of base and is
// not the base tip.
func (r Runner) ancestorState(ctx context.Context, head, base string) (MergeState, error) {
	out, err := r.Run(ctx, "rev-list", "--first-parent", "--reverse", "HEAD.."+base)
	if err != nil {
		return MergeUnknown, err
	}
	c, _, _ := strings.Cut(out, "\n")
	if c = strings.TrimSpace(c); c == "" {
		return MergeNoCommits, nil
	}
	parent, err := r.Run(ctx, "rev-parse", "--verify", "--quiet", c+"^1")
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
		// c has no parent.
		return MergeNoCommits, nil
	}
	if err != nil {
		return MergeUnknown, err
	}
	if parent == head {
		return MergeNoCommits, nil
	}
	return MergeMerged, nil
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
