package calibrationanalytics

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

func digest(prefix string, value any) string {
	b, _ := json.Marshal(value)
	h := sha256.Sum256(b)
	return prefix + hex.EncodeToString(h[:])
}

func analyticsMarkers() AnalyticsMarkers {
	return AnalyticsMarkers{DescriptiveOnly: true, NotModelAccuracy: true, NotGroundTruth: true, NotAutomaticCalibration: true, NotAProductionDecision: true, NotARecommendation: true, NotAuthorization: true, NotACommand: true, NotAnAction: true, NotAnAlert: true, DoesNotChangeThresholds: true, DoesNotChangeWeights: true, DoesNotSelectPolicy: true, DoesNotDeployPolicy: true, HistoricalProductionAuthorityUnchanged: true, NoSecurityMeaning: true}
}

func policyEvaluationMarkers() PolicyEvaluationMarkers {
	return PolicyEvaluationMarkers{ComparativeOnly: true, NoWinnerSelected: true, NoPolicyRecommended: true, NoPolicyActivated: true, NoProductionFeedback: true, NoAutomaticCalibration: true}
}
