package ui

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/dev-shetty/wrktree/internal/disk"
	"github.com/dev-shetty/wrktree/internal/git"
	"github.com/dev-shetty/wrktree/internal/testrepo"
)

const (
	cleanHint = "y delete · b branch · any other key cancel"
	typedHint = "type yes + enter · tab branch · esc cancel"
	refusal   = "cannot delete the current worktree: run through the wt function"
)

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

// pressD moves the cursor to path, presses d, and runs the plan command.
func pressD(t *testing.T, m Model, path string) Model {
	t.Helper()
	m.cursor = path
	m, cmd := keys(t, m, "d")
	if cmd == nil {
		t.Fatal("d starts no command")
	}
	msg, ok := cmd().(planMsg)
	if !ok {
		t.Fatalf("d command returns %T, want planMsg", msg)
	}
	m, _ = send(t, m, msg)
	return m
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
	v := m.View()
	return strings.Contains(v, cleanHint) || strings.Contains(v, typedHint)
}

func TestDeleteBlockedMain(t *testing.T) {
	m := loaded(t)
	m.cursor = mainPath
	m, cmd := keys(t, m, "d")
	if cmd == nil {
		t.Fatal("d starts no command")
	}
	m, _ = send(t, m, planMsg{wt: m.rows[0].WT, plan: git.DeletePlan{Blocked: "the main worktree cannot be deleted"}})
	if v := m.View(); !strings.Contains(v, "the main worktree cannot be deleted") {
		t.Errorf("no blocked text:\n%s", v)
	}
	if inPrompt(m) {
		t.Error("blocked plan opens a prompt")
	}
	m, cmd = keys(t, m, "y")
	if cmd != nil {
		t.Error("y after a blocked plan starts a command")
	}
}

func TestDeleteBlockedRealMain(t *testing.T) {
	repo := testrepo.New(t)
	repo.AddWorktree(t, "feat-a")
	m, _ := repoModel(t, repo, repo.Dir, true)
	m = pressD(t, m, realPath(t, repo.Dir))
	if !strings.Contains(m.View(), "the main worktree cannot be deleted") || inPrompt(m) {
		t.Errorf("real main worktree:\n%s", m.View())
	}
}

func TestDeletePlanError(t *testing.T) {
	m := loaded(t)
	m.cursor = flowsPath
	m, _ = keys(t, m, "d")
	m, _ = send(t, m, planMsg{wt: m.row(flowsPath).WT, err: errors.New("status failed")})
	if v := m.View(); !strings.Contains(v, "status failed") {
		t.Errorf("no error text:\n%s", v)
	}
	if inPrompt(m) {
		t.Error("a plan error opens a prompt")
	}
	if _, cmd := keys(t, m, "y"); cmd != nil {
		t.Error("y after a plan error starts a command")
	}
}

func TestDeleteCleanConfirm(t *testing.T) {
	for _, k := range []string{"n", "enter", "Y", "x", "esc", "d"} {
		m := loaded(t)
		m.cursor = flowsPath
		m, _ = keys(t, m, "d")
		m, _ = send(t, m, planMsg{wt: m.row(flowsPath).WT})
		v := m.View()
		for _, s := range []string{
			"Delete worktree eng-6458-bugsink-key-flows at " + flowsPath + "?",
			"Ignored files are deleted too", "[ ] also delete the branch", cleanHint,
		} {
			if !strings.Contains(v, s) {
				t.Errorf("prompt lacks %q:\n%s", s, v)
			}
		}
		if strings.Contains(v, keyFooter) {
			t.Error("the prompt must replace the key footer")
		}
		m, cmd := keys(t, m, k)
		if cmd != nil || m.Result() != (Result{}) {
			t.Errorf("%q: cmd %v, Result %+v", k, cmd != nil, m.Result())
		}
		if inPrompt(m) || m.mode != modeList {
			t.Errorf("%q does not cancel:\n%s", k, m.View())
		}
		if !strings.Contains(m.View(), "cancelled") {
			t.Errorf("%q: no cancel text", k)
		}
	}
}

