package main

import (
	"fmt"
	"net/http"
	"sort"
	"strings"
	"unicode"

	"synora/pkg/contract"
)

type automationCatalogResponse struct {
	ConditionKinds []automationConditionKind  `json:"condition_kinds"`
	ActionKinds    []automationActionKind     `json:"action_kinds"`
	ActionCommands map[string][]catalogOption `json:"action_commands"`
	Targets        automationTargets          `json:"targets"`
}

type automationConditionKind struct {
	Kind        string          `json:"kind"`
	Label       string          `json:"label"`
	Description string          `json:"description"`
	Operators   []catalogOption `json:"operators"`
	Values      []catalogOption `json:"values"`
}

type automationActionKind struct {
	Kind        string `json:"kind"`
	Label       string `json:"label"`
	Description string `json:"description"`
}

type automationTargets struct {
	Devices []catalogOption `json:"devices"`
	Cameras []catalogOption `json:"cameras"`
	Sirens  []catalogOption `json:"sirens"`
	Notify  []catalogOption `json:"notify"`
}

type catalogOption struct {
	Value    string `json:"value"`
	Label    string `json:"label"`
	Category string `json:"category,omitempty"`
}

func handleAutomationCatalog(core stateProvider) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requireMethod(w, r, http.MethodGet) {
			return
		}

		snapshot, err := core.State()
		if err != nil {
			writeError(w, err)
			return
		}

		writeJSON(w, http.StatusOK, buildAutomationCatalog(snapshot))
	}
}

func buildAutomationCatalog(snapshot *contract.PublicSnapshot) automationCatalogResponse {
	nodeOptions := automationNodeOptions(snapshot)
	deviceOptions := automationDeviceOptions(snapshot, "")
	cameraOptions := automationDeviceOptions(snapshot, contract.DeviceTypeCamera)
	sirenOptions := automationDeviceOptions(snapshot, contract.DeviceTypeSiren)
	if len(sirenOptions) == 0 {
		sirenOptions = []catalogOption{{Value: "siren_main", Label: "Sirène principale"}}
	}

	return automationCatalogResponse{
		ConditionKinds: []automationConditionKind{
			{
				Kind:        "event.type",
				Label:       "Événement",
				Description: "Type d’événement détecté par Synora.",
				Operators:   automationOperators("==", "!="),
				Values:      automationEventTypeOptions(),
			},
			{
				Kind:        "system.state",
				Label:       "État système",
				Description: "État global de sécurité.",
				Operators:   automationOperators("==", "!="),
				Values:      automationSystemStateOptions(),
			},
			{
				Kind: "security.mode", Label: "Mode sécurité", Description: "Mode durable du système de sécurité.",
				Operators: automationOperators("==", "!="), Values: []catalogOption{
					{Value: "home", Label: "Maison"}, {Value: "night", Label: "Nuit"},
					{Value: "away", Label: "Absent"}, {Value: "high_security", Label: "Sécurité élevée"},
				},
			},
			{
				Kind: "security.armed", Label: "Sécurité armée", Description: "Indique si le mode de sécurité est armé.",
				Operators: automationOperators("==", "!="), Values: []catalogOption{{Value: "true", Label: "Armée"}, {Value: "false", Label: "Désarmée"}},
			},
			{
				Kind: "occupancy.expected", Label: "Occupation attendue", Description: "Présence attendue selon le mode de sécurité.",
				Operators: automationOperators("==", "!="), Values: []catalogOption{{Value: "unknown", Label: "Inconnue"}, {Value: "occupied", Label: "Présence attendue"}, {Value: "empty", Label: "Personne attendue"}},
			},
			{
				Kind: "manual_risk.active", Label: "Risque manuel actif", Description: "Indique si un risque temporaire a été injecté manuellement.",
				Operators: automationOperators("==", "!="), Values: []catalogOption{{Value: "true", Label: "Actif"}, {Value: "false", Label: "Inactif"}},
			},
			{
				Kind:        "node.id",
				Label:       "Pièce",
				Description: "Pièce ou zone concernée.",
				Operators:   automationOperators("==", "!="),
				Values:      nodeOptions,
			},
			{
				Kind:        "danger.level",
				Label:       "Niveau de danger",
				Description: "Niveau de risque estimé.",
				Operators:   automationOperators("==", "!=", ">", ">=", "<", "<="),
				Values:      automationDangerLevelOptions(),
			},
			{
				Kind:        "device.id",
				Label:       "Périphérique",
				Description: "Périphérique concerné.",
				Operators:   automationOperators("==", "!="),
				Values:      deviceOptions,
			},
		},
		ActionKinds: []automationActionKind{
			{Kind: "device.command", Label: "Commander un périphérique", Description: "Allumer, éteindre ou activer un périphérique."},
			{Kind: "record.clip", Label: "Enregistrer un clip", Description: "Créer un clip vidéo à partir d’une caméra."},
			{Kind: "notify", Label: "Notifier", Description: "Envoyer une notification aux résidents ou aux administrateurs."},
			{Kind: "siren", Label: "Sirène", Description: "Préparer ou déclencher une sirène."},
		},
		ActionCommands: map[string][]catalogOption{
			"device.command": {
				{Value: "turn_on", Label: "Allumer"},
				{Value: "turn_off", Label: "Éteindre"},
				{Value: "toggle", Label: "Basculer"},
			},
			"record.clip": {
				{Value: "record_30s", Label: "Clip 30 secondes"},
				{Value: "record_60s", Label: "Clip 60 secondes"},
			},
			"notify": {
				{Value: "push", Label: "Notification push"},
				{Value: "critical_push", Label: "Notification critique"},
			},
			"siren": {
				{Value: "arm", Label: "Préparer"},
				{Value: "trigger", Label: "Déclencher"},
			},
		},
		Targets: automationTargets{
			Devices: deviceOptions,
			Cameras: cameraOptions,
			Sirens:  sirenOptions,
			Notify: []catalogOption{
				{Value: "owner", Label: "Propriétaire"},
				{Value: "all_residents", Label: "Tous les résidents"},
				{Value: "admins", Label: "Administrateurs"},
			},
		},
	}
}

