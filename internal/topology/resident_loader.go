package topology

import (
	"os"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
	"synora/internal/configfile"
	"synora/pkg/contract"
)

type yamlResidents struct {
	Residents []Resident `yaml:"residents"`
}

func LoadResidents(path string) (map[string]*Resident, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var y yamlResidents
	if err := yaml.Unmarshal(data, &y); err != nil {
		return nil, err
	}
	out := make(map[string]*Resident, len(y.Residents))
	for i := range y.Residents {
		resident := cloneResident(y.Residents[i])
		normalizeResident(&resident)
		if err := ValidateResident(resident); err != nil {
			return nil, err
		}
		if _, exists := out[resident.ID]; exists {
			return nil, contract.NewAPIError(contract.ErrorDuplicateID, "duplicate resident id %q", resident.ID)
		}
		out[resident.ID] = &resident
	}
	return out, nil
}

func SaveResidents(path string, residents map[string]*Resident) error {
	ids := make([]string, 0, len(residents))
	for id := range residents {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	y := yamlResidents{Residents: make([]Resident, 0, len(ids))}
	for _, id := range ids {
		if residents[id] == nil {
			continue
		}
		resident := cloneResident(*residents[id])
		normalizeResident(&resident)
		if resident.ID != id {
			return contract.NewAPIError(contract.ErrorValidationFailed, "resident key %q does not match id %q", id, resident.ID)
		}
		if err := ValidateResident(resident); err != nil {
			return err
		}
		resident.Presence = nil
		y.Residents = append(y.Residents, resident)
	}
	data, err := yaml.Marshal(y)
	if err != nil {
		return err
	}
	return configfile.WriteAtomicWithBackup(path, data, 0o640)
}

func GetResident(residents map[string]*Resident, id string) (*Resident, bool) {
	value, ok := residents[strings.TrimSpace(id)]
	if !ok || value == nil {
		return nil, false
	}
	copy := cloneResident(*value)
	return &copy, true
}

// CreateResident, PatchResident and SoftDeleteResident are copy-on-write. The
// caller owns synchronization for the shared map (Core currently uses its
// snapshot mutex); a failed durable write leaves that map untouched.
func CreateResident(path string, residents map[string]*Resident, value Resident) (*Resident, error) {
	normalizeResident(&value)
	if err := ValidateResident(value); err != nil {
		return nil, err
	}
	if _, exists := residents[value.ID]; exists {
		return nil, contract.NewAPIError(contract.ErrorDuplicateID, "resident %q already exists", value.ID)
	}
	now := time.Now().UTC()
	if value.CreatedAt.IsZero() {
		value.CreatedAt = now
	}
	value.UpdatedAt = now
	staged := cloneResidents(residents)
	copy := cloneResident(value)
	staged[value.ID] = &copy
	if err := SaveResidents(path, staged); err != nil {
		return nil, err
	}
	committed := cloneResident(value)
	residents[value.ID] = &committed
	result := cloneResident(committed)
	return &result, nil
}

func PatchResident(path string, residents map[string]*Resident, id string, patch contract.ResidentPatch) (*Resident, error) {
	id = strings.TrimSpace(id)
	current, exists := residents[id]
	if !exists || current == nil {
		return nil, contract.NewAPIError(contract.ErrorNotFound, "resident %q not found", id)
	}
	updated := cloneResident(*current)
	applyResidentPatch(&updated, patch)
	updated.UpdatedAt = time.Now().UTC()
	if err := ValidateResident(updated); err != nil {
		return nil, err
	}
	staged := cloneResidents(residents)
	copy := cloneResident(updated)
	staged[id] = &copy
	if err := SaveResidents(path, staged); err != nil {
		return nil, err
	}
	committed := cloneResident(updated)
	residents[id] = &committed
	result := cloneResident(committed)
	return &result, nil
}

func SoftDeleteResident(path string, residents map[string]*Resident, id string) (*Resident, error) {
	id = strings.TrimSpace(id)
	current, exists := residents[id]
	if !exists || current == nil {
		return nil, contract.NewAPIError(contract.ErrorNotFound, "resident %q not found", id)
	}
	if current.DeletedAt != nil {
		copy := cloneResident(*current)
		return &copy, nil
	}
	updated := cloneResident(*current)
	now := time.Now().UTC()
	updated.Enabled = false
	updated.enabledSet = true
	updated.DeletedAt = &now
	updated.UpdatedAt = now
	staged := cloneResidents(residents)
	copy := cloneResident(updated)
	staged[id] = &copy
	if err := SaveResidents(path, staged); err != nil {
		return nil, err
	}
	committed := cloneResident(updated)
	residents[id] = &committed
	result := cloneResident(committed)
	return &result, nil
}

func cloneResidents(values map[string]*Resident) map[string]*Resident {
	out := make(map[string]*Resident, len(values))
	for id, value := range values {
		if value == nil {
			continue
		}
		copy := cloneResident(*value)
		out[id] = &copy
	}
	return out
}
