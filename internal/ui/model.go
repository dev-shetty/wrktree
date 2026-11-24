// Package ui is the interactive screen of wrktree.
package ui

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/dev-shetty/wrktree/internal/config"
	"github.com/dev-shetty/wrktree/internal/disk"
	"github.com/dev-shetty/wrktree/internal/git"
	"github.com/dev-shetty/wrktree/internal/status"
)

// Deps holds what the screen takes from the outside. Cache must not be nil.
type Deps struct {
	Git   git.Runner
	Cfg   config.Config
	Cache *disk.Cache
	Repo  string // cache key and title
	Name  string // RepoName
	Cwd   string
	CanCD bool // WRKTREE_CD_FILE is set
	Now   func() time.Time
	Copy  func(text string) error // puts text on the system clipboard
}

// Result tells the caller what to do after the screen quits. At most one of
// SwitchPath and OpenPath is set. Message is text for stderr, or "".
type Result struct{ SwitchPath, OpenPath, Message string }

type mode int

const (
	modeList mode = iota
	modeFilter
	modeTyped // delete prompt of a worktree with changes: yes + enter deletes
	modePrune // prune prompt: y prunes
)

const (
	missingNote   = "folder is missing: use prune"
	currentNote   = "cannot delete the current worktree: run through the wt function"
	cancelledNote = "cancelled"
)

// Model is the state of the screen.
type Model struct {
	d Deps

	loaded  bool
	loadErr error
	base    string
	rows    []status.Row // sorted with status.Sort

	// The wait maps hold the paths whose command runs now.
	changedWait, distWait, mergedWait, commitWait map[string]bool
	sizeBusy                                      map[string]bool
	sizeWait                                      []string // size commands that wait for a free slot

	fresh         bool // the next load runs du for every row
	fetchedOnLoad bool
	fetching      bool
	fetchFailed   bool
	cursor        string // path of the cursor row
	mode          mode
	filter        string
	note          string // footer text until the next key
	width, height int
	result        Result

	// jobs holds the delete job of each row that has one, by path. Change it
	// only with setJob and dropJob: a copy of the Model keeps its own map.
	jobs     map[string]job
	spin     int    // frame of the spinner
	spinning bool   // a tick command runs
	after    Result // the result after the delete of the current worktree; quit when no delete runs

	del        deleteState // the delete that the typed prompt asks about
	typed      string      // text of the typed delete prompt
	pruneLines []string    // stale entries that the prune prompt shows
	busy       string      // text of the prune step that runs; "" = none
	writing    bool        // the step that runs changes the repository
}

// New returns a screen that loads its rows when it starts.
func New(d Deps) Model {
	if d.Now == nil {
		d.Now = time.Now
	}
	if d.Copy == nil {
		d.Copy = func(string) error { return errors.New("no clipboard") }
	}
	return Model{
		d:           d,
		changedWait: map[string]bool{},
		distWait:    map[string]bool{},
		mergedWait:  map[string]bool{},
		commitWait:  map[string]bool{},
		sizeBusy:    map[string]bool{},
	}
}

// Init loads the worktree list and the base branch.
func (m Model) Init() tea.Cmd { return loadCmd(m.d.Git, m.d.Cfg.Base) }

// Result returns the action that the user selected.
func (m Model) Result() Result { return m.result }

