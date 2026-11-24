package cmd

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/dev-shetty/wrktree/internal/disk"
	"github.com/dev-shetty/wrktree/internal/git"
)

type rmFlags struct{ force, branch bool }

func newRm(env Env) *cobra.Command {
	var f rmFlags
	c := &cobra.Command{
		Use:   "rm <name>",
		Short: "Delete a worktree",
		Args:  cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			return runRm(c, env, args[0], f)
		},
	}
	c.Flags().BoolVar(&f.force, "force", false, "delete without a prompt")
	c.Flags().BoolVar(&f.branch, "branch", false, "delete the branch too, if it is merged")
	return c
}

func runRm(c *cobra.Command, env Env, name string, f rmFlags) error {
	s, err := open(c.Context(), env)
	if err != nil {
		return err
	}
	wts, err := s.r.Worktrees(s.ctx)
	if err != nil {
		return err
	}
	wt, err := s.find(name)
	if err != nil {
		return err
	}
	plan, err := s.r.PlanDelete(s.ctx, wt)
	if err != nil {
		return err
	}
	// --force never overrides a blocked plan or the current worktree.
	if plan.Blocked != "" {
		return errors.New(plan.Blocked)
	}
	// The shell is in the worktree if the directory is in its folder, also
	// inside a nested worktree.
	if git.Contains(wt.Path, env.Dir) {
		return errors.New("cannot delete the worktree of the current directory")
	}

	if !f.force {
		var ok bool
		if plan.NeedsYes {
			if !env.IsTerminal() {
				return errors.New("worktree has changes: use --force")
			}
			ok, err = confirm(env, git.DeletePrompt(wt, plan)+"Type yes to delete: ", func(a string) bool { return a == "yes" })
		} else {
			ok, err = confirm(env, git.DeletePrompt(wt, plan)+"[y/N] ", yesOrY)
		}
		if err != nil {
			return err
		}
		if !ok {
			fmt.Fprintln(env.Stderr, "cancelled")
			return nil
		}
	}

	if err := s.r.Remove(s.ctx, wt.Path, f.force || plan.NeedsYes); err != nil {
		return err
	}
	// A cache failure does not fail the command: the worktree is already gone.
	cache := disk.LoadCache(disk.DefaultCachePath())
	cache.Delete(s.repoKey(wts), wt.Path)
	_ = cache.Save()

	if f.branch && wt.Branch != "" {
		if err := s.r.DeleteBranch(s.ctx, wt.Branch); err != nil {
			return fmt.Errorf("deleted %s, but the branch stays: %w", wt.Path, err)
		}
	}
	fmt.Fprintf(env.Stdout, "deleted %s\n", wt.Path)
	return nil
}

// yesOrY accepts "y" or "yes" in any case.
func yesOrY(answer string) bool {
	return strings.EqualFold(answer, "y") || strings.EqualFold(answer, "yes")
}

// confirm prints prompt on stderr and reads one line from stdin. It returns
// true only if accept approves the trimmed line. A last line without a
// newline is an answer too. A read error, or EOF with no answer, prints
// "cancelled" and returns errReported, so the exit code is 1.
func confirm(env Env, prompt string, accept func(answer string) bool) (bool, error) {
	fmt.Fprint(env.Stderr, prompt)
	line, err := bufio.NewReader(env.Stdin).ReadString('\n')
	answer := strings.TrimSpace(line)
	if err != nil && !(errors.Is(err, io.EOF) && answer != "") {
		fmt.Fprintln(env.Stderr)
		fmt.Fprintln(env.Stderr, "cancelled")
		return false, errReported
	}
	return accept(answer), nil
}
