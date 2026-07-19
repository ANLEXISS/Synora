package demo

import "time"

// Options controls one isolated, deterministic demonstrator run.
type Options struct {
	Scenario string
	Seed     uint64
	Locale   string
	RootDir  string
}

type LiveOptions struct {
	Seed   uint64
	Locale string
}

type LiveEventInput struct {
	EventID           string    `json:"event_id,omitempty"`
	EventType         string    `json:"event_type"`
	Identity          string    `json:"identity,omitempty"`
	IdentityLabel     string    `json:"identity_label,omitempty"`
	NodeID            string    `json:"node_id"`
	HouseMode         string    `json:"house_mode"`
	Occupancy         string    `json:"occupancy"`
	ContextQuality    string    `json:"context_quality"`
	SimulatedAt       time.Time `json:"simulated_at"`
	PrepareAmbiguity  bool      `json:"prepare_ambiguity,omitempty"`
	SequenceKey       string    `json:"sequence_key,omitempty"`
	DeviceID          string    `json:"device_id,omitempty"`
	TrackID           string    `json:"track_id,omitempty"`
	TopologyAvailable *bool     `json:"topology_available,omitempty"`
}

type LiveAdvanceRequest struct {
	Minutes int       `json:"minutes,omitempty"`
	At      time.Time `json:"at,omitempty"`
}

type LiveBatchRequest struct {
	Input LiveEventInput `json:"input"`
	Count int            `json:"count"`
	Step  string         `json:"step,omitempty"`
	Delay int            `json:"delay_ms,omitempty"`
}

type LiveCandidate struct {
	ChainID        string          `json:"chain_id"`
	SourceRevision uint64          `json:"source_revision"`
	Status         string          `json:"status"`
	Eligible       bool            `json:"eligible"`
	Score          int64           `json:"score"`
	RejectionCode  string          `json:"rejection_code,omitempty"`
	Facts          []LiveScoreFact `json:"facts,omitempty"`
}
type LiveScoreFact struct {
	Code   string `json:"code"`
	Score  int64  `json:"score"`
	Detail string `json:"detail"`
}
type LiveAssociationResult struct {
	Decision        string          `json:"decision"`
	PolicyVersion   string          `json:"policy_version"`
	BestScore       int64           `json:"best_score"`
	ScoreMargin     int64           `json:"score_margin"`
	SelectedChainID string          `json:"selected_chain_id,omitempty"`
	NewChainID      string          `json:"new_chain_id,omitempty"`
	ReasonCode      string          `json:"reason_code"`
	Reason          string          `json:"reason"`
	Candidates      []LiveCandidate `json:"candidates"`
}
type LiveContextResult struct {
	ObservationID string    `json:"observation_id"`
	ObservedAt    time.Time `json:"observed_at"`
	NodeID        string    `json:"node_id"`
	ZoneID        string    `json:"zone_id"`
	NodeKind      string    `json:"node_kind"`
	HouseMode     string    `json:"house_mode"`
	Occupancy     string    `json:"occupancy"`
	Quality       string    `json:"quality"`
	Timezone      string    `json:"timezone"`
	Weekday       string    `json:"weekday"`
	MinuteOfDay   int       `json:"minute_of_day"`
	TimeBucket    int       `json:"time_bucket"`
	DayPart       string    `json:"day_part"`
	Fingerprint   string    `json:"fingerprint"`
}
type LiveEvidenceFact struct {
	Code           string   `json:"code"`
	Side           string   `json:"side"`
	Score          int64    `json:"score"`
	Detail         string   `json:"detail"`
	ObservationIDs []string `json:"observation_ids,omitempty"`
}
type LiveEvidenceResult struct {
	Decision           string             `json:"decision"`
	ChainID            string             `json:"chain_id,omitempty"`
	SourceRevision     uint64             `json:"source_revision,omitempty"`
	SupportScore       int64              `json:"support_score"`
	ContradictionScore int64              `json:"contradiction_score"`
	DecisionMargin     int64              `json:"decision_margin"`
	ReasonCode         string             `json:"reason_code,omitempty"`
	Reason             string             `json:"reason,omitempty"`
	Fingerprint        string             `json:"fingerprint,omitempty"`
	Facts              []LiveEvidenceFact `json:"facts,omitempty"`
}
type LiveHypothesisResult struct {
	Action string `json:"action"`
	Count  int    `json:"count"`
	Opened []any  `json:"opened,omitempty"`
}
type LiveFactor struct {
	Kind         string   `json:"kind"`
	Available    bool     `json:"available"`
	Score        uint16   `json:"score"`
	Weight       uint16   `json:"weight"`
	Contribution uint16   `json:"contribution"`
	ReasonCodes  []string `json:"reason_codes,omitempty"`
}

