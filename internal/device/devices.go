package device
import (
	"fmt"
	"os"
	"sort"
	"sync"

	"gopkg.in/yaml.v3"
)

type Device struct {
	ID     string `json:"id" yaml:"id"`
	Type   string `json:"type" yaml:"type"`
	Role   string `json:"role,omitempty" yaml:"role,omitempty"`
	Room   string `json:"room" yaml:"room"`
	NodeID string `json:"node_id,omitempty" yaml:"node_id,omitempty"`
}

type DeviceConfig = Device

type Registry struct {
	mu      sync.RWMutex
	devices map[string]*Device
}

func NewRegistry() *Registry {
	return &Registry{devices: make(map[string]*Device)}
}

func (r *Registry) Register(configs []DeviceConfig) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, cfg := range configs {
		if cfg.ID == "" {
			continue
		}
		nodeID := cfg.NodeID
		if nodeID == "" {
			nodeID = cfg.Room
		}
		r.devices[cfg.ID] = &Device{
			ID:     cfg.ID,
			Type:   cfg.Type,
			Role:   cfg.Role,
			Room:   cfg.Room,
			NodeID: nodeID,
		}
	}
}

func (r *Registry) Replace(configs []DeviceConfig) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.devices = make(map[string]*Device)
	for _, cfg := range configs {
		if cfg.ID == "" {
			continue
		}
		nodeID := cfg.NodeID
		if nodeID == "" {
			nodeID = cfg.Room
		}
		r.devices[cfg.ID] = &Device{ID: cfg.ID, Type: cfg.Type, Role: cfg.Role, Room: cfg.Room, NodeID: nodeID}
	}
}

func (r *Registry) Get(id string) (*Device, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	d, ok := r.devices[id]
	if !ok || d == nil {
		return nil, false
	}
	copy := *d
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
		copy := *d
		out[id] = &copy
	}
	return out
}

func (r *Registry) Ordered() []DeviceConfig {
	r.mu.RLock()
	defer r.mu.RUnlock()
	keys := make([]string, 0, len(r.devices))
	for id := range r.devices {
		keys = append(keys, id)
	}
	sort.Strings(keys)
	out := make([]DeviceConfig, 0, len(keys))
	for _, id := range keys {
		if r.devices[id] == nil {
			continue
		}
		out = append(out, *r.devices[id])
	}
	return out
}

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
	if d.NodeID == "" {
		d.NodeID = d.Room
	}
	return true
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
	if err := yaml.Unmarshal(data, &wrapped); err == nil && len(wrapped.Devices) > 0 {
		normalizeDevices(wrapped.Devices)
		return wrapped.Devices, nil
	}

	var list []DeviceConfig
	if err := yaml.Unmarshal(data, &list); err == nil && len(list) > 0 {
		normalizeDevices(list)
		return list, nil
	}

	var mapped map[string]DeviceConfig
	if err := yaml.Unmarshal(data, &mapped); err == nil && len(mapped) > 0 {
		out := make([]DeviceConfig, 0, len(mapped))
		for id, cfg := range mapped {
			if cfg.ID == "" {
				cfg.ID = id
			}
			out = append(out, cfg)
		}
		normalizeDevices(out)
		return out, nil
	}

	return nil, fmt.Errorf("devices: unsupported devices yaml format")
}

func Save(path string, configs []DeviceConfig) error {
	normalizeDevices(configs)
	wrapped := fileDevices{Devices: configs}
	data, err := yaml.Marshal(&wrapped)
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func normalizeDevices(items []DeviceConfig) {
	for i := range items {
		if items[i].NodeID == "" {
			items[i].NodeID = items[i].Room
		}
	}
}
