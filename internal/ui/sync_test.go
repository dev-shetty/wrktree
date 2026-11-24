package ui

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/dev-shetty/wrktree/internal/git"
	"github.com/dev-shetty/wrktree/internal/testrepo"
)

// syncing returns loaded(t) with a sync job for each path. The cursor is
// on the main row.
func syncing(t *testing.T, paths ...string) Model {
	t.Helper()
	m := loaded(t)
	for _, p := range paths {
		m.cursor = p
		var cmd tea.Cmd
		m, cmd = keys(t, m, "s")
		if cmd == nil || m.jobs[p].state != jobSyncing {
			t.Fatalf("%s: s starts no sync: jobs %+v", p, m.jobs)
		}
	}
	m.cursor = mainPath
	return m
}

// syncOf runs cmd and returns its sync message. It does not run the tick.
func syncOf(t *testing.T, cmd tea.Cmd) syncedMsg {
	t.Helper()
	for _, msg := range msgs(cmd) {
		if s, ok := msg.(syncedMsg); ok {
			return s
		}
	}
	t.Fatal("the command returns no syncedMsg")
	return syncedMsg{}
}

// TestSyncKeyStartsJob: s gives the cursor row a sync job with a spinner.
// The screen stays live.
func TestSyncKeyStartsJob(t *testing.T) {
	m := loaded(t)
	m.cursor = flowsPath
	m, cmd := keys(t, m, "s")
	if j, ok := m.jobs[flowsPath]; !ok || j.state != jobSyncing {
		t.Fatalf("job after s = %+v, %v; want syncing", j, ok)
	}
	if !hasTick(cmd) {
		t.Error("s starts no spinner tick")
	}
	if m.busy != "" || m.writing {
		t.Errorf("s sets the global lock: busy %q", m.busy)
	}
	m, _ = keys(t, m, "k")
	if m.cursor == flowsPath {
		t.Error("k does not move the cursor while a sync runs")
	}
	l := lineWith(t, m.View(), "eng-6458")
	if !strings.Contains(l, " "+spinFrames[m.spin]+" syncing") {
		t.Errorf("row while the sync runs: %q", l)
	}
	m, _ = send(t, m, tickMsg{})
	if l := lineWith(t, m.View(), "eng-6458"); !strings.Contains(l, " "+spinFrames[m.spin]+" syncing") {
		t.Errorf("row after a tick: %q", l)
	}
}

// TestSyncOneJobPerRow: a row with a job refuses s, d, D, enter, and o.
func TestSyncOneJobPerRow(t *testing.T) {
	m := syncing(t, flowsPath)
	m.cursor = flowsPath
	for _, k := range []string{"s", "d", "D", "enter", "o"} {
		m2, cmd := keys(t, m, k)
		if k == "enter" || k == "o" {
			// The quit rule comes first for these keys.
			if cmd != nil || m2.Result() != (Result{}) {
				t.Errorf("%s on the sync row: cmd %v, Result %+v", k, cmd != nil, m2.Result())
			}
			continue
		}
		if cmd != nil || m2.note != "sync in progress" || m2.jobs[flowsPath].state != jobSyncing {
			t.Errorf("%s on the sync row: cmd %v, note %q, jobs %+v", k, cmd != nil, m2.note, m2.jobs)
		}
	}
	// A row with a check job (no quit rule) refuses enter and o with its word.
	m = loaded(t)
	m.cursor = flowsPath
	m, _ = keys(t, m, "d")
	for _, k := range []string{"s", "enter", "o"} {
		m2, cmd := keys(t, m, k)
		if cmd != nil || m2.note != "delete in progress" {
			t.Errorf("%s on the delete row: cmd %v, note %q", k, cmd != nil, m2.note)
		}
	}
	// A different row can start a sync.
	m = syncing(t, flowsPath)
	m.cursor = alertPath
	m, cmd := keys(t, m, "s")
	if cmd == nil || m.jobs[alertPath].state != jobSyncing || m.jobs[flowsPath].state != jobSyncing {
		t.Errorf("s on a second row: jobs %+v", m.jobs)
	}
}

