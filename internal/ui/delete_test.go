package ui

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/dev-shetty/wrktree/internal/disk"
	"github.com/dev-shetty/wrktree/internal/git"
	"github.com/dev-shetty/wrktree/internal/testrepo"
)

const refusal = "cannot delete the current worktree: run through the wt function"

func realPath(t *testing.T, p string) string {
	t.Helper()
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r
	}
	return p
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// branches returns the local branches of repo, one for each line.
func branches(t *testing.T, repo *testrepo.Repo) string {
	t.Helper()
	return repo.Git(t, repo.Dir, "branch", "--format=%(refname:short)")
}

func hasBranch(t *testing.T, repo *testrepo.Repo, name string) bool {
	t.Helper()
	for _, b := range strings.Split(branches(t, repo), "\n") {
		if b == name {
			return true
		}
	}
	return false
}

// repoModel loads a screen for a real test repository. The screen runs in cwd.
func repoModel(t *testing.T, repo *testrepo.Repo, cwd string, canCD bool) (Model, Deps) {
	t.Helper()
	r, err := git.Open(cwd)
	if err != nil {
		t.Fatal(err)
	}
	d := deps(t)
	d.Git = r
	d.Repo = realPath(t, repo.Dir)
	d.Cwd = cwd
	d.CanCD = canCD
	m := New(d)
	m, _ = send(t, m, tea.WindowSizeMsg{Width: 160, Height: 30})
	m, _ = send(t, m, m.Init()())
	if m.loadErr != nil {
		t.Fatal(m.loadErr)
	}
	return m, d
}

// wt returns the worktree of the row at path.
func wt(t *testing.T, m Model, path string) git.Worktree {
	t.Helper()
	r := m.row(path)
	if r == nil {
		t.Fatalf("no row for %s", path)
	}
	return r.WT
}

// press moves the cursor to path, presses k (d or D), runs the plan command,
// and sends its message. It returns the command that the plan message starts.
func press(t *testing.T, m Model, path, k string) (Model, tea.Cmd) {
	t.Helper()
	m.cursor = path
	m, cmd := keys(t, m, k)
	if cmd == nil {
		t.Fatalf("%s starts no command", k)
	}
	return send(t, m, planOf(t, cmd))
}

// planOf runs cmd and returns its plan message. It does not run the spinner tick.
func planOf(t *testing.T, cmd tea.Cmd) planMsg {
	t.Helper()
	for _, msg := range msgs(cmd) {
		if p, ok := msg.(planMsg); ok {
			return p
		}
	}
	t.Fatal("the command returns no planMsg")
	return planMsg{}
}

// run runs cmd and returns its message.
func run(t *testing.T, cmd tea.Cmd) tea.Msg {
	t.Helper()
	if cmd == nil {
		t.Fatal("no command")
	}
	return cmd()
}

func inPrompt(m Model) bool {
	return m.mode == modeTyped || strings.Contains(m.View(), typedHint)
}

// noOldPrompt reports an error if the view shows a part of the old prompt.
func noOldPrompt(t *testing.T, v string) {
	t.Helper()
	for _, s := range []string{"[ ]", "[x]", "also delete the branch", "y delete", "tab branch", "b branch"} {
		if strings.Contains(v, s) {
			t.Errorf("view contains %q:\n%s", s, v)
		}
	}
}

func TestDeleteBlockedMain(t *testing.T) {
	for _, k := range []string{"d", "D"} {
		m := loaded(t)
		m.cursor = mainPath
		m, cmd := keys(t, m, k)
		if cmd == nil {
			t.Fatalf("%s starts no command", k)
		}
		m, cmd = send(t, m, planMsg{wt: m.rows[0].WT, plan: git.DeletePlan{Blocked: "the main worktree cannot be deleted"}})
		if cmd != nil {
			t.Errorf("%s: a blocked plan starts a command", k)
		}
		if v := m.View(); !strings.Contains(v, "the main worktree cannot be deleted") {
			t.Errorf("no blocked text:\n%s", v)
		}
		if inPrompt(m) {
			t.Error("blocked plan opens a prompt")
		}
		if len(m.jobs) != 0 {
			t.Errorf("%s: a blocked plan leaves a job: %v", k, m.jobs)
		}
		if _, cmd = keys(t, m, "y"); cmd != nil {
			t.Error("y after a blocked plan starts a command")
		}
	}
}

func TestDeleteBlockedRealMain(t *testing.T) {
	repo := testrepo.New(t)
	repo.AddWorktree(t, "feat-a")
	m, _ := repoModel(t, repo, repo.Dir, true)
	m, cmd := press(t, m, realPath(t, repo.Dir), "D")
	if cmd != nil || !strings.Contains(m.View(), "the main worktree cannot be deleted") || inPrompt(m) {
		t.Errorf("real main worktree: cmd %v\n%s", cmd != nil, m.View())
	}
	if !exists(repo.Dir) || !hasBranch(t, repo, "master") {
		t.Error("main worktree or branch is gone")
	}
}

func TestDeleteBlockedLocked(t *testing.T) {
	repo := testrepo.New(t)
	feat := realPath(t, repo.AddWorktree(t, "feat-a"))
	repo.Git(t, repo.Dir, "worktree", "lock", "--reason", "in use", feat)
	m, _ := repoModel(t, repo, repo.Dir, true)
	for _, k := range []string{"d", "D"} {
		m2, cmd := press(t, m, feat, k)
		if cmd != nil || !strings.Contains(m2.View(), "worktree is locked: in use") || inPrompt(m2) || len(m2.jobs) != 0 {
			t.Errorf("%s on a locked worktree: cmd %v, jobs %v\n%s", k, cmd != nil, m2.jobs, m2.View())
		}
	}
	if !exists(feat) || !hasBranch(t, repo, "feat-a") {
		t.Error("locked worktree or branch is gone")
	}
}

func TestDeletePlanError(t *testing.T) {
	m := loaded(t)
	m.cursor = flowsPath
	m, _ = keys(t, m, "d")
	m, cmd := send(t, m, planMsg{wt: m.row(flowsPath).WT, err: errors.New("status failed")})
	if cmd != nil {
		t.Error("a plan error starts a command")
	}
	if v := m.View(); !strings.Contains(v, "status failed") {
		t.Errorf("no error text:\n%s", v)
	}
	if inPrompt(m) {
		t.Error("a plan error opens a prompt")
	}
	if len(m.jobs) != 0 {
		t.Errorf("a plan error leaves a job: %v", m.jobs)
	}
	if _, cmd := keys(t, m, "y"); cmd != nil {
		t.Error("y after a plan error starts a command")
	}
}

