// Package cmd holds the command line of wrktree.
package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/dev-shetty/wrktree/internal/config"
	"github.com/dev-shetty/wrktree/internal/disk"
	"github.com/dev-shetty/wrktree/internal/git"
	"github.com/dev-shetty/wrktree/internal/shell"
	"github.com/dev-shetty/wrktree/internal/status"
	"github.com/dev-shetty/wrktree/internal/ui"
)

// Env holds everything that the commands take from the outside.
type Env struct {
	Dir            string
	Stdin          io.Reader
	Stdout, Stderr io.Writer
	IsTerminal     func() bool // stdin and stderr are terminals
	RunOpen        func(template, path string) error
	Now            func() time.Time
	RunScreen      func(ui.Deps) (ui.Result, error) // runs the interactive screen
}

// errReported means that the command already printed the error text.
var errReported = errors.New("error reported")

func (e Env) withDefaults() Env {
	if e.Dir == "" {
		e.Dir = "."
	}
	if abs, err := filepath.Abs(e.Dir); err == nil {
		e.Dir = abs
	}
	if e.Stdin == nil {
		e.Stdin = os.Stdin
	}
	if e.Stdout == nil {
		e.Stdout = os.Stdout
	}
	if e.Stderr == nil {
		e.Stderr = os.Stderr
	}
	if e.IsTerminal == nil {
		e.IsTerminal = func() bool { return false }
	}
	if e.RunOpen == nil {
		e.RunOpen = shell.RunOpen
	}
	if e.Now == nil {
		e.Now = time.Now
	}
	if e.RunScreen == nil {
		e.RunScreen = runProgram
	}
	return e
}

// runProgram runs the interactive screen. It draws on stderr.
func runProgram(d ui.Deps) (ui.Result, error) {
	p := tea.NewProgram(ui.New(d), tea.WithOutput(os.Stderr), tea.WithAltScreen())
	final, err := p.Run()
	if err != nil {
		return ui.Result{}, err
	}
	m, ok := final.(ui.Model)
	if !ok {
		return ui.Result{}, fmt.Errorf("unexpected screen model %T", final)
	}
	return m.Result(), nil
}

// New builds the root command. With no subcommand, it runs the screen, or
// ls if stdin or stderr is not a terminal.
func New(env Env) *cobra.Command {
	env = env.withDefaults()
	root := &cobra.Command{
		Use:           "wrktree",
		Short:         "Manage git worktrees",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.NoArgs,
		RunE: func(c *cobra.Command, args []string) error {
			if !env.IsTerminal() {
				return runLs(c.Context(), env, lsFlags{})
			}
			return runScreen(c.Context(), env)
		},
	}
	root.SetIn(env.Stdin)
	root.SetOut(env.Stdout)
	root.SetErr(env.Stderr)
	root.AddCommand(newLs(env), newSwitch(env), newOpen(env), newInit(env), newRm(env), newPrune(env), newSync(env))
	return root
}

// Execute runs the command line with the real environment. It returns the exit code.
func Execute() int {
	env := Env{
		IsTerminal: func() bool {
			return term.IsTerminal(int(os.Stdin.Fd())) && term.IsTerminal(int(os.Stderr.Fd()))
		},
	}.withDefaults()
	if err := New(env).Execute(); err != nil {
		if !errors.Is(err, errReported) {
			fmt.Fprintf(env.Stderr, "wrktree: %v\n", err)
		}
		return 1
	}
	return 0
}

// session holds what a command that works on a repository needs.
type session struct {
	ctx context.Context
	env Env
	r   git.Runner
	cfg config.Config
}

func open(ctx context.Context, env Env) (*session, error) {
	r, err := git.Open(env.Dir)
	if err != nil {
		return nil, err
	}
	cfg, err := config.Load(config.Path())
	if err != nil {
		return nil, err
	}
	if ctx == nil {
		ctx = context.Background()
	}
	return &session{ctx: ctx, env: env, r: r, cfg: cfg}, nil
}

// runScreen runs the interactive screen, then does the action that the
// user selected.
func runScreen(ctx context.Context, env Env) error {
	s, err := open(ctx, env)
	if err != nil {
		return err
	}
	wts, err := s.r.Worktrees(s.ctx)
	if err != nil {
		return err
	}
	res, err := env.RunScreen(ui.Deps{
		Git:   s.r,
		Cfg:   s.cfg,
		Cache: disk.LoadCache(disk.DefaultCachePath()),
		Repo:  s.repoKey(wts),
		Name:  s.r.RepoName(s.ctx),
		Cwd:   env.Dir,
		CanCD: os.Getenv(shell.CDFileEnv) != "",
		Now:   env.Now,
		Copy:  shell.Copy,
	})
	if err != nil {
		return err
	}
	if res.Message != "" {
		fmt.Fprintf(env.Stderr, "wrktree: %s\n", res.Message)
	}
	switch {
	case res.SwitchPath != "":
		return shell.WriteCD(res.SwitchPath, env.Stdout, env.Stderr)
	case res.OpenPath != "":
		return s.openPath(res.OpenPath)
	}
	return nil
}

