// Package file is the default out-of-repo file backend. A single Store
// implements both the BoardStore and IDRegistry ports, rooted at a directory
// that lives outside any scanned repo.
package file

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/unsafe9/clutch/internal/model"
	"github.com/unsafe9/clutch/internal/store"
)

// Store is a file-backed BoardStore + IDRegistry rooted at an out-of-repo dir.
//
// Layout under Root:
//   - boards/<taskID>.json   one model.Board per task
//   - registry.json          the id registry (signature index + per-id metadata)
//
// Id scheme: each minted id is a 16-hex-char (8-byte) random string from
// crypto/rand. It is independent of any representation and persisted in the
// registry, so the same signature resolves to the same id across restarts.
type Store struct {
	// Root is the out-of-repo directory holding boards and the id registry.
	Root string
	// now supplies the wall clock for identity timestamps; overridable in tests
	// to keep timestamp behavior deterministic.
	now func() time.Time

	mu       sync.Mutex
	registry registry
}

// registry is the persisted id-registry index.
type registry struct {
	// Sigs maps a signature key to the id it anchors.
	Sigs map[string]string `json:"sigs"`
	// Ids holds per-id metadata.
	Ids map[string]*idMeta `json:"ids"`
}

// idMeta is per-id registry metadata. Created/Updated are the durable identity
// timestamps surfaced in the projection; Fingerprint is the digest of the task's
// representations recorded at the last Updated, so a later scan can tell whether
// the observed state changed. Mode is the stored policy mode, set only by an
// explicit policy write (none exists yet), NOT the effective projection default.
type idMeta struct {
	Retired     bool       `json:"retired"`
	MergedInto  string     `json:"merged_into,omitempty"`
	Created     time.Time  `json:"created"`
	Updated     time.Time  `json:"updated"`
	Fingerprint string     `json:"fingerprint,omitempty"`
	Mode        model.Mode `json:"mode,omitempty"`
	// Initiated holds the persisted Class ① identity/policy for a
	// clutch-initiated task (created via `clutch task new`) before it has any
	// git representation. It is nil for git-detected ids, whose Class ① is
	// derived from observations each scan; its presence marks clutch-initiated
	// provenance.
	Initiated *initiatedMeta `json:"initiated,omitempty"`
}

// initiatedMeta is the persisted identity/policy of a clutch-initiated task.
type initiatedMeta struct {
	Title   string    `json:"title"`
	Mode    string    `json:"mode,omitempty"`
	Base    string    `json:"base,omitempty"`
	Created time.Time `json:"created"`
}

// Compile-time proof that *Store implements both ports.
var (
	_ store.BoardStore = (*Store)(nil)
	_ store.IDRegistry = (*Store)(nil)
)

// New returns a file-backed store rooted at root, ensuring root and its boards
// directory exist and loading (or lazily creating) the id-registry index.
func New(root string) *Store {
	s := &Store{Root: root, now: time.Now}
	_ = os.MkdirAll(s.boardsDir(), 0o755)
	s.registry = s.loadRegistry()
	return s
}

func (s *Store) boardsDir() string    { return filepath.Join(s.Root, "boards") }
func (s *Store) registryPath() string { return filepath.Join(s.Root, "registry.json") }
func (s *Store) boardPath(taskID string) string {
	return filepath.Join(s.boardsDir(), taskID+".json")
}

// validateTaskID rejects ids that are not a single safe path segment, so a board
// read/write cannot escape boards/ or clobber the registry via path traversal.
func validateTaskID(taskID string) error {
	if taskID == "" || taskID == "." || taskID == ".." ||
		strings.ContainsAny(taskID, "/\\") || filepath.Base(taskID) != taskID {
		return fmt.Errorf("invalid task id %q", taskID)
	}
	return nil
}

// loadRegistry reads registry.json or returns an empty registry if absent.
func (s *Store) loadRegistry() registry {
	r := registry{Sigs: map[string]string{}, Ids: map[string]*idMeta{}}
	data, err := os.ReadFile(s.registryPath())
	if err != nil {
		return r
	}
	_ = json.Unmarshal(data, &r)
	if r.Sigs == nil {
		r.Sigs = map[string]string{}
	}
	if r.Ids == nil {
		r.Ids = map[string]*idMeta{}
	}
	return r
}

// persistRegistry atomically writes the in-memory registry to disk. Caller holds mu.
func (s *Store) persistRegistry() error {
	return writeJSONAtomic(s.registryPath(), s.registry)
}

// sigKey derives a stable index key for a single durable signature.
func sigKey(sig model.Signature) string {
	if sig.IssueLink != "" {
		return "issue\x00" + sig.IssueLink
	}
	return "repo\x00" + sig.Repo + "\x00" + sig.Branch
}

// Get implements store.BoardStore. A missing board file yields a zero-value
// &model.Board{} with nil error (callers expect an empty board, not an error).
func (s *Store) Get(taskID string) (*model.Board, error) {
	if err := validateTaskID(taskID); err != nil {
		return nil, err
	}
	data, err := os.ReadFile(s.boardPath(taskID))
	if os.IsNotExist(err) {
		return &model.Board{}, nil
	}
	if err != nil {
		return nil, err
	}
	var b model.Board
	if err := json.Unmarshal(data, &b); err != nil {
		return nil, err
	}
	return &b, nil
}