// TestDeleteCleanStartsAtOnce: d on a clean worktree deletes it with no
// prompt and no other key. The branch stays.
func TestDeleteCleanStartsAtOnce(t *testing.T) {
	repo := testrepo.New(t)
	featA := realPath(t, repo.AddWorktree(t, "feat-a"))
	repo.AddWorktree(t, "feat-b")
	m, d := repoModel(t, repo, repo.Dir, true)
	if len(m.rows) != 3 {
		t.Fatalf("rows = %d, want 3", len(m.rows))
	}
	d.Cache.Set(d.Repo, featA, 1024, now)
	if err := d.Cache.Save(); err != nil {
		t.Fatal(err)
	}

	m, cmd := press(t, m, featA, "d")
	if cmd == nil {
		t.Fatalf("a clean plan starts no delete:\n%s", m.View())
	}
	if inPrompt(m) || m.mode != modeList {
		t.Fatalf("a clean plan opens a prompt:\n%s", m.View())
	}
	noOldPrompt(t, m.View())
	if l := lineWith(t, m.View(), "feat-a"); !strings.Contains(l, "deleting") {
		t.Errorf("the row does not show deleting: %q", l)
	}
	if strings.Contains(m.View(), "deleting "+featA) {
		t.Errorf("the footer shows the old deleting text:\n%s", m.View())
	}
	// A second delete of the same row cannot start. p waits for the delete.
	for _, k := range []string{"d", "D", "p", "y"} {
		if _, c := keys(t, m, k); c != nil {
			t.Errorf("%s while a delete runs starts a command", k)
		}
	}
	msg, ok := run(t, cmd).(removedMsg)
	if !ok || msg.err != nil || msg.branchErr != nil || msg.path != featA {
		t.Fatalf("removedMsg = %+v", msg)
	}
	if exists(featA) {
		t.Error("folder still exists")
	}
	if !hasBranch(t, repo, "feat-a") {
		t.Error("d deletes the branch")
	}
	m, cmd = send(t, m, msg)
	if isQuit(cmd) || m.Result() != (Result{}) {
		t.Error("delete of another worktree quits")
	}
	if len(m.rows) != 2 || m.row(featA) != nil {
		t.Errorf("rows after delete: %d", len(m.rows))
	}
	if _, ok := d.Cache.Get(d.Repo, featA); ok {
		t.Error("cache keeps the entry")
	}
	if _, ok := disk.LoadCache(cachePath()).Get(d.Repo, featA); ok {
		t.Error("saved cache keeps the entry")
	}
	if m.note != "deleted feat-a" {
		t.Errorf("note = %q, want %q", m.note, "deleted feat-a")
	}
	if _, c := keys(t, m, "d"); c == nil {
		t.Error("d does not work after the delete is done")
	}
}

// TestDeleteCleanWithBranch: D deletes the worktree and then its branch.
func TestDeleteCleanWithBranch(t *testing.T) {
	repo := testrepo.New(t)
	feat := realPath(t, repo.AddWorktree(t, "feat-a"))
	m, _ := repoModel(t, repo, repo.Dir, true)
	m, cmd := press(t, m, feat, "D")
	if cmd == nil || inPrompt(m) {
		t.Fatalf("D on a clean worktree: cmd %v\n%s", cmd != nil, m.View())
	}
	msg, ok := run(t, cmd).(removedMsg)
	if !ok || msg.err != nil || msg.branchErr != nil {
		t.Fatalf("removedMsg = %+v", msg)
	}
	if exists(feat) {
		t.Error("folder still exists")
	}
	if hasBranch(t, repo, "feat-a") {
		t.Error("branch still exists")
	}
	m, _ = send(t, m, msg)
	if want := "deleted feat-a and its branch"; m.note != want {
		t.Errorf("note = %q, want %q", m.note, want)
	}
}

// TestDeleteBranchUnmerged: git branch -d refuses an unmerged branch. The
// worktree delete stays done and the footer shows the git message.
func TestDeleteBranchUnmerged(t *testing.T) {
	repo := testrepo.New(t)
	feat := realPath(t, repo.AddWorktree(t, "feat-a"))
	repo.Commit(t, feat, "a.txt")
	m, _ := repoModel(t, repo, repo.Dir, true)
	m, cmd := press(t, m, feat, "D")
	msg := run(t, cmd).(removedMsg)
	if msg.err != nil || msg.branchErr == nil {
		t.Fatalf("removedMsg = %+v", msg)
	}
	if exists(feat) {
		t.Error("a branch error must not undo the delete")
	}
	if !hasBranch(t, repo, "feat-a") {
		t.Error("unmerged branch deleted")
	}
	m, _ = send(t, m, msg)
	if m.row(feat) != nil {
		t.Error("row stays after the delete")
	}
	if want := "deleted feat-a, but the branch stays: " + msg.branchErr.Error(); m.note != want {
		t.Errorf("note = %q, want %q", m.note, want)
	}
	if v := m.View(); !strings.Contains(v, "but the branch stays") || strings.Contains(v, "and its branch") {
		t.Errorf("branch error text:\n%s", v)
	}
}

