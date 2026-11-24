package ui

import (
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/dev-shetty/wrktree/internal/disk"
	"github.com/dev-shetty/wrktree/internal/git"
	"github.com/dev-shetty/wrktree/internal/testrepo"
)

// gitErr is a git error text with 4 lines and a tab.
const gitErr = "git merge --no-edit origin/master: exit status 1: error: The following untracked working tree files would be overwritten by merge:\n\tfoo\nPlease move or remove them before you merge.\nAborting"

// A git error with many lines and a tab shows as wrapped text. The view is
// not higher than the terminal, and the title stays on the screen.
func TestNoteWithManyLinesKeepsHeight(t *testing.T) {
	m := loaded(t)
	const height = 10
	m, _ = send(t, m, tea.WindowSizeMsg{Width: 120, Height: height})
	for name, msg := range map[string]tea.Msg{
		"sync":   syncedMsg{path: flowsPath, wt: m.row(flowsPath).WT, base: "origin/master", err: errors.New(gitErr)},
		"delete": removedMsg{path: flowsPath, err: errors.New(gitErr)},
		"plan":   planMsg{wt: m.row(flowsPath).WT, err: errors.New(gitErr)},
	} {
		got, _ := send(t, m, msg)
		v := got.View()
		lines := strings.Split(v, "\n")
		if len(lines) > height {
			t.Errorf("%s: the view has %d lines, the terminal has %d:\n%s", name, len(lines), height, v)
		}
		if !strings.HasPrefix(lines[0], " wrktree") {
			t.Errorf("%s: the title is not the first line:\n%s", name, v)
		}
		for _, l := range lines {
			if lipgloss.Width(l) > 120 || strings.Contains(l, "\t") {
				t.Errorf("%s: line too wide or with a tab: %q", name, l)
			}
		}
		if !strings.Contains(v, "foo Please move") || !strings.Contains(v, "Aborting") {
			t.Errorf("%s: the error text is cut:\n%s", name, v)
		}
	}
}

func TestLoadErrorWithManyLinesKeepsHeight(t *testing.T) {
	m := New(deps(t))
	m, _ = send(t, m, tea.WindowSizeMsg{Width: 120, Height: 30})
	m, _ = send(t, m, worktreesMsg{err: errors.New(gitErr)})
	v := m.View()
	if n := len(strings.Split(v, "\n")); n > 2+maxStatusLines+2 {
		t.Errorf("the view has %d lines:\n%s", n, v)
	}
	if strings.Contains(v, "\t") || !strings.Contains(v, "Aborting") {
		t.Errorf("the error text has a tab or is cut:\n%s", v)
	}
}

// The size of a worktree in the folder of a different worktree is already
// in the size of that worktree. The total counts it one time.
func TestTitleTotalNestedWorktree(t *testing.T) {
	const nested = mainPath + "/.claude/worktrees/x"
	m := New(deps(t))
	m, _ = send(t, m, tea.WindowSizeMsg{Width: 120, Height: 30})
	m, _ = send(t, m, worktreesMsg{wts: []git.Worktree{
		{Path: mainPath, Branch: "master", IsMain: true},
		{Path: nested, Branch: "x"},
		{Path: mainPath + "-2", Branch: "y"}, // the same prefix, but not in the folder
	}, base: "origin/master"})
	m, _ = send(t, m, sizeMsg{path: nested, bytes: 1 << 30})
	m, _ = send(t, m, sizeMsg{path: mainPath + "-2", bytes: 2 << 30})
	title := func() string { return strings.Split(m.View(), "\n")[0] }
	// The outer size is not known: the nested size counts.
	if want := " · " + disk.Human(3<<30); !strings.HasSuffix(title(), want) {
		t.Errorf("before the outer size: title %q, want %s", title(), want)
	}
	m, _ = send(t, m, sizeMsg{path: mainPath, bytes: 4 << 30})
	if want := " · " + disk.Human(6<<30); !strings.HasSuffix(title(), want) {
		t.Errorf("title %q, want total %s", title(), want)
	}
}

// After the delete of the current worktree, the screen can stay open for a
// different job. Its git commands then run in the main worktree.
func TestGitRunsInMainAfterCurrentDelete(t *testing.T) {
	repo := testrepo.New(t)
	feat := realPath(t, repo.AddWorktree(t, "feat-a"))
	other := realPath(t, repo.AddWorktree(t, "feat-b"))
	m, _ := repoModel(t, repo, feat, true)
	m, cmd := press(t, m, feat, "d")
	msg := run(t, cmd).(removedMsg)
	if msg.err != nil || exists(feat) {
		t.Fatalf("removedMsg = %+v", msg)
	}
	// A sync of a different row keeps the screen open.
	m.setJob(other, job{state: jobSyncing, wt: m.row(other).WT})
	m, cmd = send(t, m, msg)
	if isQuit(cmd) {
		t.Fatal("the screen quit while a sync runs")
	}
	m, _ = keys(t, m, "r")
	m, _ = send(t, m, loadCmd(m.d.Git, m.d.Cfg.Base)())
	if m.loadErr != nil {
		t.Fatalf("load after the delete of the current worktree: %v", m.loadErr)
	}
	if err := m.d.Git.FetchBase(t.Context(), m.base); err != nil {
		t.Errorf("fetch after the delete of the current worktree: %v", err)
	}
}

// d on a clean worktree with a submodule opens the typed prompt. The typed
// yes deletes with --force.
func TestDeleteSubmoduleNeedsTypedYes(t *testing.T) {
	repo := testrepo.New(t)
	repo.AddSubmodule(t)
	feat := realPath(t, repo.AddWorktree(t, "feat-sub"))
	repo.InitSubmodules(t, feat)
	m, _ := repoModel(t, repo, realPath(t, repo.Dir), true)
	m, cmd := press(t, m, feat, "d")
	if cmd != nil || !inPrompt(m) {
		t.Fatalf("d on a worktree with a submodule: cmd %v, prompt %v\n%s", cmd != nil, inPrompt(m), m.View())
	}
	if v := m.View(); !strings.Contains(v, "feat-sub has a submodule.") {
		t.Errorf("the prompt does not name the submodule:\n%s", v)
	}
	m, cmd = keys(t, m, "y", "e", "s", "enter")
	var msg removedMsg
	for _, got := range msgs(cmd) {
		if r, ok := got.(removedMsg); ok {
			msg = r
		}
	}
	if msg.path != feat || msg.err != nil || exists(feat) {
		t.Fatalf("removedMsg = %+v, folder exists %v", msg, exists(feat))
	}
}
