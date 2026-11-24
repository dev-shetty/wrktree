package git

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/dev-shetty/wrktree/internal/testrepo"
)

func open(t *testing.T, dir string) Runner {
	t.Helper()
	r, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestBaseBranch(t *testing.T) {
	ctx := context.Background()
	repo := testrepo.New(t)
	r := open(t, repo.Dir)
	if got, err := r.BaseBranch(ctx, ""); err != nil || got != "origin/master" {
		t.Fatalf("default: got %q, err %v", got, err)
	}
	repo.Git(t, repo.Dir, "remote", "set-head", "origin", "-d")
	if got, err := r.BaseBranch(ctx, ""); err != nil || got != "origin/master" {
		t.Fatalf("fallback: got %q, err %v", got, err)
	}
	if got, err := r.BaseBranch(ctx, "origin/develop"); err != nil || got != "origin/develop" {
		t.Fatalf("override: got %q, err %v", got, err)
	}
	none := testrepo.NewNoOrigin(t)
	if _, err := open(t, none.Dir).BaseBranch(ctx, ""); !errors.Is(err, ErrNoBase) {
		t.Fatalf("no origin: err = %v, want ErrNoBase", err)
	}
}

func TestDistance(t *testing.T) {
	repo := testrepo.New(t)
	feat := repo.AddWorktree(t, "feat")
	repo.Commit(t, feat, "f1.txt")
	repo.Commit(t, feat, "f2.txt")
	for _, f := range []string{"m1.txt", "m2.txt", "m3.txt"} {
		repo.Commit(t, repo.Dir, f)
	}
	repo.Git(t, repo.Dir, "push", "origin", "master")
	ahead, behind, err := open(t, repo.Dir).Distance(context.Background(), feat, "origin/master")
	if err != nil {
		t.Fatal(err)
	}
	if ahead != 2 || behind != 3 {
		t.Errorf("ahead = %d, behind = %d, want 2 and 3", ahead, behind)
	}
}

func TestFetchBase(t *testing.T) {
	ctx := context.Background()
	repo := testrepo.New(t)
	other := filepath.Join(t.TempDir(), "other")
	repo.Git(t, filepath.Dir(other), "clone", repo.Origin, other)
	repo.Commit(t, other, "remote.txt")
	repo.Git(t, other, "push", "origin", "master")

	r := open(t, repo.Dir)
	_, behind, err := r.Distance(ctx, repo.Dir, "origin/master")
	if err != nil || behind != 0 {
		t.Fatalf("before: behind = %d, err = %v", behind, err)
	}
	if err := r.FetchBase(ctx, "origin/master"); err != nil {
		t.Fatal(err)
	}
	_, behind, err = r.Distance(ctx, repo.Dir, "origin/master")
	if err != nil || behind != 1 {
		t.Fatalf("after: behind = %d, err = %v", behind, err)
	}
}

// commitText writes text to file in dir, then stages and commits it.
func commitText(t *testing.T, repo *testrepo.Repo, dir, file, text string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, file), []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	repo.Git(t, dir, "add", file)
	repo.Git(t, dir, "commit", "-m", "write "+file)
}

// squash makes one squash commit of branch on master and pushes it.
func squash(t *testing.T, repo *testrepo.Repo, branch string) {
	t.Helper()
	repo.Git(t, repo.Dir, "merge", "--squash", branch)
	repo.Git(t, repo.Dir, "commit", "-m", "squash "+branch)
	repo.Git(t, repo.Dir, "push", "origin", "master")
}

func merged(t *testing.T, repo *testrepo.Repo, path string) MergeState {
	t.Helper()
	st, err := open(t, repo.Dir).Merged(context.Background(), path, "origin/master")
	if err != nil {
		t.Fatal(err)
	}
	return st
}

func wantState(t *testing.T, name string, got, want MergeState) {
	t.Helper()
	if got != want {
		t.Errorf("%s: state = %d, want %d", name, got, want)
	}
}

func TestMergedSquash(t *testing.T) {
	repo := testrepo.New(t)
	feat := repo.AddWorktree(t, "feat")
	repo.Commit(t, feat, "f1.txt")
	commitText(t, repo, feat, "README.md", "changed\n")
	squash(t, repo, "feat")
	wantState(t, "squash merge", merged(t, repo, feat), MergeMerged)
	if ahead, _, err := open(t, repo.Dir).Distance(context.Background(), feat, "origin/master"); err != nil || ahead == 0 {
		t.Errorf("ahead = %d, err = %v; want ahead > 0", ahead, err)
	}
}

func TestMergedMergeCommit(t *testing.T) {
	repo := testrepo.New(t)
	feat := repo.AddWorktree(t, "feat")
	repo.Commit(t, feat, "f1.txt")
	repo.Commit(t, repo.Dir, "m1.txt")
	repo.Git(t, repo.Dir, "merge", "--no-ff", "-m", "merge feat", "feat")
	repo.Git(t, repo.Dir, "push", "origin", "master")
	wantState(t, "merge commit", merged(t, repo, feat), MergeMerged)
}

func TestMergedUnmerged(t *testing.T) {
	repo := testrepo.New(t)
	feat := repo.AddWorktree(t, "feat")
	repo.Commit(t, feat, "f1.txt")
	repo.Commit(t, repo.Dir, "m1.txt")
	repo.Git(t, repo.Dir, "push", "origin", "master")
	wantState(t, "unmerged", merged(t, repo, feat), MergeNone)
}

// A new branch from an old master commit has no commits of its own. An
// untracked file does not change that.
func TestMergedNoCommits(t *testing.T) {
	repo := testrepo.New(t)
	repo.Commit(t, repo.Dir, "m0.txt")
	old := repo.Git(t, repo.Dir, "rev-parse", "HEAD")
	repo.Commit(t, repo.Dir, "m1.txt")
	repo.Commit(t, repo.Dir, "m2.txt")
	repo.Git(t, repo.Dir, "push", "origin", "master")
	feat := filepath.Join(filepath.Dir(repo.Dir), "feat")
	repo.Git(t, repo.Dir, "worktree", "add", "-b", "feat", feat, old)
	wantState(t, "new branch", merged(t, repo, feat), MergeNoCommits)
	if err := os.WriteFile(filepath.Join(feat, "untracked.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	wantState(t, "new branch with an untracked file", merged(t, repo, feat), MergeNoCommits)
}