type LiveTemporalBin struct {
	Weekday    string `json:"weekday"`
	TimeBucket int    `json:"time_bucket"`
	Count      uint64 `json:"count"`
}

type LiveIntervalStatistics struct {
	Count   uint64        `json:"count"`
	Minimum time.Duration `json:"minimum"`
	Maximum time.Duration `json:"maximum"`
	Total   time.Duration `json:"total"`
	Mean    time.Duration `json:"mean"`
}

type LiveRoutineDiagnostic struct {
	RoutineID          string                 `json:"routine_id"`
	Revision           uint64                 `json:"revision"`
	OccurrenceCount    uint64                 `json:"occurrence_count"`
	LastSeenAt         time.Time              `json:"last_seen_at"`
	PatternKind        string                 `json:"pattern_kind"`
	PatternNodeID      string                 `json:"pattern_node_id,omitempty"`
	PatternHouseMode   string                 `json:"pattern_house_mode,omitempty"`
	PatternOccupancy   string                 `json:"pattern_occupancy,omitempty"`
	TemporalBins       []LiveTemporalBin      `json:"temporal_bins"`
	IntervalStatistics LiveIntervalStatistics `json:"interval_statistics"`
}
type LiveDeviationResult struct {
	Attempted      bool                   `json:"attempted"`
	Status         string                 `json:"status,omitempty"`
	Band           string                 `json:"band,omitempty"`
	Score          uint16                 `json:"score"`
	Coverage       uint16                 `json:"coverage"`
	RoutineID      string                 `json:"routine_id,omitempty"`
	CandidateCount int                    `json:"candidate_count"`
	BaselineCount  int                    `json:"baseline_count"`
	Factors        []LiveFactor           `json:"factors,omitempty"`
	ReasonCodes    []string               `json:"reason_codes,omitempty"`
	Fingerprint    string                 `json:"fingerprint,omitempty"`
	Observed       LiveContextResult      `json:"observed"`
	Routine        *LiveRoutineDiagnostic `json:"routine,omitempty"`
	RoutineReady   bool                   `json:"routine_ready"`
}
type LiveLearningResult struct {
	PresenceCreated       int    `json:"presence_created"`
	PresenceAdded         int    `json:"presence_added"`
	TransitionCreated     int    `json:"transition_created"`
	TransitionAdded       int    `json:"transition_added"`
	RoutineCountBefore    int    `json:"routine_count_before"`
	RoutineCountAfter     int    `json:"routine_count_after"`
	OccurrenceCountBefore uint64 `json:"occurrence_count_before"`
	OccurrenceCountAfter  uint64 `json:"occurrence_count_after"`
}
type LiveChainSummary struct {
	ID               string    `json:"id"`
	EntityID         string    `json:"entity_id,omitempty"`
	Status           string    `json:"status"`
	Revision         uint64    `json:"revision"`
	ObservationCount int       `json:"observation_count"`
	Confidence       float64   `json:"confidence"`
	FirstSeenAt      time.Time `json:"first_seen_at"`
	LastSeenAt       time.Time `json:"last_seen_at"`
}
type LiveWALRecord struct {
	Sequence      uint64    `json:"sequence"`
	Kind          string    `json:"kind"`
	RecordedAt    time.Time `json:"recorded_at"`
	Actor         string    `json:"actor"`
	CorrelationID string    `json:"correlation_id"`
	PreviousHash  string    `json:"previous_hash"`
	RecordHash    string    `json:"record_hash"`
}
type LiveGlobalState struct {
	SimulatedAt         time.Time `json:"simulated_at"`
	ChainCount          int       `json:"chain_count"`
	OpenHypothesisCount int       `json:"open_hypothesis_count"`
	HypothesisCount     int       `json:"hypothesis_count"`
	RoutineCount        int       `json:"routine_count"`
	ObservationCount    uint64    `json:"observation_count"`
	JournalSequence     uint64    `json:"journal_sequence"`
	JournalHeadHash     string    `json:"journal_head_hash"`
	CoordinatorState    string    `json:"coordinator_state"`
	DeviationStoreCount int       `json:"deviation_store_count"`
	ActionsEnabled      bool      `json:"actions_enabled"`
	Synthetic           bool      `json:"synthetic"`
}
type LiveTraceStep struct {
	Sequence       uint64    `json:"sequence"`
	Kind           string    `json:"kind"`
	At             time.Time `json:"at"`
	DurationMicros uint64    `json:"duration_micros,omitempty"`
	Payload        any       `json:"payload,omitempty"`
}
type LiveInjectionResult struct {
	EventID             string                `json:"event_id"`
	SimulatedAt         time.Time             `json:"simulated_at"`
	Context             LiveContextResult     `json:"context"`
	Association         LiveAssociationResult `json:"association"`
	Evidence            LiveEvidenceResult    `json:"evidence"`
	Hypothesis          LiveHypothesisResult  `json:"hypothesis"`
	Deviation           LiveDeviationResult   `json:"deviation"`
	Learning            LiveLearningResult    `json:"learning"`
	ChainBefore         *LiveChainSummary     `json:"chain_before,omitempty"`
	ChainAfter          *LiveChainSummary     `json:"chain_after,omitempty"`
	WALRecords          []LiveWALRecord       `json:"wal_records"`
	GlobalState         LiveGlobalState       `json:"global_state"`
	TotalDurationMicros uint64                `json:"total_duration_micros"`
	Trace               []LiveTraceStep       `json:"trace"`
}
type LiveState struct {
	SessionID            string                `json:"session_id"`
	Seed                 uint64                `json:"seed"`
	SimulatedAt          time.Time             `json:"simulated_at"`
	Topology             any                   `json:"topology"`
	Global               LiveGlobalState       `json:"global"`
	Chains               []LiveChainSummary    `json:"chains"`
	Hypotheses           []any                 `json:"hypotheses"`
	Routines             []any                 `json:"routines"`
	WAL                  []LiveWALRecord       `json:"wal"`
	Trace                []LiveTraceStep       `json:"trace"`
	Events               []LiveInjectionResult `json:"events"`
	LastResult           *LiveInjectionResult  `json:"last_result,omitempty"`
	Mode                 string                `json:"mode"`
	SyntheticNotice      string                `json:"synthetic_notice"`
	Qualification        string                `json:"qualification"`
	Capabilities         CapabilityRegistry    `json:"capabilities"`
	InterpretationNotice string                `json:"interpretation_notice"`
	Scenario             *ScenarioRunState     `json:"scenario,omitempty"`
}

