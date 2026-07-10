package device

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
	"synora/internal/configfile"
	"synora/pkg/contract"
)

const UnlocatedNodeID = "unlocated"

var (
	identifierPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)
	deviceTypes       = map[string]struct{}{
		contract.DeviceTypeCamera: {}, contract.DeviceTypeLight: {},
		contract.DeviceTypeSensor: {}, contract.DeviceTypeSiren: {},
		contract.DeviceTypeSpeaker: {}, contract.DeviceTypeLock: {},
		contract.DeviceTypeBridge: {}, contract.DeviceTypeUnknown: {},
		// Kept for the installed legacy configuration. New clients should use
		// speaker with an explicit role.
		"voice_relay": {},
	}
)

// Device is Core's durable representation. Secret and Extra preserve installed
// YAML fields but can never be emitted through JSON or PublicView.
type Device struct {
	ID           string         `json:"id" yaml:"id"`
	Name         string         `json:"name,omitempty" yaml:"name,omitempty"`
	Type         string         `json:"type" yaml:"type"`
	Role         string         `json:"role,omitempty" yaml:"role,omitempty"`
	Room         string         `json:"room,omitempty" yaml:"room,omitempty"` // legacy node id
	NodeID       string         `json:"node_id,omitempty" yaml:"node_id,omitempty"`
	ZoneRole     string         `json:"zone_role,omitempty" yaml:"zone_role,omitempty"`
	RoomName     string         `json:"room_name,omitempty" yaml:"room_name,omitempty"`
	Enabled      bool           `json:"enabled" yaml:"enabled"`
	Trusted      bool           `json:"trusted" yaml:"trusted"`
	Capabilities []string       `json:"capabilities" yaml:"capabilities"`
	Config       map[string]any `json:"config,omitempty" yaml:"config,omitempty"`
	Metadata     map[string]any `json:"metadata,omitempty" yaml:"metadata,omitempty"`
	CreatedAt    time.Time      `json:"created_at,omitempty" yaml:"created_at,omitempty"`
	UpdatedAt    time.Time      `json:"updated_at,omitempty" yaml:"updated_at,omitempty"`
	DeletedAt    *time.Time     `json:"deleted_at,omitempty" yaml:"deleted_at,omitempty"`

	Secret   string         `json:"-" yaml:"secret,omitempty"`
	Network  map[string]any `json:"-" yaml:"network,omitempty"`
	Firmware map[string]any `json:"-" yaml:"firmware,omitempty"`
	Extra    map[string]any `json:"-" yaml:",inline"`

	enabledSet bool
	trustedSet bool
}

type DeviceConfig = Device
type DevicePatch = contract.DevicePatch

func (d *Device) UnmarshalYAML(value *yaml.Node) error {
	type plain Device
	raw := plain{Enabled: true, Trusted: true}
	if err := value.Decode(&raw); err != nil {
		return err
	}
	*d = Device(raw)
	d.enabledSet = mappingHasKey(value, "enabled")
	d.trustedSet = mappingHasKey(value, "trusted")
	return nil
}

func (d *Device) UnmarshalJSON(data []byte) error {
	type plain Device
	raw := plain{Enabled: true, Trusted: true}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	*d = Device(raw)
	_, d.enabledSet = fields["enabled"]
	_, d.trustedSet = fields["trusted"]
	return nil
}

func (d Device) PublicView() contract.DeviceView {
	value := cloneDevice(d)
	return contract.DeviceView{
		ID: value.ID, Name: value.Name, Type: value.Type, Role: value.Role,
		NodeID: value.NodeID, ZoneRole: value.ZoneRole, RoomName: value.RoomName,
		Enabled: value.Enabled, Trusted: value.Trusted,
		Capabilities: append([]string(nil), value.Capabilities...),
		Config:       sanitizePublicMap(value.Config), Metadata: sanitizePublicMap(value.Metadata),
		CreatedAt: value.CreatedAt, UpdatedAt: value.UpdatedAt, DeletedAt: cloneTime(value.DeletedAt),
	}
}

type Registry struct {
	mu      sync.RWMutex
	devices map[string]*Device
	path    string
	now     func() time.Time
}

func NewRegistry(paths ...string) *Registry {
	path := ""
	if len(paths) > 0 {
		path = strings.TrimSpace(paths[0])
	}
	return &Registry{
		devices: make(map[string]*Device),
		path:    path,
		now:     func() time.Time { return time.Now().UTC() },
	}
}

func (r *Registry) SetPersistencePath(path string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.path = strings.TrimSpace(path)
}

func (r *Registry) Register(configs []DeviceConfig) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, cfg := range configs {
		normalizeDevice(&cfg)
		if cfg.ID == "" {
			continue
		}
		copy := cloneDevice(cfg)
		r.devices[cfg.ID] = &copy
	}
}

