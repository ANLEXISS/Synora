package topology

import (
	"encoding/json"
	"regexp"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
	"synora/pkg/contract"
)

var residentIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)

type Presence struct {
	ResidentID string  `json:"resident_id"`
	Location   string  `json:"location"`
	LastSeen   int64   `json:"last_seen"`
	Confidence float64 `json:"confidence"`
}

type Resident struct {
	ID          string `json:"id" yaml:"id"`
	Name        string `json:"name" yaml:"name"`
	DisplayName string `json:"display_name,omitempty" yaml:"display_name,omitempty"`
	Role        string `json:"role" yaml:"role"`
	Admin       bool   `json:"admin" yaml:"admin"`
	Enabled     bool   `json:"enabled" yaml:"enabled"`
	Trusted     bool   `json:"trusted" yaml:"trusted"`

	Contact         contract.Contact         `json:"contact,omitempty" yaml:"contact,omitempty"`
	Baseline        contract.Baseline        `json:"baseline,omitempty" yaml:"baseline,omitempty"`
	PresenceProfile map[string]any           `json:"presence_profile,omitempty" yaml:"presence_profile,omitempty"`
	IdentityProfile contract.IdentityProfile `json:"identity_profile,omitempty" yaml:"identity_profile,omitempty"`
	Permissions     map[string]any           `json:"permissions,omitempty" yaml:"permissions,omitempty"`
	Metadata        map[string]any           `json:"metadata,omitempty" yaml:"metadata,omitempty"`
	CreatedAt       time.Time                `json:"created_at,omitempty" yaml:"created_at,omitempty"`
	UpdatedAt       time.Time                `json:"updated_at,omitempty" yaml:"updated_at,omitempty"`
	DeletedAt       *time.Time               `json:"deleted_at,omitempty" yaml:"deleted_at,omitempty"`

	Presence *Presence      `json:"presence,omitempty" yaml:"-"`
	Extra    map[string]any `json:"-" yaml:",inline"`

	enabledSet bool
	trustedSet bool
}

func (r *Resident) UnmarshalYAML(value *yaml.Node) error {
	type plain Resident
	raw := plain{Enabled: true}
	if err := value.Decode(&raw); err != nil {
		return err
	}
	*r = Resident(raw)
	r.enabledSet = yamlMappingHasKey(value, "enabled")
	r.trustedSet = yamlMappingHasKey(value, "trusted")
	normalizeResident(r)
	return nil
}

func (r *Resident) UnmarshalJSON(data []byte) error {
	type plain Resident
	raw := plain{Enabled: true}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	*r = Resident(raw)
	_, r.enabledSet = fields["enabled"]
	_, r.trustedSet = fields["trusted"]
	normalizeResident(r)
	return nil
}

func (r Resident) ConfigView() contract.ResidentView {
	value := cloneResident(r)
	return contract.Resident{
		ID: value.ID, Name: value.Name, DisplayName: value.DisplayName,
		Role: value.Role, Admin: value.Admin, Enabled: value.Enabled, Trusted: value.Trusted,
		Contact: value.Contact, Baseline: value.Baseline,
		PresenceProfile: cloneAnyMap(value.PresenceProfile), IdentityProfile: value.IdentityProfile,
		Permissions: cloneAnyMap(value.Permissions), Metadata: sanitizeResidentMap(value.Metadata),
		CreatedAt: value.CreatedAt, UpdatedAt: value.UpdatedAt, DeletedAt: cloneResidentTime(value.DeletedAt),
	}
}

func (r Resident) PublicView() contract.ResidentPublicView {
	return contract.ResidentPublicView{
		ID: r.ID, Name: r.Name, DisplayName: r.DisplayName, Role: r.Role,
		Admin: r.Admin, Enabled: r.Enabled, Trusted: r.Trusted,
		Metadata: sanitizeResidentMap(r.Metadata), CreatedAt: r.CreatedAt,
		UpdatedAt: r.UpdatedAt, DeletedAt: cloneResidentTime(r.DeletedAt),
	}
}

func ValidateResident(value Resident) error {
	if !residentIDPattern.MatchString(value.ID) {
		return contract.NewAPIError(contract.ErrorValidationFailed, "resident id is required and must be a stable identifier")
	}
	if strings.TrimSpace(value.Name) == "" {
		return contract.NewAPIError(contract.ErrorValidationFailed, "resident name is required")
	}
	switch value.Role {
	case contract.ResidentRoleOwner, contract.ResidentRoleResident, contract.ResidentRoleChild,
		contract.ResidentRoleGuest, contract.ResidentRoleCaregiver:
	default:
		return contract.NewAPIError(contract.ErrorValidationFailed, "unsupported resident role %q", value.Role)
	}
	return nil
}

