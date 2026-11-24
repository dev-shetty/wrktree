package ui

import (
	"context"
	"errors"
	"fmt"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/dev-shetty/wrktree/internal/git"
)

// A sync runs in the background, as a delete does. The s key gives the
// cursor row a syncing job. git.Runner.Sync merges the base branch into the
// branch of the row. The screen stays live.

const syncInProgressNote = "sync in progress"

// syncFetchNote follows the note of a sync that used the local base.
const syncFetchNote = " (fetch failed: used the local copy)"

type syncedMsg struct {
	path string
	wt   git.Worktree
	base string
	res  git.SyncResult
	err  error
}

func syncCmd(r git.Runner, wt git.Worktree, base string) tea.Cmd {
	return func() tea.Msg {
		res, err := r.Sync(context.Background(), wt, base)
		return syncedMsg{path: wt.Path, wt: wt, base: base, res: res, err: err}
	}
}

// startSync gives the cursor row a sync job and starts the sync.
func (m Model) startSync() (tea.Model, tea.Cmd) {
	i := m.cursorIndex()
	if m.busy != "" || i < 0 {
		return m, nil
	}
	w := m.visible()[i].WT
	if j, ok := m.jobs[w.Path]; ok {
		m.note = jobNote(j)
		return m, nil
	}
	m.setJob(w.Path, job{state: jobSyncing, wt: w})
	return m, tea.Batch(syncCmd(m.d.Git, w, m.base), m.startSpin())
}

// synced ends the job of a sync and shows the result. After a sync that
// changed the branch, it reads the values of the row again.
func (m Model) synced(msg syncedMsg) (tea.Model, tea.Cmd) {
	m.dropJob(msg.path)
	label := wtLabel(msg.wt)
	switch {
	case errors.Is(msg.err, git.ErrSyncRefused):
		m.note = msg.err.Error()
	case msg.err != nil:
		m.note = "sync failed: " + msg.err.Error()
	case len(msg.res.Conflicts) > 0:
		m.note = "sync failed: conflict in " + git.ConflictText(msg.res.Conflicts)
	case msg.res.UpToDate:
		m.note = fmt.Sprintf("%s is already up to date with %s", label, msg.base)
	default:
		m.note = fmt.Sprintf("synced %s with %s", label, msg.base)
	}
	if msg.err == nil && len(msg.res.Conflicts) == 0 && msg.res.FetchFailed {
		m.note += syncFetchNote
	}
	changed := msg.err == nil && len(msg.res.Conflicts) == 0 && !msg.res.UpToDate
	if !changed || m.row(msg.path) == nil {
		return m.finish()
	}
	cmds := m.refreshRow(msg.path)
	next, quit := m.finish()
	return next, tea.Batch(append(cmds, quit)...)
}

// refreshRow starts the value commands of the row at path again. The size
// runs du again.
func (m *Model) refreshRow(path string) []tea.Cmd {
	r := m.row(path)
	m.changedWait[path] = true
	m.commitWait[path] = true
	cmds := []tea.Cmd{changedCmd(m.d.Git, path), commitCmd(m.d.Git, path)}
	if m.base != "" {
		m.distWait[path] = true
		cmds = append(cmds, distCmd(m.d.Git, path, m.base))
		if !r.WT.IsMain {
			m.mergedWait[path] = true
			cmds = append(cmds, mergedCmd(m.d.Git, path, m.base))
		}
	}
	m.queueSize(path)
	return append(cmds, m.startSizes()...)
}
