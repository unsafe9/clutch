package cli

import (
	"io"

	"github.com/spf13/cobra"

	"github.com/unsafe9/clutch/internal/model"
)

// Output is split into two distinct paths (architecture invariant 3): the
// machine path emits the stable, schema-versioned ProjectionEnvelope; the human
// path emits a TTY rendering. The --json flag selects between them.

// emitEnvelope renders a projection in machine (JSON) or human (TTY) form.
func emitEnvelope(cmd *cobra.Command, env model.ProjectionEnvelope) error {
	if jsonOutput {
		return emitJSON(cmd.OutOrStdout(), env)
	}
	return emitHumanEnvelope(cmd.OutOrStdout(), env)
}

// emitBoard renders a board in machine (JSON) or human (TTY) form.
func emitBoard(cmd *cobra.Command, b *model.Board) error {
	if jsonOutput {
		return emitJSON(cmd.OutOrStdout(), b)
	}
	return emitHumanBoard(cmd.OutOrStdout(), b)
}

// emitJSON writes the stable, schema-versioned machine contract.
func emitJSON(w io.Writer, v any) error {
	// TODO(wave1-d): deterministic, indented JSON encoding.
	panic("not implemented")
}

// emitHumanEnvelope writes a human/TTY rendering of a projection.
func emitHumanEnvelope(w io.Writer, env model.ProjectionEnvelope) error {
	// TODO(wave1-d): TTY table rendering.
	panic("not implemented")
}

// emitHumanBoard writes a human/TTY rendering of a board.
func emitHumanBoard(w io.Writer, b *model.Board) error {
	// TODO(wave1-d): TTY board rendering.
	panic("not implemented")
}
