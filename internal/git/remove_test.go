package git

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dev-shetty/wrktree/internal/testrepo"
)

// find returns the worktree whose folder name is name.
func find(t *testing.T, r Runner, name string) Worktree {
	t.Helper()
	wts, err := r.Worktrees(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, w := range wts {
		if filepath.Base(w.Path) == name {
			return w
		}
	}
	t.Fatalf("no worktree named %q in %+v", name, wts)
	return Worktree{}
}

func exists(p string) bool { _, err := os.Stat(p); return err == nil }

func TestPlanDeleteMain(t *testing.T) {
	repo := testrepo.New(t)
	r := Runner{Dir: repo.Dir}
	wts, _ := r.Worktrees(context.Background())
	p, err := r.PlanDelete(context.Background(), wts[0])
	if err != nil {
		t.Fatal(err)
	}
	if p.Blocked != "the main worktree cannot be deleted" {
		t.Errorf("Blocked = %q", p.Blocked)
	}
}

func TestPlanDeleteLocked(t *testing.T) {
	repo := testrepo.New(t)
	path := repo.AddWorktree(t, "feat")
	r := Runner{Dir: repo.Dir}
	repo.Git(t, repo.Dir, "worktree", "lock", "--reason", "in use", path)
	p, err := r.PlanDelete(context.Background(), find(t, r, "feat"))
	if err != nil {
		t.Fatal(err)
	}
	if p.Blocked != "worktree is locked: in use" {
		t.Errorf("Blocked = %q", p.Blocked)
	}
}

func TestPlanDeleteLockedNoReason(t *testing.T) {
	repo := testrepo.New(t)
	path := repo.AddWorktree(t, "feat")
	r := Runner{Dir: repo.Dir}
	repo.Git(t, repo.Dir, "worktree", "lock", path)
	p, err := r.PlanDelete(context.Background(), find(t, r, "feat"))
	if err != nil {
		t.Fatal(err)
	}
	if p.Blocked != "worktree is locked" {
		t.Errorf("Blocked = %q", p.Blocked)
	}
}

func TestPlanDeleteMissing(t *testing.T) {
	repo := testrepo.New(t)
	path := repo.AddWorktree(t, "feat")
	r := Runner{Dir: repo.Dir}
	if err := os.RemoveAll(path); err != nil {
		t.Fatal(err)
	}
	p, err := r.PlanDelete(context.Background(), find(t, r, "feat"))
	if err != nil {
		t.Fatal(err)
	}
	if p.Blocked != "folder is missing: use prune" || p.NeedsYes {
		t.Errorf("plan = %+v", p)
	}
}

func TestPlanDeleteClean(t *testing.T) {
	repo := testrepo.New(t)
	repo.AddWorktree(t, "feat")
	r := Runner{Dir: repo.Dir}
	p, err := r.PlanDelete(context.Background(), find(t, r, "feat"))
	if err != nil {
		t.Fatal(err)
	}
	if p.Blocked != "" || p.NeedsYes || p.Changed != 0 || p.Unreachable != 0 {
		t.Errorf("plan = %+v", p)
	}
}

func TestPlanDeleteDirty(t *testing.T) {
	repo := testrepo.New(t)
	path := repo.AddWorktree(t, "feat")
	if err := os.WriteFile(filepath.Join(path, "new.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	r := Runner{Dir: repo.Dir}
	p, err := r.PlanDelete(context.Background(), find(t, r, "feat"))
	if err != nil {
		t.Fatal(err)
	}
	if p.Blocked != "" || p.Changed != 1 || !p.NeedsYes {
		t.Errorf("plan = %+v", p)
	}
}

// An existing folder that git cannot read must give an error, not a clean plan.
func TestPlanDeleteUnreadable(t *testing.T) {
	repo := testrepo.New(t)
	repo.AddWorktree(t, "feat")
	r := Runner{Dir: repo.Dir}
	wt := find(t, r, "feat")
	wt.Path = t.TempDir() // exists, but is not a repository
	p, err := r.PlanDelete(context.Background(), wt)
	if err == nil {
		t.Fatalf("err = nil, plan = %+v; want an error", p)
	}
}

func TestPlanDeleteDetachedUnreachable(t *testing.T) {
	repo := testrepo.New(t)
	path := filepath.Join(filepath.Dir(repo.Dir), "det")
	repo.Git(t, repo.Dir, "worktree", "add", "--detach", path)
	r := Runner{Dir: repo.Dir}

	// Detached at a commit that master contains.
	p, err := r.PlanDelete(context.Background(), find(t, r, "det"))
	if err != nil {
		t.Fatal(err)
	}
	if !find(t, r, "det").Detached || p.Unreachable != 0 || p.NeedsYes {
		t.Errorf("contained: plan = %+v", p)
	}

	// Two commits that no branch, remote ref, or tag contains.
	repo.Commit(t, path, "a.txt")
	repo.Commit(t, path, "b.txt")
	p, err = r.PlanDelete(context.Background(), find(t, r, "det"))
	if err != nil {
		t.Fatal(err)
	}
	if p.Blocked != "" || p.Unreachable != 2 || p.Changed != 0 || !p.NeedsYes {
		t.Errorf("unreachable: plan = %+v", p)
	}
	n, err := r.UnreachableCommits(context.Background(), path)
	if err != nil || n != 2 {
		t.Errorf("UnreachableCommits = %d, %v; want 2, nil", n, err)
	}

	// A tag makes the commits reachable.
	repo.Git(t, path, "tag", "keep")
	if n, err = r.UnreachableCommits(context.Background(), path); err != nil || n != 0 {
		t.Errorf("after tag: UnreachableCommits = %d, %v; want 0, nil", n, err)
	}
}

func TestRemove(t *testing.T) {
	repo := testrepo.New(t)
	ctx := context.Background()
	r := Runner{Dir: repo.Dir}

	clean := repo.AddWorktree(t, "clean")
	if err := r.Remove(ctx, clean, false); err != nil {
		t.Fatal(err)
	}
	if exists(clean) {
		t.Error("clean folder still exists")
	}
	wts, _ := r.Worktrees(ctx)
	if len(wts) != 1 {
		t.Errorf("worktrees = %d, want 1", len(wts))
	}
	if _, err := r.Run(ctx, "rev-parse", "--verify", "--quiet", "refs/heads/clean"); err != nil {
		t.Error("branch clean is gone")
	}

	dirty := repo.AddWorktree(t, "dirty")
	if err := os.WriteFile(filepath.Join(dirty, "new.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := r.Remove(ctx, dirty, false); err == nil {
		t.Fatal("Remove(dirty, false): err = nil")
	}
	if !exists(dirty) {
		t.Fatal("dirty folder was removed without force")
	}
	if err := r.Remove(ctx, dirty, true); err != nil {
		t.Fatal(err)
	}
	if exists(dirty) {
		t.Error("dirty folder still exists after force")
	}
}

func TestDeleteBranch(t *testing.T) {
	repo := testrepo.New(t)
	ctx := context.Background()
	r := Runner{Dir: repo.Dir}

	repo.Git(t, repo.Dir, "branch", "merged")
	if err := r.DeleteBranch(ctx, "merged"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Run(ctx, "rev-parse", "--verify", "--quiet", "refs/heads/merged"); err == nil {
		t.Error("branch merged still exists")
	}

	path := repo.AddWorktree(t, "unmerged")
	repo.Commit(t, path, "u.txt")
	repo.Git(t, repo.Dir, "worktree", "remove", path)
	err := r.DeleteBranch(ctx, "unmerged")
	if err == nil || !strings.Contains(err.Error(), "not fully merged") {
		t.Errorf("err = %v, want text with %q", err, "not fully merged")
	}
}

func TestPrune(t *testing.T) {
	repo := testrepo.New(t)
	ctx := context.Background()
	r := Runner{Dir: repo.Dir}

	lines, err := r.PruneDryRun(ctx)
	if err != nil || lines == nil || len(lines) != 0 {
		t.Fatalf("no stale: lines = %#v, err = %v; want empty slice, nil", lines, err)
	}

	path := repo.AddWorktree(t, "gone")
	if err := os.RemoveAll(path); err != nil {
		t.Fatal(err)
	}
	if lines, err = r.PruneDryRun(ctx); err != nil || len(lines) != 1 {
		t.Fatalf("stale: lines = %#v, err = %v; want 1 line", lines, err)
	}
	if err := r.Prune(ctx); err != nil {
		t.Fatal(err)
	}
	if lines, err = r.PruneDryRun(ctx); err != nil || len(lines) != 0 {
		t.Errorf("after prune: lines = %#v, err = %v; want 0 lines", lines, err)
	}
}

// "status.showUntrackedFiles=no" must not hide untracked files from the plan.
func TestPlanDeleteUntrackedIgnoresConfig(t *testing.T) {
	repo := testrepo.New(t)
	path := repo.AddWorktree(t, "feat")
	repo.Git(t, repo.Dir, "config", "status.showUntrackedFiles", "no")
	if err := os.WriteFile(filepath.Join(path, "new.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	r := Runner{Dir: repo.Dir}
	p, err := r.PlanDelete(context.Background(), find(t, r, "feat"))
	if err != nil {
		t.Fatal(err)
	}
	if p.Changed != 1 || !p.NeedsYes {
		t.Errorf("plan = %+v, want Changed 1 and NeedsYes", p)
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

func TestPlanDeleteNestedWorktree(t *testing.T) {
	repo, _, nested := nestedRepo(t)
	r := Runner{Dir: repo.Dir}
	p, err := r.PlanDelete(context.Background(), find(t, r, "outer"))
	if err != nil {
		t.Fatal(err)
	}
	if p.Blocked != "contains worktree nested: delete it first" {
		t.Errorf("plan = %+v, want blocked by the nested worktree", p)
	}

	// A detached nested worktree has the short head as its label.
	repo.Git(t, nested, "switch", "--detach")
	head := repo.Git(t, nested, "rev-parse", "HEAD")
	if p, err = r.PlanDelete(context.Background(), find(t, r, "outer")); err != nil {
		t.Fatal(err)
	}
	if want := "contains worktree " + head[:7] + ": delete it first"; p.Blocked != want {
		t.Errorf("Blocked = %q, want %q", p.Blocked, want)
	}

	// The nested worktree itself has nothing below it.
	if p, err = r.PlanDelete(context.Background(), find(t, r, "nested")); err != nil || p.Blocked != "" {
		t.Errorf("nested: plan = %+v, err = %v", p, err)
	}
}

// A sibling whose name starts with the name of the target is not inside it.
func TestPlanDeleteSiblingPrefix(t *testing.T) {
	repo := testrepo.New(t)
	repo.AddWorktree(t, "feat")
	repo.AddWorktree(t, "feat-2")
	r := Runner{Dir: repo.Dir}
	p, err := r.PlanDelete(context.Background(), find(t, r, "feat"))
	if err != nil || p.Blocked != "" {
		t.Errorf("plan = %+v, err = %v", p, err)
	}
}

// Git refuses to remove a worktree with an initialized submodule without
// --force, also if the worktree is clean. The plan asks for the typed yes.
func TestPlanDeleteSubmodule(t *testing.T) {
	ctx := context.Background()
	repo := testrepo.New(t)
	repo.AddSubmodule(t)
	path := repo.AddWorktree(t, "feat-sub")
	repo.InitSubmodules(t, path)
	r := open(t, repo.Dir)
	p, err := r.PlanDelete(ctx, find(t, r, "feat-sub"))
	if err != nil {
		t.Fatal(err)
	}
	if p.Blocked != "" || !p.Submodule || !p.NeedsYes || p.Changed != 0 {
		t.Errorf("plan = %+v, want Submodule and NeedsYes", p)
	}
	if err := r.Remove(ctx, path, false); err == nil {
		t.Fatal("git removed a worktree with a submodule without --force")
	}
	if err := r.Remove(ctx, path, true); err != nil || exists(path) {
		t.Errorf("forced remove: err %v, exists %v", err, exists(path))
	}
}

// A submodule that is not initialized does not stop git, so the plan is
// clean and the delete needs no --force.
func TestPlanDeleteSubmoduleNotInitialized(t *testing.T) {
	ctx := context.Background()
	repo := testrepo.New(t)
	repo.AddSubmodule(t)
	path := repo.AddWorktree(t, "feat-sub")
	r := open(t, repo.Dir)
	p, err := r.PlanDelete(ctx, find(t, r, "feat-sub"))
	if err != nil {
		t.Fatal(err)
	}
	if p.Submodule || p.NeedsYes {
		t.Errorf("plan = %+v, want a clean plan", p)
	}
	if err := r.Remove(ctx, path, false); err != nil || exists(path) {
		t.Errorf("remove: err %v, exists %v", err, exists(path))
	}
}

func TestPlanDeleteNoSubmodule(t *testing.T) {
	repo := testrepo.New(t)
	repo.AddWorktree(t, "feat-a")
	r := open(t, repo.Dir)
	p, err := r.PlanDelete(context.Background(), find(t, r, "feat-a"))
	if err != nil || p.Submodule || p.NeedsYes {
		t.Errorf("plan = %+v, err %v", p, err)
	}
}

func TestDeletePromptSubmodule(t *testing.T) {
	wt := Worktree{Path: "/r/feat", Branch: "feat"}
	for _, tc := range []struct {
		plan DeletePlan
		want string
	}{
		{DeletePlan{Submodule: true, NeedsYes: true},
			"feat has a submodule. Commits in the submodule that are not pushed will be lost. Ignored files are deleted too. "},
		{DeletePlan{Changed: 2, Submodule: true, NeedsYes: true},
			"feat has 2 changed files. They will be lost. feat has a submodule. Commits in the submodule that are not pushed will be lost. Ignored files are deleted too. "},
		{DeletePlan{Changed: 1, NeedsYes: true},
			"feat has 1 changed file. They will be lost. Ignored files are deleted too. "},
	} {
		if got := DeletePrompt(wt, tc.plan); got != tc.want {
			t.Errorf("DeletePrompt(%+v) =\n%q, want\n%q", tc.plan, got, tc.want)
		}
	}
}