type Capability struct {
	ID          string        `json:"id"`
	Label       LocalizedText `json:"label"`
	Description LocalizedText `json:"description"`
}

type CapabilityRegistry struct {
	Title       LocalizedText `json:"title"`
	Available   []Capability  `json:"available"`
	Unavailable []Capability  `json:"unavailable"`
}

func CurrentCapabilities() CapabilityRegistry {
	return CapabilityRegistry{
		Title: LocalizedText{FR: "Capacités actuelles", EN: "Current capabilities"},
		Available: []Capability{
			{ID: "association", Label: LocalizedText{FR: "Association", EN: "Association"}, Description: LocalizedText{FR: "Plans et décisions de rattachement des observations.", EN: "Plans and decisions for attaching observations."}},
			{ID: "association-ambiguity", Label: LocalizedText{FR: "Ambiguïté d’association", EN: "Association ambiguity"}, Description: LocalizedText{FR: "Plusieurs chaînes candidates peuvent rester ouvertes.", EN: "Several candidate chains may remain open."}},
			{ID: "context-memory", Label: LocalizedText{FR: "Mémoire contextuelle", EN: "Contextual memory"}, Description: LocalizedText{FR: "Contexte spatial, temporel et état du domicile.", EN: "Spatial, temporal and house-state context."}},
			{ID: "routines", Label: LocalizedText{FR: "Routines", EN: "Routines"}, Description: LocalizedText{FR: "Agrégation descriptive d’épisodes répétés.", EN: "Descriptive aggregation of repeated episodes."}},
			{ID: "deviation", Label: LocalizedText{FR: "Divergence explicable", EN: "Explainable divergence"}, Description: LocalizedText{FR: "Facteurs structurels, temporels et d’intervalle.", EN: "Structural, temporal and interval factors."}},
			{ID: "learning", Label: LocalizedText{FR: "Apprentissage continu", EN: "Continuous learning"}, Description: LocalizedText{FR: "Apprentissage post-évaluation et idempotent.", EN: "Post-assessment and idempotent learning."}},
			{ID: "replay", Label: LocalizedText{FR: "Replay", EN: "Replay"}, Description: LocalizedText{FR: "Mémoire durable et rejouable.", EN: "Durable and replayable memory."}},
		},
		Unavailable: []Capability{
			{ID: "situation-hypothesis", Label: LocalizedText{FR: "Hypothèse de situation", EN: "Situation hypothesis"}, Description: LocalizedText{FR: "Non disponible actuellement.", EN: "Not currently available."}},
			{ID: "intent-causality", Label: LocalizedText{FR: "Interprétation d’intention et causalité", EN: "Intent and causal interpretation"}, Description: LocalizedText{FR: "Non produite par cette version.", EN: "Not produced by this version."}},
			{ID: "threat-qualification", Label: LocalizedText{FR: "Qualification automatique d’une menace", EN: "Automatic threat qualification"}, Description: LocalizedText{FR: "Non disponible ; le CGE n’a pas d’autorité de sécurité.", EN: "Not available; the CGE has no security authority."}},
		},
	}
}

