package cli

import (
	"github.com/spf13/cobra"

	"github.com/unsafe9/clutch/internal/config"
	"github.com/unsafe9/clutch/internal/correlate"
	"github.com/unsafe9/clutch/internal/discover/fs"
	"github.com/unsafe9/clutch/internal/discover/git"
	"github.com/unsafe9/clutch/internal/discover/session"
	"github.com/unsafe9/clutch/internal/model"
	"github.com/unsafe9/clutch/internal/store/file"
)

// newScanCmd builds `clutch scan`: discover → correlate → project.
func newScanCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "scan",
		Short: "Scan configured roots and project the current Task set",
		RunE:  runScan,
	}
}

func runScan(cmd *cobra.Command, _ []string) error {
	env, err := project()
	if err != nil {
		return err
	}
	return emitEnvelope(cmd, env)
}

// project is the deterministic core pipeline and the composition root's wiring:
// load config → discover (git/fs/session) → correlate → envelope. Each callee
// is a later-wave stub today.
func project() (model.ProjectionEnvelope, error) {
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return model.ProjectionEnvelope{}, err
	}
	backend := file.New(cfg.StoreLocation)
	gitObs, err := git.Observe(cfg.SearchRoots)
	if err != nil {
		return model.ProjectionEnvelope{}, err
	}
	fsObs, err := fs.Observe(cfg.SearchRoots)
	if err != nil {
		return model.ProjectionEnvelope{}, err
	}
	sessObs, err := session.Observe()
	if err != nil {
		return model.ProjectionEnvelope{}, err
	}
	obs := model.Observations{Git: gitObs, FS: fsObs, Sessions: sessObs}
	tasks, err := correlate.Correlate(obs, backend)
	if err != nil {
		return model.ProjectionEnvelope{}, err
	}
	return model.ProjectionEnvelope{
		SchemaVersion: model.SchemaVersion,
		Tasks:         tasks,
	}, nil
}
