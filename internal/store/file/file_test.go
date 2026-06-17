package file

import (
	"testing"
	"time"

	"github.com/unsafe9/clutch/internal/correlate"
	"github.com/unsafe9/clutch/internal/model"
	"github.com/unsafe9/clutch/internal/store"
)

// *Store must satisfy the appraisal-reader seam too.
var _ correlate.AppraisalReader = (*Store)(nil)

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
