package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dev-shetty/wrktree/internal/config"
	"github.com/dev-shetty/wrktree/internal/testrepo"
)

type opened struct{ template, path string }

// runOpts holds the fakes for one run.
type runOpts struct {
	openErr error
	opened  *opened
}

func runWith(t *testing.T, dir string, o runOpts, args ...string) (string, string, error) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	c := New(Env{
		Dir:        dir,
		Stdin:      strings.NewReader(""),
		Stdout:     &stdout,
		Stderr:     &stderr,
		IsTerminal: func() bool { return false },
		RunOpen: func(template, path string) error {
			if o.opened != nil {
				*o.opened = opened{template, path}
			}
			return o.openErr
		},
	})
	c.SetArgs(args)
	err := c.Execute()
	return stdout.String(), stderr.String(), err
}

func run(t *testing.T, dir string, args ...string) (string, string, error) {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(t.TempDir(), "config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(t.TempDir(), "cache"))
	return runWith(t, dir, runOpts{}, args...)
}

func twoWorktrees(t *testing.T) (*testrepo.Repo, string) {
	t.Helper()
	repo := testrepo.New(t)
	feat := repo.AddWorktree(t, "feat-a")
	// git prints real paths. On macOS the temporary directory is a symlink.
	if real, err := filepath.EvalSymlinks(feat); err == nil {
		feat = real
	}
	return repo, feat
}

func TestNotRepo(t *testing.T) {
	_, _, err := run(t, t.TempDir(), "ls")
	if err == nil || err.Error() != "not a git repository" {
		t.Errorf("err = %v", err)
	}
}

func TestLs(t *testing.T) {
	repo, _ := twoWorktrees(t)
	out, _, err := run(t, repo.Dir, "ls")
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{"master", "feat-a", "origin/master"} {
		if !strings.Contains(out, s) {
			t.Errorf("output lacks %q:\n%s", s, out)
		}
	}
}

func TestLsMarksCurrent(t *testing.T) {
	repo, feat := twoWorktrees(t)
	out, _, err := run(t, feat, "ls")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "● feat-a") || strings.Contains(out, "● master") {
		t.Errorf("current mark:\n%s", out)
	}
	_ = repo
}

func TestRootRunsLs(t *testing.T) {
	repo, _ := twoWorktrees(t)
	out, _, err := run(t, repo.Dir)
	if err != nil || !strings.Contains(out, "feat-a") {
		t.Errorf("err %v, out:\n%s", err, out)
	}
}

func TestLsJSON(t *testing.T) {
	repo, _ := twoWorktrees(t)
	out, _, err := run(t, repo.Dir, "ls", "--json")
	if err != nil {
		t.Fatal(err)
	}
	var rows []map[string]any
	if err := json.Unmarshal([]byte(out), &rows); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if len(rows) != 2 || rows[0]["main"] != true || rows[1]["branch"] != "feat-a" || rows[1]["changed"] != float64(0) {
		t.Errorf("rows: %v", rows)
	}
}

// ls shows the merged mark of a squash-merged branch, in text and in JSON.
func TestLsMerged(t *testing.T) {
	repo, feat := twoWorktrees(t)
	repo.Commit(t, feat, "f1.txt")
	repo.Git(t, repo.Dir, "merge", "--squash", "feat-a")
	repo.Git(t, repo.Dir, "commit", "-m", "squash feat-a")
	repo.Git(t, repo.Dir, "push", "origin", "master")
	out, _, err := run(t, repo.Dir, "ls")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "feat-a ✓ merged") || strings.Contains(out, "master ✓") {
		t.Errorf("merged mark:\n%s", out)
	}
	out, _, err = run(t, repo.Dir, "ls", "--json")
	if err != nil {
		t.Fatal(err)
	}
	var rows []map[string]any
	if err := json.Unmarshal([]byte(out), &rows); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if rows[1]["merged"] != true || rows[0]["merged"] != nil {
		t.Errorf("rows: %v", rows)
	}
}

func TestLsFetch(t *testing.T) {
	repo, _ := twoWorktrees(t)
	_, stderr, err := run(t, repo.Dir, "ls", "--fetch", "--fresh")
	if err != nil || stderr != "" {
		t.Errorf("err %v, stderr %q", err, stderr)
	}
}

func TestLsFetchFailure(t *testing.T) {
	repo, _ := twoWorktrees(t)
	repo.Git(t, repo.Dir, "remote", "set-url", "origin", filepath.Join(t.TempDir(), "missing.git"))
	out, stderr, err := run(t, repo.Dir, "ls", "--fetch")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stderr, "wrktree: fetch failed: distances can be stale") || !strings.Contains(out, "feat-a") {
		t.Errorf("stderr %q, out %q", stderr, out)
	}
}

func TestLsNoOrigin(t *testing.T) {
	repo := testrepo.NewNoOrigin(t)
	out, _, err := run(t, repo.Dir, "ls")
	if err != nil || !strings.Contains(out, "—") {
		t.Errorf("err %v, out:\n%s", err, out)
	}
	if _, _, err := run(t, repo.Dir, "ls", "--fetch"); err != nil {
		t.Errorf("--fetch: %v", err)
	}
}

func TestLsBadConfig(t *testing.T) {
	repo, _ := twoWorktrees(t)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	cfg := filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "wrktree", "config.toml")
	os.MkdirAll(filepath.Dir(cfg), 0o755)
	os.WriteFile(cfg, []byte("bogus = 1\n"), 0o644)
	_, _, err := runWith(t, repo.Dir, runOpts{}, "ls")
	if err == nil || !strings.Contains(err.Error(), "unknown key") {
		t.Errorf("err = %v", err)
	}
}

