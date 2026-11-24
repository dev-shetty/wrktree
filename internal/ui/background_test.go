package ui

import (
	"reflect"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/dev-shetty/wrktree/internal/testrepo"
)

// isTick reports whether cmd is the spinner tick. spinTick is not inlined,
// so each tick command has the same code.
func isTick(cmd tea.Cmd) bool {
	return cmd != nil && reflect.ValueOf(cmd).Pointer() == reflect.ValueOf(spinTick()).Pointer()
}

// msgs runs cmd and each command of a batch, but not the spinner tick, and
// returns their messages.
func msgs(cmd tea.Cmd) []tea.Msg {
	if cmd == nil || isTick(cmd) {
		return nil
	}
	msg := cmd()
	b, ok := msg.(tea.BatchMsg)
	if !ok {
		return []tea.Msg{msg}
	}
	var out []tea.Msg
	for _, c := range b {
		out = append(out, msgs(c)...)
	}
	return out
}

// hasTick reports whether cmd is the spinner tick or a batch with it. It
// runs cmd to see a batch: use it only on a command that has no write step.
func hasTick(cmd tea.Cmd) bool {
	if cmd == nil {
		return false
	}
	if isTick(cmd) {
		return true
	}
	b, ok := cmd().(tea.BatchMsg)
	if !ok {
		return false
	}
	for _, c := range b {
		if hasTick(c) {
			return true
		}
	}
	return false
}

// TestDeleteInBackground: d on a clean row gives the row a checking job,
// and the cursor still moves. The plan turns the job into a delete that
// runs in the background, and the row shows a spinner and "deleting".
func TestDeleteInBackground(t *testing.T) {
	repo := testrepo.New(t)
	featA := realPath(t, repo.AddWorktree(t, "feat-a"))
	featB := realPath(t, repo.AddWorktree(t, "feat-b"))
	m, d := repoModel(t, repo, repo.Dir, true)
	d.Cache.Set(d.Repo, featA, 1024, now)

	m.cursor = featA
	m, cmd := keys(t, m, "d")
	if j, ok := m.jobs[featA]; !ok || j.state != jobChecking {
		t.Fatalf("job after d = %+v, %v; want checking", j, ok)
	}
	if l := lineWith(t, m.View(), "feat-a"); !strings.Contains(l, " checking") {
		t.Errorf("row while the plan runs: %q", l)
	}
	if strings.Contains(m.View(), "checking "+featA) {
		t.Errorf("the footer shows the old checking text:\n%s", m.View())
	}
	plan := planOf(t, cmd)
	m, _ = keys(t, m, "j")
	if m.cursor == featA {
		t.Fatal("j does not move the cursor while the plan runs")
	}
	m, _ = keys(t, m, "k")

	m, cmd = send(t, m, plan)
	if j := m.jobs[featA]; j.state != jobDeleting {
		t.Fatalf("job after the plan = %+v; want deleting", j)
	}
	if cmd == nil || inPrompt(m) {
		t.Fatalf("a clean plan starts no delete:\n%s", m.View())
	}
	m.cursor = featB // the job row has no cursor style
	l := lineWith(t, m.View(), "feat-a")
	found := false
	for _, f := range spinFrames {
		found = found || strings.Contains(l, " "+f+" deleting")
	}
	if !found {
		t.Errorf("row does not show a spinner and deleting: %q", l)
	}

	msg, ok := run(t, cmd).(removedMsg)
	if !ok || msg.err != nil || msg.path != featA {
		t.Fatalf("removedMsg = %+v", msg)
	}
	m, cmd = send(t, m, msg)
	if isQuit(cmd) || m.row(featA) != nil || len(m.jobs) != 0 || m.note != "deleted feat-a" {
		t.Errorf("after the delete: quit %v, jobs %v, note %q", isQuit(cmd), m.jobs, m.note)
	}
	if _, ok := d.Cache.Get(d.Repo, featA); ok {
		t.Error("cache keeps the entry")
	}
	if exists(featA) || !exists(featB) {
		t.Errorf("folders: feat-a %v, feat-b %v", exists(featA), exists(featB))
	}
}

