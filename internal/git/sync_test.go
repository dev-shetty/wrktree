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

// wantRefused fails the test unless err is a refusal.
func wantRefused(t *testing.T, err error) {
	t.Helper()
	if err == nil || !errors.Is(err, ErrSyncRefused) {
		t.Errorf("err = %v, want a refusal", err)
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
		name, file, text string
	}{
		{"untracked", "new.txt", "new\n"},
		{"tracked", "README.md", "changed\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo, r, wt := syncFeat(t)
			advanceMaster(t, repo, "m1.txt", "master\n")
			before := head(t, repo, wt.Path)
			path := filepath.Join(wt.Path, tc.file)
			writeFile(t, path, tc.text)
			_, err := r.Sync(context.Background(), wt, syncBase)
			wantRefused(t, err)
			if h := head(t, repo, wt.Path); h != before {
				t.Errorf("HEAD moved: %s -> %s", before, h)
			}
			if got := readFile(t, path); got != tc.text {
				t.Errorf("%s = %q, want %q", tc.file, got, tc.text)
			}
		})
	}
}