func normalizeResident(value *Resident) {
	if value == nil {
		return
	}
	value.ID = strings.TrimSpace(value.ID)
	value.Name = strings.TrimSpace(value.Name)
	value.DisplayName = strings.TrimSpace(value.DisplayName)
	value.Role = strings.ToLower(strings.TrimSpace(value.Role))
	if value.Name == "" {
		value.Name = value.ID
	}
	if value.DisplayName == "" {
		value.DisplayName = value.Name
	}
	if value.Role == "" {
		value.Role = contract.ResidentRoleResident
	}
	if !value.enabledSet {
		value.Enabled = true
	}
	if !value.trustedSet {
		value.Trusted = value.Role == contract.ResidentRoleOwner || value.Role == contract.ResidentRoleResident
	}
	value.IdentityProfile.FaceIDs = normalizeStrings(value.IdentityProfile.FaceIDs)
	value.IdentityProfile.VoiceIDs = normalizeStrings(value.IdentityProfile.VoiceIDs)
	value.IdentityProfile.Aliases = normalizeStrings(value.IdentityProfile.Aliases)
}

func applyResidentPatch(value *Resident, patch contract.ResidentPatch) {
	if patch.Name != nil {
		value.Name = *patch.Name
	}
	if patch.DisplayName != nil {
		value.DisplayName = *patch.DisplayName
	}
	if patch.Role != nil {
		value.Role = *patch.Role
	}
	if patch.Admin != nil {
		value.Admin = *patch.Admin
	}
	if patch.Enabled != nil {
		value.Enabled = *patch.Enabled
		value.enabledSet = true
		if value.Enabled {
			value.DeletedAt = nil
		}
	}
	if patch.Trusted != nil {
		value.Trusted = *patch.Trusted
		value.trustedSet = true
	}
	// Identity data is replaced only when the field is explicitly present.
	if patch.Contact != nil {
		value.Contact = *patch.Contact
	}
	if patch.Baseline != nil {
		value.Baseline = cloneBaseline(*patch.Baseline)
	}
	if patch.PresenceProfile != nil {
		value.PresenceProfile = cloneAnyMap(*patch.PresenceProfile)
	}
	if patch.IdentityProfile != nil {
		value.IdentityProfile = cloneIdentityProfile(*patch.IdentityProfile)
	}
	if patch.Permissions != nil {
		value.Permissions = cloneAnyMap(*patch.Permissions)
	}
	if patch.Metadata != nil {
		value.Metadata = cloneAnyMap(*patch.Metadata)
	}
	normalizeResident(value)
}

func cloneResident(value Resident) Resident {
	value.Baseline = cloneBaseline(value.Baseline)
	value.PresenceProfile = cloneAnyMap(value.PresenceProfile)
	value.IdentityProfile = cloneIdentityProfile(value.IdentityProfile)
	value.Permissions = cloneAnyMap(value.Permissions)
	value.Metadata = cloneAnyMap(value.Metadata)
	value.Extra = cloneAnyMap(value.Extra)
	value.DeletedAt = cloneResidentTime(value.DeletedAt)
	if value.Presence != nil {
		presence := *value.Presence
		value.Presence = &presence
	}
	return value
}

func cloneIdentityProfile(value contract.IdentityProfile) contract.IdentityProfile {
	value.FaceIDs = append([]string(nil), value.FaceIDs...)
	value.VoiceIDs = append([]string(nil), value.VoiceIDs...)
	value.Aliases = append([]string(nil), value.Aliases...)
	return value
}

func cloneBaseline(value contract.Baseline) contract.Baseline {
	if value.Rooms != nil {
		rooms := make(map[string]float64, len(value.Rooms))
		for key, score := range value.Rooms {
			rooms[key] = score
		}
		value.Rooms = rooms
	}
	return value
}

func cloneAnyMap(in map[string]any) map[string]any {
	if in == nil {
		return nil
	}
	out := make(map[string]any, len(in))
	for key, value := range in {
		switch typed := value.(type) {
		case map[string]any:
			out[key] = cloneAnyMap(typed)
		case []string:
			out[key] = append([]string(nil), typed...)
		case []any:
			items := make([]any, len(typed))
			for i := range typed {
				items[i] = cloneResidentValue(typed[i])
			}
			out[key] = items
		default:
			out[key] = value
		}
	}
	return out
}

func sanitizeResidentMap(in map[string]any) map[string]any {
	if in == nil {
		return nil
	}
	out := make(map[string]any, len(in))
	for key, value := range in {
		lower := strings.ToLower(key)
		if strings.Contains(lower, "secret") || strings.Contains(lower, "token") ||
			strings.Contains(lower, "password") || strings.Contains(lower, "biometric") {
			continue
		}
		out[key] = sanitizeResidentValue(value)
	}
	return out
}

func sanitizeResidentValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		return sanitizeResidentMap(typed)
	case []any:
		out := make([]any, len(typed))
		for i := range typed {
			out[i] = sanitizeResidentValue(typed[i])
		}
		return out
	default:
		return value
	}
}

func cloneResidentValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		return cloneAnyMap(typed)
	case []any:
		out := make([]any, len(typed))
		for i := range typed {
			out[i] = cloneResidentValue(typed[i])
		}
		return out
	case []string:
		return append([]string(nil), typed...)
	default:
		return value
	}
}

func normalizeStrings(values []string) []string {
	if values == nil {
		return nil
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func cloneResidentTime(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

func yamlMappingHasKey(value *yaml.Node, key string) bool {
	if value == nil || value.Kind != yaml.MappingNode {
		return false
	}
	for i := 0; i+1 < len(value.Content); i += 2 {
		if value.Content[i].Value == key {
			return true
		}
	}
	return false
}