func (r *Registry) Replace(configs []DeviceConfig) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.replaceLocked(configs)
}

func (r *Registry) replaceLocked(configs []DeviceConfig) {
	next := make(map[string]*Device, len(configs))
	for _, cfg := range configs {
		normalizeDevice(&cfg)
		if cfg.ID == "" {
			continue
		}
		copy := cloneDevice(cfg)
		next[cfg.ID] = &copy
	}
	r.devices = next
}

func (r *Registry) Get(id string) (*Device, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	d, ok := r.devices[id]
	if !ok || d == nil {
		return nil, false
	}
	copy := cloneDevice(*d)
	return &copy, true
}

func (r *Registry) List() map[string]*Device {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make(map[string]*Device, len(r.devices))
	for id, d := range r.devices {
		if d == nil {
			continue
		}
		copy := cloneDevice(*d)
		out[id] = &copy
	}
	return out
}

func (r *Registry) Ordered() []DeviceConfig {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.orderedLocked()
}

func (r *Registry) orderedLocked() []DeviceConfig {
	keys := make([]string, 0, len(r.devices))
	for id := range r.devices {
		keys = append(keys, id)
	}
	sort.Strings(keys)
	out := make([]DeviceConfig, 0, len(keys))
	for _, id := range keys {
		if r.devices[id] != nil {
			out = append(out, cloneDevice(*r.devices[id]))
		}
	}
	return out
}

func (r *Registry) PublicViews() []contract.DeviceView {
	items := r.Ordered()
	out := make([]contract.DeviceView, 0, len(items))
	for _, item := range items {
		out = append(out, item.PublicView())
	}
	return out
}

// Create validates and persists the complete device configuration before
// publishing it in the live registry.
func (r *Registry) Create(value DeviceConfig) (*Device, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	value.ID = strings.TrimSpace(value.ID)
	if _, exists := r.devices[value.ID]; exists {
		return nil, contract.NewAPIError(contract.ErrorDuplicateID, "device %q already exists", value.ID)
	}
	normalizeDevice(&value)
	if err := Validate(value); err != nil {
		return nil, err
	}
	now := r.timestamp()
	if value.CreatedAt.IsZero() {
		value.CreatedAt = now
	}
	value.UpdatedAt = now
	staged := append(r.orderedLocked(), cloneDevice(value))
	if err := r.saveLocked(staged); err != nil {
		return nil, err
	}
	r.replaceLocked(staged)
	created := cloneDevice(value)
	return &created, nil
}

func (r *Registry) Patch(id string, patch DevicePatch) (*Device, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	id = strings.TrimSpace(id)
	current, ok := r.devices[id]
	if !ok || current == nil {
		return nil, contract.NewAPIError(contract.ErrorNotFound, "device %q not found", id)
	}
	updated := cloneDevice(*current)
	applyPatch(&updated, patch)
	updated.UpdatedAt = r.timestamp()
	if err := Validate(updated); err != nil {
		return nil, err
	}
	staged := r.orderedLocked()
	for i := range staged {
		if staged[i].ID == id {
			staged[i] = cloneDevice(updated)
		}
	}
	if err := r.saveLocked(staged); err != nil {
		return nil, err
	}
	r.replaceLocked(staged)
	result := cloneDevice(updated)
	return &result, nil
}

func (r *Registry) SoftDelete(id string) (*Device, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	id = strings.TrimSpace(id)
	current, ok := r.devices[id]
	if !ok || current == nil {
		return nil, contract.NewAPIError(contract.ErrorNotFound, "device %q not found", id)
	}
	if current.DeletedAt != nil {
		copy := cloneDevice(*current)
		return &copy, nil
	}
	updated := cloneDevice(*current)
	now := r.timestamp()
	updated.Enabled = false
	updated.enabledSet = true
	updated.DeletedAt = &now
	updated.UpdatedAt = now
	staged := r.orderedLocked()
	for i := range staged {
		if staged[i].ID == id {
			staged[i] = updated
		}
	}
	if err := r.saveLocked(staged); err != nil {
		return nil, err
	}
	r.replaceLocked(staged)
	copy := cloneDevice(updated)
	return &copy, nil
}

// MoveMissingNodesToUnlocated applies all topology detachments in one durable
// replacement. The special unlocated id is always valid.
func (r *Registry) MoveMissingNodesToUnlocated(valid map[string]bool) ([]Device, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	staged := r.orderedLocked()
	changed := false
	now := r.timestamp()
	for i := range staged {
		nodeID := strings.TrimSpace(staged[i].NodeID)
		if nodeID == "" || nodeID == UnlocatedNodeID || valid[nodeID] {
			continue
		}
		staged[i].NodeID = UnlocatedNodeID
		staged[i].Room = UnlocatedNodeID
		staged[i].UpdatedAt = now
		changed = true
	}
	if changed {
		if err := r.saveLocked(staged); err != nil {
			return nil, err
		}
		r.replaceLocked(staged)
	}
	out := make([]Device, len(staged))
	for i := range staged {
		out[i] = cloneDevice(staged[i])
	}
	return out, nil
}