// SetPrinciples implements store.BoardStore.
func (s *Store) SetPrinciples(taskID, principles string) error {
	return s.mutateBoard(taskID, func(b *model.Board) {
		b.Principles = principles
	})
}

// SetDesign implements store.BoardStore.
func (s *Store) SetDesign(taskID, design string) error {
	return s.mutateBoard(taskID, func(b *model.Board) {
		b.Design = design
	})
}

// AppendDecision implements store.BoardStore. The board has no Decisions slice;
// a decision is folded into Design in a stable, readable form.
func (s *Store) AppendDecision(taskID string, d model.Decision) error {
	return s.mutateBoard(taskID, func(b *model.Board) {
		entry := "- " + d.Summary
		if d.Detail != "" {
			entry += ": " + d.Detail
		}
		if b.Design == "" {
			b.Design = entry
		} else {
			b.Design = b.Design + "\n" + entry
		}
	})
}

// AddADR implements store.BoardStore.
func (s *Store) AddADR(taskID string, adr model.ADR) error {
	return s.mutateBoard(taskID, func(b *model.Board) {
		b.ADRs = append(b.ADRs, adr)
	})
}

// AddAppraisal implements store.BoardStore: an existing appraisal with the same
// Kind+Subject is replaced (a recomputation supersedes it), otherwise appended.
// Appraisals are kept ordered by Kind then Subject on write.
func (s *Store) AddAppraisal(taskID string, a model.Appraisal) error {
	return s.mutateBoard(taskID, func(b *model.Board) {
		replaced := false
		for i := range b.Appraisals {
			if b.Appraisals[i].Kind == a.Kind && b.Appraisals[i].Subject == a.Subject {
				b.Appraisals[i] = a
				replaced = true
				break
			}
		}
		if !replaced {
			b.Appraisals = append(b.Appraisals, a)
		}
		sort.Slice(b.Appraisals, func(i, j int) bool {
			if b.Appraisals[i].Kind != b.Appraisals[j].Kind {
				return b.Appraisals[i].Kind < b.Appraisals[j].Kind
			}
			return b.Appraisals[i].Subject < b.Appraisals[j].Subject
		})
	})
}

// mutateBoard does an atomic read-modify-write of one task board.
func (s *Store) mutateBoard(taskID string, fn func(*model.Board)) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, err := s.Get(taskID)
	if err != nil {
		return err
	}
	fn(b)
	return writeJSONAtomic(s.boardPath(taskID), b)
}

// Query implements store.BoardStore: a case-insensitive substring search of
// q.Text over each board's Principles/Design/ADRs, scoped to q.TaskIDs when set.
func (s *Store) Query(q store.Query) (*store.QueryResult, error) {
	res := &store.QueryResult{Tasks: []string{}, Decisions: []model.Decision{}, ADRs: []model.ADR{}}

	taskIDs, err := s.queryTaskIDs(q.TaskIDs)
	if err != nil {
		return nil, err
	}
	needle := strings.ToLower(q.Text)

	for _, taskID := range taskIDs {
		b, err := s.Get(taskID)
		if err != nil {
			return nil, err
		}
		matched := false
		if strings.Contains(strings.ToLower(b.Principles), needle) ||
			strings.Contains(strings.ToLower(b.Design), needle) {
			matched = true
		}
		for _, adr := range b.ADRs {
			if adrMatches(adr, needle) {
				matched = true
				res.ADRs = append(res.ADRs, adr)
			}
		}
		if matched {
			res.Tasks = append(res.Tasks, taskID)
		}
	}
	return res, nil
}

func adrMatches(adr model.ADR, needle string) bool {
	if strings.Contains(strings.ToLower(adr.Decision), needle) ||
		strings.Contains(strings.ToLower(adr.Context), needle) ||
		strings.Contains(strings.ToLower(adr.Consequence), needle) {
		return true
	}
	for _, alt := range adr.Alternatives {
		if strings.Contains(strings.ToLower(alt), needle) {
			return true
		}
	}
	return false
}

// queryTaskIDs returns the scope of task ids: the explicit scope when non-empty,
// otherwise every board id discovered on disk.
func (s *Store) queryTaskIDs(scope []string) ([]string, error) {
	if len(scope) > 0 {
		return scope, nil
	}
	entries, err := os.ReadDir(s.boardsDir())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var ids []string
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".json") {
			continue
		}
		ids = append(ids, strings.TrimSuffix(name, ".json"))
	}
	return ids, nil
}

// Resolve implements store.IDRegistry.
func (s *Store) Resolve(sig model.Signature) (id string, ok bool, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	id, ok = s.registry.Sigs[sigKey(sig)]
	return id, ok, nil
}

