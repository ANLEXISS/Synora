package hypotheses

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"time"

	"synora/internal/cge/chains"
)

type SetID string

type Family string

const (
	FamilyAssociation Family = "association"
	FamilyEvidence    Family = "evidence"
)

type Status string

const (
	StatusOpen        Status = "open"
	StatusUnderReview Status = "under_review"
	StatusResolved    Status = "resolved"
	StatusInvalidated Status = "invalidated"
	StatusSuperseded  Status = "superseded"
)

type AlternativeKind string

const (
	AlternativeAttachExisting  AlternativeKind = "attach_existing"
	AlternativeCreateCandidate AlternativeKind = "create_candidate"
	AlternativeSupport         AlternativeKind = "support"
	AlternativeContradiction   AlternativeKind = "contradiction"
	AlternativeNeutral         AlternativeKind = "neutral"
	AlternativeInsufficient    AlternativeKind = "insufficient"
)

type Subject struct {
	ObservationID       string
	ChainID             chains.ChainID
	EvidenceFingerprint string
}

type Lineage struct {
	RootSetID        SetID
	PredecessorSetID SetID
	SuccessorSetID   SetID
	Generation       uint64
}

type FactReference struct {
	Code           string
	Side           string
	Score          int64
	ObservationIDs []string
}

type Provenance struct {
	Source               string
	PolicyNamespace      string
	PolicyVersion        string
	PlannedOrEvaluatedAt time.Time
	SourceRevision       uint64
}

func (p Provenance) Validate() error                 { return p.validate(FamilyAssociation) }
func (p Provenance) ValidateFor(family Family) error { return p.validate(family) }

type Alternative struct {
	ID             string
	Kind           AlternativeKind
	ChainID        chains.ChainID
	SourceRevision uint64

	Score int64
	Rank  int

	ReasonCode string
	Facts      []FactReference

	ContributionID      string
	EvidenceFingerprint string
	ResolutionEffect    *ResolutionEffect
}

type AlternativeSnapshot = Alternative

type RevisionOperation string

const (
	OperationHypothesisOpened        RevisionOperation = "hypothesis.opened"
	OperationHypothesisStatusChanged RevisionOperation = "hypothesis.status_changed"
	OperationHypothesisRebased       RevisionOperation = "hypothesis.rebased"
	OperationHypothesisSuperseded    RevisionOperation = "hypothesis.superseded"
	OperationHypothesisResolved      RevisionOperation = "hypothesis.resolved"
)

type RevisionRecord struct {
	SetID            SetID
	Operation        RevisionOperation
	PreviousRevision uint64
	NewRevision      uint64

	At            time.Time
	Actor         string
	Reason        string
	CorrelationID string

	PreviousStatus Status
	NewStatus      Status

	PreviousAssessmentVersion     uint64
	NewAssessmentVersion          uint64
	PreviousAssessmentID          string
	NewAssessmentID               string
	PreviousAssessmentFingerprint string
	NewAssessmentFingerprint      string
	PreviousSuccessorSetID        SetID
	NewSuccessorSetID             SetID
	SuccessorGeneration           uint64
	SelectedAssessmentVersion     uint64
	SelectedAssessmentID          string
	SelectedAssessmentFingerprint string
	SelectedAlternativeID         string
	SelectedAlternativeKind       AlternativeKind
	SelectedEffectKind            ResolutionEffectKind
	SelectedEffectFingerprint     string
}

type Snapshot struct {
	ID     SetID
	Family Family
	Status Status

	Subject                  Subject
	Alternatives             []AlternativeSnapshot
	Provenance               Provenance
	CurrentAssessmentVersion uint64
	Assessments              []AssessmentVersionSnapshot
	Lineage                  Lineage
	ReasonCode               string
	Reason                   string

	CreatedAt time.Time
	UpdatedAt time.Time

	Revision   uint64
	History    []RevisionRecord
	Resolution *ResolutionSnapshot
}

type HypothesisSet struct {
	id                       SetID
	family                   Family
	status                   Status
	subject                  Subject
	alternatives             []Alternative
	provenance               Provenance
	reasonCode               string
	reason                   string
	createdAt                time.Time
	updatedAt                time.Time
	revision                 uint64
	history                  []RevisionRecord
	currentAssessmentVersion uint64
	assessments              []AssessmentVersion
	lineage                  Lineage
	resolution               *ResolutionSnapshot
}

func (f Family) Validate() error {
	if f != FamilyAssociation && f != FamilyEvidence {
		return fmt.Errorf("invalid hypothesis family %q", f)
	}
	return nil
}

