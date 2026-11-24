package status

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/dev-shetty/wrktree/internal/disk"
	"github.com/dev-shetty/wrktree/internal/git"
	"github.com/dev-shetty/wrktree/internal/testrepo"
)

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

func fixedRows(now time.Time) []Row {
	return []Row{
		{WT: git.Worktree{Path: "/w/main", Branch: "master", IsMain: true}, Current: true,
			Changed: 4, ChangedOK: true, Ahead: 2, Behind: 31, DistOK: true,
			LastCommit: now.Add(-21 * 24 * time.Hour), CommitOK: true, Size: 1503238554, SizeState: disk.SizeCached},
		{WT: git.Worktree{Path: "/w/feat", Branch: "feat", Locked: true},
			Changed: 0, ChangedOK: true, Merge: git.MergeMerged, LastCommit: now.Add(-2 * time.Hour), CommitOK: true,
			Size: 2048, SizeState: disk.SizeFresh},
		{WT: git.Worktree{Path: "/w/gone", Branch: "gone", Prunable: true}},
		{WT: git.Worktree{Path: "/w/det", Head: "abcdef1234567", Detached: true}, Changed: 1, ChangedOK: true,
			Merge: git.MergeNoCommits},
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
	if got[1]["merged"] != true || got[1]["no_commits"] != false || got[0]["merged"] != nil || got[0]["no_commits"] != nil {
		t.Errorf("merged: row 0 %v, row 1 %v", got[0], got[1])
	}
	if got[3]["merged"] != false || got[3]["no_commits"] != true {
		t.Errorf("no commits: row 3 %v", got[3])
	}
	for _, k := range []string{"merged", "no_commits"} {
		if _, ok := got[0][k]; !ok {
			t.Errorf("no %s field", k)
		}
	}
	if got[0]["base"] != "origin/master" {
		t.Errorf("base: %v", got[0]["base"])
	}
	if _, err := time.Parse(time.RFC3339, got[0]["last_commit"].(string)); err != nil {
		t.Errorf("last_commit: %v", err)
	}
}