// openPath runs the open command on path. A command that is not found gives
// an error that names the configuration file.
func (s *session) openPath(path string) error {
	err := s.env.RunOpen(s.cfg.Open, path)
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitCode() == 127 {
		return fmt.Errorf("open command not found: %s (set \"open\" in %s)", s.cfg.Open, config.Path())
	}
	return err
}

// base returns the base branch, or "" if the repository has none.
func (s *session) base() (string, error) {
	b, err := s.r.BaseBranch(s.ctx, s.cfg.Base)
	if errors.Is(err, git.ErrNoBase) {
		return "", nil
	}
	return b, err
}

// repoKey returns the cache key of the repository: the path of the main
// worktree, or the git common directory for a bare repository.
func (s *session) repoKey(wts []git.Worktree) string {
	if len(wts) > 0 && wts[0].IsMain {
		return wts[0].Path
	}
	if dir, err := s.r.Run(s.ctx, "rev-parse", "--path-format=absolute", "--git-common-dir"); err == nil {
		return dir
	}
	return s.env.Dir
}

// find resolves name to one worktree. For an ambiguous name, it prints the
// matches and returns errReported.
func (s *session) find(name string) (git.Worktree, error) {
	wts, err := s.r.Worktrees(s.ctx)
	if err != nil {
		return git.Worktree{}, err
	}
	w, err := git.Match(wts, name)
	var amb *git.AmbiguousError
	if errors.As(err, &amb) {
		var b strings.Builder
		fmt.Fprintf(&b, "wrktree: %v:\n", amb)
		for _, m := range amb.Matches {
			label := m.Branch
			if label == "" {
				label = m.Path
			}
			fmt.Fprintf(&b, "  %s\n", label)
		}
		fmt.Fprint(s.env.Stderr, b.String())
		return git.Worktree{}, errReported
	}
	return w, err
}

// findExisting is find for a worktree whose folder must exist.
func (s *session) findExisting(name string) (git.Worktree, error) {
	w, err := s.find(name)
	if err == nil && w.Prunable {
		return git.Worktree{}, errors.New("folder is missing: use prune")
	}
	return w, err
}

type lsFlags struct{ json, fetch, fresh bool }

func newLs(env Env) *cobra.Command {
	var f lsFlags
	c := &cobra.Command{
		Use:   "ls",
		Short: "List worktrees",
		Args:  cobra.NoArgs,
		RunE: func(c *cobra.Command, args []string) error {
			return runLs(c.Context(), env, f)
		},
	}
	c.Flags().BoolVar(&f.json, "json", false, "print JSON")
	c.Flags().BoolVar(&f.fetch, "fetch", false, "fetch the base branch first")
	c.Flags().BoolVar(&f.fresh, "fresh", false, "measure sizes again")
	return c
}

func runLs(ctx context.Context, env Env, f lsFlags) error {
	s, err := open(ctx, env)
	if err != nil {
		return err
	}
	base, err := s.base()
	if err != nil {
		return err
	}
	if f.fetch && base != "" {
		fctx, cancel := context.WithTimeout(s.ctx, 15*time.Second)
		err := s.r.FetchBase(fctx, base)
		cancel()
		if err != nil {
			fmt.Fprintln(env.Stderr, "wrktree: fetch failed: distances can be stale")
		}
	}
	wts, err := s.r.Worktrees(s.ctx)
	if err != nil {
		return err
	}
	now := env.Now()
	cache := disk.LoadCache(disk.DefaultCachePath())
	rows := status.LoadAll(s.ctx, s.r, wts, cache, s.repoKey(wts), status.Options{
		Base: base, TTL: s.cfg.SizeTTL, Fresh: f.fresh, Cwd: env.Dir, Now: now,
	})
	status.Sort(rows)
	if f.json {
		return status.JSON(env.Stdout, rows, base)
	}
	status.Table(env.Stdout, rows, base, now)
	return nil
}

func newSwitch(env Env) *cobra.Command {
	return &cobra.Command{
		Use:   "switch <name>",
		Short: "Change to a worktree",
		Args:  cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			s, err := open(c.Context(), env)
			if err != nil {
				return err
			}
			w, err := s.findExisting(args[0])
			if err != nil {
				return err
			}
			return shell.WriteCD(w.Path, env.Stdout, env.Stderr)
		},
	}
}

func newOpen(env Env) *cobra.Command {
	return &cobra.Command{
		Use:   "open <name>",
		Short: "Open a worktree in the editor",
		Args:  cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			s, err := open(c.Context(), env)
			if err != nil {
				return err
			}
			w, err := s.findExisting(args[0])
			if err != nil {
				return err
			}
			return s.openPath(w.Path)
		},
	}
}

func newInit(env Env) *cobra.Command {
	var name string
	c := &cobra.Command{
		Use:   "init <zsh|bash>",
		Short: "Print the shell function",
		Args:  cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			script, err := shell.InitScript(args[0], name)
			if err != nil {
				return err
			}
			_, err = fmt.Fprint(env.Stdout, script)
			return err
		},
	}
	c.Flags().StringVar(&name, "name", "wt", "name of the shell function")
	return c
}