func (s Status) Validate() error {
	switch s {
	case StatusOpen, StatusUnderReview, StatusResolved, StatusInvalidated, StatusSuperseded:
		return nil
	default:
		return fmt.Errorf("invalid hypothesis status %q", s)
	}
}

func (l Lineage) Validate(setID SetID) error {
	if err := validSetID(l.RootSetID); err != nil || l.Generation == 0 {
		if err == nil {
			err = fmt.Errorf("lineage root or generation is invalid")
		}
		return err
	}
	if l.PredecessorSetID == setID || l.SuccessorSetID == setID {
		return fmt.Errorf("lineage cannot point to itself")
	}
	if l.PredecessorSetID != "" {
		if err := validSetID(l.PredecessorSetID); err != nil {
			return err
		}
		if l.Generation < 2 {
			return fmt.Errorf("successor lineage generation must be greater than one")
		}
	} else if l.RootSetID != setID || l.Generation != 1 {
		return fmt.Errorf("root lineage is inconsistent")
	}
	if l.SuccessorSetID != "" {
		if err := validSetID(l.SuccessorSetID); err != nil {
			return err
		}
	}
	return nil
}

func CanTransition(from, to Status) bool {
	switch from {
	case StatusOpen:
		return to == StatusUnderReview || to == StatusInvalidated || to == StatusSuperseded
	case StatusUnderReview:
		return to == StatusOpen || to == StatusInvalidated || to == StatusSuperseded
	default:
		return false
	}
}

func CanRebase(status Status) bool { return status == StatusOpen || status == StatusUnderReview }

func CanSupersede(status Status) bool { return status == StatusOpen || status == StatusUnderReview }

func ValidateTransition(from, to Status) error {
	if err := from.Validate(); err != nil {
		return fmt.Errorf("%w: from: %v", ErrInvalidHypothesisTransition, err)
	}
	if err := to.Validate(); err != nil {
		return fmt.Errorf("%w: to: %v", ErrInvalidHypothesisTransition, err)
	}
	if from == to || !CanTransition(from, to) {
		return fmt.Errorf("%w: %s -> %s", ErrInvalidHypothesisTransition, from, to)
	}
	return nil
}

func (h *HypothesisSet) ID() SetID {
	if h == nil {
		return ""
	}
	return h.id
}
func (h *HypothesisSet) Family() Family {
	if h == nil {
		return ""
	}
	return h.family
}
func (h *HypothesisSet) Status() Status {
	if h == nil {
		return ""
	}
	return h.status
}
func (h *HypothesisSet) Revision() uint64 {
	if h == nil {
		return 0
	}
	return h.revision
}

func DeriveAssociationSetID(observationID, policyVersion string) (SetID, error) {
	if err := validText(observationID, "observation id", true, 256); err != nil {
		return "", hypothesisError(ErrInvalidHypothesisSubject, FamilyAssociation, "", "derive_id", 0, 0, err)
	}
	if err := validText(policyVersion, "policy version", true, 128); err != nil {
		return "", hypothesisError(ErrInvalidHypothesisProvenance, FamilyAssociation, "", "derive_id", 0, 0, err)
	}
	_ = policyVersion // retained in the API for compatibility; it is assessment provenance
	// The dossier subject is the observation itself. Policy versions are
	// assessment provenance and must not create a second dossier for the same
	// ambiguity when a re-evaluation uses a new policy.
	return deriveSetID("association", observationID), nil
}

func DeriveEvidenceSetID(chainID chains.ChainID, observationID, fingerprint string) (SetID, error) {
	if _, err := chains.NewChainID(string(chainID)); err != nil {
		return "", hypothesisError(ErrInvalidHypothesisSubject, FamilyEvidence, "", "derive_id", 0, 0, err)
	}
	if err := validText(observationID, "observation id", true, 256); err != nil {
		return "", hypothesisError(ErrInvalidHypothesisSubject, FamilyEvidence, "", "derive_id", 0, 0, err)
	}
	if err := validText(fingerprint, "evidence fingerprint", true, 256); err != nil {
		return "", hypothesisError(ErrInvalidHypothesisSubject, FamilyEvidence, "", "derive_id", 0, 0, err)
	}
	return deriveSetID("evidence", string(chainID), observationID, fingerprint), nil
}

func deriveSetID(parts ...string) SetID {
	material := strings.Join(append([]string{"synora.cge.hypotheses"}, parts...), "\x00")
	digest := sha256.Sum256([]byte(material))
	return SetID("cge-hyp-" + hex.EncodeToString(digest[:]))
}

