package git

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/dev-shetty/wrktree/internal/testrepo"
)

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
