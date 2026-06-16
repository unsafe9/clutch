// Package cli builds the clutch cobra command tree. Together with cmd/clutch it
// is the composition root — the only place that imports across discover,
// correlate, store, adapter and config. The CLI is clutch's sole public
// boundary (everything else lives under internal/, so Go-import is blocked).
package cli

import "github.com/spf13/cobra"

// Persistent flags shared by all commands.
var (
	cfgPath    string
	jsonOutput bool
)

// NewRootCmd builds the root `clutch` command and wires its subcommands.
func NewRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "clutch",
		Short: "Agent-neutral CLI gateway to the Task+Board store",
		Long: "clutch is the authoritative gateway to a Task+Board store. " +
			"External agents/systems consume it via the CLI (JSON), never by " +
			"importing Go.",
		SilenceUsage: true,
	}
	root.PersistentFlags().StringVar(&cfgPath, "config", "",
		"path to clutch config")
	root.PersistentFlags().BoolVar(&jsonOutput, "json", false,
		"emit machine (JSON) output instead of human/TTY output")
	root.AddCommand(newScanCmd(), newTasksCmd(), newTaskCmd(), newBoardCmd())
	return root
}