func (h *HypothesisSet) Snapshot() Snapshot {
	if h == nil {
		return Snapshot{}
	}
	alternatives := cloneAlternatives(h.alternatives)
	sort.SliceStable(alternatives, func(i, j int) bool { return alternatives[i].Rank < alternatives[j].Rank })
	return Snapshot{ID: h.id, Family: h.family, Status: h.status, Subject: cloneSubject(h.subject), Alternatives: alternatives, Provenance: h.provenance, CurrentAssessmentVersion: h.currentAssessmentVersion, Assessments: cloneAssessments(h.assessments), Lineage: h.lineage, ReasonCode: h.reasonCode, Reason: h.reason, CreatedAt: h.createdAt, UpdatedAt: h.updatedAt, Revision: h.revision, History: cloneHistory(h.history), Resolution: h.resolution.Clone()}
}

func (s Snapshot) Validate() error {
	_, err := Restore(s)
	return err
}

// Validate verifies the complete hypothesis aggregate, including its
// deterministic identity, alternatives, and local revision history.
func (h *HypothesisSet) Validate() error {
	if h == nil {
		return hypothesisError(ErrInvalidHypothesis, "", "", "validate", 0, 0, fmt.Errorf("hypothesis is nil"))
	}
	if err := h.family.Validate(); err != nil {
		return hypothesisError(ErrInvalidHypothesis, h.family, h.id, "validate", 0, 0, err)
	}
	if err := validSetID(h.id); err != nil {
		return hypothesisError(ErrInvalidHypothesis, h.family, h.id, "validate", 0, 0, err)
	}
	if err := h.subject.validate(h.family); err != nil {
		return hypothesisError(ErrInvalidHypothesisSubject, h.family, h.id, "validate", 0, 0, err)
	}
	if err := h.lineage.Validate(h.id); err != nil {
		return hypothesisError(ErrInvalidHypothesis, h.family, h.id, "validate", 0, 0, err)
	}
	if err := h.provenance.validate(h.family); err != nil {
		return hypothesisError(ErrInvalidHypothesisProvenance, h.family, h.id, "validate", 0, 0, err)
	}
	expectedID, err := h.expectedSetID()
	legacyID := SetID("")
	if h.family == FamilyAssociation {
		policyVersion := h.provenance.PolicyVersion
		if len(h.assessments) > 0 {
			policyVersion = h.assessments[0].Provenance.PolicyVersion
		}
		legacyID = deriveSetID("association", h.subject.ObservationID, policyVersion)
	}
	if err != nil || (expectedID != h.id && legacyID != h.id) {
		if err == nil {
			err = fmt.Errorf("set id does not match deterministic derivation")
		}
		return hypothesisError(ErrInvalidHypothesis, h.family, h.id, "validate", 0, 0, err)
	}
	if err := h.status.Validate(); err != nil {
		return hypothesisError(ErrInvalidHypothesis, h.family, h.id, "validate", 0, 0, err)
	}
	if (h.status == StatusResolved) != (h.resolution != nil) {
		return hypothesisError(ErrInvalidHypothesis, h.family, h.id, "validate", 0, 0, fmt.Errorf("resolved status and resolution snapshot disagree"))
	}
	if err := validText(h.reasonCode, "hypothesis reason code", true, 64); err != nil {
		return hypothesisError(ErrInvalidHypothesis, h.family, h.id, "validate", 0, 0, err)
	}
	if err := validText(h.reason, "hypothesis reason", true, 256); err != nil {
		return hypothesisError(ErrInvalidHypothesis, h.family, h.id, "validate", 0, 0, err)
	}
	if h.createdAt.IsZero() || h.updatedAt.IsZero() || h.updatedAt.Before(h.createdAt) {
		return hypothesisError(ErrInvalidHypothesis, h.family, h.id, "validate", 0, 0, fmt.Errorf("timestamps are invalid"))
	}
	if len(h.alternatives) < 2 {
		return hypothesisError(ErrInsufficientHypothesisAlternatives, h.family, h.id, "validate", 0, 0, nil)
	}
	seenIDs := make(map[string]struct{}, len(h.alternatives))
	seenRanks := make(map[int]struct{}, len(h.alternatives))
	for index, alternative := range h.alternatives {
		if alternative.Rank != index+1 {
			return hypothesisError(ErrInvalidHypothesisAlternative, h.family, h.id, "validate", 0, 0, fmt.Errorf("alternative order and ranks are inconsistent"))
		}
		if _, ok := seenIDs[alternative.ID]; ok {
			return hypothesisError(ErrInvalidHypothesisAlternative, h.family, h.id, "validate", 0, 0, fmt.Errorf("duplicate alternative id"))
		}
		if _, ok := seenRanks[alternative.Rank]; ok {
			return hypothesisError(ErrInvalidHypothesisAlternative, h.family, h.id, "validate", 0, 0, fmt.Errorf("duplicate alternative rank"))
		}
		seenIDs[alternative.ID] = struct{}{}
		seenRanks[alternative.Rank] = struct{}{}
		if err := alternative.validate(h.family, h.subject, h.id); err != nil {
			return hypothesisError(ErrInvalidHypothesisAlternative, h.family, h.id, "validate", 0, 0, err)
		}
	}
	for rank := 1; rank <= len(h.alternatives); rank++ {
		if _, ok := seenRanks[rank]; !ok {
			return hypothesisError(ErrInvalidHypothesisAlternative, h.family, h.id, "validate", 0, 0, fmt.Errorf("alternative ranks are not contiguous"))
		}
	}
	if len(h.assessments) == 0 || h.currentAssessmentVersion == 0 || h.currentAssessmentVersion != uint64(len(h.assessments)) {
		return hypothesisError(ErrInvalidHypothesis, h.family, h.id, "validate", 0, 0, fmt.Errorf("assessment versions are inconsistent"))
	}
	for index, assessment := range h.assessments {
		if assessment.Version != uint64(index+1) {
			return hypothesisError(ErrInvalidHypothesisAssessment, h.family, h.id, "validate", 0, 0, fmt.Errorf("assessment versions are not continuous"))
		}
		if err := assessment.validate(h.family, h.subject, h.id); err != nil {
			return hypothesisError(ErrInvalidHypothesisAssessment, h.family, h.id, "validate", 0, 0, err)
		}
		if index > 0 && assessment.CreatedAt.Before(h.assessments[index-1].CreatedAt) {
			return hypothesisError(ErrInvalidHypothesisAssessment, h.family, h.id, "validate", 0, 0, fmt.Errorf("assessment timestamps are not monotone"))
		}
	}
	currentAssessment := h.assessments[len(h.assessments)-1]
	if !reflect.DeepEqual(currentAssessment.Alternatives, h.alternatives) || currentAssessment.Provenance != h.provenance {
		return hypothesisError(ErrInvalidHypothesisAssessment, h.family, h.id, "validate", 0, 0, fmt.Errorf("current assessment does not match view"))
	}
	resolvedRecords := 0
	if h.resolution != nil {
		if err := validateResolutionSnapshot(*h.resolution, h.family, h.id, h.currentAssessmentVersion, currentAssessment, h.alternatives); err != nil {
			return err
		}
	}
	if h.revision == 0 || len(h.history) != int(h.revision) {
		return hypothesisError(ErrInvalidHypothesis, h.family, h.id, "validate", 0, 0, fmt.Errorf("revision and history length are inconsistent"))
	}
	for i, record := range h.history {
		if err := record.validate(h.family, h.id); err != nil {
			return hypothesisError(ErrInvalidHypothesis, h.family, h.id, "validate", 0, 0, err)
		}
		if record.NewRevision != uint64(i+1) || (i > 0 && record.PreviousRevision != uint64(i)) {
			return hypothesisError(ErrInvalidHypothesis, h.family, h.id, "validate", 0, 0, fmt.Errorf("history revisions are not continuous"))
		}
		if i > 0 && record.At.Before(h.history[i-1].At) {
			return hypothesisError(ErrInvalidHypothesis, h.family, h.id, "validate", 0, 0, fmt.Errorf("history timestamps are not monotone"))
		}
		if record.Operation == OperationHypothesisRebased {
			if record.PreviousAssessmentVersion == 0 || record.NewAssessmentVersion != record.PreviousAssessmentVersion+1 || record.PreviousAssessmentID == "" || record.NewAssessmentID == "" || !validFingerprint(record.PreviousAssessmentFingerprint) || !validFingerprint(record.NewAssessmentFingerprint) || record.PreviousStatus != record.NewStatus {
				return hypothesisError(ErrInvalidHypothesisAssessment, h.family, h.id, "validate", 0, 0, fmt.Errorf("rebase history record is invalid"))
			}
		}
		if record.Operation == OperationHypothesisResolved {
			resolvedRecords++
		}
	}
	if (h.status == StatusResolved && resolvedRecords != 1) || (h.status != StatusResolved && resolvedRecords != 0) {
		return hypothesisError(ErrInvalidHypothesis, h.family, h.id, "validate", 0, 0, fmt.Errorf("resolution history is inconsistent"))
	}
	if h.resolution != nil {
		record := h.history[len(h.history)-1]
		if record.Operation != OperationHypothesisResolved || record.SelectedAssessmentVersion != h.resolution.AssessmentVersion || record.SelectedAssessmentID != h.resolution.AssessmentID || record.SelectedAssessmentFingerprint != h.resolution.AssessmentFingerprint || record.SelectedAlternativeID != h.resolution.AlternativeID || record.SelectedAlternativeKind != h.resolution.AlternativeKind || record.SelectedEffectKind != h.resolution.EffectKind || record.SelectedEffectFingerprint != h.resolution.EffectFingerprint {
			return hypothesisError(ErrInvalidHypothesis, h.family, h.id, "validate", 0, 0, fmt.Errorf("resolution history does not match snapshot"))
		}
		if !h.resolution.ResolvedAt.Equal(record.At) || h.resolution.Actor != record.Actor || h.resolution.Reason != record.Reason || h.resolution.CorrelationID != record.CorrelationID {
			return hypothesisError(ErrInvalidHypothesis, h.family, h.id, "validate", 0, 0, fmt.Errorf("resolution provenance does not match history"))
		}
	}
	first := h.history[0]
	if first.Operation != OperationHypothesisOpened || first.PreviousRevision != 0 || first.NewRevision != 1 || first.PreviousStatus != "" || first.NewStatus != StatusOpen || first.At.Before(h.createdAt) {
		return hypothesisError(ErrInvalidHypothesis, h.family, h.id, "validate", 0, 0, fmt.Errorf("invalid opening revision"))
	}
	if h.history[len(h.history)-1].NewStatus != h.status || !h.updatedAt.Equal(h.history[len(h.history)-1].At) {
		return hypothesisError(ErrInvalidHypothesis, h.family, h.id, "validate", 0, 0, fmt.Errorf("current status or updated timestamp disagrees with history"))
	}
	rebases := 0
	supersessions := 0
	for _, record := range h.history {
		if record.Operation == OperationHypothesisRebased {
			rebases++
			if record.PreviousAssessmentVersion > uint64(len(h.assessments)) || record.NewAssessmentVersion > uint64(len(h.assessments)) {
				return hypothesisError(ErrInvalidHypothesisAssessment, h.family, h.id, "validate", 0, 0, fmt.Errorf("rebase assessment version is out of range"))
			}
			previous := h.assessments[record.PreviousAssessmentVersion-1]
			current := h.assessments[record.NewAssessmentVersion-1]
			if previous.ID != record.PreviousAssessmentID || current.ID != record.NewAssessmentID || previous.Fingerprint != record.PreviousAssessmentFingerprint || current.Fingerprint != record.NewAssessmentFingerprint {
				return hypothesisError(ErrInvalidHypothesisAssessment, h.family, h.id, "validate", 0, 0, fmt.Errorf("rebase history does not match assessment versions"))
			}
		}
		if record.Operation == OperationHypothesisSuperseded {
			supersessions++
			if record.NewSuccessorSetID != h.lineage.SuccessorSetID || record.SuccessorGeneration != h.lineage.Generation+1 || record.PreviousSuccessorSetID != "" {
				return hypothesisError(ErrInvalidHypothesis, h.family, h.id, "validate", 0, 0, fmt.Errorf("supersession history does not match lineage"))
			}
		}
	}
	if len(h.assessments) != rebases+1 {
		return hypothesisError(ErrInvalidHypothesisAssessment, h.family, h.id, "validate", 0, 0, fmt.Errorf("assessment history count is inconsistent"))
	}
	if h.lineage.PredecessorSetID != "" && h.family != FamilyEvidence {
		return hypothesisError(ErrInvalidHypothesis, h.family, h.id, "validate", 0, 0, fmt.Errorf("only evidence hypotheses may be superseded"))
	}
	if h.status == StatusSuperseded {
		if h.lineage.SuccessorSetID == "" || supersessions != 1 {
			return hypothesisError(ErrInvalidHypothesis, h.family, h.id, "validate", 0, 0, fmt.Errorf("superseded lineage is incomplete"))
		}
	} else if h.lineage.SuccessorSetID != "" || supersessions != 0 {
		return hypothesisError(ErrInvalidHypothesis, h.family, h.id, "validate", 0, 0, fmt.Errorf("non-terminal lineage has a successor"))
	}
	return nil
}

