package discovery

import (
	"os"
	"sync"

	"gopkg.in/yaml.v3"
)

type DeviceConfig struct {
	ID string `yaml:"id"`

	Type string `yaml:"type"`

	Role string `yaml:"role"`

	Room string `yaml:"room"`

	Secret string `yaml:"secret"`
}

type DevicesConfig struct {
	Devices []DeviceConfig `yaml:"devices"`
}

type DeviceStore struct {
	mu sync.RWMutex

	secrets map[string]string
}

func LoadDevicesConfig(
	path string,
) (*DevicesConfig, error) {

	var cfg DevicesConfig

	data, err := os.ReadFile(path)

	if err != nil {
		return nil, err
	}

	err = yaml.Unmarshal(
		data,
		&cfg,
	)

	if err != nil {
		return nil, err
	}

	return &cfg, nil
}

func NewDeviceStoreFromConfig(
	cfg *DevicesConfig,
) *DeviceStore {

	store := &DeviceStore{
		secrets: map[string]string{},
	}

	for _, dev := range cfg.Devices {

		store.secrets[dev.ID] = dev.Secret
	}

	return store
}

func (s *DeviceStore) GetSecret(
	deviceID string,
) (string, bool) {

	s.mu.RLock()
	defer s.mu.RUnlock()

	secret, ok := s.secrets[deviceID]

	return secret, ok
}

func (s *DeviceStore) SetSecret(
	deviceID string,
	secret string,
) {

	s.mu.Lock()
	defer s.mu.Unlock()

	s.secrets[deviceID] = secret
}