// TestSyncWhilePruneRuns: s does nothing while a prune step runs. p does
// nothing while a sync runs.
func TestSyncWhilePruneRuns(t *testing.T) {
	m := loaded(t)
	m, _ = keys(t, m, "p")
	if m2, cmd := keys(t, m, "s"); cmd != nil || len(m2.jobs) != 0 {
		t.Error("s while a prune list runs starts a sync")
	}
	m, _ = send(t, m, pruneListMsg{lines: []string{"Removing worktrees/x"}})
	m, _ = keys(t, m, "y")
	if m2, cmd := keys(t, m, "s"); cmd != nil || len(m2.jobs) != 0 {
		t.Error("s while a prune runs starts a sync")
	}

	m = syncing(t, flowsPath)
	m, cmd := keys(t, m, "p")
	if cmd != nil || m.note != "sync in progress" || m.busy != "" {
		t.Errorf("p while a sync runs: cmd %v, note %q, busy %q", cmd != nil, m.note, m.busy)
	}
}

// TestSyncNotes: the note for each result. Only a sync that changed the
// branch reads the values of the row again.
func TestSyncNotes(t *testing.T) {
	const label = "eng-6458-bugsink-key-flows"
	for _, tc := range []struct {
		name    string
		res     git.SyncResult
		err     error
		note    string
		refresh bool
	}{
		{"synced", git.SyncResult{}, nil, "synced " + label + " with origin/master", true},
		{"synced, fetch failed", git.SyncResult{FetchFailed: true}, nil,
			"synced " + label + " with origin/master (fetch failed: used the local copy)", true},
		{"up to date", git.SyncResult{UpToDate: true}, nil, label + " is already up to date with origin/master", false},
		{"up to date, fetch failed", git.SyncResult{UpToDate: true, FetchFailed: true}, nil,
			label + " is already up to date with origin/master (fetch failed: used the local copy)", false},
		{"conflict", git.SyncResult{Conflicts: []string{"a", "b", "c", "d"}}, nil, "sync failed: conflict in a, b, c and 1 more", false},
		{"refused", git.SyncResult{}, fmt.Errorf("x: %w", git.ErrSyncRefused), "x: sync refused", false},
		{"error", git.SyncResult{}, errors.New("boom"), "sync failed: boom", false},
	} {
		m := syncing(t, flowsPath)
		wt := m.row(flowsPath).WT
		m, cmd := send(t, m, syncedMsg{path: flowsPath, wt: wt, base: "origin/master", res: tc.res, err: tc.err})
		if m.note != tc.note {
			t.Errorf("%s: note %q, want %q", tc.name, m.note, tc.note)
		}
		if len(m.jobs) != 0 {
			t.Errorf("%s: the job stays: %+v", tc.name, m.jobs)
		}
		got := m.changedWait[flowsPath] && m.distWait[flowsPath] && m.mergedWait[flowsPath] &&
			m.commitWait[flowsPath] && sizePlanned(m, flowsPath)
		if got != tc.refresh || (cmd != nil) != tc.refresh {
			t.Errorf("%s: values read again %v (cmd %v), want %v", tc.name, got, cmd != nil, tc.refresh)
		}
		if l := lineWith(t, m.View(), "eng-6458"); strings.Contains(l, "syncing") {
			t.Errorf("%s: row after the sync: %q", tc.name, l)
		}
	}
	// The refusal text shows as it is.
	m := syncing(t, flowsPath)
	_, err := (git.Runner{}).Sync(t.Context(), git.Worktree{Path: "/x", Detached: true}, "origin/master")
	m, _ = send(t, m, syncedMsg{path: flowsPath, wt: m.row(flowsPath).WT, base: "origin/master", err: err})
	if m.note != "detached worktree: no branch to update" {
		t.Errorf("refusal note %q", m.note)
	}
}