// Delete and Update are retained for runtime callers that intentionally make
// an in-memory change. Public configuration endpoints should use SoftDelete and
// Patch so durability and rollback are guaranteed.
func (r *Registry) Delete(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.devices, id)
}

func (r *Registry) Update(id string, cb func(*Device)) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	d, ok := r.devices[id]
	if !ok || d == nil {
		return false
	}
	cb(d)
	normalizeDevice(d)
	return true
}

func (r *Registry) saveLocked(configs []DeviceConfig) error {
	if strings.TrimSpace(r.path) == "" {
		return contract.NewAPIError(contract.ErrorInternal, "device persistence path is not configured")
	}
	return Save(r.path, configs)
}

func (r *Registry) timestamp() time.Time {
	if r.now != nil {
		return r.now().UTC()
	}
	return time.Now().UTC()
}

type fileDevices struct {
	Devices []DeviceConfig `yaml:"devices"`
}

func Load(path string) ([]DeviceConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var wrapped fileDevices
	if err := yaml.Unmarshal(data, &wrapped); err == nil && yamlHasRootKey(data, "devices") {
		if err := normalizeAndValidate(wrapped.Devices); err != nil {
			return nil, err
		}
		return wrapped.Devices, nil
	}

	var list []DeviceConfig
	if err := yaml.Unmarshal(data, &list); err == nil && list != nil {
		if err := normalizeAndValidate(list); err != nil {
			return nil, err
		}
		return list, nil
	}

	var mapped map[string]DeviceConfig
	if err := yaml.Unmarshal(data, &mapped); err == nil && mapped != nil {
		keys := make([]string, 0, len(mapped))
		for id := range mapped {
			keys = append(keys, id)
		}
		sort.Strings(keys)
		out := make([]DeviceConfig, 0, len(mapped))
		for _, id := range keys {
			cfg := mapped[id]
			if cfg.ID == "" {
				cfg.ID = id
			}
			out = append(out, cfg)
		}
		if err := normalizeAndValidate(out); err != nil {
			return nil, err
		}
		return out, nil
	}

	return nil, fmt.Errorf("devices: unsupported devices yaml format")
}

func Save(path string, configs []DeviceConfig) error {
	cloned := make([]DeviceConfig, len(configs))
	for i := range configs {
		cloned[i] = cloneDevice(configs[i])
	}
	if err := normalizeAndValidate(cloned); err != nil {
		return err
	}
	sort.Slice(cloned, func(i, j int) bool { return cloned[i].ID < cloned[j].ID })
	data, err := yaml.Marshal(&fileDevices{Devices: cloned})
	if err != nil {
		return err
	}
	return configfile.WriteAtomicWithBackup(path, data, 0o640)
}

func Validate(value DeviceConfig) error {
	if !identifierPattern.MatchString(value.ID) {
		return contract.NewAPIError(contract.ErrorValidationFailed, "device id is required and must be a stable identifier")
	}
	if _, ok := deviceTypes[strings.ToLower(strings.TrimSpace(value.Type))]; !ok {
		return contract.NewAPIError(contract.ErrorValidationFailed, "unsupported device type %q", value.Type)
	}
	seen := make(map[string]struct{}, len(value.Capabilities))
	for _, capability := range value.Capabilities {
		capability = strings.TrimSpace(capability)
		if !identifierPattern.MatchString(capability) {
			return contract.NewAPIError(contract.ErrorValidationFailed, "invalid device capability %q", capability)
		}
		if _, duplicate := seen[capability]; duplicate {
			return contract.NewAPIError(contract.ErrorValidationFailed, "duplicate device capability %q", capability)
		}
		seen[capability] = struct{}{}
	}
	return nil
}

func normalizeAndValidate(items []DeviceConfig) error {
	seen := make(map[string]struct{}, len(items))
	for i := range items {
		normalizeDevice(&items[i])
		if _, duplicate := seen[items[i].ID]; duplicate {
			return contract.NewAPIError(contract.ErrorDuplicateID, "duplicate device id %q", items[i].ID)
		}
		seen[items[i].ID] = struct{}{}
		if err := Validate(items[i]); err != nil {
			return err
		}
	}
	return nil
}

