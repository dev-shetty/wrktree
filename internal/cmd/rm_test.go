package cmd

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dev-shetty/wrktree/internal/disk"
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
	out, stderr, err := runIn(t, repo.Dir, "y\n", false, "rm", "feat-a")
	if err != nil {
		t.Fatal(err)
	}
	if exists(feat) {
		t.Error("folder still exists")
	}
	if out != "deleted "+feat+"\n" {
		t.Errorf("stdout = %q", out)
	}
	want := "Delete worktree feat-a at " + feat + "? Ignored files are deleted too. [y/N] "
	if stderr != want {
		t.Errorf("stderr = %q, want %q", stderr, want)
	}
	if !strings.Contains(branches(t, repo), "feat-a") {
		t.Error("branch was deleted without --branch")
	}
}

func TestRmCleanAnswers(t *testing.T) {
	for _, tc := range []struct {
		stdin string
		gone  bool
	}{
		{"Y\n", true}, {"YES\n", true}, {" yes \n", true}, {"yes\r\n", true}, {"y", true},
		{"n\n", false}, {"\n", false}, {"ye\n", false}, {"yess\n", false}, {"n", false},
	} {
		isolate(t)
		repo, feat := twoWorktrees(t)
		_, stderr, err := runIn(t, repo.Dir, tc.stdin, false, "rm", "feat-a")
		if err != nil {
			t.Fatalf("%q: %v", tc.stdin, err)
		}
		if exists(feat) == tc.gone {
			t.Errorf("stdin %q: folder exists = %v", tc.stdin, exists(feat))
		}
		if !tc.gone && !strings.HasSuffix(stderr, "cancelled\n") {
			t.Errorf("stdin %q: stderr %q", tc.stdin, stderr)
		}
	}
}

func TestRmCancel(t *testing.T) {
	isolate(t)
	repo, feat := twoWorktrees(t)
	out, stderr, err := runIn(t, repo.Dir, "n\n", false, "rm", "feat-a")
	if err != nil {
		t.Fatal(err)
	}
	if !exists(feat) || out != "" || !strings.Contains(stderr, "cancelled") {
		t.Errorf("exists %v, stdout %q, stderr %q", exists(feat), out, stderr)
	}
}

func TestRmDirtyNeedsYes(t *testing.T) {
	isolate(t)
	repo, feat := twoWorktrees(t)
	dirty(t, feat)

	_, stderr, err := runIn(t, repo.Dir, "y\n", true, "rm", "feat-a")
	if err != nil {
		t.Fatal(err)
	}
	if !exists(feat) {
		t.Fatal("a plain y deleted a dirty worktree")
	}
	want := "feat-a has 1 changed file. They will be lost. Ignored files are deleted too. Type yes to delete: "
	if !strings.HasPrefix(stderr, want) || !strings.Contains(stderr, "cancelled") {
		t.Errorf("stderr = %q", stderr)
	}

	if _, _, err := runIn(t, repo.Dir, "YES\n", true, "rm", "feat-a"); err != nil || !exists(feat) {
		t.Errorf("YES must not delete: err %v, exists %v", err, exists(feat))
	}

	out, _, err := runIn(t, repo.Dir, "yes\n", true, "rm", "feat-a")
	if err != nil {
		t.Fatal(err)
	}
	if exists(feat) || out != "deleted "+feat+"\n" {
		t.Errorf("exists %v, stdout %q", exists(feat), out)
	}
}

