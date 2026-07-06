package cli

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/unsafe9/clutch/internal/model"
)

// newTaskCmd builds `clutch task <id>`: show a single task. Its `new`
// subcommand is the creation primitive for clutch-initiated work; `task <id>`
// still resolves for any id that is not the literal "new".
func newTaskCmd() *cobra.Command {
	cmd := &cobra.Command{
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
	cmd.AddCommand(newTaskNewCmd())
	return cmd
}

// newTaskNewCmd builds `clutch task new`: mint a clutch-initiated task. It has
// no git representation yet, so it starts at the idea lifecycle and appears in
// subsequent scans as a registry-only task until a branch is linked to its id.
func newTaskNewCmd() *cobra.Command {
	var title, mode, base string
	cmd := &cobra.Command{
		Use:   "new",
		Short: "Create a clutch-initiated task (starts at the idea lifecycle)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := gate(cmd, "task.new"); err != nil {
				return err
			}
			if title == "" {
				return fmt.Errorf("--title is required")
			}
			if mode != "" && !knownMode(mode) {
				return fmt.Errorf("--mode %q is not a known mode (cruise|steer)", mode)
			}
			s, err := openStore()
			if err != nil {
				return err
			}
			id, err := s.CreateInitiatedTask(title, model.Mode(mode), base, time.Now())
			if err != nil {
				return err
			}
			return emitConfirm(cmd, id, "task-new")
		},
	}
	cmd.Flags().StringVar(&title, "title", "", "task title (required)")
	cmd.Flags().StringVar(&mode, "mode", "", "execution mode (cruise|steer)")
	cmd.Flags().StringVar(&base, "base", "", "explicit base ref for the task")
	return cmd
}

// knownMode reports whether m is a recognized model.Mode value.
func knownMode(m string) bool {
	switch model.Mode(m) {
	case model.ModeCruise, model.ModeSteer:
		return true
	}
	return false
}