// TestQuitKeysWhileSyncRuns: q, esc, enter, and o do not quit while a sync
// or a delete runs. The note counts the jobs. ctrl+c quits.
func TestQuitKeysWhileSyncRuns(t *testing.T) {
	both := syncing(t, flowsPath)
	both.cursor = alertPath
	both, _ = keys(t, both, "d")
	both, _ = send(t, both, planMsg{wt: both.row(alertPath).WT})
	if both.jobs[alertPath].state != jobDeleting {
		t.Fatalf("jobs %+v", both.jobs)
	}
	both.cursor = mainPath
	for _, tc := range []struct {
		name string
		m    Model
		note string
	}{
		{"1 sync", syncing(t, flowsPath), "1 sync in progress: wait, or press ctrl+c"},
		{"2 syncs", syncing(t, flowsPath, alertPath), "2 syncs in progress: wait, or press ctrl+c"},
		{"both", both, "2 jobs in progress: wait, or press ctrl+c"},
		{"1 delete", deleting(t, flowsPath), "1 delete in progress: wait, or press ctrl+c"},
	} {
		for _, k := range []string{"q", "esc", "enter", "o"} {
			m2, cmd := keys(t, tc.m, k)
			if cmd != nil || m2.Result() != (Result{}) || m2.note != tc.note {
				t.Errorf("%s, %s: cmd %v, Result %+v, note %q", tc.name, k, cmd != nil, m2.Result(), m2.note)
			}
		}
		if _, cmd := keys(t, tc.m, "ctrl+c"); !isQuit(cmd) {
			t.Errorf("%s: ctrl+c does not quit", tc.name)
		}
	}
}

// TestSyncRowWidth: no line is wider than the terminal while a row syncs.
func TestSyncRowWidth(t *testing.T) {
	old := renderer.ColorProfile()
	renderer.SetColorProfile(termenv.ANSI)
	t.Cleanup(func() { renderer.SetColorProfile(old) })
	const (
		yellow = "\x1b[33m"
		faint  = "\x1b[2m"
	)
	for _, w := range []int{40, 80} {
		for _, cur := range []string{mainPath, alertPath} {
			m := syncing(t, alertPath)
			m.cursor = cur
			m, _ = send(t, m, tea.WindowSizeMsg{Width: w, Height: 20})
			v := m.View()
			for _, l := range strings.Split(v, "\n") {
				if lipgloss.Width(l) > w {
					t.Errorf("width %d: line too wide (%d): %q", w, lipgloss.Width(l), l)
				}
			}
			l := lineWith(t, v, "4 files") // the alert row
			if !strings.Contains(l, "syncing") {
				t.Errorf("width %d: the cut row lost its job: %q", w, l)
			}
			if cur == alertPath {
				if !strings.HasPrefix(l, "\x1b[1;36m") || strings.Contains(l, yellow) || strings.Contains(l, faint) {
					t.Errorf("width %d: cursor row: %q", w, l)
				}
				continue
			}
			if !strings.Contains(l, yellow+" "+spinFrames[m.spin]+" syncing") || !strings.Contains(l, faint) {
				t.Errorf("width %d: job row is not dim with a yellow job: %q", w, l)
			}
		}
	}
}

func TestKeyFooterText(t *testing.T) {
	const want = " enter switch · o open · c copy · s sync · d delete · D delete + branch · p prune · f fetch · / filter · q quit"
	m := loaded(t)
	m, _ = send(t, m, tea.WindowSizeMsg{Width: 200, Height: 30})
	if !strings.Contains(m.View(), "\n"+want) {
		t.Errorf("footer:\n%s", m.View())
	}
}

