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
	msg, ok := cmd().(planMsg)
	if !ok {
		t.Fatalf("%s command returns %T, want planMsg", k, msg)
	}
	return send(t, m, msg)
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
		if cmd != nil || !strings.Contains(m2.View(), "worktree is locked: in use") || inPrompt(m2) {
			t.Errorf("%s on a locked worktree: cmd %v\n%s", k, cmd != nil, m2.View())
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
	if !strings.Contains(m.View(), "deleting "+featA) {
		t.Errorf("no deleting text:\n%s", m.View())
	}
	// A second delete cannot start while the first one runs.
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
	if cmd == nil || m2.del.branch {
		t.Errorf("clean detached D: cmd %v, branch %v", cmd != nil, m2.del.branch)
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
			if v := m.View(); !strings.Contains(v, refusal) || inPrompt(m) {
				t.Errorf("%s:\n%s", tc.name, v)
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

// TestKeysWaitForPlan: between d and the plan, no key can change the mode,
// move the cursor, or change the branch choice. A key typed early never
// answers a prompt.
func TestKeysWaitForPlan(t *testing.T) {
	early := []string{"/", "y", "e", "s", "enter", "o", "f", "r", "p", "d", "D", "b", "j", "k", "up", "down"}
	m := loaded(t)
	m.cursor = flowsPath
	m, _ = keys(t, m, "d")
	for _, k := range early {
		var cmd tea.Cmd
		m, cmd = keys(t, m, k)
		if cmd != nil {
			t.Errorf("%s while a plan runs starts a command", k)
		}
	}
	m, _ = send(t, m, tea.KeyMsg{Type: tea.KeyTab})
	if m.mode != modeList || m.filter != "" || m.cursor != flowsPath || m.Result() != (Result{}) {
		t.Fatalf("keys changed the state: mode %v, filter %q, cursor %s", m.mode, m.filter, m.cursor)
	}
	m2, cmd := send(t, m, planMsg{wt: m.row(flowsPath).WT})
	if cmd == nil || inPrompt(m2) {
		t.Fatalf("clean plan does not start the delete:\n%s", m2.View())
	}
	if m2.del.branch {
		t.Error("D typed while the plan runs selects the branch")
	}

	// The typed prompt starts empty: early keys are not in the answer.
	m2, cmd = send(t, m, planMsg{wt: m.row(flowsPath).WT, plan: git.DeletePlan{Changed: 2, NeedsYes: true}})
	if cmd != nil || !inPrompt(m2) || m2.typed != "" || m2.del.branch {
		t.Fatalf("typed prompt after early keys: cmd %v, typed %q, branch %v", cmd != nil, m2.typed, m2.del.branch)
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
		m := loaded(t)
		m, _ = keys(t, m, "p")
		if _, cmd := keys(t, m, k); !isQuit(cmd) {
			t.Errorf("%s while a prune list runs does not quit", k)
		}
	}
}

func TestPlanForOtherRowCancels(t *testing.T) {
	for _, plan := range []git.DeletePlan{{}, {Changed: 1, NeedsYes: true}} {
		m := loaded(t)
		m.cursor = flowsPath
		m, cmd := send(t, m, planMsg{wt: m.row(alertPath).WT, plan: plan})
		if cmd != nil {
			t.Error("plan for another row starts a command")
		}
		if inPrompt(m) || m.mode != modeList || !strings.Contains(m.View(), "cancelled") {
			t.Errorf("plan for another row opens a prompt:\n%s", m.View())
		}
		if _, cmd := keys(t, m, "y"); cmd != nil {
			t.Error("y starts a command")
		}
	}
}

func TestPlanInFilterModeCancels(t *testing.T) {
	for _, plan := range []git.DeletePlan{{}, {Changed: 1, NeedsYes: true}} {
		m := loaded(t)
		m.cursor = flowsPath
		m.mode = modeFilter
		m, cmd := send(t, m, planMsg{wt: m.row(flowsPath).WT, plan: plan})
		if cmd != nil {
			t.Error("plan in filter mode starts a command")
		}
		if m.mode != modeFilter || inPrompt(m) {
			t.Fatalf("plan in filter mode opens a prompt:\n%s", m.View())
		}
		m, cmd = keys(t, m, "y", "e", "s", "enter")
		if cmd != nil || m.Result() != (Result{}) {
			t.Error("filter keys start a command")
		}
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
	m, cmd := send(t, m, planMsg{wt: m.row(flowsPath).WT})
	if cmd == nil {
		t.Fatal("clean plan starts no delete")
	}
	for _, k := range []string{"q", "esc", "r", "enter", "y", "d", "D"} {
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
	m, _ = keys(t, m, "D")
	m, _ = send(t, m, planMsg{wt: m.row(flowsPath).WT})
	m, _ = send(t, m, removedMsg{path: flowsPath})
	if m.del != (deleteState{}) {
		t.Errorf("del not reset: %+v", m.del)
	}
	// The next d after a D does not delete the branch.
	m.cursor = alertPath
	m, _ = keys(t, m, "d")
	m, _ = send(t, m, planMsg{wt: m.row(alertPath).WT})
	if m.del.branch {
		t.Error("the branch choice of the last D stays")
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
