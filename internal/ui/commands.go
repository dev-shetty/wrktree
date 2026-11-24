package ui

import (
	"context"
	"errors"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/dev-shetty/wrktree/internal/disk"
	"github.com/dev-shetty/wrktree/internal/git"
)

// fetchTimeout stops a fetch that takes too long.
const fetchTimeout = 15 * time.Second

// sizeLimit is the number of size commands that run at one time.
const sizeLimit = 4

type worktreesMsg struct {
	wts  []git.Worktree
	base string
	err  error
}

type changedMsg struct {
	path string
	n    int
	err  error
}

type distMsg struct {
	path          string
	ahead, behind int
	err           error
}

type mergedMsg struct {
	path  string
	state git.MergeState
	err   error
}

type commitMsg struct {
	path string
	t    time.Time
	err  error
}

type sizeMsg struct {
	path  string
	bytes int64
	err   error
}

type fetchMsg struct{ err error }

// loadCmd reads the worktree list and the base branch. No base is not an error.
func loadCmd(r git.Runner, baseOverride string) tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()
		wts, err := r.Worktrees(ctx)
		if err != nil {
			return worktreesMsg{err: err}
		}
		base, err := r.BaseBranch(ctx, baseOverride)
		if err != nil && !errors.Is(err, git.ErrNoBase) {
			return worktreesMsg{err: err}
		}
		return worktreesMsg{wts: wts, base: base}
	}
}

func changedCmd(r git.Runner, path string) tea.Cmd {
	return func() tea.Msg {
		n, err := r.ChangedFiles(context.Background(), path)
		return changedMsg{path: path, n: n, err: err}
	}
}

func distCmd(r git.Runner, path, base string) tea.Cmd {
	return func() tea.Msg {
		a, b, err := r.Distance(context.Background(), path, base)
		return distMsg{path: path, ahead: a, behind: b, err: err}
	}
}

func mergedCmd(r git.Runner, path, base string) tea.Cmd {
	return func() tea.Msg {
		st, err := r.Merged(context.Background(), path, base)
		return mergedMsg{path: path, state: st, err: err}
	}
}

func commitCmd(r git.Runner, path string) tea.Cmd {
	return func() tea.Msg {
		t, err := r.LastCommit(context.Background(), path)
		return commitMsg{path: path, t: t, err: err}
	}
}

func sizeCmd(path string) tea.Cmd {
	return func() tea.Msg {
		n, err := disk.Size(context.Background(), path)
		return sizeMsg{path: path, bytes: n, err: err}
	}
}

func fetchCmd(r git.Runner, base string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), fetchTimeout)
		defer cancel()
		return fetchMsg{err: r.FetchBase(ctx, base)}
	}
}

type planMsg struct {
	wt   git.Worktree
	plan git.DeletePlan
	err  error
}

type removedMsg struct {
	path           string
	err, branchErr error
}

type pruneListMsg struct {
	lines []string
	err   error
}

type prunedMsg struct {
	n   int
	err error
}

func planCmd(r git.Runner, wt git.Worktree) tea.Cmd {
	return func() tea.Msg {
		p, err := r.PlanDelete(context.Background(), wt)
		return planMsg{wt: wt, plan: p, err: err}
	}
}

// removeCmd deletes the worktree, then the branch if branch is true. A
// branch error does not undo the delete.
func removeCmd(r git.Runner, wt git.Worktree, force, branch bool) tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()
		if err := r.Remove(ctx, wt.Path, force); err != nil {
			return removedMsg{path: wt.Path, err: err}
		}
		var berr error
		if branch && wt.Branch != "" {
			berr = r.DeleteBranch(ctx, wt.Branch)
		}
		return removedMsg{path: wt.Path, branchErr: berr}
	}
}

func pruneListCmd(r git.Runner) tea.Cmd {
	return func() tea.Msg {
		lines, err := r.PruneDryRun(context.Background())
		return pruneListMsg{lines: lines, err: err}
	}
}

// pruneCmd prunes the stale entries. n is the count that the prompt showed.
func pruneCmd(r git.Runner, n int) tea.Cmd {
	return func() tea.Msg {
		return prunedMsg{n: n, err: r.Prune(context.Background())}
	}
}

type copiedMsg struct {
	text string
	err  error
}

// copyCmd puts text on the clipboard with put, away from the event loop.
func copyCmd(put func(string) error, text string) tea.Cmd {
	return func() tea.Msg { return copiedMsg{text: text, err: put(text)} }
}