func (h *HypothesisSet) expectedSetID() (SetID, error) {
	switch h.family {
	case FamilyAssociation:
		return DeriveAssociationSetID(h.subject.ObservationID, h.provenance.PolicyVersion)
	case FamilyEvidence:
		return DeriveEvidenceSetID(h.subject.ChainID, h.subject.ObservationID, h.subject.EvidenceFingerprint)
	default:
		return "", fmt.Errorf("invalid hypothesis family")
	}
}

// Clone returns an independently owned aggregate without adding a revision.
func (h *HypothesisSet) Clone() (*HypothesisSet, error) {
	if h == nil {
		return nil, hypothesisError(ErrInvalidHypothesis, "", "", "clone", 0, 0, fmt.Errorf("hypothesis is nil"))
	}
	return Restore(h.Snapshot())
}

// Restore reconstructs an owned aggregate from a defensive snapshot.
func Restore(snapshot Snapshot) (*HypothesisSet, error) {
	h := &HypothesisSet{
		id: snapshot.ID, family: snapshot.Family, status: snapshot.Status,
		subject: cloneSubject(snapshot.Subject), alternatives: cloneAlternatives(snapshot.Alternatives),
		provenance: snapshot.Provenance, reasonCode: snapshot.ReasonCode, reason: snapshot.Reason,
		createdAt: snapshot.CreatedAt, updatedAt: snapshot.UpdatedAt, revision: snapshot.Revision,
		history:    cloneHistory(snapshot.History),
		lineage:    snapshot.Lineage,
		resolution: snapshot.Resolution.Clone(),
	}
	if snapshot.Lineage.RootSetID == "" {
		h.lineage = Lineage{RootSetID: snapshot.ID, Generation: 1}
	}
	if len(snapshot.Assessments) == 0 {
		if snapshot.CurrentAssessmentVersion != 0 {
			return nil, hypothesisError(ErrInvalidHypothesisAssessment, snapshot.Family, snapshot.ID, "restore", snapshot.Revision, snapshot.Revision, fmt.Errorf("assessment version marker has no assessments"))
		}
		fingerprint, err := DeriveAssessmentFingerprint(snapshot.Family, snapshot.Subject, snapshot.Alternatives, snapshot.Provenance)
		if err != nil {
			return nil, hypothesisError(ErrInvalidHypothesisAssessment, snapshot.Family, snapshot.ID, "restore", snapshot.Revision, snapshot.Revision, err)
		}
		id, err := DeriveAssessmentID(snapshot.ID, 1, fingerprint)
		if err != nil {
			return nil, hypothesisError(ErrInvalidHypothesisAssessment, snapshot.Family, snapshot.ID, "restore", snapshot.Revision, snapshot.Revision, err)
		}
		h.assessments = []AssessmentVersion{{Version: 1, ID: id, Fingerprint: fingerprint, Alternatives: cloneAlternatives(snapshot.Alternatives), Provenance: snapshot.Provenance, CreatedAt: snapshot.Provenance.PlannedOrEvaluatedAt, ResolutionSchemaVersion: ResolutionSchemaLegacy}}
		h.currentAssessmentVersion = 1
	} else {
		h.assessments = cloneAssessments(snapshot.Assessments)
		h.currentAssessmentVersion = snapshot.CurrentAssessmentVersion
	}
	if err := h.Validate(); err != nil {
		return nil, err
	}
	return h, nil
}

