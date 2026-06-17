// Package correlate is the pure correlation core: it groups raw discovery
// observations into stable Tasks. It is deterministic and performs no IO.
//
// Dependency rule: correlate imports ONLY internal/model. Anything it needs
// that is not a model type (e.g. the id resolver) is expressed as a local,
// consumer-defined interface over model types — it never imports the store.
package correlate

import "github.com/unsafe9/clutch/internal/model"

// IDResolver mints/resolves/anchors stable task ids from durable representation
// signatures, plus the lifecycle ops the correlation core needs. The file
// backend's store.IDRegistry satisfies this interface; correlate depends on this
// narrow local interface to stay model-pure. (Retire is registry maintenance
// and is intentionally NOT part of this interface.)
type IDResolver interface {
	// Resolve returns the existing id for sig, or ok=false if none is anchored.
	Resolve(sig model.Signature) (id string, ok bool, err error)
	// Mint anchors a new stable id to sig and returns it.
	Mint(sig model.Signature) (id string, err error)
	// Attach anchors an additional signature to an existing id.
	Attach(id string, sig model.Signature) error
	// Merge folds mergeID into keepID and returns the surviving id.
	Merge(keepID, mergeID string) (id string, err error)
}

// AppraisalReader reads back persisted appraisals so correlation can reuse a
// cached classify/relation result instead of recomputing it. It is a
// consumer-defined interface over model types; the file backend satisfies it.
type AppraisalReader interface {
	// Appraisals returns the cached appraisals persisted for taskID.
	Appraisals(taskID string) ([]model.Appraisal, error)
}

// Correlate is the deterministic projection step: raw observations, the id
// resolver, and the appraisal cache in, correlated Tasks out. Pure — no IO, no
// git/fs/LLM.
func Correlate(obs model.Observations, ids IDResolver, appraisals AppraisalReader) ([]model.Task, error) {
	// TODO(wave2): group observations by signature, resolve/mint/attach/merge
	// ids, derive class-② representations, fold in cached appraisals read via
	// appraisals, infer class-③ lineage/relations/links, and emit unresolved
	// flags for the ambiguous remainder.
	panic("not implemented")
}
