package ui

import (
	"fmt"
	"maps"
	"slices"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/dev-shetty/wrktree/internal/git"
	"github.com/dev-shetty/wrktree/internal/status"
)

// A delete runs in the background. Each row that d or D started has a job:
// first the plan is read (checking), then git deletes the worktree
// (deleting). On a [missing] row, d or D reads no plan: git worktree prune
// removes the stale record (deleting). The screen stays live, and several
// deletes can run at the same time.

type jobState int

const (
	jobChecking jobState = iota + 1 // the delete plan is read, or the typed prompt asks
	jobDeleting                     // git worktree remove runs, or git worktree prune for a [missing] row
	jobSyncing                      // git.Runner.Sync runs
)

// job is the delete or the sync of one row.
type job struct {
	state  jobState
	branch bool // also delete the branch: D started the delete. After the plan, only if wt has a branch.
	// These fields are set when the plan arrives.
	wt      git.Worktree
	plan    git.DeletePlan
	current bool   // the shell is in this worktree
	main    string // path of the main worktree, or ""
}

// deleteState is the delete that the typed prompt asks about.
type deleteState struct {
	wt     git.Worktree
	plan   git.DeletePlan
	branch bool // the prompt says that the branch is deleted too
}

const (
	inProgressNote = "delete in progress"
	spinEvery      = 120 * time.Millisecond
)