// SetStatus applies one of the explicitly supported hypothesis transitions.
func (h *HypothesisSet) SetStatus(target Status, mutation chains.MutationContext) error {
	if h == nil {
		return hypothesisError(ErrInvalidHypothesis, "", "", "status", 0, 0, fmt.Errorf("hypothesis is nil"))
	}
	if target == StatusSuperseded {
		return hypothesisError(ErrSupersessionNotAllowed, h.family, h.id, "status", h.revision, h.revision, nil)
	}
	if target == StatusResolved {
		return hypothesisError(ErrResolutionNotAllowed, h.family, h.id, "status", h.revision, h.revision, nil)
	}
	if err := ValidateTransition(h.status, target); err != nil {
		return err
	}
	if err := mutation.Validate(); err != nil {
		return hypothesisError(ErrInvalidContext, h.family, h.id, "status", h.revision, h.revision, err)
	}
	if mutation.At.Before(h.updatedAt) {
		return hypothesisError(ErrInvalidContext, h.family, h.id, "status", h.revision, h.revision, fmt.Errorf("mutation timestamp is older than updated timestamp"))
	}
	candidate := *h
	candidate.history = cloneHistory(h.history)
	candidate.alternatives = cloneAlternatives(h.alternatives)
	candidate.status = target
	candidate.updatedAt = mutation.At
	candidate.revision++
	candidate.history = append(candidate.history, RevisionRecord{SetID: h.id, Operation: OperationHypothesisStatusChanged, PreviousRevision: h.revision, NewRevision: candidate.revision, At: mutation.At, Actor: mutation.Actor, Reason: mutation.Reason, CorrelationID: mutation.CorrelationID, PreviousStatus: h.status, NewStatus: target})
	if err := candidate.Validate(); err != nil {
		return err
	}
	*h = candidate
	return nil
}

