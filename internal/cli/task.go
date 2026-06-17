package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/unsafe9/clutch/internal/model"
)

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
			id := args[0]
			for _, t := range env.Tasks {
				if t.ID == id {
					env.Tasks = []model.Task{t}
					return emitEnvelope(cmd, env)
				}
			}
			return fmt.Errorf("task %q not found", id)
		},
	}
}