func normalizeDevice(value *Device) {
	if value == nil {
		return
	}
	value.ID = strings.TrimSpace(value.ID)
	value.Name = strings.TrimSpace(value.Name)
	value.Type = strings.ToLower(strings.TrimSpace(value.Type))
	value.Role = strings.TrimSpace(value.Role)
	value.NodeID = strings.TrimSpace(value.NodeID)
	value.Room = strings.TrimSpace(value.Room)
	value.ZoneRole = strings.TrimSpace(value.ZoneRole)
	value.RoomName = strings.TrimSpace(value.RoomName)
	if value.NodeID == "" {
		value.NodeID = value.Room
	}
	if value.NodeID == "" {
		value.NodeID = UnlocatedNodeID
	}
	if value.Room == "" || value.Room != value.NodeID {
		value.Room = value.NodeID
	}
	if value.Name == "" {
		value.Name = value.ID
	}
	if !value.enabledSet {
		value.Enabled = true
	}
	if !value.trustedSet {
		value.Trusted = true
	}
	for i := range value.Capabilities {
		value.Capabilities[i] = strings.TrimSpace(value.Capabilities[i])
	}
	sort.Strings(value.Capabilities)
}

func applyPatch(value *Device, patch DevicePatch) {
	if patch.Name != nil {
		value.Name = strings.TrimSpace(*patch.Name)
	}
	if patch.Role != nil {
		value.Role = strings.TrimSpace(*patch.Role)
	}
	if patch.NodeID != nil {
		value.NodeID = strings.TrimSpace(*patch.NodeID)
		if value.NodeID == "" {
			value.NodeID = UnlocatedNodeID
		}
		value.Room = value.NodeID
	}
	if patch.ZoneRole != nil {
		value.ZoneRole = strings.TrimSpace(*patch.ZoneRole)
	}
	if patch.RoomName != nil {
		value.RoomName = strings.TrimSpace(*patch.RoomName)
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
	if patch.Capabilities != nil {
		value.Capabilities = append([]string(nil), (*patch.Capabilities)...)
	}
	if patch.Config != nil {
		value.Config = cloneMap(*patch.Config)
	}
	if patch.Metadata != nil {
		value.Metadata = cloneMap(*patch.Metadata)
	}
	normalizeDevice(value)
}

func cloneDevice(value Device) Device {
	value.Capabilities = append([]string(nil), value.Capabilities...)
	value.Config = cloneMap(value.Config)
	value.Metadata = cloneMap(value.Metadata)
	value.Network = cloneMap(value.Network)
	value.Firmware = cloneMap(value.Firmware)
	value.Extra = cloneMap(value.Extra)
	value.DeletedAt = cloneTime(value.DeletedAt)
	return value
}

func cloneMap(in map[string]any) map[string]any {
	if in == nil {
		return nil
	}
	out := make(map[string]any, len(in))
	for key, value := range in {
		switch typed := value.(type) {
		case map[string]any:
			out[key] = cloneMap(typed)
		case []string:
			out[key] = append([]string(nil), typed...)
		case []any:
			items := make([]any, len(typed))
			for i := range typed {
				items[i] = cloneValue(typed[i])
			}
			out[key] = items
		default:
			out[key] = value
		}
	}
	return out
}

func sanitizePublicMap(in map[string]any) map[string]any {
	if in == nil {
		return nil
	}
	out := make(map[string]any, len(in))
	for key, value := range in {
		lower := strings.ToLower(key)
		if strings.Contains(lower, "secret") || strings.Contains(lower, "token") ||
			strings.Contains(lower, "password") || strings.Contains(lower, "credential") ||
			strings.Contains(lower, "private_key") || strings.Contains(lower, "api_key") {
			continue
		}
		out[key] = sanitizePublicValue(value)
	}
	return out
}

func sanitizePublicValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		return sanitizePublicMap(typed)
	case []any:
		out := make([]any, len(typed))
		for i := range typed {
			out[i] = sanitizePublicValue(typed[i])
		}
		return out
	case []map[string]any:
		out := make([]map[string]any, len(typed))
		for i := range typed {
			out[i] = sanitizePublicMap(typed[i])
		}
		return out
	default:
		return value
	}
}

func cloneValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		return cloneMap(typed)
	case []any:
		out := make([]any, len(typed))
		for i := range typed {
			out[i] = cloneValue(typed[i])
		}
		return out
	case []string:
		return append([]string(nil), typed...)
	default:
		return value
	}
}

func cloneTime(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

func mappingHasKey(value *yaml.Node, key string) bool {
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

func yamlHasRootKey(data []byte, key string) bool {
	var root yaml.Node
	if yaml.Unmarshal(data, &root) != nil || len(root.Content) == 0 {
		return false
	}
	node := root.Content[0]
	return mappingHasKey(node, key)
}
