package deviation

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	"synora/internal/cge/routines"
)

func occurrenceFingerprint(occurrence routines.Occurrence) (string, error) {
	payload, err := json.Marshal(occurrence)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(payload)
	return "sha256:" + hex.EncodeToString(digest[:]), nil
}

func assessmentFingerprint(assessment Assessment) (string, error) {
	copy := assessment
	copy.Fingerprint = ""
	payload, err := json.Marshal(copy)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(payload)
	return "sha256:" + hex.EncodeToString(digest[:]), nil
}

func planAssessmentFingerprint(assessment PlanAssessment) (string, error) {
	copy := assessment
	copy.Fingerprint = ""
	payload, err := json.Marshal(copy)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(payload)
	return "sha256:" + hex.EncodeToString(digest[:]), nil
}
