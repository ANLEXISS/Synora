package network

import (
	"errors"
	"fmt"
	"log"
	"sync"
)

type Manager struct {
	mu sync.RWMutex
}

func NewManager() *Manager {

	return &Manager{}
}

func (m *Manager) Start() error {

	log.Println(
		"network manager starting",
	)

	var failures []error
	steps := []struct {
		name string
		fn   func() error
	}{
		{name: "bridge", fn: EnsureBridge},
		{name: "hostapd", fn: EnsureHostapd},
		{name: "dnsmasq", fn: EnsureDnsmasq},
		{name: "firewall", fn: EnsureFirewall},
	}
	for _, step := range steps {
		if err := step.fn(); err != nil {
			wrapped := fmt.Errorf("%s init failed: %w", step.name, err)
			failures = append(failures, wrapped)
			log.Printf("network component degraded component=%s err=%v", step.name, err)
			continue
		}
		log.Printf("network component ready component=%s", step.name)
	}

	log.Println(
		"SynoraNet ready",
	)

	return errors.Join(failures...)
}
