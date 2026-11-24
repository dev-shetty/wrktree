package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dev-shetty/wrktree/internal/testrepo"
)

// isolate points the configuration and the cache at temporary directories.
func isolate(t *testing.T) {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(t.TempDir(), "config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(t.TempDir(), "cache"))
}

// runIn runs one command with the given stdin text and terminal flag.
func runIn(t *testing.T, dir, stdin string, terminal bool, args ...string) (string, string, error) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	c := New(Env{
		Dir:        dir,
		Stdin:      strings.NewReader(stdin),
		Stdout:     &stdout,
		Stderr:     &stderr,
		IsTerminal: func() bool { return terminal },
	})
	c.SetArgs(args)
	err := c.Execute()
	return stdout.String(), stderr.String(), err
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func branches(t *testing.T, repo *testrepo.Repo) string {
	t.Helper()
	return repo.Git(t, repo.Dir, "branch", "--format=%(refname:short)")
}

func dirty(t *testing.T, path string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(path, "scratch.txt"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestRmClean(t *testing.T) {
	isolate(t)
	repo, feat := twoWorktrees(t)
	_, _, err := runIn(t, repo.Dir, "y\n", false, "rm", "feat-a")
	if err != nil {
		t.Fatal(err)
	}
	if exists(feat) {
		t.Error("folder still exists")
	}
	if !strings.Contains(branches(t, repo), "feat-a") {
		t.Error("branch was deleted without --branch")
	}
}

func TestRmDirtyNeedsYes(t *testing.T) {
	isolate(t)
	repo, feat := twoWorktrees(t)
	dirty(t, feat)

	_, _, err := runIn(t, repo.Dir, "y\n", true, "rm", "feat-a")
	if err != nil {
		t.Fatal(err)
	}
	if !exists(feat) {
		t.Fatal("a plain y deleted a dirty worktree")
	}

	if _, _, err := runIn(t, repo.Dir, "YES\n", true, "rm", "feat-a"); err != nil || !exists(feat) {
		t.Errorf("YES must not delete: err %v, exists %v", err, exists(feat))
	}

	_, _, err = runIn(t, repo.Dir, "yes\n", true, "rm", "feat-a")
	if err != nil {
		t.Fatal(err)
	}
	if exists(feat) {
		t.Error("yes did not delete the worktree")
	}
}

func TestRmForce(t *testing.T) {
	isolate(t)
	repo, feat := twoWorktrees(t)
	dirty(t, feat)
	_, _, err := runIn(t, repo.Dir, "", false, "rm", "feat-a", "--force")
	if err != nil {
		t.Fatal(err)
	}
	if exists(feat) {
		t.Error("--force did not delete the worktree")
	}
}

func TestRmBranch(t *testing.T) {
	isolate(t)
	repo, feat := twoWorktrees(t)
	if _, _, err := runIn(t, repo.Dir, "y\n", false, "rm", "feat-a", "--branch"); err != nil {
		t.Fatal(err)
	}
	if exists(feat) || strings.Contains(branches(t, repo), "feat-a") {
		t.Errorf("folder %v, branches %q", exists(feat), branches(t, repo))
	}

	// An unmerged branch: the worktree is deleted, the branch stays.
	feat2 := repo.AddWorktree(t, "feat-b")
	repo.Commit(t, feat2, "b.txt")
	_, _, err := runIn(t, repo.Dir, "y\n", false, "rm", "feat-b", "--branch")
	if err == nil {
		t.Errorf("err = %v", err)
	}
	if exists(feat2) {
		t.Error("worktree was not deleted")
	}
	if !strings.Contains(branches(t, repo), "feat-b") {
		t.Error("unmerged branch was deleted")
	}
}

func TestRmCurrent(t *testing.T) {
	isolate(t)
	repo, feat := twoWorktrees(t)
	sub := filepath.Join(feat, "sub")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"rm", "feat-a"}, {"rm", "feat-a", "--force"}} {
		_, _, err := runIn(t, sub, "yes\n", true, args...)
		if err == nil {
			t.Errorf("%v: err = %v", args, err)
		}
		if !exists(feat) {
			t.Errorf("%v: folder is gone", args)
		}
	}
	_ = repo
}

func TestPrune(t *testing.T) {
	isolate(t)
	repo, feat := twoWorktrees(t)

	_, stderr, err := runIn(t, repo.Dir, "y\n", false, "prune")
	if err != nil || !strings.Contains(stderr, "nothing to prune") {
		t.Errorf("err %v, stderr %q", err, stderr)
	}

	if err := os.RemoveAll(feat); err != nil {
		t.Fatal(err)
	}
	_, stderr, err = runIn(t, repo.Dir, "n\n", false, "prune")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stderr, "Prune 1 entries? [y/N] ") || strings.Contains(stderr, "pruned") {
		t.Errorf("stderr = %q", stderr)
	}
	out, _, _ := runIn(t, repo.Dir, "", false, "ls")
	if !strings.Contains(out, "feat-a") {
		t.Fatalf("entry vanished after a no:\n%s", out)
	}

	_, stderr, err = runIn(t, repo.Dir, "y\n", false, "prune")
	if err != nil || !strings.Contains(stderr, "pruned 1 entries") {
		t.Errorf("err %v, stderr %q", err, stderr)
	}
	out, _, _ = runIn(t, repo.Dir, "", false, "ls")
	if strings.Contains(out, "feat-a") {
		t.Errorf("entry still listed:\n%s", out)
	}
}
