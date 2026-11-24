package ui

import (
	"io"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// loopModel starts a batch of slow commands and records when a key arrives.
type loopModel struct {
	slow  time.Duration
	keyAt chan time.Time
}

func (m loopModel) Init() tea.Cmd {
	slow := func() tea.Msg { time.Sleep(m.slow); return nil }
	return tea.Batch(slow, slow, slow)
}

func (m loopModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if _, ok := msg.(tea.KeyMsg); ok {
		m.keyAt <- time.Now()
		return m, tea.Quit
	}
	return m, nil
}

func (m loopModel) View() string { return "" }

// TestBatchDoesNotBlockKeys guards against a Bubble Tea version that runs
// batched commands on the event loop (v1.3.7 does). With that defect, the
// screen ignores all keys until every git and du command finishes.
func TestBatchDoesNotBlockKeys(t *testing.T) {
	const slow = 400 * time.Millisecond
	m := loopModel{slow: slow, keyAt: make(chan time.Time, 1)}
	pr, pw := io.Pipe()
	defer pw.Close()
	p := tea.NewProgram(m, tea.WithInput(pr), tea.WithOutput(io.Discard), tea.WithoutSignalHandler())
	done := make(chan error, 1)
	go func() { _, err := p.Run(); done <- err }()

	time.Sleep(50 * time.Millisecond) // the batch is now in progress
	sent := time.Now()
	p.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})

	select {
	case at := <-m.keyAt:
		if d := at.Sub(sent); d > slow/2 {
			t.Fatalf("key handled after %v: batched commands block the event loop", d)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("key not handled")
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("program did not quit")
	}
}

// TestTitleKeepsRightMargin makes sure that the total size does not touch the
// last columns, where a terminal scroll bar can hide it.
func TestTitleKeepsRightMargin(t *testing.T) {
	for _, w := range []int{120, 80, 61} {
		tm, _ := loaded(t).Update(tea.WindowSizeMsg{Width: w, Height: 30})
		title := lineWith(t, tm.(Model).View(), "worktrees")
		if got := lipgloss.Width(title); got > w-rightMargin {
			t.Errorf("width %d: title is %d columns wide, limit is %d: %q", w, got, w-rightMargin, title)
		}
		if !strings.HasSuffix(title, "GB") && !strings.HasSuffix(title, "MB") && !strings.HasSuffix(title, "KB") {
			t.Errorf("width %d: title does not end with the total size: %q", w, title)
		}
	}
}
