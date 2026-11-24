package git

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dev-shetty/wrktree/internal/testrepo"
)

const syncBase = "origin/master"

// advanceMaster commits file with text on master and pushes it to origin.
// Then it moves the local origin/master back, so that only a fetch gets the
// new commit. It returns the new tip.
func advanceMaster(t *testing.T, repo *testrepo.Repo, file, text string) string {
	t.Helper()
	old := repo.Git(t, repo.Dir, "rev-parse", "refs/remotes/origin/master")
	writeFile(t, filepath.Join(repo.Dir, file), text)
	repo.Git(t, repo.Dir, "add", file)
	repo.Git(t, repo.Dir, "commit", "-m", "master "+file)
	repo.Git(t, repo.Dir, "push", "origin", "master")
	repo.Git(t, repo.Dir, "update-ref", "refs/remotes/origin/master", old)
	return repo.Git(t, repo.Dir, "rev-parse", "master")
}

// commitFile writes file with text in dir and commits it.
func commitFile(t *testing.T, repo *testrepo.Repo, dir, file, text string) {
	t.Helper()
	writeFile(t, filepath.Join(dir, file), text)
	repo.Git(t, dir, "add", file)
	repo.Git(t, dir, "commit", "-m", "change "+file)
}

func writeFile(t *testing.T, path, text string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// syncFeat adds the worktree feat and returns the runner and the worktree.
func syncFeat(t *testing.T) (*testrepo.Repo, Runner, Worktree) {
	t.Helper()
	repo := testrepo.New(t)
	repo.AddWorktree(t, "feat")
	r := Runner{Dir: repo.Dir}
	return repo, r, find(t, r, "feat")
}

func head(t *testing.T, repo *testrepo.Repo, dir string) string {
	t.Helper()
	return repo.Git(t, dir, "rev-parse", "HEAD")
}

// mergeHead reports whether MERGE_HEAD exists in the worktree at dir.
func mergeHead(t *testing.T, repo *testrepo.Repo, dir string) bool {
	t.Helper()
	p := repo.Git(t, dir, "rev-parse", "--git-path", "MERGE_HEAD")
	if !filepath.IsAbs(p) {
		p = filepath.Join(dir, p)
	}
	return exists(p)
}

// wantRefused fails the test unless err is a refusal with the text want.
func wantRefused(t *testing.T, err error, want string) {
	t.Helper()
	if err == nil || !errors.Is(err, ErrSyncRefused) || err.Error() != want {
		t.Errorf("err = %v, want refusal %q", err, want)
	}
}

func TestSyncCleanMerge(t *testing.T) {
	repo, r, wt := syncFeat(t)
	commitFile(t, repo, wt.Path, "f1.txt", "feat\n")
	tip := advanceMaster(t, repo, "m1.txt", "master\n")
	res, err := r.Sync(context.Background(), wt, syncBase)
	if err != nil || res.UpToDate || res.FetchFailed || len(res.Conflicts) != 0 {
		t.Fatalf("Sync = %+v, %v", res, err)
	}
	parents := strings.Fields(repo.Git(t, wt.Path, "rev-list", "--parents", "-n", "1", "HEAD"))
	if len(parents) != 3 || parents[2] != tip {
		t.Errorf("HEAD parents = %v, want 2 with the base tip %s", parents[1:], tip)
	}
	if s := repo.Git(t, wt.Path, "status", "--porcelain"); s != "" {
		t.Errorf("status not clean: %q", s)
	}
	if _, behind, err := r.Distance(context.Background(), wt.Path, syncBase); err != nil || behind != 0 {
		t.Errorf("behind = %d, %v", behind, err)
	}
}

func TestSyncFastForward(t *testing.T) {
	repo, r, wt := syncFeat(t)
	tip := advanceMaster(t, repo, "m1.txt", "master\n")
	res, err := r.Sync(context.Background(), wt, syncBase)
	if err != nil || res.UpToDate || res.FetchFailed || len(res.Conflicts) != 0 {
		t.Fatalf("Sync = %+v, %v", res, err)
	}
	if h := head(t, repo, wt.Path); h != tip {
		t.Errorf("HEAD = %s, want the base tip %s", h, tip)
	}
}

func TestSyncUpToDate(t *testing.T) {
	repo, r, wt := syncFeat(t)
	commitFile(t, repo, wt.Path, "f1.txt", "feat\n")
	before := head(t, repo, wt.Path)
	res, err := r.Sync(context.Background(), wt, syncBase)
	if err != nil || !res.UpToDate || res.FetchFailed || len(res.Conflicts) != 0 {
		t.Fatalf("Sync = %+v, %v", res, err)
	}
	if h := head(t, repo, wt.Path); h != before {
		t.Errorf("HEAD moved: %s -> %s", before, h)
	}
}

func TestSyncConflict(t *testing.T) {
	repo, r, wt := syncFeat(t)
	commitFile(t, repo, wt.Path, "README.md", "feat\n")
	advanceMaster(t, repo, "README.md", "master\n")
	before := head(t, repo, wt.Path)
	res, err := r.Sync(context.Background(), wt, syncBase)
	if err != nil || len(res.Conflicts) != 1 || res.Conflicts[0] != "README.md" || res.UpToDate {
		t.Fatalf("Sync = %+v, %v; want a conflict in README.md", res, err)
	}
	if h := head(t, repo, wt.Path); h != before {
		t.Errorf("HEAD moved: %s -> %s", before, h)
	}
	if s := repo.Git(t, wt.Path, "status", "--porcelain"); s != "" {
		t.Errorf("status not clean: %q", s)
	}
	if mergeHead(t, repo, wt.Path) {
		t.Error("MERGE_HEAD exists: the merge was not cancelled")
	}
	if got := readFile(t, filepath.Join(wt.Path, "README.md")); got != "feat\n" {
		t.Errorf("README.md = %q, want %q", got, "feat\n")
	}
}

func TestSyncDirty(t *testing.T) {
	for _, tc := range []struct {
		name, file, text, want string
		tracked                bool
	}{
		{"untracked", "new.txt", "new\n", "has 1 changed file: commit or stash first", false},
		{"tracked", "README.md", "changed\n", "has 1 changed file: commit or stash first", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo, r, wt := syncFeat(t)
			advanceMaster(t, repo, "m1.txt", "master\n")
			before := head(t, repo, wt.Path)
			path := filepath.Join(wt.Path, tc.file)
			writeFile(t, path, tc.text)
			_, err := r.Sync(context.Background(), wt, syncBase)
			wantRefused(t, err, tc.want)
			if h := head(t, repo, wt.Path); h != before {
				t.Errorf("HEAD moved: %s -> %s", before, h)
			}
			if got := readFile(t, path); got != tc.text {
				t.Errorf("%s = %q, want %q", tc.file, got, tc.text)
			}
		})
	}
	repo, r, wt := syncFeat(t)
	writeFile(t, filepath.Join(wt.Path, "a.txt"), "a\n")
	writeFile(t, filepath.Join(wt.Path, "b.txt"), "b\n")
	_, err := r.Sync(context.Background(), wt, syncBase)
	wantRefused(t, err, "has 2 changed files: commit or stash first")
	_ = repo
}

