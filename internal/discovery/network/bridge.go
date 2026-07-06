package network

import (
	"bytes"
	"fmt"
	"os/exec"
)

const (
	BridgeName = "synorabr0"

	BridgeCIDR = "10.42.0.1/24"
)

func EnsureBridge() error {

	exists := true

	err := exec.Command(
		"ip",
		"link",
		"show",
		BridgeName,
	).Run()

	if err != nil {
		exists = false
	}

	if !exists {

		var stderr bytes.Buffer

		cmd := exec.Command(
			"ip",
			"link",
			"add",
			"name",
			BridgeName,
			"type",
			"bridge",
		)

		cmd.Stderr = &stderr

		err = cmd.Run()

		if err != nil {

			return fmt.Errorf(
				"create bridge failed: %s",
				stderr.String(),
			)
		}
	}

	_ = exec.Command(
		"ip",
		"addr",
		"flush",
		"dev",
		BridgeName,
	).Run()

	err = exec.Command(
		"ip",
		"addr",
		"add",
		BridgeCIDR,
		"dev",
		BridgeName,
	).Run()

	if err != nil {
		return fmt.Errorf(
			"bridge ip: %w",
			err,
		)
	}

	err = exec.Command(
		"ip",
		"link",
		"set",
		BridgeName,
		"up",
	).Run()

	if err != nil {
		return fmt.Errorf(
			"bridge up: %w",
			err,
		)
	}

	return nil
}