func TestRmDirtyPlural(t *testing.T) {
	isolate(t)
	repo, feat := twoWorktrees(t)
	dirty(t, feat)
	repo.Commit(t, feat, "two.txt")
	if err := os.WriteFile(filepath.Join(feat, "README.md"), []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	dirty(t, feat)
	if err := os.WriteFile(filepath.Join(feat, "other.txt"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, stderr, _ := runIn(t, repo.Dir, "no\n", true, "rm", "feat-a")
	if !strings.Contains(stderr, "feat-a has 3 changed files. They will be lost.") {
		t.Errorf("stderr = %q", stderr)
	}
}

func TestRmDirtyNoTerminal(t *testing.T) {
	isolate(t)
	repo, feat := twoWorktrees(t)
	dirty(t, feat)
	_, stderr, err := runIn(t, repo.Dir, "yes\n", false, "rm", "feat-a")
	if err == nil || err.Error() != "worktree has changes: use --force" {
		t.Errorf("err = %v", err)
	}
	if !exists(feat) || stderr != "" {
		t.Errorf("exists %v, stderr %q", exists(feat), stderr)
	}
}

// EOF with no answer cancels with exit code 1.
func TestRmEOF(t *testing.T) {
	for _, d := range []bool{false, true} {
		isolate(t)
		repo, feat := twoWorktrees(t)
		if d {
			dirty(t, feat)
		}
		_, stderr, err := runIn(t, repo.Dir, "", true, "rm", "feat-a")
		if err == nil || !exists(feat) || !strings.HasSuffix(stderr, "\ncancelled\n") {
			t.Errorf("dirty %v: err %v, exists %v, stderr %q", d, err, exists(feat), stderr)
		}
		if strings.Contains(stderr, "wrktree:") {
			t.Errorf("dirty %v: stderr has an error line: %q", d, stderr)
		}
	}
}

// A read error cancels with exit code 1.
func TestRmReadError(t *testing.T) {
	isolate(t)
	repo, feat := twoWorktrees(t)
	var stdout, stderr bytes.Buffer
	c := New(Env{
		Dir: repo.Dir, Stdin: errReader{}, Stdout: &stdout, Stderr: &stderr,
		IsTerminal: func() bool { return true },
	})
	c.SetArgs([]string{"rm", "feat-a"})
	if err := c.Execute(); err == nil || !exists(feat) || !strings.HasSuffix(stderr.String(), "cancelled\n") {
		t.Errorf("err %v, exists %v, stderr %q", err, exists(feat), stderr.String())
	}
}

type errReader struct{}

func (errReader) Read([]byte) (int, error) { return 0, errors.New("read failed") }

func TestRmForce(t *testing.T) {
	isolate(t)
	repo, feat := twoWorktrees(t)
	dirty(t, feat)
	out, stderr, err := runIn(t, repo.Dir, "", false, "rm", "feat-a", "--force")
	if err != nil {
		t.Fatal(err)
	}
	if exists(feat) || out != "deleted "+feat+"\n" || stderr != "" {
		t.Errorf("exists %v, stdout %q, stderr %q", exists(feat), out, stderr)
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
	if err == nil || !strings.Contains(err.Error(), "not fully merged") {
		t.Errorf("err = %v", err)
	}
	if exists(feat2) {
		t.Error("worktree was not deleted")
	}
	if !strings.Contains(branches(t, repo), "feat-b") {
		t.Error("unmerged branch was deleted")
	}
}

func TestRmMain(t *testing.T) {
	isolate(t)
	repo, _ := twoWorktrees(t)
	_, _, err := runIn(t, repo.Dir, "yes\n", true, "rm", "master", "--force")
	if err == nil || err.Error() != "the main worktree cannot be deleted" {
		t.Errorf("err = %v", err)
	}
	if !exists(repo.Dir) {
		t.Error("main folder is gone")
	}
}

func TestRmLocked(t *testing.T) {
	isolate(t)
	repo, feat := twoWorktrees(t)
	repo.Git(t, repo.Dir, "worktree", "lock", "--reason", "busy", feat)
	_, _, err := runIn(t, repo.Dir, "yes\n", true, "rm", "feat-a", "--force")
	if err == nil || !strings.Contains(err.Error(), "worktree is locked") {
		t.Errorf("err = %v", err)
	}
	if !exists(feat) {
		t.Error("locked folder is gone")
	}
}

func TestRmMissingFolder(t *testing.T) {
	isolate(t)
	repo, feat := twoWorktrees(t)
	if err := os.RemoveAll(feat); err != nil {
		t.Fatal(err)
	}
	_, _, err := runIn(t, repo.Dir, "yes\n", true, "rm", "feat-a", "--force")
	if err == nil || err.Error() != "folder is missing: use prune" {
		t.Errorf("err = %v", err)
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
		if err == nil || err.Error() != "cannot delete the worktree of the current directory" {
			t.Errorf("%v: err = %v", args, err)
		}
		if !exists(feat) {
			t.Errorf("%v: folder is gone", args)
		}
	}
	_ = repo
}

func TestRmNoMatch(t *testing.T) {
	isolate(t)
	repo, _ := twoWorktrees(t)
	_, _, err := runIn(t, repo.Dir, "y\n", false, "rm", "zzz")
	if err == nil || !strings.Contains(err.Error(), `no worktree matches "zzz"`) {
		t.Errorf("err = %v", err)
	}
}

// detached adds a detached worktree with n commits that no branch contains.
func detached(t *testing.T, repo *testrepo.Repo, n int) (path, head string) {
	t.Helper()
	path = filepath.Join(filepath.Dir(repo.Dir), "det")
	repo.Git(t, repo.Dir, "worktree", "add", "--detach", path)
	for i := 0; i < n; i++ {
		repo.Commit(t, path, "d"+string(rune('a'+i))+".txt")
	}
	if real, err := filepath.EvalSymlinks(path); err == nil {
		path = real
	}
	return path, repo.Git(t, path, "rev-parse", "HEAD")
}

func TestRmDetached(t *testing.T) {
	isolate(t)
	repo := testrepo.New(t)
	path, head := detached(t, repo, 2)
	_, stderr, err := runIn(t, repo.Dir, "no\n", true, "rm", filepath.Base(path))
	if err != nil {
		t.Fatal(err)
	}
	want := head[:7] + " has 2 commits that no branch contains. They will be lost. Ignored files are deleted too. Type yes to delete: "
	if !strings.HasPrefix(stderr, want) || !exists(path) {
		t.Errorf("stderr = %q, exists %v", stderr, exists(path))
	}
}

func TestRmDetachedOneCommitAndChanges(t *testing.T) {
	isolate(t)
	repo := testrepo.New(t)
	path, head := detached(t, repo, 1)
	_, stderr, _ := runIn(t, repo.Dir, "no\n", true, "rm", filepath.Base(path))
	want := head[:7] + " has 1 commit that no branch contains. They will be lost. Ignored files are deleted too. Type yes to delete: "
	if !strings.HasPrefix(stderr, want) {
		t.Errorf("one commit: stderr = %q", stderr)
	}

	dirty(t, path)
	_, stderr, _ = runIn(t, repo.Dir, "no\n", true, "rm", filepath.Base(path))
	want = head[:7] + " has 1 changed file and 1 commit that no branch contains. They will be lost. Ignored files are deleted too. Type yes to delete: "
	if !strings.HasPrefix(stderr, want) {
		t.Errorf("both: stderr = %q", stderr)
	}

	out, _, err := runIn(t, repo.Dir, "yes\n", true, "rm", filepath.Base(path))
	if err != nil || exists(path) || out != "deleted "+path+"\n" {
		t.Errorf("err %v, exists %v, stdout %q", err, exists(path), out)
	}
}

func TestRmDetachedClean(t *testing.T) {
	isolate(t)
	repo := testrepo.New(t)
	path, head := detached(t, repo, 0)
	_, stderr, err := runIn(t, repo.Dir, "y\n", false, "rm", filepath.Base(path), "--branch")
	if err != nil || exists(path) {
		t.Fatalf("err %v, exists %v", err, exists(path))
	}
	want := "Delete worktree " + head[:7] + " at " + path + "? Ignored files are deleted too. [y/N] "
	if stderr != want {
		t.Errorf("stderr = %q, want %q", stderr, want)
	}
}

func TestRmDropsCache(t *testing.T) {
	isolate(t)
	repo, feat := twoWorktrees(t)
	if _, _, err := runIn(t, repo.Dir, "", false, "ls"); err != nil {
		t.Fatal(err)
	}
	file := disk.DefaultCachePath()
	before, _ := os.ReadFile(file)
	if !strings.Contains(string(before), feat) {
		t.Fatalf("cache lacks %s:\n%s", feat, before)
	}
	if _, _, err := runIn(t, repo.Dir, "y\n", false, "rm", "feat-a"); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(file)
	if strings.Contains(string(after), feat) || !strings.Contains(string(after), repo.Dir) {
		t.Errorf("cache after rm:\n%s", after)
	}
}

func TestRmCacheFailureDoesNotFail(t *testing.T) {
	isolate(t)
	repo, feat := twoWorktrees(t)
	// The cache directory cannot be created: a file is in the way.
	cacheHome := os.Getenv("XDG_CACHE_HOME")
	if err := os.WriteFile(cacheHome, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, _, err := runIn(t, repo.Dir, "y\n", false, "rm", "feat-a")
	if err != nil || exists(feat) || out != "deleted "+feat+"\n" {
		t.Errorf("err %v, exists %v, stdout %q", err, exists(feat), out)
	}
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

func TestPruneYes(t *testing.T) {
	isolate(t)
	repo, feat := twoWorktrees(t)
	if err := os.RemoveAll(feat); err != nil {
		t.Fatal(err)
	}
	_, stderr, err := runIn(t, repo.Dir, "", false, "prune", "--yes")
	if err != nil || !strings.Contains(stderr, "pruned 1 entries") || strings.Contains(stderr, "[y/N]") {
		t.Errorf("err %v, stderr %q", err, stderr)
	}
	out, _, _ := runIn(t, repo.Dir, "", false, "ls")
	if strings.Contains(out, "feat-a") {
		t.Errorf("entry still listed:\n%s", out)
	}
}

func TestPruneEOFCancels(t *testing.T) {
	isolate(t)
	repo, feat := twoWorktrees(t)
	if err := os.RemoveAll(feat); err != nil {
		t.Fatal(err)
	}
	_, stderr, err := runIn(t, repo.Dir, "", false, "prune")
	if err == nil || !strings.HasSuffix(stderr, "\ncancelled\n") {
		t.Errorf("err %v, stderr %q", err, stderr)
	}
	out, _, _ := runIn(t, repo.Dir, "", false, "ls")
	if !strings.Contains(out, "feat-a") {
		t.Errorf("entry vanished:\n%s", out)
	}
}

// nestedRepo adds a worktree "outer" and a worktree "nested" inside it. An
// ignore rule hides the nested folder, so the status of outer is clean.
func nestedRepo(t *testing.T) (repo *testrepo.Repo, outer, nested string) {
	t.Helper()
	repo = testrepo.New(t)
	outer = repo.AddWorktree(t, "outer")
	if err := os.WriteFile(filepath.Join(outer, ".gitignore"), []byte("nested/\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	repo.Git(t, outer, "add", ".gitignore")
	repo.Git(t, outer, "commit", "-m", "ignore nested")
	nested = filepath.Join(outer, "nested")
	repo.Git(t, repo.Dir, "worktree", "add", "-b", "nested", nested)
	return repo, outer, nested
}

func TestRmNestedRefused(t *testing.T) {
	for _, args := range [][]string{{"rm", "outer"}, {"rm", "outer", "--force"}} {
		isolate(t)
		repo, outer, nested := nestedRepo(t)
		_, _, err := runIn(t, repo.Dir, "y\n", true, args...)
		if err == nil || err.Error() != "contains worktree nested: delete it first" {
			t.Errorf("%v: err = %v", args, err)
		}
		if !exists(outer) || !exists(nested) {
			t.Errorf("%v: outer exists %v, nested exists %v", args, exists(outer), exists(nested))
		}
	}
}

// From inside a nested worktree, the outer worktree holds the current
// directory too.
func TestRmOuterFromNested(t *testing.T) {
	isolate(t)
	_, outer, nested := nestedRepo(t)
	_, _, err := runIn(t, nested, "yes\n", true, "rm", "outer", "--force")
	if err == nil {
		t.Error("err = nil")
	}
	if !exists(outer) || !exists(nested) {
		t.Errorf("outer exists %v, nested exists %v", exists(outer), exists(nested))
	}
}