// pushMaster commits file with text on master and pushes it to origin.
func pushMaster(t *testing.T, repo *testrepo.Repo, file, text string) string {
	t.Helper()
	if err := os.WriteFile(filepath.Join(repo.Dir, file), []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	repo.Git(t, repo.Dir, "add", file)
	repo.Git(t, repo.Dir, "commit", "-m", "master "+file)
	repo.Git(t, repo.Dir, "push", "origin", "master")
	return repo.Git(t, repo.Dir, "rev-parse", "master")
}

// syncRepo presses s on the row at path and runs the sync command. It
// returns the model after the result and the command that the result starts.
func syncRepo(t *testing.T, m Model, path string) (Model, tea.Cmd) {
	t.Helper()
	m.cursor = path
	m, cmd := keys(t, m, "s")
	if m.jobs[path].state != jobSyncing {
		t.Fatalf("s gives no sync job: %+v", m.jobs)
	}
	msg := syncOf(t, cmd)
	if msg.path != path {
		t.Fatalf("sync of %s, want %s", msg.path, path)
	}
	return send(t, m, msg)
}

func TestSyncRealClean(t *testing.T) {
	repo := testrepo.New(t)
	feat := realPath(t, repo.AddWorktree(t, "feat"))
	repo.Commit(t, feat, "f1.txt")
	tip := pushMaster(t, repo, "m1.txt", "master\n")
	m, _ := repoModel(t, repo, repo.Dir, true)
	m, cmd := syncRepo(t, m, feat)
	if m.note != "synced feat with origin/master" || len(m.jobs) != 0 {
		t.Fatalf("note %q, jobs %+v", m.note, m.jobs)
	}
	repo.Git(t, feat, "merge-base", "--is-ancestor", tip, "HEAD")
	if s := repo.Git(t, feat, "status", "--porcelain"); s != "" {
		t.Errorf("status %q", s)
	}
	var dist *distMsg
	for _, msg := range msgs(cmd) {
		if d, ok := msg.(distMsg); ok && d.path == feat {
			dist = &d
		}
	}
	if dist == nil || dist.err != nil || dist.behind != 0 || dist.ahead != 2 {
		t.Errorf("distance after the sync: %+v", dist)
	}
}

func TestSyncRealConflict(t *testing.T) {
	repo := testrepo.New(t)
	feat := realPath(t, repo.AddWorktree(t, "feat"))
	if err := os.WriteFile(filepath.Join(feat, "README.md"), []byte("feat\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	repo.Git(t, feat, "commit", "-am", "feat readme")
	pushMaster(t, repo, "README.md", "master\n")
	before := repo.Git(t, feat, "rev-parse", "HEAD")
	m, _ := repoModel(t, repo, repo.Dir, true)
	m, cmd := syncRepo(t, m, feat)
	if m.note != "sync failed: conflict in README.md" || len(m.jobs) != 0 || cmd != nil {
		t.Fatalf("note %q, jobs %+v, cmd %v", m.note, m.jobs, cmd != nil)
	}
	if h := repo.Git(t, feat, "rev-parse", "HEAD"); h != before {
		t.Error("HEAD moved")
	}
	if s := repo.Git(t, feat, "status", "--porcelain"); s != "" {
		t.Errorf("status %q", s)
	}
	if b, _ := os.ReadFile(filepath.Join(feat, "README.md")); string(b) != "feat\n" {
		t.Errorf("README.md = %q", b)
	}
}

func TestSyncRealDirty(t *testing.T) {
	repo := testrepo.New(t)
	feat := realPath(t, repo.AddWorktree(t, "feat"))
	pushMaster(t, repo, "m1.txt", "master\n")
	file := filepath.Join(feat, "README.md")
	if err := os.WriteFile(file, []byte("mine\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	before := repo.Git(t, feat, "rev-parse", "HEAD")
	m, _ := repoModel(t, repo, repo.Dir, true)
	m, _ = syncRepo(t, m, feat)
	if m.note != "has 1 changed file: commit or stash first" || len(m.jobs) != 0 {
		t.Errorf("note %q, jobs %+v", m.note, m.jobs)
	}
	if h := repo.Git(t, feat, "rev-parse", "HEAD"); h != before {
		t.Error("HEAD moved")
	}
	if b, _ := os.ReadFile(file); string(b) != "mine\n" {
		t.Errorf("README.md = %q", b)
	}
}

// TestQuitAfterCurrentDeleteWaitsForSync: after the delete of the current
// worktree, the screen quits only when the sync is done.
func TestQuitAfterCurrentDeleteWaitsForSync(t *testing.T) {
	m := syncing(t, flowsPath)
	m.after = Result{SwitchPath: mainPath}
	m, cmd := send(t, m, removedMsg{path: alertPath})
	if isQuit(cmd) {
		t.Fatal("quit while a sync runs")
	}
	m, cmd = send(t, m, syncedMsg{path: flowsPath, wt: m.row(flowsPath).WT, base: "origin/master", res: git.SyncResult{UpToDate: true}})
	if !isQuit(cmd) || m.Result() != (Result{SwitchPath: mainPath}) {
		t.Errorf("after the sync: quit %v, Result %+v", isQuit(cmd), m.Result())
	}
}
