package git

import (
	"bytes"
	"context"
	"fmt"
	"strconv"
	"strings"
)

// DeletePlan says whether a worktree can be deleted and what the delete loses.
type DeletePlan struct {
	Blocked     string // reason. Empty = delete is possible.
	Changed     int
	Unreachable int  // commits of a detached HEAD that no branch, remote ref, or tag contains
	NeedsYes    bool // Changed > 0 || Unreachable > 0
}

// PlanDelete checks a worktree. The order of checks is: main, locked, missing
// folder, other worktrees inside its folder, then the counts of changes. It
// returns an error if it cannot read the state of an existing worktree. It
// never treats an unreadable worktree as clean.
func (r Runner) PlanDelete(ctx context.Context, wt Worktree) (DeletePlan, error) {
	switch {
	case wt.IsMain:
		return DeletePlan{Blocked: "the main worktree cannot be deleted"}, nil
	case wt.Locked && wt.LockReason != "":
		return DeletePlan{Blocked: "worktree is locked: " + wt.LockReason}, nil
	case wt.Locked:
		return DeletePlan{Blocked: "worktree is locked"}, nil
	case wt.Prunable:
		return DeletePlan{Blocked: "folder is missing: use prune"}, nil
	}
	// "git worktree remove" deletes a nested worktree with the folder, and
	// the status of the folder does not show it.
	all, err := r.Worktrees(ctx)
	if err != nil {
		return DeletePlan{}, err
	}
	for _, o := range all {
		if resolve(o.Path) != resolve(wt.Path) && Contains(wt.Path, o.Path) {
			return DeletePlan{Blocked: "contains worktree " + label(o) + ": delete it first"}, nil
		}
	}
	var p DeletePlan
	if p.Changed, err = r.ChangedFiles(ctx, wt.Path); err != nil {
		return DeletePlan{}, err
	}
	if wt.Detached {
		if p.Unreachable, err = r.UnreachableCommits(ctx, wt.Path); err != nil {
			return DeletePlan{}, err
		}
	}
	p.NeedsYes = p.Changed > 0 || p.Unreachable > 0
	return p, nil
}

// UnreachableCommits counts the commits of HEAD in the worktree at path that
// no branch, remote ref, or tag contains.
func (r Runner) UnreachableCommits(ctx context.Context, path string) (int, error) {
	out, err := r.In(path).Run(ctx, "rev-list", "--count", "HEAD", "--not", "--branches", "--remotes", "--tags")
	if err != nil {
		return 0, err
	}
	n, err := strconv.Atoi(strings.TrimSpace(out))
	if err != nil {
		return 0, fmt.Errorf("unexpected rev-list output %q: %w", out, err)
	}
	return n, nil
}

// Remove runs "git worktree remove". It does no safety check: call PlanDelete first.
func (r Runner) Remove(ctx context.Context, path string, force bool) error {
	args := []string{"worktree", "remove"}
	if force {
		args = append(args, "--force")
	}
	_, err := r.Run(ctx, append(args, path)...)
	return err
}

// DeleteBranch runs "git branch -d". Git refuses an unmerged branch.
func (r Runner) DeleteBranch(ctx context.Context, branch string) error {
	_, err := r.Run(ctx, "branch", "-d", branch)
	return err
}

// PruneDryRun returns one line for each stale worktree entry. Git prints these
// lines on stderr.
func (r Runner) PruneDryRun(ctx context.Context) ([]string, error) {
	cmd := r.command(ctx, "worktree", "prune", "--dry-run", "-v")
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("git worktree prune --dry-run -v: %w: %s", err, strings.TrimSpace(out.String()))
	}
	lines := []string{}
	for _, l := range strings.Split(out.String(), "\n") {
		if strings.TrimSpace(l) != "" {
			lines = append(lines, l)
		}
	}
	return lines, nil
}

// Prune runs "git worktree prune". It does no safety check.
func (r Runner) Prune(ctx context.Context) error {
	_, err := r.Run(ctx, "worktree", "prune")
	return err
}

// DeletePrompt returns the text that asks the user to confirm the delete of
// wt. It does not contain the end part: the caller adds "[y/N] " when
// plan.NeedsYes is false, or "Type yes to delete: " when it is true.
func DeletePrompt(wt Worktree, p DeletePlan) string {
	name := label(wt)
	if !p.NeedsYes {
		return fmt.Sprintf("Delete worktree %s at %s? Ignored files are deleted too. ", name, wt.Path)
	}
	var lost []string
	if p.Changed > 0 {
		lost = append(lost, plural(p.Changed, "changed file", "changed files"))
	}
	if p.Unreachable > 0 {
		lost = append(lost, plural(p.Unreachable, "commit", "commits"))
	}
	text := name + " has " + strings.Join(lost, " and ")
	if p.Unreachable > 0 {
		text += " that no branch contains"
	}
	return text + ". They will be lost. Ignored files are deleted too. "
}

// label returns the branch of wt, or the first 7 characters of its head if
// it is detached.
func label(wt Worktree) string {
	if wt.Branch != "" {
		return wt.Branch
	}
	if len(wt.Head) > 7 {
		return wt.Head[:7]
	}
	return wt.Head
}

func plural(n int, one, many string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, one)
	}
	return fmt.Sprintf("%d %s", n, many)
}
