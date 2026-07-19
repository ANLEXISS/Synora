package episodes

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"
)

type Policy struct {
	SameTrackMaxGap      time.Duration
	SameActivationMaxGap time.Duration
	SameSubjectMaxGap    time.Duration
	UnknownSubjectMaxGap time.Duration

	MaxEpisodeDuration time.Duration
	QuiescentAfter     time.Duration
	CloseAfter         time.Duration

	LateObservationGrace time.Duration

	MinAttachScore    int
	MinDecisionMargin int
	MaxCandidates     int
	MaxObservations   int
}

func DefaultPolicy() Policy {
	return Policy{SameTrackMaxGap: 45 * time.Second, SameActivationMaxGap: 3 * time.Minute, SameSubjectMaxGap: 2 * time.Minute, UnknownSubjectMaxGap: 30 * time.Second, MaxEpisodeDuration: 10 * time.Minute, QuiescentAfter: 30 * time.Second, CloseAfter: 2 * time.Minute, LateObservationGrace: 30 * time.Second, MinAttachScore: 650, MinDecisionMargin: 100, MaxCandidates: 20, MaxObservations: 128}
}

func (p Policy) Validate() error {
	if p.SameTrackMaxGap <= 0 || p.SameActivationMaxGap <= 0 || p.SameSubjectMaxGap <= 0 || p.UnknownSubjectMaxGap <= 0 || p.MaxEpisodeDuration <= 0 || p.QuiescentAfter <= 0 || p.CloseAfter <= 0 || p.LateObservationGrace < 0 || p.MinAttachScore < 0 || p.MinAttachScore > 1000 || p.MinDecisionMargin < 0 || p.MinDecisionMargin > 1000 || p.MaxCandidates <= 0 || p.MaxObservations <= 0 {
		return ErrInvalidPolicy
	}
	if p.CloseAfter < p.QuiescentAfter || p.SameTrackMaxGap > p.MaxEpisodeDuration || p.SameActivationMaxGap > p.MaxEpisodeDuration || p.SameSubjectMaxGap > p.MaxEpisodeDuration || p.UnknownSubjectMaxGap > p.MaxEpisodeDuration {
		return fmt.Errorf("%w: duration ordering", ErrInvalidPolicy)
	}
	return nil
}

func (p Policy) Fingerprint() string {
	if p.Validate() != nil {
		return "episode-policy-v1:invalid"
	}
	value := struct {
		SameTrackMaxGap, SameActivationMaxGap, SameSubjectMaxGap, UnknownSubjectMaxGap int64
		MaxEpisodeDuration, QuiescentAfter, CloseAfter, LateObservationGrace           int64
		MinAttachScore, MinDecisionMargin, MaxCandidates, MaxObservations              int
	}{p.SameTrackMaxGap.Nanoseconds(), p.SameActivationMaxGap.Nanoseconds(), p.SameSubjectMaxGap.Nanoseconds(), p.UnknownSubjectMaxGap.Nanoseconds(), p.MaxEpisodeDuration.Nanoseconds(), p.QuiescentAfter.Nanoseconds(), p.CloseAfter.Nanoseconds(), p.LateObservationGrace.Nanoseconds(), p.MinAttachScore, p.MinDecisionMargin, p.MaxCandidates, p.MaxObservations}
	payload, _ := json.Marshal(value)
	digest := sha256.Sum256(payload)
	return "episode-policy-v1:" + hex.EncodeToString(digest[:])
}