func automationOperators(values ...string) []catalogOption {
	out := make([]catalogOption, 0, len(values))
	for _, value := range values {
		out = append(out, catalogOption{Value: value, Label: automationOperatorLabel(value)})
	}
	return out
}

func automationOperatorLabel(value string) string {
	switch value {
	case "==":
		return "est"
	case "!=":
		return "n’est pas"
	case ">":
		return "est supérieur à"
	case "<":
		return "est inférieur à"
	case ">=":
		return "est supérieur ou égal à"
	case "<=":
		return "est inférieur ou égal à"
	default:
		return value
	}
}

func automationEventTypeOptions() []catalogOption {
	return []catalogOption{
		{Value: "vision.unknown", Label: "Personne inconnue détectée", Category: "Vision"},
		{Value: "vision.identity", Label: "Résident reconnu", Category: "Vision"},
		{Value: "vision.uncertain", Label: "Identité incertaine", Category: "Vision"},
		{Value: "motion.detected", Label: "Mouvement détecté", Category: "Capteurs"},
		{Value: "camera.offline", Label: "Caméra hors ligne", Category: "Sécurité"},
		{Value: "camera.tampered", Label: "Caméra manipulée", Category: "Sécurité"},
		{Value: "weapon.detected", Label: "Arme détectée", Category: "Vision"},
		{Value: "fall.detected", Label: "Chute détectée", Category: "Capteurs"},
		{Value: "glass_break.detected", Label: "Bris de vitre détecté", Category: "Capteurs"},
		{Value: "device.offline", Label: "Périphérique hors ligne", Category: "Sécurité"},
	}
}

func automationSystemStateOptions() []catalogOption {
	return []catalogOption{
		{Value: "idle", Label: "Repos", Category: "Système"},
		{Value: "activity", Label: "Activité", Category: "Système"},
		{Value: "suspicious", Label: "Suspect", Category: "Système"},
		{Value: "intrusion", Label: "Intrusion", Category: "Système"},
		{Value: "break-in", Label: "Effraction", Category: "Système"},
	}
}