// DemoEvent is the typed, bounded transport envelope used by the local UI.
type DemoEvent struct {
	Sequence uint64    `json:"sequence"`
	Chapter  string    `json:"chapter"`
	Kind     string    `json:"kind"`
	At       time.Time `json:"at"`
	Payload  any       `json:"payload"`
}

type RunResult struct {
	Scenario  string      `json:"scenario"`
	Seed      uint64      `json:"seed"`
	StartedAt time.Time   `json:"started_at"`
	EndedAt   time.Time   `json:"ended_at"`
	Events    []DemoEvent `json:"events"`
	Snapshot  Snapshot    `json:"snapshot"`
	Manifest  Manifest    `json:"manifest"`
}

type Snapshot struct {
	ObservationCount uint64 `json:"observation_count"`
	ChainCount       int    `json:"chain_count"`
	HypothesisCount  int    `json:"hypothesis_count"`
	RoutineCount     int    `json:"routine_count"`
	JournalSequence  uint64 `json:"journal_sequence"`
	JournalHeadHash  string `json:"journal_head_hash"`
	DurableDigest    string `json:"durable_digest"`
	ReplayDigest     string `json:"replay_digest,omitempty"`
	ReplayEqual      bool   `json:"replay_equal"`
	DeviationStore   int    `json:"deviation_store_count"`
	CoordinatorState string `json:"coordinator_state"`
	Metrics          any    `json:"metrics,omitempty"`
	Chains           any    `json:"chains,omitempty"`
	Hypotheses       any    `json:"hypotheses,omitempty"`
	Routines         any    `json:"routines,omitempty"`
	LatestDeviation  any    `json:"latest_deviation,omitempty"`
	Performance      any    `json:"performance,omitempty"`
}

