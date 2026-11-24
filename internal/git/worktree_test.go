package git

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/dev-shetty/wrktree/internal/testrepo"
)

func TestParseWorktrees(t *testing.T) {
	in := "worktree /r/main\nHEAD aaa\nbranch refs/heads/master\n\n" +
		"worktree /r/feat\nHEAD bbb\nbranch refs/heads/eng-1-feat\nlocked reason text\n\n" +
		"worktree /r/det\nHEAD ccc\ndetached\nprunable gitdir file points to non-existent location\n"
	got := ParseWorktrees(in)
	if len(got) != 3 {
		t.Fatalf("len = %d, want 3", len(got))
	}
	if g := got[0]; g.Path != "/r/main" || g.Branch != "master" || g.Head != "aaa" || !g.IsMain {
		t.Errorf("got[0] = %+v", g)
	}
	if g := got[1]; g.Branch != "eng-1-feat" || !g.Locked || g.LockReason != "reason text" || g.IsMain {
		t.Errorf("got[1] = %+v", g)
	}
	if g := got[2]; !g.Detached || g.Branch != "" || !g.Prunable || g.IsMain {
		t.Errorf("got[2] = %+v", g)
	}
}

func TestParseWorktreesBare(t *testing.T) {
	got := ParseWorktrees("worktree /r/repo.git\nbare\n\nworktree /r/a\nHEAD aaa\nbranch refs/heads/a\n")
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
	if !got[0].Bare || got[0].IsMain || got[1].IsMain {
		t.Errorf("got = %+v", got)
	}
}

func realPath(t *testing.T, p string) string {
	t.Helper()
	r, err := filepath.EvalSymlinks(p)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestWorktreesReal(t *testing.T) {
	repo := testrepo.New(t)
	feat := repo.AddWorktree(t, "feat-a")
	r, err := Open(repo.Dir)
	if err != nil {
		t.Fatal(err)
	}
	got, err := r.Worktrees(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
	if !got[0].IsMain || realPath(t, got[0].Path) != realPath(t, repo.Dir) {
		t.Errorf("got[0] = %+v", got[0])
	}
	if got[1].Branch != "feat-a" || realPath(t, got[1].Path) != realPath(t, feat) {
		t.Errorf("got[1] = %+v", got[1])
	}
}

func TestOpenNotRepo(t *testing.T) {
	_, err := Open(t.TempDir())
	if !errors.Is(err, ErrNotRepo) {
		t.Fatalf("err = %v, want ErrNotRepo", err)
	}
}

func TestRepoName(t *testing.T) {
	repo := testrepo.New(t)
	r, err := Open(repo.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := r.RepoName(context.Background()); got != "main" {
		t.Errorf("RepoName = %q, want main", got)
	}
}