// MarkResolved is the only operation allowed to enter the terminal resolved
// state. The effect is applied by the durable coordinator, not here.
func (h *HypothesisSet) MarkResolved(command ResolveCommand, outcome ResolutionOutcome) error {
	if h == nil {
		return ErrInvalidHypothesis
	}
	command = command.Clone()
	if err := command.Validate(); err != nil {
		return err
	}
	if h.id != command.SetID || h.revision != command.SourceRevision || !CanResolve(h.status) || h.lineage.SuccessorSetID != "" {
		return hypothesisError(ErrStaleHypothesisResolution, h.family, h.id, "resolve", command.SourceRevision, h.revision, nil)
	}
	if command.Mutation.At.Before(h.updatedAt) {
		return hypothesisError(ErrInvalidContext, h.family, h.id, "resolve", command.SourceRevision, h.revision, fmt.Errorf("mutation timestamp is older than updated timestamp"))
	}
	if len(h.assessments) == 0 || h.currentAssessmentVersion != command.AssessmentVersion {
		return hypothesisError(ErrStaleHypothesisResolution, h.family, h.id, "resolve_assessment", command.SourceRevision, h.revision, nil)
	}
	assessment := h.assessments[len(h.assessments)-1]
	if assessment.ResolutionSchemaVersion != ResolutionSchemaV1 {
		return hypothesisError(ErrResolutionMaterialMissing, h.family, h.id, "resolve_assessment", command.SourceRevision, h.revision, nil)
	}
	if assessment.Version != command.AssessmentVersion || assessment.ID != command.AssessmentID || assessment.Fingerprint != command.AssessmentFingerprint {
		return hypothesisError(ErrStaleHypothesisResolution, h.family, h.id, "resolve_assessment", command.SourceRevision, h.revision, nil)
	}
	var selected *Alternative
	for i := range assessment.Alternatives {
		if assessment.Alternatives[i].ID == command.AlternativeID {
			copy := assessment.Alternatives[i]
			selected = &copy
			break
		}
	}
	if selected == nil || selected.Kind != command.AlternativeKind {
		return hypothesisError(ErrResolutionAlternativeMismatch, h.family, h.id, "resolve_alternative", command.SourceRevision, h.revision, nil)
	}
	if selected.ResolutionEffect == nil || !reflect.DeepEqual(*selected.ResolutionEffect, command.Effect) {
		return hypothesisError(ErrResolutionEffectMismatch, h.family, h.id, "resolve_effect", command.SourceRevision, h.revision, nil)
	}
	fingerprint, err := command.Effect.Fingerprint()
	if err != nil {
		return hypothesisError(ErrResolutionEffectMismatch, h.family, h.id, "resolve_effect", command.SourceRevision, h.revision, err)
	}
	if err := outcome.validateFor(command.Effect); err != nil {
		return hypothesisError(ErrResolutionOutcomeMismatch, h.family, h.id, "resolve_outcome", command.SourceRevision, h.revision, err)
	}
	candidate := *h
	candidate.alternatives = cloneAlternatives(h.alternatives)
	candidate.assessments = cloneAssessments(h.assessments)
	candidate.history = cloneHistory(h.history)
	candidate.resolution = &ResolutionSnapshot{AssessmentVersion: command.AssessmentVersion, AssessmentID: command.AssessmentID, AssessmentFingerprint: command.AssessmentFingerprint, AlternativeID: command.AlternativeID, AlternativeKind: command.AlternativeKind, EffectKind: command.Effect.Kind, EffectFingerprint: fingerprint, Outcome: outcome.Clone(), ResolvedAt: command.Mutation.At, Actor: command.Mutation.Actor, Reason: command.Mutation.Reason, CorrelationID: command.Mutation.CorrelationID}
	candidate.status = StatusResolved
	candidate.updatedAt = command.Mutation.At
	candidate.revision++
	candidate.history = append(candidate.history, RevisionRecord{SetID: h.id, Operation: OperationHypothesisResolved, PreviousRevision: h.revision, NewRevision: candidate.revision, At: command.Mutation.At, Actor: command.Mutation.Actor, Reason: command.Mutation.Reason, CorrelationID: command.Mutation.CorrelationID, PreviousStatus: h.status, NewStatus: StatusResolved, SelectedAssessmentVersion: command.AssessmentVersion, SelectedAssessmentID: command.AssessmentID, SelectedAssessmentFingerprint: command.AssessmentFingerprint, SelectedAlternativeID: command.AlternativeID, SelectedAlternativeKind: command.AlternativeKind, SelectedEffectKind: command.Effect.Kind, SelectedEffectFingerprint: fingerprint})
	if err := candidate.Validate(); err != nil {
		return err
	}
	*h = candidate
	return nil
}

