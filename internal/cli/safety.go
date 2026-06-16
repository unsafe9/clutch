package cli

import "github.com/spf13/cobra"

// gate is the caller-agnostic safety gate every command passes through before
// touching the store (architecture invariant 3). It enforces the safety floor
// independently of which agent/system invoked the CLI. Placeholder.
func gate(cmd *cobra.Command, action string) error {
	// TODO(wave2): enforce confirmations, dry-run, and policy for `action`,
	// independent of the calling agent/system.
	panic("not implemented")
}
