package network

import (
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

	err := EnsureBridge()

	if err != nil {
		return fmt.Errorf(
			"bridge init failed: %w",
			err,
		)
	}

	log.Println(
		"bridge ready",
	)

	err = EnsureHostapd()

	if err != nil {
		return fmt.Errorf(
			"hostapd init failed: %w",
			err,
		)
	}

	log.Println(
		"hostapd ready",
	)

	err = EnsureDnsmasq()

	if err != nil {
		return fmt.Errorf(
			"dnsmasq init failed: %w",
			err,
		)
	}

	log.Println(
		"dnsmasq ready",
	)

	err = EnsureFirewall()

	if err != nil {
		return fmt.Errorf(
			"firewall init failed: %w",
			err,
		)
	}

	log.Println(
		"firewall ready",
	)

	log.Println(
		"SynoraNet ready",
	)

	return nil
}