// Update handles one message.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case tea.KeyMsg:
		m.note = ""
		switch m.mode {
		case modeFilter:
			return m.filterKey(msg)
		case modeTyped:
			return m.typedKey(msg)
		case modePrune:
			return m.pruneKey(msg)
		}
		return m.listKey(msg)
	case planMsg:
		return m.planDone(msg)
	case removedMsg:
		return m.removed(msg)
	case tickMsg:
		return m.spun()
	case pruneListMsg:
		return m.pruneListed(msg)
	case prunedMsg:
		m.busy, m.writing = "", false
		if msg.err != nil {
			m.note = "prune failed: " + msg.err.Error()
			return m, nil
		}
		m.note = fmt.Sprintf("pruned %d entries", msg.n)
		return m, loadCmd(m.d.Git, m.d.Cfg.Base)
	case worktreesMsg:
		return m.setWorktrees(msg)
	case changedMsg:
		delete(m.changedWait, msg.path)
		if r := m.row(msg.path); r != nil {
			r.Changed, r.ChangedOK = msg.n, msg.err == nil
		}
	case distMsg:
		delete(m.distWait, msg.path)
		if r := m.row(msg.path); r != nil && m.base != "" {
			r.Ahead, r.Behind, r.DistOK = msg.ahead, msg.behind, msg.err == nil
		}
	case mergedMsg:
		delete(m.mergedWait, msg.path)
		if r := m.row(msg.path); r != nil && m.base != "" && !r.WT.IsMain {
			r.Merge = msg.state
			if msg.err != nil {
				r.Merge = git.MergeUnknown
			}
		}
	case commitMsg:
		delete(m.commitWait, msg.path)
		if r := m.row(msg.path); r != nil {
			r.LastCommit, r.CommitOK = msg.t, msg.err == nil
			status.Sort(m.rows)
		}
	case sizeMsg:
		return m.sizeDone(msg)
	case copiedMsg:
		if msg.err != nil {
			m.note = "cannot copy: " + msg.err.Error()
		} else {
			m.note = "copied " + msg.text
		}
	case fetchMsg:
		m.fetching = false
		m.fetchFailed = msg.err != nil
		if m.fetchFailed {
			return m, nil
		}
		return m, m.startDistances()
	}
	return m, nil
}

func (m *Model) row(path string) *status.Row {
	for i := range m.rows {
		if m.rows[i].WT.Path == path {
			return &m.rows[i]
		}
	}
	return nil
}

// setWorktrees makes the rows and starts one command for each value. A row
// that existed before keeps its values until the new ones arrive.
func (m Model) setWorktrees(msg worktreesMsg) (tea.Model, tea.Cmd) {
	m.loaded = true
	m.loadErr = msg.err
	if msg.err != nil {
		return m, nil
	}
	m.base = msg.base
	old := make(map[string]status.Row, len(m.rows))
	for _, r := range m.rows {
		old[r.WT.Path] = r
	}
	fresh := m.fresh
	m.fresh = false
	now := m.d.Now()
	cur := git.Current(msg.wts, m.d.Cwd)
	m.changedWait, m.distWait, m.mergedWait, m.commitWait = map[string]bool{}, map[string]bool{}, map[string]bool{}, map[string]bool{}

	var keep []string
	for _, p := range m.sizeWait {
		if containsPath(msg.wts, p) {
			keep = append(keep, p)
		}
	}
	m.sizeWait = keep

	var cmds []tea.Cmd
	m.rows = make([]status.Row, 0, len(msg.wts))
	paths := make([]string, 0, len(msg.wts))
	for i, w := range msg.wts {
		paths = append(paths, w.Path)
		r := old[w.Path]
		r.WT, r.Current = w, i == cur
		if w.Prunable {
			m.rows = append(m.rows, status.Row{WT: w, Current: r.Current})
			continue
		}
		if m.base == "" {
			r.DistOK, r.Merge = false, git.MergeUnknown
		}
		e, ok := m.d.Cache.Get(m.d.Repo, w.Path)
		bytes, state, runDu := disk.Decide(e, ok, m.d.Cfg.SizeTTL, now, fresh)
		r.Size, r.SizeState = bytes, state
		if runDu {
			m.queueSize(w.Path)
		}
		m.changedWait[w.Path] = true
		m.commitWait[w.Path] = true
		cmds = append(cmds, changedCmd(m.d.Git, w.Path), commitCmd(m.d.Git, w.Path))
		m.rows = append(m.rows, r)
	}
	status.Sort(m.rows)
	// On the first load, the cursor starts on the current worktree.
	if m.cursor == "" && cur >= 0 {
		m.cursor = msg.wts[cur].Path
	}
	// A job lives as long as its row.
	for p := range m.jobs {
		if !containsPath(msg.wts, p) {
			m.dropJob(p)
		}
	}
	m.d.Cache.Retain(m.d.Repo, paths)
	_ = m.d.Cache.Save()
	m.fixCursor()

	cmds = append(cmds, m.startDistances())
	cmds = append(cmds, m.startSizes()...)
	if m.d.Cfg.Fetch && m.base != "" && !m.fetchedOnLoad {
		m.fetchedOnLoad = true
		m.fetching = true
		cmds = append(cmds, fetchCmd(m.d.Git, m.base))
	}
	return m, tea.Batch(cmds...)
}