// Mint implements store.IDRegistry.
func (s *Store) Mint(sig model.Signature) (id string, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := sigKey(sig)
	if existing, ok := s.registry.Sigs[key]; ok {
		return existing, nil
	}
	id, err = s.newID()
	if err != nil {
		return "", err
	}
	s.registry.Sigs[key] = id
	s.registry.Ids[id] = &idMeta{}
	if err := s.persistRegistry(); err != nil {
		return "", err
	}
	return id, nil
}

// Attach implements store.IDRegistry.
func (s *Store) Attach(id string, sig model.Signature) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.registry.Ids[id]; !ok {
		s.registry.Ids[id] = &idMeta{}
	}
	s.registry.Sigs[sigKey(sig)] = id
	return s.persistRegistry()
}

// Merge implements store.IDRegistry: repoint mergeID's signatures to keepID,
// mark mergeID merged-into keepID, and return keepID.
func (s *Store) Merge(keepID, mergeID string) (id string, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for k, v := range s.registry.Sigs {
		if v == mergeID {
			s.registry.Sigs[k] = keepID
		}
	}
	if _, ok := s.registry.Ids[keepID]; !ok {
		s.registry.Ids[keepID] = &idMeta{}
	}
	if _, ok := s.registry.Ids[mergeID]; !ok {
		s.registry.Ids[mergeID] = &idMeta{}
	}
	s.registry.Ids[mergeID].MergedInto = keepID
	if err := s.persistRegistry(); err != nil {
		return "", err
	}
	return keepID, nil
}

// Retire implements store.IDRegistry: mark id retired without deleting the board.
func (s *Store) Retire(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.registry.Ids[id]; !ok {
		s.registry.Ids[id] = &idMeta{}
	}
	s.registry.Ids[id].Retired = true
	return s.persistRegistry()
}

// Identity records and returns the durable identity metadata for id given the
// task's current representation fingerprint. created is set once, when the id is
// first seen; updated advances to now() whenever fingerprint differs from the
// value recorded at the last update (i.e. the task's observed state changed
// between scans); mode is the stored policy mode, empty until an explicit policy
// write records one. Any change is persisted before returning.
func (s *Store) Identity(id, fingerprint string) (created, updated time.Time, mode model.Mode, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, ok := s.registry.Ids[id]
	if !ok {
		m = &idMeta{}
		s.registry.Ids[id] = m
	}
	changed := false
	switch {
	case m.Created.IsZero():
		now := s.now().UTC()
		m.Created, m.Updated, m.Fingerprint = now, now, fingerprint
		changed = true
	case m.Fingerprint != fingerprint:
		m.Updated, m.Fingerprint = s.now().UTC(), fingerprint
		changed = true
	}
	if changed {
		if err := s.persistRegistry(); err != nil {
			return time.Time{}, time.Time{}, "", err
		}
	}
	return m.Created, m.Updated, m.Mode, nil
}

// CreateInitiatedTask mints a fresh id for a clutch-initiated task and persists
// its Class ① identity/policy in the registry. The task anchors NO signature
// yet (it has no git representation); a later scan that finds a correlating
// branch attaches that signature to this id. Returns the new id.
func (s *Store) CreateInitiatedTask(title string, mode model.Mode, base string, created time.Time) (id string, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	id, err = s.newID()
	if err != nil {
		return "", err
	}
	s.registry.Ids[id] = &idMeta{
		Initiated: &initiatedMeta{
			Title:   title,
			Mode:    string(mode),
			Base:    base,
			Created: created,
		},
	}
	if err := s.persistRegistry(); err != nil {
		return "", err
	}
	return id, nil
}

// InitiatedTasks implements correlate.InitiatedTaskReader: the persisted
// clutch-initiated tasks that are still live (retired or merged-away ids are
// omitted, matching the vanished-representation and merge semantics).
func (s *Store) InitiatedTasks() ([]model.InitiatedTask, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []model.InitiatedTask{}
	for id, m := range s.registry.Ids {
		if m == nil || m.Initiated == nil || m.Retired || m.MergedInto != "" {
			continue
		}
		out = append(out, model.InitiatedTask{
			ID:      id,
			Title:   m.Initiated.Title,
			Mode:    model.Mode(m.Initiated.Mode),
			Created: m.Initiated.Created,
		})
	}
	return out, nil
}

// Appraisals implements correlate.AppraisalReader: the cached board.Appraisals
// for taskID (empty slice if none).
func (s *Store) Appraisals(taskID string) ([]model.Appraisal, error) {
	b, err := s.Get(taskID)
	if err != nil {
		return nil, err
	}
	if b.Appraisals == nil {
		return []model.Appraisal{}, nil
	}
	return b.Appraisals, nil
}

// newID allocates a fresh collision-free id. Caller holds mu.
func (s *Store) newID() (string, error) {
	for {
		var buf [8]byte
		if _, err := rand.Read(buf[:]); err != nil {
			return "", err
		}
		id := hex.EncodeToString(buf[:])
		if _, taken := s.registry.Ids[id]; !taken {
			return id, nil
		}
	}
}

// writeJSONAtomic marshals v and writes it to path via a temp file + rename.
func writeJSONAtomic(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("rename %s: %w", path, err)
	}
	return nil
}
