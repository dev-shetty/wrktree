package status

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dev-shetty/wrktree/internal/disk"
	"github.com/dev-shetty/wrktree/internal/git"
	"github.com/dev-shetty/wrktree/internal/testrepo"
)

func TestAge(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	h := time.Hour
	d := 24 * h
	cases := map[time.Duration]string{
		30 * time.Second: "just now",
		5 * time.Minute:  "5 minutes ago",
		1 * time.Minute:  "1 minute ago",
		1 * h:            "1 hour ago",
		2 * h:            "2 hours ago",
		26 * h:           "1 day ago",
		9 * d:            "9 days ago",
		21 * d:           "3 weeks ago",
		70 * d:           "2 months ago",
		800 * d:          "2 years ago",
		-5 * h:           "just now",
	}
	for ago, want := range cases {
		if got := Age(now.Add(-ago), now); got != want {
			t.Errorf("Age(%v ago) = %q, want %q", ago, got, want)
		}
	}
}

func TestSort(t *testing.T) {
	now := time.Now()
	mk := func(name string, main bool, ago time.Duration, ok bool) Row {
		return Row{WT: git.Worktree{Branch: name, IsMain: main}, LastCommit: now.Add(-ago), CommitOK: ok}
	}
	rows := []Row{
		mk("old", false, 48*time.Hour, true),
		mk("none", false, 0, false),
		mk("main", true, 900*time.Hour, true),
		mk("new", false, time.Hour, true),
	}
	Sort(rows)
	var got []string
	for _, r := range rows {
		got = append(got, r.WT.Branch)
	}
	if want := "main new old none"; strings.Join(got, " ") != want {
		t.Errorf("order = %v, want %s", got, want)
	}
}

