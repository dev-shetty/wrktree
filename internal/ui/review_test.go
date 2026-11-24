package ui

import (
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
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
