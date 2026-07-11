package topology

import (
	"encoding/json"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
	"synora/pkg/contract"
)

var residentIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,127}$`)

type Presence struct {
	ResidentID string  `json:"resident_id"`
	Location   string  `json:"location"`
	LastSeen   int64   `json:"last_seen"`
	Confidence float64 `json:"confidence"`
}

type Resident struct {
	ID              string               `json:"id" yaml:"id"`
	Name            string               `json:"name" yaml:"name"`
	FirstName       string               `json:"first_name,omitempty" yaml:"first_name,omitempty"`
	LastName        string               `json:"last_name,omitempty" yaml:"last_name,omitempty"`
	DisplayName     string               `json:"display_name,omitempty" yaml:"display_name,omitempty"`
	Role            string               `json:"role" yaml:"role"`
	Admin           bool                 `json:"admin" yaml:"admin"`
	Enabled         bool                 `json:"enabled" yaml:"enabled"`
	Trusted         bool                 `json:"trusted" yaml:"trusted"`
	ReferenceNodeID string               `json:"reference_node_id,omitempty" yaml:"reference_node_id,omitempty"`
	AccountID       string               `json:"account_id,omitempty" yaml:"account_id,omitempty"`
	FaceProfile     contract.FaceProfile `json:"face_profile,omitempty" yaml:"face_profile,omitempty"`

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
		ID: value.ID, Name: value.Name, FirstName: value.FirstName, LastName: value.LastName, DisplayName: value.DisplayName,
		Role: value.Role, Admin: value.Admin, Enabled: value.Enabled, Trusted: value.Trusted,
		ReferenceNodeID: value.ReferenceNodeID, AccountID: value.AccountID, FaceProfile: cloneFaceProfile(value.FaceProfile),
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
	if len(value.FaceProfile.BasePhotos) > 4 {
		return contract.NewAPIError(contract.ErrorValidationFailed, "a resident may have at most 4 base face photos")
	}
	for _, photo := range value.FaceProfile.BasePhotos {
		if !safeFaceMetadataPart(photo.ID) || !safeFaceMetadataPart(photo.Filename) || photo.Path == "" || filepath.Base(photo.Path) != photo.Filename {
			return contract.NewAPIError(contract.ErrorValidationFailed, "invalid face photo metadata")
		}
	}
	return nil
}

func normalizeResident(value *Resident) {
	if value == nil {
		return
	}
	value.ID = strings.TrimSpace(value.ID)
	value.Name = strings.TrimSpace(value.Name)
	value.FirstName = strings.TrimSpace(value.FirstName)
	value.LastName = strings.TrimSpace(value.LastName)
	value.DisplayName = strings.TrimSpace(value.DisplayName)
	value.ReferenceNodeID = strings.TrimSpace(value.ReferenceNodeID)
	value.AccountID = strings.TrimSpace(value.AccountID)
	value.Role = strings.ToLower(strings.TrimSpace(value.Role))
	if value.Name == "" {
		value.Name = strings.TrimSpace(strings.Join([]string{value.FirstName, value.LastName}, " "))
		if value.Name == "" {
			value.Name = value.ID
		}
	}
	if value.DisplayName == "" {
		fullName := strings.TrimSpace(strings.Join([]string{value.FirstName, value.LastName}, " "))
		if fullName != "" {
			value.DisplayName = fullName
		} else {
			value.DisplayName = value.Name
		}
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
	normalizeFaceProfile(&value.FaceProfile)
}

func applyResidentPatch(value *Resident, patch contract.ResidentPatch) {
	if patch.Name != nil {
		value.Name = *patch.Name
	}
	if patch.FirstName != nil {
		value.FirstName = *patch.FirstName
	}
	if patch.LastName != nil {
		value.LastName = *patch.LastName
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
	if patch.ReferenceNodeID != nil {
		value.ReferenceNodeID = *patch.ReferenceNodeID
	}
	if patch.AccountID != nil {
		value.AccountID = *patch.AccountID
	}
	if patch.FaceProfile != nil {
		value.FaceProfile = cloneFaceProfile(*patch.FaceProfile)
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
	value.FaceProfile = cloneFaceProfile(value.FaceProfile)
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

func cloneFaceProfile(value contract.FaceProfile) contract.FaceProfile {
	value.BasePhotos = append([]contract.FacePhoto(nil), value.BasePhotos...)
	return value
}

func normalizeFaceProfile(value *contract.FaceProfile) {
	if value == nil {
		return
	}
	switch value.Status {
	case "empty", "ready", "needs_rebuild", "error":
	default:
		value.Status = "empty"
	}
	if len(value.BasePhotos) == 0 && value.Status == "ready" {
		value.Status = "empty"
	}
	if len(value.BasePhotos) > 0 && value.Status == "empty" {
		value.Status = "needs_rebuild"
	}
	if value.PendingCount < 0 {
		value.PendingCount = 0
	}
	if value.AutoCount < 0 {
		value.AutoCount = 0
	}
	if value.ReviewCount < 0 {
		value.ReviewCount = 0
	}
	value.PendingCount = value.ReviewCount
}

func safeFaceMetadataPart(value string) bool {
	value = strings.TrimSpace(value)
	return value != "" && value != "." && value != ".." && filepath.Base(value) == value && !strings.ContainsAny(value, `/\\`)
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
