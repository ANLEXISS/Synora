package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"time"

	"synora/internal/bus"
	"synora/internal/cognitivecore"
	"synora/internal/discovery"
	"synora/internal/facegallery"
	"synora/internal/security"
)

type residentGalleryResult struct {
	TerminalStatus string
	Result         string
	HTTPStatus     int
	Promotion      string
	Redaction      galleryRedaction
}

const (
	galleryAdminToken   = "central-e2e-gallery-admin-token"
	galleryResidentOnly = "central-e2e-gallery-resident-token"
	galleryManageOnly   = "central-e2e-gallery-manage-token"
)

func runResidentGalleryCase(name string, discoveryClient *bus.Client, store *cognitivecore.UniversalStore, now time.Time) (residentGalleryResult, error) {
	result := residentGalleryResult{TerminalStatus: "completed", Result: "redacted_semantic_only", Redaction: galleryRedaction{
		RawMediaAbsent: true, CropAbsent: true, EmbeddingAbsent: true, PreciseScoreAbsent: true,
		NameAbsent: true, LocalPathAbsent: true, IdentityAbsent: true,
	}}
	config := &security.Config{APITokenScopes: map[string][]string{
		security.HashSecret(galleryAdminToken):   {"resident:write", "face_gallery:manage"},
		security.HashSecret(galleryResidentOnly): {"resident:write"},
		security.HashSecret(galleryManageOnly):   {"face_gallery:manage"},
	}}
	api := discovery.NewExternalAPI(config, &discovery.Boundary{DryRun: true, Store: discovery.NewSnapshotCache()}, nil, nil, nil, discovery.NewBusResidentGalleryService(discoveryClient, func() time.Time { return now }))
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return result, err
	}
	server := &http.Server{Handler: api, ReadHeaderTimeout: time.Second}
	serveDone := make(chan error, 1)
	go func() { serveDone <- server.Serve(listener) }()
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = server.Shutdown(ctx)
		_ = listener.Close()
		select {
		case <-serveDone:
		case <-time.After(time.Second):
		}
	}()
	client := &http.Client{Timeout: 3 * time.Second, Transport: &http.Transport{Proxy: nil}}
	base := "http://" + listener.Addr().String()
	call := func(method, path, token, key string, payload []byte) (int, []byte, error) {
		request, err := http.NewRequest(method, base+path, bytes.NewReader(payload))
		if err != nil {
			return 0, nil, err
		}
		if token != "" {
			request.Header.Set("Authorization", "Bearer "+token)
		}
		if key != "" {
			request.Header.Set("Idempotency-Key", key)
		}
		if len(payload) > 0 {
			request.Header.Set("Content-Type", "application/json")
		}
		response, err := client.Do(request)
		if err != nil {
			return 0, nil, err
		}
		defer response.Body.Close()
		body, err := io.ReadAll(io.LimitReader(response.Body, 4096))
		return response.StatusCode, body, err
	}
	type createdView struct {
		ResidentRef string `json:"resident_ref"`
		Duplicate   bool   `json:"duplicate"`
	}
	create := func(key string) (int, createdView, error) {
		status, body, err := call(http.MethodPost, "/api/residents", galleryAdminToken, key, []byte(`{}`))
		var view createdView
		if err == nil && status == http.StatusCreated {
			err = assertRedactedProjection(body, "resident_ref", "gallery_status", "duplicate")
		}
		if err == nil && status == http.StatusCreated {
			err = json.Unmarshal(body, &view)
			if err == nil && view.ResidentRef == "" {
				err = errors.New("resident create returned no opaque reference")
			}
		}
		return status, view, err
	}
	setupStatus, resident, err := create("gallery-setup-" + name)
	if err != nil || setupStatus != http.StatusCreated {
		return result, fmt.Errorf("authenticated Discovery resident setup failed (HTTP %d): %w", setupStatus, err)
	}
	result.HTTPStatus = setupStatus
	statusPath := "/api/residents/" + resident.ResidentRef + "/face-gallery/status"
	adminRead := func() (int, []byte, error) {
		status, body, callErr := call(http.MethodGet, statusPath, galleryAdminToken, "", nil)
		if callErr == nil && status == http.StatusOK {
			callErr = assertRedactedProjection(body, "resident_ref", "gallery_status", "gallery_generation", "policy_version", "last_error_category")
		}
		return status, body, callErr
	}

	switch name {
	case "resident_create_scoped":
		result.Result = "created_with_required_scopes"
	case "resident_no_token":
		result.HTTPStatus, _, err = call(http.MethodPost, "/api/residents", "", "gallery-no-token-0001", []byte(`{}`))
		result.TerminalStatus, result.Result = "expected_rejection", "authentication_required"
		if err == nil && result.HTTPStatus != http.StatusUnauthorized {
			err = fmt.Errorf("expected HTTP 401, got %d", result.HTTPStatus)
		}
	case "resident_scope_missing_write":
		result.HTTPStatus, _, err = call(http.MethodPost, "/api/residents", galleryManageOnly, "gallery-no-write-0001", []byte(`{}`))
		result.TerminalStatus, result.Result = "expected_rejection", "required_scope_missing"
		if err == nil && result.HTTPStatus != http.StatusForbidden {
			err = fmt.Errorf("expected HTTP 403, got %d", result.HTTPStatus)
		}
	case "resident_scope_missing_manage":
		result.HTTPStatus, _, err = call(http.MethodPost, "/api/residents", galleryResidentOnly, "gallery-no-manage-001", []byte(`{}`))
		result.TerminalStatus, result.Result = "expected_rejection", "required_scope_missing"
		if err == nil && result.HTTPStatus != http.StatusForbidden {
			err = fmt.Errorf("expected HTTP 403, got %d", result.HTTPStatus)
		}
	case "resident_status_redacted":
		result.HTTPStatus, _, err = adminRead()
		result.Result = "redacted_status_read"
		if err == nil && result.HTTPStatus != http.StatusOK {
			err = fmt.Errorf("expected HTTP 200, got %d", result.HTTPStatus)
		}
	case "resident_logical_delete":
		var body []byte
		result.HTTPStatus, body, err = call(http.MethodDelete, "/api/residents/"+resident.ResidentRef, galleryAdminToken, "", nil)
		if err == nil && result.HTTPStatus == http.StatusOK {
			err = assertRedactedProjection(body, "resident_ref", "gallery_status", "gallery_generation", "policy_version", "last_error_category")
		}
		result.Result = "logical_delete"
		if err == nil && result.HTTPStatus != http.StatusOK {
			err = fmt.Errorf("expected HTTP 200, got %d", result.HTTPStatus)
		}
	case "resident_create_idempotent":
		key := "gallery-idempotent-0001"
		firstStatus, first, firstErr := create(key)
		secondStatus, second, secondErr := create(key)
		result.HTTPStatus, result.Result = secondStatus, "idempotent_create"
		if firstErr != nil || secondErr != nil || firstStatus != http.StatusCreated || secondStatus != http.StatusCreated || first.ResidentRef != second.ResidentRef || !second.Duplicate {
			err = errors.New("API idempotence did not return the same redacted resident projection")
		}
	case "face_attestation_missing", "face_attestation_expired", "face_attestation_revoked", "face_source_not_allowlisted":
		result.HTTPStatus, _, err = adminRead()
		result.TerminalStatus, result.Result = "expected_rejection", "attestation_rejected"
		if err == nil && result.HTTPStatus != http.StatusOK {
			err = fmt.Errorf("status API returned HTTP %d", result.HTTPStatus)
		}
		if err == nil {
			err = validateSyntheticAttestationCase(name, resident.ResidentRef, now)
		}
	case "face_backend_unavailable":
		result.HTTPStatus, _, err = adminRead()
		result.TerminalStatus, result.Result = "blocked", "backend_unavailable"
		if (facegallery.UnavailableBackend{}).Status() != "unavailable" {
			err = errors.New("face backend unexpectedly available")
		}
	case "face_score_0649":
		result.Result = facegallery.Evaluate(0.649).Result
		result.TerminalStatus = "unknown"
		if result.Result != "unknown" {
			err = errors.New("below-threshold synthetic case was not unknown")
		}
	case "face_score_0650", "face_score_0899":
		score := 0.650
		if name == "face_score_0899" {
			score = 0.899
		}
		result.Result = facegallery.Evaluate(score).Result
		result.TerminalStatus = "suppressed"
		if result.Result != "candidate" {
			err = errors.New("mid-band synthetic case did not remain a candidate")
		}
	case "face_score_0900_no_consensus":
		result.Result = facegallery.Evaluate(0.900).Reason
		result.TerminalStatus = "unknown"
		if result.Result != "consensus_required" {
			err = errors.New("high-score synthetic case bypassed consensus")
		}
	case "face_consensus_synthetic":
		life := facegallery.NewDeterministicTestLifecycle(func() time.Time { return now })
		for i, observed := range []struct {
			score  float64
			offset time.Duration
		}{{.65, 0}, {.90, 2 * time.Second}, {.90, 4 * time.Second}} {
			semantic := life.Observe(facegallery.Observation{Score: observed.score, Quality: .8, FaceCount: 1, EpisodeKey: "episode-test", TrackKey: "track-test", SampleKey: fmt.Sprintf("sample-%d", i), Provenance: "simulated_test", ObservedAt: now.Add(observed.offset)})
			result.Result = semantic.Result
		}
		if life.PromotedCount() != 1 {
			err = errors.New("synthetic consensus lifecycle did not reach test-only promotion")
		}
		result.Promotion = "simulated_test_memory_only"
	case "face_multi_faces", "face_quality_low":
		faceCount, quality := 2, .8
		if name == "face_quality_low" {
			faceCount, quality = 1, .2
		}
		semantic := facegallery.NewDeterministicTestLifecycle(func() time.Time { return now }).Observe(facegallery.Observation{Score: .95, Quality: quality, FaceCount: faceCount, EpisodeKey: "episode-test", TrackKey: "track-test", SampleKey: "sample-test", Provenance: "simulated_test", ObservedAt: now})
		result.TerminalStatus, result.Result = "expected_rejection", semantic.CandidateState
		if semantic.CandidateState != "candidate_rejected" {
			err = errors.New("unsafe synthetic sample was not rejected")
		}
	case "face_quota":
		episodeLifecycle := facegallery.NewDeterministicTestLifecycle(func() time.Time { return now })
		for i := 0; i < facegallery.MaxPerEpisode+1; i++ {
			result.Result = episodeLifecycle.Observe(facegallery.Observation{Score: .7, Quality: .8, FaceCount: 1, EpisodeKey: "one-episode", TrackKey: fmt.Sprintf("track-%d", i), SampleKey: "sample", Provenance: "simulated_test", ObservedAt: now}).Reason
		}
		if result.Result != "quota_exceeded" {
			err = errors.New("synthetic per-episode quota was not enforced")
		}
		dayLifecycle := facegallery.NewDeterministicTestLifecycle(func() time.Time { return now })
		for i := 0; i < facegallery.MaxPerDay+1; i++ {
			result.Result = dayLifecycle.Observe(facegallery.Observation{Score: .7, Quality: .8, FaceCount: 1, EpisodeKey: fmt.Sprintf("episode-%d", i), TrackKey: fmt.Sprintf("track-%d", i), SampleKey: "sample", Provenance: "simulated_test", ObservedAt: now}).Reason
		}
		if result.Result != "quota_exceeded" {
			err = errors.New("synthetic per-day quota was not enforced")
		}
		result.TerminalStatus = "suppressed"
	case "face_candidate_ttl":
		life := facegallery.NewDeterministicTestLifecycle(func() time.Time { return now })
		_ = life.Observe(facegallery.Observation{Score: .7, Quality: .8, FaceCount: 1, EpisodeKey: "episode-test", TrackKey: "track-test", SampleKey: "sample", Provenance: "simulated_test", ObservedAt: now})
		transitions := life.ExpireTransitions(now.Add(facegallery.CandidateTTL))
		if len(transitions) != 1 {
			err = errors.New("synthetic candidate TTL did not expire")
		}
		if len(transitions) > 0 {
			result.Result = transitions[0].CandidateState
		}
		result.TerminalStatus = "suppressed"
	case "face_generation_rollback":
		vaultRoot, vaultErr := os.MkdirTemp("", "synora-gallery-e2e-")
		if vaultErr == nil {
			defer os.RemoveAll(vaultRoot)
		}
		vault, vaultErr := facegallery.NewVault(vaultRoot)
		if vaultErr == nil {
			_, vaultErr = vault.Initialize(resident.ResidentRef)
		}
		if vaultErr == nil {
			_, vaultErr = vault.CommitEmptyGeneration(resident.ResidentRef)
		}
		if vaultErr == nil {
			var rolled facegallery.VaultManifest
			rolled, _, vaultErr = vault.Rollback(resident.ResidentRef)
			if rolled.Generation != 1 || len(rolled.Entries) != 0 {
				vaultErr = errors.New("rollback projection was not empty generation 1")
			}
		}
		var rollbackBody []byte
		result.HTTPStatus, rollbackBody, err = call(http.MethodPost, "/api/residents/"+resident.ResidentRef+"/face-gallery/rollback", galleryAdminToken, "", []byte(`{}`))
		if err == nil && result.HTTPStatus == http.StatusOK {
			err = assertRedactedProjection(rollbackBody, "resident_ref", "gallery_status", "gallery_generation", "policy_version", "last_error_category")
		}
		if err == nil && (vaultErr != nil || result.HTTPStatus != http.StatusOK) {
			err = fmt.Errorf("rollback failed (HTTP %d)", result.HTTPStatus)
		}
		result.Result = "empty_generation_rollback"
	case "face_forbidden_payload":
		before, beforeExists := store.ResidentGallery(resident.ResidentRef)
		result.HTTPStatus, _, err = call(http.MethodPost, "/api/residents", galleryAdminToken, "gallery-forbidden-0001", []byte(`{"name":"DO_NOT_STORE_NAME","embedding":[0.1]}`))
		after, exists := store.ResidentGallery(resident.ResidentRef)
		result.TerminalStatus, result.Result = "expected_rejection", "forbidden_payload_rejected_before_core_store"
		if err == nil && (result.HTTPStatus != http.StatusBadRequest || !beforeExists || !exists || before != after) {
			err = errors.New("forbidden payload changed Universal Store or was not rejected")
		}
	default:
		err = fmt.Errorf("unknown resident gallery case %q", name)
	}
	if err == nil && name != "resident_logical_delete" {
		record, exists := store.ResidentGallery(resident.ResidentRef)
		if !exists {
			err = errors.New("synthetic resident record disappeared from Universal Store")
		} else if body, marshalErr := json.Marshal(record); marshalErr != nil {
			err = errors.New("Universal Store resident projection could not be checked")
		} else if projectionErr := assertRedactedProjection(body, "resident_ref", "gallery_status", "gallery_generation", "policy_version", "last_error_category", "deleted", "created_at", "updated_at"); projectionErr != nil {
			err = errors.New("Universal Store resident projection was not redacted")
		}
	}
	if err != nil {
		return result, err
	}
	if result.TerminalStatus == "" {
		result.TerminalStatus = "completed"
	}
	return result, nil
}