// TestDeleteCleanPlanNeverForces gives a clean plan for a worktree that has
// changes. The delete must not force, so git refuses it.
func TestDeleteCleanPlanNeverForces(t *testing.T) {
	repo := testrepo.New(t)
	feat := realPath(t, repo.AddWorktree(t, "feat-a"))
	m, _ := repoModel(t, repo, repo.Dir, true)
	if err := os.WriteFile(filepath.Join(feat, "late.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	m.cursor = feat
	m, _ = keys(t, m, "d")
	m, cmd := send(t, m, planMsg{wt: wt(t, m, feat)})
	msg, ok := run(t, cmd).(removedMsg)
	if !ok || msg.err == nil {
		t.Fatalf("delete with a clean plan must not force: %+v", msg)
	}
	if !exists(feat) {
		t.Fatal("folder deleted")
	}
	m, _ = send(t, m, msg)
	if m.row(feat) == nil {
		t.Error("row removed after a failed delete")
	}
	if !strings.Contains(m.View(), "delete failed") {
		t.Errorf("no failure text:\n%s", m.View())
	}
	if _, ok := m.jobs[feat]; ok {
		t.Error("the row keeps its job after a failed delete")
	}
	if l := lineWith(t, m.View(), "feat-a"); strings.Contains(l, "deleting") {
		t.Errorf("the row still shows deleting: %q", l)
	}
}

func TestDeleteDirtyNeedsTypedYes(t *testing.T) {
	repo := testrepo.New(t)
	feat := realPath(t, repo.AddWorktree(t, "feat-a"))
	for _, f := range []string{"a.txt", "b.txt", "c.txt"} {
		if err := os.WriteFile(filepath.Join(feat, f), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	m, _ := repoModel(t, repo, repo.Dir, true)

	m, cmd := press(t, m, feat, "d")
	if cmd != nil {
		t.Fatal("a plan with changes starts a command before yes")
	}
	v := m.View()
	for _, s := range []string{"feat-a has 3 changed files. They will be lost. Ignored files are deleted too.",
		"Type yes to delete: █", typedHint} {
		if !strings.Contains(v, s) {
			t.Errorf("typed prompt lacks %q:\n%s", s, v)
		}
	}
	if strings.Contains(v, keyFooter) {
		t.Error("the prompt must replace the key footer")
	}
	if strings.Contains(v, "The branch") {
		t.Errorf("d prompt names the branch:\n%s", v)
	}
	noOldPrompt(t, v)
	for _, answer := range [][]string{
		{"y", "enter"},
		{"Y", "E", "S", "enter"},
		{"y", "e", "s", " ", "enter"},
		{"y", "e", "s", "esc"},
		{"enter"},
	} {
		m2, cmd := keys(t, m, answer...)
		if cmd != nil {
			t.Errorf("%v starts a command", answer)
		}
		if inPrompt(m2) || m2.mode != modeList || m2.note != cancelledNote {
			t.Errorf("%v does not cancel:\n%s", answer, m2.View())
		}
	}
	if !exists(feat) {
		t.Fatal("folder deleted without yes")
	}

	m, _ = keys(t, m, "y", "e", "s", "s", "backspace")
	if !strings.Contains(m.View(), "Type yes to delete: yes█") {
		t.Errorf("typed text does not show:\n%s", m.View())
	}
	// tab is not a key of the prompt any more.
	m, _ = send(t, m, tea.KeyMsg{Type: tea.KeyTab})
	if m.del.branch {
		t.Error("tab selects the branch")
	}
	noOldPrompt(t, m.View())
	m, cmd = keys(t, m, "enter")
	// The delete runs in the background: the list is back and keys work.
	if m.mode != modeList || m.jobs[feat].state != jobDeleting {
		t.Fatalf("after yes: mode %v, job %+v", m.mode, m.jobs[feat])
	}
	if l := lineWith(t, m.View(), "feat-a"); !strings.Contains(l, "deleting") {
		t.Errorf("the row does not show deleting: %q", l)
	}
	if moved, _ := keys(t, m, "j"); moved.cursor == m.cursor {
		t.Error("j does not move the cursor while the delete runs")
	}
	msg, ok := run(t, cmd).(removedMsg)
	// git refuses a worktree with changes unless the command forces it.
	if !ok || msg.err != nil || exists(feat) {
		t.Fatalf("forced delete: %+v, folder exists %v", msg, exists(feat))
	}
	if !hasBranch(t, repo, "feat-a") {
		t.Error("d deletes the branch")
	}
	m, _ = send(t, m, msg)
	if m.note != "deleted feat-a" {
		t.Errorf("note = %q", m.note)
	}
}

func TestDeleteDirtyWithBranch(t *testing.T) {
	repo := testrepo.New(t)
	feat := realPath(t, repo.AddWorktree(t, "feat-a"))
	if err := os.WriteFile(filepath.Join(feat, "a.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	m, _ := repoModel(t, repo, repo.Dir, true)
	m, cmd := press(t, m, feat, "D")
	if cmd != nil {
		t.Fatal("D with changes starts a command before yes")
	}
	v := m.View()
	for _, s := range []string{"feat-a has 1 changed file.", "The branch feat-a is deleted too.", "Type yes to delete: █", typedHint} {
		if !strings.Contains(v, s) {
			t.Errorf("typed prompt lacks %q:\n%s", s, v)
		}
	}
	noOldPrompt(t, v)
	// The branch line comes after the prompt text and before the typed line.
	if a, b, c := strings.Index(v, "Ignored files"), strings.Index(v, "The branch feat-a"), strings.Index(v, "Type yes"); !(a < b && b < c) {
		t.Errorf("line order is wrong:\n%s", v)
	}
	m, cmd = keys(t, m, "y", "e", "s", "enter")
	msg, ok := run(t, cmd).(removedMsg)
	if !ok || msg.err != nil || msg.branchErr != nil || exists(feat) {
		t.Fatalf("removedMsg = %+v, folder exists %v", msg, exists(feat))
	}
	if hasBranch(t, repo, "feat-a") {
		t.Error("branch still exists")
	}
	m, _ = send(t, m, msg)
	if want := "deleted feat-a and its branch"; m.note != want {
		t.Errorf("note = %q, want %q", m.note, want)
	}
}

// TestDeleteDetachedD: D on a detached worktree behaves as d.
func TestDeleteDetachedD(t *testing.T) {
	m := loaded(t)
	w := m.row(flowsPath).WT
	w.Branch, w.Detached, w.Head = "", true, "0123456789abcdef"
	m.cursor = flowsPath
	m, _ = keys(t, m, "D")
	m2, cmd := send(t, m, planMsg{wt: w, plan: git.DeletePlan{Unreachable: 2, NeedsYes: true}})
	if cmd != nil || !inPrompt(m2) {
		t.Fatalf("detached typed prompt: cmd %v\n%s", cmd != nil, m2.View())
	}
	if v := m2.View(); strings.Contains(v, "The branch") || !strings.Contains(v, "0123456") {
		t.Errorf("detached prompt:\n%s", v)
	}
	if m2.del.branch {
		t.Error("D on a detached worktree selects a branch")
	}
	m2, _ = send(t, m2, removedMsg{path: flowsPath})
	if m2.note != "deleted 0123456" {
		t.Errorf("note = %q, want %q", m2.note, "deleted 0123456")
	}

	m2, cmd = send(t, m, planMsg{wt: w})
	if cmd == nil || m2.jobs[flowsPath].branch {
		t.Errorf("clean detached D: cmd %v, branch %v", cmd != nil, m2.jobs[flowsPath].branch)
	}
}

func TestDeleteCurrentWithCD(t *testing.T) {
	repo := testrepo.New(t)
	feat := realPath(t, repo.AddWorktree(t, "feat-a"))
	m, _ := repoModel(t, repo, feat, true)
	if r := m.row(feat); r == nil || !r.Current {
		t.Fatal("feat-a is not the current row")
	}
	m, cmd := press(t, m, feat, "d")
	if cmd == nil || inPrompt(m) {
		t.Fatalf("clean d on the current worktree: cmd %v\n%s", cmd != nil, m.View())
	}
	msg := run(t, cmd).(removedMsg)
	if msg.err != nil || msg.branchErr != nil || exists(feat) {
		t.Fatalf("removedMsg = %+v, folder exists %v", msg, exists(feat))
	}
	if !hasBranch(t, repo, "feat-a") {
		t.Error("d deletes the branch")
	}
	m, cmd = send(t, m, msg)
	if got := m.Result(); got.SwitchPath != realPath(t, repo.Dir) || got.OpenPath != "" {
		t.Errorf("Result = %+v, want SwitchPath %s", got, repo.Dir)
	}
	if !isQuit(cmd) {
		t.Error("delete of the current worktree does not quit")
	}
}

func TestDeleteCurrentWithoutCD(t *testing.T) {
	cases := []struct {
		name  string
		canCD bool
		wts   func() []git.Worktree
	}{
		{"no cd file", false, threeWorktrees},
		{"no main worktree", true, func() []git.Worktree {
			wts := threeWorktrees()
			wts[0].IsMain = false
			return wts
		}},
	}
	for _, tc := range cases {
		for _, plan := range []git.DeletePlan{{}, {Changed: 1, NeedsYes: true}} {
			d := deps(t)
			d.Cwd = alertPath
			d.CanCD = tc.canCD
			m := New(d)
			m, _ = send(t, m, tea.WindowSizeMsg{Width: 120, Height: 30})
			m, _ = send(t, m, worktreesMsg{wts: tc.wts(), base: "origin/master"})
			m.cursor = alertPath
			m, _ = keys(t, m, "d")
			m, cmd := send(t, m, planMsg{wt: m.row(alertPath).WT, plan: plan})
			if cmd != nil {
				t.Errorf("%s: the plan starts a command", tc.name)
			}
			if v := m.View(); !strings.Contains(v, refusal) || inPrompt(m) || len(m.jobs) != 0 {
				t.Errorf("%s: jobs %v\n%s", tc.name, m.jobs, v)
			}
			if _, cmd := keys(t, m, "y"); cmd != nil {
				t.Errorf("%s: y starts a command", tc.name)
			}
		}
	}
}

func TestDeletePromptWidth(t *testing.T) {
	for _, w := range []int{30, 50} {
		for _, k := range []string{"d", "D"} {
			m := loaded(t)
			m, _ = send(t, m, tea.WindowSizeMsg{Width: w, Height: 20})
			m.cursor = alertPath
			m, _ = keys(t, m, k)
			m, _ = send(t, m, planMsg{wt: m.row(alertPath).WT, plan: git.DeletePlan{Changed: 4, NeedsYes: true}})
			m, _ = keys(t, m, "y", "e", "s")
			v := m.View()
			for _, l := range strings.Split(v, "\n") {
				if lipgloss.Width(l) > w {
					t.Errorf("width %d: line too wide (%d): %q", w, lipgloss.Width(l), l)
				}
			}
			flat := strings.Join(strings.Fields(v), " ")
			for _, s := range []string{"Ignored files are deleted too.", "type yes + enter", "esc cancel"} {
				if !strings.Contains(flat, s) {
					t.Errorf("width %d: prompt text %q is lost:\n%s", w, s, v)
				}
			}
			if k == "D" && !strings.Contains(flat, "is deleted too.") {
				t.Errorf("width %d: branch line is lost:\n%s", w, v)
			}
		}
	}
}

// removeStale presses k on the [missing] row at path. No plan runs and no
// prompt opens: the row shows deleting at once. Then it runs the command,
// sends its result, runs the reload, and sends the reload.
func removeStale(t *testing.T, m Model, path, branch, k string) Model {
	t.Helper()
	m.cursor = path
	m, cmd := keys(t, m, k)
	if cmd == nil {
		t.Fatalf("%s on a [missing] row starts no command", k)
	}
	if inPrompt(m) || m.mode != modeList {
		t.Fatalf("%s on a [missing] row opens a prompt:\n%s", k, m.View())
	}
	if j, ok := m.jobs[path]; !ok || j.state != jobDeleting {
		t.Fatalf("job after %s = %+v, %v; want deleting", k, j, ok)
	}
	if l := lineWith(t, m.View(), branch); !strings.Contains(l, "deleting") {
		t.Errorf("row after %s: %q", k, l)
	}
	var res tea.Msg
	for _, msg := range msgs(cmd) {
		if _, ok := msg.(planMsg); ok {
			t.Fatalf("%s on a [missing] row reads a delete plan", k)
		}
		res = msg
	}
	if res == nil {
		t.Fatalf("%s: the command returns no message", k)
	}
	m, cmd = send(t, m, res)
	for _, msg := range msgs(cmd) {
		if reload, ok := msg.(worktreesMsg); ok {
			if reload.err != nil {
				t.Fatal(reload.err)
			}
			m, _ = send(t, m, reload)
			return m
		}
	}
	t.Fatalf("%s: no reload after the stale record is removed", k)
	return m
}

// TestDeleteStaleRecord: d and D on a [missing] row remove the stale record
// with git worktree prune. The branch stays, and a healthy worktree stays.
func TestDeleteStaleRecord(t *testing.T) {
	for _, k := range []string{"d", "D"} {
		repo := testrepo.New(t)
		feat := realPath(t, repo.AddWorktree(t, "feat-a"))
		healthy := realPath(t, repo.AddWorktree(t, "feat-b"))
		if err := os.RemoveAll(feat); err != nil {
			t.Fatal(err)
		}
		m, _ := repoModel(t, repo, repo.Dir, true)
		if r := m.row(feat); r == nil || !r.WT.Prunable {
			t.Fatalf("%s: no [missing] row for %s", k, feat)
		}
		m = removeStale(t, m, feat, "feat-a", k)
		if m.row(feat) != nil {
			t.Errorf("%s: the [missing] row stays:\n%s", k, m.View())
		}
		if out := repo.Git(t, repo.Dir, "worktree", "list", "--porcelain"); strings.Contains(out, feat) {
			t.Errorf("%s: stale record still listed:\n%s", k, out)
		}
		if !hasBranch(t, repo, "feat-a") {
			t.Errorf("%s deletes the branch of a stale record", k)
		}
		if m.row(healthy) == nil || !exists(healthy) || !hasBranch(t, repo, "feat-b") {
			t.Errorf("%s: the healthy worktree is changed", k)
		}
		if m.note != "removed 1 stale record" {
			t.Errorf("%s: note %q", k, m.note)
		}
		if len(m.jobs) != 0 {
			t.Errorf("%s: jobs after the reload: %+v", k, m.jobs)
		}
	}
}

// TestDeleteTwoStaleRecords: git worktree prune removes every stale record,
// and the note counts the [missing] rows that went.
func TestDeleteTwoStaleRecords(t *testing.T) {
	repo := testrepo.New(t)
	a := realPath(t, repo.AddWorktree(t, "feat-a"))
	b := realPath(t, repo.AddWorktree(t, "feat-b"))
	healthy := realPath(t, repo.AddWorktree(t, "feat-c"))
	for _, p := range []string{a, b} {
		if err := os.RemoveAll(p); err != nil {
			t.Fatal(err)
		}
	}
	m, _ := repoModel(t, repo, repo.Dir, true)
	m = removeStale(t, m, a, "feat-a", "d")
	if m.row(a) != nil || m.row(b) != nil {
		t.Errorf("a [missing] row stays:\n%s", m.View())
	}
	if m.row(healthy) == nil || !exists(healthy) {
		t.Error("the healthy worktree is changed")
	}
	if m.note != "removed 2 stale records" {
		t.Errorf("note %q", m.note)
	}
	if !hasBranch(t, repo, "feat-a") || !hasBranch(t, repo, "feat-b") {
		t.Error("a branch of a stale record is deleted")
	}
}

// missingModel returns loaded(t) with the alert row [missing] and the
// cursor on it. Its git commands fail: never run the stale command of it.
func missingModel(t *testing.T) Model {
	t.Helper()
	m := New(deps(t))
	wts := threeWorktrees()
	wts[1].Prunable = true
	m, _ = send(t, m, tea.WindowSizeMsg{Width: 120, Height: 30})
	m, _ = send(t, m, worktreesMsg{wts: wts, base: "origin/master"})
	m.cursor = alertPath
	return m
}

// TestStaleJobRules: while a stale record is removed, the rules of a
// deleting job apply.
func TestStaleJobRules(t *testing.T) {
	m := missingModel(t)
	m, cmd := keys(t, m, "d")
	if cmd == nil || m.jobs[alertPath].state != jobDeleting {
		t.Fatalf("d on a [missing] row: cmd %v, jobs %+v", cmd != nil, m.jobs)
	}
	for _, k := range []string{"enter", "o", "d", "D", "s"} {
		m2, cmd := keys(t, m, k)
		if cmd != nil || m2.Result() != (Result{}) || !strings.Contains(m2.note, inProgressNote) {
			t.Errorf("%s on the row: cmd %v, note %q", k, cmd != nil, m2.note)
		}
	}
	for _, k := range []string{"q", "esc"} {
		m2, cmd := keys(t, m, k)
		if cmd != nil || m2.note != "1 delete in progress: wait, or press ctrl+c" {
			t.Errorf("%s: cmd %v, note %q", k, cmd != nil, m2.note)
		}
	}
	if m2, _ := keys(t, m, "j"); m2.cursor == alertPath {
		t.Error("j does not move the cursor")
	}
	if _, cmd := keys(t, m, "ctrl+c"); !isQuit(cmd) {
		t.Error("ctrl+c does not quit")
	}
}

// TestStaleRecordError: after an error, the job goes, the row stays, and
// no reload starts.
func TestStaleRecordError(t *testing.T) {
	m := missingModel(t)
	m, _ = keys(t, m, "D")
	m, cmd := send(t, m, staleMsg{path: alertPath, err: errors.New("boom")})
	if cmd != nil || len(m.jobs) != 0 || m.row(alertPath) == nil {
		t.Errorf("cmd %v, jobs %v, row %v", cmd != nil, m.jobs, m.row(alertPath) != nil)
	}
	if m.note != "cannot remove the stale record: boom" {
		t.Errorf("note %q", m.note)
	}
}

// TestNoPruneKey: p does nothing, and the screen does not show prune.
func TestNoPruneKey(t *testing.T) {
	m := missingModel(t)
	m2, cmd := keys(t, m, "p")
	if cmd != nil || m2.mode != modeList || len(m2.jobs) != 0 || m2.note != "" {
		t.Errorf("p: cmd %v, mode %v, jobs %v, note %q", cmd != nil, m2.mode, m2.jobs, m2.note)
	}
	if v := m2.View(); strings.Contains(strings.ToLower(v), "prune") {
		t.Errorf("view contains prune:\n%s", v)
	}
}

// TestKeysLiveWhilePlanRuns: between d and the plan, the cursor moves, and
// the row of the plan refuses enter, o, d, and D. A key typed early never
// answers the typed prompt. D on another row does not change the branch
// choice of the plan.
func TestKeysLiveWhilePlanRuns(t *testing.T) {
	m := loaded(t) // rows: main, flows, alert
	m.cursor = flowsPath
	m, _ = keys(t, m, "d")
	if j, ok := m.jobs[flowsPath]; !ok || j.state != jobChecking || j.branch {
		t.Fatalf("job = %+v, %v; want checking, no branch", j, ok)
	}
	for _, k := range []string{"enter", "o", "d", "D"} {
		m2, cmd := keys(t, m, k)
		if cmd != nil || m2.note != inProgressNote || m2.Result() != (Result{}) {
			t.Errorf("%s on the row of the plan: cmd %v, note %q", k, cmd != nil, m2.note)
		}
	}
	for _, k := range []string{"j", "k", "down", "up", "right", "left"} {
		if m2, _ := keys(t, m, k); m2.cursor == flowsPath {
			t.Errorf("%s does not move the cursor while a plan runs", k)
		}
	}
	m, _ = keys(t, m, "y", "e", "s", "j", "D") // D on the alert row
	if !m.jobs[alertPath].branch || m.jobs[flowsPath].branch {
		t.Fatalf("jobs = %+v", m.jobs)
	}

	// The typed prompt starts empty: early keys are not in the answer.
	m2, cmd := send(t, m, planMsg{wt: m.row(flowsPath).WT, plan: git.DeletePlan{Changed: 2, NeedsYes: true}})
	if cmd != nil || !inPrompt(m2) || m2.typed != "" || m2.del.branch {
		t.Fatalf("typed prompt after early keys: cmd %v, typed %q, branch %v", cmd != nil, m2.typed, m2.del.branch)
	}
	if m2.cursor != flowsPath {
		t.Errorf("cursor on %s, want the row of the prompt", m2.cursor)
	}
	if _, cmd = keys(t, m2, "enter"); cmd != nil {
		t.Error("enter alone deletes")
	}
}

func TestQuitWhilePlanRuns(t *testing.T) {
	for _, k := range []string{"q", "esc", "ctrl+c"} {
		for _, del := range []string{"d", "D"} {
			m := loaded(t)
			m, _ = keys(t, m, del)
			if _, cmd := keys(t, m, k); !isQuit(cmd) {
				t.Errorf("%s while a %s plan runs does not quit", k, del)
			}
		}
	}
}

// TestPlanWithoutJobCancels: a plan for a row that has no job (no d started
// it, or a reload removed the job) deletes nothing.
func TestPlanWithoutJobCancels(t *testing.T) {
	for _, plan := range []git.DeletePlan{{}, {Changed: 1, NeedsYes: true}} {
		m := loaded(t)
		m.cursor = flowsPath
		m, cmd := send(t, m, planMsg{wt: m.row(alertPath).WT, plan: plan})
		if cmd != nil {
			t.Error("a plan without a job starts a command")
		}
		if inPrompt(m) || m.mode != modeList || m.note != cancelledNote || len(m.jobs) != 0 {
			t.Errorf("a plan without a job: jobs %v\n%s", m.jobs, m.View())
		}
		if _, cmd := keys(t, m, "y"); cmd != nil {
			t.Error("y starts a command")
		}
	}
}

// TestPlanAfterCursorMovedOpensPrompt: a plan that needs yes moves the
// cursor back to its row and opens the prompt for that row.
func TestPlanAfterCursorMovedOpensPrompt(t *testing.T) {
	m := loaded(t)
	m.cursor = flowsPath
	m, _ = keys(t, m, "D", "k") // the cursor goes to main
	if m.cursor != mainPath {
		t.Fatalf("cursor on %s", m.cursor)
	}
	m, cmd := send(t, m, planMsg{wt: m.row(flowsPath).WT, plan: git.DeletePlan{Changed: 2, NeedsYes: true}})
	if cmd != nil || m.mode != modeTyped || m.cursor != flowsPath || m.del.wt.Path != flowsPath || !m.del.branch {
		t.Fatalf("cmd %v, mode %v, cursor %s, del %+v", cmd != nil, m.mode, m.cursor, m.del)
	}
	if v := m.View(); !strings.Contains(v, "eng-6458-bugsink-key-flows has 2 changed files.") {
		t.Errorf("the prompt is not for the row of the plan:\n%s", v)
	}
	// A hidden row: the filter clears so that the cursor can show it.
	m = loaded(t)
	m.cursor = flowsPath
	m, _ = keys(t, m, "d", "/", "m", "o", "n", "o", "enter")
	m, _ = send(t, m, planMsg{wt: m.row(flowsPath).WT, plan: git.DeletePlan{Changed: 2, NeedsYes: true}})
	if m.mode != modeTyped || m.cursorPath() != flowsPath {
		t.Errorf("hidden row: mode %v, cursor %s", m.mode, m.cursorPath())
	}
}

// TestPlanWhileFilterOpen: while the filter input or another typed prompt is
// open, a plan that needs yes cancels. A clean plan deletes.
func TestPlanWhileFilterOpen(t *testing.T) {
	m := loaded(t)
	m.cursor = flowsPath
	m, _ = keys(t, m, "d", "/")
	m2, cmd := send(t, m, planMsg{wt: m.row(flowsPath).WT, plan: git.DeletePlan{Changed: 1, NeedsYes: true}})
	if cmd != nil || m2.mode != modeFilter || inPrompt(m2) || m2.note != cancelledNote || len(m2.jobs) != 0 {
		t.Fatalf("plan with yes in filter mode: cmd %v, mode %v, note %q, jobs %v", cmd != nil, m2.mode, m2.note, m2.jobs)
	}
	m2, cmd = keys(t, m2, "y", "e", "s", "enter")
	if cmd != nil || m2.Result() != (Result{}) {
		t.Error("filter keys start a command")
	}

	m2, cmd = send(t, m, planMsg{wt: m.row(flowsPath).WT})
	if cmd == nil || m2.mode != modeFilter || m2.jobs[flowsPath].state != jobDeleting {
		t.Errorf("clean plan in filter mode: cmd %v, mode %v, jobs %v", cmd != nil, m2.mode, m2.jobs)
	}

	// Another typed prompt is open.
	m = loaded(t)
	m.cursor = flowsPath
	m, _ = keys(t, m, "d", "j", "d")
	m, _ = send(t, m, planMsg{wt: m.row(alertPath).WT, plan: git.DeletePlan{Changed: 1, NeedsYes: true}})
	m2, cmd = send(t, m, planMsg{wt: m.row(flowsPath).WT, plan: git.DeletePlan{Changed: 1, NeedsYes: true}})
	if cmd != nil || m2.del.wt.Path != alertPath || m2.note != cancelledNote {
		t.Errorf("second prompt: cmd %v, prompt for %s, note %q", cmd != nil, m2.del.wt.Path, m2.note)
	}
	if _, ok := m2.jobs[flowsPath]; ok {
		t.Error("the cancelled plan keeps its job")
	}
}

// deleting returns loaded(t) with a clean delete that runs for each path.
// The cursor is on the main row.
func deleting(t *testing.T, paths ...string) Model {
	t.Helper()
	m := loaded(t)
	for _, p := range paths {
		m.cursor = p
		m, _ = keys(t, m, "d")
		var cmd tea.Cmd
		m, cmd = send(t, m, planMsg{wt: m.row(p).WT})
		if cmd == nil || m.jobs[p].state != jobDeleting {
			t.Fatalf("%s: clean plan starts no delete", p)
		}
	}
	m.cursor = mainPath
	return m
}

// TestQuitKeysWhileDeleteRuns: q, esc, enter, and o do not quit while a
// delete runs. ctrl+c quits. A plan alone does not stop a quit.
func TestQuitKeysWhileDeleteRuns(t *testing.T) {
	for _, tc := range []struct {
		paths []string
		note  string
	}{
		{[]string{flowsPath}, "1 delete in progress: wait, or press ctrl+c"},
		{[]string{flowsPath, alertPath}, "2 deletes in progress: wait, or press ctrl+c"},
	} {
		m := deleting(t, tc.paths...)
		for _, k := range []string{"q", "esc", "enter", "o"} {
			m2, cmd := keys(t, m, k)
			if cmd != nil || m2.Result() != (Result{}) || m2.note != tc.note {
				t.Errorf("%d deletes, %s: cmd %v, Result %+v, note %q", len(tc.paths), k, cmd != nil, m2.Result(), m2.note)
			}
		}
		if _, cmd := keys(t, m, "ctrl+c"); !isQuit(cmd) {
			t.Errorf("%d deletes: ctrl+c does not quit", len(tc.paths))
		}
	}

	m := loaded(t)
	m.cursor = flowsPath
	m, _ = keys(t, m, "d", "k")
	if _, cmd := keys(t, m, "q"); !isQuit(cmd) {
		t.Error("q while only a plan runs does not quit")
	}
	if m2, cmd := keys(t, m, "enter"); !isQuit(cmd) || m2.Result().SwitchPath != mainPath {
		t.Error("enter on another row while only a plan runs does not switch")
	}
}

// TestKeysLiveWhileDeleteRuns: while row A is deleted, the other rows work.
func TestKeysLiveWhileDeleteRuns(t *testing.T) {
	m := deleting(t, flowsPath)
	var copied []string
	m.d.Copy = func(s string) error { copied = append(copied, s); return nil }

	m.cursor = flowsPath
	for _, k := range []string{"enter", "o", "d", "D"} {
		m2, cmd := keys(t, m, k)
		if cmd != nil || m2.Result() != (Result{}) || !strings.Contains(m2.note, inProgressNote) {
			t.Errorf("%s on the deleted row: cmd %v, note %q", k, cmd != nil, m2.note)
		}
	}
	m, _ = keys(t, m, "j")
	if m.cursor != alertPath {
		t.Fatalf("j: cursor on %s", m.cursor)
	}
	m2, cmd := keys(t, m, "D")
	if j := m2.jobs[alertPath]; j.state != jobChecking || !j.branch {
		t.Errorf("D on another row: job %+v", j)
	}
	if p := planOf(t, cmd); p.wt.Path != alertPath {
		t.Errorf("the second plan is for %s", p.wt.Path)
	}
	if m2.jobs[flowsPath].state != jobDeleting {
		t.Error("the first delete lost its job")
	}

	m2, cmd = keys(t, m, "c")
	if m2, _ = send(t, m2, run(t, cmd)); len(copied) != 1 || m2.note != "copied eng-6201-onboarding-v2-alerts" {
		t.Errorf("c: copied %v, note %q", copied, m2.note)
	}
	if m2, _ = keys(t, m, "/", "6", "2"); m2.mode != modeFilter || m2.filter != "62" {
		t.Errorf("/: mode %v, filter %q", m2.mode, m2.filter)
	}
	if _, cmd = keys(t, m, "f"); cmd == nil {
		t.Error("f starts no fetch")
	}
	if _, cmd = keys(t, m, "r"); cmd == nil {
		t.Error("r starts no reload")
	}
}

// TestReloadKeepsJobs: a reload keeps the job of each row that stays.
func TestReloadKeepsJobs(t *testing.T) {
	m := deleting(t, flowsPath)
	m.cursor = alertPath
	m, _ = keys(t, m, "d", "r")
	m2, _ := send(t, m, worktreesMsg{wts: threeWorktrees(), base: "origin/master"})
	if m2.jobs[flowsPath].state != jobDeleting || m2.jobs[alertPath].state != jobChecking {
		t.Errorf("jobs after reload: %+v", m2.jobs)
	}
	if l := lineWith(t, m2.View(), "eng-6458"); !strings.Contains(l, "deleting") {
		t.Errorf("row after reload: %q", l)
	}
	m2, _ = send(t, m, worktreesMsg{wts: threeWorktrees()[:2], base: "origin/master"})
	if _, ok := m2.jobs[flowsPath]; ok || m2.jobs[alertPath].state != jobChecking {
		t.Errorf("a row that is gone keeps its job: %+v", m2.jobs)
	}
}

// TestSpinnerTick: a tick advances the frame while a row has a job. With no
// job, no tick command comes back.
func TestSpinnerTick(t *testing.T) {
	m := loaded(t)
	if m2, cmd := send(t, m, tickMsg{}); cmd != nil || m2.spin != m.spin {
		t.Errorf("tick without a job: cmd %v, frame %d", cmd != nil, m2.spin)
	}
	m.cursor = flowsPath
	m, cmd := keys(t, m, "d")
	if !hasTick(cmd) {
		t.Fatal("the first job starts no tick")
	}
	m, _ = keys(t, m, "k")
	for i := range 5 {
		before := m.spin
		m, cmd = send(t, m, tickMsg{})
		if !isTick(cmd) || m.spin != (before+1)%len(spinFrames) {
			t.Fatalf("tick %d: frame %d -> %d, tick cmd %v", i, before, m.spin, isTick(cmd))
		}
		if l := lineWith(t, m.View(), "eng-6458"); !strings.Contains(l, " "+spinFrames[m.spin]+" checking") {
			t.Errorf("tick %d: row %q", i, l)
		}
	}
	// A second job does not start a second tick.
	m.cursor = alertPath
	if _, cmd = keys(t, m, "d"); hasTick(cmd) {
		t.Error("a second job starts a second tick")
	}
	m, _ = send(t, m, planMsg{wt: m.row(flowsPath).WT, err: errors.New("x")})
	if _, cmd = send(t, m, tickMsg{}); cmd != nil {
		t.Error("tick with no job returns a command")
	}
}

// TestJobRowWidth: no line is wider than the terminal while a row is deleted.
func TestJobRowWidth(t *testing.T) {
	old := renderer.ColorProfile()
	renderer.SetColorProfile(termenv.ANSI)
	t.Cleanup(func() { renderer.SetColorProfile(old) })
	const (
		yellow = "\x1b[33m"
		faint  = "\x1b[2m"
	)
	for _, w := range []int{40, 80} {
		for _, cur := range []string{mainPath, alertPath} {
			m := deleting(t, alertPath)
			m.cursor = cur
			m, _ = send(t, m, tea.WindowSizeMsg{Width: w, Height: 20})
			v := m.View()
			for _, l := range strings.Split(v, "\n") {
				if lipgloss.Width(l) > w {
					t.Errorf("width %d: line too wide (%d): %q", w, lipgloss.Width(l), l)
				}
			}
			l := lineWith(t, v, "4 files") // the alert row
			if !strings.Contains(l, "deleting") {
				t.Errorf("width %d: the cut row lost its job: %q", w, l)
			}
			if cur == alertPath {
				if !strings.HasPrefix(l, "\x1b[1;36m") || strings.Contains(l, yellow) || strings.Contains(l, faint) {
					t.Errorf("width %d: cursor row: %q", w, l)
				}
				continue
			}
			if !strings.Contains(l, yellow+" "+spinFrames[m.spin]+" deleting") || !strings.Contains(l, faint) {
				t.Errorf("width %d: job row is not dim with a yellow job: %q", w, l)
			}
		}
	}
}

func TestDelResetAfterDelete(t *testing.T) {
	m := loaded(t)
	m.cursor = flowsPath
	m, _ = keys(t, m, "D")
	m, _ = send(t, m, planMsg{wt: m.row(flowsPath).WT})
	m, _ = send(t, m, removedMsg{path: flowsPath})
	if len(m.jobs) != 0 {
		t.Errorf("job not removed: %+v", m.jobs)
	}
	// The next d after a D does not delete the branch.
	m.cursor = alertPath
	m, _ = keys(t, m, "d")
	m, _ = send(t, m, planMsg{wt: m.row(alertPath).WT})
	if m.jobs[alertPath].branch {
		t.Error("the branch choice of the last D stays")
	}
	m, _ = send(t, m, removedMsg{path: alertPath, err: errors.New("boom")})
	if len(m.jobs) != 0 || m.row(alertPath) == nil || m.note != "delete failed: boom" {
		t.Errorf("failed delete: jobs %v, note %q", m.jobs, m.note)
	}
}

// TestDeleteNestedBlocked: a worktree with another worktree inside its
// folder cannot be deleted. The nested folder is ignored, so it looks clean.
func TestDeleteNestedBlocked(t *testing.T) {
	repo := testrepo.New(t)
	outer := realPath(t, repo.AddWorktree(t, "outer"))
	if err := os.WriteFile(filepath.Join(outer, ".gitignore"), []byte("nested/\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	repo.Git(t, outer, "add", ".gitignore")
	repo.Git(t, outer, "commit", "-m", "ignore nested")
	nested := filepath.Join(outer, "nested")
	repo.Git(t, repo.Dir, "worktree", "add", "-b", "nested", nested)

	m, _ := repoModel(t, repo, repo.Dir, true)
	for _, k := range []string{"d", "D"} {
		m2, cmd := press(t, m, outer, k)
		if cmd != nil {
			t.Errorf("%s on a worktree with a nested worktree starts a command", k)
		}
		if v := m2.View(); !strings.Contains(v, "contains worktree nested: delete it first") || inPrompt(m2) {
			t.Errorf("nested worktree:\n%s", v)
		}
		if _, cmd := keys(t, m2, "y"); cmd != nil {
			t.Error("y starts a command")
		}
	}
	if !exists(outer) || !exists(nested) || !hasBranch(t, repo, "outer") {
		t.Error("a folder or the branch is gone")
	}
}

// TestCtrlCWhileCurrentDeleteRuns: ctrl+c during the delete of the current
// worktree quits with a change to the main worktree. The shell must not
// stay in a deleted directory.
func TestCtrlCWhileCurrentDeleteRuns(t *testing.T) {
	d := deps(t)
	d.Cwd = alertPath
	m := listed(t, d, "origin/master")
	m.cursor = alertPath
	m, _ = keys(t, m, "d")
	m, cmd := send(t, m, planMsg{wt: m.row(alertPath).WT})
	if cmd == nil {
		t.Fatal("clean plan starts no delete")
	}
	m, cmd = keys(t, m, "ctrl+c")
	if !isQuit(cmd) || m.Result() != (Result{SwitchPath: mainPath}) {
		t.Errorf("quit %v, Result %+v; want SwitchPath %s", isQuit(cmd), m.Result(), mainPath)
	}

	// ctrl+c during the delete of another worktree changes nothing.
	m = loaded(t)
	m.cursor = flowsPath
	m, _ = keys(t, m, "d")
	m, _ = send(t, m, planMsg{wt: m.row(flowsPath).WT})
	m, cmd = keys(t, m, "ctrl+c")
	if !isQuit(cmd) || m.Result() != (Result{}) {
		t.Errorf("other worktree: quit %v, Result %+v", isQuit(cmd), m.Result())
	}
}

// TestDeleteDetachedUnreachable: commits that no branch contains need the
// typed yes. A plain y does not delete.
func TestDeleteDetachedUnreachable(t *testing.T) {
	repo := testrepo.New(t)
	det := filepath.Join(filepath.Dir(repo.Dir), "det")
	repo.Git(t, repo.Dir, "worktree", "add", "--detach", det)
	repo.Commit(t, det, "a.txt")
	det = realPath(t, det)
	head := repo.Git(t, det, "rev-parse", "HEAD")
	m, _ := repoModel(t, repo, repo.Dir, true)

	m, cmd := press(t, m, det, "d")
	if cmd != nil {
		t.Fatal("unreachable commits start a command before yes")
	}
	if m.del.plan.Unreachable != 1 || !m.del.plan.NeedsYes || m.mode != modeTyped {
		t.Fatalf("plan %+v, mode %v", m.del.plan, m.mode)
	}
	if v := m.View(); !strings.Contains(v, head[:7]+" has 1 commit that no branch contains.") || !strings.Contains(v, "Type yes to delete: ") {
		t.Fatalf("typed prompt:\n%s", v)
	}
	m2, cmd := keys(t, m, "y")
	if cmd != nil || m2.mode != modeTyped {
		t.Fatalf("y starts a command or leaves the prompt: mode %v", m2.mode)
	}
	if _, cmd = keys(t, m2, "enter"); cmd != nil {
		t.Error("y + enter starts a command")
	}
	if !exists(det) {
		t.Fatal("folder deleted without yes")
	}

	m, cmd = keys(t, m, "y", "e", "s", "enter")
	msg, ok := run(t, cmd).(removedMsg)
	if !ok || msg.err != nil || exists(det) {
		t.Fatalf("removedMsg = %+v, folder exists %v", msg, exists(det))
	}
	m, _ = send(t, m, msg)
	if m.note != "deleted "+head[:7] {
		t.Errorf("note = %q, want %q", m.note, "deleted "+head[:7])
	}
}
