package cli

import "github.com/spf13/cobra"

// newTaskCmd builds `clutch task <id>`: show a single task.
func newTaskCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "task <id>",
		Short: "Show a single task",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			env, err := project()
			if err != nil {
				return err
			}
			// TODO(wave1-d): filter env.Tasks to args[0].
			return emitEnvelope(cmd, env)
		},
	}
}
