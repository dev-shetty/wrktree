package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/dev-shetty/wrktree/internal/testrepo"
)

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

func TestSyncCommand(t *testing.T) {
	repo, feat := twoWorktrees(t)
	tip := pushMaster(t, repo, "m1.txt", "master\n")
	if _, _, err := run(t, repo.Dir, "sync", "feat-a"); err != nil {
		t.Fatal(err)
	}
	if h := repo.Git(t, feat, "rev-parse", "HEAD"); h != tip {
		t.Errorf("HEAD = %s, want %s", h, tip)
	}
	if _, _, err := run(t, repo.Dir, "sync", "feat-a"); err != nil {
		t.Errorf("second sync: %v", err)
	}
}

func TestSyncCommandConflict(t *testing.T) {
	repo, feat := twoWorktrees(t)
	for _, f := range []string{"a.txt", "b.txt", "c.txt", "d.txt", "e.txt"} {
		if err := os.WriteFile(filepath.Join(feat, f), []byte("feat\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		repo.Git(t, feat, "add", f)
	}
	repo.Git(t, feat, "commit", "-m", "feat files")
	for _, f := range []string{"a.txt", "b.txt", "c.txt", "d.txt", "e.txt"} {
		if err := os.WriteFile(filepath.Join(repo.Dir, f), []byte("master\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		repo.Git(t, repo.Dir, "add", f)
	}
	repo.Git(t, repo.Dir, "commit", "-m", "master files")
	repo.Git(t, repo.Dir, "push", "origin", "master")
	before := repo.Git(t, feat, "rev-parse", "HEAD")

	if _, _, err := run(t, repo.Dir, "sync", "feat-a"); err == nil {
		t.Error("want an error for a conflict")
	}
	if h := repo.Git(t, feat, "rev-parse", "HEAD"); h != before {
		t.Errorf("HEAD moved")
	}
	if s := repo.Git(t, feat, "status", "--porcelain"); s != "" {
		t.Errorf("status %q", s)
	}
}