// TestCleanPlanAfterCursorMoved: the plan deletes the row of the d key,
// and only that row, wherever the cursor is now.
func TestCleanPlanAfterCursorMoved(t *testing.T) {
	repo := testrepo.New(t)
	featA := realPath(t, repo.AddWorktree(t, "feat-a"))
	featB := realPath(t, repo.AddWorktree(t, "feat-b"))
	m, _ := repoModel(t, repo, repo.Dir, true)
	m.cursor = featA
	m, cmd := keys(t, m, "d")
	m.cursor = featB
	m, _ = keys(t, m, "k", "j") // moves with keys, back to feat-b
	if m.cursor != featB {
		t.Fatalf("cursor on %s", m.cursor)
	}
	m, cmd = send(t, m, planOf(t, cmd))
	if cmd == nil || m.cursor != featB || len(m.jobs) != 1 || m.jobs[featA].state != jobDeleting {
		t.Fatalf("cmd %v, cursor %s, jobs %+v", cmd != nil, m.cursor, m.jobs)
	}
	m, _ = send(t, m, run(t, cmd))
	if exists(featA) || !exists(featB) || m.row(featB) == nil || m.cursor != featB {
		t.Errorf("feat-a %v, feat-b %v, cursor %s", exists(featA), exists(featB), m.cursor)
	}
}

// TestTwoDeletesAtOnce: a second delete starts while the first one runs.
func TestTwoDeletesAtOnce(t *testing.T) {
	repo := testrepo.New(t)
	featA := realPath(t, repo.AddWorktree(t, "feat-a"))
	featB := realPath(t, repo.AddWorktree(t, "feat-b"))
	m, _ := repoModel(t, repo, repo.Dir, true)
	m, delA := press(t, m, featA, "d")
	m, delB := press(t, m, featB, "D")
	if delA == nil || delB == nil || m.jobs[featA].state != jobDeleting || m.jobs[featB].state != jobDeleting {
		t.Fatalf("jobs %+v", m.jobs)
	}
	m, _ = send(t, m, run(t, delB))
	if m.jobs[featA].state != jobDeleting || m.note != "deleted feat-b and its branch" {
		t.Errorf("after delete B: jobs %+v, note %q", m.jobs, m.note)
	}
	m, _ = send(t, m, run(t, delA))
	if len(m.jobs) != 0 || exists(featA) || exists(featB) || hasBranch(t, repo, "feat-b") || !hasBranch(t, repo, "feat-a") {
		t.Errorf("jobs %v, folders %v %v", m.jobs, exists(featA), exists(featB))
	}
}

// TestDeleteCurrentWaitsForOthers: after the delete of the current worktree,
// the screen quits only when the last other delete is done.
func TestDeleteCurrentWaitsForOthers(t *testing.T) {
	repo := testrepo.New(t)
	cur := realPath(t, repo.AddWorktree(t, "feat-cur"))
	other := realPath(t, repo.AddWorktree(t, "feat-other"))
	m, _ := repoModel(t, repo, cur, true)
	main := realPath(t, repo.Dir)
	m, delOther := press(t, m, other, "d")
	m, delCur := press(t, m, cur, "d")
	if delOther == nil || delCur == nil {
		t.Fatal("a clean plan starts no delete")
	}
	if m2, cmd := keys(t, m, "ctrl+c"); !isQuit(cmd) || m2.Result() != (Result{SwitchPath: main}) {
		t.Errorf("ctrl+c while the current worktree is deleted: Result %+v", m2.Result())
	}

	m, cmd := send(t, m, run(t, delCur))
	if exists(cur) {
		t.Fatal("current folder still exists")
	}
	if isQuit(cmd) || m.Result() != (Result{}) {
		t.Fatalf("quit before the other delete is done: Result %+v", m.Result())
	}
	if m2, cmd := keys(t, m, "q"); cmd != nil || m2.note != "1 delete in progress: wait, or press ctrl+c" {
		t.Errorf("q while the other delete runs: note %q", m2.note)
	}
	if m2, cmd := keys(t, m, "ctrl+c"); !isQuit(cmd) || m2.Result().SwitchPath != main {
		t.Errorf("ctrl+c after the current delete: Result %+v", m2.Result())
	}
	m, cmd = send(t, m, run(t, delOther))
	if !isQuit(cmd) || m.Result() != (Result{SwitchPath: main}) {
		t.Errorf("after the last delete: quit %v, Result %+v", isQuit(cmd), m.Result())
	}
}
