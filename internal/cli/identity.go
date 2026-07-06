package cli

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"

	"github.com/unsafe9/clutch/internal/model"
)

// identityStamper records and returns a task's persisted identity metadata
// (created/updated timestamps and the stored policy mode) given the task's
// current representation fingerprint. The file backend satisfies it.
type identityStamper interface {
	Identity(id, fingerprint string) (created, updated time.Time, mode model.Mode, err error)
}

// fillIdentity stamps each task's persisted Class-① identity metadata from the
// registry: created/updated come from the store (updated advances only when the
// task's representation fingerprint changes between scans), and mode falls back
// to the human-in-the-loop default "steer" when the store holds no explicit
// policy mode.
func fillIdentity(tasks []model.Task, reg identityStamper) error {
	for i := range tasks {
		// A clutch-initiated task carries its persisted created/updated and
		// stored mode from the registry via correlation's materialization, so
		// the fingerprint stamper (which governs git-detected tasks, whose
		// Class ① is re-derived from observations each scan) must not overwrite
		// them. The effective-mode default still applies uniformly.
		if tasks[i].Provenance == model.ProvenanceClutchInitiated {
			if tasks[i].Mode == "" {
				tasks[i].Mode = model.ModeSteer
			}
			continue
		}
		created, updated, mode, err := reg.Identity(tasks[i].ID, fingerprint(tasks[i]))
		if err != nil {
			return err
		}
		tasks[i].Created = created
		tasks[i].Updated = updated
		if mode == "" {
			mode = model.ModeSteer
		}
		tasks[i].Mode = mode
	}
	return nil
}

// fingerprint digests only the derived/observed state of a task so that its
// stored `updated` advances exactly when a scan sees the task's representations
// or relations change. Identity & policy fields (id/created/updated/mode/board)
// are stamping OUTPUTS, not inputs, so they are excluded to avoid churn.
func fingerprint(t model.Task) string {
	t.ID = ""
	t.Created = time.Time{}
	t.Updated = time.Time{}
	t.Mode = ""
	t.Board = nil
	data, _ := json.Marshal(t)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
