package git

import (
	"context"
	"errors"
	"fmt"
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

// resolve returns the real path of p, or the cleaned path if p does not exist.
func resolve(p string) string {
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r
	}
	return filepath.Clean(p)
}

// Current returns the index of the worktree that contains cwd, or -1.
// If worktrees are nested, the deepest one wins.
func Current(wts []Worktree, cwd string) int {
	cwd = resolve(cwd)
	best, bestLen := -1, -1
	for i, w := range wts {
		p := resolve(w.Path)
		if (cwd == p || strings.HasPrefix(cwd, p+string(filepath.Separator))) && len(p) > bestLen {
			best, bestLen = i, len(p)
		}
	}
	return best
}

// ErrNoMatch means that no worktree matches a name.
var ErrNoMatch = errors.New("no worktree matches")

// AmbiguousError means that more than one worktree matches a name.
type AmbiguousError struct {
	Name    string
	Matches []Worktree
}

func (e *AmbiguousError) Error() string {
	return fmt.Sprintf("more than one worktree matches %q", e.Name)
}

// Match finds one worktree by name. An exact match of the branch or the folder
// name wins. Otherwise a case-insensitive substring of either must match once.
func Match(wts []Worktree, name string) (Worktree, error) {
	n := strings.ToLower(strings.TrimSpace(name))
	if n == "" {
		return Worktree{}, fmt.Errorf("%w %q", ErrNoMatch, name)
	}
	var exact, partial []Worktree
	for _, w := range wts {
		b, f := strings.ToLower(w.Branch), strings.ToLower(filepath.Base(w.Path))
		if b == n || f == n {
			exact = append(exact, w)
		}
		if strings.Contains(b, n) || strings.Contains(f, n) {
			partial = append(partial, w)
		}
	}
	for _, set := range [][]Worktree{exact, partial} {
		switch len(set) {
		case 0:
			continue
		case 1:
			return set[0], nil
		default:
			return Worktree{}, &AmbiguousError{Name: name, Matches: set}
		}
	}
	return Worktree{}, fmt.Errorf("%w %q", ErrNoMatch, name)
}