func setup(t *testing.T) (*testrepo.Repo, git.Runner, string) {
	t.Helper()
	repo := testrepo.New(t)
	feat := repo.AddWorktree(t, "feat")
	if err := os.WriteFile(filepath.Join(feat, "untracked.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	repo.Commit(t, feat, "a.txt")
	repo.Commit(t, feat, "b.txt")
	r, err := git.Open(repo.Dir)
	if err != nil {
		t.Fatal(err)
	}
	return repo, r, feat
}

func find(t *testing.T, rows []Row, branch string) Row {
	t.Helper()
	for _, r := range rows {
		if r.WT.Branch == branch {
			return r
		}
	}
	t.Fatalf("no row for %s", branch)
	return Row{}
}

func TestLoadAll(t *testing.T) {
	repo, r, feat := setup(t)
	ctx := context.Background()
	wts, err := r.Worktrees(ctx)
	if err != nil {
		t.Fatal(err)
	}
	cache := disk.LoadCache(filepath.Join(t.TempDir(), "sizes.json"))
	now := time.Now()
	o := Options{Base: "origin/master", TTL: time.Hour, Cwd: feat, Now: now}

	rows := LoadAll(ctx, r, wts, cache, repo.Dir, o)
	f := find(t, rows, "feat")
	if !f.Current || f.Changed != 1 || !f.ChangedOK || f.Ahead != 2 || f.Behind != 0 || !f.DistOK || !f.CommitOK {
		t.Errorf("feat row: %+v", f)
	}
	if f.SizeState != disk.SizeFresh || f.Size <= 0 {
		t.Errorf("feat size: state %v, size %d", f.SizeState, f.Size)
	}
	if m := find(t, rows, "master"); m.Current || !m.WT.IsMain {
		t.Errorf("master row: %+v", m)
	}
	for _, w := range wts {
		if _, ok := cache.Get(repo.Dir, w.Path); !ok {
			t.Errorf("no cache entry for %s", w.Path)
		}
	}

	rows = LoadAll(ctx, r, wts, cache, repo.Dir, o)
	if s := find(t, rows, "feat").SizeState; s != disk.SizeCached {
		t.Errorf("second call: state %v, want SizeCached", s)
	}
	o.Fresh = true
	rows = LoadAll(ctx, r, wts, cache, repo.Dir, o)
	if s := find(t, rows, "feat").SizeState; s != disk.SizeFresh {
		t.Errorf("fresh call: state %v, want SizeFresh", s)
	}
}

func TestLoadAllNoBase(t *testing.T) {
	repo, r, _ := setup(t)
	ctx := context.Background()
	wts, _ := r.Worktrees(ctx)
	cache := disk.LoadCache(filepath.Join(t.TempDir(), "sizes.json"))
	rows := LoadAll(ctx, r, wts, cache, repo.Dir, Options{TTL: time.Hour, Cwd: repo.Dir, Now: time.Now()})
	for _, row := range rows {
		if row.DistOK {
			t.Errorf("%s: DistOK with no base", row.WT.Branch)
		}
	}
}

func TestLoadAllMissingFolder(t *testing.T) {
	repo, r, feat := setup(t)
	ctx := context.Background()
	wts, _ := r.Worktrees(ctx)
	if err := os.RemoveAll(feat); err != nil {
		t.Fatal(err)
	}
	cache := disk.LoadCache(filepath.Join(t.TempDir(), "sizes.json"))
	rows := LoadAll(ctx, r, wts, cache, repo.Dir, Options{Base: "origin/master", TTL: time.Hour, Cwd: repo.Dir, Now: time.Now()})
	f := find(t, rows, "feat")
	if f.ChangedOK || f.SizeState != disk.SizeNone {
		t.Errorf("missing folder row: %+v", f)
	}
}

func fixedRows(now time.Time) []Row {
	return []Row{
		{WT: git.Worktree{Path: "/w/main", Branch: "master", IsMain: true}, Current: true,
			Changed: 4, ChangedOK: true, Ahead: 2, Behind: 31, DistOK: true,
			LastCommit: now.Add(-21 * 24 * time.Hour), CommitOK: true, Size: 1503238554, SizeState: disk.SizeCached},
		{WT: git.Worktree{Path: "/w/feat", Branch: "feat", Locked: true},
			Changed: 0, ChangedOK: true, LastCommit: now.Add(-2 * time.Hour), CommitOK: true,
			Size: 2048, SizeState: disk.SizeFresh},
		{WT: git.Worktree{Path: "/w/gone", Branch: "gone", Prunable: true}},
		{WT: git.Worktree{Path: "/w/det", Head: "abcdef1234567", Detached: true}, Changed: 1, ChangedOK: true},
	}
}

func TestTable(t *testing.T) {
	now := time.Now()
	var buf bytes.Buffer
	Table(&buf, fixedRows(now), "origin/master", now)
	out := buf.String()
	for _, s := range []string{"origin/master", "● master", "4 files", "↑2 ↓31", "~1.4 GB", "3 weeks ago",
		"[locked]", "[missing]", "—", "clean", "2 KB", "abcdef1", "feat"} {
		if !strings.Contains(out, s) {
			t.Errorf("table lacks %q:\n%s", s, out)
		}
	}
	if strings.Contains(out, "~2 KB") {
		t.Errorf("fresh size has ~:\n%s", out)
	}
	if !strings.Contains(out, "1 file") {
		t.Errorf("no singular form:\n%s", out)
	}
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if s, c := strings.Index(lines[0], "SIZE"), strings.Index(lines[0], "LAST COMMIT"); s < 0 || c < 0 || s > c {
		t.Errorf("header order: want SIZE before LAST COMMIT: %q", lines[0])
	}
	if len(lines) != 5 {
		t.Fatalf("want 5 lines, got %d:\n%s", len(lines), out)
	}
	for _, l := range lines[2:] {
		if !strings.HasPrefix(l, "  ") && !strings.HasPrefix(l, " ") {
			t.Errorf("row does not start with a space: %q", l)
		}
	}
	buf.Reset()
	Table(&buf, fixedRows(now), "", now)
	if !strings.Contains(buf.String(), "BASE") {
		t.Errorf("no BASE header:\n%s", buf.String())
	}
}

func TestJSON(t *testing.T) {
	now := time.Now()
	var buf bytes.Buffer
	if err := JSON(&buf, fixedRows(now), "origin/master"); err != nil {
		t.Fatal(err)
	}
	var got []map[string]any
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 4 {
		t.Fatalf("len %d", len(got))
	}
	if got[0]["main"] != true || got[0]["current"] != true || got[0]["ahead"] != float64(2) || got[0]["size_approx"] != true {
		t.Errorf("row 0: %v", got[0])
	}
	if got[1]["locked"] != true || got[1]["ahead"] != nil || got[1]["changed"] != float64(0) {
		t.Errorf("row 1: %v", got[1])
	}
	if got[2]["changed"] != nil || got[2]["last_commit"] != nil || got[2]["size_bytes"] != nil || got[2]["prunable"] != true {
		t.Errorf("row 2: %v", got[2])
	}
	if got[0]["base"] != "origin/master" {
		t.Errorf("base: %v", got[0]["base"])
	}
	if _, err := time.Parse(time.RFC3339, got[0]["last_commit"].(string)); err != nil {
		t.Errorf("last_commit: %v", err)
	}
}

func TestCellText(t *testing.T) {
	now := time.Now()
	rows := fixedRows(now)
	cases := []struct{ got, want string }{
		{Label(rows[0]), "master"},
		{FolderNote(rows[0]), "(main)"},
		{Name(rows[0]), "master (main)"},
		{Label(rows[1]), "feat [locked]"},
		{FolderNote(rows[1]), ""},
		{Name(rows[2]), "gone [missing]"},
		{Name(rows[3]), "abcdef1"},
		{ChangedText(rows[0]), "4 files"},
		{ChangedText(rows[2]), "—"},
		{DistText(rows[0]), "↑2 ↓31"},
		{DistText(rows[1]), "—"},
		{SizeText(rows[0]), "~1.4 GB"},
		{SizeText(rows[1]), "2 KB"},
		{SizeText(rows[2]), "—"},
		{CommitText(rows[0], now), "3 weeks ago"},
		{CommitText(rows[2], now), "—"},
	}
	for i, c := range cases {
		if c.got != c.want {
			t.Errorf("case %d: got %q, want %q", i, c.got, c.want)
		}
	}
}
