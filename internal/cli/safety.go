package cli

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

// confirmEnv is the documented non-interactive override for the safety gate: set
// it (to any non-empty value) to confirm mutating actions without --yes.
const confirmEnv = "CLUTCH_ASSUME_YES"

// gate is the caller-agnostic safety floor (architecture invariant 3) every
// command passes through before touching the store. It is independent of which
// agent/system invoked the CLI.
//
// Read actions (scan, tasks, task, or any action ending in ".read") always pass.
// Mutating actions require explicit confirmation: the persistent --yes flag or
// the documented CLUTCH_ASSUME_YES override env. Without confirmation a mutating
// action is rejected with an instruction to confirm.
func gate(_ *cobra.Command, action string) error {
	if isReadAction(action) {
		return nil
	}
	if assumeYes || os.Getenv(confirmEnv) != "" {
		return nil
	}
	return fmt.Errorf("action %q is mutating; confirm with --yes or set %s to proceed", action, confirmEnv)
}

// isReadAction reports whether action is a non-mutating read.
func isReadAction(action string) bool {
	switch action {
	case "scan", "tasks", "task":
		return true
	}
	return strings.HasSuffix(action, ".read")
}
