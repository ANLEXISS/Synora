package facegallery

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func validAttestation(now time.Time) Attestation {
	return Attestation{SchemaVersion: ConsentSchemaVersion, ResidentRef: "res_0123456789abcdef0123456789abcdef", Purpose: "face_gallery_enrollment", SourceSetID: "seed-approved-v1", SourceSetDigest: "sha256:synthetic-digest", IssuedAt: now.Add(-time.Minute), PolicyVersion: "face-gallery-policy/v1", AttestationReference: "attest-ref-001", Provenance: "local_attestation"}
}

func TestAttestationValidationFailClosed(t *testing.T) {
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	allowed := map[string]SourceSet{"seed-approved-v1": {ID: "seed-approved-v1", Digest: "sha256:synthetic-digest"}}
	valid := validAttestation(now)
	if err := ValidateAttestation(valid, valid.ResidentRef, allowed, now); err != nil {
		t.Fatalf("valid technical attestation rejected: %v", err)
	}
	for name, mutate := range map[string]func(*Attestation){
		"wrong resident":         func(a *Attestation) { a.ResidentRef = "res_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" },
		"wrong purpose":          func(a *Attestation) { a.Purpose = "other" },
		"source not allowlisted": func(a *Attestation) { a.SourceSetID = "arbitrary" },
		"digest mismatch":        func(a *Attestation) { a.SourceSetDigest = "sha256:wrong" },
		"revoked":                func(a *Attestation) { a.Revoked = true },
		"simulated":              func(a *Attestation) { a.Provenance = "simulated_test" },
		"path in reference":      func(a *Attestation) { a.AttestationReference = "../../private" },
		"policy mismatch":        func(a *Attestation) { a.PolicyVersion = "other" },
	} {
		t.Run(name, func(t *testing.T) {
			candidate := valid
			mutate(&candidate)
			if err := ValidateAttestation(candidate, valid.ResidentRef, allowed, now); err != ErrConsentUnverified {
				t.Fatalf("invalid attestation error=%v", err)
			}
		})
	}
	expired := valid
	expiry := now
	expired.ExpiresAt = &expiry
	if err := ValidateAttestation(expired, valid.ResidentRef, allowed, now); err != ErrConsentUnverified {
		t.Fatalf("expired attestation error=%v", err)
	}
	future := valid
	future.IssuedAt = now.Add(time.Hour)
	if err := ValidateAttestation(future, valid.ResidentRef, allowed, now); err != ErrConsentUnverified {
		t.Fatalf("future attestation error=%v", err)
	}
}