func TestSwitch(t *testing.T) {
	repo, feat := twoWorktrees(t)

	cdFile := filepath.Join(t.TempDir(), "cd")
	t.Setenv("WRKTREE_CD_FILE", cdFile)
	out, _, err := run(t, repo.Dir, "switch", "feat-a")
	if err != nil || out != "" {
		t.Fatalf("err %v, stdout %q", err, out)
	}
	if b, _ := os.ReadFile(cdFile); string(b) != feat {
		t.Errorf("cd file = %q, want %q", b, feat)
	}

	t.Setenv("WRKTREE_CD_FILE", "")
	out, _, err = run(t, repo.Dir, "switch", "feat-a")
	if err != nil || out != feat+"\n" {
		t.Errorf("err %v, stdout %q", err, out)
	}

	repo.AddWorktree(t, "feat-b")
	_, stderr, err := run(t, repo.Dir, "switch", "feat")
	if err == nil {
		t.Fatal("want error for ambiguous name")
	}
	for _, s := range []string{`more than one worktree matches "feat":`, "feat-a", "feat-b"} {
		if !strings.Contains(stderr, s) {
			t.Errorf("stderr lacks %q:\n%s", s, stderr)
		}
	}

	_, _, err = run(t, repo.Dir, "switch", "zzz")
	if err == nil || !strings.Contains(err.Error(), `no worktree matches "zzz"`) {
		t.Errorf("err = %v", err)
	}
}

func TestOpen(t *testing.T) {
	repo, feat := twoWorktrees(t)
	var got opened
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	if _, _, err := runWith(t, repo.Dir, runOpts{opened: &got}, "open", "feat-a"); err != nil {
		t.Fatal(err)
	}
	if got.template != "code -n {path}" || got.path != feat {
		t.Errorf("got %+v", got)
	}

	cfg := filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "wrktree", "config.toml")
	os.MkdirAll(filepath.Dir(cfg), 0o755)
	os.WriteFile(cfg, []byte("open = \"cursor {path}\"\n"), 0o644)
	if _, _, err := runWith(t, repo.Dir, runOpts{opened: &got}, "open", "feat-a"); err != nil {
		t.Fatal(err)
	}
	if got.template != "cursor {path}" {
		t.Errorf("got %+v", got)
	}
}

func TestOpenNotFound(t *testing.T) {
	repo, _ := twoWorktrees(t)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	// A real exit code 127 from sh.
	err127 := exec127(t)
	_, _, err := runWith(t, repo.Dir, runOpts{openErr: err127}, "open", "feat-a")
	want := `open command not found: code -n {path} (set "open" in ` + config.Path() + ")"
	if err == nil || err.Error() != want {
		t.Errorf("err = %v, want %s", err, want)
	}

	// The error names the full template, not the first word.
	cfg := config.Path()
	os.MkdirAll(filepath.Dir(cfg), 0o755)
	os.WriteFile(cfg, []byte("open = \"cd {path} && claude\"\n"), 0o644)
	_, _, err = runWith(t, repo.Dir, runOpts{openErr: err127}, "open", "feat-a")
	want = `open command not found: cd {path} && claude (set "open" in ` + cfg + ")"
	if err == nil || err.Error() != want {
		t.Errorf("err = %v, want %s", err, want)
	}
}

func TestInit(t *testing.T) {
	out, _, err := run(t, t.TempDir(), "init", "zsh")
	if err != nil || !strings.Contains(out, "wt() {") {
		t.Errorf("err %v, out %q", err, out)
	}
	out, _, err = run(t, t.TempDir(), "init", "zsh", "--name", "w")
	if err != nil || !strings.Contains(out, "w() {") {
		t.Errorf("err %v, out %q", err, out)
	}
	if _, _, err = run(t, t.TempDir(), "init", "fish"); err == nil {
		t.Error("fish: want error")
	}
	if _, _, err = run(t, t.TempDir(), "init", "zsh", "--name", "a b"); err == nil {
		t.Error("bad name: want error")
	}
}

// A base with no remote part is not fetched, and ls shows no failure.
func TestLsFetchLocalBase(t *testing.T) {
	repo, _ := twoWorktrees(t)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	cfg := filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "wrktree", "config.toml")
	os.MkdirAll(filepath.Dir(cfg), 0o755)
	os.WriteFile(cfg, []byte("base = \"master\"\n"), 0o644)
	out, stderr, err := runWith(t, repo.Dir, runOpts{}, "ls", "--fetch")
	if err != nil || stderr != "" {
		t.Errorf("err %v, stderr %q", err, stderr)
	}
	if !strings.Contains(out, "feat-a") {
		t.Errorf("out:\n%s", out)
	}
}

func TestSwitchOpenMissing(t *testing.T) {
	repo, feat := twoWorktrees(t)
	if err := os.RemoveAll(feat); err != nil {
		t.Fatal(err)
	}
	cdFile := filepath.Join(t.TempDir(), "cd")
	t.Setenv("WRKTREE_CD_FILE", cdFile)
	out, _, err := run(t, repo.Dir, "switch", "feat-a")
	if err == nil || err.Error() != "folder is missing: use prune" || out != "" {
		t.Errorf("switch: err %v, stdout %q", err, out)
	}
	if exists(cdFile) {
		t.Error("switch wrote the cd file")
	}

	var got opened
	_, _, err = runWith(t, repo.Dir, runOpts{opened: &got}, "open", "feat-a")
	if err == nil || err.Error() != "folder is missing: use prune" || got != (opened{}) {
		t.Errorf("open: err %v, opened %+v", err, got)
	}
}
