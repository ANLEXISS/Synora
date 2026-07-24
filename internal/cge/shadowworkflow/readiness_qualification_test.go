package shadowworkflow

import "testing"

func TestQualificationReadinessRequiresSoftwareEvidenceAndNoPhysicalAuthority(t *testing.T) {
	readiness := QualificationReadiness()
	if !readiness.ReadyForPhysicalShadowQualification || !readiness.FullPipelineIntegrationValidated || !readiness.CorruptWALIsolationValidated || !readiness.CircuitHalfOpenValidated || !readiness.HistoricalGoldenRegressionValidated || !readiness.LogsRedacted || !readiness.ConcurrencyValidated {
		t.Fatalf("software qualification incomplete: %+v", readiness)
	}
	if readiness.PhysicalDeploymentPerformed || readiness.MultiDayStabilityValidated || readiness.ProductionAuthority || readiness.ActiveObservationImplemented || readiness.ActionExecutionImplemented || readiness.SecurityAuthority {
		t.Fatalf("qualification crossed physical or authority boundary: %+v", readiness)
	}
}
