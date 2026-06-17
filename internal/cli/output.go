package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"text/tabwriter"

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

// emitConfirm reports a completed write. In machine mode it emits a small stable
// JSON object; in human mode a one-line message. Honors the --json/human split.
func emitConfirm(cmd *cobra.Command, taskID, action string) error {
	if jsonOutput {
		return emitJSON(cmd.OutOrStdout(), map[string]string{
			"task_id": taskID,
			"action":  action,
			"status":  "ok",
		})
	}
	_, err := fmt.Fprintf(cmd.OutOrStdout(), "%s: %s\n", action, taskID)
	return err
}

// emitJSON writes the stable, schema-versioned machine contract: deterministic
// two-space-indented JSON plus a trailing newline. It MUST stay deterministic —
// no timestamps or other non-deterministic content injected here.
func emitJSON(w io.Writer, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	if _, err := w.Write(b); err != nil {
		return err
	}
	_, err = io.WriteString(w, "\n")
	return err
}

// emitHumanEnvelope writes a compact TTY table of tasks plus a one-line scan
// summary.
func emitHumanEnvelope(w io.Writer, env model.ProjectionEnvelope) error {
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tTITLE\tLIFECYCLE\tMODE\tREPRESENTATIONS")
	for _, t := range env.Tasks {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n",
			t.ID, t.Title, t.Lifecycle, t.Mode, rollup(t))
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	s := env.Diagnostics.ScanStats
	_, err := fmt.Fprintf(w,
		"scan: %d repos, %d worktrees, %d sessions, %d tasks in %dms\n",
		s.ReposScanned, s.Worktrees, s.Sessions, s.TasksProjected, s.DurationMS)
	return err
}

// rollup is a short branches/PRs/sessions count summary for a task.
func rollup(t model.Task) string {
	return fmt.Sprintf("%db/%dpr/%ds", len(t.Branches), len(t.PRs), len(t.Sessions))
}

// emitHumanBoard writes a readable, sectioned TTY rendering of a board.
func emitHumanBoard(w io.Writer, b *model.Board) error {
	if _, err := fmt.Fprintf(w, "Principles:\n%s\n\n", b.Principles); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "Design:\n%s\n\n", b.Design); err != nil {
		return err
	}
	if _, err := io.WriteString(w, "ADRs:\n"); err != nil {
		return err
	}
	for _, a := range b.ADRs {
		if _, err := fmt.Fprintf(w, "  - %s\n    context: %s\n    consequence: %s\n",
			a.Decision, a.Context, a.Consequence); err != nil {
			return err
		}
	}
	if _, err := io.WriteString(w, "\nAppraisals:\n"); err != nil {
		return err
	}
	for _, ap := range b.Appraisals {
		if _, err := fmt.Fprintf(w, "  - [%s] %s -> %s (%.2f)\n",
			ap.Kind, ap.Subject, ap.Result, ap.Confidence); err != nil {
			return err
		}
	}
	return nil
}
