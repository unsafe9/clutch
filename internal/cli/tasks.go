package cli

import "github.com/spf13/cobra"

// newTasksCmd builds `clutch tasks`: list the projected tasks.
func newTasksCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "tasks",
		Short: "List the projected tasks",
		RunE: func(cmd *cobra.Command, _ []string) error {
			env, err := project()
			if err != nil {
				return err
			}
			return emitEnvelope(cmd, env)
		},
	}
}