func TestDeleteCleanYes(t *testing.T) {
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

	m = pressD(t, m, featA)
	if !strings.Contains(m.View(), cleanHint) {
		t.Fatalf("no clean prompt:\n%s", m.View())
	}
	m, cmd := keys(t, m, "y")
	if cmd == nil {
		t.Fatal("y starts no command")
	}
	// A second delete cannot start while the first one runs.
	for _, k := range []string{"d", "p", "y"} {
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
	if !strings.Contains(repo.Git(t, repo.Dir, "branch", "--format=%(refname:short)"), "feat-a") {
		t.Error("branch deleted without b")
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
	if !strings.Contains(m.View(), "deleted "+featA) {
		t.Errorf("no deleted text:\n%s", m.View())
	}
	if _, c := keys(t, m, "d"); c == nil {
		t.Error("d does not work after the delete is done")
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

	m = pressD(t, m, feat)
	v := m.View()
	for _, s := range []string{"feat-a has 3 changed files. They will be lost. Ignored files are deleted too.",
		"[ ] also delete the branch", "Type yes to delete: ", typedHint} {
		if !strings.Contains(v, s) {
			t.Errorf("typed prompt lacks %q:\n%s", s, v)
		}
	}
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
		if inPrompt(m2) || m2.mode != modeList {
			t.Errorf("%v does not cancel:\n%s", answer, m2.View())
		}
	}
	if !exists(feat) {
		t.Fatal("folder deleted without yes")
	}

	m, _ = keys(t, m, "y", "e", "s", "s", "backspace")
	if !strings.Contains(m.View(), "Type yes to delete: yes") {
		t.Errorf("typed text does not show:\n%s", m.View())
	}
	m, _ = send(t, m, tea.KeyMsg{Type: tea.KeyTab})
	if !strings.Contains(m.View(), "[x] also delete the branch") {
		t.Errorf("tab does not toggle the branch:\n%s", m.View())
	}
	m, _ = send(t, m, tea.KeyMsg{Type: tea.KeyTab})
	m, cmd := keys(t, m, "enter")
	msg, ok := run(t, cmd).(removedMsg)
	// git refuses a worktree with changes unless the command forces it.
	if !ok || msg.err != nil || exists(feat) {
		t.Fatalf("forced delete: %+v, folder exists %v", msg, exists(feat))
	}
	if !strings.Contains(repo.Git(t, repo.Dir, "branch", "--format=%(refname:short)"), "feat-a") {
		t.Error("branch deleted with the option off")
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
	m, _ = send(t, m, planMsg{wt: wt(t, m, feat)})
	m, cmd := keys(t, m, "y")
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
}

func TestDeleteBranchToggle(t *testing.T) {
	repo := testrepo.New(t)
	feat := realPath(t, repo.AddWorktree(t, "feat-a"))
	m, _ := repoModel(t, repo, repo.Dir, true)
	m = pressD(t, m, feat)
	m, _ = keys(t, m, "b")
	if !strings.Contains(m.View(), "[x] also delete the branch") {
		t.Fatalf("b does not toggle:\n%s", m.View())
	}
	m, _ = keys(t, m, "b")
	if !strings.Contains(m.View(), "[ ] also delete the branch") {
		t.Fatalf("b does not toggle back:\n%s", m.View())
	}
	m, cmd := keys(t, m, "b", "y")
	msg, ok := run(t, cmd).(removedMsg)
	if !ok || msg.err != nil || msg.branchErr != nil {
		t.Fatalf("removedMsg = %+v", msg)
	}
	if exists(feat) {
		t.Error("folder still exists")
	}
	if strings.Contains(repo.Git(t, repo.Dir, "branch", "--format=%(refname:short)"), "feat-a") {
		t.Error("branch still exists")
	}
}

func TestDeleteBranchUnmerged(t *testing.T) {
	repo := testrepo.New(t)
	feat := realPath(t, repo.AddWorktree(t, "feat-a"))
	repo.Commit(t, feat, "a.txt")
	m, _ := repoModel(t, repo, repo.Dir, true)
	m = pressD(t, m, feat)
	m, cmd := keys(t, m, "b", "y")
	msg := run(t, cmd).(removedMsg)
	if msg.err != nil || msg.branchErr == nil {
		t.Fatalf("removedMsg = %+v", msg)
	}
	if exists(feat) {
		t.Error("a branch error must not undo the delete")
	}
	m, _ = send(t, m, msg)
	if m.row(feat) != nil {
		t.Error("row stays after the delete")
	}
	if v := m.View(); !strings.Contains(v, "but the branch stays") {
		t.Errorf("no branch error text:\n%s", v)
	}
}

func TestDeleteDetachedHidesBranch(t *testing.T) {
	m := loaded(t)
	w := m.row(flowsPath).WT
	w.Branch, w.Detached, w.Head = "", true, "0123456789abcdef"
	m.cursor = flowsPath
	m, _ = keys(t, m, "d")
	m, _ = send(t, m, planMsg{wt: w})
	if v := m.View(); strings.Contains(v, "also delete the branch") || !strings.Contains(v, "0123456") {
		t.Errorf("detached prompt:\n%s", v)
	}
	m, cmd := keys(t, m, "b")
	if cmd != nil || inPrompt(m) {
		t.Error("b must cancel the prompt of a detached worktree")
	}
}

func TestDeleteCurrentWithCD(t *testing.T) {
	repo := testrepo.New(t)
	feat := realPath(t, repo.AddWorktree(t, "feat-a"))
	m, _ := repoModel(t, repo, feat, true)
	if r := m.row(feat); r == nil || !r.Current {
		t.Fatal("feat-a is not the current row")
	}
	m = pressD(t, m, feat)
	if !strings.Contains(m.View(), cleanHint) {
		t.Fatalf("no prompt:\n%s", m.View())
	}
	m, cmd := keys(t, m, "b", "y")
	msg := run(t, cmd).(removedMsg)
	if msg.err != nil || msg.branchErr != nil || exists(feat) {
		t.Fatalf("removedMsg = %+v, folder exists %v", msg, exists(feat))
	}
	if strings.Contains(repo.Git(t, repo.Dir, "branch", "--format=%(refname:short)"), "feat-a") {
		t.Error("branch still exists")
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
		d := deps(t)
		d.Cwd = alertPath
		d.CanCD = tc.canCD
		m := New(d)
		m, _ = send(t, m, tea.WindowSizeMsg{Width: 120, Height: 30})
		m, _ = send(t, m, worktreesMsg{wts: tc.wts(), base: "origin/master"})
		m.cursor = alertPath
		m, _ = keys(t, m, "d")
		m, _ = send(t, m, planMsg{wt: m.row(alertPath).WT})
		if v := m.View(); !strings.Contains(v, refusal) || inPrompt(m) {
			t.Errorf("%s:\n%s", tc.name, v)
		}
		if _, cmd := keys(t, m, "y"); cmd != nil {
			t.Errorf("%s: y starts a command", tc.name)
		}
	}
}

func TestDeletePromptWidth(t *testing.T) {
	for _, w := range []int{30, 50} {
		m := loaded(t)
		m, _ = send(t, m, tea.WindowSizeMsg{Width: w, Height: 20})
		m.cursor = alertPath
		m, _ = keys(t, m, "d")
		for _, p := range []git.DeletePlan{{}, {Changed: 4, NeedsYes: true}} {
			m2, _ := send(t, m, planMsg{wt: m.row(alertPath).WT, plan: p})
			v := m2.View()
			for _, l := range strings.Split(v, "\n") {
				if lipgloss.Width(l) > w {
					t.Errorf("width %d: line too wide (%d): %q", w, lipgloss.Width(l), l)
				}
			}
			flat := strings.Join(strings.Fields(v), " ")
			if !strings.Contains(flat, "Ignored files are deleted too.") {
				t.Errorf("width %d: prompt text is lost:\n%s", w, v)
			}
		}
	}
}

func TestPruneNothing(t *testing.T) {
	m := loaded(t)
	m, cmd := keys(t, m, "p")
	if cmd == nil {
		t.Fatal("p starts no command")
	}
	m, _ = send(t, m, pruneListMsg{})
	if v := m.View(); !strings.Contains(v, "nothing to prune") || strings.Contains(v, "Prune ") {
		t.Errorf("view:\n%s", v)
	}
	m, _ = keys(t, m, "p")
	m, _ = send(t, m, pruneListMsg{err: errors.New("prune list failed")})
	if v := m.View(); !strings.Contains(v, "prune list failed") {
		t.Errorf("no error text:\n%s", v)
	}
}

func TestPruneConfirm(t *testing.T) {
	repo := testrepo.New(t)
	feat := realPath(t, repo.AddWorktree(t, "feat-a"))
	if err := os.RemoveAll(feat); err != nil {
		t.Fatal(err)
	}
	m, _ := repoModel(t, repo, repo.Dir, true)

	m, cmd := keys(t, m, "p")
	list, ok := run(t, cmd).(pruneListMsg)
	if !ok || list.err != nil || len(list.lines) != 1 {
		t.Fatalf("pruneListMsg = %+v", list)
	}
	m, _ = send(t, m, list)
	v := m.View()
	if !strings.Contains(v, "Prune 1 entries? y/N") || !strings.Contains(v, list.lines[0]) {
		t.Fatalf("prune prompt:\n%s", v)
	}
	m2, cmd := keys(t, m, "n")
	if cmd != nil || strings.Contains(m2.View(), "Prune 1 entries") {
		t.Error("n does not cancel the prune")
	}

	m, cmd = keys(t, m, "y")
	if _, c := keys(t, m, "p"); c != nil {
		t.Error("p while a prune runs starts a command")
	}
	pruned, ok := run(t, cmd).(prunedMsg)
	if !ok || pruned.err != nil || pruned.n != 1 {
		t.Fatalf("prunedMsg = %+v", pruned)
	}
	if out := repo.Git(t, repo.Dir, "worktree", "list", "--porcelain"); strings.Contains(out, feat) {
		t.Errorf("entry still listed:\n%s", out)
	}
	m, cmd = send(t, m, pruned)
	if !strings.Contains(m.View(), "pruned 1 entries") {
		t.Errorf("no pruned text:\n%s", m.View())
	}
	reload, ok := run(t, cmd).(worktreesMsg)
	if !ok || reload.err != nil || len(reload.wts) != 1 {
		t.Fatalf("reload = %+v", reload)
	}
	m, _ = send(t, m, reload)
	if len(m.rows) != 1 {
		t.Errorf("rows after prune: %d", len(m.rows))
	}
}

// TestKeysWaitForPlan: between d and the prompt, no key can change the
// mode or move the cursor. A filter key must never answer a prompt.
func TestKeysWaitForPlan(t *testing.T) {
	m := loaded(t)
	m.cursor = flowsPath
	m, _ = keys(t, m, "d")
	for _, k := range []string{"/", "y", "e", "s", "enter", "o", "f", "r", "p", "d", "j", "k", "up", "down"} {
		var cmd tea.Cmd
		m, cmd = keys(t, m, k)
		if cmd != nil {
			t.Errorf("%s while a plan runs starts a command", k)
		}
	}
	if m.mode != modeList || m.filter != "" || m.cursor != flowsPath || m.Result() != (Result{}) {
		t.Fatalf("keys changed the state: mode %v, filter %q, cursor %s", m.mode, m.filter, m.cursor)
	}
	m, _ = send(t, m, planMsg{wt: m.row(flowsPath).WT})
	if !strings.Contains(m.View(), cleanHint) {
		t.Fatalf("prompt does not show:\n%s", m.View())
	}
	if _, cmd := keys(t, m, "y"); cmd == nil {
		t.Error("y on the visible prompt does not delete")
	}
}

func TestQuitWhilePlanRuns(t *testing.T) {
	for _, k := range []string{"q", "esc", "ctrl+c"} {
		m := loaded(t)
		m, _ = keys(t, m, "d")
		if _, cmd := keys(t, m, k); !isQuit(cmd) {
			t.Errorf("%s while a plan runs does not quit", k)
		}
		m = loaded(t)
		m, _ = keys(t, m, "p")
		if _, cmd := keys(t, m, k); !isQuit(cmd) {
			t.Errorf("%s while a prune list runs does not quit", k)
		}
	}
}

func TestPlanForOtherRowCancels(t *testing.T) {
	m := loaded(t)
	m.cursor = flowsPath
	m, _ = send(t, m, planMsg{wt: m.row(alertPath).WT})
	if inPrompt(m) || m.mode != modeList || !strings.Contains(m.View(), "cancelled") {
		t.Errorf("plan for another row opens a prompt:\n%s", m.View())
	}
	if _, cmd := keys(t, m, "y"); cmd != nil {
		t.Error("y starts a command")
	}
}

func TestPlanInFilterModeCancels(t *testing.T) {
	m := loaded(t)
	m.cursor = flowsPath
	m.mode = modeFilter
	m, _ = send(t, m, planMsg{wt: m.row(flowsPath).WT, plan: git.DeletePlan{Changed: 1, NeedsYes: true}})
	if m.mode != modeFilter || inPrompt(m) {
		t.Fatalf("plan in filter mode opens a prompt:\n%s", m.View())
	}
	m, cmd := keys(t, m, "y", "e", "s", "enter")
	if cmd != nil || m.Result() != (Result{}) {
		t.Error("filter keys start a command")
	}
}

func TestPruneKeysWaitForList(t *testing.T) {
	m := loaded(t)
	m, _ = keys(t, m, "p")
	m, cmd := keys(t, m, "/")
	if cmd != nil || m.mode != modeList {
		t.Fatalf("/ while a prune list runs: mode %v", m.mode)
	}
	m, _ = send(t, m, pruneListMsg{lines: []string{"Removing worktrees/x: gitdir file points to non-existent location"}})
	if !strings.Contains(m.View(), "Prune 1 entries? y/N") {
		t.Fatalf("no prune prompt:\n%s", m.View())
	}
	if _, cmd := keys(t, m, "x"); cmd != nil {
		t.Error("x prunes")
	}

	m = loaded(t)
	m.mode = modeFilter
	m, _ = send(t, m, pruneListMsg{lines: []string{"Removing worktrees/x"}})
	if m.mode != modeFilter || strings.Contains(m.View(), "Prune 1 entries") {
		t.Fatalf("prune list in filter mode opens a prompt:\n%s", m.View())
	}
	if _, cmd := keys(t, m, "y"); cmd != nil {
		t.Error("filter y prunes")
	}
}

func TestQuitBlockedWhileDeleteRuns(t *testing.T) {
	m := loaded(t)
	m.cursor = flowsPath
	m, _ = keys(t, m, "d")
	m, _ = send(t, m, planMsg{wt: m.row(flowsPath).WT})
	m, cmd := keys(t, m, "y")
	if cmd == nil {
		t.Fatal("y starts no delete")
	}
	for _, k := range []string{"q", "esc", "r", "enter"} {
		if _, cmd := keys(t, m, k); cmd != nil {
			t.Errorf("%s while a delete runs returns a command", k)
		}
	}
	if _, cmd := keys(t, m, "ctrl+c"); !isQuit(cmd) {
		t.Error("ctrl+c while a delete runs does not quit")
	}
	m, _ = send(t, m, removedMsg{path: flowsPath, err: errors.New("boom")})
	if m.del != (deleteState{}) {
		t.Errorf("del not reset after a failed delete: %+v", m.del)
	}
}

func TestQuitBlockedWhilePruneRuns(t *testing.T) {
	m := loaded(t)
	m, _ = keys(t, m, "p")
	m, _ = send(t, m, pruneListMsg{lines: []string{"Removing worktrees/x"}})
	m, cmd := keys(t, m, "y")
	if cmd == nil {
		t.Fatal("y starts no prune")
	}
	for _, k := range []string{"q", "esc", "r"} {
		if _, cmd := keys(t, m, k); cmd != nil {
			t.Errorf("%s while a prune runs returns a command", k)
		}
	}
	if _, cmd := keys(t, m, "ctrl+c"); !isQuit(cmd) {
		t.Error("ctrl+c while a prune runs does not quit")
	}
}

func TestDelResetAfterDelete(t *testing.T) {
	m := loaded(t)
	m.cursor = flowsPath
	m, _ = keys(t, m, "d")
	m, _ = send(t, m, planMsg{wt: m.row(flowsPath).WT})
	m, _ = keys(t, m, "y")
	m, _ = send(t, m, removedMsg{path: flowsPath})
	if m.del != (deleteState{}) {
		t.Errorf("del not reset: %+v", m.del)
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
	m = pressD(t, m, outer)
	if v := m.View(); !strings.Contains(v, "contains worktree nested: delete it first") || inPrompt(m) {
		t.Errorf("nested worktree:\n%s", v)
	}
	if _, cmd := keys(t, m, "y"); cmd != nil {
		t.Error("y starts a command")
	}
	if !exists(outer) || !exists(nested) {
		t.Error("a folder is gone")
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
	m, _ = send(t, m, planMsg{wt: m.row(alertPath).WT})
	m, cmd := keys(t, m, "y")
	if cmd == nil {
		t.Fatal("y starts no delete")
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
	m, _ = keys(t, m, "y")
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

	m = pressD(t, m, det)
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
}
