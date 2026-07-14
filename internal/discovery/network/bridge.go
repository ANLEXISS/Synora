package network

import (
	"bytes"
	"fmt"
	"os/exec"
)

const (
	BridgeName = "synorabr0"
	BridgeCIDR = "10.77.0.1/24"
)

func EnsureBridge() error { return ensureBridge(DefaultConfig().SynoraNet) }

func ensureBridge(cfg SynoraNetConfig) error {
	if cfg.GatewayIP == "" {
		cfg.GatewayIP = DefaultGateway
	}
	exists := exec.Command("ip", "link", "show", BridgeName).Run() == nil
	if !exists {
		var stderr bytes.Buffer
		cmd := exec.Command("ip", "link", "add", "name", BridgeName, "type", "bridge")
		cmd.Stderr = &stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("create bridge failed: %s", stderr.String())
		}
	}
	_ = exec.Command("ip", "addr", "flush", "dev", BridgeName).Run()
	if err := exec.Command("ip", "addr", "add", cfg.GatewayIP+"/24", "dev", BridgeName).Run(); err != nil {
		return fmt.Errorf("bridge ip: %w", err)
	}
	if err := exec.Command("ip", "link", "set", BridgeName, "up").Run(); err != nil {
		return fmt.Errorf("bridge up: %w", err)
	}
	return nil
}
