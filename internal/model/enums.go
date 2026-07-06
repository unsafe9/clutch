package model

// Enums are string-typed so their JSON wire form is exactly the listed value.
// Keep these values stable: they are part of the machine (--json) contract.

// Lifecycle is the task lifecycle state.
type Lifecycle string

// Lifecycle values.
const (
	LifecycleIdea       Lifecycle = "idea"
	LifecyclePlanned    Lifecycle = "planned"
	LifecycleActive     Lifecycle = "active"
	LifecycleReview     Lifecycle = "review"
	LifecycleMerged     Lifecycle = "merged"
	LifecycleDone       Lifecycle = "done"
	LifecycleStale      Lifecycle = "stale"
	LifecycleSuperseded Lifecycle = "superseded"
)

// Mode is the task execution mode. The project sets a default; a task may
// override it.
type Mode string

// Mode values.
const (
	ModeCruise Mode = "cruise"
	ModeSteer  Mode = "steer"
)

// Provenance records how a task came to exist.
type Provenance string

// Provenance values.
const (
	ProvenanceClutchInitiated Provenance = "clutch-initiated"
	ProvenanceGitDetected     Provenance = "git-detected"
)

// Integration is the task's integration state relative to its base.
type Integration string

// Integration values.
const (
	IntegrationUnmerged  Integration = "unmerged"
	IntegrationMerged    Integration = "merged"
	IntegrationConflicts Integration = "conflicts"
	IntegrationBehind    Integration = "behind"
)

// LinkMethod records how a representation link was established.
type LinkMethod string

// LinkMethod values.
const (
	LinkConvention LinkMethod = "convention"
	LinkAppraisal  LinkMethod = "appraisal"
	LinkDeclared   LinkMethod = "declared"
)

// UnresolvedKind categorizes an Unresolved ambiguity flag. The set is
// extensible — consumers MUST tolerate kinds they do not recognize.
type UnresolvedKind string

// UnresolvedKind values.
const (
	UnresolvedLineage        UnresolvedKind = "lineage"
	UnresolvedRelation       UnresolvedKind = "relation"
	UnresolvedLink           UnresolvedKind = "link"
	UnresolvedIdentity       UnresolvedKind = "identity"
	UnresolvedSession        UnresolvedKind = "session"
	UnresolvedClassification UnresolvedKind = "classification"
)

// AppraisalKind categorizes a cached Appraisal result. The set is extensible —
// consumers MUST tolerate kinds they do not recognize.
type AppraisalKind string

// AppraisalKind values.
const (
	AppraisalClassification AppraisalKind = "classification"
	AppraisalRelation       AppraisalKind = "relation"
	AppraisalLink           AppraisalKind = "link"
)