func TestLoadAttestationRejectsSymlinkAndUnknownFields(t *testing.T) {
	root := t.TempDir()
	ref := "res_0123456789abcdef0123456789abcdef"
	if _, err := LoadAttestation(root, ref); err == nil {
		t.Fatal("missing attestation accepted")
	}
	path := filepath.Join(root, ref+".json")
	if err := os.Symlink(filepath.Join(root, "missing"), path); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadAttestation(root, ref); err == nil {
		t.Fatal("symlinked consent manifest accepted")
	}
	_ = os.Remove(path)
	if err := os.WriteFile(path, []byte(`{"schema_version":"face-gallery-consent/v1","unexpected":"value"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadAttestation(root, ref); err == nil {
		t.Fatal("unknown attestation field accepted")
	}
}

func TestVaultEmptyGenerationsRollbackAndIntegrity(t *testing.T) {
	root := filepath.Join(t.TempDir(), "vault")
	vault, err := NewVault(root)
	if err != nil {
		t.Fatal(err)
	}
	ref := "res_0123456789abcdef0123456789abcdef"
	first, err := vault.Initialize(ref)
	if err != nil || first.Generation != 1 || len(first.Entries) != 0 {
		t.Fatalf("empty vault init failed: %+v err=%v", first, err)
	}
	second, err := vault.CommitEmptyGeneration(ref)
	if err != nil || second.Generation != 2 || second.Previous != 1 || len(second.Entries) != 0 {
		t.Fatalf("empty generation commit failed: %+v err=%v", second, err)
	}
	rolled, repeated, err := vault.Rollback(ref)
	if err != nil || repeated || rolled.Generation != 1 {
		t.Fatalf("rollback failed: %+v repeated=%t err=%v", rolled, repeated, err)
	}
	rolled, repeated, err = vault.Rollback(ref)
	if err != nil || !repeated || rolled.Generation != 1 {
		t.Fatalf("rollback was not idempotent: %+v repeated=%t err=%v", rolled, repeated, err)
	}
	if err := os.Chmod(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := NewVault(root); err == nil {
		t.Fatal("vault with broad permissions accepted")
	}
}

func TestVaultRejectsPathAndDetectsCorruption(t *testing.T) {
	vault, err := NewVault(filepath.Join(t.TempDir(), "vault"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := vault.Initialize("../unsafe"); err == nil {
		t.Fatal("path traversal resident accepted")
	}
	ref := "res_0123456789abcdef0123456789abcdef"
	if _, err := vault.Initialize(ref); err != nil {
		t.Fatal(err)
	}
	manifestPath := filepath.Join(vault.root, ref, "generation-1.json")
	if err := os.WriteFile(manifestPath, []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := vault.Current(ref); err == nil {
		t.Fatal("corrupted manifest accepted")
	}
}

func TestDeterministicFacePolicyThresholdsConsensusAndSafety(t *testing.T) {
	if got := Evaluate(.649).Result; got != "unknown" {
		t.Fatalf("0.649 result=%s", got)
	}
	if got := Evaluate(.650).Result; got != "candidate" {
		t.Fatalf("0.650 result=%s", got)
	}
	if got := Evaluate(.899).Result; got != "candidate" {
		t.Fatalf("0.899 result=%s", got)
	}
	if got := Evaluate(.900).Result; got != "ambiguous" {
		t.Fatalf("0.900 without consensus result=%s", got)
	}
	if got := Evaluate(1.1).Result; got != "unavailable" {
		t.Fatalf("out-of-range score result=%s", got)
	}

	now := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	lifecycle := NewDeterministicTestLifecycle(func() time.Time { return now })
	observation := Observation{Score: .7, Quality: .9, FaceCount: 1, EpisodeKey: "episode-a", TrackKey: "track-a", SampleKey: "sample-a", Provenance: "simulated_test", ObservedAt: now}
	if got := lifecycle.Observe(observation); got.Result != "candidate" || got.CandidateState != "candidate_pending" {
		t.Fatalf("candidate not retained in test lifecycle: %+v", got)
	}
	if got := lifecycle.Observe(observation); got.Reason != "duplicate_suppressed" {
		t.Fatalf("duplicate was not suppressed: %+v", got)
	}
	for _, mismatch := range []Observation{
		{Score: .95, Quality: .9, FaceCount: 1, EpisodeKey: "episode-other", TrackKey: "track-a", SampleKey: "other-episode-1", Provenance: "simulated_test", ObservedAt: now},
		{Score: .95, Quality: .9, FaceCount: 1, EpisodeKey: "episode-other", TrackKey: "track-a", SampleKey: "other-episode-2", Provenance: "simulated_test", ObservedAt: now.Add(3 * time.Second)},
		{Score: .95, Quality: .9, FaceCount: 1, EpisodeKey: "episode-a", TrackKey: "track-other", SampleKey: "other-track-1", Provenance: "simulated_test", ObservedAt: now},
		{Score: .95, Quality: .9, FaceCount: 1, EpisodeKey: "episode-a", TrackKey: "track-other", SampleKey: "other-track-2", Provenance: "simulated_test", ObservedAt: now.Add(3 * time.Second)},
	} {
		_ = lifecycle.Observe(mismatch)
	}
	if lifecycle.PromotedCount() != 0 {
		t.Fatal("candidate was promoted across episode or track boundaries")
	}
	anchor := observation
	anchor.Score, anchor.SampleKey = .95, "anchor-1"
	if got := lifecycle.Observe(anchor); got.Result != "ambiguous" {
		t.Fatalf("single anchor promoted: %+v", got)
	}
	anchor.SampleKey, anchor.ObservedAt = "anchor-2", now.Add(3*time.Second)
	if got := lifecycle.Observe(anchor); got.Result != "recognized" || got.CandidateState != "candidate_promoted" || lifecycle.PromotedCount() != 1 {
		t.Fatalf("coherent synthetic anchors did not promote in test lifecycle: %+v", got)
	}
	if got := lifecycle.Observe(Observation{Score: .95, Quality: .99, FaceCount: 2, EpisodeKey: "episode-b", TrackKey: "track-b", SampleKey: "multi", Provenance: "simulated_test", ObservedAt: now}); got.CandidateState != "candidate_rejected" {
		t.Fatalf("multiple faces were not rejected: %+v", got)
	}
	if got := lifecycle.Observe(Observation{Score: .8, Quality: .2, FaceCount: 1, EpisodeKey: "episode-c", TrackKey: "track-c", SampleKey: "low", Provenance: "simulated_test", ObservedAt: now}); got.CandidateState != "candidate_rejected" {
		t.Fatalf("low quality candidate was retained: %+v", got)
	}
	if got := lifecycle.Observe(Observation{Score: .8, Quality: .9, FaceCount: 1, EpisodeKey: "episode-z", TrackKey: "track-z", SampleKey: "replay", Provenance: "replay", ObservedAt: now}); got.Result != "unavailable" {
		t.Fatalf("replay was accepted: %+v", got)
	}
}

func TestCandidateTTLAndQuotas(t *testing.T) {
	now := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	lifecycle := NewDeterministicTestLifecycle(func() time.Time { return now })
	base := Observation{Score: .7, Quality: .9, FaceCount: 1, EpisodeKey: "episode", TrackKey: "track", Provenance: "simulated_test", ObservedAt: now}
	base.SampleKey = "candidate"
	if got := lifecycle.Observe(base); got.Result != "candidate" {
		t.Fatalf("candidate failed: %+v", got)
	}
	expired := lifecycle.ExpireTransitions(now.Add(CandidateTTL))
	if len(expired) != 1 || expired[0].CandidateState != "candidate_expired" {
		t.Fatalf("expired transitions=%+v", expired)
	}
	for i := 0; i < MaxPerEpisode-1; i++ {
		caseItem := base
		caseItem.EpisodeKey = "episode"
		caseItem.TrackKey = "quota-track-" + string(rune('a'+i))
		caseItem.SampleKey = "sample-" + string(rune('a'+i))
		if got := lifecycle.Observe(caseItem); got.Result != "candidate" {
			t.Fatalf("quota case %d unexpected: %+v", i, got)
		}
	}
	base.EpisodeKey, base.TrackKey, base.SampleKey = "episode", "track-over", "over"
	if got := lifecycle.Observe(base); got.Reason != "quota_exceeded" {
		t.Fatalf("candidate quota not enforced: %+v", got)
	}
	dayLifecycle := NewDeterministicTestLifecycle(func() time.Time { return now })
	for i := 0; i < MaxPerDay; i++ {
		caseItem := base
		caseItem.EpisodeKey = "daily-episode-" + string(rune('a'+i))
		caseItem.TrackKey = "daily-track"
		caseItem.SampleKey = "daily-sample-" + string(rune('a'+i))
		if got := dayLifecycle.Observe(caseItem); got.Result != "candidate" {
			t.Fatalf("daily quota setup %d failed: %+v", i, got)
		}
	}
	base.EpisodeKey, base.TrackKey, base.SampleKey = "daily-over", "daily-track", "daily-over-sample"
	if got := dayLifecycle.Observe(base); got.Reason != "quota_exceeded" {
		t.Fatalf("daily quota not enforced: %+v", got)
	}
}

func TestOnlySyntheticBackendProducesDeterministicScores(t *testing.T) {
	if got := (UnavailableBackend{}).Status(); got != "unavailable" {
		t.Fatalf("real backend status=%q", got)
	}
	if _, err := (UnavailableBackend{}).Score(context.Background(), SyntheticObservation{CaseID: "known"}); err == nil {
		t.Fatal("unavailable backend returned a score")
	}
	backend := DeterministicTestBackend{Scores: map[string]float64{"synthetic-case": .65}}
	if got := backend.Status(); got != "simulated_test" {
		t.Fatalf("test backend status=%q", got)
	}
	if score, err := backend.Score(context.Background(), SyntheticObservation{CaseID: "synthetic-case"}); err != nil || score != .65 {
		t.Fatalf("deterministic test score=%v err=%v", score, err)
	}
	if _, err := backend.Score(context.Background(), SyntheticObservation{CaseID: "unlisted"}); err == nil {
		t.Fatal("unlisted synthetic case accepted")
	}
}
