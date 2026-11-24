package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/dev-shetty/wrktree/internal/git"
)

func newSync(env Env) *cobra.Command {
	return &cobra.Command{
		Use:   "sync <name>",
		Short: "Merge the base branch into the branch of a worktree",
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
			base, err := s.base()
			if err != nil {
				return err
			}
			res, err := s.r.Sync(s.ctx, w, base)
			if err != nil {
				return err
			}
			if res.FetchFailed {
				fmt.Fprintf(env.Stderr, "wrktree: fetch failed: used the local copy of %s\n", base)
			}
			switch {
			case len(res.Conflicts) > 0:
				return fmt.Errorf("sync failed: conflict in %s", git.ConflictText(res.Conflicts))
			case res.UpToDate:
				fmt.Fprintf(env.Stdout, "%s is already up to date with %s\n", w.Branch, base)
			default:
				fmt.Fprintf(env.Stdout, "synced %s with %s\n", w.Branch, base)
			}
			return nil
		},
	}
}
