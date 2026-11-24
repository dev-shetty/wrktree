package git

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

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

func TestChangedFiles(t *testing.T) {
	repo := testrepo.New(t)
	r := open(t, repo.Dir)
	ctx := context.Background()
	n, err := r.ChangedFiles(ctx, repo.Dir)
	if err != nil || n != 0 {
		t.Fatalf("clean: n = %d, err = %v", n, err)
	}
	if err := os.WriteFile(filepath.Join(repo.Dir, "new.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo.Dir, "README.md"), []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	n, err = r.ChangedFiles(ctx, repo.Dir)
	if err != nil || n != 2 {
		t.Fatalf("dirty: n = %d, err = %v", n, err)
	}
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

func TestLastCommit(t *testing.T) {
	repo := testrepo.New(t)
	got, err := open(t, repo.Dir).LastCommit(context.Background(), repo.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if d := time.Since(got); d < -time.Minute || d > time.Minute {
		t.Errorf("LastCommit = %v, not within 1 minute of now", got)
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
