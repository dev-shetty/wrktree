package ui

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/dev-shetty/wrktree/internal/config"
	"github.com/dev-shetty/wrktree/internal/disk"
	"github.com/dev-shetty/wrktree/internal/git"
	"github.com/dev-shetty/wrktree/internal/status"
	"github.com/dev-shetty/wrktree/internal/testrepo"
)

// TestMain isolates every git call from the git setup of the developer.
func TestMain(m *testing.M) {
	for _, kv := range os.Environ() {
		if k, _, _ := strings.Cut(kv, "="); strings.HasPrefix(k, "GIT_") || k == "WRKTREE_CD_FILE" {
			os.Unsetenv(k)
		}
	}
	for k, v := range map[string]string{
		"GIT_CONFIG_GLOBAL":   "/dev/null",
		"GIT_CONFIG_SYSTEM":   "/dev/null",
		"GIT_AUTHOR_NAME":     "Test",
		"GIT_AUTHOR_EMAIL":    "test@example.com",
		"GIT_COMMITTER_NAME":  "Test",
		"GIT_COMMITTER_EMAIL": "test@example.com",
	} {
		os.Setenv(k, v)
	}
	os.Exit(m.Run())
}

const (
	repoPath  = "/r/mono"
	mainPath  = "/r/mono"
	alertPath = "/r/eng-6201-onboarding-v2-alerts"
	flowsPath = "/r/eng-6458-bugsink-key-flows"
)

var now = time.Date(2026, 11, 24, 12, 0, 0, 0, time.UTC)

func threeWorktrees() []git.Worktree {
	return []git.Worktree{
		{Path: mainPath, Branch: "master", IsMain: true},
		{Path: alertPath, Branch: "eng-6201-onboarding-v2-alerts"},
		{Path: flowsPath, Branch: "eng-6458-bugsink-key-flows"},
	}
}