// A merge that the user started stays: Sync must not cancel it.
func TestSyncMergeInProgress(t *testing.T) {
	repo, r, wt := syncFeat(t)
	commitFile(t, repo, wt.Path, "f1.txt", "feat\n")
	advanceMaster(t, repo, "m1.txt", "master\n")
	repo.Git(t, wt.Path, "fetch", "origin", "master")
	repo.Git(t, wt.Path, "merge", "--no-commit", "--no-ff", syncBase)
	if !mergeHead(t, repo, wt.Path) {
		t.Fatal("the test merge left no MERGE_HEAD")
	}
	_, err := r.Sync(context.Background(), wt, syncBase)
	wantRefused(t, err, "merge or rebase in progress: finish it first")
	if !mergeHead(t, repo, wt.Path) {
		t.Error("MERGE_HEAD is gone: Sync cancelled the merge of the user")
	}
}

func TestSyncDetached(t *testing.T) {
	repo := testrepo.New(t)
	repo.Git(t, repo.Dir, "worktree", "add", "--detach", filepath.Join(filepath.Dir(repo.Dir), "det"))
	r := Runner{Dir: repo.Dir}
	_, err := r.Sync(context.Background(), find(t, r, "det"), syncBase)
	wantRefused(t, err, "detached worktree: no branch to update")
}

func TestSyncMissingFolder(t *testing.T) {
	_, r, wt := syncFeat(t)
	if err := os.RemoveAll(wt.Path); err != nil {
		t.Fatal(err)
	}
	_, err := r.Sync(context.Background(), find(t, r, "feat"), syncBase)
	wantRefused(t, err, "folder is missing: use prune")
}

