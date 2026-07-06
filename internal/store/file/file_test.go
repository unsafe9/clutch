package file

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/unsafe9/clutch/internal/correlate"
	"github.com/unsafe9/clutch/internal/model"
	"github.com/unsafe9/clutch/internal/store"
)

// *Store must satisfy the appraisal-reader and design-reader seams too.
var (
	_ correlate.AppraisalReader = (*Store)(nil)
	_ correlate.DesignReader    = (*Store)(nil)
)

func TestGetMissingBoardIsEmpty(t *testing.T) {
	s := New(t.TempDir())
	b, err := s.Get("nope")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if b == nil || b.Principles != "" || b.Design != "" || len(b.ADRs) != 0 {
		t.Fatalf("want zero-value board, got %+v", b)
	}
}

func TestBoardSetGetRoundTripPersists(t *testing.T) {
	root := t.TempDir()
	s := New(root)

	if err := s.SetPrinciples("t1", "be surgical"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetDesign("t1", "layered store"); err != nil {
		t.Fatal(err)
	}
	if err := s.AppendDecision("t1", model.Decision{Summary: "use json", Detail: "atomic writes"}); err != nil {
		t.Fatal(err)
	}
	adr := model.ADR{
		Decision:     "atomic rename",
		Context:      "crash safety",
		Alternatives: []string{"in-place write"},
		Consequence:  "no torn files",
	}
	if err := s.AddADR("t1", adr); err != nil {
		t.Fatal(err)
	}

	// Reopen with a fresh Store to prove persistence across process restarts.
	s2 := New(root)
	b, err := s2.Get("t1")
	if err != nil {
		t.Fatal(err)
	}
	if b.Principles != "be surgical" {
		t.Errorf("principles = %q", b.Principles)
	}
	wantDesign := "layered store\n- use json: atomic writes"
	if b.Design != wantDesign {
		t.Errorf("design = %q, want %q", b.Design, wantDesign)
	}
	if len(b.ADRs) != 1 || b.ADRs[0].Decision != "atomic rename" {
		t.Errorf("adrs = %+v", b.ADRs)
	}
}

func TestAppendDecisionIntoEmptyDesign(t *testing.T) {
	s := New(t.TempDir())
	if err := s.AppendDecision("t", model.Decision{Summary: "first"}); err != nil {
		t.Fatal(err)
	}
	b, _ := s.Get("t")
	if b.Design != "- first" {
		t.Fatalf("design = %q", b.Design)
	}
}

func TestQuery(t *testing.T) {
	s := New(t.TempDir())
	_ = s.SetDesign("alpha", "uses a Redis cache layer")
	_ = s.SetPrinciples("beta", "keep it simple")
	_ = s.AddADR("gamma", model.ADR{Decision: "adopt REDIS streams", Context: "events"})

	res, err := s.Query(store.Query{Text: "redis"})
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, id := range res.Tasks {
		got[id] = true
	}
	if !got["alpha"] || !got["gamma"] || got["beta"] {
		t.Fatalf("tasks = %v", res.Tasks)
	}
	if len(res.ADRs) != 1 || res.ADRs[0].Decision != "adopt REDIS streams" {
		t.Fatalf("adrs = %+v", res.ADRs)
	}

	// Scoped query excludes out-of-scope matches.
	scoped, err := s.Query(store.Query{Text: "redis", TaskIDs: []string{"alpha"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(scoped.Tasks) != 1 || scoped.Tasks[0] != "alpha" {
		t.Fatalf("scoped tasks = %v", scoped.Tasks)
	}
}

func TestRegistryResolveMintIdempotent(t *testing.T) {
	root := t.TempDir()
	s := New(root)
	sig := model.Signature{Repo: "r", Branch: "main"}

	if _, ok, _ := s.Resolve(sig); ok {
		t.Fatal("unexpected resolve before mint")
	}
	id, err := s.Mint(sig)
	if err != nil || id == "" {
		t.Fatalf("mint: id=%q err=%v", id, err)
	}
	if again, _ := s.Mint(sig); again != id {
		t.Fatalf("mint not idempotent: %q vs %q", again, id)
	}

	// Persistence: a fresh Store resolves the same sig to the same id.
	s2 := New(root)
	got, ok, err := s2.Resolve(sig)
	if err != nil || !ok || got != id {
		t.Fatalf("resolve after restart: got=%q ok=%v err=%v want=%q", got, ok, err, id)
	}
}

func TestRegistryAttach(t *testing.T) {
	root := t.TempDir()
	s := New(root)
	id, _ := s.Mint(model.Signature{Repo: "r", Branch: "main"})
	alias := model.Signature{IssueLink: "gh#7"}
	if err := s.Attach(id, alias); err != nil {
		t.Fatal(err)
	}

	s2 := New(root)
	got, ok, _ := s2.Resolve(alias)
	if !ok || got != id {
		t.Fatalf("attach resolve: got=%q ok=%v want=%q", got, ok, id)
	}
}

func TestRegistryMerge(t *testing.T) {
	root := t.TempDir()
	s := New(root)
	keep, _ := s.Mint(model.Signature{Repo: "r", Branch: "keep"})
	mergeSig := model.Signature{Repo: "r", Branch: "merge"}
	merge, _ := s.Mint(mergeSig)

	got, err := s.Merge(keep, merge)
	if err != nil || got != keep {
		t.Fatalf("merge: got=%q err=%v want=%q", got, err, keep)
	}

	// mergeID's signatures now resolve to keepID, persisted.
	s2 := New(root)
	resolved, ok, _ := s2.Resolve(mergeSig)
	if !ok || resolved != keep {
		t.Fatalf("merged sig resolve: got=%q ok=%v want=%q", resolved, ok, keep)
	}
	if mi := s2.registry.Ids[merge].MergedInto; mi != keep {
		t.Fatalf("merged_into = %q want %q", mi, keep)
	}
}

func TestRegistryRetireKeepsBoard(t *testing.T) {
	root := t.TempDir()
	s := New(root)
	id, _ := s.Mint(model.Signature{Repo: "r", Branch: "b"})
	if err := s.SetPrinciples(id, "keep me"); err != nil {
		t.Fatal(err)
	}
	if err := s.Retire(id); err != nil {
		t.Fatal(err)
	}

	s2 := New(root)
	if !s2.registry.Ids[id].Retired {
		t.Fatal("id not marked retired")
	}
	b, _ := s2.Get(id)
	if b.Principles != "keep me" {
		t.Fatalf("board knowledge lost: %+v", b)
	}
}

func TestCreateInitiatedTaskPersistsAndLists(t *testing.T) {
	root := t.TempDir()
	s := New(root)
	created := time.Date(2026, 7, 6, 12, 0, 0, 0, time.UTC)

	id, err := s.CreateInitiatedTask("spike the parser", model.ModeSteer, "main", created)
	if err != nil {
		t.Fatalf("CreateInitiatedTask: %v", err)
	}
	// A second create with the same title mints a distinct id.
	other, err := s.CreateInitiatedTask("spike the parser", "", "", created)
	if err != nil {
		t.Fatalf("CreateInitiatedTask (2nd): %v", err)
	}
	if id == other {
		t.Fatalf("two creates reused id %q, want distinct", id)
	}

	// Reopen with a fresh Store to prove the metadata persisted across restarts.
	s2 := New(root)
	its, err := s2.InitiatedTasks()
	if err != nil {
		t.Fatalf("InitiatedTasks: %v", err)
	}
	if len(its) != 2 {
		t.Fatalf("InitiatedTasks = %d, want 2: %+v", len(its), its)
	}
	var got model.InitiatedTask
	found := false
	for _, it := range its {
		if it.ID == id {
			got = it
			found = true
		}
	}
	if !found {
		t.Fatalf("id %q missing from InitiatedTasks: %+v", id, its)
	}
	if got.Title != "spike the parser" || got.Mode != model.ModeSteer || !got.Created.Equal(created) {
		t.Fatalf("initiated task = %+v, want title/steer/created", got)
	}

	// A retired initiated id is omitted from the live list.
	if err := s2.Retire(id); err != nil {
		t.Fatal(err)
	}
	its, err = s2.InitiatedTasks()
	if err != nil {
		t.Fatalf("InitiatedTasks after retire: %v", err)
	}
	if len(its) != 1 || its[0].ID != other {
		t.Fatalf("after retire InitiatedTasks = %+v, want only %q", its, other)
	}
}

func TestAddAppraisalUpsertOrderingAndPersist(t *testing.T) {
	root := t.TempDir()
	s := New(root)

	mk := func(kind, subject, result, fp string) model.Appraisal {
		return model.Appraisal{
			Kind:             model.AppraisalKind(kind),
			Subject:          model.RepRef(subject),
			Result:           result,
			Confidence:       0.8,
			InputFingerprint: fp,
			ComputedAt:       time.Now().UTC(),
		}
	}

	// Append two distinct appraisals (inserted out of sort order).
	if err := s.AddAppraisal("t", mk("relation", "branch:r/main", "depends", "fpA")); err != nil {
		t.Fatal(err)
	}
	if err := s.AddAppraisal("t", mk("classification", "branch:r/feat", "feature", "fpB")); err != nil {
		t.Fatal(err)
	}

	// Upsert: same Kind+Subject as the first append → replace, not append.
	if err := s.AddAppraisal("t", mk("relation", "branch:r/main", "blocks", "fpC")); err != nil {
		t.Fatal(err)
	}

	// Read back via Appraisals on a fresh Store (persistence across restarts).
	s2 := New(root)
	got, err := s2.Appraisals("t")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 appraisals (upsert replaced one), got %d: %+v", len(got), got)
	}
	// Deterministic order: Kind then Subject → classification before relation.
	if got[0].Kind != model.AppraisalKind("classification") || got[1].Kind != model.AppraisalKind("relation") {
		t.Fatalf("ordering = %+v", got)
	}
	// The relation appraisal carries the superseding result + fingerprint.
	if got[1].Result != "blocks" || got[1].InputFingerprint != "fpC" {
		t.Fatalf("upsert did not supersede: %+v", got[1])
	}
}

func TestAppraisalsReadBack(t *testing.T) {
	root := t.TempDir()
	s := New(root)

	if a, err := s.Appraisals("none"); err != nil || len(a) != 0 {
		t.Fatalf("empty appraisals: a=%v err=%v", a, err)
	}

	// Seed a board with appraisals by writing it directly through Get-modify.
	b, _ := s.Get("t")
	b.Appraisals = []model.Appraisal{{
		Kind:             model.AppraisalKind("classification"),
		Subject:          model.RepRef("branch:r/main"),
		Result:           "feature",
		Confidence:       0.9,
		InputFingerprint: "fp1",
		ComputedAt:       time.Now().UTC(),
	}}
	if err := writeJSONAtomic(s.boardPath("t"), b); err != nil {
		t.Fatal(err)
	}

	s2 := New(root)
	got, err := s2.Appraisals("t")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Result != "feature" || got[0].Subject != model.RepRef("branch:r/main") {
		t.Fatalf("appraisals = %+v", got)
	}
}

func TestHasDesign(t *testing.T) {
	s := New(t.TempDir())

	// No board yet → no design.
	if has, err := s.HasDesign("t"); err != nil || has {
		t.Fatalf("HasDesign(empty) = %v, %v; want false, nil", has, err)
	}
	// A whitespace-only design still counts as empty.
	if err := s.SetDesign("t", "  \n\t "); err != nil {
		t.Fatal(err)
	}
	if has, err := s.HasDesign("t"); err != nil || has {
		t.Fatalf("HasDesign(whitespace) = %v, %v; want false, nil", has, err)
	}
	// A real design reads as present.
	if err := s.SetDesign("t", "layered store"); err != nil {
		t.Fatal(err)
	}
	if has, err := s.HasDesign("t"); err != nil || !has {
		t.Fatalf("HasDesign(design) = %v, %v; want true, nil", has, err)
	}
}

func TestIdentityStamp(t *testing.T) {
	root := t.TempDir()
	s := New(root)
	clock := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	s.now = func() time.Time { return clock }

	id, _ := s.Mint(model.Signature{Repo: "r", Branch: "main"})

	// First sight: created and updated both stamped to now; mode empty (no
	// explicit policy write).
	c1, u1, m1, err := s.Identity(id, "fpA")
	if err != nil {
		t.Fatal(err)
	}
	if !c1.Equal(clock) || !u1.Equal(clock) {
		t.Fatalf("first stamp created/updated = %v/%v, want %v", c1, u1, clock)
	}
	if m1 != "" {
		t.Fatalf("mode = %q, want empty", m1)
	}

	// Same fingerprint at a later clock: updated does NOT advance, created stable.
	clock = clock.Add(time.Hour)
	c2, u2, _, _ := s.Identity(id, "fpA")
	if !c2.Equal(c1) || !u2.Equal(u1) {
		t.Fatalf("stable-fingerprint stamp drifted: created=%v updated=%v", c2, u2)
	}

	// Changed fingerprint: updated advances to now, created stays.
	c3, u3, _, _ := s.Identity(id, "fpB")
	if !c3.Equal(c1) {
		t.Fatalf("created changed on fingerprint change: %v", c3)
	}
	if !u3.Equal(clock) {
		t.Fatalf("updated = %v, want advanced to %v", u3, clock)
	}

	// Persistence: a fresh Store reuses the stamped timestamps for an unchanged
	// fingerprint.
	s2 := New(root)
	c4, u4, _, _ := s2.Identity(id, "fpB")
	if !c4.Equal(c1) || !u4.Equal(u3) {
		t.Fatalf("identity not persisted: created=%v updated=%v", c4, u4)
	}
}

func TestTaskIDTraversalRejected(t *testing.T) {
	root := t.TempDir()
	s := New(root)

	// Seed a registry so we can prove a traversal write does not clobber it.
	if _, err := s.Mint(model.Signature{Repo: "r", Branch: "main"}); err != nil {
		t.Fatalf("Mint: %v", err)
	}
	before, err := os.ReadFile(s.registryPath())
	if err != nil {
		t.Fatalf("read registry: %v", err)
	}

	for _, id := range []string{"../registry", "../../../tmp/evil", "a/b", "..", "."} {
		if err := s.SetDesign(id, "x"); err == nil {
			t.Fatalf("SetDesign(%q) = nil, want error", id)
		}
		if _, err := s.Get(id); err == nil {
			t.Fatalf("Get(%q) = nil error, want error", id)
		}
	}

	after, err := os.ReadFile(s.registryPath())
	if err != nil {
		t.Fatalf("read registry after: %v", err)
	}
	if string(before) != string(after) {
		t.Fatalf("registry.json was modified by a traversal write")
	}
	// No stray file should have escaped the boards dir to <root>/registry.json's sibling.
	if _, err := os.Stat(filepath.Join(root, "evil.json")); !os.IsNotExist(err) {
		t.Fatalf("traversal write escaped boards dir")
	}
}