type Manifest struct {
	Scenario               string `json:"scenario"`
	Seed                   uint64 `json:"seed"`
	Commit                 string `json:"commit,omitempty"`
	PolicyVersions         any    `json:"policy_versions"`
	CognitiveFingerprint   string `json:"cognitive_fingerprint"`
	ExecutedAt             string `json:"executed_at"`
	SyntheticScenario      bool   `json:"synthetic_scenario"`
	SyntheticWarning       string `json:"synthetic_warning"`
	SecurityAuthority      string `json:"security_authority"`
	QualificationAvailable bool   `json:"qualification_available"`
	Qualification          any    `json:"qualification,omitempty"`
}

type ClaimsFile struct {
	Claims []Claim `json:"claims"`
}

type Claim struct {
	ID          string   `json:"id"`
	Title       string   `json:"title"`
	Status      string   `json:"status"`
	Evidence    []string `json:"evidence"`
	Limitations []string `json:"limitations"`
}

type ScenarioInfo struct {
	ID                   string             `json:"id"`
	Title                string             `json:"title"`
	Description          string             `json:"description"`
	Minutes              int                `json:"minutes"`
	LocalizedTitle       LocalizedText      `json:"localized_title,omitempty"`
	LocalizedDescription LocalizedText      `json:"localized_description,omitempty"`
	Category             ScenarioCategory   `json:"category,omitempty"`
	Difficulty           ScenarioDifficulty `json:"difficulty,omitempty"`
	StepCount            int                `json:"step_count,omitempty"`
	Tags                 []string           `json:"tags,omitempty"`
	Unsupported          bool               `json:"unsupported,omitempty"`
}

func Scenarios() []ScenarioInfo {
	return []ScenarioInfo{
		{ID: "investor-core", Title: "Parcours investisseur", Description: "Du signal brut à une mémoire cognitive locale, durable et explicable.", Minutes: 9},
		{ID: "routine-formation", Title: "Routine en formation", Description: "Épisodes, warm-up, readiness et première comparaison.", Minutes: 3},
		{ID: "temporal-divergence", Title: "Divergence temporelle", Description: "Même sujet et lieu, horaire différent.", Minutes: 2},
		{ID: "spatial-divergence", Title: "Divergence spatiale", Description: "Même sujet et heure, autre nœud.", Minutes: 2},
		{ID: "house-mode-divergence", Title: "Divergence contextuelle", Description: "Mode du domicile différent.", Minutes: 2},
		{ID: "interval-divergence", Title: "Divergence d’intervalle", Description: "Intervalle hors enveloppe historique.", Minutes: 2},
		{ID: "combined-divergence", Title: "Divergence combinée", Description: "Décomposition de plusieurs facteurs.", Minutes: 2},
		{ID: "partial-context", Title: "Contexte partiel", Description: "Couverture réduite et champs indisponibles.", Minutes: 2},
		{ID: "routine-shift", Title: "Adaptation de la mémoire", Description: "Évolution progressive des bins.", Minutes: 3},
		{ID: "association-ambiguity", Title: "Association ambiguë", Description: "Plusieurs chaînes candidates restent ouvertes.", Minutes: 2},
		{ID: "unknown-new-subject", Title: "Nouveau sujet inconnu", Description: "Chaîne séparée et historique insuffisant.", Minutes: 2},
		{ID: "idempotent-retry", Title: "Retry idempotent", Description: "Continuité de l’apprentissage et du WAL.", Minutes: 2},
		{ID: "restart-replay", Title: "Redémarrage et replay", Description: "Le journal reconstruit la mémoire durable.", Minutes: 2},
		{ID: "memory-field-isolation", Title: "Isolation des champs de mémoire", Description: "Matrice des facteurs à baseline constante.", Minutes: 3},
	}
}
