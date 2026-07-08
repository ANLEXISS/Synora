package manager

import (
	"context"
	"os/exec"
)

func runCommand(
	ctx context.Context,
	name string,
	args ...string,
) ([]byte, error) {
	return exec.CommandContext(
		ctx,
		name,
		args...,
	).CombinedOutput()
}