func TestSyncBare(t *testing.T) {
	_, err := Runner{Dir: t.TempDir()}.Sync(context.Background(), Worktree{Path: "/x", Bare: true}, syncBase)
	wantRefused(t, err, "bare repository")
}

func TestSyncNoBase(t *testing.T) {
	repo := testrepo.NewNoOrigin(t)
	repo.AddWorktree(t, "feat")
	r := Runner{Dir: repo.Dir}
	_, err := r.Sync(context.Background(), find(t, r, "feat"), "")
	wantRefused(t, err, "no base branch")
}

// If the fetch fails, the merge uses the local copy of the base.
func TestSyncFetchFailed(t *testing.T) {
	repo, r, wt := syncFeat(t)
	commitFile(t, repo, wt.Path, "f1.txt", "feat\n")
	tip := advanceMaster(t, repo, "m1.txt", "master\n")
	repo.Git(t, repo.Dir, "update-ref", "refs/remotes/origin/master", tip)
	if err := os.Rename(repo.Origin, repo.Origin+".gone"); err != nil {
		t.Fatal(err)
	}
	res, err := r.Sync(context.Background(), wt, syncBase)
	if err != nil || !res.FetchFailed || res.UpToDate || len(res.Conflicts) != 0 {
		t.Fatalf("Sync = %+v, %v", res, err)
	}
	if out := repo.Git(t, wt.Path, "merge-base", "--is-ancestor", tip, "HEAD"); out != "" {
		t.Errorf("merge-base output %q", out)
	}
}

// A hook that refuses the merge commit gives an error with the git message.
// The merge is cancelled, and the worktree is as before.
func TestSyncHookRefuses(t *testing.T) {
	repo, r, wt := syncFeat(t)
	commitFile(t, repo, wt.Path, "f1.txt", "feat\n")
	advanceMaster(t, repo, "m1.txt", "master\n")
	hook := filepath.Join(repo.Dir, ".git", "hooks", "pre-merge-commit")
	if err := os.MkdirAll(filepath.Dir(hook), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(hook, []byte("#!/bin/sh\necho hook says no >&2\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	before := head(t, repo, wt.Path)
	res, err := r.Sync(context.Background(), wt, syncBase)
	if err == nil || errors.Is(err, ErrSyncRefused) || !strings.Contains(err.Error(), "hook says no") {
		t.Fatalf("Sync = %+v, %v; want the hook message", res, err)
	}
	if h := head(t, repo, wt.Path); h != before {
		t.Errorf("HEAD moved: %s -> %s", before, h)
	}
	if mergeHead(t, repo, wt.Path) {
		t.Error("MERGE_HEAD exists")
	}
	if s := repo.Git(t, wt.Path, "status", "--porcelain"); s != "" {
		t.Errorf("status not clean: %q", s)
	}
}

func TestConflictText(t *testing.T) {
	for _, tc := range []struct {
		files []string
		want  string
	}{
		{[]string{"a"}, "a"},
		{[]string{"a", "b", "c"}, "a, b, c"},
		{[]string{"a", "b", "c", "d"}, "a, b, c and 1 more"},
		{[]string{"a", "b", "c", "d", "e"}, "a, b, c and 2 more"},
	} {
		if got := ConflictText(tc.files); got != tc.want {
			t.Errorf("ConflictText(%v) = %q, want %q", tc.files, got, tc.want)
		}
	}
}

func TestMergeEnv(t *testing.T) {
	base := []string{"HOME=/h", "GIT_TERMINAL_PROMPT=1", "GIT_MERGE_AUTOEDIT=yes"}
	for _, args := range [][]string{{"merge", "--no-edit", syncBase}, {"merge", "--abort"}} {
		env := gitEnv(base, args)
		if v, _ := envValue(env, "GIT_TERMINAL_PROMPT"); v != "0" {
			t.Errorf("%v: GIT_TERMINAL_PROMPT = %q", args, v)
		}
		if v, _ := envValue(env, "GIT_MERGE_AUTOEDIT"); v != "no" {
			t.Errorf("%v: GIT_MERGE_AUTOEDIT = %q", args, v)
		}
		c := Runner{Dir: "/r"}.command(context.Background(), args...)
		if c.Args[3] == "--no-optional-locks" {
			t.Errorf("%v: a write call gets --no-optional-locks", args)
		}
		if c.SysProcAttr != nil {
			t.Errorf("%v: merge runs in a new session", args)
		}
	}
}
