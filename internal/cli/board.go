package cli

import (
	"github.com/spf13/cobra"

	"github.com/unsafe9/clutch/internal/adapter"
	"github.com/unsafe9/clutch/internal/adapter/github"
	"github.com/unsafe9/clutch/internal/config"
	"github.com/unsafe9/clutch/internal/store/file"
)

// newBoardCmd builds `clutch board <task-id>`: show a task's board. The mutating
// board write commands are wired as subcommands; each passes the safety gate.
func newBoardCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "board <task-id>",
		Short: "Show a task's board (principles/design/questions/adrs/appraisals)",
		Args:  cobra.ExactArgs(1),
		RunE:  runBoard,
	}
	cmd.AddCommand(
		newSetPrinciplesCmd(),
		newSetDesignCmd(),
		newAddDecisionCmd(),
		newAddADRCmd(),
		newAppraiseCmd(),
		newAddQuestionCmd(),
		newResolveQuestionCmd(),
	)
	return cmd
}

func runBoard(cmd *cobra.Command, args []string) error {
	// Every command path passes the caller-agnostic safety gate first.
	if err := gate(cmd, "board.read"); err != nil {
		return err
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return err
	}
	backend := file.New(cfg.StoreLocation)
	var tracker adapter.IssueTracker = github.New() // wave2: issue enrichment
	_ = tracker
	board, err := backend.Get(args[0])
	if err != nil {
		return err
	}
	// The contract's machine shape renders each documented board array as [],
	// never null.
	normalizeBoard(board)
	return emitBoard(cmd, board)
}
