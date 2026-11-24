package git

import (
	"context"
	"path/filepath"
	"strings"
)

// Worktree is one entry of "git worktree list --porcelain".
type Worktree struct {
	Path, Branch, Head                       string // Branch is the short name, "" if detached
	Bare, Detached, Locked, Prunable, IsMain bool
	LockReason                               string
}

// ParseWorktrees parses porcelain output. The first entry is main unless it is bare.
func ParseWorktrees(porcelain string) []Worktree {
	var out []Worktree
	var cur *Worktree
	for _, line := range strings.Split(porcelain, "\n") {
		if line == "" {
			cur = nil
			continue
		}
		key, val, _ := strings.Cut(line, " ")
		if key == "worktree" {
			out = append(out, Worktree{Path: val})
			cur = &out[len(out)-1]
			continue
		}
		if cur == nil {
			continue
		}
		switch key {
		case "HEAD":
			cur.Head = val
		case "branch":
			cur.Branch = strings.TrimPrefix(val, "refs/heads/")
		case "bare":
			cur.Bare = true
		case "detached":
			cur.Detached = true
		case "locked":
			cur.Locked = true
			cur.LockReason = val
		case "prunable":
			cur.Prunable = true
		}
	}
	if len(out) > 0 && !out[0].Bare {
		out[0].IsMain = true
	}
	return out
}

func (r Runner) listAll(ctx context.Context) ([]Worktree, error) {
	out, err := r.Run(ctx, "worktree", "list", "--porcelain")
	if err != nil {
		return nil, err
	}
	return ParseWorktrees(out), nil
}

// Worktrees lists the worktrees of the repository. Bare entries are removed.
func (r Runner) Worktrees(ctx context.Context) ([]Worktree, error) {
	all, err := r.listAll(ctx)
	if err != nil {
		return nil, err
	}
	var out []Worktree
	for _, w := range all {
		if !w.Bare {
			out = append(out, w)
		}
	}
	return out, nil
}

// RepoName returns the base name of the main worktree, or of the bare
// directory without ".git". It returns "" if the lookup fails.
func (r Runner) RepoName(ctx context.Context) string {
	all, err := r.listAll(ctx)
	if err != nil || len(all) == 0 {
		return ""
	}
	name := filepath.Base(all[0].Path)
	if all[0].Bare {
		name = strings.TrimSuffix(name, ".git")
	}
	return name
}
