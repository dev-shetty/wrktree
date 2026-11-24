// Package testrepo builds throwaway git repositories for tests.
package testrepo

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Repo is a test repository. Dir is the main worktree. Origin is the bare remote.
type Repo struct{ Dir, Origin string }

// New creates a repository with branch "master", one commit, pushed to a bare Origin.
func New(t *testing.T) *Repo {
	t.Helper()
	root := t.TempDir()
	r := &Repo{Dir: filepath.Join(root, "main"), Origin: filepath.Join(root, "origin.git")}
	for _, d := range []string{r.Dir, r.Origin} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	r.Git(t, r.Origin, "init", "--bare", "-b", "master")
	r.Git(t, r.Dir, "init", "-b", "master")
	r.Commit(t, r.Dir, "README.md")
	r.Git(t, r.Dir, "remote", "add", "origin", r.Origin)
	r.Git(t, r.Dir, "push", "-u", "origin", "master")
	r.Git(t, r.Dir, "remote", "set-head", "origin", "master")
	return r
}

// NewNoOrigin creates a repository with branch "master", one commit, and no remote.
func NewNoOrigin(t *testing.T) *Repo {
	t.Helper()
	r := &Repo{Dir: filepath.Join(t.TempDir(), "main")}
	if err := os.MkdirAll(r.Dir, 0o755); err != nil {
		t.Fatal(err)
	}
	r.Git(t, r.Dir, "init", "-b", "master")
	r.Commit(t, r.Dir, "README.md")
	return r
}

// Git runs git in dir with an isolated environment. It fails the test on error.
func (r *Repo) Git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	var env []string
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, "GIT_") {
			env = append(env, kv)
		}
	}
	cmd.Env = append(env,
		"GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_CONFIG_SYSTEM=/dev/null",
		"GIT_AUTHOR_NAME=Test",
		"GIT_AUTHOR_EMAIL=test@example.com",
		"GIT_COMMITTER_NAME=Test",
		"GIT_COMMITTER_EMAIL=test@example.com",
	)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("git %s (in %s): %v\n%s%s", strings.Join(args, " "), dir, err, stdout.String(), stderr.String())
	}
	out := stdout.Bytes()
	return strings.TrimRight(string(out), "\n")
}

// AddWorktree adds a worktree on a new branch. The path is a sibling of Dir.
func (r *Repo) AddWorktree(t *testing.T, branch string) string {
	t.Helper()
	path := filepath.Join(filepath.Dir(r.Dir), branch)
	r.Git(t, r.Dir, "worktree", "add", "-b", branch, path)
	return path
}

// Commit writes file in dir, then stages and commits it.
func (r *Repo) Commit(t *testing.T, dir, file string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, file), []byte(file+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	r.Git(t, dir, "add", file)
	r.Git(t, dir, "commit", "-m", "add "+file)
}
