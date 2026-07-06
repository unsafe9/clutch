package cli

import (
	"github.com/unsafe9/clutch/internal/adapter"
	"github.com/unsafe9/clutch/internal/adapter/github"
	"github.com/unsafe9/clutch/internal/correlate"
	"github.com/unsafe9/clutch/internal/store"
	"github.com/unsafe9/clutch/internal/store/file"
)

// Composition-root contract: the concrete backends satisfy their ports, and the
// file backend additionally satisfies correlate's narrow resolver and appraisal
// reader — so the CLI can wire it straight into the pure correlation core. These
// assertions fail at compile time if any port drifts.
var (
	_ store.BoardStore              = (*file.Store)(nil)
	_ store.IDRegistry              = (*file.Store)(nil)
	_ correlate.IDResolver          = (*file.Store)(nil)
	_ correlate.AppraisalReader     = (*file.Store)(nil)
	_ correlate.InitiatedTaskReader = (*file.Store)(nil)
	_ adapter.IssueTracker          = (*github.Tracker)(nil)
)
