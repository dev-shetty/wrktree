package cmd

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dev-shetty/wrktree/internal/git"
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
	out, stderr, err := run(t, repo.Dir, "sync", "feat-a")
	if err != nil || out != "synced feat-a with origin/master\n" || stderr != "" {
		t.Fatalf("err %v, stdout %q, stderr %q", err, out, stderr)
	}
	if h := repo.Git(t, feat, "rev-parse", "HEAD"); h != tip {
		t.Errorf("HEAD = %s, want %s", h, tip)
	}
	out, _, err = run(t, repo.Dir, "sync", "feat-a")
	if err != nil || out != "feat-a is already up to date with origin/master\n" {
		t.Errorf("second sync: err %v, stdout %q", err, out)
	}
}

func TestSyncCommandFetchFailed(t *testing.T) {
	repo, _ := twoWorktrees(t)
	pushMaster(t, repo, "m1.txt", "master\n")
	if err := os.Rename(repo.Origin, repo.Origin+".gone"); err != nil {
		t.Fatal(err)
	}
	out, stderr, err := run(t, repo.Dir, "sync", "feat-a")
	if err != nil || out != "synced feat-a with origin/master\n" {
		t.Errorf("err %v, stdout %q", err, out)
	}
	if stderr != "wrktree: fetch failed: used the local copy of origin/master\n" {
		t.Errorf("stderr %q", stderr)
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

	out, _, err := run(t, repo.Dir, "sync", "feat-a")
	if err == nil || err.Error() != "sync failed: conflict in a.txt, b.txt, c.txt and 2 more" || out != "" {
		t.Errorf("err %v, stdout %q", err, out)
	}
	if h := repo.Git(t, feat, "rev-parse", "HEAD"); h != before {
		t.Errorf("HEAD moved")
	}
	if s := repo.Git(t, feat, "status", "--porcelain"); s != "" {
		t.Errorf("status %q", s)
	}
}

func TestSyncCommandRefusals(t *testing.T) {
	repo, feat := twoWorktrees(t)
	pushMaster(t, repo, "m1.txt", "master\n")
	if err := os.WriteFile(filepath.Join(feat, "new.txt"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, _, err := run(t, repo.Dir, "sync", "feat-a")
	if err == nil || err.Error() != "has 1 changed file: commit or stash first" || !errors.Is(err, git.ErrSyncRefused) || out != "" {
		t.Errorf("dirty: err %v, stdout %q", err, out)
	}

	det := filepath.Join(filepath.Dir(repo.Dir), "det")
	repo.Git(t, repo.Dir, "worktree", "add", "--detach", det)
	if _, _, err := run(t, repo.Dir, "sync", "det"); err == nil || err.Error() != "detached worktree: no branch to update" {
		t.Errorf("detached: err %v", err)
	}

	if err := os.RemoveAll(feat); err != nil {
		t.Fatal(err)
	}
	if _, _, err := run(t, repo.Dir, "sync", "feat-a"); err == nil || err.Error() != "folder is missing: use prune" {
		t.Errorf("missing: err %v", err)
	}

	noOrigin := testrepo.NewNoOrigin(t)
	noOrigin.AddWorktree(t, "feat-b")
	if _, _, err := run(t, noOrigin.Dir, "sync", "feat-b"); err == nil || err.Error() != "no base branch" {
		t.Errorf("no base: err %v", err)
	}

	if _, _, err := run(t, repo.Dir, "sync"); err == nil {
		t.Error("sync with no name: no error")
	}
	if _, _, err := run(t, repo.Dir, "sync", "nothing-matches"); err == nil || !strings.Contains(err.Error(), "no worktree matches") {
		t.Errorf("no match: err %v", err)
	}
}