func containsPath(wts []git.Worktree, path string) bool {
	for _, w := range wts {
		if w.Path == path {
			return true
		}
	}
	return false
}

// startDistances starts a distance command for each row with a folder, and
// a merged command for each of these rows that is not the main worktree.
// Both answers depend on the base.
func (m *Model) startDistances() tea.Cmd {
	if m.base == "" {
		return nil
	}
	var cmds []tea.Cmd
	for _, r := range m.rows {
		if r.WT.Prunable {
			continue
		}
		m.distWait[r.WT.Path] = true
		cmds = append(cmds, distCmd(m.d.Git, r.WT.Path, m.base))
		if !r.WT.IsMain {
			m.mergedWait[r.WT.Path] = true
			cmds = append(cmds, mergedCmd(m.d.Git, r.WT.Path, m.base))
		}
	}
	return tea.Batch(cmds...)
}

func (m *Model) queueSize(path string) {
	if m.sizeBusy[path] {
		return
	}
	for _, p := range m.sizeWait {
		if p == path {
			return
		}
	}
	m.sizeWait = append(m.sizeWait, path)
}

// startSizes starts waiting size commands until sizeLimit of them run.
func (m *Model) startSizes() []tea.Cmd {
	var cmds []tea.Cmd
	for len(m.sizeBusy) < sizeLimit && len(m.sizeWait) > 0 {
		p := m.sizeWait[0]
		m.sizeWait = m.sizeWait[1:]
		m.sizeBusy[p] = true
		cmds = append(cmds, sizeCmd(p))
	}
	return cmds
}

// sizeDone shows a new size, writes it to the cache, and starts the next
// waiting size command. A failed size keeps the value that shows.
func (m Model) sizeDone(msg sizeMsg) (tea.Model, tea.Cmd) {
	delete(m.sizeBusy, msg.path)
	if r := m.row(msg.path); r != nil && msg.err == nil {
		r.Size, r.SizeState = msg.bytes, disk.SizeFresh
		m.d.Cache.Set(m.d.Repo, msg.path, msg.bytes, m.d.Now())
		_ = m.d.Cache.Save()
	}
	return m, tea.Batch(m.startSizes()...)
}

func (m Model) listKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.busy != "" {
		return m.busyKey(msg)
	}
	switch k := msg.String(); k {
	case "up", "left", "k":
		m.move(-1)
	case "down", "right", "j":
		m.move(1)
	case "enter", "o":
		// A quit during a delete can leave the shell in a deleted directory.
		if n := m.deletes(); n > 0 {
			m.note = waitNote(n)
			return m, nil
		}
		if _, ok := m.jobs[m.cursorPath()]; ok {
			m.note = inProgressNote
			return m, nil
		}
		return m.choose(k == "o")
	case "c":
		if i := m.cursorIndex(); i >= 0 {
			return m, copyCmd(m.d.Copy, wtLabel(m.visible()[i].WT))
		}
	case "f":
		if m.base != "" && !m.fetching {
			m.fetching, m.fetchFailed = true, false
			return m, fetchCmd(m.d.Git, m.base)
		}
	case "r":
		m.fresh = true
		return m, loadCmd(m.d.Git, m.d.Cfg.Base)
	case "d":
		return m.startDelete(false)
	case "D":
		return m.startDelete(true)
	case "p":
		if len(m.jobs) > 0 {
			m.note = inProgressNote
			return m, nil
		}
		m.busy = "reading stale entries"
		return m, pruneListCmd(m.d.Git)
	case "/":
		m.mode = modeFilter
	case "q", "esc":
		if n := m.deletes(); n > 0 {
			m.note = waitNote(n)
			return m, nil
		}
		return m, tea.Quit
	case "ctrl+c":
		return m.quitNow()
	}
	return m, nil
}

