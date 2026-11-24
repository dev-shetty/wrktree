package git

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// SyncResult tells what a sync did.
type SyncResult struct {
	UpToDate    bool     // the branch already contains the base
	FetchFailed bool     // the fetch failed; the merge used the local copy of the base
	Conflicts   []string // files with conflicts; the merge was cancelled
}

// ErrSyncRefused wraps every refusal that changed nothing.
var ErrSyncRefused = errors.New("sync refused")

// syncRefusal is a refusal of Sync. Its text is the reason only.
type syncRefusal string

func (e syncRefusal) Error() string { return string(e) }
func (e syncRefusal) Unwrap() error { return ErrSyncRefused }

// syncFetchTimeout stops a fetch of Sync that takes too long.
const syncFetchTimeout = 15 * time.Second

// opStates are the files and folders in the git directory of a worktree
// that show a merge, cherry-pick, revert, or rebase in progress.
var opStates = []string{"MERGE_HEAD", "CHERRY_PICK_HEAD", "REVERT_HEAD", "rebase-merge", "rebase-apply"}

// Sync merges base into the branch of wt with "git merge --no-edit". It
// refuses, and changes nothing, if the worktree is not clean or an
// operation is in progress. A merge with conflicts is cancelled with
// "git merge --abort", so the worktree stays as it was. Sync never pushes,
// rebases, resets, or stashes.
func (r Runner) Sync(ctx context.Context, wt Worktree, base string) (SyncResult, error) {
	switch {
	case base == "":
		return SyncResult{}, syncRefusal("no base branch")
	case wt.Prunable:
		return SyncResult{}, syncRefusal("folder is missing: use prune")
	case wt.Bare:
		return SyncResult{}, syncRefusal("bare repository")
	case wt.Detached || wt.Branch == "":
		return SyncResult{}, syncRefusal("detached worktree: no branch to update")
	}
	g := r.In(wt.Path)
	busy, err := g.opInProgress(ctx, wt.Path, opStates...)
	if err != nil {
		return SyncResult{}, err
	}
	if busy {
		return SyncResult{}, syncRefusal("merge or rebase in progress: finish it first")
	}
	n, err := r.ChangedFiles(ctx, wt.Path)
	if err != nil {
		return SyncResult{}, err
	}
	if n > 0 {
		return SyncResult{}, syncRefusal("has " + plural(n, "changed file", "changed files") + ": commit or stash first")
	}

	var res SyncResult
	fctx, cancel := context.WithTimeout(ctx, syncFetchTimeout)
	res.FetchFailed = r.FetchBase(fctx, base) != nil
	cancel()

	_, behind, err := r.Distance(ctx, wt.Path, base)
	if err != nil {
		return res, err
	}
	if behind == 0 {
		res.UpToDate = true
		return res, nil
	}

	_, mergeErr := g.Run(ctx, "merge", "--no-edit", base)
	if mergeErr == nil {
		return res, nil
	}
	out, err := g.Run(ctx, "diff", "--name-only", "--diff-filter=U")
	if err != nil {
		return res, fmt.Errorf("%w (cannot read the conflicts: %v)", mergeErr, err)
	}
	var files []string
	for _, l := range strings.Split(out, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			files = append(files, l)
		}
	}
	// The check above found no merge in progress, so a MERGE_HEAD now is
	// the merge of this call.
	started, err := g.opInProgress(ctx, wt.Path, "MERGE_HEAD")
	if err != nil {
		return res, fmt.Errorf("%w (cannot read the merge state: %v)", mergeErr, err)
	}
	if started {
		if _, err := g.Run(ctx, "merge", "--abort"); err != nil {
			return res, fmt.Errorf("merge --abort failed in %s: %w", wt.Path, err)
		}
	}
	if len(files) > 0 {
		res.Conflicts = files
		return res, nil
	}
	return res, mergeErr
}

// opInProgress reports whether one of names exists in the git directory of
// the worktree at path.
func (r Runner) opInProgress(ctx context.Context, path string, names ...string) (bool, error) {
	for _, name := range names {
		p, err := r.Run(ctx, "rev-parse", "--git-path", name)
		if err != nil {
			return false, err
		}
		if !filepath.IsAbs(p) {
			p = filepath.Join(path, p)
		}
		if _, err := os.Stat(p); err == nil {
			return true, nil
		} else if !errors.Is(err, os.ErrNotExist) {
			return false, err
		}
	}
	return false, nil
}

// ConflictText joins the conflict files with ", ". After 3 files, it adds
// " and N more".
func ConflictText(files []string) string {
	if len(files) <= 3 {
		return strings.Join(files, ", ")
	}
	return fmt.Sprintf("%s and %d more", strings.Join(files[:3], ", "), len(files)-3)
}