func automationDangerLevelOptions() []catalogOption {
	return []catalogOption{
		{Value: "low", Label: "Faible", Category: "Danger"},
		{Value: "medium", Label: "Moyen", Category: "Danger"},
		{Value: "medium_high", Label: "Moyen élevé", Category: "Danger"},
		{Value: "high", Label: "Élevé", Category: "Danger"},
		{Value: "critical", Label: "Critique", Category: "Danger"},
	}
}

func automationNodeOptions(snapshot *contract.PublicSnapshot) []catalogOption {
	if snapshot == nil || len(snapshot.Nodes) == 0 {
		return []catalogOption{}
	}

	out := make([]catalogOption, 0, len(snapshot.Nodes))
	for _, node := range snapshot.Nodes {
		id := stringValue(node["id"])
		if id == "" {
			continue
		}

		name := prettyCatalogLabel(stringValue(node["name"]))
		if name == "" {
			name = prettyCatalogLabel(id)
		}

		label := name
		if kind := strings.ToLower(stringValue(node["type"])); kind == "room" {
			label = roomCatalogLabel(id, name)
		}

		out = append(out, catalogOption{
			Value:    id,
			Label:    id + " → " + label,
			Category: fallbackCatalogCategory(prettyCatalogLabel(stringValue(node["type"])), "Maison"),
		})
	}

	sort.SliceStable(out, func(i, j int) bool {
		return out[i].Label < out[j].Label
	})
	return out
}

func automationDeviceOptions(snapshot *contract.PublicSnapshot, deviceType string) []catalogOption {
	if snapshot == nil || len(snapshot.Devices) == 0 {
		return []catalogOption{}
	}

	out := make([]catalogOption, 0, len(snapshot.Devices))
	for _, device := range snapshot.Devices {
		if deviceType != "" && !strings.EqualFold(stringValue(device["type"]), deviceType) {
			continue
		}
		id := stringValue(device["id"])
		if id == "" {
			continue
		}
		label := stringValue(device["name"])
		if label == "" {
			label = id
		}
		category := prettyCatalogLabel(stringValue(device["type"]))
		out = append(out, catalogOption{
			Value:    id,
			Label:    label,
			Category: category,
		})
	}

	sort.SliceStable(out, func(i, j int) bool {
		return strings.Compare(out[i].Label, out[j].Label) < 0
	})
	return out
}

func stringValue(value any) string {
	if value == nil {
		return ""
	}
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	case []byte:
		return strings.TrimSpace(string(typed))
	default:
		trimmed := strings.TrimSpace(fmt.Sprint(value))
		if trimmed == "<nil>" {
			return ""
		}
		return trimmed
	}
}

func mapSlice(value any) []map[string]any {
	switch typed := value.(type) {
	case []map[string]any:
		return typed
	case []any:
		out := make([]map[string]any, 0, len(typed))
		for _, item := range typed {
			if mapped, ok := item.(map[string]any); ok {
				out = append(out, mapped)
			}
		}
		return out
	default:
		return nil
	}
}

func prettyCatalogLabel(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	value = strings.ReplaceAll(value, "_", " ")
	parts := strings.Fields(value)
	for i, part := range parts {
		runes := []rune(part)
		if len(runes) == 0 {
			continue
		}
		runes[0] = unicode.ToUpper(runes[0])
		parts[i] = string(runes)
	}
	return strings.Join(parts, " ")
}

func fallbackCatalogCategory(primary string, fallback string) string {
	if strings.TrimSpace(primary) != "" {
		return primary
	}
	return fallback
}

func roomCatalogLabel(id string, name string) string {
	if strings.TrimSpace(id) == "" {
		return name
	}
	parts := strings.Split(id, ".")
	if len(parts) < 2 {
		return name
	}
	floor := prettyCatalogLabel(parts[len(parts)-2])
	if floor == "" {
		return name
	}
	return name + " · " + floor
}
