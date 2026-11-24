package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

func newPrune(env Env) *cobra.Command {
	var yes bool
	c := &cobra.Command{
		Use:   "prune",
		Short: "Remove entries of worktrees whose folder is missing",
		Args:  cobra.NoArgs,
		RunE: func(c *cobra.Command, args []string) error {
			s, err := open(c.Context(), env)
			if err != nil {
				return err
			}
			lines, err := s.r.PruneDryRun(s.ctx)
			if err != nil {
				return err
			}
			if len(lines) == 0 {
				fmt.Fprintln(env.Stderr, "nothing to prune")
				return nil
			}
			fmt.Fprintln(env.Stderr, strings.Join(lines, "\n"))
			if !yes {
				ok, err := confirm(env, fmt.Sprintf("Prune %d entries? [y/N] ", len(lines)), yesOrY)
				if err != nil {
					return err
				}
				if !ok {
					fmt.Fprintln(env.Stderr, "cancelled")
					return nil
				}
			}
			if err := s.r.Prune(s.ctx); err != nil {
				return err
			}
			fmt.Fprintf(env.Stderr, "pruned %d entries\n", len(lines))
			return nil
		},
	}
	c.Flags().BoolVar(&yes, "yes", false, "prune without a prompt")
	return c
}