func deps(t *testing.T) Deps {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(t.TempDir(), "config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(t.TempDir(), "cache"))
	return Deps{
		// An empty folder that is not a repository: a git command of a test
		// with fake worktrees fails and never reads a real repository.
		Git:   git.Runner{Dir: t.TempDir()},
		Cfg:   config.Default(),
		Cache: disk.LoadCache(cachePath()),
		Repo:  repoPath,
		Name:  "mono",
		Cwd:   mainPath,
		CanCD: true,
		Now:   func() time.Time { return now },
	}
}

// cachePath is the cache file of the test. deps sets XDG_CACHE_HOME first.
func cachePath() string { return filepath.Join(os.Getenv("XDG_CACHE_HOME"), "sizes.json") }

func send(t *testing.T, m Model, msg tea.Msg) (Model, tea.Cmd) {
	t.Helper()
	next, cmd := m.Update(msg)
	nm, ok := next.(Model)
	if !ok {
		t.Fatalf("Update returned %T", next)
	}
	return nm, cmd
}

func key(s string) tea.KeyMsg {
	switch s {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "up":
		return tea.KeyMsg{Type: tea.KeyUp}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	case "backspace":
		return tea.KeyMsg{Type: tea.KeyBackspace}
	case "ctrl+c":
		return tea.KeyMsg{Type: tea.KeyCtrlC}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

func keys(t *testing.T, m Model, ks ...string) (Model, tea.Cmd) {
	t.Helper()
	var cmd tea.Cmd
	for _, k := range ks {
		m, cmd = send(t, m, key(k))
	}
	return m, cmd
}

func isQuit(cmd tea.Cmd) bool {
	if cmd == nil || isTick(cmd) {
		return false
	}
	_, ok := cmd().(tea.QuitMsg)
	return ok
}

// sizePlanned reports if a size command runs or waits for path.
func sizePlanned(m Model, path string) bool {
	if m.sizeBusy[path] {
		return true
	}
	for _, p := range m.sizeWait {
		if p == path {
			return true
		}
	}
	return false
}

// listed loads the 3 worktrees with base origin/master into d.
func listed(t *testing.T, d Deps, base string) Model {
	t.Helper()
	m := New(d)
	m, _ = send(t, m, tea.WindowSizeMsg{Width: 120, Height: 30})
	m, _ = send(t, m, worktreesMsg{wts: threeWorktrees(), base: base})
	return m
}

// loaded returns a model with all values in place.
func loaded(t *testing.T) Model {
	t.Helper()
	m := listed(t, deps(t), "origin/master")
	values := []tea.Msg{
		changedMsg{path: mainPath, n: 0},
		changedMsg{path: alertPath, n: 4},
		changedMsg{path: flowsPath, n: 0},
		distMsg{path: mainPath, ahead: 0, behind: 3},
		distMsg{path: alertPath, ahead: 2, behind: 31},
		distMsg{path: flowsPath, ahead: 0, behind: 12},
		commitMsg{path: mainPath, t: now.Add(-2 * time.Hour)},
		commitMsg{path: alertPath, t: now.Add(-21 * 24 * time.Hour)},
		commitMsg{path: flowsPath, t: now.Add(-9 * 24 * time.Hour)},
		sizeMsg{path: mainPath, bytes: 3 << 30},
		sizeMsg{path: alertPath, bytes: 1503238554},
		sizeMsg{path: flowsPath, bytes: 1288490189},
		fetchMsg{},
	}
	for _, v := range values {
		m, _ = send(t, m, v)
	}
	return m
}

func lineWith(t *testing.T, view, s string) string {
	t.Helper()
	for _, l := range strings.Split(view, "\n") {
		if strings.Contains(l, s) {
			return l
		}
	}
	t.Fatalf("no line contains %q:\n%s", s, view)
	return ""
}

func TestViewShowsRows(t *testing.T) {
	v := loaded(t).View()
	for _, s := range []string{"master", "eng-6201-onboarding-v2-alerts", "eng-6458-bugsink-key-flows",
		"origin/master", "4 files", "↑2 ↓31", "3 worktrees", "wrktree · mono", "1.4 GB", "3 weeks ago",
		" enter switch · o open · c copy · s sync · d delete · D delete + branch · p prune · f fetch · / filter · q quit"} {
		if !strings.Contains(v, s) {
			t.Errorf("view lacks %q:\n%s", s, v)
		}
	}
	h := lineWith(t, v, "BRANCH")
	order := []string{"BRANCH", "CHANGES", "origin/master", "SIZE", "LAST COMMIT"}
	for i := 1; i < len(order); i++ {
		if strings.Index(h, order[i-1]) > strings.Index(h, order[i]) {
			t.Errorf("header order wrong: %q", h)
		}
	}
	if l := lineWith(t, v, "eng-6201"); !strings.Contains(l, "1.4 GB") || strings.Contains(l, "~") {
		t.Errorf("fresh size row: %q", l)
	}
}

func TestMarks(t *testing.T) {
	m := loaded(t)
	v := m.View()
	if l := lineWith(t, v, "master (mono)"); !strings.HasPrefix(l, " › ") {
		t.Errorf("cursor on the current row must show ›: %q", l)
	}
	m, _ = keys(t, m, "j")
	v = m.View()
	if l := lineWith(t, v, "master (mono)"); !strings.HasPrefix(l, " ● ") {
		t.Errorf("current row: %q", l)
	}
	// Row 1 is the newest branch, eng-6458.
	if l := lineWith(t, v, "eng-6458"); !strings.HasPrefix(l, " › ") {
		t.Errorf("cursor row: %q", l)
	}
}

func TestTitleTotal(t *testing.T) {
	m := loaded(t)
	title := strings.Split(m.View(), "\n")[0]
	want := disk.Human(3<<30 + 1503238554 + 1288490189)
	if !strings.HasPrefix(title, " wrktree · mono") || !strings.HasSuffix(title, "3 worktrees · "+want) {
		t.Errorf("title = %q, want total %s", title, want)
	}
	if want := 120 - rightMargin; lipgloss.Width(title) != want {
		t.Errorf("title width %d, want %d", lipgloss.Width(title), want)
	}
}

func TestTitleTotalApprox(t *testing.T) {
	d := deps(t)
	d.Cache.Set(repoPath, mainPath, 2<<30, now.Add(-time.Hour))
	m := listed(t, d, "origin/master")
	title := strings.Split(m.View(), "\n")[0]
	if !strings.HasSuffix(title, "3 worktrees · ~2.0 GB") {
		t.Errorf("title = %q", title)
	}
}

func TestPendingCells(t *testing.T) {
	m := listed(t, deps(t), "origin/master")
	if l := lineWith(t, m.View(), "eng-6458"); !strings.Contains(l, "…") {
		t.Errorf("pending row has no …: %q", l)
	}
	if !sizePlanned(m, flowsPath) {
		t.Error("no size command for a path without a cache entry")
	}
	m, _ = send(t, m, sizeMsg{path: flowsPath, err: errors.New("du failed")})
	m, _ = send(t, m, changedMsg{path: flowsPath, err: errors.New("x")})
	m, _ = send(t, m, distMsg{path: flowsPath, err: errors.New("x")})
	m, _ = send(t, m, commitMsg{path: flowsPath, err: errors.New("x")})
	if l := lineWith(t, m.View(), "eng-6458"); strings.Contains(l, "…") || !strings.Contains(l, "—") {
		t.Errorf("failed values must show —: %q", l)
	}
}

func TestCachedSizeNoDu(t *testing.T) {
	d := deps(t)
	d.Cache.Set(repoPath, alertPath, 1503238554, now.Add(-time.Hour))
	m := listed(t, d, "origin/master")
	if l := lineWith(t, m.View(), "eng-6201"); !strings.Contains(l, "~1.4 GB") {
		t.Errorf("cached size row: %q", l)
	}
	if sizePlanned(m, alertPath) {
		t.Error("size command planned for a valid cache entry")
	}
	if !sizePlanned(m, flowsPath) {
		t.Error("no size command for a path without a cache entry")
	}
}

func TestStaleSizeRunsDu(t *testing.T) {
	d := deps(t)
	d.Cache.Set(repoPath, alertPath, 1503238554, now.Add(-25*time.Hour))
	m := listed(t, d, "origin/master")
	if l := lineWith(t, m.View(), "eng-6201"); !strings.Contains(l, "~1.4 GB") {
		t.Errorf("stale size row: %q", l)
	}
	if !sizePlanned(m, alertPath) {
		t.Fatal("no size command for a stale cache entry")
	}
	m, _ = send(t, m, sizeMsg{path: alertPath, bytes: 1288490189})
	if l := lineWith(t, m.View(), "eng-6201"); strings.Contains(l, "~") || !strings.Contains(l, "1.2 GB") {
		t.Errorf("row after du: %q", l)
	}
	e, ok := d.Cache.Get(repoPath, alertPath)
	if !ok || !e.At.Equal(now) || e.Bytes != 1288490189 {
		t.Errorf("cache entry = %+v, %v", e, ok)
	}
	if e, ok := disk.LoadCache(cachePath()).Get(repoPath, alertPath); !ok || e.Bytes != 1288490189 {
		t.Errorf("saved cache entry = %+v, %v", e, ok)
	}
}

func TestSizeLimit(t *testing.T) {
	d := deps(t)
	m := New(d)
	var wts []git.Worktree
	for i := range 7 {
		wts = append(wts, git.Worktree{Path: filepath.Join("/r", string(rune('a'+i))), Branch: string(rune('a' + i))})
	}
	m, _ = send(t, m, worktreesMsg{wts: wts, base: "origin/master"})
	if len(m.sizeBusy) != 4 || len(m.sizeWait) != 3 {
		t.Fatalf("busy %d, wait %d; want 4 and 3", len(m.sizeBusy), len(m.sizeWait))
	}
	m, cmd := send(t, m, sizeMsg{path: "/r/a", bytes: 1024})
	if len(m.sizeBusy) != 4 || len(m.sizeWait) != 2 || cmd == nil {
		t.Errorf("after one result: busy %d, wait %d, cmd %v", len(m.sizeBusy), len(m.sizeWait), cmd != nil)
	}
}

func TestCursorMoves(t *testing.T) {
	m := loaded(t)
	m, _ = keys(t, m, "j", "j", "k")
	if m.cursorIndex() != 1 {
		t.Errorf("cursor = %d, want 1", m.cursorIndex())
	}
	m, _ = keys(t, m, "up", "down", "down")
	if m.cursorIndex() != 2 {
		t.Errorf("cursor = %d, want 2", m.cursorIndex())
	}
}

// TestCursorLeftRight makes sure that left moves as up and right moves as down.
func TestCursorLeftRight(t *testing.T) {
	m := loaded(t)
	m, _ = keys(t, m, "right", "right")
	if m.cursorIndex() != 2 {
		t.Errorf("after right, right: cursor = %d, want 2", m.cursorIndex())
	}
	m, _ = keys(t, m, "left")
	if m.cursorIndex() != 1 {
		t.Errorf("after left: cursor = %d, want 1", m.cursorIndex())
	}
}

// TestCursorWraps makes sure that the cursor goes from the first row to the
// last row and back, with each move key.
func TestCursorWraps(t *testing.T) {
	for _, pair := range [][2]string{{"up", "down"}, {"k", "j"}, {"left", "right"}} {
		m := loaded(t)
		m, _ = keys(t, m, pair[0])
		if m.cursorIndex() != 2 {
			t.Errorf("%s at the first row: cursor = %d, want the last row 2", pair[0], m.cursorIndex())
		}
		m, _ = keys(t, m, pair[1])
		if m.cursorIndex() != 0 {
			t.Errorf("%s at the last row: cursor = %d, want the first row 0", pair[1], m.cursorIndex())
		}
	}
}

// TestCursorWrapsInFilter makes sure that the wrap uses the rows that show.
func TestCursorWrapsInFilter(t *testing.T) {
	m := loaded(t)
	m, _ = keys(t, m, "/", "e", "n", "g", "enter")
	if n := len(m.visible()); n != 2 {
		t.Fatalf("filter shows %d rows, want 2", n)
	}
	first := m.cursorIndex()
	m, _ = keys(t, m, "down", "down")
	if m.cursorIndex() != first {
		t.Errorf("two moves over 2 rows: cursor = %d, want %d", m.cursorIndex(), first)
	}
}

func TestCursorFollowsPath(t *testing.T) {
	m := listed(t, deps(t), "origin/master")
	m, _ = keys(t, m, "j", "j")
	m, _ = send(t, m, commitMsg{path: flowsPath, t: now})
	m, _ = send(t, m, commitMsg{path: alertPath, t: now.Add(-time.Hour)})
	if m.rows[1].WT.Path != flowsPath || m.cursorIndex() != 1 {
		t.Errorf("cursor %d on %s, want the flows row at 1", m.cursorIndex(), m.rows[m.cursorIndex()].WT.Path)
	}
}

func TestEnterSwitches(t *testing.T) {
	m := loaded(t)
	m, _ = keys(t, m, "j")
	want := m.rows[1].WT.Path
	m, cmd := keys(t, m, "enter")
	if got := m.Result(); got.SwitchPath != want || got.OpenPath != "" {
		t.Errorf("Result = %+v, want SwitchPath %s", got, want)
	}
	if !isQuit(cmd) {
		t.Error("enter does not quit")
	}
}

func TestOpenKey(t *testing.T) {
	m := loaded(t)
	m, cmd := keys(t, m, "o")
	if got := m.Result(); got.OpenPath != m.rows[0].WT.Path || got.SwitchPath != "" {
		t.Errorf("Result = %+v", got)
	}
	if !isQuit(cmd) {
		t.Error("o does not quit")
	}
}

func TestQuitKeys(t *testing.T) {
	for _, k := range []string{"q", "esc", "ctrl+c"} {
		m := loaded(t)
		m, cmd := keys(t, m, k)
		if !isQuit(cmd) {
			t.Errorf("%s does not quit", k)
		}
		if m.Result() != (Result{}) {
			t.Errorf("%s: Result = %+v", k, m.Result())
		}
	}
}

func branchRows(v string) int {
	n := 0
	for _, l := range strings.Split(v, "\n") {
		if strings.Contains(l, "master (mono)") || strings.Contains(l, "eng-6201-") || strings.Contains(l, "eng-6458-") {
			n++
		}
	}
	return n
}

func TestFilter(t *testing.T) {
	m := loaded(t)
	m, _ = keys(t, m, "/", "6", "4", "5", "8")
	v := m.View()
	if n := branchRows(v); n != 1 || !strings.Contains(v, "eng-6458-bugsink-key-flows") {
		t.Errorf("filter 6458: %d rows:\n%s", n, v)
	}
	m, cmd := keys(t, m, "enter")
	if isQuit(cmd) || m.Result() != (Result{}) {
		t.Fatal("enter in filter mode must not switch")
	}
	if n := branchRows(m.View()); n != 1 {
		t.Errorf("enter must keep the filter: %d rows", n)
	}
	m, cmd = keys(t, m, "enter")
	if m.Result().SwitchPath != flowsPath || !isQuit(cmd) {
		t.Errorf("enter on the filtered row: %+v", m.Result())
	}

	m = loaded(t)
	m, _ = keys(t, m, "/", "B", "U", "G")
	if n := branchRows(m.View()); n != 1 {
		t.Errorf("filter ignores case: %d rows", n)
	}
	m, cmd = keys(t, m, "esc")
	if isQuit(cmd) {
		t.Error("esc in filter mode must not quit")
	}
	if n := branchRows(m.View()); n != 3 {
		t.Errorf("esc must clear the filter: %d rows", n)
	}
}

func TestFilterByFolder(t *testing.T) {
	m := loaded(t)
	m, _ = keys(t, m, "/", "m", "o", "n", "o")
	if v := m.View(); branchRows(v) != 1 || !strings.Contains(v, "master (mono)") {
		t.Errorf("filter by folder:\n%s", v)
	}
	m, _ = keys(t, m, "backspace", "backspace", "backspace", "backspace")
	if n := branchRows(m.View()); n != 3 {
		t.Errorf("backspace: %d rows", n)
	}
}

func TestFetchStarts(t *testing.T) {
	m := listed(t, deps(t), "origin/master")
	if !m.fetching {
		t.Error("fetch is not in progress after load")
	}
	m, _ = send(t, m, distMsg{path: alertPath, ahead: 2, behind: 31})
	m, _ = send(t, m, fetchMsg{})
	if m.fetching || !m.distWait[alertPath] {
		t.Errorf("after fetch: fetching %v, distance waits %v", m.fetching, m.distWait[alertPath])
	}
	if l := lineWith(t, m.View(), "eng-6201"); !strings.Contains(l, "↑2 ↓31") {
		t.Errorf("old distance must stay while the new one loads: %q", l)
	}
	m, cmd := keys(t, m, "f")
	if !m.fetching || cmd == nil {
		t.Error("f does not start a fetch")
	}
}

func TestFetchFailed(t *testing.T) {
	m := loaded(t)
	m, _ = send(t, m, fetchMsg{err: errors.New("timeout")})
	if v := m.View(); !strings.Contains(v, "fetch failed: distances can be stale") {
		t.Errorf("no fetch failure text:\n%s", v)
	}
}

func TestFetchOffByConfig(t *testing.T) {
	d := deps(t)
	d.Cfg.Fetch = false
	m := listed(t, d, "origin/master")
	if m.fetching {
		t.Error("fetch in progress with fetch = false")
	}
}

func TestNoBase(t *testing.T) {
	m := listed(t, deps(t), "")
	if m.fetching {
		t.Error("fetch in progress with no base")
	}
	m, _ = send(t, m, changedMsg{path: alertPath, n: 4})
	m, _ = send(t, m, commitMsg{path: alertPath, t: now})
	v := m.View()
	if !strings.Contains(lineWith(t, v, "BRANCH"), "BASE") {
		t.Errorf("no BASE header:\n%s", v)
	}
	if l := lineWith(t, v, "eng-6201"); !strings.Contains(l, "—") {
		t.Errorf("distance cell: %q", l)
	}
	if len(m.distWait) != 0 {
		t.Errorf("distance commands with no base: %v", m.distWait)
	}
	if _, cmd := keys(t, m, "f"); cmd != nil {
		t.Error("f starts a fetch with no base")
	}
}

func TestPrunableRow(t *testing.T) {
	m := New(deps(t))
	wts := threeWorktrees()
	wts[1].Prunable = true
	m, _ = send(t, m, tea.WindowSizeMsg{Width: 120, Height: 30})
	m, _ = send(t, m, worktreesMsg{wts: wts, base: "origin/master"})
	if l := lineWith(t, m.View(), "eng-6201"); !strings.Contains(l, "[missing]") || strings.Contains(l, "…") {
		t.Errorf("prunable row: %q", l)
	}
	if sizePlanned(m, alertPath) {
		t.Error("size command for a missing folder")
	}
	for _, k := range []string{"enter", "o"} {
		m2, _ := keys(t, m, "j")
		m2, cmd := keys(t, m2, k)
		if isQuit(cmd) || m2.Result() != (Result{}) {
			t.Errorf("%s on a prunable row quits", k)
		}
		if v := m2.View(); !strings.Contains(v, "folder is missing: use prune") {
			t.Errorf("%s: no footer text:\n%s", k, v)
		}
	}
}

func TestNarrowTerminal(t *testing.T) {
	for _, w := range []int{40, 50, 70} {
		m := loaded(t)
		m, _ = send(t, m, tea.WindowSizeMsg{Width: w, Height: 20})
		v := m.View()
		for _, l := range strings.Split(v, "\n") {
			if lipgloss.Width(l) > w {
				t.Errorf("width %d: line too wide (%d): %q", w, lipgloss.Width(l), l)
			}
		}
		if hasCommit := strings.Contains(v, "LAST COMMIT"); hasCommit != (w >= 60) {
			t.Errorf("width %d: LAST COMMIT shown = %v", w, hasCommit)
		}
		if hasSize := strings.Contains(lineWith(t, v, "BRANCH"), "SIZE"); hasSize != (w >= 45) {
			t.Errorf("width %d: SIZE shown = %v", w, hasSize)
		}
		if w == 40 && !strings.Contains(lineWith(t, v, "eng-6201"), "…") {
			t.Errorf("long branch is not cut:\n%s", v)
		}
	}
}

func TestCutKeepsMarks(t *testing.T) {
	m := New(deps(t))
	wts := threeWorktrees()
	wts[1].Locked, wts[1].Prunable = true, true
	m, _ = send(t, m, tea.WindowSizeMsg{Width: 80, Height: 30})
	m, _ = send(t, m, worktreesMsg{wts: wts, base: "origin/master"})
	if l := lineWith(t, m.View(), "eng-6201"); !strings.Contains(l, "… [locked] [missing]") {
		t.Errorf("cut row must keep the marks: %q", l)
	}
	// The label with its marks fills the width exactly: no mark is cut.
	r := status.Row{WT: git.Worktree{Path: "/r/other-folder", Branch: "feat", Locked: true, Prunable: true}}
	label := status.Label(r)
	if got := branchCell(r, lipgloss.Width(label), true, ""); got != label {
		t.Errorf("branchCell at the label width = %q, want %q", got, label)
	}
}

// TestDimUsesScreenRenderer forces a color profile on the renderer of the
// screen. The lipgloss default renderer stays as it is.
func TestDimUsesScreenRenderer(t *testing.T) {
	old := renderer.ColorProfile()
	renderer.SetColorProfile(termenv.ANSI)
	t.Cleanup(func() { renderer.SetColorProfile(old) })

	d := deps(t)
	d.Cache.Set(repoPath, alertPath, 1503238554, now.Add(-25*time.Hour))
	m := listed(t, d, "origin/master")
	const faint = "\x1b[2m"
	if l := lineWith(t, m.View(), "eng-6201"); !strings.Contains(l, faint+"~1.4 GB") {
		t.Errorf("stale size is not faint: %q", l)
	}
	m, _ = send(t, m, tea.WindowSizeMsg{Width: 40, Height: 20})
	for _, l := range strings.Split(m.View(), "\n") {
		if lipgloss.Width(l) > 40 {
			t.Errorf("styled line too wide (%d): %q", lipgloss.Width(l), l)
		}
	}
}

func TestDefaultWidth(t *testing.T) {
	m := New(deps(t))
	m, _ = send(t, m, worktreesMsg{wts: threeWorktrees(), base: "origin/master"})
	for _, l := range strings.Split(m.View(), "\n") {
		if lipgloss.Width(l) > 80 {
			t.Errorf("line wider than 80: %q", l)
		}
	}
}

func TestMainFirstThenNewest(t *testing.T) {
	m := listed(t, deps(t), "origin/master")
	m, _ = send(t, m, commitMsg{path: alertPath, t: now.Add(-30 * 24 * time.Hour)})
	m, _ = send(t, m, commitMsg{path: mainPath, t: now.Add(-90 * 24 * time.Hour)})
	m, _ = send(t, m, commitMsg{path: flowsPath, t: now.Add(-time.Hour)})
	var got []string
	for _, r := range m.rows {
		got = append(got, r.WT.Path)
	}
	if want := []string{mainPath, flowsPath, alertPath}; strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("order = %v, want %v", got, want)
	}
	v := m.View()
	if strings.Index(v, "eng-6458") > strings.Index(v, "eng-6201") {
		t.Errorf("view order wrong:\n%s", v)
	}
}

func TestLoadError(t *testing.T) {
	m := New(deps(t))
	m, _ = send(t, m, worktreesMsg{err: errors.New("boom")})
	if v := m.View(); !strings.Contains(v, "boom") {
		t.Errorf("no error text:\n%s", v)
	}
}

func TestReloadKey(t *testing.T) {
	m := loaded(t)
	m, cmd := keys(t, m, "r")
	if cmd == nil {
		t.Fatal("r starts no command")
	}
	m, _ = send(t, m, worktreesMsg{wts: threeWorktrees(), base: "origin/master"})
	if !sizePlanned(m, alertPath) {
		t.Error("r must run du again for a fresh cache entry")
	}
	if l := lineWith(t, m.View(), "eng-6201"); !strings.Contains(l, "4 files") || !strings.Contains(l, "~1.4 GB") {
		t.Errorf("old values must stay while new ones load: %q", l)
	}
}

// TestCommands runs the command functions against a real test repository.
func TestCommands(t *testing.T) {
	repo := testrepo.New(t)
	feat := repo.AddWorktree(t, "feat")
	repo.Commit(t, feat, "a.txt")
	if err := os.WriteFile(filepath.Join(feat, "new.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if real, err := filepath.EvalSymlinks(feat); err == nil {
		feat = real
	}
	r, err := git.Open(repo.Dir)
	if err != nil {
		t.Fatal(err)
	}
	d := deps(t)
	d.Git = r
	d.Repo = repo.Dir
	m := New(d)

	msg, ok := m.Init()().(worktreesMsg)
	if !ok || msg.err != nil || len(msg.wts) != 2 || msg.base != "origin/master" {
		t.Fatalf("Init: %+v", msg)
	}
	if c := changedCmd(r, feat)().(changedMsg); c.err != nil || c.n != 1 || c.path != feat {
		t.Errorf("changed: %+v", c)
	}
	if c := distCmd(r, feat, "origin/master")().(distMsg); c.err != nil || c.ahead != 1 || c.behind != 0 {
		t.Errorf("dist: %+v", c)
	}
	if c := commitCmd(r, feat)().(commitMsg); c.err != nil || c.t.IsZero() {
		t.Errorf("commit: %+v", c)
	}
	if c := sizeCmd(feat)().(sizeMsg); c.err != nil || c.bytes <= 0 {
		t.Errorf("size: %+v", c)
	}
	if c := fetchCmd(r, "origin/master")().(fetchMsg); c.err != nil {
		t.Errorf("fetch: %v", c.err)
	}
	if c := fetchCmd(r, "nowhere")().(fetchMsg); c.err != nil {
		t.Errorf("a base with no remote ref is not fetched: %v", c.err)
	}
}

func TestCommandsNoBase(t *testing.T) {
	repo := testrepo.NewNoOrigin(t)
	r, err := git.Open(repo.Dir)
	if err != nil {
		t.Fatal(err)
	}
	d := deps(t)
	d.Git = r
	msg := New(d).Init()().(worktreesMsg)
	if msg.err != nil || msg.base != "" || len(msg.wts) != 1 {
		t.Errorf("Init with no origin: %+v", msg)
	}
}

// TestFetchLocalBase: with base = "master", the fetch on open is skipped
// with no error, and the screen shows no fetch failure.
func TestFetchLocalBase(t *testing.T) {
	repo := testrepo.New(t)
	r, err := git.Open(repo.Dir)
	if err != nil {
		t.Fatal(err)
	}
	d := deps(t)
	d.Git = r
	d.Cfg.Base = "master"
	m := New(d)
	m, _ = send(t, m, tea.WindowSizeMsg{Width: 120, Height: 30})
	msg := m.Init()().(worktreesMsg)
	if msg.err != nil || msg.base != "master" {
		t.Fatalf("Init: %+v", msg)
	}
	m, _ = send(t, m, msg)
	if !m.fetching {
		t.Fatal("no fetch on open")
	}
	f, ok := fetchCmd(r, m.base)().(fetchMsg)
	if !ok || f.err != nil {
		t.Fatalf("fetch: %+v", f)
	}
	m, _ = send(t, m, f)
	if v := m.View(); m.fetchFailed || strings.Contains(v, "fetch failed") {
		t.Errorf("fetch failure shows:\n%s", v)
	}
}

// TestCursorStartsOnCurrent: on the first load, the cursor is on the row of
// the current worktree. A reload keeps the row of the cursor.
func TestCursorStartsOnCurrent(t *testing.T) {
	d := deps(t)
	d.Cwd = alertPath + "/sub"
	m := listed(t, d, "origin/master")
	if m.cursorPath() != alertPath {
		t.Fatalf("cursor on %s, want %s", m.cursorPath(), alertPath)
	}
	// The cursor mark wins on the row that is both.
	if l := lineWith(t, m.View(), "eng-6201"); !strings.HasPrefix(l, " › ") {
		t.Errorf("current row with the cursor: %q", l)
	}
	m, _ = keys(t, m, "k")
	m, _ = send(t, m, worktreesMsg{wts: threeWorktrees(), base: "origin/master"})
	if m.cursorPath() == alertPath {
		t.Error("a reload moves the cursor back to the current row")
	}
}

// TestMergedMark: the mark shows after mergedMsg, never on the main row.
func TestMergedMark(t *testing.T) {
	m := loaded(t)
	if m.mergedWait[mainPath] || !m.mergedWait[alertPath] || !m.mergedWait[flowsPath] {
		t.Errorf("merged waits: %v", m.mergedWait)
	}
	m, _ = send(t, m, mergedMsg{path: alertPath, state: git.MergeMerged})
	m, _ = send(t, m, mergedMsg{path: flowsPath, state: git.MergeNone})
	m, _ = send(t, m, mergedMsg{path: mainPath, state: git.MergeMerged})
	v := m.View()
	if l := lineWith(t, v, "eng-6201"); !strings.Contains(l, "eng-6201-onboarding-v2-alerts ✓ merged") {
		t.Errorf("merged row: %q", l)
	}
	if l := lineWith(t, v, "eng-6458"); strings.Contains(l, "merged") {
		t.Errorf("unmerged row: %q", l)
	}
	if l := lineWith(t, v, "master"); strings.Contains(l, "merged") {
		t.Errorf("main row: %q", l)
	}
	// A cut name keeps the mark.
	m, _ = send(t, m, tea.WindowSizeMsg{Width: 40, Height: 20})
	if l := lineWith(t, m.View(), "4 files"); !strings.Contains(l, "eng… ✓ merged") {
		t.Errorf("cut row must keep the mark: %q", l)
	}
	// A fetch and r start the merged commands again.
	m, _ = keys(t, m, "f")
	m, _ = send(t, m, fetchMsg{})
	if !m.mergedWait[alertPath] {
		t.Error("no merged command after the fetch")
	}
	m, _ = keys(t, m, "r")
	m, _ = send(t, m, worktreesMsg{wts: threeWorktrees(), base: "origin/master"})
	if !m.mergedWait[alertPath] || m.mergedWait[mainPath] {
		t.Errorf("after r: merged waits %v", m.mergedWait)
	}
}

func TestMergedMarkIsGreen(t *testing.T) {
	old := renderer.ColorProfile()
	renderer.SetColorProfile(termenv.ANSI)
	t.Cleanup(func() { renderer.SetColorProfile(old) })
	m := loaded(t)
	m, _ = send(t, m, mergedMsg{path: alertPath, state: git.MergeMerged})
	if l := lineWith(t, m.View(), "eng-6201"); !strings.Contains(l, "\x1b[32m ✓ merged") {
		t.Errorf("merged mark is not green: %q", l)
	}
}

func TestMergedCmd(t *testing.T) {
	repo := testrepo.New(t)
	feat := repo.AddWorktree(t, "feat")
	repo.Commit(t, feat, "a.txt")
	repo.Git(t, repo.Dir, "merge", "--squash", "feat")
	repo.Git(t, repo.Dir, "commit", "-m", "squash feat")
	repo.Git(t, repo.Dir, "push", "origin", "master")
	r, err := git.Open(repo.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if c := mergedCmd(r, feat, "origin/master")().(mergedMsg); c.err != nil || c.state != git.MergeMerged || c.path != feat {
		t.Errorf("merged: %+v", c)
	}
}

// TestCursorRowColor: the cursor row is bold cyan, with no other style in
// it. Other rows do not change. No styled line is wider than the terminal.
func TestCursorRowColor(t *testing.T) {
	old := renderer.ColorProfile()
	renderer.SetColorProfile(termenv.ANSI)
	t.Cleanup(func() { renderer.SetColorProfile(old) })
	const (
		boldCyan = "\x1b[1;36m"
		faint    = "\x1b[2m"
	)
	m := loaded(t)
	m, _ = send(t, m, fetchMsg{err: errors.New("timeout")}) // dims the distances
	m, _ = send(t, m, mergedMsg{path: alertPath, state: git.MergeMerged})
	m, _ = keys(t, m, "j", "j") // main, flows, alert: the cursor goes to alert
	v := m.View()
	cur := lineWith(t, v, "eng-6201")
	if !strings.Contains(cur, cursorMark) || !strings.HasPrefix(cur, boldCyan) {
		t.Errorf("cursor row is not bold cyan: %q", cur)
	}
	if strings.Contains(cur, faint) || strings.Contains(cur, "\x1b[32m") {
		t.Errorf("cursor row has a second style: %q", cur)
	}
	other := lineWith(t, v, "eng-6458")
	if strings.Contains(other, boldCyan) || !strings.Contains(other, faint) {
		t.Errorf("other row: %q", other)
	}
	if l := lineWith(t, v, "(mono)"); strings.Contains(l, boldCyan) || !strings.Contains(l, faint+"(mono)") {
		t.Errorf("main row: %q", l)
	}
	for _, w := range []int{40, 50, 70} {
		m, _ = send(t, m, tea.WindowSizeMsg{Width: w, Height: 20})
		for _, l := range strings.Split(m.View(), "\n") {
			if lipgloss.Width(l) > w {
				t.Errorf("width %d: styled line too wide (%d): %q", w, lipgloss.Width(l), l)
			}
		}
	}
}

// TestNarrowTerminalStyled is TestNarrowTerminal with styles on.
func TestNarrowTerminalStyled(t *testing.T) {
	old := renderer.ColorProfile()
	renderer.SetColorProfile(termenv.ANSI)
	t.Cleanup(func() { renderer.SetColorProfile(old) })
	TestNarrowTerminal(t)
}

// copyDeps returns deps with a fake clipboard. got receives the copied text.
func copyDeps(t *testing.T, got *[]string, err error) Deps {
	t.Helper()
	d := deps(t)
	d.Copy = func(s string) error { *got = append(*got, s); return err }
	return d
}

// TestCopyKey: c copies the branch name of the cursor row in a command.
func TestCopyKey(t *testing.T) {
	var got []string
	m := listed(t, copyDeps(t, &got, nil), "origin/master")
	m, _ = keys(t, m, "j")
	m, cmd := keys(t, m, "c")
	if cmd == nil || len(got) != 0 {
		t.Fatalf("c: cmd %v, copied %v before the command runs", cmd != nil, got)
	}
	m, _ = send(t, m, cmd())
	if len(got) != 1 || got[0] != "eng-6201-onboarding-v2-alerts" {
		t.Errorf("copied %q", got)
	}
	if v := m.View(); !strings.Contains(v, "copied eng-6201-onboarding-v2-alerts") {
		t.Errorf("no note:\n%s", v)
	}
}

func TestCopyDetached(t *testing.T) {
	var got []string
	m := New(copyDeps(t, &got, nil))
	wts := threeWorktrees()
	wts[1].Branch, wts[1].Detached, wts[1].Head = "", true, "abcdef1234567890"
	m, _ = send(t, m, tea.WindowSizeMsg{Width: 120, Height: 30})
	m, _ = send(t, m, worktreesMsg{wts: wts, base: "origin/master"})
	m.cursor = alertPath
	m, cmd := keys(t, m, "c")
	m, _ = send(t, m, cmd())
	if len(got) != 1 || got[0] != "abcdef1" {
		t.Errorf("copied %q", got)
	}
	if v := m.View(); !strings.Contains(v, "copied abcdef1") {
		t.Errorf("no note:\n%s", v)
	}
}

func TestCopyError(t *testing.T) {
	var got []string
	m := listed(t, copyDeps(t, &got, errors.New("no clipboard tool found (pbcopy, wl-copy, xclip, xsel)")), "origin/master")
	m, cmd := keys(t, m, "c")
	m, _ = send(t, m, cmd())
	if v := m.View(); !strings.Contains(v, "cannot copy: no clipboard tool found (pbcopy, wl-copy, xclip, xsel)") {
		t.Errorf("no error note:\n%s", v)
	}
}

// c does nothing while a delete or prune step runs.
func TestCopyWaitsForBusy(t *testing.T) {
	var got []string
	m := listed(t, copyDeps(t, &got, nil), "origin/master")
	m.busy = "pruning"
	if _, cmd := keys(t, m, "c"); cmd != nil {
		t.Error("c starts a command while a step runs")
	}
}

// TestFooterWraps: a footer that does not fit wraps at the separators, and
// the table gives up rows for the extra lines.
func TestFooterWraps(t *testing.T) {
	const w, h = 50, 10
	m := loaded(t)
	m, _ = send(t, m, tea.WindowSizeMsg{Width: w, Height: h})
	lines := strings.Split(m.View(), "\n")
	if len(lines) > h {
		t.Errorf("%d lines, want at most %d:\n%s", len(lines), h, m.View())
	}
	var foot []string
	for _, l := range lines {
		if lipgloss.Width(l) > w {
			t.Errorf("line too wide (%d): %q", lipgloss.Width(l), l)
		}
		if strings.Contains(l, "switch") || strings.Contains(l, "quit") || strings.Contains(l, "delete") {
			foot = append(foot, l)
		}
	}
	all := strings.Join(foot, " · ")
	if len(foot) < 2 || strings.Contains(all, "…") {
		t.Errorf("footer does not wrap: %q", foot)
	}
	for _, k := range []string{"enter switch", "o open", "c copy", "s sync", "d delete", "D delete + branch", "p prune", "f fetch", "/ filter", "q quit"} {
		if !strings.Contains(all, k) {
			t.Errorf("footer lacks %q: %q", k, foot)
		}
	}
	for _, l := range foot {
		if strings.HasSuffix(l, "·") || strings.HasPrefix(l, " ·") {
			t.Errorf("footer line breaks inside a separator: %q", l)
		}
	}
	if n := branchRows(m.View()); n != h-6-(len(foot)-1) {
		t.Errorf("%d table rows with %d footer lines", n, len(foot))
	}
}

// TestNoCommitsMark: "no commits" shows after the message, dim on a row
// without the cursor, cut names keep it, and the main row never shows it.
func TestNoCommitsMark(t *testing.T) {
	old := renderer.ColorProfile()
	renderer.SetColorProfile(termenv.ANSI)
	t.Cleanup(func() { renderer.SetColorProfile(old) })
	m := loaded(t) // the cursor is on the main row
	m, _ = send(t, m, mergedMsg{path: flowsPath, state: git.MergeNoCommits})
	m, _ = send(t, m, mergedMsg{path: mainPath, state: git.MergeNoCommits})
	v := m.View()
	if l := lineWith(t, v, "eng-6458"); !strings.Contains(l, "eng-6458-bugsink-key-flows\x1b[2m no commits") {
		t.Errorf("no commits mark is not dim: %q", l)
	}
	if l := lineWith(t, v, "(mono)"); strings.Contains(l, "no commits") {
		t.Errorf("main row: %q", l)
	}
	if l := lineWith(t, v, "eng-6201"); strings.Contains(l, "no commits") {
		t.Errorf("other row: %q", l)
	}
	m, _ = keys(t, m, "j")
	if l := lineWith(t, m.View(), "eng-6458"); strings.Contains(l, "\x1b[2m") || !strings.Contains(l, "no commits") {
		t.Errorf("cursor row: %q", l)
	}
	m, _ = send(t, m, tea.WindowSizeMsg{Width: 40, Height: 20})
	if l := lineWith(t, m.View(), "↑0 ↓12"); !strings.Contains(l, "… no commits") {
		t.Errorf("cut row must keep the mark: %q", l)
	}
}
