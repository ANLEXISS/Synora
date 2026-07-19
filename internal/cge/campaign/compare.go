package campaign

import "fmt"

func calibrationFindings(report Report, profile Profile) []CalibrationFinding {
	findings := make([]CalibrationFinding, 0)
	if report.Warmup.FirstEvaluatedAt == nil {
		findings = append(findings, CalibrationFinding{Code: "warmup_too_long", Severity: SeverityWarning, Message: "aucune occurrence évaluable dans la campagne"})
	}
	if report.BenignDeviation.HighRatePermille > 200 {
		findings = append(findings, CalibrationFinding{Code: "benign_high_deviation_rate", Severity: SeverityWarning, Message: fmt.Sprintf("taux high bénin %d‰", report.BenignDeviation.HighRatePermille)})
	}
	if profile.ID == "synthetic_intrusion_30d" && report.Separation.SyntheticP50 <= report.Separation.OrdinaryMedian {
		findings = append(findings, CalibrationFinding{Code: "synthetic_episode_not_separated", Severity: SeverityWarning, Message: "médiane synthétique non supérieure à la médiane ordinaire"})
	}
	if report.EventCount > 0 && report.Warmup.InsufficientHistoryCount*1000/report.EventCount > 900 {
		findings = append(findings, CalibrationFinding{Code: "warmup_dominant", Severity: SeverityInfo, Message: "historique insuffisant dominant la campagne"})
	}
	if report.RestartCount > 0 && len(report.InvariantFailures) > 0 {
		findings = append(findings, CalibrationFinding{Code: "restart_invariant_failure", Severity: SeverityBlocking, Message: "une frontière de redémarrage a divergé"})
	}
	return findings
}
