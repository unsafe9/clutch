package cli

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/unsafe9/clutch/internal/config"
	"github.com/unsafe9/clutch/internal/model"
	"github.com/unsafe9/clutch/internal/store/file"
)

// openStore loads config and returns the file-backed store. Every board write
// command passes the safety gate (board.write) before reaching the store.
func openStore() (*file.Store, error) {
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return nil, err
	}
	return file.New(cfg.StoreLocation), nil
}

// readMessage returns the -m/--message value, or stdin when -m is absent.
func readMessage(cmd *cobra.Command, msg string) (string, error) {
	if msg != "" {
		return msg, nil
	}
	data, err := io.ReadAll(cmd.InOrStdin())
	if err != nil {
		return "", err
	}
	return strings.TrimRight(string(data), "\n"), nil
}

func newSetPrinciplesCmd() *cobra.Command {
	var msg string
	cmd := &cobra.Command{
		Use:   "set-principles <task-id>",
		Short: "Set a task's work principles (-m/--message or stdin)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := gate(cmd, "board.write"); err != nil {
				return err
			}
			s, err := openStore()
			if err != nil {
				return err
			}
			principles, err := readMessage(cmd, msg)
			if err != nil {
				return err
			}
			if err := s.SetPrinciples(args[0], principles); err != nil {
				return err
			}
			return emitConfirm(cmd, args[0], "set-principles")
		},
	}
	cmd.Flags().StringVarP(&msg, "message", "m", "", "principles text (stdin if omitted)")
	return cmd
}

func newSetDesignCmd() *cobra.Command {
	var msg string
	cmd := &cobra.Command{
		Use:   "set-design <task-id>",
		Short: "Set a task's design (-m/--message or stdin)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := gate(cmd, "board.write"); err != nil {
				return err
			}
			s, err := openStore()
			if err != nil {
				return err
			}
			design, err := readMessage(cmd, msg)
			if err != nil {
				return err
			}
			if err := s.SetDesign(args[0], design); err != nil {
				return err
			}
			return emitConfirm(cmd, args[0], "set-design")
		},
	}
	cmd.Flags().StringVarP(&msg, "message", "m", "", "design text (stdin if omitted)")
	return cmd
}

func newAddDecisionCmd() *cobra.Command {
	var summary, detail string
	cmd := &cobra.Command{
		Use:   "add-decision <task-id>",
		Short: "Append a design decision to a task",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := gate(cmd, "board.write"); err != nil {
				return err
			}
			if summary == "" {
				return fmt.Errorf("--summary is required")
			}
			s, err := openStore()
			if err != nil {
				return err
			}
			if err := s.AppendDecision(args[0], model.Decision{Summary: summary, Detail: detail}); err != nil {
				return err
			}
			return emitConfirm(cmd, args[0], "add-decision")
		},
	}
	cmd.Flags().StringVar(&summary, "summary", "", "decision summary (required)")
	cmd.Flags().StringVar(&detail, "detail", "", "decision detail")
	return cmd
}

func newAddADRCmd() *cobra.Command {
	var decision, context, consequence string
	var alternatives []string
	cmd := &cobra.Command{
		Use:   "add-adr <task-id>",
		Short: "Append an architecture decision record to a task",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := gate(cmd, "board.write"); err != nil {
				return err
			}
			if decision == "" {
				return fmt.Errorf("--decision is required")
			}
			s, err := openStore()
			if err != nil {
				return err
			}
			adr := model.ADR{
				Decision:     decision,
				Context:      context,
				Alternatives: alternatives,
				Consequence:  consequence,
			}
			if err := s.AddADR(args[0], adr); err != nil {
				return err
			}
			return emitConfirm(cmd, args[0], "add-adr")
		},
	}
	cmd.Flags().StringVar(&decision, "decision", "", "the decision (required)")
	cmd.Flags().StringVar(&context, "context", "", "decision context")
	cmd.Flags().StringArrayVar(&alternatives, "alternatives", nil, "alternative considered (repeatable)")
	cmd.Flags().StringVar(&consequence, "consequence", "", "decision consequence")
	return cmd
}

func newAppraiseCmd() *cobra.Command {
	var kind, subject, result, fingerprint string
	var confidence float64
	cmd := &cobra.Command{
		Use:   "appraise <task-id>",
		Short: "Cache an appraisal result on a task's board",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := gate(cmd, "board.write"); err != nil {
				return err
			}
			if confidence < 0 || confidence >= 1 {
				return fmt.Errorf("--confidence must be in [0,1), got %v", confidence)
			}
			if !knownAppraisalKind(kind) {
				return fmt.Errorf("--kind %q is not a known appraisal kind (classification|relation|link)", kind)
			}
			if kind == string(model.AppraisalClassification) && !knownLifecycle(result) {
				return fmt.Errorf("--result %q is not a known lifecycle for a classification appraisal", result)
			}
			s, err := openStore()
			if err != nil {
				return err
			}
			a := model.Appraisal{
				Kind:             model.AppraisalKind(kind),
				Subject:          model.RepRef(subject),
				Result:           result,
				Confidence:       confidence,
				InputFingerprint: fingerprint,
				ComputedAt:       time.Now(),
			}
			if err := s.AddAppraisal(args[0], a); err != nil {
				return err
			}
			return emitConfirm(cmd, args[0], "appraise")
		},
	}
	cmd.Flags().StringVar(&kind, "kind", "", "appraisal kind (classification|relation|link)")
	cmd.Flags().StringVar(&subject, "subject", "", "subject RepRef the appraisal concerns")
	cmd.Flags().StringVar(&result, "result", "", "appraisal result")
	cmd.Flags().Float64Var(&confidence, "confidence", 0, "confidence in [0,1)")
	cmd.Flags().StringVar(&fingerprint, "fingerprint", "", "input fingerprint")
	return cmd
}

// knownAppraisalKind reports whether k is a recognized model.AppraisalKind.
func knownAppraisalKind(k string) bool {
	switch model.AppraisalKind(k) {
	case model.AppraisalClassification, model.AppraisalRelation, model.AppraisalLink:
		return true
	}
	return false
}

// knownLifecycle reports whether r is a recognized model.Lifecycle value.
func knownLifecycle(r string) bool {
	switch model.Lifecycle(r) {
	case model.LifecycleIdea, model.LifecyclePlanned, model.LifecycleActive,
		model.LifecycleReview, model.LifecycleMerged, model.LifecycleDone,
		model.LifecycleStale, model.LifecycleSuperseded:
		return true
	}
	return false
}
