// Package correlate is the pure correlation core: it groups raw discovery
// observations into stable Tasks. It is deterministic and performs no IO.
//
// Dependency rule: correlate imports ONLY internal/model. Anything it needs
// that is not a model type (e.g. the id resolver) is expressed as a local,
// consumer-defined interface over model types — it never imports the store.
package correlate

import "github.com/unsafe9/clutch/internal/model"

// IDResolver mints/resolves stable task ids from durable representation
// signatures. The file backend's store.IDRegistry satisfies this interface;
// correlate depends on this narrow local interface to stay model-pure.
type IDResolver interface {
	// Resolve returns the existing id for sig, or ok=false if none is anchored.
	Resolve(sig model.Signature) (id string, ok bool, err error)
	// Mint anchors a new stable id to sig and returns it.
	Mint(sig model.Signature) (id string, err error)
}

// Correlate is the deterministic projection step: raw observations plus the id
// resolver in, correlated Tasks out. Pure — no IO, no git/fs/LLM.
func Correlate(obs model.Observations, ids IDResolver) ([]model.Task, error) {
	// TODO(wave2): group observations by signature, resolve/mint ids, derive
	// class-② representations, infer class-③ lineage/relations/links, and emit
	// unresolved flags for the ambiguous remainder.
	panic("not implemented")
}
