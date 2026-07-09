package simulation

import "synora/pkg/contract"

func ListScenarios() []Scenario {
	scenarios := []Scenario{
		{
			ID:          "resident_enters_home",
			Name:        "Resident reconnu qui rentre a la maison",
			Description: "Simule un resident connu vu successivement par plusieurs cameras.",
			Steps: []ScenarioStep{
				{ID: "entry_identity", Label: "Alexis reconnu a l'entree", EventType: contract.EventVisionIdentity, DeviceID: "cam_01", CameraID: "cam_01", Identity: "alexis", Confidence: 0.92},
				{ID: "hall_motion", Label: "Mouvement dans le couloir", DelayMs: 600, EventType: contract.EventVisionMotion, DeviceID: "cam_02", CameraID: "cam_02"},
				{ID: "inside_identity", Label: "Alexis reconnu plus loin", DelayMs: 600, EventType: contract.EventVisionIdentity, DeviceID: "cam_03", CameraID: "cam_03", Identity: "alexis", Confidence: 0.90},
			},
		},
		{
			ID:          "unknown_at_entrance",
			Name:        "Inconnu detecte a l'entree",
			Description: "Simule un inconnu qui reste proche de l'entree.",
			Steps: []ScenarioStep{
				{ID: "unknown_first", Label: "Premier inconnu", EventType: contract.EventVisionUnknown, DeviceID: "cam_01", CameraID: "cam_01", Confidence: 0.70},
				{ID: "entry_motion", Label: "Mouvement a l'entree", DelayMs: 500, EventType: contract.EventVisionMotion, DeviceID: "cam_01", CameraID: "cam_01"},
				{ID: "unknown_confirmed", Label: "Inconnu confirme", DelayMs: 500, EventType: contract.EventVisionUnknown, DeviceID: "cam_01", CameraID: "cam_01", Confidence: 0.74},
			},
		},
		{
			ID:          "camera_offline",
			Name:        "Camera deconnectee",
			Description: "Simule une camera puis son device declares hors ligne.",
			Steps: []ScenarioStep{
				{ID: "camera_offline", Label: "Camera offline", EventType: contract.EventDiscoveryCameraOffline, DeviceID: "cam_01", CameraID: "cam_01"},
				{ID: "device_offline", Label: "Device offline", DelayMs: 500, EventType: contract.EventDeviceOffline, DeviceID: "cam_01", CameraID: "cam_01"},
			},
		},
		{
			ID:          "fall_detected",
			Name:        "Chute detectee",
			Description: "Simule une chute detectee par une camera.",
			Steps: []ScenarioStep{
				{ID: "fall", Label: "Chute detectee", EventType: contract.EventVisionFall, DeviceID: "cam_03", CameraID: "cam_03", Confidence: 0.84},
			},
		},
		{
			ID:          "weapon_detected",
			Name:        "Menace detectee",
			Description: "Simule un inconnu suivi d'une detection de menace.",
			Steps: []ScenarioStep{
				{ID: "unknown_before_weapon", Label: "Inconnu avant menace", EventType: contract.EventVisionUnknown, DeviceID: "cam_01", CameraID: "cam_01", Confidence: 0.72},
				{ID: "weapon", Label: "Menace detectee", DelayMs: 600, EventType: contract.EventVisionWeapon, DeviceID: "cam_01", CameraID: "cam_01", Confidence: 0.88},
			},
		},
		{
			ID:          "uncertain_identity",
			Name:        "Identite incertaine a valider",
			Description: "Simule une identite connue puis une detection incertaine a valider.",
			Steps: []ScenarioStep{
				{ID: "known_identity", Label: "Alexis reconnu", EventType: contract.EventVisionIdentity, DeviceID: "cam_01", CameraID: "cam_01", Identity: "alexis", Confidence: 0.94},
				{ID: "uncertain_match", Label: "Identite incertaine", DelayMs: 2500, EventType: contract.EventVisionUncertain, DeviceID: "cam_05", CameraID: "cam_05", Identity: "alexis", Confidence: 0.60},
			},
		},
	}
	return scenarios
}

func ScenarioByID(id string) (Scenario, bool) {
	for _, scenario := range ListScenarios() {
		if scenario.ID == id {
			return scenario, true
		}
	}
	return Scenario{}, false
}
