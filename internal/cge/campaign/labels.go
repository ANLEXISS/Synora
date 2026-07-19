package campaign

func ValidLabel(value EpisodeLabel) bool {
	switch value {
	case LabelOrdinary, LabelBenignVariation, LabelRoutineChange, LabelRareLegitimate,
		LabelSyntheticIntrusion, LabelSensorDropout, LabelIdentityUncertain,
		LabelTopologyUnavailable, LabelSystemRestart:
		return true
	default:
		return false
	}
}
