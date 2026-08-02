package calibrationanalytics

func reportFingerprint(value CalibrationAnalyticsReport) string {
	value.ReportFingerprint = ""
	return digest("calibration-analytics-report-v1:", value)
}
