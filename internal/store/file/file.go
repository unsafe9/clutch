// Package file is the default out-of-repo file backend. A single Store
// implements both the BoardStore and IDRegistry ports, rooted at a directory
// that lives outside any scanned repo.
package file

import (
	"github.com/unsafe9/clutch/internal/model"
	"github.com/unsafe9/clutch/internal/store"
)

// Store is a file-backed BoardStore + IDRegistry rooted at an out-of-repo dir.
type Store struct {
	// Root is the out-of-repo directory holding boards and the id registry.
	Root string
}

// Compile-time proof that *Store implements both ports.
var (
	_ store.BoardStore = (*Store)(nil)
	_ store.IDRegistry = (*Store)(nil)
)

// New returns a file-backed store rooted at root.
func New(root string) *Store {
	// TODO(wave1-c): ensure root exists and load the id-registry index.
	return &Store{Root: root}
}

// Get implements store.BoardStore.
func (s *Store) Get(taskID string) (*model.Board, error) {
	// TODO(wave1-c): read <root>/boards/<taskID>.json.
	panic("not implemented")
}

// SetPrinciples implements store.BoardStore.
func (s *Store) SetPrinciples(taskID, principles string) error {
	// TODO(wave1-c)
	panic("not implemented")
}

// SetDesign implements store.BoardStore.
func (s *Store) SetDesign(taskID, design string) error {
	// TODO(wave1-c)
	panic("not implemented")
}

// AppendDecision implements store.BoardStore.
func (s *Store) AppendDecision(taskID string, d model.Decision) error {
	// TODO(wave1-c)
	panic("not implemented")
}

// AddADR implements store.BoardStore.
func (s *Store) AddADR(taskID string, adr model.ADR) error {
	// TODO(wave1-c)
	panic("not implemented")
}

// Query implements store.BoardStore.
func (s *Store) Query(q store.Query) (*store.QueryResult, error) {
	// TODO(wave1-c)
	panic("not implemented")
}

// Resolve implements store.IDRegistry.
func (s *Store) Resolve(sig model.Signature) (id string, ok bool, err error) {
	// TODO(wave1-c): look up sig in the id-registry index.
	panic("not implemented")
}

// Mint implements store.IDRegistry.
func (s *Store) Mint(sig model.Signature) (id string, err error) {
	// TODO(wave1-c): allocate a new id and anchor it to sig.
	panic("not implemented")
}

// Attach implements store.IDRegistry.
func (s *Store) Attach(id string, sig model.Signature) error {
	// TODO(wave1-c): anchor an additional signature to an existing id.
	panic("not implemented")
}

// Merge implements store.IDRegistry.
func (s *Store) Merge(keepID, mergeID string) (id string, err error) {
	// TODO(wave1-c): fold mergeID into keepID and return the survivor.
	panic("not implemented")
}

// Retire implements store.IDRegistry.
func (s *Store) Retire(id string) error {
	// TODO(wave1-c): mark id retired without deleting board knowledge.
	panic("not implemented")
}

// Appraisals implements correlate.AppraisalReader.
func (s *Store) Appraisals(taskID string) ([]model.Appraisal, error) {
	// TODO(wave1-c): read cached appraisals from <root>/boards/<taskID>.json.
	panic("not implemented")
}