func (m Model) filterKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyCtrlC:
		return m.quitNow()
	case tea.KeyEsc:
		m.filter, m.mode = "", modeList
	case tea.KeyEnter:
		m.mode = modeList
	case tea.KeyBackspace:
		if r := []rune(m.filter); len(r) > 0 {
			m.filter = string(r[:len(r)-1])
		}
	case tea.KeyUp:
		m.move(-1)
	case tea.KeyDown:
		m.move(1)
	case tea.KeyRunes, tea.KeySpace:
		m.filter += string(msg.Runes)
	}
	m.fixCursor()
	return m, nil
}

// choose sets the result for the cursor row and quits.
func (m Model) choose(open bool) (tea.Model, tea.Cmd) {
	vis := m.visible()
	i := m.cursorIndex()
	if i < 0 {
		return m, nil
	}
	w := vis[i].WT
	if w.Prunable {
		m.note = missingNote
		return m, nil
	}
	if open {
		m.result = Result{OpenPath: w.Path}
	} else {
		m.result = Result{SwitchPath: w.Path}
	}
	return m, tea.Quit
}

// visible returns the rows that match the filter.
func (m Model) visible() []status.Row {
	if m.filter == "" {
		return m.rows
	}
	f := strings.ToLower(m.filter)
	var out []status.Row
	for _, r := range m.rows {
		if strings.Contains(strings.ToLower(r.WT.Branch), f) ||
			strings.Contains(strings.ToLower(filepath.Base(r.WT.Path)), f) {
			out = append(out, r)
		}
	}
	return out
}

// cursorIndex returns the index of the cursor in visible(), or -1 if no row shows.
func (m Model) cursorIndex() int {
	vis := m.visible()
	for i, r := range vis {
		if r.WT.Path == m.cursor {
			return i
		}
	}
	if len(vis) > 0 {
		return 0
	}
	return -1
}

// fixCursor puts the cursor on the first row if its row does not show.
func (m *Model) fixCursor() {
	vis := m.visible()
	if i := m.cursorIndex(); i >= 0 {
		m.cursor = vis[i].WT.Path
	}
}

func (m *Model) move(delta int) {
	vis := m.visible()
	i := m.cursorIndex()
	if i < 0 {
		return
	}
	// The cursor wraps: up from the first row goes to the last row.
	i = ((i+delta)%len(vis) + len(vis)) % len(vis)
	m.cursor = vis[i].WT.Path
}

// busyKey handles a key while a prune step runs. No key can change the mode
// or move the cursor. ctrl+c quits. q and esc quit only while the step
// reads, not while it prunes.
func (m Model) busyKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m.quitNow()
	case "q", "esc":
		if !m.writing {
			return m, tea.Quit
		}
	}
	return m, nil
}

// cursorPath returns the path of the cursor row, or "" if no row shows.
func (m Model) cursorPath() string {
	if i := m.cursorIndex(); i >= 0 {
		return m.visible()[i].WT.Path
	}
	return ""
}

// mainPath returns the path of the main worktree, or "" if no row is main.
func (m Model) mainPath() string {
	for _, r := range m.rows {
		if r.WT.IsMain {
			return r.WT.Path
		}
	}
	return ""
}

func (m Model) pruneListed(msg pruneListMsg) (tea.Model, tea.Cmd) {
	m.busy = ""
	switch {
	case msg.err != nil:
		m.note = "prune failed: " + msg.err.Error()
	case len(msg.lines) == 0:
		m.note = "nothing to prune"
	case m.mode != modeList:
		m.note = cancelledNote
	default:
		m.pruneLines = msg.lines
		m.mode = modePrune
	}
	return m, nil
}

// pruneKey handles the prune prompt: only y prunes. Every other key cancels.
func (m Model) pruneKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "y":
		n := len(m.pruneLines)
		m.mode, m.pruneLines = modeList, nil
		if m.busy != "" {
			return m, nil
		}
		m.busy, m.writing = "pruning", true
		return m, pruneCmd(m.d.Git, n)
	case "ctrl+c":
		return m.quitNow()
	}
	return m.cancel()
}
