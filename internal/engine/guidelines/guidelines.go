package guidelines

var BlockingEvents = map[string]bool{
	"vision.weapon.firearm":  true,
	"vision.pose.fallen":     true,
	"vision.camera.tampered": true,
}

var GuidelineScores = map[string]float64{

	// ---------------------------------------------------------------------
	// IDENTITY
	// ---------------------------------------------------------------------

	"vision.id.seen":      0.05,
	"vision.id.lost":      0.30,
	"vision.id.uncertain": 0.40,
	"vision.id.changed":   0.50,

	// ---------------------------------------------------------------------
	// MOTION
	// ---------------------------------------------------------------------

	"vision.motion.detected": 0.10,
	"vision.motion.stopped":  0.05,

	// ---------------------------------------------------------------------
	// POSE
	// ---------------------------------------------------------------------

	"vision.pose.standing": 0.05,
	"vision.pose.sitting":  0.05,
	"vision.pose.lying":    0.25,
	"vision.pose.fallen":   0.90,

	// ---------------------------------------------------------------------
	// WEAPONS
	// ---------------------------------------------------------------------

	"vision.weapon.detected": 0.90,
	"vision.weapon.knife":    0.60,
	"vision.weapon.firearm":  1.00,

	// ---------------------------------------------------------------------
	// CAMERA HEALTH
	// ---------------------------------------------------------------------

	"vision.camera.offline":  0.80,
	"vision.camera.occluded": 0.85,
	"vision.camera.tampered": 0.95,
	"vision.camera.blurred":  0.40,

	// ---------------------------------------------------------------------
	// LIGHTS
	// ---------------------------------------------------------------------

	"vision.light.on":      0.05,
	"vision.light.off":     0.05,
	"vision.light.changed": 0.10,

	// ---------------------------------------------------------------------
	// TRACKING
	// ---------------------------------------------------------------------

	"vision.track.started": 0.05,
	"vision.track.lost":    0.50,
}

func Score(
	eventType string,
) float64 {

	score, ok :=
		GuidelineScores[eventType]

	if !ok {
		return 0.50
	}

	return score
}