func validateResolutionSnapshot(resolution ResolutionSnapshot, family Family, setID SetID, currentVersion uint64, assessment AssessmentVersion, alternatives []Alternative) error {
	if resolution.AssessmentVersion != currentVersion || resolution.AssessmentVersion != assessment.Version || resolution.AssessmentID != assessment.ID || resolution.AssessmentFingerprint != assessment.Fingerprint || validText(resolution.AlternativeID, "resolution alternative id", true, 128) != nil || resolution.AlternativeKind.Validate() != nil || resolution.EffectKind.Validate() != nil || !validFingerprint(resolution.EffectFingerprint) || resolution.ResolvedAt.IsZero() || validText(resolution.Actor, "resolution actor", true, 128) != nil || validText(resolution.Reason, "resolution reason", true, 256) != nil || validText(resolution.CorrelationID, "resolution correlation id", false, 128) != nil {
		return hypothesisError(ErrInvalidHypothesis, family, setID, "validate_resolution", 0, 0, ErrResolutionMaterialMissing)
	}
	var selected *Alternative
	for i := range alternatives {
		if alternatives[i].ID == resolution.AlternativeID {
			selected = &alternatives[i]
			break
		}
	}
	if selected == nil || selected.Kind != resolution.AlternativeKind || selected.ResolutionEffect == nil || selected.ResolutionEffect.Kind != resolution.EffectKind {
		return ErrResolutionAlternativeMismatch
	}
	fingerprint, err := selected.ResolutionEffect.Fingerprint()
	if err != nil || fingerprint != resolution.EffectFingerprint {
		return ErrResolutionEffectMismatch
	}
	return resolution.Outcome.validateFor(*selected.ResolutionEffect)
}

