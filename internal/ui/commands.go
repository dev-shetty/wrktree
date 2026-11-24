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
