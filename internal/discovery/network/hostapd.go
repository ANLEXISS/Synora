package network

import (
	"fmt"
	"os"
	"os/exec"
)

func EnsureHostapd() error {

	err := os.MkdirAll(
		"/run/synora",
		0755,
	)

	if err != nil {
		return err
	}

	config := fmt.Sprintf(`
		interface=%s
		bridge=%s
		ssid=%s

		hw_mode=g
		channel=6

		wmm_enabled=1

		auth_algs=1

		wpa=2
		wpa_passphrase=%s
		wpa_key_mgmt=WPA-PSK

		rsn_pairwise=CCMP
		`,
		WirelessInterface,
		BridgeName,
		SSID,
		Passphrase,
	)

	err = os.WriteFile(
		HostapdConfigPath,
		[]byte(config),
		0600,
	)

	if err != nil {
		return err
	}
	if err := os.Chmod(HostapdConfigPath, 0600); err != nil {
		return err
	}

	cmd := exec.Command(
		"hostapd",
		HostapdConfigPath,
		"-B",
	)

	err = cmd.Run()

	if err != nil {
		return err
	}

	return nil
}