// spinFrames are the frames of the spinner on a row with a job.
var spinFrames = []string{"|", "/", "-", `\`}

type tickMsg struct{}

// spinTick returns the command that shows the next spinner frame. It is
// not inlined, so that each tick command has the same code. The tests
// use this to find the tick in a batch.
//
//go:noinline
func spinTick() tea.Cmd {
	return tea.Tick(spinEvery, func(time.Time) tea.Msg { return tickMsg{} })
}

// setJob sets the job of path in a new map. A copy of the Model keeps the
// old map.
func (m *Model) setJob(path string, j job) {
	jobs := maps.Clone(m.jobs)
	if jobs == nil {
		jobs = map[string]job{}
	}
	jobs[path] = j
	m.jobs = jobs
}

// dropJob removes the job of path in a new map.
func (m *Model) dropJob(path string) {
	if _, ok := m.jobs[path]; !ok {
		return
	}
	jobs := maps.Clone(m.jobs)
	delete(jobs, path)
	m.jobs = jobs
}

// deletes returns the number of delete commands that run.
func (m Model) deletes() int {
	n := 0
	for _, j := range m.jobs {
		if j.state == jobDeleting {
			n++
		}
	}
	return n
}

// syncs returns the number of syncs that run.
func (m Model) syncs() int {
	n := 0
	for _, j := range m.jobs {
		if j.state == jobSyncing {
			n++
		}
	}
	return n
}

// waitNote is the note for a quit key while deletes and syncs run.
func waitNote(deletes, syncs int) string {
	word, n := "job", deletes+syncs
	switch {
	case syncs == 0:
		word = "delete"
	case deletes == 0:
		word = "sync"
	}
	if n != 1 {
		word += "s"
	}
	return fmt.Sprintf("%d %s in progress: wait, or press ctrl+c", n, word)
}

// jobNote is the note for a key that the job j blocks on its row.
func jobNote(j job) string {
	if j.state == jobSyncing {
		return syncInProgressNote
	}
	return inProgressNote
}

// jobText is the text after the label of a row with a job, for example
// " | deleting". It is "" for a row with no job.
func (m Model) jobText(path string) string {
	j, ok := m.jobs[path]
	if !ok {
		return ""
	}
	word := "checking"
	switch j.state {
	case jobDeleting:
		word = "deleting"
	case jobSyncing:
		word = "syncing"
	}
	return " " + spinFrames[m.spin] + " " + word
}

// startSpin starts the tick if a row has a job and no tick runs.
func (m *Model) startSpin() tea.Cmd {
	if m.spinning || len(m.jobs) == 0 {
		return nil
	}
	m.spinning = true
	return spinTick()
}

// spun shows the next spinner frame. The tick stops when no row has a job.
func (m Model) spun() (tea.Model, tea.Cmd) {
	if len(m.jobs) == 0 {
		m.spinning = false
		return m, nil
	}
	m.spin = (m.spin + 1) % len(spinFrames)
	return m, spinTick()
}

// quitNow quits at once. If the shell is in a worktree that is deleted, or
// that is gone, the shell changes to the main worktree.
func (m Model) quitNow() (tea.Model, tea.Cmd) {
	if m.after.SwitchPath != "" {
		m.result = m.after
		return m, tea.Quit
	}
	for _, j := range m.jobs {
		if j.state == jobDeleting && j.current && j.main != "" {
			m.result = Result{SwitchPath: j.main}
			break
		}
	}
	return m, tea.Quit
}

// startDelete gives the cursor row a job and reads its delete plan. If
// branch is true (the D key), the delete also deletes the branch. On a
// [missing] row, it removes the stale record instead.
func (m Model) startDelete(branch bool) (tea.Model, tea.Cmd) {
	i := m.cursorIndex()
	if i < 0 {
		return m, nil
	}
	w := m.visible()[i].WT
	if j, ok := m.jobs[w.Path]; ok {
		m.note = jobNote(j)
		return m, nil
	}
	if w.Prunable {
		return m.startStale(w)
	}
	m.setJob(w.Path, job{state: jobChecking, branch: branch})
	return m, tea.Batch(planCmd(m.d.Git, w), m.startSpin())
}

// planDone starts the delete of a clean worktree, or opens the typed prompt
// for a worktree with changes or unreachable commits. The plan is for the
// row of the d key, wherever the cursor is now. A plan error, a blocked
// plan, the current worktree without a safe directory to change to, or a
// row that is gone deletes nothing.
func (m Model) planDone(msg planMsg) (tea.Model, tea.Cmd) {
	path := msg.wt.Path
	j, ok := m.jobs[path]
	switch {
	case msg.err != nil:
		m.dropJob(path)
		m.note = "cannot check " + path + ": " + msg.err.Error()
		return m, nil
	case msg.plan.Blocked != "":
		m.dropJob(path)
		m.note = msg.plan.Blocked
		return m, nil
	}
	main := m.mainPath()
	// The shell is in the worktree if the directory is in its folder.
	current := git.Contains(path, m.d.Cwd)
	if current && (!m.d.CanCD || main == "") {
		m.dropJob(path)
		m.note = currentNote
		return m, nil
	}
	if !ok || m.row(path) == nil {
		m.dropJob(path)
		m.note = cancelledNote
		return m, nil
	}
	j.wt, j.plan, j.current, j.main = msg.wt, msg.plan, current, main
	j.branch = j.branch && msg.wt.Branch != ""
	m.setJob(path, j)
	if !msg.plan.NeedsYes {
		// A clean plan deletes at once. The delete does not force, so git
		// refuses it if files changed after the plan.
		return m.runDelete(path, false)
	}
	// The prompt opens only in the list. It does not take over the filter
	// input or another prompt.
	if m.mode != modeList {
		m.dropJob(path)
		m.note = cancelledNote
		return m, nil
	}
	m.cursor = path
	if m.cursorPath() != path {
		m.filter = "" // the filter hides the row
	}
	m.del = deleteState{wt: msg.wt, plan: msg.plan, branch: j.branch}
	m.typed = ""
	m.mode = modeTyped
	return m, nil
}

// typedKey handles the typed prompt: only the exact text "yes" and enter
// delete. Any other text and enter, or esc, cancels.
func (m Model) typedKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyCtrlC:
		return m.quitNow()
	case tea.KeyEsc:
		return m.cancel()
	case tea.KeyEnter:
		if m.typed != "yes" {
			return m.cancel()
		}
		path := m.del.wt.Path
		m.mode, m.typed, m.del = modeList, "", deleteState{}
		if _, ok := m.jobs[path]; !ok {
			m.note = cancelledNote
			return m, nil
		}
		// Only the typed yes forces the delete.
		return m.runDelete(path, true)
	case tea.KeyBackspace:
		if r := []rune(m.typed); len(r) > 0 {
			m.typed = string(r[:len(r)-1])
		}
	case tea.KeySpace:
		m.typed += " "
	case tea.KeyRunes:
		m.typed += string(msg.Runes)
	}
	return m, nil
}

// cancel closes the typed prompt. The row of the prompt loses its job.
func (m Model) cancel() (tea.Model, tea.Cmd) {
	m.dropJob(m.del.wt.Path)
	m.mode, m.typed = modeList, ""
	m.del = deleteState{}
	m.note = cancelledNote
	return m, nil
}

// runDelete starts the delete of the job of path in the background. The
// git commands run in the main worktree, because the current worktree can
// be the one that goes.
func (m Model) runDelete(path string, force bool) (tea.Model, tea.Cmd) {
	j := m.jobs[path]
	j.state = jobDeleting
	m.setJob(path, j)
	r := m.d.Git
	if j.main != "" {
		r = r.In(j.main)
	}
	return m, tea.Batch(removeCmd(r, j.wt, force, j.branch), m.startSpin())
}

// removed ends the job of a delete. After a good delete, it removes the
// row and its cache entry. After the delete of the current worktree, the
// shell changes to the main worktree: the screen quits when no other
// delete runs.
func (m Model) removed(msg removedMsg) (tea.Model, tea.Cmd) {
	j, ok := m.jobs[msg.path]
	m.dropJob(msg.path)
	if msg.err != nil {
		m.note = "delete failed: " + msg.err.Error()
		return m.finish()
	}
	if m.mode == modeTyped && m.del.wt.Path == msg.path {
		m.mode, m.typed, m.del = modeList, "", deleteState{}
	}
	rows := make([]status.Row, 0, len(m.rows))
	for _, r := range m.rows {
		if r.WT.Path != msg.path {
			rows = append(rows, r)
		}
	}
	m.rows = rows
	var wait []string
	for _, p := range m.sizeWait {
		if p != msg.path {
			wait = append(wait, p)
		}
	}
	m.sizeWait = wait
	m.d.Cache.Delete(m.d.Repo, msg.path)
	_ = m.d.Cache.Save()
	m.fixCursor()

	name := msg.path
	if ok {
		name = wtLabel(j.wt)
	}
	m.note = "deleted " + name
	switch {
	case msg.branchErr != nil:
		m.note += ", but the branch stays: " + msg.branchErr.Error()
	case ok && j.branch:
		m.note += " and its branch"
	}
	if ok && j.current {
		// The directory of the git runner is gone. The next commands run in
		// the main worktree.
		m.d.Git = m.d.Git.In(j.main)
		m.after = Result{SwitchPath: j.main}
		if msg.branchErr != nil {
			m.after.Message = m.note
		}
	}
	return m.finish()
}

// finish quits with m.after when the current worktree is gone and no other
// delete or sync runs.
func (m Model) finish() (tea.Model, tea.Cmd) {
	if m.after.SwitchPath != "" && m.deletes() == 0 && m.syncs() == 0 {
		m.result = m.after
		return m, tea.Quit
	}
	return m, nil
}

// startStale gives the [missing] row of w a deleting job and removes the
// stale records. No plan and no prompt: git worktree prune deletes no folder
// and no branch. D does the same as d. The command runs in the main
// worktree, or in the repository directory if no row is main.
func (m Model) startStale(w git.Worktree) (tea.Model, tea.Cmd) {
	m.setJob(w.Path, job{state: jobDeleting, wt: w})
	r := m.d.Git
	if main := m.mainPath(); main != "" {
		r = r.In(main)
	}
	return m, tea.Batch(staleCmd(r, w.Path), m.startSpin())
}

// staleRemoved ends the job of a stale record removal. After an error, the
// row stays. Else the list loads again, and the reload counts the [missing]
// rows that went. If a reload removed the row first, the record is gone and
// nothing more happens.
func (m Model) staleRemoved(msg staleMsg) (tea.Model, tea.Cmd) {
	_, ok := m.jobs[msg.path]
	m.dropJob(msg.path)
	if msg.err != nil {
		m.note = "cannot remove the stale record: " + msg.err.Error()
		return m.finish()
	}
	if !ok {
		return m.finish()
	}
	m.stale = []string{}
	for _, r := range m.rows {
		if r.WT.Prunable {
			m.stale = append(m.stale, r.WT.Path)
		}
	}
	if next, quit := m.finish(); quit != nil {
		return next, quit
	}
	return m, loadCmd(m.d.Git, m.d.Cfg.Base)
}

// staleNote counts the paths of stale that are not [missing] rows of wts.
func staleNote(stale []string, wts []git.Worktree) string {
	n := 0
	for _, p := range stale {
		if !slices.ContainsFunc(wts, func(w git.Worktree) bool { return w.Path == p && w.Prunable }) {
			n++
		}
	}
	if n == 1 {
		return "removed 1 stale record"
	}
	return fmt.Sprintf("removed %d stale records", n)
}

// wtLabel returns the branch of w, or the first 7 characters of its head if
// it is detached.
func wtLabel(w git.Worktree) string {
	if w.Branch != "" {
		return w.Branch
	}
	if len(w.Head) > 7 {
		return w.Head[:7]
	}
	return w.Head
}