func cloneSubject(subject Subject) Subject { return subject }

func cloneFacts(facts []FactReference) []FactReference {
	result := make([]FactReference, len(facts))
	for i, fact := range facts {
		result[i] = fact
		result[i].ObservationIDs = append([]string(nil), fact.ObservationIDs...)
	}
	return result
}

func cloneAlternatives(alternatives []Alternative) []Alternative {
	result := make([]Alternative, len(alternatives))
	for i, alternative := range alternatives {
		result[i] = alternative
		result[i].Facts = cloneFacts(alternative.Facts)
		result[i].ResolutionEffect = cloneResolutionEffect(alternative.ResolutionEffect)
	}
	return result
}

func cloneAssessments(assessments []AssessmentVersion) []AssessmentVersion {
	result := make([]AssessmentVersion, len(assessments))
	for i, assessment := range assessments {
		result[i] = assessment.Clone()
	}
	return result
}

func cloneHistory(history []RevisionRecord) []RevisionRecord {
	return append([]RevisionRecord(nil), history...)
}

func validText(value, name string, required bool, max int) error {
	if required && strings.TrimSpace(value) == "" {
		return fmt.Errorf("%s must not be empty", name)
	}
	if strings.ContainsAny(value, "\r\n") || len([]rune(value)) > max {
		return fmt.Errorf("%s is invalid", name)
	}
	return nil
}

func validSetID(id SetID) error {
	if err := validText(string(id), "hypothesis set id", true, 128); err != nil {
		return err
	}
	if !strings.HasPrefix(string(id), "cge-hyp-") {
		return fmt.Errorf("hypothesis set id has invalid prefix")
	}
	return nil
}