func assertRedactedProjection(body []byte, allowed ...string) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(body, &fields); err != nil {
		return errors.New("Discovery API projection was not JSON")
	}
	allow := make(map[string]bool, len(allowed))
	for _, key := range allowed {
		allow[key] = true
	}
	for key := range fields {
		if !allow[key] {
			return errors.New("Discovery API projection contained a non-allowlisted field")
		}
	}
	for _, forbidden := range []string{"image", "media", "crop", "embedding", "score", "name", "path", "identity", "photo", "url"} {
		if bytes.Contains(bytes.ToLower(body), []byte(forbidden)) {
			return errors.New("Discovery API projection contained a forbidden biometric or identifying value")
		}
	}
	return nil
}

func validateSyntheticAttestationCase(name, residentRef string, now time.Time) error {
	allowed := map[string]facegallery.SourceSet{"synthetic-allowlist": {ID: "synthetic-allowlist", Digest: "synthetic-digest"}}
	value := facegallery.Attestation{SchemaVersion: facegallery.ConsentSchemaVersion, ResidentRef: residentRef, Purpose: "face_gallery_enrollment", SourceSetID: "synthetic-allowlist", SourceSetDigest: "synthetic-digest", IssuedAt: now.Add(-time.Hour), PolicyVersion: "face-gallery-policy/v1", AttestationReference: "synthetic-attestation", Provenance: "local_attestation"}
	switch name {
	case "face_attestation_missing":
		value = facegallery.Attestation{}
	case "face_attestation_expired":
		expired := now.Add(-time.Second)
		value.ExpiresAt = &expired
	case "face_attestation_revoked":
		value.Revoked = true
	case "face_source_not_allowlisted":
		value.SourceSetID = "not-allowlisted"
	}
	if err := facegallery.ValidateAttestation(value, residentRef, allowed, now); err == nil {
		return errors.New("invalid synthetic attestation was accepted")
	}
	return nil
}
